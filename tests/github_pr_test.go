//go:build integration

package tests

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// TestMain lets the test binary stand in for gh: with RISKELLE_FAKE_GH set to
// a state file, it serves `gh api` from that file instead of running tests.
func TestMain(m *testing.M) {
	if state := os.Getenv("RISKELLE_FAKE_GH"); state != "" {
		os.Exit(fakeGH(state, os.Args[1:]))
	}
	os.Exit(m.Run())
}

// ghState is the fake GitHub: one PR's comments and the statuses posted.
type ghState struct {
	NextID   int                 `json:"next_id"`
	Comments []ghComment         `json:"comments"`
	Statuses []map[string]string `json:"statuses"`
}

type ghComment struct {
	ID   int    `json:"id"`
	Body string `json:"body"`
	URL  string `json:"html_url"`
}

// fakeGH serves the `gh api` calls scripts/github-pr.sh makes, against state.
func fakeGH(statePath string, args []string) int {
	method, input, path := "GET", "", ""
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--method":
			i++
			method = args[i]
		case "--input":
			i++
			input = args[i]
		case "--paginate":
		default:
			path = args[i]
		}
	}
	var st ghState
	raw, _ := os.ReadFile(statePath)
	json.Unmarshal(raw, &st)
	var in map[string]string
	if input != "" {
		raw, _ := os.ReadFile(input)
		json.Unmarshal(raw, &in)
	}
	parts := strings.Split(path, "/") // repos o r ...
	var out any
	switch {
	case len(parts) < 4 || args[0] != "api":
		fmt.Fprintf(os.Stderr, "fake gh: unexpected %q\n", args)
		return 1
	case parts[3] == "statuses" && method == "POST":
		in["sha"] = parts[4]
		st.Statuses = append(st.Statuses, in)
	case parts[3] == "issues" && parts[5] == "comments" && method == "GET":
		out = st.Comments
	case parts[3] == "issues" && parts[5] == "comments" && method == "POST":
		st.NextID++
		c := ghComment{st.NextID, in["body"], fmt.Sprintf("https://github.example/o/r/pull/%s#issuecomment-%d", parts[4], st.NextID)}
		st.Comments = append(st.Comments, c)
		out = c
	case parts[3] == "issues" && parts[4] == "comments":
		id, _ := strconv.Atoi(parts[5])
		for i, c := range st.Comments {
			if c.ID != id {
				continue
			}
			if method == "DELETE" {
				st.Comments = append(st.Comments[:i], st.Comments[i+1:]...)
			} else {
				st.Comments[i].Body = in["body"]
				out = st.Comments[i]
			}
			break
		}
	default:
		fmt.Fprintf(os.Stderr, "fake gh: unexpected %s %s\n", method, path)
		return 1
	}
	raw, _ = json.Marshal(st)
	os.WriteFile(statePath, raw, 0o644)
	if out != nil {
		json.NewEncoder(os.Stdout).Encode(out)
	}
	return 0
}

const prMarker = "<!-- riskelle:summary -->\n"

// TestGitHubPullRequest drives scripts/github-pr.sh, as the action runs it,
// with the built binary against a fixture repository and a fake gh. The PR
// gets a riskelle status carrying the band and total of a direct local
// score of the same refs, and exactly one summary comment, the marker and
// that score's Markdown byte for byte, updated in place on the next push;
// duplicates are healed, other comments are left alone, and a failure posts
// an error status and no comment.
func TestGitHubPullRequest(t *testing.T) {
	for _, tool := range []string{"bash", "jq"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not on PATH", tool)
		}
	}
	script, err := filepath.Abs("../scripts/github-pr.sh")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "riskelle")
	if out, err := exec.Command("go", "build", "-o", bin, "../cmd/riskelle").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	ghDir := t.TempDir()
	if err := os.Symlink(os.Args[0], filepath.Join(ghDir, "gh")); err != nil {
		t.Fatal(err)
	}
	dir := rulesFixture(t,
		map[string]string{"go.mod": "module example.com/m\n", "core/calc.go": numbered("package core // ", 40)},
		map[string]string{"core/calc.go": numbered("package core // ", 60)})
	fixtureGit(t, dir, "update-ref", "refs/remotes/origin/main", "main") // as actions/checkout leaves it
	mainSHA := fixtureGit(t, dir, "rev-parse", "main")

	// run runs the script for the PR's head with the fake gh at state and
	// returns its exit code, GITHUB_OUTPUT and step summary.
	run := func(state ghState, baseRef, head string) (ghState, int, string, string) {
		t.Helper()
		tmp := t.TempDir()
		statePath, output, summary := filepath.Join(tmp, "gh.json"), filepath.Join(tmp, "out"), filepath.Join(tmp, "summary")
		raw, _ := json.Marshal(state)
		writeFile(t, tmp, "gh.json", string(raw))
		cmd := exec.Command("bash", script)
		cmd.Dir = dir
		// riskelle on the PATH, as the action's install step leaves it, and
		// no RISKELLE_BIN: the action no longer sets one.
		var env []string
		for _, kv := range os.Environ() {
			if !strings.HasPrefix(kv, "RISKELLE_BIN=") {
				env = append(env, kv)
			}
		}
		cmd.Env = append(env, "PATH="+ghDir+string(os.PathListSeparator)+filepath.Dir(bin)+string(os.PathListSeparator)+os.Getenv("PATH"),
			"RISKELLE_FAKE_GH="+statePath, "GITHUB_REPOSITORY=o/r", "PR_NUMBER=7",
			"BASE_REF="+baseRef, "HEAD_SHA="+head, "GITHUB_SERVER_URL=https://github.example", "GITHUB_RUN_ID=42",
			"GITHUB_OUTPUT="+output, "GITHUB_STEP_SUMMARY="+summary, "RUNNER_TEMP="+tmp)
		log, _ := cmd.CombinedOutput()
		t.Logf("github-pr.sh:\n%s", log)
		raw, _ = os.ReadFile(statePath)
		var after ghState
		if err := json.Unmarshal(raw, &after); err != nil {
			t.Fatal(err)
		}
		out, _ := os.ReadFile(output)
		sum, _ := os.ReadFile(summary)
		return after, cmd.ProcessState.ExitCode(), string(out), string(sum)
	}
	// local is the score a developer gets running riskelle on the same refs.
	local := func(head string) (md, description string, total int, level string) {
		t.Helper()
		_, js, _ := runScore(t, bin, dir, "--base", mainSHA, "--head", head)
		_, md, _ = runScore(t, bin, dir, "--format", "markdown", "--base", mainSHA, "--head", head)
		var r struct {
			Total int
			Level string
		}
		if err := json.Unmarshal([]byte(js), &r); err != nil {
			t.Fatalf("score JSON %q: %v", js, err)
		}
		return md, fmt.Sprintf("%s · %d / 100", r.Level, r.Total), r.Total, r.Level
	}
	other := ghComment{7, "looks good", "https://github.example/o/r/pull/7#issuecomment-7"}

	// First push: one comment created beside the reviewer's, one status.
	head1 := fixtureGit(t, dir, "rev-parse", "topic")
	md1, desc1, total1, level1 := local(head1)
	st, code, out, sum := run(ghState{NextID: 99, Comments: []ghComment{other}}, "main", head1)
	if code != 0 {
		t.Fatalf("first push: exit %d", code)
	}
	url := "https://github.example/o/r/pull/7#issuecomment-100"
	if want := []ghComment{other, {100, prMarker + md1, url}}; !reflect.DeepEqual(st.Comments, want) {
		t.Errorf("first push comments = %+v, want %+v", st.Comments, want)
	}
	want := []map[string]string{{"sha": head1, "state": "success", "context": "riskelle", "description": desc1, "target_url": url}}
	if !reflect.DeepEqual(st.Statuses, want) {
		t.Errorf("first push statuses = %v, want %v", st.Statuses, want)
	}
	if wantOut := fmt.Sprintf("total=%d\nlevel=%s\n", total1, level1); out != wantOut || sum != md1 {
		t.Errorf("GITHUB_OUTPUT %q, step summary %q; want %q and the local Markdown", out, sum, wantOut)
	}

	// Second push: the same comment, updated in place; a status on the new head.
	fixtureGit(t, dir, "switch", "--quiet", "topic")
	writeFile(t, dir, "core/calc.go", numbered("package core // ", 400))
	fixtureGit(t, dir, "commit", "--quiet", "-am", "second push")
	fixtureGit(t, dir, "switch", "--quiet", "main")
	head2 := fixtureGit(t, dir, "rev-parse", "topic")
	md2, desc2, _, _ := local(head2)
	if md2 == md1 {
		t.Fatalf("the second push scores the same Markdown as the first; the fixture proves nothing")
	}
	st, code, _, _ = run(st, "main", head2)
	if want := []ghComment{other, {100, prMarker + md2, url}}; code != 0 || !reflect.DeepEqual(st.Comments, want) {
		t.Errorf("second push: exit %d, comments = %+v, want %+v", code, st.Comments, want)
	}
	if n := len(st.Statuses); n != 2 || st.Statuses[1]["sha"] != head2 || st.Statuses[1]["description"] != desc2 {
		t.Errorf("second push statuses = %v, want a second one on %s with %q", st.Statuses, head2, desc2)
	}

	// A past race left two summary comments: the oldest is kept, the other deleted.
	dupes := ghState{NextID: 9, Comments: []ghComment{
		{5, prMarker + "old", "u5"}, other, {9, prMarker + "older race", "u9"}}}
	st, code, _, _ = run(dupes, "main", head2)
	if want := []ghComment{{5, prMarker + md2, "u5"}, other}; code != 0 || !reflect.DeepEqual(st.Comments, want) {
		t.Errorf("duplicates: exit %d, comments = %+v, want %+v", code, st.Comments, want)
	}

	// riskelle cannot score: an error status pointing at the run, no comment.
	st, code, _, _ = run(ghState{}, "nope", head2)
	want = []map[string]string{{"sha": head2, "state": "error", "context": "riskelle",
		"description": "riskelle failed; see the run", "target_url": "https://github.example/o/r/actions/runs/42"}}
	if code != 1 || len(st.Comments) != 0 || !reflect.DeepEqual(st.Statuses, want) {
		t.Errorf("failure: exit %d, comments %+v, statuses %v; want exit 1, no comment, %v", code, st.Comments, st.Statuses, want)
	}
}

// TestActionInstallStep runs action.yml's install step as the runner does,
// against a stand-in release: it needs no release token, version defaults to
// latest, which installs the latest release, the pinned version's own
// install.sh puts that version in the directory it adds to GITHUB_PATH, and
// every refusal fails the step with nothing added.
func TestActionInstallStep(t *testing.T) {
	for _, tool := range []string{"bash", "curl", "tar"} {
		if _, err := exec.LookPath(tool); err != nil || runtime.GOOS != "linux" {
			t.Skipf("the action runs on Linux runners with %s", tool)
		}
	}
	raw, _ := os.ReadFile("../action.yml")
	for _, gone := range []string{"release-token", "RISKELLE_BIN", "gh release download"} {
		if strings.Contains(string(raw), gone) {
			t.Errorf("action.yml still names %q", gone)
		}
	}
	input, _, _ := strings.Cut(strings.SplitN(string(raw), "  version:\n", 2)[1], "  token:\n")
	if !strings.Contains(input, "    default: latest\n") || strings.Contains(input, "required: true") {
		t.Errorf("action.yml's version input %q is not optional with the default latest", input)
	}
	script := stepScript(t, "../action.yml", "run: |\n        # riskelle: install\n", "        ")
	if strings.Contains(script, "${{") {
		t.Fatalf("action.yml's install step uses an expression; it must read only its env")
	}
	repo := standInRelease(t)
	install := func(version string) (code int, stderr string, path []string) {
		t.Helper()
		tmp := t.TempDir()
		pathFile := filepath.Join(tmp, "github_path")
		writeFile(t, tmp, "github_path", "")
		cmd := exec.Command("bash", "-c", script)
		for _, kv := range os.Environ() {
			if !strings.HasPrefix(kv, "HOME=") && !strings.HasPrefix(kv, "RISKELLE_") && !strings.HasPrefix(kv, "GH_TOKEN=") && !strings.HasPrefix(kv, "GITHUB_TOKEN=") {
				cmd.Env = append(cmd.Env, kv)
			}
		}
		cmd.Env = append(cmd.Env, "HOME="+tmp, "RISKELLE_VERSION="+version, "RISKELLE_REPO_URL="+repo,
			"RUNNER_OS=Linux", "RUNNER_TEMP="+tmp, "GITHUB_PATH="+pathFile)
		out, _ := cmd.CombinedOutput()
		added, _ := os.ReadFile(pathFile)
		return cmd.ProcessState.ExitCode(), string(out), strings.Fields(string(added))
	}
	code, out, path := install("v0.0.1")
	if code != 0 || len(path) != 1 {
		t.Fatalf("install: exit %d, GITHUB_PATH %q, output %q; want one directory added", code, path, out)
	}
	if got := installedVersion(t, filepath.Join(path[0], "riskelle")); got != "v0.0.1" {
		t.Errorf("riskelle in %s reports %q, want v0.0.1", path[0], got)
	}
	code, out, path = install("latest")
	if code != 0 || len(path) != 1 {
		t.Fatalf("install latest: exit %d, GITHUB_PATH %q, output %q; want one directory added", code, path, out)
	}
	if got := installedVersion(t, filepath.Join(path[0], "riskelle")); got != "v0.0.2" {
		t.Errorf("latest: riskelle in %s reports %q, want the latest release v0.0.2", path[0], got)
	}
	for _, tt := range pipelineRefusals {
		code, out, path := install(tt.version)
		if code == 0 || len(path) != 0 || !strings.Contains(out, tt.stderr) {
			t.Errorf("%s: exit %d, GITHUB_PATH %q, output %q; want a failure naming %q with nothing added", tt.name, code, path, out, tt.stderr)
		}
	}
}

// TestOwnWorkflowUsesLatest keeps riskelle's own pull request workflow on the
// action's default, the latest release: no version pin under its step.
func TestOwnWorkflowUsesLatest(t *testing.T) {
	raw, err := os.ReadFile("../.github/workflows/riskelle.yml")
	if err != nil {
		t.Fatal(err)
	}
	uses := false
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		uses = uses || line == "      - uses: ./"
		if strings.Contains(line, "version:") {
			t.Errorf("riskelle.yml pins a version: %q", line)
		}
	}
	if !uses {
		t.Errorf("riskelle.yml does not run the action with `- uses: ./`")
	}
}
