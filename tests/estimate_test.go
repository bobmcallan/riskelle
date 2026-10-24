//go:build integration

package tests

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bobmcallan/riskelle/internal/report"
	"github.com/bobmcallan/riskelle/internal/score"
)

// runEstimate runs `riskelle estimate args...` in dir and returns its exit code,
// stdout and stderr.
func runEstimate(t *testing.T, bin, dir string, args ...string) (int, string, string) {
	t.Helper()
	cmd := exec.Command(bin, append([]string{"estimate"}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Dir, cmd.Stdout, cmd.Stderr = dir, &stdout, &stderr
	if err := cmd.Run(); err != nil && cmd.ProcessState == nil {
		t.Fatalf("riskelle estimate %v: %v", args, err)
	}
	return cmd.ProcessState.ExitCode(), stdout.String(), stderr.String()
}

// TestEstimateScoresLikeTheActualDiff commits a real change on a fixture
// repository, writes the plan predicting exactly that change, and checks the
// built binary's estimate scores the same as the actual diff collected from
// git and scored with the target's rules: total, level, breakdown and rules.
func TestEstimateScoresLikeTheActualDiff(t *testing.T) {
	// Built from the working tree, so the binary carries the code under test.
	bin := filepath.Join(t.TempDir(), "riskelle")
	if out, err := exec.Command("go", "build", "-o", bin, "../cmd/riskelle").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	dir := rulesFixture(t,
		map[string]string{"go.mod": "module example.com/m\n", ".risk/rules.json": `{"generated":["gen/**"]}` + "\n",
			"core/calc.go": numbered("package core // ", 40)},
		map[string]string{"core/calc.go": numbered("package core // ", 50), // +10
			"core/calc_test.go": numbered("package core // ", 30), "ui/package.json": "{}\n",
			"ui/app.js": numbered("// ", 60), "gen/api.go": numbered("package gen // ", 300),
			"docs/guide.md": numbered("# ", 300)})
	// Untracked: estimate reads only the target commit, not the working tree.
	writeFile(t, dir, "plan.json", `{"target": "main", "files": [
		{"path": "ui/app.js", "added": 60}, {"path": "core/calc.go", "added": 10},
		{"path": "core/calc_test.go", "added": 30}, {"path": "ui/package.json", "added": 1},
		{"path": "gen/api.go", "added": 300}, {"path": "docs/guide.md", "added": 300}]}`)

	_, actual, err := collectAndScore(t, dir)
	if err != nil {
		t.Fatalf("rules.Score: %v", err)
	}
	// Pinned from the rules: size 10+30+1+60 = 101 lines (gen/** by the
	// target's rules, docs by class, left out) -> 5; 6 files -> 2; 4
	// directories (core, docs, gen, ui) -> 2; 2 projects (., ui) -> 3; a test
	// is changed and nothing else reaches a band: 12 Low.
	if actual.Total != 12 || actual.Level != score.Low || actual.Provenance.RulesSource != ".risk/rules.json" {
		t.Fatalf("actual diff scores %d %s with %q, want 12 Low with .risk/rules.json",
			actual.Total, actual.Level, actual.Provenance.RulesSource)
	}

	code, stdout, stderr := runEstimate(t, bin, dir, "plan.json")
	if code != 0 {
		t.Fatalf("riskelle estimate plan.json: exit %d, stderr %q", code, stderr)
	}
	if _, again, _ := runEstimate(t, bin, dir, "plan.json"); again != stdout {
		t.Errorf("estimate differs between runs:\n%s\n%s", stdout, again)
	}
	var got report.Report
	dec := json.NewDecoder(strings.NewReader(stdout))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("decoding estimate output %q: %v", stdout, err)
	}
	if got.Kind != "estimate" || got.Plan != "plan.json" || got.Context.Note != report.Note {
		t.Errorf("kind, plan, note = %q, %q, %q; want \"estimate\", \"plan.json\", report.Note", got.Kind, got.Plan, got.Context.Note)
	}
	// Two things differ by nature, not by scoring: the test binary and the
	// built binary report different builds (the rules source, commit and hash
	// must match), and a plan has no commits, so its branch_age entries name
	// no commit range.
	got.Provenance.Version, actual.Provenance.Version = "", ""
	for i, p := range actual.Breakdown {
		if p.Measure == score.MeasureBranchAge {
			actual.Breakdown[i].From = []string{}
		}
	}
	want := report.New(report.KindEstimate, actual)
	want.Plan = "plan.json"
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		t.Errorf("estimate scores\n%s\nactual diff scores\n%s", gotJSON, wantJSON)
	}

	// The Markdown is the same report rendered for a PR comment, and stable.
	code, md, stderr := runEstimate(t, bin, dir, "--format", "markdown", "plan.json")
	if code != 0 {
		t.Fatalf("riskelle estimate --format markdown: exit %d, stderr %q", code, stderr)
	}
	if _, again, _ := runEstimate(t, bin, dir, "--format", "markdown", "plan.json"); again != md {
		t.Errorf("markdown estimate differs between runs:\n%s\n%s", md, again)
	}
	var wantMD strings.Builder
	var decoded report.Report
	json.Unmarshal([]byte(stdout), &decoded)
	decoded.Markdown(&wantMD)
	if md != wantMD.String() || !strings.HasPrefix(md, "## riskelle estimate: 12 / 100, Low\n") {
		t.Errorf("markdown estimate:\n%s\nwant the JSON report rendered:\n%s", md, wantMD.String())
	}

	t.Run("refusals", func(t *testing.T) {
		writeFile(t, dir, "no-target.json", `{"files":[{"path":"a.go","added":1}]}`)
		writeFile(t, dir, "bad-target.json", `{"target":"nope","files":[{"path":"a.go","added":1}]}`)
		for plan, want := range map[string]string{
			"no-target.json": `missing "target"`, "bad-target.json": `"nope"`, "missing.json": "missing.json",
		} {
			code, stdout, stderr := runEstimate(t, bin, dir, plan)
			if code != 1 || stdout != "" || !strings.Contains(stderr, plan) || !strings.Contains(stderr, want) {
				t.Errorf("estimate %s: exit %d, stdout %q, stderr %q; want exit 1, no stdout, stderr naming %s and %q",
					plan, code, stdout, stderr, plan, want)
			}
		}
	})
}
