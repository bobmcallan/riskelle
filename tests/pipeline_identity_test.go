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

// releaseYML models a release pipeline that runs as a deploy identity: a
// deployment job to the dev environment with an Azure CLI step on a service
// connection.
const releaseYML = `trigger: none

pool:
  vmImage: ubuntu-latest

stages:
- stage: Release
  jobs:
  - deployment: DeployGraph
    environment: dev
    strategy:
      runOnce:
        deploy:
          steps:
          - checkout: self
          - task: AzureCLI@2
            displayName: Apply
            inputs:
              azureSubscription: graph-deploy
              scriptType: bash
              scriptLocation: inlineScript
              inlineScript: |
                set -euo pipefail
                ./scripts/apply.sh
`

// buildYML is an Azure pipeline whose list job has a variable named
// environment: not a deployment environment.
const buildYML = `trigger:
- main

jobs:
- job: Build
  variables:
    environment: dev
  steps:
  - script: make
`

// workflowYML is a GitHub workflow with a deployment environment and an OIDC
// token grant.
const workflowYML = `on:
  push:
    branches: [main]

jobs:
  deploy:
    runs-on: ubuntu-latest
    environment: dev
    permissions:
      id-token: write
      contents: read
    steps:
    - uses: actions/checkout@v4
`

const composeYML = `services:
  app:
    image: app
    environment:
      - MODE=dev
`

// TestPipelineIdentityFloorsTheLevel drives the built binary over a change
// modelled on a real one: a release pipeline that names a service connection
// and a deployment environment is Low by shape and reported Medium, or High
// under a production root; a GitHub workflow with an environment and an OIDC
// grant is Medium; removing the connection is Medium; a script change, a
// variable named environment and a compose environment carry nothing;
// estimate with a plan naming the class agrees.
func TestPipelineIdentityFloorsTheLevel(t *testing.T) {
	bin := buildRiskelle(t, t.TempDir())
	run := func(t *testing.T, dir string, args ...string) string {
		t.Helper()
		code, out, stderr := runScore(t, bin, dir, append([]string{"--base", "main", "--head", "topic"}, args...)...)
		if code != 0 {
			t.Fatalf("score exit %d: %s", code, stderr)
		}
		return out
	}
	consequences := func(t *testing.T, r report.Report, want string) {
		t.Helper()
		if b, _ := json.Marshal(r.Consequences); string(b) != want {
			t.Errorf("consequences = %s\nwant %s", b, want)
		}
	}
	base := map[string]string{"README.md": "# fixture\n"}
	release := `[{"class":"pipeline_identity","paths":["pipelines/release.yml"],"types":["azureSubscription","environment"],"floor":"Medium"}]`

	t.Run("824decc", func(t *testing.T) {
		dir := rulesFixture(t, base, map[string]string{"pipelines/release.yml": releaseYML})
		got := decodeReport(t, run(t, dir))
		n := 0
		for _, p := range got.Breakdown {
			n += p.Points
		}
		if got.Total != n || got.Total >= 10 || got.ShapeLevel != score.Low || got.Level != score.Medium ||
			!reflect.DeepEqual(got.FlooredBy, []string{"pipeline_identity"}) {
			t.Errorf("total %d (sum %d) shape %s level %s floored_by %v, want a Low total under 10 floored Medium by pipeline_identity",
				got.Total, n, got.ShapeLevel, got.Level, got.FlooredBy)
		}
		consequences(t, got, release)
		md := run(t, dir, "--format", "markdown")
		for _, w := range []string{"- pipeline_identity, floor Medium: paths `pipelines/release.yml`; types `azureSubscription`, `environment`", report.SplitNote} {
			if !strings.Contains(md, w) {
				t.Errorf("markdown lacks %q:\n%s", w, md)
			}
		}

		for _, tt := range []struct {
			name, consequences string
			level              score.Level
		}{
			{"plan naming the class", `,"consequences":["pipeline_identity"]`, score.Medium},
			{"plan naming none", ``, score.Low},
		} {
			plan := filepath.Join(t.TempDir(), "plan.json")
			os.WriteFile(plan, []byte(`{"target":"main","files":[{"path":"pipelines/release.yml","added":40`+tt.consequences+`}]}`), 0o644)
			code, out, stderr := runEstimate(t, bin, dir, plan)
			if code != 0 {
				t.Fatalf("%s: estimate exit %d: %s", tt.name, code, stderr)
			}
			est := decodeReport(t, out)
			if est.Level != tt.level {
				t.Errorf("%s: estimate level %s, want %s", tt.name, est.Level, tt.level)
			}
			if tt.level == score.Medium && (!reflect.DeepEqual(est.FlooredBy, []string{"pipeline_identity"}) ||
				!strings.Contains(out, `"types": []`)) {
				t.Errorf("%s: estimate floored_by %v:\n%s", tt.name, est.FlooredBy, out)
			}
		}
	})

	t.Run("under a production root", func(t *testing.T) {
		b := map[string]string{"README.md": "# fixture\n", ".risk/rules.json": `{"production_roots":["pipelines/**"]}`}
		got := decodeReport(t, run(t, rulesFixture(t, b, map[string]string{"pipelines/release.yml": releaseYML})))
		if got.Level != score.High {
			t.Errorf("level %s, want High", got.Level)
		}
		consequences(t, got, `[{"class":"pipeline_identity","paths":["pipelines/release.yml"],"types":["azureSubscription","environment"],`+
			`"floor":"High","production_roots":["pipelines/**"]}]`)
	})

	t.Run("GitHub workflow", func(t *testing.T) {
		got := decodeReport(t, run(t, rulesFixture(t, base, map[string]string{".github/workflows/deploy.yml": workflowYML})))
		if got.Level != score.Medium {
			t.Errorf("level %s, want Medium", got.Level)
		}
		consequences(t, got, `[{"class":"pipeline_identity","paths":[".github/workflows/deploy.yml"],"types":["environment","id-token: write"],"floor":"Medium"}]`)
	})

	withRelease := map[string]string{"pipelines/release.yml": releaseYML, "pipelines/build.yml": buildYML,
		"docker-compose.yml": composeYML, "pipelines/docker-compose.test.yml": composeYML}

	t.Run("removed service connection", func(t *testing.T) {
		removed := strings.Replace(releaseYML, "              azureSubscription: graph-deploy\n", "", 1)
		got := decodeReport(t, run(t, rulesFixture(t, withRelease, map[string]string{"pipelines/release.yml": removed})))
		if got.Level != score.Medium {
			t.Errorf("level %s, want Medium", got.Level)
		}
		consequences(t, got, `[{"class":"pipeline_identity","paths":["pipelines/release.yml"],"types":["azureSubscription"],"floor":"Medium"}]`)
	})

	t.Run("names neither", func(t *testing.T) {
		out := run(t, rulesFixture(t, withRelease, map[string]string{
			"pipelines/release.yml":             strings.Replace(releaseYML, "./scripts/apply.sh", "./scripts/apply.sh --plan", 1),
			"pipelines/build.yml":               strings.Replace(buildYML, "environment: dev", "environment: test", 1),
			"docker-compose.yml":                strings.Replace(composeYML, "MODE=dev", "MODE=test", 1),
			"pipelines/docker-compose.test.yml": strings.Replace(composeYML, "MODE=dev", "MODE=ci", 1),
		}))
		got := decodeReport(t, out)
		if got.Level != score.Low || !strings.Contains(out, `"consequences": []`) || !strings.Contains(out, `"floored_by": []`) {
			t.Errorf("level %s consequences %+v, want Low and none:\n%s", got.Level, got.Consequences, out)
		}
	})
}

// terraformReleaseYML models a shared Azure Pipelines release template: its
// deployment job's environment and its service connection are parameters.
const terraformReleaseYML = `parameters:
- name: environment
  type: string
- name: serviceConnection
  type: string

stages:
- stage: Release
  jobs:
  - deployment: TerraformApply
    environment: ${{ parameters.environment }}
    strategy:
      runOnce:
        deploy:
          steps:
          - checkout: self
          - task: AzureCLI@2
            displayName: Terraform apply
            inputs:
              azureSubscription: ${{ parameters.serviceConnection }}
              scriptType: bash
              scriptLocation: inlineScript
              inlineScript: |
                set -euo pipefail
                terraform apply -auto-approve tfplan
`

// prReviewYML is a step template that names no connection.
const prReviewYML = `parameters:
- name: target
  type: string

steps:
- script: |
    ./review.sh ${{ parameters.target }}
  displayName: Review
`

// helmDeploymentYAML is a Helm chart template with an environment label and
// an ENVIRONMENT variable.
const helmDeploymentYAML = `{{- if .Values.enabled }}
apiVersion: apps/v1
kind: Deployment
metadata:
  labels:
    environment: dev
spec:
  template:
    spec:
      containers:
      - name: app
        env:
        - name: ENVIRONMENT
          value: dev
{{- end }}
`

// TestPipelineTemplatesFloorTheLevel drives the built binary over a change
// modelled on a real one: a shared release template under templates/ that
// names its deployment job's environment and its service connection is Low
// by shape and reported Medium, or High under a production root; a Helm
// chart template and a template's unmarked lines carry nothing.
func TestPipelineTemplatesFloorTheLevel(t *testing.T) {
	bin := buildRiskelle(t, t.TempDir())
	run := func(t *testing.T, dir string, args ...string) string {
		t.Helper()
		code, out, stderr := runScore(t, bin, dir, append([]string{"--base", "main", "--head", "topic"}, args...)...)
		if code != 0 {
			t.Fatalf("score exit %d: %s", code, stderr)
		}
		return out
	}
	release := map[string]string{"templates/terraform-release.yml": terraformReleaseYML}

	t.Run("terraform-release", func(t *testing.T) {
		dir := rulesFixture(t, map[string]string{"README.md": "# fixture\n"}, release)
		got := decodeReport(t, run(t, dir))
		n := 0
		for _, p := range got.Breakdown {
			n += p.Points
		}
		if got.Total != n || got.ShapeLevel != score.Low || got.Level != score.Medium ||
			!reflect.DeepEqual(got.FlooredBy, []string{"pipeline_identity"}) {
			t.Errorf("total %d (sum %d) shape %s level %s floored_by %v, want Low floored Medium by pipeline_identity",
				got.Total, n, got.ShapeLevel, got.Level, got.FlooredBy)
		}
		want := `[{"class":"pipeline_identity","paths":["templates/terraform-release.yml"],"types":["azureSubscription","environment"],"floor":"Medium"}]`
		if b, _ := json.Marshal(got.Consequences); string(b) != want {
			t.Errorf("consequences = %s\nwant %s", b, want)
		}
		if md := run(t, dir, "--format", "markdown"); !strings.Contains(md,
			"- pipeline_identity, floor Medium: paths `templates/terraform-release.yml`; types `azureSubscription`, `environment`") {
			t.Errorf("markdown lacks the consequence:\n%s", md)
		}
	})

	t.Run("under a production root", func(t *testing.T) {
		base := map[string]string{"README.md": "# fixture\n", ".risk/rules.json": `{"production_roots":["templates/**"]}`}
		got := decodeReport(t, run(t, rulesFixture(t, base, release)))
		want := `[{"class":"pipeline_identity","paths":["templates/terraform-release.yml"],"types":["azureSubscription","environment"],` +
			`"floor":"High","production_roots":["templates/**"]}]`
		if b, _ := json.Marshal(got.Consequences); got.Level != score.High || string(b) != want {
			t.Errorf("level %s consequences = %s\nwant High %s", got.Level, b, want)
		}
	})

	t.Run("Helm chart and a step template", func(t *testing.T) {
		base := map[string]string{"charts/app/templates/deployment.yaml": helmDeploymentYAML,
			"templates/pr-review.yml": prReviewYML, "templates/terraform-release.yml": terraformReleaseYML}
		out := run(t, rulesFixture(t, base, map[string]string{
			"charts/app/templates/deployment.yaml": strings.ReplaceAll(helmDeploymentYAML, "dev", "prod"),
			"templates/pr-review.yml":              strings.Replace(prReviewYML, "./review.sh", "./review.sh --strict", 1),
			"templates/terraform-release.yml":      strings.Replace(terraformReleaseYML, "-auto-approve tfplan", "-auto-approve -input=false tfplan", 1),
		}))
		got := decodeReport(t, out)
		if got.Level != score.Low || !strings.Contains(out, `"consequences": []`) || !strings.Contains(out, `"floored_by": []`) {
			t.Errorf("level %s consequences %+v, want Low and none:\n%s", got.Level, got.Consequences, out)
		}
	})
}

// releaseRefYML models a release pipeline that extends a template from a
// repository resource pinned by ref, passing it a service connection.
const releaseRefYML = `trigger: none

resources:
  repositories:
  - repository: templates
    type: git
    name: platform/pipelines
    ref: refs/tags/v1.4.0

extends:
  template: stages/terraform-release.yml@templates
  parameters:
    serviceConnection: graph-deploy
    environment: prod
    displayName: Release graph
`

// TestPipelineReferencesFloorTheLevel drives the built binary over a change
// modelled on a real one: moving the template repository's ref in a pipeline
// that runs as an identity is Low by shape and reported Medium, or High under
// a production root, and so is changing its extends template; the same move in
// a pipeline that runs as no identity, or a displayName change, carries none.
func TestPipelineReferencesFloorTheLevel(t *testing.T) {
	bin := buildRiskelle(t, t.TempDir())
	run := func(t *testing.T, dir string, args ...string) string {
		t.Helper()
		code, out, stderr := runScore(t, bin, dir, append([]string{"--base", "main", "--head", "topic"}, args...)...)
		if code != 0 {
			t.Fatalf("score exit %d: %s", code, stderr)
		}
		return out
	}
	base := map[string]string{"pipelines/release.yml": releaseRefYML}
	moved := strings.Replace(releaseRefYML, "refs/tags/v1.4.0", "refs/heads/main", 1)

	t.Run("31885b0", func(t *testing.T) {
		dir := rulesFixture(t, base, map[string]string{"pipelines/release.yml": moved})
		got := decodeReport(t, run(t, dir))
		n := 0
		for _, p := range got.Breakdown {
			n += p.Points
		}
		if got.Total != n || got.ShapeLevel != score.Low || got.Level != score.Medium ||
			!reflect.DeepEqual(got.FlooredBy, []string{"pipeline_identity"}) {
			t.Errorf("total %d (sum %d) shape %s level %s floored_by %v, want Low floored Medium by pipeline_identity",
				got.Total, n, got.ShapeLevel, got.Level, got.FlooredBy)
		}
		want := `[{"class":"pipeline_identity","paths":["pipelines/release.yml"],"types":["resources.repositories.ref"],"floor":"Medium"}]`
		if b, _ := json.Marshal(got.Consequences); string(b) != want {
			t.Errorf("consequences = %s\nwant %s", b, want)
		}
		if md := run(t, dir, "--format", "markdown"); !strings.Contains(md,
			"- pipeline_identity, floor Medium: paths `pipelines/release.yml`; types `resources.repositories.ref`") {
			t.Errorf("markdown lacks the consequence:\n%s", md)
		}
	})

	t.Run("extends template", func(t *testing.T) {
		alt := strings.Replace(releaseRefYML, "terraform-release.yml@templates", "terraform-release-v2.yml@templates", 1)
		got := decodeReport(t, run(t, rulesFixture(t, base, map[string]string{"pipelines/release.yml": alt})))
		want := `[{"class":"pipeline_identity","paths":["pipelines/release.yml"],"types":["extends.template"],"floor":"Medium"}]`
		if b, _ := json.Marshal(got.Consequences); got.Level != score.Medium || string(b) != want {
			t.Errorf("level %s consequences = %s\nwant Medium %s", got.Level, b, want)
		}
	})

	t.Run("under a production root", func(t *testing.T) {
		b := map[string]string{"pipelines/release.yml": releaseRefYML, ".risk/rules.json": `{"production_roots":["pipelines/**"]}`}
		got := decodeReport(t, run(t, rulesFixture(t, b, map[string]string{"pipelines/release.yml": moved})))
		want := `[{"class":"pipeline_identity","paths":["pipelines/release.yml"],"types":["resources.repositories.ref"],` +
			`"floor":"High","production_roots":["pipelines/**"]}]`
		if b, _ := json.Marshal(got.Consequences); got.Level != score.High || string(b) != want {
			t.Errorf("level %s consequences = %s\nwant High %s", got.Level, b, want)
		}
	})

	for _, tt := range []struct{ name, base, topic string }{
		{"no identity", strings.Replace(releaseRefYML, "serviceConnection: graph-deploy", "target: graph", 1),
			strings.Replace(strings.Replace(releaseRefYML, "serviceConnection: graph-deploy", "target: graph", 1), "refs/tags/v1.4.0", "refs/heads/main", 1)},
		{"unmarked lines", releaseRefYML, strings.Replace(releaseRefYML, "Release graph", "Release Graph API", 1)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			out := run(t, rulesFixture(t, map[string]string{"pipelines/release.yml": tt.base}, map[string]string{"pipelines/release.yml": tt.topic}))
			if got := decodeReport(t, out); got.Level != score.Low || !strings.Contains(out, `"consequences": []`) || !strings.Contains(out, `"floored_by": []`) {
				t.Errorf("level %s consequences %+v, want Low and none", got.Level, got.Consequences)
			}
		})
	}
}
