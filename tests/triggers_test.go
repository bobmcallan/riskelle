//go:build integration

package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bobmcallan/riskelle/internal/report"
	"github.com/bobmcallan/riskelle/internal/score"
)

// TestTriggersReported drives the built binary over a fixture whose target
// rules name prod-tenant/** a production root. A role assignment added under
// the root fires one trigger: the report is High, its "triggers" names the
// class, the path, the matched type and why, and the Markdown lists it;
// estimate agrees. The same grant under sandbox/ fires none: the report is
// Medium, "triggers" is [] and the Markdown says none fired. In both the
// level is High exactly when triggers is not empty.
func TestTriggersReported(t *testing.T) {
	bin := buildRiskelle(t, t.TempDir())
	base := map[string]string{
		"prod-tenant/main.tf": tfStorage,
		"sandbox/main.tf":     tfStorage,
		".risk/rules.json":    `{"production_roots":["prod-tenant/**"]}`,
	}
	run := func(t *testing.T, dir string, args ...string) string {
		t.Helper()
		code, out, stderr := runScore(t, bin, dir, append([]string{"--base", "main", "--head", "topic"}, args...)...)
		if code != 0 {
			t.Fatalf("score exit %d: %s", code, stderr)
		}
		return out
	}
	triggers := func(t *testing.T, r report.Report) string {
		t.Helper()
		b, err := json.Marshal(r.Triggers)
		if err != nil {
			t.Fatal(err)
		}
		if (r.Level == score.High) != (len(r.Triggers) > 0) {
			t.Errorf("level %s with triggers %s: want High if and only if a trigger fired", r.Level, b)
		}
		return string(b)
	}

	t.Run("one trigger", func(t *testing.T) {
		dir := rulesFixture(t, base, map[string]string{"prod-tenant/identity.tf": tfRole})
		got := decodeReport(t, run(t, dir))
		want := `[{"class":"identity_rights","paths":["prod-tenant/identity.tf"],"types":["azurerm_role_assignment"],"why":"production_root"}]`
		if got.Level != score.High || got.ShapeLevel != score.Low || triggers(t, got) != want {
			t.Errorf("level %s shape %s triggers %s, want High, Low, %s", got.Level, got.ShapeLevel, triggers(t, got), want)
		}
		md := run(t, dir, "--format", "markdown")
		for _, w := range []string{"### " + report.TriggerHeading + "\n\n" + report.TriggerNote,
			"- identity_rights, High by production_root: paths `prod-tenant/identity.tf`; types `azurerm_role_assignment`"} {
			if !strings.Contains(md, w) {
				t.Errorf("markdown lacks %q:\n%s", w, md)
			}
		}
		if strings.Contains(md, report.NoTriggers) {
			t.Errorf("markdown says none fired:\n%s", md)
		}

		plan := filepath.Join(t.TempDir(), "plan.json")
		if err := os.WriteFile(plan, []byte(`{"target":"main","files":[{"path":"prod-tenant/identity.tf","added":3,"consequences":["identity_rights"]}]}`), 0o644); err != nil {
			t.Fatal(err)
		}
		code, out, stderr := runEstimate(t, bin, dir, plan)
		if code != 0 {
			t.Fatalf("estimate exit %d: %s", code, stderr)
		}
		est := decodeReport(t, out)
		want = `[{"class":"identity_rights","paths":["prod-tenant/identity.tf"],"types":[],"why":"production_root"}]`
		if est.Level != score.High || triggers(t, est) != want {
			t.Errorf("estimate level %s triggers %s, want High, %s", est.Level, triggers(t, est), want)
		}
	})

	t.Run("none", func(t *testing.T) {
		dir := rulesFixture(t, base, map[string]string{"sandbox/identity.tf": tfRole})
		out := run(t, dir)
		got := decodeReport(t, out)
		if got.Level != score.Medium || triggers(t, got) != "[]" || !strings.Contains(out, `"triggers": []`) {
			t.Errorf("level %s, want Medium and \"triggers\": []:\n%s", got.Level, out)
		}
		md := run(t, dir, "--format", "markdown")
		if !strings.Contains(md, "### "+report.TriggerHeading+"\n\n"+report.NoTriggers) || strings.Contains(md, "High by") {
			t.Errorf("markdown, want the heading and that none fired:\n%s", md)
		}
	})
}
