package facts

import (
	"context"
	"os/exec"
	"reflect"
	"testing"
)

// datedFixture is a repository whose commits carry chosen dates, set in 2031
// so that an age read from the wall clock could not match.
type datedFixture struct {
	dir, branchPoint string
}

// newDatedFixture builds:
//
//	main:  base (2031-03-01) - t1 (03-02) - t2 (03-04) - t3 (03-06)
//	topic: base - c1 (author+committer 2031-03-03T22:00+10:00, i.e. 12:00Z)
//	            - c2 (author 2031-03-05T00:00Z, committer 2031-03-10T11:59:59Z)
func newDatedFixture(t *testing.T) datedFixture {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	commit := func(author, committer, name string) {
		t.Helper()
		write(t, dir, name, name+"\n")
		runGitAt(t, dir, author, committer, "add", name)
		runGitAt(t, dir, author, committer, "commit", "--quiet", "-m", name)
	}
	runGit(t, dir, "init", "--quiet", "--initial-branch=main")
	commit("2031-03-01T00:00:00Z", "2031-03-01T00:00:00Z", "base.txt")
	f := datedFixture{dir: dir, branchPoint: runGit(t, dir, "rev-parse", "HEAD")}

	runGit(t, dir, "switch", "--quiet", "-c", "topic")
	commit("2031-03-03T22:00:00+10:00", "2031-03-03T22:00:00+10:00", "c1.txt")
	commit("2031-03-05T00:00:00Z", "2031-03-10T11:59:59Z", "c2.txt")

	runGit(t, dir, "switch", "--quiet", "main")
	commit("2031-03-02T00:00:00Z", "2031-03-02T00:00:00Z", "t1.txt")
	commit("2031-03-04T00:00:00Z", "2031-03-04T00:00:00Z", "t2.txt")
	commit("2031-03-06T00:00:00Z", "2031-03-06T00:00:00Z", "t3.txt")
	return f
}

func TestCollectBranchHistory(t *testing.T) {
	f := newDatedFixture(t)
	ctx := context.Background()

	got, err := Collect(ctx, f.dir, "main", "topic")
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	// c1's author date (03-03 12:00Z) to c2's committer date (03-10 11:59:59Z)
	// is 6 days 23:59:59: 6 whole days. The head's author date would give 1,
	// rounding would give 7.
	if got.BranchAgeDays != 6 {
		t.Errorf("BranchAgeDays = %d, want 6", got.BranchAgeDays)
	}
	if got.TrunkCommits != 3 {
		t.Errorf("TrunkCommits = %d, want 3", got.TrunkCommits)
	}

	// Collected again, and with log config that would change git log's
	// output if it were not pinned, the facts are the same.
	again, err := Collect(ctx, f.dir, "main", "topic")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, again) {
		t.Errorf("second collection differs:\n%+v\n%+v", got, again)
	}
	runGit(t, f.dir, "config", "log.showSignature", "true")
	runGit(t, f.dir, "config", "log.date", "relative")
	runGit(t, f.dir, "config", "format.pretty", "oneline")
	configured, err := Collect(ctx, f.dir, "main", "topic")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, configured) {
		t.Errorf("collection after log config differs:\n%+v\n%+v", got, configured)
	}
}

func TestCollectBranchWithNoCommitsOfItsOwn(t *testing.T) {
	f := newDatedFixture(t)
	got, err := Collect(context.Background(), f.dir, "main", f.branchPoint)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if got.BranchAgeDays != 0 || got.TrunkCommits != 3 {
		t.Errorf("age, drift = %d, %d, want 0, 3", got.BranchAgeDays, got.TrunkCommits)
	}
}

func TestAgeDays(t *testing.T) {
	const day = 86400
	tests := []struct {
		name        string
		first, head int64
		want        int
	}{
		{"same second", 1000, 1000, 0},
		{"one second short of a day", 0, day - 1, 0},
		{"exactly one day", 0, day, 1},
		{"floored, not rounded", 0, 6*day + day - 1, 6},
		{"head before first is clamped", 2 * day, day, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ageDays(tt.first, tt.head); got != tt.want {
				t.Errorf("ageDays(%d, %d) = %d, want %d", tt.first, tt.head, got, tt.want)
			}
		})
	}
}

func TestTimestampsRejectsMalformedLines(t *testing.T) {
	for _, in := range []string{"abc\n", "-5\n", "12 34\n"} {
		if _, err := timestamps([]byte(in)); err == nil {
			t.Errorf("timestamps(%q) = nil error, want one", in)
		}
	}
	got, err := timestamps([]byte("30\n10\n\n"))
	if err != nil || !reflect.DeepEqual(got, []int64{30, 10}) {
		t.Errorf("timestamps = %v, %v, want [30 10]", got, err)
	}
}
