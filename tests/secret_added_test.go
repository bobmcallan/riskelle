//go:build integration

package tests

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/bobmcallan/riskelle/internal/score"
)

// TestSecretAddedFloorsHigh drives the built binary. A GitHub token added to
// a config file and a private key header added in a new file carry
// secret_added (floor High) naming both paths and both rules; the change is
// High though its shape is Low, secret_added is its trigger, and neither the
// JSON nor the Markdown report holds the token, the key header or the key
// body. A removed token line, an untouched token line of a modified file, a
// placeholder shorter than the rule and a token in a binary file carry
// nothing. Every sample is built at runtime, so no token sits in this file.
func TestSecretAddedFloorsHigh(t *testing.T) {
	bin := buildRiskelle(t, t.TempDir())
	run := func(t *testing.T, dir string, args ...string) string {
		t.Helper()
		code, out, stderr := runScore(t, bin, dir, append([]string{"--base", "main", "--head", "topic"}, args...)...)
		if code != 0 {
			t.Fatalf("score exit %d: %s", code, stderr)
		}
		return out
	}
	token := "gh" + "p_" + strings.Repeat("A", 36)
	keyHeader := "-----BEGIN " + "RSA " + "PRIVATE KEY-----"
	keyBody := "MIIEow" + strings.Repeat("Q", 40)

	t.Run("added", func(t *testing.T) {
		dir := rulesFixture(t,
			map[string]string{"go.mod": "module example.com/m\n", "config/app.env": "LOG_LEVEL=info\n"},
			map[string]string{
				"config/app.env": "LOG_LEVEL=info\nGITHUB_TOKEN=" + token + "\n",
				"deploy/id_rsa":  keyHeader + "\n" + keyBody + "\n",
			})
		out := run(t, dir)
		got := decodeReport(t, out)
		if got.Level != score.High || got.ShapeLevel != score.Low || !reflect.DeepEqual(got.FlooredBy, []string{"secret_added"}) {
			t.Errorf("level %s shape %s floored_by %v, want High Low [secret_added]", got.Level, got.ShapeLevel, got.FlooredBy)
		}
		sum := 0
		for _, p := range got.Breakdown {
			sum += p.Points
		}
		if got.Total != sum {
			t.Errorf("total %d, want the sum of the points %d", got.Total, sum)
		}
		const cons = `[{"class":"secret_added","paths":["config/app.env","deploy/id_rsa"],"types":["github_token","private_key"],"floor":"High"}]`
		if b, _ := json.Marshal(got.Consequences); string(b) != cons {
			t.Errorf("consequences = %s\nwant %s", b, cons)
		}
		const trig = `[{"class":"secret_added","paths":["config/app.env","deploy/id_rsa"],"types":["github_token","private_key"],"why":"class"}]`
		if b, _ := json.Marshal(got.Triggers); string(b) != trig {
			t.Errorf("triggers = %s\nwant %s", b, trig)
		}
		md := run(t, dir, "--format", "markdown")
		for _, w := range []string{
			"- secret_added, floor High: paths `config/app.env`, `deploy/id_rsa`; types `github_token`, `private_key`",
			"- secret_added, High by class: paths `config/app.env`, `deploy/id_rsa`; types `github_token`, `private_key`",
			"Shape level Low; the level is High, set by the consequence floor of secret_added.",
		} {
			if !strings.Contains(md, w) {
				t.Errorf("markdown lacks %q", w) // the report is not printed: it is under test for the value
			}
		}
		for name, report := range map[string]string{"JSON": out, "Markdown": md} {
			for what, v := range map[string]string{"token": token, "key header": keyHeader, "key body": keyBody, "token tail": strings.Repeat("A", 36)} {
				if strings.Contains(report, v) {
					t.Errorf("the %s report contains the %s", name, what) // never print the value
				}
			}
		}
	})

	t.Run("nothing carried", func(t *testing.T) {
		dir := rulesFixture(t,
			map[string]string{
				"go.mod":         "module example.com/m\n",
				"config/app.env": "GITHUB_TOKEN=" + token + "\nLOG_LEVEL=info\n",
				"keep.env":       "GITHUB_TOKEN=" + token + "\nLOG_LEVEL=info\n",
			},
			map[string]string{
				"config/app.env": "LOG_LEVEL=info\n",                              // the token line removed
				"keep.env":       "GITHUB_TOKEN=" + token + "\nLOG_LEVEL=debug\n", // only line 2 changed
				"docs/setup.md":  "Set GITHUB_TOKEN to " + "gh" + "p_xxx" + "\n",  // a placeholder
				"bin/blob.dat":   "\x00" + token + "\n",                           // binary
			})
		out := run(t, dir)
		got := decodeReport(t, out)
		if got.Level != got.ShapeLevel || len(got.FlooredBy) != 0 || len(got.Triggers) != 0 {
			t.Errorf("level %s shape %s floored_by %v triggers %+v, want the shape level and none", got.Level, got.ShapeLevel, got.FlooredBy, got.Triggers)
		}
		if !strings.Contains(out, `"consequences": []`) {
			t.Errorf("consequences = %+v, want []", got.Consequences)
		}
		md := run(t, dir, "--format", "markdown")
		if strings.Contains(md, "secret_added") || strings.Contains(md, "Consequences (floor the level)") {
			t.Errorf("markdown names a consequence:\n%s", md)
		}
	})
}
