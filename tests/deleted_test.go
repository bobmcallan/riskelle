//go:build integration

package tests

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/bobmcallan/riskelle/internal/facts"
	"github.com/bobmcallan/riskelle/internal/report"
	"github.com/bobmcallan/riskelle/internal/score"
)

// deletionFixture commits base on main, then on topic removes rm (a file or a
// directory) and changes nothing else, and returns to main.
func deletionFixture(t *testing.T, base map[string]string, rm string) string {
	t.Helper()
	dir := t.TempDir()
	fixtureGit(t, dir, "init", "--quiet", "--initial-branch=main")
	for name, content := range base {
		writeFile(t, dir, name, content)
	}
	fixtureGit(t, dir, "add", ".")
	fixtureGit(t, dir, "commit", "--quiet", "-m", "base")
	fixtureGit(t, dir, "switch", "--quiet", "-c", "topic")
	fixtureGit(t, dir, "rm", "-r", "--quiet", rm)
	fixtureGit(t, dir, "commit", "--quiet", "-m", "delete "+rm)
	fixtureGit(t, dir, "switch", "--quiet", "main")
	return dir
}

// TestDeletedLegacyTreeScoresMedium models a change that deletes a whole
// legacy tree nothing references. On main, app/ stays
// and references nothing in legacy/; legacy/ holds three projects (admin,
// portal, shared), each with three directories of two 50-line files. topic
// deletes legacy/ and nothing else. Every deleted file has 0 dependents, so
// size, degree and tests are 0 and the total is spread alone: 21 files (8),
// 12 directories (6), 3 projects (6) = 20, Medium. Counted in full it would
// be 900 lines (30), 900 existing lines (15) and 18 untested files (10): the High
// band, reported Medium.
func TestDeletedLegacyTreeScoresMedium(t *testing.T) {
	base := map[string]string{
		"app/package.json": "{}\n",
		"app/index.js":     "export const app = 1\n",
	}
	var deleted []string
	for _, p := range []string{"admin", "portal", "shared"} {
		base["legacy/"+p+"/package.json"] = "{}\n"
		deleted = append(deleted, "legacy/"+p+"/package.json")
		for _, d := range []string{"a", "b", "c"} {
			for _, f := range []string{"one.js", "two.js"} {
				name := "legacy/" + p + "/" + d + "/" + f
				base[name] = numbered("export const "+p+d+"_", 50)
				deleted = append(deleted, name)
			}
		}
	}
	sort.Strings(deleted)
	dir := deletionFixture(t, base, "legacy")

	f, res, err := collectAndScore(t, dir)
	if err != nil {
		t.Fatalf("rules.Score: %v", err)
	}
	if len(f.Files) != len(deleted) {
		t.Fatalf("files %+v, want the %d deleted paths", f.Files, len(deleted))
	}
	for _, file := range f.Files {
		if !file.Deleted || file.New {
			t.Errorf("%s deleted %v new %v, want deleted only", file.Path, file.Deleted, file.New)
		}
	}
	for _, d := range f.Impact.Files {
		if d.Dependents != 0 {
			t.Errorf("impact %s has %d dependents, want 0", d.Name, d.Dependents)
		}
	}

	want := map[string]struct{ value, points int }{
		"size/lines":                      {0, 0},
		"spread/files":                    {21, 8},
		"spread/directories":              {12, 6},
		"spread/projects":                 {3, 6},
		"degree/existing_lines":           {0, 0},
		"tests/untested_production_files": {0, 0},
		"impact/dependents":               {0, 0},
	}
	for _, p := range res.Breakdown {
		w, ok := want[p.Measure+"/"+p.Fact]
		if !ok {
			continue
		}
		if p.Value != w.value || p.Points != w.points {
			t.Errorf("%s/%s = %d (%d points), want %d (%d)", p.Measure, p.Fact, p.Value, p.Points, w.value, w.points)
		}
		if p.Measure == score.MeasureSize && !reflect.DeepEqual(p.Excluded, deleted) {
			t.Errorf("size excluded %v, want every deleted path %v", p.Excluded, deleted)
		}
	}
	if res.Total != 20 || res.Level != score.Medium || res.ShapeLevel != score.Medium {
		t.Errorf("total %d level %s shape %s, want 20 Medium Medium", res.Total, res.Level, res.ShapeLevel)
	}
}

// TestDeletedFileStillReferencedCounts: a deleted file something at head
// other than docs still refers to counts in full in size, degree and tests,
// though a README names it too.
func TestDeletedFileStillReferencedCounts(t *testing.T) {
	dir := deletionFixture(t, map[string]string{
		"lib/old.js":   numbered("export const old", 60),
		"app/index.js": "require('lib/old.js')\n",
		"README.md":    "lib/old.js is the old library\n",
	}, "lib/old.js")

	f, res, err := collectAndScore(t, dir)
	if err != nil {
		t.Fatalf("rules.Score: %v", err)
	}
	if len(f.Files) != 1 || !f.Files[0].Deleted {
		t.Fatalf("files %+v, want lib/old.js deleted", f.Files)
	}
	if len(f.Impact.Files) != 1 || f.Impact.Files[0].Dependents != 2 {
		t.Fatalf("impact %+v, want lib/old.js with 2 dependents", f.Impact.Files)
	}
	if want := []string{"README.md", "app/index.js"}; !reflect.DeepEqual(f.Files[0].Referrers, want) {
		t.Errorf("referrers %v, want %v", f.Files[0].Referrers, want)
	}
	from := []string{"lib/old.js"}
	for _, p := range res.Breakdown {
		switch p.Measure {
		case score.MeasureSize:
			if p.Value != 60 || p.Points != 5 || !reflect.DeepEqual(p.From, from) || p.Excluded != nil {
				t.Errorf("size = %+v, want 60 (5) from %v, nothing excluded", p, from)
			}
		case score.MeasureDegree, score.MeasureTests:
			if !reflect.DeepEqual(p.From, from) {
				t.Errorf("%s from %v, want %v", p.Measure, p.From, from)
			}
		}
	}
}

// TestDeletedFileOnlyAReadmeNames drives the built binary on a change that
// deletes flow/foundry.py, which only a README at head names. A doc naming a
// deleted file does not rely on it: size, degree and tests are 0 with the file
// in size's excluded paths, while impact still counts the README.
func TestDeletedFileOnlyAReadmeNames(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "riskelle")
	if out, err := exec.Command("go", "build", "-o", bin, "../cmd/riskelle").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	dir := deletionFixture(t, map[string]string{
		"flow/foundry.py": numbered("x", 60),
		"README.md":       "The Foundry review (flow/foundry.py) is removed.\n",
	}, "flow/foundry.py")

	code, out, stderr := runScore(t, bin, dir, "--base", "main", "--head", "topic")
	if code != 0 || stderr != "" {
		t.Fatalf("riskelle score: exit %d, stderr %q", code, stderr)
	}
	var rep report.Report
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("decode report: %v\n%s", err, out)
	}
	seen := 0
	for _, p := range rep.Breakdown {
		switch p.Measure + "/" + p.Fact {
		case "size/lines":
			seen++
			if p.Value != 0 || p.Points != 0 || len(p.From) != 0 || !reflect.DeepEqual(p.Excluded, []string{"flow/foundry.py"}) {
				t.Errorf("size = %+v, want 0 (0) with flow/foundry.py excluded", p)
			}
		case "degree/existing_lines":
			seen++
			if p.Value != 0 || p.Points != 0 {
				t.Errorf("degree = %+v, want 0 (0)", p)
			}
		case "tests/untested_production_files":
			seen++
			if p.Value != 0 {
				t.Errorf("tests = %+v, want 0", p)
			}
		case "impact/dependents":
			seen++
			want := []facts.Dependency{{Name: "flow/foundry.py", Dependents: 1}}
			if p.Value != 1 || !reflect.DeepEqual(p.Dependents, want) {
				t.Errorf("impact = %+v, want 1 with %v", p, want)
			}
		}
	}
	if seen != 4 {
		t.Errorf("found %d of the 4 measures in %s", seen, out)
	}

	// After the run: collectAndScore changes the working directory.
	f, _, err := collectAndScore(t, dir)
	if err != nil {
		t.Fatalf("rules.Score: %v", err)
	}
	if len(f.Files) != 1 || !reflect.DeepEqual(f.Files[0].Referrers, []string{"README.md"}) {
		t.Errorf("files %+v, want flow/foundry.py referred to by README.md", f.Files)
	}
}
