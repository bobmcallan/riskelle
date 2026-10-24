//go:build integration

package tests

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/bobmcallan/riskelle/internal/facts"
)

// TestCollectDegreeSplitsExistingFromNewCode drives collection against a
// fixture repository whose branch modifies, deletes and creates files, and
// whose trunk later adds a file at the path the branch created. It pins the
// per-file new markers and the degree split in their JSON form, over two runs.
func TestCollectDegreeSplitsExistingFromNewCode(t *testing.T) {
	dir := t.TempDir()
	fixtureGit(t, dir, "init", "--quiet", "--initial-branch=main")
	writeFile(t, dir, "go.mod", "module example.com/m\n")
	writeFile(t, dir, "core/calc.go", "package core\n\nfunc Add(a, b int) int {\n\treturn a + b\n}\n")
	writeFile(t, dir, "core/legacy.go", "package core\n\n// Old is unused.\nfunc Old() {}\n")
	fixtureGit(t, dir, "add", ".")
	fixtureGit(t, dir, "commit", "--quiet", "-m", "base")

	fixtureGit(t, dir, "switch", "--quiet", "-c", "topic")
	// modified: +2 -1
	writeFile(t, dir, "core/calc.go", "package core\n\nfunc Add(a, b int) int {\n\tsum := a + b\n\treturn sum\n}\n")
	// deleted: -4
	fixtureGit(t, dir, "rm", "--quiet", "core/legacy.go")
	// new: +5, in a directory that did not exist at base
	writeFile(t, dir, "feature/flag.go", "package feature\n\nfunc On() bool {\n\treturn true\n}\n")
	fixtureGit(t, dir, "add", ".")
	fixtureGit(t, dir, "commit", "--quiet", "-m", "topic")

	// Trunk adds a file at the same path after the branch point. The diff is
	// taken from the merge base, where the path is absent, so it stays new.
	fixtureGit(t, dir, "switch", "--quiet", "main")
	writeFile(t, dir, "feature/flag.go", "package feature\n")
	fixtureGit(t, dir, "add", ".")
	fixtureGit(t, dir, "commit", "--quiet", "-m", "trunk moved")

	ctx := context.Background()
	var runs []string
	for i := 0; i < 2; i++ {
		got, err := facts.Collect(ctx, dir, "main", "topic")
		if err != nil {
			t.Fatalf("Collect: %v", err)
		}
		files, _ := json.Marshal(got.Files)
		degree, _ := json.Marshal(got.Degree)
		runs = append(runs, string(files)+"\n"+string(degree))
	}
	if runs[0] != runs[1] {
		t.Errorf("degree differs between runs:\n%s\n%s", runs[0], runs[1])
	}

	want := `[{"path":"core/calc.go","added":2,"removed":1},` +
		`{"path":"core/legacy.go","added":0,"removed":4},` +
		`{"path":"feature/flag.go","added":5,"removed":0,"new":true}]` + "\n" +
		`{"existing":{"added":2,"removed":5},"new":{"added":5,"removed":0}}`
	if runs[0] != want {
		t.Errorf("files and degree =\n%s\nwant\n%s", runs[0], want)
	}
}
