package facts

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// fixture is a git repository in a temp dir: a base commit on main, one
// commit on branch feature, and a later trunk-only commit on main.
type fixture struct {
	dir                      string
	baseCommit, feature, tip string
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	return runGitAt(t, dir, "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z", args...)
}

// runGitAt runs git in dir with the fixture identity and global config shut
// out, recording the given author and committer dates on any commit it makes.
func runGitAt(t *testing.T, dir, authorDate, committerDate string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.com",
		"GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.com",
		"GIT_AUTHOR_DATE="+authorDate, "GIT_COMMITTER_DATE="+committerDate,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	runGit(t, dir, "init", "--quiet", "--initial-branch=main")

	write(t, dir, "app/go.mod", "module app\n")
	write(t, dir, "app/main.go", "package main\n\nfunc main() {}\n")
	write(t, dir, "web/package.json", "{}\n")
	write(t, dir, "web/src/index.js", "export {}\n")
	write(t, dir, "svc/Svc.csproj", "<Project />\n")
	write(t, dir, "svc/Program.cs", "class P {\n  static void Main() {}\n}\n")
	write(t, dir, "docs/old.md", "one\ntwo\nthree\nfour\nfive\n")
	write(t, dir, "logo.png", "\x89PNG\x00\x01\x02")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "--quiet", "-m", "base")
	f := fixture{dir: dir, baseCommit: runGit(t, dir, "rev-parse", "HEAD")}

	runGit(t, dir, "switch", "--quiet", "-c", "feature")
	write(t, dir, "app/main.go", "package main\n\nfunc main() { run() }\nfunc run() {}\n")
	write(t, dir, "web/src/new.js", "a\nb\nc\nd\n")
	write(t, dir, "web/src/new.test.js", "t\n")
	write(t, dir, "bin/data.bin", "\x00\x01\x02\x03")
	write(t, dir, "cli/run.go", "package cli\n\nfunc Run() {}\n")
	if err := os.MkdirAll(filepath.Join(dir, "docs", "guide"), 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "mv", "docs/old.md", "docs/guide/new.md")
	write(t, dir, "docs/guide/new.md", "one\ntwo\nthree\nfour\nfive\nsix\n")
	write(t, dir, "logo.png", "\x89PNG\x00\x03\x04")
	runGit(t, dir, "rm", "--quiet", "svc/Program.cs")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "--quiet", "-m", "feature")
	f.feature = runGit(t, dir, "rev-parse", "HEAD")

	runGit(t, dir, "switch", "--quiet", "main")
	write(t, dir, "trunk.txt", "trunk only\n")
	runGit(t, dir, "add", "trunk.txt")
	runGit(t, dir, "commit", "--quiet", "-m", "trunk")
	f.tip = runGit(t, dir, "rev-parse", "HEAD")
	return f
}

func TestCollect(t *testing.T) {
	f := newFixture(t)
	got, err := Collect(context.Background(), f.dir, "main", "feature")
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	want := Facts{
		Base:      f.tip,
		MergeBase: f.baseCommit,
		Head:      f.feature,
		Files: []File{
			{Path: "app/main.go", Added: 2, Removed: 1},
			{Path: "bin/data.bin", Binary: true, New: true},
			{Path: "cli/run.go", Added: 3, New: true},
			{Path: "docs/guide/new.md", OldPath: "docs/old.md", Added: 1, Classes: []Class{ClassDocs}},
			{Path: "logo.png", Binary: true},
			{Path: "svc/Program.cs", Removed: 3},
			{Path: "web/src/new.js", Added: 4, New: true},
			{Path: "web/src/new.test.js", Added: 1, New: true, Classes: []Class{ClassTest}},
		},
		Directories: []string{".", "app", "bin", "cli", "docs", "docs/guide", "svc", "web/src"},
		Projects:    []string{"app", "svc", "web"},
		// Existing: app/main.go +2 -1, the renamed docs/guide/new.md +1 and
		// the deleted svc/Program.cs -3. New: cli/run.go +3, web/src/new.js +4,
		// web/src/new.test.js +1.
		Degree: Degree{Existing: Lines{Added: 3, Removed: 4}, New: Lines{Added: 8}},

		BranchAgeDays: 0, // every fixture commit is dated 2026-01-01
		TrunkCommits:  1, // the "trunk" commit on main after the branch point
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Collect =\n%+v\nwant\n%+v", got, want)
	}
}

func TestCollectIsDeterministic(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	first, err := Collect(ctx, f.dir, "main", "feature")
	if err != nil {
		t.Fatal(err)
	}
	second, err := Collect(ctx, f.dir, "main", "feature")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("second collection differs:\n%+v\n%+v", first, second)
	}

	// Local config and working-tree state must not change the facts.
	runGit(t, f.dir, "config", "diff.renames", "false")
	runGit(t, f.dir, "config", "diff.algorithm", "patience")
	runGit(t, f.dir, "config", "diff.relative", "true")
	write(t, f.dir, ".gitattributes", "* binary\n")
	third, err := Collect(ctx, f.dir, "main", "feature")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, third) {
		t.Errorf("collection after config and worktree changes differs:\n%+v\n%+v", first, third)
	}
	if first.Degree == (Degree{}) || first.Degree != third.Degree {
		t.Errorf("degree = %+v then %+v, want the same non-zero split", first.Degree, third.Degree)
	}
}

func TestCollectUnknownRef(t *testing.T) {
	f := newFixture(t)
	_, err := Collect(context.Background(), f.dir, "no-such-branch", "feature")
	if err == nil || !strings.Contains(err.Error(), "no-such-branch") {
		t.Errorf("Collect(unknown base) error = %v, want one naming the ref", err)
	}
}

func TestCollectSameRef(t *testing.T) {
	f := newFixture(t)
	got, err := Collect(context.Background(), f.dir, "feature", "feature")
	if err != nil {
		t.Fatal(err)
	}
	if got.Files == nil || got.Directories == nil || got.Projects == nil ||
		len(got.Files)+len(got.Directories)+len(got.Projects) != 0 {
		t.Errorf("Collect(feature, feature) = %+v, want empty non-nil slices", got)
	}
	if got.BranchAgeDays != 0 || got.TrunkCommits != 0 {
		t.Errorf("Collect(feature, feature) age, drift = %d, %d, want 0, 0", got.BranchAgeDays, got.TrunkCommits)
	}
}
