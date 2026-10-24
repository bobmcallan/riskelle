//go:build integration

package tests

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/bobmcallan/riskelle/internal/facts"
	"github.com/bobmcallan/riskelle/internal/score"
)

// TestScoreDegreeAgeAndTests collects a change from a fixture repository and
// scores it with the built-in rules, pinning the degree, branch age and tests
// entries against values worked out from the fixture by hand. The branch
// rewrites 30 lines of an existing file with no test, adds a new file eight
// days later, and the trunk gains 11 commits meanwhile. A second head adds a
// test on top of the same branch, which takes the tests points away.
func TestScoreDegreeAgeAndTests(t *testing.T) {
	dir := t.TempDir()
	at := func(author, committer string, args ...string) {
		t.Helper()
		fixtureGitAt(t, dir, author, committer, args...)
	}
	fixtureGit(t, dir, "init", "--quiet", "--initial-branch=main")
	writeFile(t, dir, "core/calc.go", numbered("old ", 40))
	fixtureGit(t, dir, "add", ".")
	at("2031-01-01T09:00:00Z", "2031-01-01T09:00:00Z", "commit", "--quiet", "-m", "base")

	fixtureGit(t, dir, "switch", "--quiet", "-c", "topic")
	// Lines 0-29 rewritten, 30-39 kept: 30 added and 30 removed in an
	// existing file.
	writeFile(t, dir, "core/calc.go", numbered("new ", 30)+strings.TrimPrefix(numbered("old ", 40), numbered("old ", 30)))
	fixtureGit(t, dir, "add", ".")
	at("2031-01-02T09:00:00Z", "2031-01-02T09:00:00Z", "commit", "--quiet", "-m", "rewrite")
	writeFile(t, dir, "core/extra.go", numbered("extra ", 20))
	fixtureGit(t, dir, "add", ".")
	at("2031-01-10T09:00:00Z", "2031-01-10T09:00:00Z", "commit", "--quiet", "-m", "extra")

	fixtureGit(t, dir, "switch", "--quiet", "-c", "tested")
	writeFile(t, dir, "core/calc_test.go", numbered("test ", 5))
	fixtureGit(t, dir, "add", ".")
	at("2031-01-10T10:00:00Z", "2031-01-10T10:00:00Z", "commit", "--quiet", "-m", "test")

	fixtureGit(t, dir, "switch", "--quiet", "main")
	for i := 0; i < 11; i++ {
		name := fmt.Sprintf("trunk%02d.txt", i)
		writeFile(t, dir, name, name+"\n")
		fixtureGit(t, dir, "add", name)
		at("2031-01-05T09:00:00Z", "2031-01-05T09:00:00Z", "commit", "--quiet", "-m", name)
	}

	t.Chdir(t.TempDir())
	ctx := context.Background()
	collect := func(head string) (facts.Facts, score.Result) {
		t.Helper()
		f, err := facts.Collect(ctx, dir, "main", head)
		if err != nil {
			t.Fatalf("Collect %s: %v", head, err)
		}
		return f, score.Score(f, score.Default())
	}
	check := func(r score.Result, measure, fact string, value, min, points int, from []string) {
		t.Helper()
		for _, p := range r.Breakdown {
			if p.Measure == measure && p.Fact == fact {
				if p.Value != value || p.Min != min || p.Points != points || !reflect.DeepEqual(p.From, from) {
					t.Errorf("%s/%s = %+v, want value %d band %d points %d from %v", measure, fact, p, value, min, points, from)
				}
				return
			}
		}
		t.Errorf("no %s/%s in %+v", measure, fact, r.Breakdown)
	}

	f, r := collect("topic")
	// degree: 30 added + 30 removed in core/calc.go, which existed at the
	// merge base; core/extra.go is new. 60 -> band 26, 2 points.
	check(r, "degree", "existing_lines", 60, 26, 2, []string{"core/calc.go"})
	// branch age: authored 01-02 09:00 to head committed 01-10 09:00 is 8 days
	// -> band 8, 6 points. 11 trunk commits -> band 11, 2 points.
	check(r, "branch_age", "days", 8, 8, 6, []string{f.MergeBase + ".." + f.Head})
	check(r, "branch_age", "trunk_commits", 11, 11, 2, []string{f.MergeBase + ".." + f.Base})
	// tests: two production files, no test -> 10 points.
	check(r, "tests", "untested_production_files", 2, 1, 10, []string{"core/calc.go", "core/extra.go"})
	// size 80 lines -> 5; spread 2 files, 1 directory, no project -> 0.
	// 5 + 2 + 6 + 2 + 10 = 25: Medium.
	if r.Total != 25 || r.Level != score.Medium {
		t.Errorf("topic: total %d level %q, want 25 Medium", r.Total, r.Level)
	}

	f, r = collect("tested")
	// The same change with a test: tests scores nothing, the rest holds
	// (age now runs to 01-10 10:00, still 8 whole days).
	check(r, "tests", "untested_production_files", 0, 0, 0, []string{})
	check(r, "degree", "existing_lines", 60, 26, 2, []string{"core/calc.go"})
	check(r, "branch_age", "days", 8, 8, 6, []string{f.MergeBase + ".." + f.Head})
	check(r, "branch_age", "trunk_commits", 11, 11, 2, []string{f.MergeBase + ".." + f.Base})
	// size 85 lines -> 5; 5 + 2 + 6 + 2 = 15: Low.
	if r.Total != 15 || r.Level != score.Low {
		t.Errorf("tested: total %d level %q, want 15 Low", r.Total, r.Level)
	}
}
