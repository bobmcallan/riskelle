//go:build integration

package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/bobmcallan/riskelle/internal/report"
	"github.com/bobmcallan/riskelle/internal/score"
)

// TestStatefulDestroyFloorsHigh drives the built binary over a Terraform
// directory holding a virtual network and a storage account. Removing the
// storage account's block, or deleting its whole file, carries
// stateful_destroy (floor High) naming the type: the change is High though its
// shape is Low, stateful_destroy floors it and is its trigger, and the
// Markdown lists it; estimate with a plan naming the class agrees. Moving the
// block to another file of the directory, renaming it with a moved block,
// adding a removed block for it, or removing a non-stateful type carries
// nothing and stays Low.
func TestStatefulDestroyFloorsHigh(t *testing.T) {
	bin := buildRiskelle(t, t.TempDir())
	run := func(t *testing.T, dir string, args ...string) string {
		t.Helper()
		code, out, stderr := runScore(t, bin, dir, append([]string{"--base", "main", "--head", "topic"}, args...)...)
		if code != 0 {
			t.Fatalf("score exit %d: %s", code, stderr)
		}
		return out
	}
	sum := func(r report.Report) int {
		n := 0
		for _, p := range r.Breakdown {
			n += p.Points
		}
		return n
	}
	const (
		vnet    = "resource \"azurerm_virtual_network\" \"n\" {\n  name = \"n\"\n}\n"
		renamed = "resource \"azurerm_storage_account\" \"t\" {\n  name = \"one\"\n}\n"
		moved   = "moved {\n  from = azurerm_storage_account.s\n  to   = azurerm_storage_account.t\n}\n"
		removed = "removed {\n  from = azurerm_storage_account.s\n\n  lifecycle {\n    destroy = false\n  }\n}\n"
	)
	both := map[string]string{"infra/main.tf": vnet + "\n" + tfStorage}

	for _, tt := range []struct {
		name, path string
		dir        func(t *testing.T) string
	}{
		{"block deleted", "infra/main.tf", func(t *testing.T) string {
			return rulesFixture(t, both, map[string]string{"infra/main.tf": vnet})
		}},
		{"whole file deleted", "infra/storage.tf", func(t *testing.T) string {
			return deletionFixture(t, map[string]string{"infra/main.tf": vnet, "infra/storage.tf": tfStorage}, "infra/storage.tf")
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := tt.dir(t)
			got := decodeReport(t, run(t, dir))
			if got.Level != score.High || got.ShapeLevel != score.Low || got.Total >= 20 || got.Total != sum(got) {
				t.Errorf("level %s shape %s total %d (points sum %d), want High, Low and a Low total equal to the sum",
					got.Level, got.ShapeLevel, got.Total, sum(got))
			}
			if !reflect.DeepEqual(got.FlooredBy, []string{"stateful_destroy"}) {
				t.Errorf("floored_by %v, want [stateful_destroy]", got.FlooredBy)
			}
			want := `[{"class":"stateful_destroy","paths":["` + tt.path + `"],"types":["azurerm_storage_account"],"floor":"High"}]`
			if b, _ := json.Marshal(got.Consequences); string(b) != want {
				t.Errorf("consequences = %s\nwant %s", b, want)
			}
			trigger := `[{"class":"stateful_destroy","paths":["` + tt.path + `"],"types":["azurerm_storage_account"],"why":"class"}]`
			if b, _ := json.Marshal(got.Triggers); string(b) != trigger {
				t.Errorf("triggers = %s\nwant %s", b, trigger)
			}
			md := run(t, dir, "--format", "markdown")
			for _, w := range []string{
				"- stateful_destroy, floor High: paths `" + tt.path + "`; types `azurerm_storage_account`",
				"Shape level Low; the level is High, set by the consequence floor of stateful_destroy.",
			} {
				if !strings.Contains(md, w) {
					t.Errorf("markdown lacks %q:\n%s", w, md)
				}
			}

			plan := filepath.Join(t.TempDir(), "plan.json")
			if err := os.WriteFile(plan, []byte(`{"target":"main","files":[{"path":"`+tt.path+`","removed":3,"consequences":["stateful_destroy"]}]}`), 0o644); err != nil {
				t.Fatal(err)
			}
			code, out, stderr := runEstimate(t, bin, dir, plan)
			if code != 0 {
				t.Fatalf("estimate exit %d: %s", code, stderr)
			}
			if est := decodeReport(t, out); est.Level != score.High || !reflect.DeepEqual(est.FlooredBy, []string{"stateful_destroy"}) {
				t.Errorf("estimate level %s floored_by %v, want High [stateful_destroy]", est.Level, est.FlooredBy)
			}
		})
	}

	for _, tt := range []struct {
		name  string
		topic map[string]string
	}{
		{"moved to another file", map[string]string{"infra/main.tf": vnet, "infra/storage.tf": tfStorage}},
		{"renamed with a moved block", map[string]string{"infra/main.tf": vnet + "\n" + renamed + "\n" + moved}},
		{"removed block", map[string]string{"infra/main.tf": vnet + "\n" + removed}},
		{"non-stateful type removed", map[string]string{"infra/main.tf": tfStorage}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			out := run(t, rulesFixture(t, both, tt.topic))
			got := decodeReport(t, out)
			if got.Level != score.Low || len(got.Consequences) != 0 || !strings.Contains(out, `"consequences": []`) {
				t.Errorf("level %s consequences %+v, want Low and []", got.Level, got.Consequences)
			}
			if strings.Contains(out, "stateful_destroy") {
				t.Errorf("report names stateful_destroy:\n%s", out)
			}
		})
	}
}
