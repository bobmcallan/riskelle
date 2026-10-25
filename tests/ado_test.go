//go:build integration

package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/bobmcallan/riskelle/internal/report"
)

const adoTemplate = "../ado/riskelle.yml"

// stepScript returns the YAML block scalar in file that follows opener,
// with indent removed from each line: the exact script a pipeline step runs.
// It ends at the first non-blank line that is not indented by indent.
func stepScript(t *testing.T, file, opener, indent string) string {
	t.Helper()
	raw, _ := os.ReadFile(file)
	_, after, ok := strings.Cut(string(raw), opener)
	if !ok {
		t.Fatalf("%s: no step opening %q", file, opener)
	}
	var lines []string
	for _, l := range strings.Split(after, "\n") {
		body, in := strings.CutPrefix(l, indent)
		if !in && strings.TrimSpace(l) != "" {
			break
		}
		lines = append(lines, body)
	}
	return strings.Join(lines, "\n")
}

// adoScript returns the template's `- bash: |` step whose first line is
// marker, unindented: the exact script an agent runs. A pipeline macro in it
// fails the test: the scripts read only the environment.
func adoScript(t *testing.T, marker string) string {
	t.Helper()
	for _, tool := range []string{"bash", "curl", "jq", "tar", "sha256sum"} {
		if _, err := exec.LookPath(tool); err != nil || runtime.GOOS != "linux" {
			t.Skipf("the template runs on Linux agents with %s", tool)
		}
	}
	script := stepScript(t, adoTemplate, "- bash: |\n          "+marker+"\n", "          ")
	if m := regexp.MustCompile(`\$\{\{|\$\([A-Za-z_][A-Za-z0-9_.]*\)`).FindString(script); m != "" {
		t.Fatalf("%s: bash step %q uses the pipeline macro %q", adoTemplate, marker, m)
	}
	return script
}

// runADOScript runs script with bash in dir with env added, and returns its
// exit code, stdout and stderr.
func runADOScript(t *testing.T, script, dir string, env ...string) (int, string, string) {
	t.Helper()
	cmd := exec.Command("bash", "-c", script)
	var stdout, stderr bytes.Buffer
	cmd.Dir, cmd.Stdout, cmd.Stderr = dir, &stdout, &stderr
	cmd.Env = append(os.Environ(), append(env, "AGENT_TEMPDIRECTORY="+t.TempDir())...)
	if err := cmd.Run(); err != nil && cmd.ProcessState == nil {
		t.Fatalf("bash: %v", err)
	}
	return cmd.ProcessState.ExitCode(), stdout.String(), stderr.String()
}

func buildRiskelle(t *testing.T, dir string, flags ...string) string {
	t.Helper()
	bin := filepath.Join(dir, "riskelle")
	if out, err := exec.Command("go", append(append([]string{"build"}, flags...), "-o", bin, "../cmd/riskelle")...).CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

// TestADOTemplate pins the wiring only an agent runs (PR builds only, full
// history, a pinned version, where releases come from, the token) and that it
// never names id4me or the retired releases parameter.
func TestADOTemplate(t *testing.T) {
	raw, _ := os.ReadFile(adoTemplate)
	for _, want := range []string{
		"condition: and(succeeded(), eq(variables['Build.Reason'], 'PullRequest'))",
		"- checkout: self\n        fetchDepth: 0\n", "  - name: version\n    type: string\n  - name: pool",
		"SYSTEM_ACCESSTOKEN: $(System.AccessToken)", "RISKELLE_VERSION: ${{ parameters.version }}",
		"RISKELLE_REPO_URL: ${{ parameters.repository }}",
	} {
		if !strings.Contains(string(raw), want) || strings.Contains(strings.ToLower(string(raw)), "id4me") {
			t.Errorf("%s lacks %q or names id4me", adoTemplate, want)
		}
	}
	if strings.Contains(string(raw), "parameters.releases") || strings.Contains(string(raw), "RISKELLE_RELEASES") {
		t.Errorf("%s still reads the retired releases parameter", adoTemplate)
	}
}

// pipelineRefusals are the releases each pipeline's install step must refuse
// against standInRelease, failing the step with nothing put on the PATH, and
// what its stderr must name. v0.0.3 is the checksum mismatch.
var pipelineRefusals = []struct{ name, version, stderr string }{
	{"version not a tag", "0.0.1", "vX.Y.Z"},
	{"empty version", "", "vX.Y.Z"},
	{"release without install.sh", "v0.0.9", "v0.19.0 or later"},
	{"checksum mismatch", "v0.0.3", "checksum mismatch"},
	{"no checksum line", "v0.0.4", "has no line for"},
}

// TestADOInstallStep runs the template's install step against a stand-in
// release: the pinned version's own install.sh puts that version on the
// PATH, and every refusal fails the step with nothing on the PATH.
func TestADOInstallStep(t *testing.T) {
	script := adoScript(t, "# riskelle: install")
	repo := standInRelease(t)
	prepend := regexp.MustCompile(`(?m)^##vso\[task\.prependpath\](.+)$`)
	install := func(version string) (int, string, string) {
		return runADOScript(t, script, t.TempDir(), "HOME="+t.TempDir(), "RISKELLE_VERSION="+version, "RISKELLE_REPO_URL="+repo)
	}
	code, stdout, stderr := install("v0.0.1")
	m := prepend.FindAllStringSubmatch(stdout, -1)
	if code != 0 || len(m) != 1 {
		t.Fatalf("install: exit %d, stdout %q, stderr %q; want one ##vso[task.prependpath]", code, stdout, stderr)
	}
	if got := installedVersion(t, filepath.Join(m[0][1], "riskelle")); got != "v0.0.1" {
		t.Errorf("riskelle on the prepended path reports %q, want v0.0.1", got)
	}
	for _, tt := range pipelineRefusals {
		code, stdout, stderr := install(tt.version)
		if code == 0 || prepend.MatchString(stdout) || !strings.Contains(stderr, tt.stderr) {
			t.Errorf("%s: exit %d, stdout %q, stderr %q; want a failure naming %q with nothing on the PATH", tt.name, code, stdout, stderr, tt.stderr)
		}
	}
}

// fakeADO serves, in memory, the part of the Azure DevOps REST API the
// template calls for pull request 7, for the token "tok". The script calls
// it one request at a time and the test reads it once the script exits.
type fakeADO struct {
	threads  []adoThread
	statuses []map[string]any
}

type adoThread struct {
	ID         int                        `json:"id"`
	Status     string                     `json:"status"`
	IsDeleted  bool                       `json:"isDeleted"`
	Properties map[string]json.RawMessage `json:"properties,omitempty"`
	Comments   []struct {
		ID      int    `json:"id"`
		Content string `json:"content"`
	} `json:"comments"`
}

func (f *fakeADO) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rest, ok := strings.CutPrefix(r.URL.Path, "/proj/_apis/git/repositories/repo/pullRequests/7/")
	switch {
	case r.Header.Get("Authorization") != "Bearer tok":
		w.WriteHeader(http.StatusUnauthorized)
	case !ok || r.URL.Query().Get("api-version") != "7.0":
		w.WriteHeader(http.StatusNotFound)
	case r.Method == "POST" && rest == "statuses":
		var s map[string]any
		json.NewDecoder(r.Body).Decode(&s)
		f.statuses = append(f.statuses, s)
	case r.Method == "GET" && rest == "threads":
		json.NewEncoder(w).Encode(map[string]any{"value": f.threads, "count": len(f.threads)})
	case r.Method == "POST" && rest == "threads":
		th := adoThread{ID: 100 + len(f.threads)}
		json.NewDecoder(r.Body).Decode(&th)
		th.Comments[0].ID = 1
		f.threads = append(f.threads, th)
		json.NewEncoder(w).Encode(th)
	case r.Method == "PATCH":
		var tid, cid int
		fmt.Sscanf(rest, "threads/%d/comments/%d", &tid, &cid)
		var c struct{ Content string }
		json.NewDecoder(r.Body).Decode(&c)
		for i, th := range f.threads {
			if th.ID == tid && th.Comments[0].ID == cid {
				f.threads[i].Comments[0].Content = c.Content
			}
		}
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// summaries returns the live threads carrying the riskelle property.
func (f *fakeADO) summaries() (out []adoThread) {
	for _, th := range f.threads {
		if _, ok := th.Properties["riskelle.summary"]; ok && !th.IsDeleted {
			out = append(out, th)
		}
	}
	return out
}

// TestADOScoreAndPublish runs the publish script on a PR build checkout (a
// detached merge into origin/main) against a fake ADO holding a human and a
// deleted riskelle thread. Each of two pushes posts the band as a status and
// leaves one summary comment, updated in place, equal to a local score of the
// same refs; an unscorable head posts an error, a refused token posts nothing.
func TestADOScoreAndPublish(t *testing.T) {
	script := adoScript(t, "# riskelle: score and publish")
	bin := buildRiskelle(t, t.TempDir())
	dir := rulesFixture(t,
		map[string]string{"go.mod": "module example.com/m\n", "core/calc.go": numbered("package core // ", 40)},
		map[string]string{"core/calc.go": numbered("package core // ", 120), "src/app.go": numbered("package src // ", 100)})
	fixtureGit(t, dir, "update-ref", "refs/remotes/origin/main", "main")
	mainSHA := fixtureGit(t, dir, "rev-parse", "main")
	var seed []adoThread
	json.Unmarshal([]byte(`[{"id":1,"status":"active","comments":[{"id":1,"content":"looks good"}]},
		{"id":2,"status":"closed","isDeleted":true,"properties":{"riskelle.summary":{"$type":"System.String","$value":"1"}},
		 "comments":[{"id":1,"content":"old"}]}]`), &seed)
	ado := &fakeADO{threads: seed}
	srv := httptest.NewServer(ado)
	defer srv.Close()
	publish := func(head, token string) (int, string, string) {
		return runADOScript(t, script, dir, "PATH="+filepath.Dir(bin)+string(os.PathListSeparator)+os.Getenv("PATH"),
			"SYSTEM_COLLECTIONURI="+srv.URL+"/", "SYSTEM_TEAMPROJECTID=proj", "BUILD_REPOSITORY_ID=repo",
			"SYSTEM_PULLREQUEST_PULLREQUESTID=7", "SYSTEM_PULLREQUEST_TARGETBRANCH=refs/heads/main",
			"SYSTEM_PULLREQUEST_SOURCECOMMITID="+head, "BUILD_BUILDID=42", "SYSTEM_ACCESSTOKEN="+token)
	}
	artifact := regexp.MustCompile(`(?m)^##vso\[artifact\.upload artifactname=riskelle\](.+)$`)
	var previous string
	for push := 1; push <= 2; push++ {
		if push == 2 {
			fixtureGit(t, dir, "switch", "--quiet", "topic")
			writeFile(t, dir, "src/more.go", numbered("package src // more ", 200))
			fixtureGit(t, dir, "add", ".")
			fixtureGit(t, dir, "commit", "--quiet", "-m", "second push")
		}
		fixtureGit(t, dir, "switch", "--quiet", "--detach", "origin/main")
		fixtureGit(t, dir, "merge", "--quiet", "--no-ff", "--no-edit", "topic")
		head := fixtureGit(t, dir, "rev-parse", "topic")
		code, stdout, stderr := publish(head, "tok")
		m := artifact.FindStringSubmatch(stdout)
		if code != 0 || m == nil {
			t.Fatalf("push %d: exit %d\nstdout %s\nstderr %s", push, code, stdout, stderr)
		}
		gotJSON, _ := os.ReadFile(m[1])
		_, wantJSON, _ := runScore(t, bin, dir, "--base", "origin/main", "--head", head)
		_, wantMD, _ := runScore(t, bin, dir, "--format", "markdown", "--base", "origin/main", "--head", head)
		var rep report.Report
		if err := json.Unmarshal(gotJSON, &rep); err != nil || string(gotJSON) != wantJSON || rep.Base != mainSHA || rep.Head != head {
			t.Errorf("push %d: pipeline report (%v) differs from the local score of %s..%s:\n%s\n%s", push, err, mainSHA, head, gotJSON, wantJSON)
		}
		want := fmt.Sprintf(`{"context":{"genre":"riskelle","name":"score"},"description":"%s, %d/100","state":"succeeded","targetUrl":"%s/proj/_build/results?buildId=42"}`,
			rep.Level, rep.Total, srv.URL)
		if got, _ := json.Marshal(ado.statuses); len(ado.statuses) != push || !strings.HasSuffix(string(got), want+"]") {
			t.Errorf("push %d: statuses %s, want %d ending %s", push, got, push, want)
		}
		// Push 1 creates thread 102 (two seeded); push 2 must update its comment.
		sum := ado.summaries()
		if len(sum) != 1 || sum[0].ID != 102 || len(sum[0].Comments) != 1 || sum[0].Comments[0].ID != 1 ||
			sum[0].Comments[0].Content != wantMD || sum[0].Status != "closed" || wantMD == previous ||
			compact(t, sum[0].Properties["riskelle.summary"]) != `{"$type":"System.String","$value":"1"}` {
			t.Fatalf("push %d: riskelle threads %+v, want closed thread 102 whose one comment is the new local Markdown:\n%s", push, sum, wantMD)
		}
		previous = wantMD
	}
	if len(ado.threads) != 3 || ado.threads[0].Comments[0].Content != "looks good" || ado.threads[1].Comments[0].Content != "old" {
		t.Errorf("threads %+v, want the human and the deleted thread untouched", ado.threads)
	}

	ado.statuses, ado.threads = nil, nil
	code, _, stderr := publish(strings.Repeat("d", 40), "tok")
	if code == 0 || !strings.Contains(stderr, "fetchDepth: 0") || len(ado.statuses) != 1 || ado.statuses[0]["state"] != "error" || len(ado.threads) != 0 {
		t.Errorf("unknown head: exit %d, stderr %q, statuses %v, threads %v; want a failure, one error status, no thread", code, stderr, ado.statuses, ado.threads)
	}
	ado.statuses = nil
	if code, _, stderr := publish(fixtureGit(t, dir, "rev-parse", "topic"), "wrong"); code == 0 ||
		!strings.Contains(stderr, "Contribute to pull requests") || len(ado.statuses) != 0 || len(ado.threads) != 0 {
		t.Errorf("refused token: exit %d, stderr %q; want a failure naming the permission and nothing posted", code, stderr)
	}
}
