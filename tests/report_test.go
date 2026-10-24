//go:build integration

package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bobmcallan/riskelle/internal/report"
)

// TestReportFormatsAndContext drives the built binary's estimate in both
// formats on a fixture repository whose target branch's rules add a
// "payments" area. A plan landing in auth and payments reports both areas
// under context, labelled as not scored, beside the total, level, breakdown
// and provenance; a plan landing nowhere says so. Each output is the same
// bytes run to run.
func TestReportFormatsAndContext(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "riskelle")
	if out, err := exec.Command("go", "build", "-o", bin, "../cmd/riskelle").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	dir := rulesFixture(t,
		map[string]string{"go.mod": "module example.com/m\n", ".risk/rules.json": `{"areas":{"payments":["billing/**"]}}` + "\n",
			"core/calc.go": numbered("package core // ", 40)},
		map[string]string{"core/calc.go": numbered("package core // ", 41)})
	mainSHA := strings.TrimSpace(fixtureGit(t, dir, "rev-parse", "main"))
	writeFile(t, dir, "lands.json", `{"target": "main", "files": [
		{"path": "internal/auth/login.go", "added": 20}, {"path": "billing/charge.go", "added": 10},
		{"path": "core/calc.go", "added": 5}, {"path": "core/calc_test.go", "added": 5}]}`)
	writeFile(t, dir, "nowhere.json", `{"target": "main", "files": [{"path": "core/calc.go", "added": 5}]}`)

	stable := func(args ...string) string {
		t.Helper()
		code, out, stderr := runEstimate(t, bin, dir, args...)
		if code != 0 || stderr != "" {
			t.Fatalf("riskelle estimate %v: exit %d, stderr %q", args, code, stderr)
		}
		if _, again, _ := runEstimate(t, bin, dir, args...); again != out {
			t.Errorf("riskelle estimate %v differs between runs:\n%s\n%s", args, out, again)
		}
		return out
	}

	out := stable("lands.json")
	if def := stable("--format", "json", "lands.json"); def != out {
		t.Errorf("--format json differs from the default:\n%s\n%s", def, out)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &top); err != nil {
		t.Fatalf("estimate JSON %q: %v", out, err)
	}
	for _, key := range []string{"kind", "plan", "total", "level", "breakdown", "context", "provenance"} {
		if _, ok := top[key]; !ok {
			t.Errorf("JSON lacks %q:\n%s", key, out)
		}
	}
	if _, ok := top["areas"]; ok {
		t.Errorf("areas at the top level, beside the scored fields; want them only under context:\n%s", out)
	}
	wantContext := `{"note":"` + report.Note + `","areas":[{"name":"auth","paths":["internal/auth/login.go"]},` +
		`{"name":"payments","paths":["billing/charge.go"]}]}`
	if compact(t, top["context"]) != wantContext {
		t.Errorf("context = %s, want %s", top["context"], wantContext)
	}
	var r report.Report
	json.Unmarshal([]byte(out), &r)
	pv := r.Provenance
	if pv.Version == "" || pv.RulesSource != ".risk/rules.json" || pv.RulesCommit != mainSHA || !strings.HasPrefix(pv.RulesHash, "sha256:") {
		t.Errorf("provenance = %+v, want a version and .risk/rules.json at %s with a sha256 hash", pv, mainSHA)
	}
	if len(r.Breakdown) == 0 {
		t.Fatalf("empty breakdown:\n%s", out)
	}

	md := stable("--format", "markdown", "lands.json")
	want := []string{
		fmt.Sprintf("## riskelle estimate: %d / 100, %s\n", r.Total, r.Level),
		"Estimate from plan `lands.json`, not a measured diff.",
		"### Where it lands (context, not scored)\n\n" + report.Note + "\n\n" +
			"- auth: `internal/auth/login.go`\n- payments: `billing/charge.go`\n",
		"- riskelle `" + pv.Version + "`\n- rules `.risk/rules.json` at `" + mainSHA + "`\n- rules hash `" + pv.RulesHash + "`\n",
	}
	for _, p := range r.Breakdown {
		want = append(want, fmt.Sprintf("| %s | %s | %d | %d | %d |\n", p.Measure, p.Fact, p.Value, p.Min, p.Points))
	}
	for _, s := range want {
		if !strings.Contains(md, s) {
			t.Errorf("markdown lacks %q:\n%s", s, md)
		}
	}

	if out := stable("nowhere.json"); !strings.Contains(out, `"areas": []`) || !strings.Contains(out, `"note": "`+report.Note) {
		t.Errorf("no-area JSON lacks the note and \"areas\": []:\n%s", out)
	}
	if md := stable("--format", "markdown", "nowhere.json"); !strings.Contains(md, report.Note+"\n\nNone: the change lands in no sensitive area.\n") {
		t.Errorf("no-area markdown lacks the note and None:\n%s", md)
	}

	code, stdout, stderr := runEstimate(t, bin, dir, "--format", "xml", "lands.json")
	if code != 2 || stdout != "" || !strings.Contains(stderr, `unknown format "xml"`) {
		t.Errorf("--format xml: exit %d, stdout %q, stderr %q; want exit 2, no stdout, unknown format", code, stdout, stderr)
	}
}

func compact(t *testing.T, raw []byte) string {
	t.Helper()
	var b bytes.Buffer
	if err := json.Compact(&b, raw); err != nil {
		t.Fatal(err)
	}
	return b.String()
}
