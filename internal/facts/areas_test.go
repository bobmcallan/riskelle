package facts

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestAreasBuiltin(t *testing.T) {
	tests := []struct {
		path string
		want []string
	}{
		// auth: Go, JavaScript and .NET layouts
		{"internal/auth/login.go", []string{AreaAuth}},
		{"pkg/oauth/client.go", []string{AreaAuth}},
		{"src/Api/Authentication/Handler.cs", []string{AreaAuth}},
		{"src/Api/AuthController.cs", []string{AreaAuth}},
		{"web/src/auth.ts", []string{AreaAuth}},
		{"web/src/jwt.js", []string{AreaAuth}},
		{"src/App/Services/TokenService.cs", []string{AreaAuth}},
		{"src/identity/user.go", []string{AreaAuth}},

		// migrations
		{"db/migrations/0001_init.up.sql", []string{AreaMigrations}},
		{"src/App/Migrations/20240101_Init.cs", []string{AreaMigrations}},
		{"server/migrate/run.go", []string{AreaMigrations}},
		{"schema/seed.sql", []string{AreaMigrations}},

		// infrastructure: Terraform, Bicep, k8s, Docker, CI and pipeline YAML
		{"pipelines/release.yml", []string{AreaInfrastructure}},
		{"pipelines/stages/deploy.yaml", []string{AreaInfrastructure}},
		{".pipelines/build.yml", []string{AreaInfrastructure}},
		{".circleci/config.yml", []string{AreaInfrastructure}},
		{".buildkite/pipeline.yml", []string{AreaInfrastructure}},
		{"infra/main.tf", []string{AreaInfrastructure}},
		{"env/prod.tfvars", []string{AreaInfrastructure}},
		{"deploy/main.bicep", []string{AreaInfrastructure}},
		{"k8s/deployment.yaml", []string{AreaInfrastructure}},
		{"charts/helm/values.yaml", []string{AreaInfrastructure}},
		{"Dockerfile", []string{AreaInfrastructure}},
		{"deploy/Dockerfile.prod", []string{AreaInfrastructure}},
		{"docker-compose.override.yml", []string{AreaInfrastructure}},
		{".github/workflows/ci.yml", []string{AreaInfrastructure}},
		{"azure-pipelines.yml", []string{AreaInfrastructure}},
		{".azuredevops/pipelines/build.yaml", []string{AreaInfrastructure}},

		// secrets
		{"config/server.pem", []string{AreaSecrets}},
		{"certs/site.pfx", []string{AreaSecrets}},
		{".env", []string{AreaSecrets}},
		{"web/.env.production", []string{AreaSecrets}},
		{"internal/secrets/vault.go", []string{AreaSecrets}},
		{"src/App/SecretStore.cs", []string{AreaSecrets}},
		{"src/App/Encryptor.cs", []string{AreaSecrets}},
		{"internal/crypto/aes.go", []string{AreaSecrets}},

		// one path in two areas, sorted
		{"infra/secrets.tf", []string{AreaInfrastructure, AreaSecrets}},

		// none
		{"config/app.yml", nil},
		{"src/pipelines/etl.yaml", nil},
		{"docs/pipelines.yml", nil},
		{"pipelines/README.md", nil},
		{"src/app.go", nil},
		{"internal/author/name.go", nil},
		{"docs/guide.md", nil},
		{"web/src/components/Button.tsx", nil},
		{"go.mod", nil},
	}
	for _, tt := range tests {
		if got := Areas(tt.path); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Areas(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestBuiltinAreaNames(t *testing.T) {
	var got []string
	for _, a := range builtinAreas {
		got = append(got, a.name)
	}
	want := []string{AreaAuth, AreaInfrastructure, AreaMigrations, AreaSecrets}
	if !reflect.DeepEqual(got, want) || want[0] != "auth" || want[1] != "infrastructure" || want[2] != "migrations" || want[3] != "secrets" {
		t.Errorf("built-in areas = %v, want %v in that order", got, want)
	}
}

// TestReadmeListsAreas pins that the README's Sensitive areas table names, in
// each area's own row, every built-in pattern of that area as a code span, so
// the two cannot drift.
func TestReadmeListsAreas(t *testing.T) {
	readme, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	s := string(readme)
	i := strings.Index(s, "\n### Sensitive areas\n")
	if i < 0 {
		t.Fatal("README has no ### Sensitive areas section")
	}
	s = s[i+1:]
	if j := strings.Index(s, "\n## "); j >= 0 {
		s = s[:j]
	}
	rows := map[string]string{}
	for _, line := range strings.Split(s, "\n") {
		if name, _, ok := strings.Cut(strings.TrimPrefix(line, "| "), " |"); ok && strings.HasPrefix(line, "| ") {
			rows[name] = line
		}
	}
	for _, a := range builtinAreas {
		row, ok := rows[a.name]
		if !ok {
			t.Errorf("README has no %s row", a.name)
			continue
		}
		for _, p := range a.patterns {
			if !strings.Contains(row, "`"+p+"`") {
				t.Errorf("README's %s row does not list area pattern `%s`", a.name, p)
			}
		}
	}
}
