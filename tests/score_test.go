//go:build integration

package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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
	var f facts.Facts
	for i := 0; i < 2; i++ {
		var err error
		f, err = facts.Collect(ctx, dir, "main", "topic")
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

	// size: 120+60 = 180 lines -> band 51, 5 points; core/calc_test.go is a
	// test, left out with the doc, lock file and generated code.
	// spread: 6 files -> 2, 5 directories (., api, core, docs, ui) -> 4,
	// 2 projects (., ui) -> 3. Every file is new, so degree is 0; the trunk
	// did not move and the branch's one commit is its first, so branch age
	// is 0; core/calc_test.go pairs with core/calc.go, but ui/app.js changes
	// without ui/app.test.js, so tests is 1 -> band 1, 10 points. impact:
	// nothing depends on the changed packages or ui/app.js -> 0; the doc and
	// lock file are left out, and api/svc.pb.go counts as its package.
	// Total 24: Medium.
	want := `{"total":24,"level":"Medium","breakdown":[` +
		`{"measure":"size","fact":"lines","value":180,"min":51,"points":5,` +
		`"from":["core/calc.go","ui/app.js"],` +
		`"excluded":["api/svc.pb.go","core/calc_test.go","docs/guide.md","go.sum"]},` +
		`{"measure":"spread","fact":"files","value":6,"min":6,"points":2,` +
		`"from":["api/svc.pb.go","core/calc.go","core/calc_test.go","docs/guide.md","go.sum","ui/app.js"]},` +
		`{"measure":"spread","fact":"directories","value":5,"min":5,"points":4,` +
		`"from":[".","api","core","docs","ui"]},` +
		`{"measure":"spread","fact":"projects","value":2,"min":2,"points":3,"from":[".","ui"]},` +
		`{"measure":"degree","fact":"existing_lines","value":0,"min":0,"points":0,"from":[]},` +
		fmt.Sprintf(`{"measure":"branch_age","fact":"days","value":0,"min":0,"points":0,"from":["%s..%s"]},`, f.MergeBase, f.Head) +
		fmt.Sprintf(`{"measure":"branch_age","fact":"trunk_commits","value":0,"min":0,"points":0,"from":["%s..%s"]},`, f.MergeBase, f.Base) +
		`{"measure":"tests","fact":"untested_production_files","value":1,"min":1,"points":10,"from":["ui/app.js"]},` +
		`{"measure":"impact","fact":"dependents","value":0,"min":0,"points":0,` +
		`"from":["example.com/m/api","example.com/m/core","ui/app.js"],` +
		`"excluded":["docs/guide.md","go.sum"],` +
		`"dependents":[{"name":"example.com/m/api","dependents":0},{"name":"example.com/m/core","dependents":0},{"name":"ui/app.js","dependents":0}]}]}`
	if runs[0] != want {
		t.Errorf("score =\n%s\nwant\n%s", runs[0], want)
	}
}

// TestReadmeSizeAndDegreeLeaveOutTests pins that the README's Measures
// table says test files are left out of size and degree.
func TestReadmeSizeAndDegreeLeaveOutTests(t *testing.T) {
	readme, err := os.ReadFile("../README.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []string{"| Size |", "| Degree |"} {
		found := false
		for _, line := range strings.Split(string(readme), "\n") {
			if strings.HasPrefix(line, row) {
				found = true
				if !strings.Contains(line, "tests") {
					t.Errorf("README row %q does not say tests are left out: %s", row, line)
				}
			}
		}
		if !found {
			t.Errorf("README has no row starting %q", row)
		}
	}
}

// TestReadmeTestsRowDescribesPairing pins that the README's Measures table
// says the tests measure counts production files changed without their
// paired test.
func TestReadmeTestsRowDescribesPairing(t *testing.T) {
	readme, err := os.ReadFile("../README.md")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, line := range strings.Split(string(readme), "\n") {
		if strings.HasPrefix(line, "| Tests |") {
			found = true
			if !strings.Contains(line, "paired test") {
				t.Errorf("README Tests row does not describe the paired rule: %s", line)
			}
		}
	}
	if !found {
		t.Error("README has no row starting \"| Tests |\"")
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
