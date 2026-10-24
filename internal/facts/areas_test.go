package facts

import (
	"reflect"
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

		// infrastructure: Terraform, Bicep, k8s, Docker, CI
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
