//go:build integration

// Package tests holds riskelle's integration tests: each one builds the real
// binary and drives it as a user would. Run with:
//
//	go test -tags integration ./tests/...
package tests

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// git runs a git command in dir and returns its trimmed output.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

// cleanBuild clones the repository at its HEAD commit into a fresh directory,
// builds riskelle there with the standard toolchain, and returns the binary
// and the commit it was built from.
func cleanBuild(t *testing.T) (bin, commit string) {
	t.Helper()
	root := git(t, ".", "rev-parse", "--show-toplevel")
	commit = git(t, root, "rev-parse", "HEAD")

	src := filepath.Join(t.TempDir(), "riskelle")
	if out, err := exec.Command("git", "clone", "--quiet", root, src).CombinedOutput(); err != nil {
		t.Fatalf("git clone: %v\n%s", err, out)
	}
	git(t, src, "checkout", "--quiet", commit)

	build := exec.Command("go", "build", "./cmd/riskelle")
	build.Dir = src
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build ./cmd/riskelle in a clean clone: %v\n%s", err, out)
	}
	name := "riskelle"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(src, name), commit
}

func TestCleanCloneBuildsRiskelleAndReportsItsCommit(t *testing.T) {
	bin, commit := cleanBuild(t)

	out, err := exec.Command(bin, "version").Output()
	if err != nil {
		t.Fatalf("riskelle version: %v", err)
	}
	got := string(out)
	if !strings.HasPrefix(got, "riskelle ") {
		t.Errorf("riskelle version = %q, want it to start with \"riskelle \"", got)
	}
	if want := "commit " + commit[:12]; !strings.Contains(got, want) {
		t.Errorf("riskelle version = %q, want it to name the build's %s", got, want)
	}
}

func TestUsageErrors(t *testing.T) {
	bin, _ := cleanBuild(t)

	tests := []struct {
		args     []string
		wantCode int
		wantErr  string
	}{
		{nil, 2, "usage"},
		{[]string{"bogus"}, 2, `unknown command "bogus"`},
		{[]string{"version", "extra"}, 2, "takes no arguments"},
	}
	for _, tt := range tests {
		cmd := exec.Command(bin, tt.args...)
		var stderr strings.Builder
		cmd.Stderr = &stderr
		err := cmd.Run()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatalf("riskelle %v: %v", tt.args, err)
		}
		if code != tt.wantCode {
			t.Errorf("riskelle %v: exit code %d, want %d", tt.args, code, tt.wantCode)
		}
		if !strings.Contains(stderr.String(), tt.wantErr) {
			t.Errorf("riskelle %v: stderr %q, want it to contain %q", tt.args, stderr.String(), tt.wantErr)
		}
	}
}
