//go:build integration

package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/bobmcallan/riskelle/internal/facts"
	"github.com/bobmcallan/riskelle/internal/score"
)

// TestScoreCollectedFacts scores a change collected from a fixture repository
// with the built-in rules, the way the score command will: Collect, then
// Score. The branch adds code across two projects, a doc, a lock file and
// generated code. It pins the result's JSON and that it is the same twice.
// The working directory is an empty temp dir, so no rules file is read.
func TestScoreCollectedFacts(t *testing.T) {
	dir := t.TempDir()
	fixtureGit(t, dir, "init", "--quiet", "--initial-branch=main")
	writeFile(t, dir, "go.mod", "module example.com/m\n")
	writeFile(t, dir, "ui/package.json", "{}\n")
	fixtureGit(t, dir, "add", ".")
	fixtureGit(t, dir, "commit", "--quiet", "-m", "base")

	fixtureGit(t, dir, "switch", "--quiet", "-c", "topic")
	writeFile(t, dir, "core/calc.go", numbered("package core // ", 120))
	writeFile(t, dir, "core/calc_test.go", numbered("package core // ", 30))
	writeFile(t, dir, "ui/app.js", numbered("// ", 60))
	// Left out of size: 900 lines of doc, lock and generated code.
	writeFile(t, dir, "docs/guide.md", numbered("# ", 300))
	writeFile(t, dir, "go.sum", numbered("example.com/x v1 h1:", 300))
	writeFile(t, dir, "api/svc.pb.go", numbered("package api // ", 300))
	fixtureGit(t, dir, "add", ".")
	fixtureGit(t, dir, "commit", "--quiet", "-m", "topic")

	t.Chdir(t.TempDir())
	ctx := context.Background()
	var runs []string
	for i := 0; i < 2; i++ {
		f, err := facts.Collect(ctx, dir, "main", "topic")
		if err != nil {
			t.Fatalf("Collect: %v", err)
		}
		out, err := json.Marshal(score.Score(f, score.Default()))
		if err != nil {
			t.Fatal(err)
		}
		runs = append(runs, string(out))
	}
	if runs[0] != runs[1] {
		t.Errorf("score differs between runs:\n%s\n%s", runs[0], runs[1])
	}

	// size: 120+30+60 = 210 lines -> band 201, 10 points.
	// spread: 6 files -> 2, 5 directories (., api, core, docs, ui) -> 4,
	// 2 projects (., ui) -> 3. impact: nothing depends on the changed
	// packages or ui/app.js -> 0; the doc and lock file are left out, and
	// api/svc.pb.go counts as its package. Total 19: Low.
	want := `{"total":19,"level":"Low","breakdown":[` +
		`{"measure":"size","fact":"lines","value":210,"min":201,"points":10,` +
		`"from":["core/calc.go","core/calc_test.go","ui/app.js"],` +
		`"excluded":["api/svc.pb.go","docs/guide.md","go.sum"]},` +
		`{"measure":"spread","fact":"files","value":6,"min":6,"points":2,` +
		`"from":["api/svc.pb.go","core/calc.go","core/calc_test.go","docs/guide.md","go.sum","ui/app.js"]},` +
		`{"measure":"spread","fact":"directories","value":5,"min":5,"points":4,` +
		`"from":[".","api","core","docs","ui"]},` +
		`{"measure":"spread","fact":"projects","value":2,"min":2,"points":3,"from":[".","ui"]},` +
		`{"measure":"impact","fact":"dependents","value":0,"min":0,"points":0,` +
		`"from":["example.com/m/api","example.com/m/core","ui/app.js"],` +
		`"excluded":["docs/guide.md","go.sum"],` +
		`"dependents":[{"name":"example.com/m/api","dependents":0},{"name":"example.com/m/core","dependents":0},{"name":"ui/app.js","dependents":0}]}]}`
	if runs[0] != want {
		t.Errorf("score =\n%s\nwant\n%s", runs[0], want)
	}
}

// numbered returns n distinct lines, each prefix followed by its number.
func numbered(prefix string, n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "%s%d\n", prefix, i)
	}
	return b.String()
}
