//go:build integration

package tests

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/bobmcallan/riskelle/internal/facts"
)

// TestCollectClassifiesChangedPaths drives collection against a fixture
// repository whose branch touches Go, JavaScript, .NET and Python files, config
// and data files, of every class, and checks each changed file's classes in the facts JSON: one or more
// classes in a fixed order, or no "classes" key at all.
func TestCollectClassifiesChangedPaths(t *testing.T) {
	dir := t.TempDir()
	fixtureGit(t, dir, "init", "--quiet", "--initial-branch=main")
	writeFile(t, dir, "go.mod", "module example.com/m\n")
	fixtureGit(t, dir, "add", ".")
	fixtureGit(t, dir, "commit", "--quiet", "-m", "base")

	fixtureGit(t, dir, "switch", "--quiet", "-c", "topic")
	changed := map[string][]string{
		// Go
		"main.go":              nil,
		"main_test.go":         {"test"},
		"go.sum":               {"lock"},
		"api/svc.pb.go":        {"generated"},
		"pkg/testdata/gold.md": {"docs", "test"},
		// JavaScript
		"web/package-lock.json":                  {"lock"},
		"web/src/app.js":                         nil,
		"web/src/app.spec.js":                    {"test"},
		"web/src/__snapshots__/app.test.js.snap": {"generated", "test"},
		// .NET
		"svc/Program.cs":            nil,
		"svc/Form1.Designer.cs":     {"generated"},
		"svc/packages.lock.json":    {"lock"},
		"svc.Tests/ProgramTests.cs": {"test"},
		// Python
		"flow/test_steps.py": {"test"},
		"flow/steps.py":      nil,
		// Terraform
		"infra/prod/.terraform.lock.hcl": {"lock"},
		// docs
		"README.md":      {"docs"},
		"docs/setup.txt": {"docs"},
		// config and data
		".gitignore":             {"non_code"},
		"config/app.toml":        {"non_code"},
		"data/rates.csv":         {"non_code"},
		"pkg/testdata/rates.csv": {"non_code", "test"},
		// agent and harness config
		".claude/settings.json":           {"non_code"},
		".satelle/workflows/story.json":   {"non_code"},
		".github/copilot-instructions.md": {"docs", "non_code"},
		// run records
		"logs/run.log":      {"run_record"},
		"infra/main.tfplan": {"run_record"},
	}
	for p := range changed {
		writeFile(t, dir, p, "x\n")
	}
	fixtureGit(t, dir, "add", ".")
	fixtureGit(t, dir, "commit", "--quiet", "-m", "topic")

	got, err := facts.Collect(context.Background(), dir, "main", "topic")
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		Files []map[string]json.RawMessage `json:"files"`
	}
	if err := json.Unmarshal(b, &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Files) != len(changed) {
		t.Fatalf("facts list %d files, want %d: %s", len(report.Files), len(changed), b)
	}
	for _, f := range report.Files {
		var p string
		if err := json.Unmarshal(f["path"], &p); err != nil {
			t.Fatal(err)
		}
		want, ok := changed[p]
		if !ok {
			t.Errorf("unexpected changed path %q", p)
			continue
		}
		raw, has := f["classes"]
		if want == nil {
			if has {
				t.Errorf("%s: classes = %s, want no classes key", p, raw)
			}
			continue
		}
		wantJSON, _ := json.Marshal(want)
		if string(raw) != string(wantJSON) {
			t.Errorf("%s: classes = %s, want %s", p, raw, wantJSON)
		}
	}
}

// TestREADMESizeRowNamesLockFiles checks that the README's size row names the
// lock files the lock class leaves out of size, one per ecosystem, so the
// documented measure matches the classes Collect assigns.
func TestREADMESizeRowNamesLockFiles(t *testing.T) {
	readme, err := os.ReadFile("../README.md")
	if err != nil {
		t.Fatal(err)
	}
	var row string
	for _, line := range strings.Split(string(readme), "\n") {
		if strings.HasPrefix(line, "| Size |") {
			row = line
			break
		}
	}
	if row == "" {
		t.Fatal("README.md has no Size row in its measures table")
	}
	for _, name := range []string{"go.sum", "package-lock.json", "packages.lock.json", "*.lock", ".terraform.lock.hcl"} {
		if !strings.Contains(row, "`"+name+"`") {
			t.Errorf("README size row does not name `%s`: %s", name, row)
		}
	}
}
