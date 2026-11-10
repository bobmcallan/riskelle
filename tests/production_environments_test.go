//go:build integration

package tests

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/bobmcallan/riskelle/internal/report"
	"github.com/bobmcallan/riskelle/internal/score"
)

// liveDeployYML is a release pipeline whose deployment job deploys to the
// live environment, a name riskelle does not build in, and runs a script.
const liveDeployYML = `trigger: none

stages:
- stage: Release
  jobs:
  - deployment: Deploy
    environment: live
    strategy:
      runOnce:
        deploy:
          steps:
          - script: ./deploy.sh
`

// TestProductionEnvironmentsNamedByRepo drives the built binary over a
// definition deploying to live. With live named under production_environments
// in the target branch's .risk/rules.json, a script line changed in it carries
// production_deploy (High) as its trigger; without the name, or with the name
// added by the change itself, it does not. An added name matches a prefixed
// environment, and the built-in names still match beside it. An empty or
// non-string entry is refused by name.
func TestProductionEnvironmentsNamedByRepo(t *testing.T) {
	bin := buildRiskelle(t, t.TempDir())
	run := func(t *testing.T, dir string) report.Report {
		t.Helper()
		code, out, stderr := runScore(t, bin, dir, "--base", "main", "--head", "topic")
		if code != 0 {
			t.Fatalf("score exit %d: %s", code, stderr)
		}
		return decodeReport(t, out)
	}
	deploy := func(env string) string {
		return strings.Replace(liveDeployYML, "environment: live", "environment: "+env, 1)
	}
	changed := func(yml string) string { return strings.Replace(yml, "./deploy.sh", "./deploy.sh --all", 1) }
	fixture := func(rulesJSON, env string) string {
		base := map[string]string{"pipelines/release.yml": deploy(env)}
		if rulesJSON != "" {
			base[".risk/rules.json"] = rulesJSON + "\n"
		}
		return rulesFixture(t, base, map[string]string{"pipelines/release.yml": changed(deploy(env))})
	}
	asJSON := func(v any) string {
		b, _ := json.Marshal(v)
		return string(b)
	}
	deployed := func(t *testing.T, got report.Report, env string) {
		t.Helper()
		if got.ShapeLevel != score.Low || got.Level != score.High || !reflect.DeepEqual(got.FlooredBy, []string{"production_deploy"}) {
			t.Errorf("shape %s level %s floored_by %v, want Low floored High by production_deploy", got.ShapeLevel, got.Level, got.FlooredBy)
		}
		if b, want := asJSON(got.Consequences), `[{"class":"production_deploy","paths":["pipelines/release.yml"],"types":["`+env+`"],"floor":"High"}]`; b != want {
			t.Errorf("consequences = %s\nwant %s", b, want)
		}
		if b, want := asJSON(got.Triggers), `[{"class":"production_deploy","paths":["pipelines/release.yml"],"types":["`+env+`"],"why":"class"}]`; b != want {
			t.Errorf("triggers = %s\nwant %s", b, want)
		}
	}
	none := func(t *testing.T, got report.Report) {
		t.Helper()
		if got.Level != score.Low || len(got.Consequences) != 0 || len(got.Triggers) != 0 {
			t.Errorf("level %s consequences %+v triggers %+v, want Low and none", got.Level, got.Consequences, got.Triggers)
		}
	}

	named := run(t, fixture(`{"production_environments":["Live"]}`, "live"))
	t.Run("named at the target", func(t *testing.T) {
		deployed(t, named, "live")
		if named.Provenance.RulesSource != ".risk/rules.json" {
			t.Errorf("rules source %q, want .risk/rules.json", named.Provenance.RulesSource)
		}
	})

	notNamed := run(t, fixture("", "live"))
	t.Run("not named", func(t *testing.T) {
		none(t, notNamed)
		if notNamed.Provenance.RulesHash == named.Provenance.RulesHash {
			t.Error("naming a production environment did not change the rules hash")
		}
	})

	t.Run("prefix", func(t *testing.T) {
		deployed(t, run(t, fixture(`{"production_environments":["live"]}`, "live-aue")), "live-aue")
	})

	t.Run("built-in names still match", func(t *testing.T) {
		deployed(t, run(t, fixture(`{"production_environments":["live"]}`, "prd")), "prd")
	})

	t.Run("named by the change itself", func(t *testing.T) {
		dir := rulesFixture(t, map[string]string{"pipelines/release.yml": liveDeployYML},
			map[string]string{"pipelines/release.yml": changed(liveDeployYML),
				".risk/rules.json": `{"production_environments":["live"]}` + "\n"})
		got := run(t, dir)
		none(t, got)
		if got.Provenance.RulesSource != "built-in" || got.Provenance.RulesHash != notNamed.Provenance.RulesHash {
			t.Errorf("rules source %q hash %s, want built-in %s", got.Provenance.RulesSource, got.Provenance.RulesHash, notNamed.Provenance.RulesHash)
		}
	})

	for _, tt := range []struct {
		name, rules string
		wants       []string
	}{
		{"empty name refused", `{"production_environments":["live",""]}`, []string{"production_environments[1]: empty name", ".risk/rules.json"}},
		{"non-string refused", `{"production_environments":[5]}`, []string{"production_environments", ".risk/rules.json"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			code, _, stderr := runScore(t, bin, fixture(tt.rules, "live"), "--base", "main", "--head", "topic")
			if code != 1 {
				t.Errorf("exit %d, want 1", code)
			}
			for _, w := range tt.wants {
				if !strings.Contains(stderr, w) {
					t.Errorf("stderr lacks %q: %s", w, stderr)
				}
			}
		})
	}
}
