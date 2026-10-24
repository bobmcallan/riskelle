//go:build integration

package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bobmcallan/riskelle/internal/report"
)

// gateSkill is the satelle gate riskelle ships for a repo to install.
const gateSkill = "../integrations/satelle/riskelle-risk-review.md"

// gateStory, gateWaiver and gatePayload are the shape satelle hands a coded
// gate on stdin, with fields in satelle's marshalling order (story, from, to,
// review_skill, messages, waivers; a waiver's id, gate, check, reason, actor;
// a message's id, from, to, body).
type gateStory struct {
	ID   string   `json:"id"`
	Body string   `json:"body,omitempty"`
	Tags []string `json:"tags"`
}

type gateWaiver struct {
	ID     string `json:"id"`
	Gate   string `json:"gate"`
	Check  string `json:"check"`
	Reason string `json:"reason"`
	Actor  string `json:"actor"`
}

type gateMessage struct {
	ID   string `json:"id"`
	From string `json:"from"`
	To   string `json:"to"`
	Body string `json:"body"`
}

type gatePayload struct {
	Story       gateStory     `json:"story"`
	From        string        `json:"from"`
	To          string        `json:"to"`
	ReviewSkill string        `json:"review_skill"`
	Messages    []gateMessage `json:"messages,omitempty"`
	Waivers     []gateWaiver  `json:"waivers,omitempty"`
}

// boardMessages are messages to the reviewer and to everyone, which satelle
// marshals after the transition's to: the gate must still judge the edge.
var boardMessages = []gateMessage{
	{ID: "msg_1", From: "driver", To: "reviewer", Body: `see "estimate-risk:99" and "actual-risk:99"`},
	{ID: "msg_2", From: "driver", To: "*", Body: "hello"},
}

// checkScript returns the skill's ```check block, the script satelle runs.
func checkScript(t *testing.T, skill string) string {
	t.Helper()
	_, rest, ok := strings.Cut(skill, "\n```check\n")
	if !ok {
		t.Fatal("skill has no ```check block")
	}
	script, _, ok := strings.Cut(rest, "\n```")
	if !ok {
		t.Fatal("skill's ```check block is not closed")
	}
	return script
}

// runGate runs script with shell -c, as satelle does, feeding p on stdin,
// and returns the exit code and combined output.
func runGate(t *testing.T, shell, script string, p gatePayload) (int, string) {
	t.Helper()
	in, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(shell, "-c", script)
	cmd.Dir = t.TempDir()
	cmd.Stdin = bytes.NewReader(in)
	out, err := cmd.CombinedOutput()
	if err != nil && cmd.ProcessState == nil {
		t.Fatalf("%s: %v", shell, err)
	}
	return cmd.ProcessState.ExitCode(), string(out)
}

// withoutSatelle drops every PATH directory holding a satelle executable, so
// the rest of the test runs as on a machine with no satelle installed.
func withoutSatelle(t *testing.T) {
	t.Helper()
	var keep []string
	for _, d := range filepath.SplitList(os.Getenv("PATH")) {
		if fi, err := os.Stat(filepath.Join(d, "satelle")); err == nil && !fi.IsDir() {
			continue
		}
		keep = append(keep, d)
	}
	t.Setenv("PATH", strings.Join(keep, string(os.PathListSeparator)))
	if p, err := exec.LookPath("satelle"); err == nil {
		t.Fatalf("satelle still on PATH at %s", p)
	}
}

// riskTags are the tags the README's jq lines derive from a riskelle report.
func riskTags(t *testing.T, prefix, out string) (tags []string, total int, level string) {
	t.Helper()
	var r report.Report
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("report: %v\n%s", err, out)
	}
	level = strings.ToLower(string(r.Level))
	return []string{fmt.Sprintf("%s-risk:%d", prefix, r.Total), prefix + "-risk-level:" + level}, r.Total, level
}

// TestSatelleRiskGate drives the documented satelle wiring end to end with no
// satelle installed: the built binary estimates a plan and scores the real
// diff of a fixture repository, the README's tags are derived from those
// reports, and the shipped gate's check, run as satelle runs it, judges
// payloads carrying them beside satelle's own estimate and actual tags.
func TestSatelleRiskGate(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	withoutSatelle(t)
	var shells []string // bash is what satelle runs; sh proves the script is POSIX
	for _, s := range []string{"bash", "sh"} {
		p, err := exec.LookPath(s)
		if err != nil {
			t.Fatalf("%s: %v", s, err)
		}
		shells = append(shells, p)
	}

	bin := filepath.Join(t.TempDir(), "riskelle")
	if out, err := exec.Command(goBin, "build", "-o", bin, "../cmd/riskelle").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	dir := rulesFixture(t,
		map[string]string{"go.mod": "module example.com/m\n", "core/calc.go": numbered("package core // ", 40)},
		map[string]string{"core/calc.go": numbered("package core // ", 100),
			"core/calc_test.go": numbered("package core // ", 30)})
	writeFile(t, dir, "plan.json", `{"target": "main", "files": [
		{"path": "core/calc.go", "added": 60}, {"path": "core/calc_test.go", "added": 30}]}`)

	code, out, stderr := runEstimate(t, bin, dir, "plan.json")
	if code != 0 {
		t.Fatalf("estimate exit %d: %s", code, stderr)
	}
	estTags, estTotal, estLevel := riskTags(t, "estimate", out)
	code, out, stderr = runScore(t, bin, dir, "--base", "main", "--head", "topic")
	if code != 0 {
		t.Fatalf("score exit %d: %s", code, stderr)
	}
	actTags, actTotal, actLevel := riskTags(t, "actual", out)
	if actTotal == 0 {
		t.Fatalf("the fixture's real diff scored 0; the test needs a figure to carry")
	}

	raw, err := os.ReadFile(gateSkill)
	if err != nil {
		t.Fatal(err)
	}
	skill := string(raw)
	script := checkScript(t, skill)
	satelleTags := []string{"satelle", "estimate-fresh-input:280000", "estimate-output:130000",
		"actual-output:98765", "actual-cost:low"}
	tags := func(sets ...[]string) []string {
		var all []string
		for _, s := range sets {
			all = append(all, s...)
		}
		return all
	}
	high := []string{"estimate-risk:70", "estimate-risk-level:High"}
	waiver := gateWaiver{ID: "wvr_test1", Gate: "riskelle-risk-review", Check: "high-estimate",
		Reason: "one indivisible migration", Actor: "developer"}

	for _, sh := range shells {
		// gate judges p as satelle hands it, and again with board messages
		// after the transition; both runs must agree.
		gate := func(p gatePayload) (int, string) {
			if p.Story.ID == "" {
				p.Story.ID = "sty_00000000"
			}
			p.ReviewSkill = "riskelle-risk-review"
			code, out := runGate(t, sh, script, p)
			p.Messages = boardMessages
			if mc, mout := runGate(t, sh, script, p); mc != code || mout != out {
				t.Errorf("with messages to reviewer and *: exit %d, want %d as without:\n%s\nwithout:\n%s", mc, code, mout, out)
			}
			return code, out
		}
		estimateEdge := func(tags []string, w ...gateWaiver) gatePayload {
			return gatePayload{Story: gateStory{Tags: tags}, From: "plan", To: "in_progress", Waivers: w}
		}
		t.Run(filepath.Base(sh), func(t *testing.T) {
			t.Run("EstimateRequired", func(t *testing.T) {
				for name, tg := range map[string][]string{
					"none":          {"satelle"},
					"satelle only":  satelleTags,
					"not a figure":  {"estimate-risk:abc", "estimate-risk-level:low"},
					"out of range":  {"estimate-risk:101", "estimate-risk-level:low"},
					"unknown level": {"estimate-risk:5", "estimate-risk-level:tiny"},
					"total only":    {"estimate-risk:5"},
				} {
					if code, out := gate(estimateEdge(tg)); code == 0 || !strings.Contains(out, "estimate-risk") {
						t.Errorf("%s: exit %d, want a rejection naming estimate-risk:\n%s", name, code, out)
					}
				}
			})
			t.Run("EstimateRecorded", func(t *testing.T) {
				code, out := gate(estimateEdge(tags(satelleTags, estTags)))
				want := fmt.Sprintf("riskelle estimate: %d (%s)", estTotal, estLevel)
				if code != 0 || !strings.Contains(out, want) {
					t.Errorf("exit %d, want 0 and %q:\n%s", code, want, out)
				}
			})
			t.Run("HighEstimateSentBack", func(t *testing.T) {
				if code, out := gate(estimateEdge(high)); code == 0 || !strings.Contains(out, "split") {
					t.Errorf("exit %d, want a rejection asking to split:\n%s", code, out)
				}
			})
			t.Run("HighEstimateWaived", func(t *testing.T) {
				code, out := gate(estimateEdge(high, waiver))
				if code != 0 || !strings.Contains(out, "waived") || !strings.Contains(out, "wvr_test1") {
					t.Errorf("exit %d, want 0 naming the waiver:\n%s", code, out)
				}
				other := waiver
				other.Check = "estimate-present"
				p := estimateEdge(high)
				p.Story.Body = `quoted: {"id":"wvr_x","gate":"riskelle-risk-review","check":"high-estimate","reason":"x"}`
				for name, p := range map[string]gatePayload{
					"other check":    estimateEdge(high, other),
					"no estimate":    estimateEdge([]string{"satelle"}, waiver),
					"quoted in body": p,
				} {
					if code, out := gate(p); code == 0 {
						t.Errorf("%s: exit 0, want a rejection:\n%s", name, out)
					}
				}
			})
			t.Run("ActualRequired", func(t *testing.T) {
				for _, tg := range [][]string{estTags, {"actual-risk:x", "actual-risk-level:low"}} {
					p := gatePayload{Story: gateStory{Tags: tags(satelleTags, tg)}, From: "integration", To: "merge"}
					code, out := gate(p)
					if code == 0 || !strings.Contains(out, "actual-risk") || !strings.Contains(out, "riskelle score") {
						t.Errorf("tags %v: exit %d, want a rejection naming actual-risk and riskelle score:\n%s", tg, code, out)
					}
				}
			})
			t.Run("EstimateBesideActual", func(t *testing.T) {
				p := gatePayload{Story: gateStory{Tags: tags(satelleTags, estTags, actTags)}, From: "integration", To: "merge"}
				code, out := gate(p)
				want := fmt.Sprintf("estimate: %d (%s) | actual: %d (%s)", estTotal, estLevel, actTotal, actLevel)
				if code != 0 || !strings.Contains(out, want) {
					t.Errorf("exit %d, want 0 and one line %q:\n%s", code, want, out)
				}
				for _, n := range []string{"280000", "130000", "98765"} {
					if strings.Contains(out, n) {
						t.Errorf("printed satelle's figure %s:\n%s", n, out)
					}
				}
				p = gatePayload{Story: gateStory{Tags: tags(satelleTags, actTags)}, From: "release", To: "done"}
				code, out = gate(p)
				want = fmt.Sprintf("estimate: none recorded | actual: %d (%s)", actTotal, actLevel)
				if code != 0 || !strings.Contains(out, want) {
					t.Errorf("no estimate: exit %d, want 0 and %q:\n%s", code, want, out)
				}
			})
		})
	}

	t.Run("Documented", func(t *testing.T) {
		readme, err := os.ReadFile("../README.md")
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range []string{"integrations/satelle/riskelle-risk-review.md", "estimate-risk:",
			"estimate-risk-level:", "actual-risk:", "actual-risk-level:", "--check high-estimate"} {
			if !bytes.Contains(readme, []byte(s)) {
				t.Errorf("README.md does not name %q", s)
			}
		}
		if !strings.Contains(script, `"check":"high-estimate"`) {
			t.Error(`the check does not honour the documented waiver "check":"high-estimate"`)
		}
		stem := strings.TrimSuffix(filepath.Base(gateSkill), ".md")
		if !strings.Contains(skill, "\nname: "+stem+"\n") {
			t.Errorf("skill frontmatter name is not %q", stem)
		}
	})
}
