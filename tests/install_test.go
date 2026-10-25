//go:build integration

package tests

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const installScript = "../scripts/install.sh"

// standInRelease serves, from a local httptest server, the part of GitHub
// scripts/install.sh reads: /releases/latest redirecting to the latest tag
// (v0.0.2), and /releases/download/<tag>/ holding each tag's archive, laid
// out as scripts/release.sh makes it, its checksums file and install.sh (a
// copy of scripts/install.sh, as release.sh attaches it, which the pipelines
// fetch). v0.0.3's checksums carry a wrong hash and v0.0.4's have no line for
// the archive. It returns the server's URL, the repository URL the script is
// given.
func standInRelease(t *testing.T) string {
	t.Helper()
	stage, releases := t.TempDir(), t.TempDir()
	installer, err := os.ReadFile(installScript)
	if err != nil {
		t.Fatal(err)
	}
	for _, tag := range []string{"v0.0.1", "v0.0.2", "v0.0.3", "v0.0.4"} {
		name := "riskelle_" + tag + "_" + runtime.GOOS + "_" + runtime.GOARCH
		buildRiskelle(t, filepath.Join(stage, name), "-ldflags", "-X github.com/bobmcallan/riskelle/internal/version.version="+tag)
		dir := filepath.Join(releases, tag)
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command("tar", "-czf", filepath.Join(dir, name+".tar.gz"), "-C", stage, name).CombinedOutput(); err != nil {
			t.Fatalf("tar: %v\n%s", err, out)
		}
		tgz, err := os.ReadFile(filepath.Join(dir, name+".tar.gz"))
		if err != nil {
			t.Fatal(err)
		}
		line := fmt.Sprintf("%x  %s.tar.gz\n", sha256.Sum256(tgz), name)
		switch tag {
		case "v0.0.3":
			line = fmt.Sprintf("%064d  %s.tar.gz\n", 0, name)
		case "v0.0.4":
			line = ""
		}
		writeFile(t, dir, "riskelle_"+tag+"_checksums.txt", fmt.Sprintf("%064d  riskelle_%s_windows_amd64.zip\n", 1, tag)+line)
		writeFile(t, dir, "install.sh", string(installer))
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/releases/tag/v0.0.2", http.StatusFound)
	})
	mux.HandleFunc("/releases/tag/", func(w http.ResponseWriter, r *http.Request) {})
	mux.Handle("/releases/download/", http.StripPrefix("/releases/download/", http.FileServer(http.Dir(releases))))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

// runInstall runs scripts/install.sh with sh against the repository at repo,
// with HOME set to home and env added. A developer's own HOME and RISKELLE_*
// settings are removed first, so they cannot leak into the test.
func runInstall(t *testing.T, repo, home string, env ...string) (int, string, string) {
	t.Helper()
	cmd := exec.Command("sh", installScript)
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "HOME=") && !strings.HasPrefix(kv, "RISKELLE_") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	cmd.Env = append(cmd.Env, append([]string{"HOME=" + home, "RISKELLE_REPO_URL=" + repo}, env...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil && cmd.ProcessState == nil {
		t.Fatalf("sh %s: %v", installScript, err)
	}
	return cmd.ProcessState.ExitCode(), stdout.String(), stderr.String()
}

// installedVersion returns the version the riskelle at bin reports, or "" when
// there is none.
func installedVersion(t *testing.T, bin string) string {
	t.Helper()
	if _, err := os.Stat(bin); err != nil {
		return ""
	}
	out, err := exec.Command(bin, "version").Output()
	if err != nil {
		t.Fatalf("%s version: %v", bin, err)
	}
	if f := strings.Fields(string(out)); len(f) > 1 {
		return f[1]
	}
	return string(out)
}

// TestInstallScript installs from a stand-in release: the latest into
// ~/.local/bin, a RISKELLE_VERSION pin, a RISKELLE_INSTALL_DIR override, and
// a refusal, leaving what was installed untouched, on a bad checksum, a
// missing checksum line and a version that is not a tag.
func TestInstallScript(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" || runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		t.Skipf("scripts/install.sh installs on linux and darwin, amd64 and arm64, not %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	for _, tool := range []string{"sh", "curl", "tar"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not on PATH", tool)
		}
	}
	repo := standInRelease(t)

	t.Run("latest into ~/.local/bin", func(t *testing.T) {
		home := t.TempDir()
		code, stdout, stderr := runInstall(t, repo, home)
		if code != 0 {
			t.Fatalf("exit %d, stderr %q", code, stderr)
		}
		if got := installedVersion(t, filepath.Join(home, ".local", "bin", "riskelle")); got != "v0.0.2" {
			t.Errorf("~/.local/bin/riskelle reports %q, want v0.0.2", got)
		}
		if !strings.Contains(stdout, "riskelle v0.0.2") {
			t.Errorf("stdout %q does not print the installed version", stdout)
		}
	})

	t.Run("RISKELLE_VERSION pins the release", func(t *testing.T) {
		home := t.TempDir()
		code, stdout, stderr := runInstall(t, repo, home, "RISKELLE_VERSION=v0.0.1")
		if code != 0 {
			t.Fatalf("exit %d, stderr %q", code, stderr)
		}
		if got := installedVersion(t, filepath.Join(home, ".local", "bin", "riskelle")); got != "v0.0.1" {
			t.Errorf("~/.local/bin/riskelle reports %q, want v0.0.1", got)
		}
		if !strings.Contains(stdout, "riskelle v0.0.1") {
			t.Errorf("stdout %q does not print the installed version", stdout)
		}
	})

	t.Run("RISKELLE_INSTALL_DIR overrides the directory", func(t *testing.T) {
		home, dir := t.TempDir(), filepath.Join(t.TempDir(), "bin")
		code, _, stderr := runInstall(t, repo, home, "RISKELLE_INSTALL_DIR="+dir)
		if code != 0 {
			t.Fatalf("exit %d, stderr %q", code, stderr)
		}
		if got := installedVersion(t, filepath.Join(dir, "riskelle")); got != "v0.0.2" {
			t.Errorf("%s/riskelle reports %q, want v0.0.2", dir, got)
		}
		if _, err := os.Stat(filepath.Join(home, ".local", "bin", "riskelle")); err == nil {
			t.Errorf("riskelle was also installed into ~/.local/bin")
		}
	})

	t.Run("a bad checksum installs nothing", func(t *testing.T) {
		home, dir := t.TempDir(), t.TempDir()
		if code, _, stderr := runInstall(t, repo, home, "RISKELLE_INSTALL_DIR="+dir, "RISKELLE_VERSION=v0.0.1"); code != 0 {
			t.Fatalf("first install: exit %d, stderr %q", code, stderr)
		}
		code, _, stderr := runInstall(t, repo, home, "RISKELLE_INSTALL_DIR="+dir, "RISKELLE_VERSION=v0.0.3")
		if code == 0 || !strings.Contains(stderr, "checksum mismatch") {
			t.Errorf("exit %d, stderr %q; want a refusal naming the checksum mismatch", code, stderr)
		}
		if got := installedVersion(t, filepath.Join(dir, "riskelle")); got != "v0.0.1" {
			t.Errorf("installed riskelle reports %q after the refusal, want v0.0.1 kept", got)
		}
		entries, _ := os.ReadDir(dir)
		if len(entries) != 1 {
			t.Errorf("install dir holds %d entries after the refusal, want only riskelle", len(entries))
		}
	})

	for _, tt := range []struct{ name, version, stderr string }{
		{"no checksum line installs nothing", "v0.0.4", "has no line for"},
		{"a version that is not a tag installs nothing", "0.0.1", "vX.Y.Z"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			home, dir := t.TempDir(), t.TempDir()
			code, _, stderr := runInstall(t, repo, home, "RISKELLE_INSTALL_DIR="+dir, "RISKELLE_VERSION="+tt.version)
			if code == 0 || !strings.Contains(stderr, tt.stderr) {
				t.Errorf("exit %d, stderr %q; want a refusal naming %q", code, stderr, tt.stderr)
			}
			if entries, _ := os.ReadDir(dir); len(entries) != 0 {
				t.Errorf("install dir holds %d entries after the refusal, want none", len(entries))
			}
		})
	}
}
