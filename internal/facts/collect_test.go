package facts

import (
	"context"
	"encoding/json"
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

// quietGitConfig turns off git's automatic background work (auto
// maintenance and gc, and their detaching) in every fixture repository:
// runGitAt passes it on each git it runs. Otherwise git commit leaves a
// detached "git maintenance run --auto" writing under .git while t.TempDir
// removes the fixture.
var quietGitConfig = [][2]string{
	{"maintenance.auto", "false"},
	{"gc.auto", "0"},
	{"maintenance.autoDetach", "false"},
	{"gc.autoDetach", "false"},
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	return runGitAt(t, dir, "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z", args...)
}

// runGitAt runs git in dir with the fixture identity and global config shut
// out, recording the given author and committer dates on any commit it makes.
func runGitAt(t *testing.T, dir, authorDate, committerDate string, args ...string) string {
	t.Helper()
	argv := []string{"-C", dir, "-c", "commit.gpgsign=false"}
	for _, kv := range quietGitConfig {
		argv = append(argv, "-c", kv[0]+"="+kv[1])
	}
	cmd := exec.Command("git", append(argv, args...)...)
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
			{Path: "svc/Program.cs", Removed: 3, Deleted: true},
			{Path: "web/src/new.js", Added: 4, New: true},
			{Path: "web/src/new.test.js", Added: 1, New: true, Classes: []Class{ClassTest}},
		},
		// A rename counts in its new directory only: docs (where docs/old.md
		// lived) is not touched.
		Directories: []string{".", "app", "bin", "cli", "docs/guide", "svc", "web/src"},
		Projects:    []string{"app", "svc", "web"},
		// Existing: app/main.go +2 -1, the renamed docs/guide/new.md +1 and
		// the deleted svc/Program.cs -3. New: cli/run.go +3, web/src/new.js +4,
		// web/src/new.test.js +1.
		Degree: Degree{Existing: Lines{Added: 3, Removed: 4}, New: Lines{Added: 8}},
		// cli/run.go is under no go.mod, so it is a file, not a package.
		Impact: Impact{
			Packages: []Dependency{{Name: "app", Dependents: 0}},
			Files: []Dependency{
				{Name: "bin/data.bin"},
				{Name: "cli/run.go"},
				{Name: "docs/guide/new.md"},
				{Name: "docs/old.md"},
				{Name: "logo.png"},
				{Name: "svc/Program.cs"},
				{Name: "web/src/new.js"},
				{Name: "web/src/new.test.js"},
			},
		},

		TestLanguages: []string{},
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

// TestCollectMoveCountsNewDirectoryOnly moves three files from one project's
// directory to another's: spread counts only the new directory and project,
// while impact still checks every old path.
func TestCollectMoveCountsNewDirectoryOnly(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	runGit(t, dir, "init", "--quiet", "--initial-branch=main")
	write(t, dir, "alpha/package.json", "{}\n")
	write(t, dir, "alpha/stack/main.tf", "resource \"a\" \"main\" {\n  name = \"main\"\n  size = 1\n}\n")
	write(t, dir, "alpha/stack/vars.tf", "variable \"region\" {\n  type    = string\n  default = \"west\"\n}\n")
	write(t, dir, "alpha/stack/outputs.tf", "output \"id\" {\n  value       = a.main.id\n  description = \"id\"\n}\n")
	write(t, dir, "beta/package.json", "{\"name\": \"beta\"}\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "--quiet", "-m", "base")

	runGit(t, dir, "switch", "--quiet", "-c", "feature")
	if err := os.MkdirAll(filepath.Join(dir, "beta", "stack"), 0o755); err != nil {
		t.Fatal(err)
	}
	moved := []string{"main.tf", "outputs.tf", "vars.tf"}
	for _, name := range moved {
		runGit(t, dir, "mv", "alpha/stack/"+name, "beta/stack/"+name)
	}
	runGit(t, dir, "commit", "--quiet", "-m", "move")

	got, err := Collect(context.Background(), dir, "main", "feature")
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	if len(got.Files) != len(moved) {
		t.Fatalf("Files = %+v, want %d renames", got.Files, len(moved))
	}
	for i, name := range moved {
		f := got.Files[i]
		if f.Path != "beta/stack/"+name || f.OldPath != "alpha/stack/"+name {
			t.Errorf("Files[%d] = %+v, want a rename alpha/stack/%s -> beta/stack/%s", i, f, name, name)
		}
	}
	if want := []string{"beta/stack"}; !reflect.DeepEqual(got.Directories, want) {
		t.Errorf("Directories = %v, want %v", got.Directories, want)
	}
	if want := []string{"beta"}; !reflect.DeepEqual(got.Projects, want) {
		t.Errorf("Projects = %v, want %v", got.Projects, want)
	}
	inImpact := map[string]bool{}
	for _, d := range got.Impact.Files {
		inImpact[d.Name] = true
	}
	for _, name := range moved {
		for _, p := range []string{"alpha/stack/" + name, "beta/stack/" + name} {
			if !inImpact[p] {
				t.Errorf("Impact.Files = %+v, missing %s", got.Impact.Files, p)
			}
		}
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
		got.Impact.Packages == nil || got.Impact.Files == nil ||
		len(got.Files)+len(got.Directories)+len(got.Projects)+
			len(got.Impact.Packages)+len(got.Impact.Files) != 0 {
		t.Errorf("Collect(feature, feature) = %+v, want empty non-nil slices", got)
	}
	if got.BranchAgeDays != 0 || got.TrunkCommits != 0 {
		t.Errorf("Collect(feature, feature) age, drift = %d, %d, want 0, 0", got.BranchAgeDays, got.TrunkCommits)
	}
}

// TestRunGitStartsNoBackgroundGit checks a commit made through runGit, in a
// fixture repository, starts no child process at all: in particular not the
// detached "git maintenance run --auto" that can outlive the test.
func TestRunGitStartsNoBackgroundGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	runGit(t, dir, "init", "--quiet", "--initial-branch=main")
	write(t, dir, "a.txt", "a\n")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "--quiet", "-m", "base")

	trace := filepath.Join(t.TempDir(), "trace.json")
	t.Setenv("GIT_TRACE2_EVENT", trace)
	runGit(t, dir, "commit", "--quiet", "--allow-empty", "-m", "traced")

	events, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 {
		t.Fatal("git wrote an empty trace; nothing was proved")
	}
	for _, line := range strings.Split(string(events), "\n") {
		if strings.Contains(line, `"event":"child_start"`) {
			t.Errorf("git commit in the fixture started a child process: %s", line)
		}
	}
}

// TestCollectTestLanguages pins that the languages with tests are read from
// the head tree, not the base: the branch deletes the repo's only .bats file.
func TestCollectTestLanguages(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	runGit(t, dir, "init", "--quiet", "--initial-branch=main")
	write(t, dir, "t/smoke.bats", "@test x { true; }\n")
	write(t, dir, "infra/main.tftest.hcl", "run \"x\" {}\n")
	write(t, dir, "run.sh", "echo one\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "--quiet", "-m", "base")
	runGit(t, dir, "switch", "--quiet", "-c", "feature")
	runGit(t, dir, "rm", "--quiet", "t/smoke.bats")
	write(t, dir, "run.sh", "echo two\n")
	runGit(t, dir, "commit", "--quiet", "-am", "feature")

	got, err := Collect(context.Background(), dir, "main", "feature")
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if want := []string{"Terraform"}; !reflect.DeepEqual(got.TestLanguages, want) {
		t.Errorf("TestLanguages = %v, want %v", got.TestLanguages, want)
	}
}

// TestCollectConsequences collects identity_rights from a real diff: a
// catalogued block altered, one added as a data block and one removed, and
// nothing from an uncatalogued block or a mention in another file.
func TestCollectConsequences(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	const storage = "resource \"azurerm_storage_account\" \"s\" {\n  name = \"one\"\n}\n"
	const role = "resource \"azurerm_role_assignment\" \"r\" {\n  role = \"Reader\"\n}\n"
	const iam = "resource \"aws_iam_role\" \"x\" {\n  name = \"x\"\n  assume_role_policy = <<EOF\n{\n  \"Version\": \"2012-10-17\"\n}\nEOF\n}\n"
	fixture := func(t *testing.T) string {
		dir := t.TempDir()
		runGit(t, dir, "init", "--quiet", "--initial-branch=main")
		write(t, dir, "iam/main.tf", storage+"\n"+role)
		write(t, dir, "iam/old.tf", "locals {\n  a = 1\n}\n\n"+iam)
		write(t, dir, "notes.md", "uses azurerm_role_assignment\n")
		runGit(t, dir, "add", ".")
		runGit(t, dir, "commit", "--quiet", "-m", "base")
		runGit(t, dir, "switch", "--quiet", "-c", "feature")
		return dir
	}

	t.Run("add, alter and remove", func(t *testing.T) {
		dir := fixture(t)
		write(t, dir, "iam/main.tf", storage+"\n"+strings.Replace(role, "Reader", "Owner", 1))
		write(t, dir, "iam/sp.tf", "data \"azuread_service_principal\" \"x\" {\n  client_id = \"c\"\n}\n")
		write(t, dir, "iam/old.tf", "locals {\n  a = 1\n}\n")
		runGit(t, dir, "add", ".")
		runGit(t, dir, "commit", "--quiet", "-m", "feature")
		got, err := Collect(context.Background(), dir, "main", "feature")
		if err != nil {
			t.Fatalf("Collect: %v", err)
		}
		want := []Consequence{{
			Class: ConsequenceIdentityRights,
			Paths: []string{"iam/main.tf", "iam/old.tf", "iam/sp.tf"},
			Types: []string{"aws_iam_role", "azuread_service_principal", "azurerm_role_assignment"},
		}}
		if !reflect.DeepEqual(got.Consequences, want) {
			t.Errorf("Consequences = %+v, want %+v", got.Consequences, want)
		}
	})

	t.Run("a policy line inside a heredoc", func(t *testing.T) {
		dir := fixture(t)
		write(t, dir, "iam/old.tf", "locals {\n  a = 1\n}\n\n"+strings.Replace(iam, "2012-10-17", "2024-01-01", 1))
		runGit(t, dir, "commit", "--quiet", "-am", "feature")
		got, err := Collect(context.Background(), dir, "main", "feature")
		if err != nil {
			t.Fatalf("Collect: %v", err)
		}
		if len(got.Consequences) != 1 || !reflect.DeepEqual(got.Consequences[0].Types, []string{"aws_iam_role"}) {
			t.Errorf("Consequences = %+v, want aws_iam_role", got.Consequences)
		}
	})

	t.Run("no catalogued type", func(t *testing.T) {
		dir := fixture(t)
		write(t, dir, "iam/main.tf", strings.Replace(storage, "one", "two", 1)+"\n"+role)
		write(t, dir, "notes.md", "now uses azurerm_role_assignment twice\n")
		runGit(t, dir, "commit", "--quiet", "-am", "feature")
		got, err := Collect(context.Background(), dir, "main", "feature")
		if err != nil {
			t.Fatalf("Collect: %v", err)
		}
		if got.Consequences != nil {
			t.Errorf("Consequences = %+v, want none", got.Consequences)
		}
		if b, _ := json.Marshal(got); strings.Contains(string(b), `"consequences"`) {
			t.Errorf("facts JSON has a consequences key: %s", b)
		}
	})
}

// TestCollectShellShebang collects each changed file's Language: Shell for an
// extensionless file whose first line at head is a shell shebang, none for a
// non-shell shebang, no shebang, an extension or a deleted file.
func TestCollectShellShebang(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	runGit(t, dir, "init", "--quiet", "--initial-branch=main")
	write(t, dir, "README.md", "# x\n")
	write(t, dir, "scripts/old", "#!/bin/sh\nrm -rf build\nmake clean\nmake all\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "--quiet", "-m", "base")
	runGit(t, dir, "switch", "--quiet", "-c", "feature")
	write(t, dir, "scripts/deploy", "#!/usr/bin/env bash\necho\n")
	write(t, dir, "scripts/sync", "#!/usr/bin/env python3\nprint()\n")
	write(t, dir, "scripts/notes", "plain\n")
	write(t, dir, "scripts/run.sh", "#!/bin/sh\necho\n")
	runGit(t, dir, "rm", "--quiet", "scripts/old")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "--quiet", "-m", "feature")

	got, err := Collect(context.Background(), dir, "main", "feature")
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	want := map[string]string{"scripts/deploy": "Shell", "scripts/sync": "", "scripts/notes": "", "scripts/run.sh": "", "scripts/old": ""}
	lang := map[string]string{"scripts/deploy": "Shell", "scripts/sync": "", "scripts/notes": "", "scripts/run.sh": "Shell", "scripts/old": ""}
	if len(got.Files) != len(want) {
		t.Fatalf("files = %+v, want %d", got.Files, len(want))
	}
	for _, f := range got.Files {
		if f.Language != want[f.Path] {
			t.Errorf("%s: Language %q, want %q", f.Path, f.Language, want[f.Path])
		}
		if l := LanguageOf(f); l != lang[f.Path] {
			t.Errorf("%s: LanguageOf %q, want %q", f.Path, l, lang[f.Path])
		}
	}
}
