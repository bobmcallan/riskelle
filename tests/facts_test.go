//go:build integration

package tests

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bobmcallan/riskelle/internal/facts"
)

// fixtureGit runs git in dir with the developer's global and system config
// shut out and fixed identities and dates, so the fixture is the same on
// every machine.
func fixtureGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	return fixtureGitAt(t, dir, "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z", args...)
}

// fixtureGitAt is fixtureGit with the given author and committer dates.
func fixtureGitAt(t *testing.T, dir, authorDate, committerDate string, args ...string) string {
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

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestCollectFactsFromAFixtureRepository drives collection the way the score
// command will: from outside the package, against a repository whose trunk
// moved after the branch was cut, with the path given as a subdirectory. It
// pins the facts' JSON shape, which later stories serialise into reports.
func TestCollectFactsFromAFixtureRepository(t *testing.T) {
	dir := t.TempDir()
	fixtureGit(t, dir, "init", "--quiet", "--initial-branch=main")
	writeFile(t, dir, "go.mod", "module example.com/m\n")
	writeFile(t, dir, "pkg/a.go", "package pkg\n\nconst A = 1\n")
	writeFile(t, dir, "ui/package.json", "{}\n")
	writeFile(t, dir, "ui/app.js", "x\n")
	writeFile(t, dir, "notes/todo.md", "one\ntwo\nthree\nfour\n")
	fixtureGit(t, dir, "add", ".")
	fixtureGit(t, dir, "commit", "--quiet", "-m", "base")

	fixtureGit(t, dir, "switch", "--quiet", "-c", "topic")
	writeFile(t, dir, "pkg/a.go", "package pkg\n\nconst A = 2\n")
	writeFile(t, dir, "ui/app.js", "x\ny\n")
	if err := os.MkdirAll(filepath.Join(dir, "notes", "done"), 0o755); err != nil {
		t.Fatal(err)
	}
	fixtureGit(t, dir, "mv", "notes/todo.md", "notes/done/todo.md")
	writeFile(t, dir, "ui/icon.ico", "\x00\x00\x01\x00")
	fixtureGit(t, dir, "add", ".")
	fixtureGit(t, dir, "commit", "--quiet", "-m", "topic")

	fixtureGit(t, dir, "switch", "--quiet", "main")
	writeFile(t, dir, "pkg/b.go", "package pkg\n")
	fixtureGit(t, dir, "add", ".")
	fixtureGit(t, dir, "commit", "--quiet", "-m", "trunk moved")

	ctx := context.Background()
	got, err := facts.Collect(ctx, filepath.Join(dir, "pkg"), "main", "topic")
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	again, err := facts.Collect(ctx, dir, "main", "topic")
	if err != nil {
		t.Fatalf("Collect from the root: %v", err)
	}

	gotJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	againJSON, _ := json.Marshal(again)
	if string(gotJSON) != string(againJSON) {
		t.Errorf("facts depend on the directory collected from:\n%s\n%s", gotJSON, againJSON)
	}

	want := `{"base":"` + fixtureGit(t, dir, "rev-parse", "main") + `",` +
		`"merge_base":"` + fixtureGit(t, dir, "rev-parse", "main~1") + `",` +
		`"head":"` + fixtureGit(t, dir, "rev-parse", "topic") + `",` +
		`"files":[` +
		`{"path":"notes/done/todo.md","old_path":"notes/todo.md","added":0,"removed":0},` +
		`{"path":"pkg/a.go","added":1,"removed":1},` +
		`{"path":"ui/app.js","added":1,"removed":0},` +
		`{"path":"ui/icon.ico","added":0,"removed":0,"binary":true}],` +
		`"directories":["notes","notes/done","pkg","ui"],` +
		`"projects":[".","ui"],` +
		`"branch_age_days":0,"trunk_commits":1}`
	if string(gotJSON) != want {
		t.Errorf("facts JSON =\n%s\nwant\n%s", gotJSON, want)
	}
}

// TestCollectFactsOfAnEmptyChange checks that an unchanged branch gives empty
// lists, never null, so a report always has the same shape.
func TestCollectFactsOfAnEmptyChange(t *testing.T) {
	dir := t.TempDir()
	fixtureGit(t, dir, "init", "--quiet", "--initial-branch=main")
	writeFile(t, dir, "go.mod", "module example.com/m\n")
	fixtureGit(t, dir, "add", ".")
	fixtureGit(t, dir, "commit", "--quiet", "-m", "base")
	fixtureGit(t, dir, "branch", "topic")

	got, err := facts.Collect(context.Background(), dir, "main", "topic")
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	b, _ := json.Marshal(got)
	if s := string(b); !strings.Contains(s, `"files":[],"directories":[],"projects":[],"branch_age_days":0,"trunk_commits":0`) {
		t.Errorf("facts JSON = %s, want empty lists", s)
	}
}
