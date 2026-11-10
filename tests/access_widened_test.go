//go:build integration

package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/bobmcallan/riskelle/internal/score"
)

// TestAccessWidenedFloorsHigh drives the built binary over a role assignment
// in iam/main.tf. A grant added or altered carries access_widened (floor
// High) beside identity_rights (Medium), each naming the path and type, and
// is High though its shape is Low; access_widened is its trigger and the
// Markdown lists it; estimate with a plan naming both classes agrees. A grant
// removed, or a line removed from a kept grant, carries identity_rights alone
// and is Medium, with no access_widened anywhere in the report.
func TestAccessWidenedFloorsHigh(t *testing.T) {
	bin := buildRiskelle(t, t.TempDir())
	run := func(t *testing.T, dir string, args ...string) string {
		t.Helper()
		code, out, stderr := runScore(t, bin, dir, append([]string{"--base", "main", "--head", "topic"}, args...)...)
		if code != 0 {
			t.Fatalf("score exit %d: %s", code, stderr)
		}
		return out
	}
	const both = `[{"class":"access_widened","paths":["iam/main.tf"],"types":["azurerm_role_assignment"],"floor":"High"},` +
		`{"class":"identity_rights","paths":["iam/main.tf"],"types":["azurerm_role_assignment"],"floor":"Medium"}]`
	const trigger = `[{"class":"access_widened","paths":["iam/main.tf"],"types":["azurerm_role_assignment"],"why":"class"}]`
	const alone = `[{"class":"identity_rights","paths":["iam/main.tf"],"types":["azurerm_role_assignment"],"floor":"Medium"}]`
	withRole := map[string]string{"iam/main.tf": tfStorage + "\n" + tfRole}
	described := strings.Replace(tfRole, "  role = \"Reader\"\n", "  role        = \"Reader\"\n  description = \"read only\"\n", 1)

	for _, tt := range []struct {
		name        string
		base, topic map[string]string
	}{
		{"grant added", map[string]string{"iam/main.tf": tfStorage}, withRole},
		{"grant altered", withRole, map[string]string{"iam/main.tf": tfStorage + "\n" + strings.Replace(tfRole, "Reader", "Owner", 1)}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := rulesFixture(t, tt.base, tt.topic)
			got := decodeReport(t, run(t, dir))
			if got.Level != score.High || got.ShapeLevel != score.Low || !reflect.DeepEqual(got.FlooredBy, []string{"access_widened"}) {
				t.Errorf("level %s shape %s floored_by %v, want High Low [access_widened]", got.Level, got.ShapeLevel, got.FlooredBy)
			}
			if b, _ := json.Marshal(got.Consequences); string(b) != both {
				t.Errorf("consequences = %s\nwant %s", b, both)
			}
			if b, _ := json.Marshal(got.Triggers); string(b) != trigger {
				t.Errorf("triggers = %s\nwant %s", b, trigger)
			}
			md := run(t, dir, "--format", "markdown")
			for _, w := range []string{
				"- access_widened, floor High: paths `iam/main.tf`; types `azurerm_role_assignment`",
				"- access_widened, High by class: paths `iam/main.tf`; types `azurerm_role_assignment`",
				"Shape level Low; the level is High, set by the consequence floor of access_widened.",
			} {
				if !strings.Contains(md, w) {
					t.Errorf("markdown lacks %q:\n%s", w, md)
				}
			}

			plan := filepath.Join(t.TempDir(), "plan.json")
			if err := os.WriteFile(plan, []byte(`{"target":"main","files":[{"path":"iam/main.tf","added":3,"consequences":["access_widened","identity_rights"]}]}`), 0o644); err != nil {
				t.Fatal(err)
			}
			code, out, stderr := runEstimate(t, bin, dir, plan)
			if code != 0 {
				t.Fatalf("estimate exit %d: %s", code, stderr)
			}
			est := decodeReport(t, out)
			if est.Level != score.High || !reflect.DeepEqual(est.FlooredBy, []string{"access_widened"}) {
				t.Errorf("estimate level %s floored_by %v, want High [access_widened]", est.Level, est.FlooredBy)
			}
		})
	}

	for _, tt := range []struct {
		name        string
		base, topic map[string]string
	}{
		{"grant removed", withRole, map[string]string{"iam/main.tf": tfStorage}},
		{"line removed from a kept grant", map[string]string{"iam/main.tf": tfStorage + "\n" + described},
			map[string]string{"iam/main.tf": tfStorage + "\n" + strings.Replace(described, "  description = \"read only\"\n", "", 1)}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			out := run(t, rulesFixture(t, tt.base, tt.topic))
			got := decodeReport(t, out)
			if got.Level != score.Medium || !reflect.DeepEqual(got.FlooredBy, []string{"identity_rights"}) || len(got.Triggers) != 0 {
				t.Errorf("level %s floored_by %v triggers %+v, want Medium [identity_rights] and none", got.Level, got.FlooredBy, got.Triggers)
			}
			if b, _ := json.Marshal(got.Consequences); string(b) != alone {
				t.Errorf("consequences = %s\nwant %s", b, alone)
			}
			if strings.Contains(out, "access_widened") {
				t.Errorf("report names access_widened:\n%s", out)
			}
		})
	}
}
