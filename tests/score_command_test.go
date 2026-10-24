//go:build integration

package tests

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bobmcallan/riskelle/internal/report"
	"github.com/bobmcallan/riskelle/internal/score"
)

// runScore runs `riskelle score args...` in dir and returns its exit code,
// stdout and stderr.
func runScore(t *testing.T, bin, dir string, args ...string) (int, string, string) {
	t.Helper()
	cmd := exec.Command(bin, append([]string{"score"}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Dir, cmd.Stdout, cmd.Stderr = dir, &stdout, &stderr
	if err := cmd.Run(); err != nil && cmd.ProcessState == nil {
		t.Fatalf("riskelle score %v: %v", args, err)
	}
	return cmd.ProcessState.ExitCode(), stdout.String(), stderr.String()
}

// TestScoreCommand drives the built binary's `score --base main --head topic`
// on a fixture whose merge base, head, target tip and working tree each hold
// different .risk/rules.json, and whose trunk moved after the branch point.
// The report is the merge-base..head diff scored with the target tip's rules,
// kind "score", naming the full base and head SHAs, in JSON (the default) and
// Markdown from the one renderer; bad refs and flags are refused.
func TestScoreCommand(t *testing.T) {
	// Built from the working tree, so the binary carries the code under test.
	bin := filepath.Join(t.TempDir(), "riskelle")
	if out, err := exec.Command("go", "build", "-o", bin, "../cmd/riskelle").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	dir := rulesFixture(t,
		map[string]string{"go.mod": "module example.com/m\n", ".risk/rules.json": `{"generated":["gen/**"]}` + "\n",
			"core/calc.go": numbered("package core // ", 40)},
		map[string]string{"core/calc.go": numbered("package core // ", 50),
			"core/calc_test.go": numbered("package core // ", 30), "gen/api.go": numbered("package gen // ", 300),
			"src/app.go": numbered("package src // ", 100),
			// The head tries to hide its own code as generated.
			".risk/rules.json": `{"generated":["gen/**","src/**"]}` + "\n"})
	// The trunk moves on after the branch point: a diff from the base tip
	// rather than the merge base would show other/x.go.
	writeFile(t, dir, "other/x.go", numbered("package other // ", 20))
	writeFile(t, dir, ".risk/rules.json", `{"generated":["gen/**","assets/**"]}`+"\n")
	fixtureGit(t, dir, "add", ".")
	fixtureGit(t, dir, "commit", "--quiet", "-m", "trunk moves")
	// Uncommitted rules in the working tree never score.
	writeFile(t, dir, ".risk/rules.json", `{"generated":["**"]}`+"\n")
	mainSHA := strings.TrimSpace(fixtureGit(t, dir, "rev-parse", "main"))
	topicSHA := strings.TrimSpace(fixtureGit(t, dir, "rev-parse", "topic"))

	// After the build: collectAndScore changes the working directory.
	f, actual, err := collectAndScore(t, dir)
	if err != nil {
		t.Fatalf("rules.Score: %v", err)
	}
	want := report.New(report.KindScore, actual)
	want.Base, want.Head = f.Base, f.Head

	stable := func(args ...string) string {
		t.Helper()
		code, out, stderr := runScore(t, bin, dir, args...)
		if code != 0 || stderr != "" {
			t.Fatalf("riskelle score %v: exit %d, stderr %q", args, code, stderr)
		}
		if _, again, _ := runScore(t, bin, dir, args...); again != out {
			t.Errorf("riskelle score %v differs between runs:\n%s\n%s", args, out, again)
		}
		return out
	}

	out := stable("--base", "main", "--head", "topic")
	if def := stable("--format", "json", "--base", "main", "--head", "topic"); def != out {
		t.Errorf("--format json differs from the default:\n%s\n%s", def, out)
	}
	var got report.Report
	dec := json.NewDecoder(strings.NewReader(out))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("decoding score output %q: %v", out, err)
	}
	if got.Kind != "score" || got.Base != mainSHA || got.Head != topicSHA || got.Plan != "" {
		t.Errorf("kind, base, head, plan = %q, %q, %q, %q; want score, %s, %s and no plan",
			got.Kind, got.Base, got.Head, got.Plan, mainSHA, topicSHA)
	}
	pv := got.Provenance
	if pv.Version == "" || pv.RulesSource != ".risk/rules.json" || pv.RulesCommit != mainSHA {
		t.Errorf("provenance = %+v, want a version and .risk/rules.json at %s", pv, mainSHA)
	}
	var size *score.Point
	for i := range got.Breakdown {
		if got.Breakdown[i].Measure == score.MeasureSize {
			size = &got.Breakdown[i]
		}
	}
	if size == nil || !slices.Contains(size.From, "src/app.go") || !slices.Contains(size.Excluded, "gen/api.go") {
		t.Errorf("size = %+v, want src/app.go counted (head's rules ignored) and gen/api.go excluded (target's)", size)
	}
	if strings.Contains(out, "other/x.go") {
		t.Errorf("report counts other/x.go, a trunk change after the merge base:\n%s", out)
	}
	// The test binary and the built binary are different builds; everything
	// else is the change collected and scored with the target's rules.
	got.Provenance.Version, want.Provenance.Version = "", ""
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		t.Errorf("score reports\n%s\nwant\n%s", gotJSON, wantJSON)
	}

	md := stable("--format", "markdown", "--base", "main", "--head", "topic")
	var decoded report.Report
	json.Unmarshal([]byte(out), &decoded)
	var wantMD strings.Builder
	decoded.Markdown(&wantMD)
	if md != wantMD.String() {
		t.Errorf("markdown:\n%s\nwant the JSON report rendered:\n%s", md, wantMD.String())
	}
	if !strings.HasPrefix(md, "## riskelle score: ") ||
		!strings.Contains(md, "Diff of head `"+topicSHA+"` against base `"+mainSHA+"`") {
		t.Errorf("markdown lacks the score heading or the base and head commits:\n%s", md)
	}

	t.Run("refusals", func(t *testing.T) {
		for _, tt := range []struct {
			args []string
			code int
			want string
		}{
			{[]string{"--base", "main", "--head", "nope"}, 1, `"nope"`},
			{[]string{"--base", "nope", "--head", "topic"}, 1, `"nope"`},
			{[]string{"--base", "main"}, 2, "needs --base and --head"},
			{[]string{"--format", "xml", "--base", "main", "--head", "topic"}, 2, `unknown format "xml"`},
		} {
			code, stdout, stderr := runScore(t, bin, dir, tt.args...)
			if code != tt.code || stdout != "" || !strings.Contains(stderr, tt.want) {
				t.Errorf("score %v: exit %d, stdout %q, stderr %q; want exit %d, no stdout, stderr containing %q",
					tt.args, code, stdout, stderr, tt.code, tt.want)
			}
		}
	})
}
