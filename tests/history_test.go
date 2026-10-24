//go:build integration

package tests

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/bobmcallan/riskelle/internal/facts"
)

// TestCollectBranchAgeAndTrunkDrift drives collection from outside the package
// against a branch kept open for weeks that merged the trunk in part way: the
// merge moves the merge base, so drift counts only trunk commits after it,
// while age still runs from the branch's first commit. The history is dated
// in 2031, so a value read from the wall clock could not match.
func TestCollectBranchAgeAndTrunkDrift(t *testing.T) {
	dir := t.TempDir()
	commit := func(author, committer, name string) {
		t.Helper()
		writeFile(t, dir, name, name+"\n")
		fixtureGitAt(t, dir, author, committer, "add", name)
		fixtureGitAt(t, dir, author, committer, "commit", "--quiet", "-m", name)
	}
	fixtureGit(t, dir, "init", "--quiet", "--initial-branch=main")
	commit("2031-05-01T09:00:00Z", "2031-05-01T09:00:00Z", "base.txt")

	fixtureGit(t, dir, "switch", "--quiet", "-c", "topic")
	commit("2031-05-02T09:00:00Z", "2031-05-02T09:00:00Z", "topic1.txt")

	fixtureGit(t, dir, "switch", "--quiet", "main")
	commit("2031-05-05T09:00:00Z", "2031-05-05T09:00:00Z", "trunk1.txt")
	commit("2031-05-06T09:00:00Z", "2031-05-06T09:00:00Z", "trunk2.txt")

	fixtureGit(t, dir, "switch", "--quiet", "topic")
	fixtureGitAt(t, dir, "2031-05-10T09:00:00Z", "2031-05-10T09:00:00Z",
		"merge", "--quiet", "--no-edit", "--no-ff", "main")
	commit("2031-05-12T09:00:00Z", "2031-05-20T08:59:59Z", "topic2.txt")

	fixtureGit(t, dir, "switch", "--quiet", "main")
	commit("2031-05-15T09:00:00Z", "2031-05-15T09:00:00Z", "trunk3.txt")
	commit("2031-05-16T09:00:00Z", "2031-05-16T09:00:00Z", "trunk4.txt")
	commit("2031-05-17T09:00:00Z", "2031-05-17T09:00:00Z", "trunk5.txt")

	ctx := context.Background()
	got, err := facts.Collect(ctx, dir, "main", "topic")
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	again, err := facts.Collect(ctx, dir, "main", "topic")
	if err != nil {
		t.Fatalf("Collect again: %v", err)
	}
	gotJSON, _ := json.Marshal(got)
	againJSON, _ := json.Marshal(again)
	if string(gotJSON) != string(againJSON) {
		t.Errorf("facts differ between runs:\n%s\n%s", gotJSON, againJSON)
	}

	var history struct {
		MergeBase     string `json:"merge_base"`
		BranchAgeDays *int   `json:"branch_age_days"`
		TrunkCommits  *int   `json:"trunk_commits"`
	}
	if err := json.Unmarshal(gotJSON, &history); err != nil {
		t.Fatal(err)
	}
	if want := fixtureGit(t, dir, "rev-parse", "main~3"); history.MergeBase != want {
		t.Fatalf("merge_base = %s, want trunk2 %s (the merged-in trunk)", history.MergeBase, want)
	}
	// topic1 authored 05-02 09:00 to the head's commit at 05-20 08:59:59:
	// 17 days 23:59:59, floored to 17.
	if history.BranchAgeDays == nil || *history.BranchAgeDays != 17 {
		t.Errorf("branch_age_days = %v, want 17 in %s", deref(history.BranchAgeDays), gotJSON)
	}
	// trunk3..trunk5 landed after the merge base; trunk1 and trunk2 were
	// merged into the branch and do not count.
	if history.TrunkCommits == nil || *history.TrunkCommits != 3 {
		t.Errorf("trunk_commits = %v, want 3 in %s", deref(history.TrunkCommits), gotJSON)
	}

	// The branch point itself as head: no commits of its own, so age 0, while
	// drift still counts the trunk's five commits since it.
	point := fixtureGit(t, dir, "rev-parse", "main~5")
	empty, err := facts.Collect(ctx, dir, "main", point)
	if err != nil {
		t.Fatalf("Collect at the branch point: %v", err)
	}
	if empty.BranchAgeDays != 0 || empty.TrunkCommits != 5 {
		t.Errorf("at the branch point: age, drift = %d, %d, want 0, 5", empty.BranchAgeDays, empty.TrunkCommits)
	}
}

func deref(p *int) any {
	if p == nil {
		return "missing"
	}
	return *p
}
