//go:build integration

package tests

import (
	"reflect"
	"sort"
	"testing"

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
// be 900 lines (30), 900 existing lines (15) and 18 untested files (10): High.
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

// TestDeletedFileStillReferencedCounts: a deleted file something at head still
// refers to counts in full in size, degree and tests.
func TestDeletedFileStillReferencedCounts(t *testing.T) {
	dir := deletionFixture(t, map[string]string{
		"lib/old.js":   numbered("export const old", 60),
		"app/index.js": "require('lib/old.js')\n",
	}, "lib/old.js")

	f, res, err := collectAndScore(t, dir)
	if err != nil {
		t.Fatalf("rules.Score: %v", err)
	}
	if len(f.Files) != 1 || !f.Files[0].Deleted {
		t.Fatalf("files %+v, want lib/old.js deleted", f.Files)
	}
	if len(f.Impact.Files) != 1 || f.Impact.Files[0].Dependents != 1 {
		t.Fatalf("impact %+v, want lib/old.js with 1 dependent", f.Impact.Files)
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
