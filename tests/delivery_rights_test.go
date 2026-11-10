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

// deliveryTF models a change that wires a pipeline to a deploy identity: a
// workload identity federation service connection, its pipeline
// authorisation, the production environment and its checks.
const deliveryTF = `resource "azuredevops_serviceendpoint_azurerm" "deploy" {
  project_id                             = azuredevops_project.p.id
  service_endpoint_name                  = "deploy"
  service_endpoint_authentication_scheme = "WorkloadIdentityFederation"
  credentials {
    serviceprincipalid = "00000000-0000-0000-0000-000000000000"
  }
  azurerm_spn_tenantid      = "00000000-0000-0000-0000-000000000001"
  azurerm_subscription_id   = "00000000-0000-0000-0000-000000000002"
  azurerm_subscription_name = "prod"
}

resource "azuredevops_pipeline_authorization" "deploy" {
  project_id  = azuredevops_project.p.id
  resource_id = azuredevops_serviceendpoint_azurerm.deploy.id
  type        = "endpoint"
}

resource "azuredevops_environment" "prod" {
  project_id = azuredevops_project.p.id
  name       = "prod"
}

resource "azuredevops_check_approval" "prod" {
  project_id           = azuredevops_project.p.id
  target_resource_id   = azuredevops_environment.prod.id
  target_resource_type = "environment"
}

resource "azuredevops_check_exclusive_lock" "prod" {
  project_id           = azuredevops_project.p.id
  target_resource_id   = azuredevops_environment.prod.id
  target_resource_type = "environment"
}

resource "azuredevops_check_required_template" "prod" {
  project_id           = azuredevops_project.p.id
  target_resource_id   = azuredevops_environment.prod.id
  target_resource_type = "environment"
  required_template {
    repository_name = "templates"
    repository_ref  = "refs/heads/main"
    template_path   = "release.yml"
  }
}

resource "azuredevops_check_branch_control" "prod" {
  project_id           = azuredevops_project.p.id
  target_resource_id   = azuredevops_environment.prod.id
  target_resource_type = "environment"
  allowed_branches     = "refs/heads/main"
}
`

// deliveryRepoTF is an uncatalogued Azure DevOps resource.
const deliveryRepoTF = `resource "azuredevops_git_repository" "app" {
  project_id = azuredevops_project.p.id
  name       = "app"
}
`

// githubTF holds GitHub delivery rights.
const githubTF = `resource "github_repository_environment" "prod" {
  repository  = "app"
  environment = "prod"
}

resource "github_branch_protection" "main" {
  repository_id = "app"
  pattern       = "main"
}

resource "github_repository_ruleset" "main" {
  repository  = "app"
  name        = "main"
  target      = "branch"
  enforcement = "active"
}

resource "github_actions_repository_oidc_subject_claim_customization_template" "app" {
  repository         = "app"
  use_default        = false
  include_claim_keys = ["repo", "environment"]
}
`

// TestDeliveryRightsFloorTheLevel drives the built binary over changes
// modelled on real ones: wiring a pipeline to a deploy identity (a service
// connection, its authorisation, an environment and its checks) is Low by
// shape and reported High, as access_widened on the service connection alone:
// the authorisation, the environment and the checks are delivery gates, which
// carry identity_rights but never widen. Under a production root both classes
// name the root, which raises the gates to High; altering or removing one
// check is Medium; of the GitHub delivery rights added only the OIDC subject
// template widens; an uncatalogued Azure DevOps or GitHub type carries no
// consequence.
func TestDeliveryRightsFloorTheLevel(t *testing.T) {
	bin := buildRiskelle(t, t.TempDir())
	run := func(t *testing.T, dir string) (report.Report, string) {
		t.Helper()
		code, out, stderr := runScore(t, bin, dir, "--base", "main", "--head", "topic")
		if code != 0 {
			t.Fatalf("score exit %d: %s", code, stderr)
		}
		return decodeReport(t, out), out
	}
	lowByShape := func(t *testing.T, r report.Report) {
		t.Helper()
		n := 0
		for _, p := range r.Breakdown {
			n += p.Points
		}
		if r.Total >= 20 || r.Total != n || r.ShapeLevel != score.Low {
			t.Errorf("total %d (points sum %d), shape %s, want a Low total equal to the sum", r.Total, n, r.ShapeLevel)
		}
	}
	consequences := func(t *testing.T, r report.Report, want string) {
		t.Helper()
		if b, _ := json.Marshal(r.Consequences); string(b) != want {
			t.Errorf("consequences = %s\nwant %s", b, want)
		}
	}
	base := map[string]string{"pipelines/main.tf": deliveryRepoTF}
	sevenTypes := `["azuredevops_check_approval","azuredevops_check_branch_control","azuredevops_check_exclusive_lock",` +
		`"azuredevops_check_required_template","azuredevops_environment","azuredevops_pipeline_authorization",` +
		`"azuredevops_serviceendpoint_azurerm"]`
	connection := `["azuredevops_serviceendpoint_azurerm"]`

	t.Run("service connection, authorisation, environment and checks", func(t *testing.T) {
		got, _ := run(t, rulesFixture(t, base, map[string]string{"pipelines/delivery.tf": deliveryTF}))
		lowByShape(t, got)
		if got.Level != score.High {
			t.Errorf("level %s, want High", got.Level)
		}
		consequences(t, got, `[{"class":"access_widened","paths":["pipelines/delivery.tf"],"types":`+connection+`,"floor":"High"},`+
			`{"class":"identity_rights","paths":["pipelines/delivery.tf"],"types":`+sevenTypes+`,"floor":"Medium"}]`)
	})

	t.Run("under a production root", func(t *testing.T) {
		b := map[string]string{"pipelines/main.tf": deliveryRepoTF, ".risk/rules.json": `{"production_roots":["prod/**"]}`}
		got, _ := run(t, rulesFixture(t, b, map[string]string{"prod/delivery.tf": deliveryTF}))
		lowByShape(t, got)
		if got.Level != score.High {
			t.Errorf("level %s, want High", got.Level)
		}
		consequences(t, got, `[{"class":"access_widened","paths":["prod/delivery.tf"],"types":`+connection+
			`,"floor":"High","production_roots":["prod/**"]},`+
			`{"class":"identity_rights","paths":["prod/delivery.tf"],"types":`+sevenTypes+
			`,"floor":"High","production_roots":["prod/**"]}]`)
	})

	withDelivery := map[string]string{"pipelines/main.tf": deliveryRepoTF, "pipelines/delivery.tf": deliveryTF}

	t.Run("altered required-template ref", func(t *testing.T) {
		alt := strings.Replace(deliveryTF, `repository_ref  = "refs/heads/main"`, `repository_ref  = "refs/tags/v2"`, 1)
		got, out := run(t, rulesFixture(t, withDelivery, map[string]string{"pipelines/delivery.tf": alt}))
		if got.Level != score.Medium || strings.Contains(out, "access_widened") {
			t.Errorf("level %s, want Medium with no access_widened:\n%s", got.Level, out)
		}
		consequences(t, got, `[{"class":"identity_rights","paths":["pipelines/delivery.tf"],"types":["azuredevops_check_required_template"],"floor":"Medium"}]`)
	})

	t.Run("removed exclusive lock", func(t *testing.T) {
		start := strings.Index(deliveryTF, `resource "azuredevops_check_exclusive_lock"`)
		end := strings.Index(deliveryTF, `resource "azuredevops_check_required_template"`)
		removed := deliveryTF[:start] + deliveryTF[end:]
		got, _ := run(t, rulesFixture(t, withDelivery, map[string]string{"pipelines/delivery.tf": removed}))
		if got.Level != score.Medium {
			t.Errorf("level %s, want Medium", got.Level)
		}
		consequences(t, got, `[{"class":"identity_rights","paths":["pipelines/delivery.tf"],"types":["azuredevops_check_exclusive_lock"],"floor":"Medium"}]`)
	})

	t.Run("GitHub delivery rights", func(t *testing.T) {
		got, _ := run(t, rulesFixture(t, base, map[string]string{"github/delivery.tf": githubTF}))
		if got.Level != score.High {
			t.Errorf("level %s, want High", got.Level)
		}
		ghTypes := `["github_actions_repository_oidc_subject_claim_customization_template","github_branch_protection",` +
			`"github_repository_environment","github_repository_ruleset"]`
		consequences(t, got, `[{"class":"access_widened","paths":["github/delivery.tf"],"types":["github_actions_repository_oidc_subject_claim_customization_template"],"floor":"High"},`+
			`{"class":"identity_rights","paths":["github/delivery.tf"],"types":`+ghTypes+`,"floor":"Medium"}]`)
	})

	t.Run("uncatalogued delivery types", func(t *testing.T) {
		got, out := run(t, rulesFixture(t, base, map[string]string{
			"pipelines/main.tf": strings.Replace(deliveryRepoTF, `"app"`, `"app2"`, 1),
			"github/repo.tf":    "resource \"github_repository\" \"app\" {\n  name = \"app\"\n}\n",
		}))
		if got.Level != score.Low || !strings.Contains(out, `"consequences": []`) || !reflect.DeepEqual(got.FlooredBy, []string{}) {
			t.Errorf("level %s consequences %+v, want Low and []", got.Level, got.Consequences)
		}
	})
}
