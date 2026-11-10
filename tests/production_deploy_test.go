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

// prdReleaseYML is a release pipeline whose deployment job deploys to the
// prd environment and runs a script.
const prdReleaseYML = `trigger: none

stages:
- stage: Release
  jobs:
  - deployment: Deploy
    environment: prd
    strategy:
      runOnce:
        deploy:
          steps:
          - checkout: self
          - script: ./scripts/apply.sh
`

// prdWorkflowYML is a GitHub workflow whose job names its environment in the
// mapping form.
const prdWorkflowYML = `on:
  push:
    branches: [main]

jobs:
  deploy:
    runs-on: ubuntu-latest
    environment:
      name: Production
      url: https://example.com
    steps:
    - run: ./deploy.sh
`

// TestProductionDeployFloorsHigh drives the built binary over pipeline
// definitions. A changed line in a definition whose deployment job
// environment is prd (a script line included) carries production_deploy,
// floor High, naming prd, and is High though its shape is Low, with
// production_deploy its trigger; a definition deploying to dev carries only
// pipeline_identity, or nothing for a script line. A prefixed name, a
// mapping-form name and a name read at the merge base carry it too; a
// variable named environment and a job environment product do not; estimate
// with a plan naming the class agrees.
func TestProductionDeployFloorsHigh(t *testing.T) {
	bin := buildRiskelle(t, t.TempDir())
	run := func(t *testing.T, dir string, args ...string) string {
		t.Helper()
		code, out, stderr := runScore(t, bin, dir, append([]string{"--base", "main", "--head", "topic"}, args...)...)
		if code != 0 {
			t.Fatalf("score exit %d: %s", code, stderr)
		}
		return out
	}
	consequences := func(t *testing.T, out, want string) {
		t.Helper()
		if b, _ := json.Marshal(decodeReport(t, out).Consequences); string(b) != want {
			t.Errorf("consequences = %s\nwant %s", b, want)
		}
	}
	high := func(t *testing.T, out string) {
		t.Helper()
		got := decodeReport(t, out)
		if got.Level != score.High || !reflect.DeepEqual(got.FlooredBy, []string{"production_deploy"}) {
			t.Errorf("level %s floored_by %v, want High [production_deploy]", got.Level, got.FlooredBy)
		}
	}
	devYML := strings.Replace(prdReleaseYML, "environment: prd", "environment: dev", 1)
	scriptChanged := func(yml string) string {
		return strings.Replace(yml, "./scripts/apply.sh", "./scripts/apply.sh --plan", 1)
	}

	t.Run("script line in a prd definition", func(t *testing.T) {
		dir := rulesFixture(t, map[string]string{"pipelines/release.yml": prdReleaseYML},
			map[string]string{"pipelines/release.yml": scriptChanged(prdReleaseYML)})
		out := run(t, dir)
		got := decodeReport(t, out)
		n := 0
		for _, p := range got.Breakdown {
			n += p.Points
		}
		if got.Total != n || got.ShapeLevel != score.Low || got.Level != score.High ||
			!reflect.DeepEqual(got.FlooredBy, []string{"production_deploy"}) {
			t.Errorf("total %d (sum %d) shape %s level %s floored_by %v, want Low floored High by production_deploy",
				got.Total, n, got.ShapeLevel, got.Level, got.FlooredBy)
		}
		consequences(t, out, `[{"class":"production_deploy","paths":["pipelines/release.yml"],"types":["prd"],"floor":"High"}]`)
		if b, _ := json.Marshal(got.Triggers); string(b) != `[{"class":"production_deploy","paths":["pipelines/release.yml"],"types":["prd"],"why":"class"}]` {
			t.Errorf("triggers = %s", b)
		}
		md := run(t, dir, "--format", "markdown")
		for _, w := range []string{
			"- production_deploy, floor High: paths `pipelines/release.yml`; types `prd`",
			"- production_deploy, High by class: paths `pipelines/release.yml`; types `prd`",
		} {
			if !strings.Contains(md, w) {
				t.Errorf("markdown lacks %q:\n%s", w, md)
			}
		}
	})

	t.Run("environment line of a dev definition", func(t *testing.T) {
		out := run(t, rulesFixture(t, map[string]string{"pipelines/release.yml": devYML},
			map[string]string{"pipelines/release.yml": strings.Replace(devYML, "environment: dev", "environment: test", 1)}))
		if got := decodeReport(t, out); got.Level != score.Medium || len(got.Triggers) != 0 {
			t.Errorf("level %s triggers %+v, want Medium and none", got.Level, got.Triggers)
		}
		consequences(t, out, `[{"class":"pipeline_identity","paths":["pipelines/release.yml"],"types":["environment"],"floor":"Medium"}]`)
	})

	for _, tt := range []struct{ name, base, topic string }{
		{"script line in a dev definition", devYML, scriptChanged(devYML)},
		{"variable named environment", "trigger: none\njobs:\n- job: Build\n  variables:\n    environment: prd\n  steps:\n  - script: make\n",
			"trigger: none\njobs:\n- job: Build\n  variables:\n    environment: prd\n  steps:\n  - script: make test\n"},
		{"job environment product", strings.Replace(prdReleaseYML, "environment: prd", "environment: product", 1),
			scriptChanged(strings.Replace(prdReleaseYML, "environment: prd", "environment: product", 1))},
	} {
		t.Run(tt.name, func(t *testing.T) {
			out := run(t, rulesFixture(t, map[string]string{"pipelines/release.yml": tt.base}, map[string]string{"pipelines/release.yml": tt.topic}))
			if got := decodeReport(t, out); got.Level != score.Low || !strings.Contains(out, `"consequences": []`) {
				t.Errorf("level %s consequences %+v, want Low and none", got.Level, got.Consequences)
			}
		})
	}

	t.Run("new definition deploying to prd-aue", func(t *testing.T) {
		out := run(t, rulesFixture(t, map[string]string{"README.md": "# fixture\n"},
			map[string]string{"pipelines/release.yml": strings.Replace(prdReleaseYML, "environment: prd", "environment: prd-aue", 1)}))
		high(t, out)
		consequences(t, out, `[{"class":"pipeline_identity","paths":["pipelines/release.yml"],"types":["environment"],"floor":"Medium"},`+
			`{"class":"production_deploy","paths":["pipelines/release.yml"],"types":["prd-aue"],"floor":"High"}]`)
	})

	t.Run("GitHub mapping-form name", func(t *testing.T) {
		out := run(t, rulesFixture(t, map[string]string{".github/workflows/deploy.yml": prdWorkflowYML},
			map[string]string{".github/workflows/deploy.yml": strings.Replace(prdWorkflowYML, "./deploy.sh", "./deploy.sh --all", 1)}))
		high(t, out)
		consequences(t, out, `[{"class":"production_deploy","paths":[".github/workflows/deploy.yml"],"types":["Production"],"floor":"High"}]`)
	})

	t.Run("prd changed to dev", func(t *testing.T) {
		out := run(t, rulesFixture(t, map[string]string{"pipelines/release.yml": prdReleaseYML},
			map[string]string{"pipelines/release.yml": devYML}))
		high(t, out)
		consequences(t, out, `[{"class":"pipeline_identity","paths":["pipelines/release.yml"],"types":["environment"],"floor":"Medium"},`+
			`{"class":"production_deploy","paths":["pipelines/release.yml"],"types":["prd"],"floor":"High"}]`)
	})

	t.Run("estimate with a plan naming the class", func(t *testing.T) {
		dir := rulesFixture(t, map[string]string{"pipelines/release.yml": prdReleaseYML}, map[string]string{"README.md": "# fixture\n"})
		plan := filepath.Join(t.TempDir(), "plan.json")
		if err := os.WriteFile(plan, []byte(`{"target":"main","files":[{"path":"pipelines/release.yml","added":2,"consequences":["production_deploy"]}]}`), 0o644); err != nil {
			t.Fatal(err)
		}
		code, out, stderr := runEstimate(t, bin, dir, plan)
		if code != 0 {
			t.Fatalf("estimate exit %d: %s", code, stderr)
		}
		high(t, out)
		consequences(t, out, `[{"class":"production_deploy","paths":["pipelines/release.yml"],"types":[],"floor":"High"}]`)
	})
}
