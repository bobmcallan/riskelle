//go:build integration

package tests

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// releaseTargets is the target list the story's criteria name; the test
// expects it from the criterion, not from scripts/release.sh.
var releaseTargets = []struct{ os, arch string }{
	{"linux", "amd64"}, {"linux", "arm64"},
	{"darwin", "amd64"}, {"darwin", "arm64"},
	{"windows", "amd64"}, {"windows", "arm64"},
}

// quietGitConfig turns off git's automatic background work (auto maintenance
// and gc, and their detaching) in every repository the tests build:
// snapshotRepo writes it into the repository's config, and fixtureGitAt passes
// it on each git it runs.
var quietGitConfig = [][2]string{
	{"maintenance.auto", "false"},
	{"gc.auto", "0"},
	{"maintenance.autoDetach", "false"},
	{"gc.autoDetach", "false"},
}

// snapshotRepo copies the working tree's tracked and untracked (not ignored)
// files into a fresh git repository with one commit, so the release script
// runs on a clean tree holding exactly what would be committed. It returns
// the repository and its commit.
func snapshotRepo(t *testing.T) (dir, commit string) {
	t.Helper()
	root := git(t, ".", "rev-parse", "--show-toplevel")
	list := git(t, root, "ls-files", "--cached", "--others", "--exclude-standard")

	dir = filepath.Join(t.TempDir(), "riskelle")
	for _, rel := range strings.Split(list, "\n") {
		src := filepath.Join(root, rel)
		info, err := os.Stat(src)
		if errors.Is(err, fs.ErrNotExist) {
			continue // deleted in the working tree
		}
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		dst := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, data, info.Mode().Perm()); err != nil {
			t.Fatal(err)
		}
	}
	git(t, dir, "init", "--quiet")
	// git commit starts auto maintenance detached, and it can still be
	// writing .git/objects when t.TempDir removes the directory. Repo config
	// keeps every later git in the snapshot (this helper, release.sh, go
	// build's VCS stamping) from leaving work running in the background.
	for _, kv := range quietGitConfig {
		git(t, dir, "config", kv[0], kv[1])
	}
	git(t, dir, "add", ".")
	git(t, dir, "-c", "user.name=test", "-c", "user.email=test@example.com",
		"commit", "--quiet", "-m", "snapshot")
	return dir, git(t, dir, "rev-parse", "HEAD")
}

// needReleaseTools skips when the host cannot run scripts/release.sh.
func needReleaseTools(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("scripts/release.sh runs on Linux")
	}
	for _, tool := range []string{"bash", "tar", "gzip", "zip", "sha256sum"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not on PATH", tool)
		}
	}
}

func runRelease(t *testing.T, repo string, args ...string) error {
	t.Helper()
	cmd := exec.Command("bash", append([]string{"scripts/release.sh"}, args...)...)
	cmd.Dir = repo
	out, err := cmd.CombinedOutput()
	t.Logf("scripts/release.sh %v:\n%s", args, out)
	return err
}

func TestReleaseScriptBuildsEveryTarget(t *testing.T) {
	needReleaseTools(t)
	repo, commit := snapshotRepo(t)
	const version = "v0.0.0"

	a := filepath.Join(t.TempDir(), "a")
	if err := runRelease(t, repo, version, a); err != nil {
		t.Fatalf("scripts/release.sh: %v", err)
	}

	// AC1 + AC2: exactly one archive per target plus the checksums file and
	// the installer.
	sums := "riskelle_" + version + "_checksums.txt"
	const installer = "install.sh"
	want := []string{sums, installer}
	for _, tg := range releaseTargets {
		ext := ".tar.gz"
		if tg.os == "windows" {
			ext = ".zip"
		}
		want = append(want, "riskelle_"+version+"_"+tg.os+"_"+tg.arch+ext)
	}
	slices.Sort(want)
	entries, err := os.ReadDir(a)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	if !slices.Equal(got, want) {
		t.Fatalf("release output = %v, want %v", got, want)
	}

	// AC2: the checksums file lists every archive with its sha256.
	sumData, err := os.ReadFile(filepath.Join(a, sums))
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(sumData)), "\n") {
		hash, name, ok := strings.Cut(line, "  ")
		if !ok {
			t.Fatalf("checksums line %q is not in sha256sum format", line)
		}
		listed[name] = hash
	}
	for _, name := range want {
		if name == sums || name == installer {
			continue
		}
		data, err := os.ReadFile(filepath.Join(a, name))
		if err != nil {
			t.Fatal(err)
		}
		h := sha256.Sum256(data)
		if listed[name] != hex.EncodeToString(h[:]) {
			t.Errorf("checksum for %s = %q, want %x", name, listed[name], h)
		}
	}
	if len(listed) != len(releaseTargets) {
		t.Errorf("checksums file lists %d files, want one per archive, %d", len(listed), len(releaseTargets))
	}

	// The install.sh asset is scripts/install.sh, byte for byte.
	gotInstaller, err := os.ReadFile(filepath.Join(a, installer))
	if err != nil {
		t.Fatal(err)
	}
	wantInstaller, err := os.ReadFile(filepath.Join(repo, "scripts", installer))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotInstaller, wantInstaller) {
		t.Errorf("release %s differs from scripts/%s", installer, installer)
	}

	// AC2: each archive holds one directory with the binary and the README.
	for _, tg := range releaseTargets {
		dir := "riskelle_" + version + "_" + tg.os + "_" + tg.arch
		bin := "riskelle"
		var names []string
		if tg.os == "windows" {
			bin = "riskelle.exe"
			names = zipNames(t, filepath.Join(a, dir+".zip"))
		} else {
			names = tarNames(t, filepath.Join(a, dir+".tar.gz"))
		}
		wantNames := []string{dir + "/", dir + "/README.md", dir + "/" + bin}
		slices.Sort(wantNames)
		if !slices.Equal(names, wantNames) {
			t.Errorf("%s holds %v, want %v", dir, names, wantNames)
		}
	}

	// Reproducible: a second build from the same commit is byte-identical.
	b := filepath.Join(t.TempDir(), "b")
	if err := runRelease(t, repo, version, b); err != nil {
		t.Fatalf("second scripts/release.sh: %v", err)
	}
	sumB, err := os.ReadFile(filepath.Join(b, sums))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(sumData, sumB) {
		t.Errorf("rebuild checksums differ:\n%s\nvs\n%s", sumData, sumB)
	}

	// AC3: the host's release binary reports the tag as its version.
	host := "riskelle_" + version + "_" + runtime.GOOS + "_" + runtime.GOARCH
	archive := filepath.Join(a, host+".tar.gz")
	if _, err := os.Stat(archive); err != nil {
		t.Logf("host %s/%s has no tar.gz release archive; skipping version check", runtime.GOOS, runtime.GOARCH)
		return
	}
	x := t.TempDir()
	if out, err := exec.Command("tar", "-xzf", archive, "-C", x).CombinedOutput(); err != nil {
		t.Fatalf("tar -xzf: %v\n%s", err, out)
	}
	out, err := exec.Command(filepath.Join(x, host, "riskelle"), "version").Output()
	if err != nil {
		t.Fatalf("release binary version: %v", err)
	}
	if want := "riskelle " + version + " (commit " + commit[:12] + ")\n"; string(out) != want {
		t.Errorf("release binary version = %q, want %q", out, want)
	}
}

func TestReleaseScriptRejectsBadVersion(t *testing.T) {
	needReleaseTools(t)
	repo, _ := snapshotRepo(t)
	for _, v := range []string{"1.2.3", "v1.2", "v1.2.3-rc1"} {
		out := filepath.Join(t.TempDir(), "out")
		err := runRelease(t, repo, v, out)
		var ee *exec.ExitError
		if !errors.As(err, &ee) || ee.ExitCode() != 2 {
			t.Errorf("scripts/release.sh %s: err %v, want exit code 2", v, err)
		}
	}
}

// TestSnapshotRepoRunsNoBackgroundGit checks the snapshot repository's
// config stops git commit leaving a detached maintenance run behind.
func TestSnapshotRepoRunsNoBackgroundGit(t *testing.T) {
	repo, _ := snapshotRepo(t)
	want := map[string]string{
		"maintenance.auto":       "false",
		"gc.auto":                "0",
		"maintenance.autoDetach": "false",
		"gc.autoDetach":          "false",
	}
	for key, value := range want {
		if got := git(t, repo, "config", "--get", key); got != value {
			t.Errorf("snapshot git config %s = %q, want %q", key, got, value)
		}
	}

	// A further commit, traced, starts no child process at all: in
	// particular not the detached "git maintenance run --auto".
	trace := filepath.Join(t.TempDir(), "trace.json")
	cmd := exec.Command("git", "-C", repo, "-c", "user.name=test", "-c", "user.email=test@example.com",
		"commit", "--quiet", "--allow-empty", "-m", "traced")
	cmd.Env = append(os.Environ(), "GIT_TRACE2_EVENT="+trace)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
	events, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(events), "\n") {
		if strings.Contains(line, `"event":"child_start"`) {
			t.Errorf("git commit in the snapshot started a child process: %s", line)
		}
	}
}

func TestReleaseScriptRefusesDirtyTree(t *testing.T) {
	needReleaseTools(t)
	repo, _ := snapshotRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runRelease(t, repo, "v0.0.0", filepath.Join(t.TempDir(), "out")); err == nil {
		t.Error("scripts/release.sh on a dirty tree succeeded, want it refused")
	}
}

func tarNames(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	tr := tar.NewReader(zr)
	var names []string
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		names = append(names, h.Name)
	}
	slices.Sort(names)
	return names
}

func zipNames(t *testing.T, path string) []string {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	defer zr.Close()
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	slices.Sort(names)
	return names
}
