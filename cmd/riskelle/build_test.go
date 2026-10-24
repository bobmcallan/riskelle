package main

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestBuiltBinaryReportsLdflagsVersion builds the real binary with the
// release-time ldflags hook and runs it, proving the binary is named riskelle
// and that the variable release builds set is the one it prints.
func TestBuiltBinaryReportsLdflagsVersion(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH")
	}
	name := "riskelle"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	bin := filepath.Join(t.TempDir(), name)

	const want = "v9.9.9-test"
	build := exec.Command("go", "build", "-o", bin,
		"-ldflags", "-X github.com/bobmcallan/riskelle/internal/version.version="+want, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	out, err := exec.Command(bin, "version").Output()
	if err != nil {
		t.Fatalf("%s version: %v", name, err)
	}
	if !strings.HasPrefix(string(out), "riskelle "+want) {
		t.Errorf("%s version = %q, want prefix %q", name, out, "riskelle "+want)
	}
}
