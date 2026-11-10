package facts

import (
	"context"
	"fmt"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

// marked returns the 1-based line numbers pipelineIdentity marks, each with
// its mark, as "n:mark".
func marked(content string) []string {
	var out []string
	marks, _ := pipelineIdentity([]byte(content))
	for i, m := range marks {
		if m != "" {
			out = append(out, fmt.Sprintf("%02d:%s", i+1, m))
		}
	}
	return out
}

func TestPipelineIdentity(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []string
	}{
		// AC1: Azure
		{"azure deployment environment", "jobs:\n- deployment: Deploy\n  environment: prod\n  strategy:\n    runOnce: {}\n",
			[]string{"03:environment"}},
		{"azure deployment indented", "stages:\n- stage: A\n  jobs:\n  - deployment: D\n    environment: prod\n",
			[]string{"05:environment"}},
		{"azure deployment environment after variables", "jobs:\n- deployment: D\n  variables:\n    x: 1\n  environment: prod\n",
			[]string{"05:environment"}},
		{"azure environment mapping", "jobs:\n- deployment: D\n  environment:\n    name: prod\n    resourceType: VirtualMachine\n  pool: x\n",
			[]string{"03:environment", "04:environment", "05:environment"}},
		{"azure cli task", "steps:\n- task: AzureCLI@2\n  inputs:\n    azureSubscription: graph-deploy\n    scriptType: bash\n",
			[]string{"04:azureSubscription"}},
		{"ARM connection", "steps:\n- task: X@1\n  inputs:\n    ConnectedServiceNameARM: sc\n", []string{"04:ConnectedServiceNameARM"}},
		{"kubernetes connection", "steps:\n- task: Kubernetes@1\n  inputs:\n    kubernetesServiceConnection: k\n", []string{"04:kubernetesServiceConnection"}},
		{"connection variable", "variables:\n  azureServiceConnection: prod-sc\n", []string{"02:azureServiceConnection"}},
		// AC1: GitHub
		{"github environment", "on: push\njobs:\n  deploy:\n    runs-on: ubuntu-latest\n    environment: prod\n", []string{"05:environment"}},
		{"github environment mapping", "on: push\njobs:\n  deploy:\n    environment:\n      name: prod\n      url: x\n",
			[]string{"04:environment", "05:environment", "06:environment"}},
		{"github second job", "on: push\njobs:\n  build:\n    runs-on: x\n  deploy:\n    environment: prod\n", []string{"06:environment"}},
		{"id-token write", "on: push\npermissions:\n  id-token: write\n  contents: read\n", []string{"03:id-token: write"}},
		{"id-token quoted with comment", "on: push\npermissions:\n  'id-token': \"write\" # oidc\n", []string{"03:id-token: write"}},
		{"write-all", "on: push\npermissions: write-all\n", []string{"02:permissions: write-all"}},
		{"crlf", "jobs:\r\n- deployment: D\r\n  environment: prod\r\n", []string{"03:environment"}},
		// Azure template
		{"azure template", "parameters:\n- name: environment\n  type: string\n- name: serviceConnection\n  type: string\n" +
			"stages:\n- stage: Release\n  jobs:\n  - deployment: Apply\n    environment: ${{ parameters.environment }}\n" +
			"    strategy:\n      runOnce:\n        deploy:\n          steps:\n          - task: AzureCLI@2\n            inputs:\n" +
			"              azureSubscription: ${{ parameters.serviceConnection }}\n",
			[]string{"10:environment", "17:azureSubscription"}},
		// Helm: unsigned, or signed with no catalogued key
		{"helm deployment", "{{- if .Values.enabled }}\napiVersion: apps/v1\nkind: Deployment\nmetadata:\n  labels:\n" +
			"    environment: {{ .Values.environment }}\nspec:\n  template:\n    spec:\n      containers:\n      - name: app\n" +
			"        env:\n        - name: ENVIRONMENT\n          value: {{ .Values.environment }}\n{{- end }}\n", nil},
		{"helm storage class", "apiVersion: storage.k8s.io/v1\nkind: StorageClass\nmetadata:\n  labels:\n    environment: prod\n" +
			"parameters:\n  type: gp3\n", nil},
		// references, in a marked definition
		{"repository ref in a marked pipeline", "resources:\n  repositories:\n  - repository: templates\n    type: git\n" +
			"    name: org/pipelines\n    ref: refs/heads/main\nextends:\n  template: stages/release.yml@templates\n" +
			"  parameters:\n    serviceConnection: graph-deploy\n",
			[]string{"06:resources.repositories.ref", "08:extends.template", "10:serviceConnection"}},
		{"ref is the item's first key", "resources:\n  repositories:\n  - ref: x\n    repository: t\n" +
			"steps:\n- task: AzureCLI@2\n  inputs:\n    azureSubscription: sc\n",
			[]string{"03:resources.repositories.ref", "08:azureSubscription"}},
		{"stage, job and step templates", "stages:\n- template: s.yml@templates\n- stage: B\n  jobs:\n  - template: j.yml\n" +
			"  - deployment: D\n    environment: prod\n    strategy:\n      runOnce:\n        deploy:\n          steps:\n" +
			"          - template: t.yml\n  - job: E\n    steps:\n    - template: u.yml\n",
			[]string{"02:stages.template", "05:jobs.template", "07:environment", "12:steps.template", "15:steps.template"}},
		{"references in an unmarked pipeline", "resources:\n  repositories:\n  - repository: templates\n    ref: refs/heads/main\n" +
			"extends:\n  template: stages/release.yml@templates\n  parameters:\n    target: x\n", nil},
		{"references elsewhere", "variables:\n  ref: x\nsteps:\n- task: X@1\n  inputs:\n    template: y\n    azureSubscription: sc\n" +
			"extends:\n  parameters:\n    template: z\n",
			[]string{"07:azureSubscription"}},
		{"variables template", "variables:\n- template: vars.yml\nsteps:\n- task: X@1\n  inputs:\n    azureSubscription: sc\n",
			[]string{"06:azureSubscription"}},
		{"displayName and script in a marked pipeline", "steps:\n- template: t.yml\n- script: |\n    template: no\n" +
			"  displayName: Build\n- task: X@1\n  inputs:\n    azureSubscription: sc\n",
			[]string{"02:steps.template", "08:azureSubscription"}},
		// AC2: no marks
		{"docker-compose", "services:\n  app:\n    environment:\n      - FOO=bar\n", nil},
		{"variables environment", "variables:\n  environment: dev\n", nil},
		{"github env environment", "on: push\njobs:\n  build:\n    env:\n      environment: x\n", nil},
		{"azure list job variable", "jobs:\n- job: Build\n  variables:\n    environment: dev\n", nil},
		{"azure list job variable indented", "jobs:\n  - job: Build\n    variables:\n      environment: dev\n", nil},
		{"deployment variable and environment", "jobs:\n- deployment: Deploy\n  variables:\n    environment: dev\n  environment: prod\n",
			[]string{"05:environment"}},
		{"plain job environment", "jobs:\n- job: Build\n  environment: prod\n", nil},
		{"id-token read", "on: push\npermissions:\n  id-token: read\n", nil},
		{"script body", "steps:\n- script: |\n    echo \"environment: prod\"\n    azureSubscription: x\n- bash: echo\n", nil},
		{"commented key", "steps:\n- task: AzureCLI@2\n  inputs:\n    # azureSubscription: x\n    scriptType: bash\n", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := marked(tt.content); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("marks = %v, want %v", got, tt.want)
			}
		})
	}
	if got, envs := pipelineIdentity([]byte("services:\n  app:\n    environment: x\n")); got != nil || envs != nil {
		t.Errorf("unsigned content = %v, %v, want nil", got, envs)
	}
	if got, _ := pipelineIdentity([]byte("jobs:\n- deployment: D\n  environment: prod\n")); len(got) != 4 {
		t.Errorf("one mark per line: %d for 4 lines", len(got))
	}
}

func TestIsPipeline(t *testing.T) {
	for p, want := range map[string]bool{
		".github/workflows/deploy.yml":         true,
		".github/workflows/ci.yaml":            true,
		"azure-pipelines.yml":                  true,
		"build/azure-pipelines-ci.yaml":        true,
		".azuredevops/release.yml":             true,
		"pipelines/release.yml":                true,
		"infra/pipelines/stages/deploy.yml":    true,
		".pipelines/build.yaml":                true,
		"azure-pipelines/templates/a.yml":      true,
		"docker-compose.yml":                   false,
		"k8s/deploy.yaml":                      false,
		".github/actions/x/action.yml":         false,
		".github/workflows/sub/x.yml":          false,
		"pipelines/README.md":                  false,
		"templates/terraform-release.yml":      true,
		"templates/pr-review.yaml":             true,
		"build/templates/stages/deploy.yml":    true,
		"charts/app/templates/deployment.yaml": true,
		"templates/README.md":                  false,
		"templates/app.json":                   false,
	} {
		if got := isPipeline(p); got != want {
			t.Errorf("isPipeline(%q) = %v, want %v", p, got, want)
		}
	}
}

// TestPipelineEnvironments pins the job environments pipelineIdentity names:
// a job environment's scalar value or its mapping's name:, as written and in
// order; none for an environment that is not a job's, a script body or
// unsigned content.
func TestPipelineEnvironments(t *testing.T) {
	for _, tt := range []struct {
		name, content string
		want          []string
	}{
		{"azure scalar", "jobs:\n- deployment: D\n  environment: prd\n", []string{"prd"}},
		{"quoted", "jobs:\n- deployment: D\n  environment: 'PRD'\n", []string{"PRD"}},
		{"trailing comment", "jobs:\n- deployment: D\n  environment: prod # live\n", []string{"prod"}},
		{"azure mapping", "jobs:\n- deployment: D\n  environment:\n    name: prd-aue\n    resourceType: VirtualMachine\n", []string{"prd-aue"}},
		{"github scalar", "on: push\njobs:\n  deploy:\n    environment: production\n", []string{"production"}},
		{"github mapping", "on: push\njobs:\n  deploy:\n    environment:\n      name: Production\n      url: x\n", []string{"Production"}},
		{"templated", "jobs:\n- deployment: D\n  environment: ${{ parameters.environment }}\n", []string{"${{ parameters.environment }}"}},
		{"two jobs", "jobs:\n- deployment: A\n  environment: dev\n- deployment: B\n  environment: prd\n", []string{"dev", "prd"}},
		{"variables environment", "variables:\n  environment: prd\n", nil},
		{"plain job", "jobs:\n- job: Build\n  environment: prd\n", nil},
		{"build job variable", "jobs:\n- job: Build\n  variables:\n    environment: prd\n", nil},
		{"name not under an environment", "jobs:\n- deployment: D\n  pool:\n    name: prd\n  environment: dev\n", []string{"dev"}},
		{"script body", "steps:\n- script: |\n    environment: prd\n", nil},
		{"unsigned", "services:\n  app:\n    environment: prd\n", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, got := pipelineIdentity([]byte(tt.content)); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("environments = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestIsProduction pins the production name rule: a name, ignoring case, or
// a name followed by a non-letter; never a longer word, a suffix, an empty
// or templated value, or anything with no names.
func TestIsProduction(t *testing.T) {
	builtin := []string{"prd", "prod", "production"}
	for _, tt := range []struct {
		env   string
		names []string
		want  bool
	}{
		{"prd", builtin, true},
		{"PRD", builtin, true},
		{"Prod", builtin, true},
		{"production", builtin, true},
		{"prd-aue", builtin, true},
		{"prod_eu", builtin, true},
		{"production.web", builtin, true},
		{"prd1", builtin, true},
		{"dev", builtin, false},
		{"test", builtin, false},
		{"product", builtin, false},
		{"prdx", builtin, false},
		{"pre-prod", builtin, false},
		{"nonprod", builtin, false},
		{"aue-prd", builtin, false},
		{"", builtin, false},
		{"${{ parameters.environment }}", builtin, false},
		{"prd", nil, false},
		{"-x", []string{""}, false},
		{"live", append(builtin, "live"), true},
		{"live-aue", append(builtin, "live"), true},
		{"lively", append(builtin, "live"), false},
	} {
		if got := IsProduction(tt.env, tt.names); got != tt.want {
			t.Errorf("IsProduction(%q, %q) = %v, want %v", tt.env, tt.names, got, tt.want)
		}
	}
}

// TestCollectEnvironments drives Collect over a fixture repository: a
// changed definition records the job environments it names at each side it
// changed on; an unchanged definition and a non-pipeline file record none.
func TestCollectEnvironments(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	const release = "trigger: none\njobs:\n- deployment: Deploy\n  environment: prd\n  strategy:\n    runOnce:\n" +
		"      deploy:\n        steps:\n        - script: ./apply.sh\n"
	const dev = "trigger: none\njobs:\n- deployment: Deploy\n  environment: dev\n  steps:\n  - script: make\n"
	const compose = "services:\n  app:\n    environment:\n      - MODE=prd\n"
	for _, tt := range []struct {
		name  string
		topic map[string]string
		want  map[string][]string
	}{
		{"script line in a prd definition",
			map[string]string{"pipelines/release.yml": strings.Replace(release, "./apply.sh", "./apply.sh --plan", 1)},
			map[string][]string{"pipelines/release.yml": {"prd"}}},
		{"prd changed to dev",
			map[string]string{"pipelines/release.yml": strings.Replace(release, "environment: prd", "environment: dev", 1)},
			map[string][]string{"pipelines/release.yml": {"dev", "prd"}}},
		{"line added to a dev definition",
			map[string]string{"pipelines/dev.yml": dev + "  - script: make test\n"},
			map[string][]string{"pipelines/dev.yml": {"dev"}}},
		{"compose file", map[string]string{"docker-compose.yml": strings.Replace(compose, "MODE=prd", "MODE=prod", 1)},
			map[string][]string{"docker-compose.yml": nil}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			runGit(t, dir, "init", "--quiet", "--initial-branch=main")
			write(t, dir, "pipelines/release.yml", release)
			write(t, dir, "pipelines/dev.yml", dev)
			write(t, dir, "docker-compose.yml", compose)
			write(t, dir, "README.md", "# x\n")
			runGit(t, dir, "add", ".")
			runGit(t, dir, "commit", "--quiet", "-m", "base")
			runGit(t, dir, "switch", "--quiet", "-c", "feature")
			for p, c := range tt.topic {
				write(t, dir, p, c)
			}
			write(t, dir, "README.md", "# y\n")
			runGit(t, dir, "commit", "--quiet", "-am", "feature")
			got, err := Collect(context.Background(), dir, "main", "feature")
			if err != nil {
				t.Fatalf("Collect: %v", err)
			}
			for _, f := range got.Files {
				if want := tt.want[f.Path]; !reflect.DeepEqual(f.Environments, want) {
					t.Errorf("%s: environments = %q, want %q", f.Path, f.Environments, want)
				}
			}
			if len(got.Files) != len(tt.want)+1 {
				t.Errorf("files = %+v, want %d changed", got.Files, len(tt.want)+1)
			}
		})
	}
}
