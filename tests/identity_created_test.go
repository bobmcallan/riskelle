//go:build integration

package tests

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/bobmcallan/riskelle/internal/report"
	"github.com/bobmcallan/riskelle/internal/score"
)

// agentsBaseTF is the directory before the change: the variable the new
// identities use, and an application that already exists.
const agentsBaseTF = `variable "owner_object_ids" {
  type = list(string)
}

resource "azuread_application" "legacy" {
  display_name = "legacy"
  owners       = var.owner_object_ids
}
`

// agentsTF models a change that creates one deploy identity per environment
// and gives none of them anything: no permission request, no credential, no
// role and no membership.
const agentsTF = `# One deploy identity per environment; its rights come in a later change.

locals {
  deploy_agents = {
    dev = { apps = "dev-api" }
    stg = { apps = "stg-api" }
    prd = { apps = "prd-api" }
  }
}

resource "azuread_application" "deploy_agent" {
  for_each = local.deploy_agents

  display_name     = "${each.key}-deploy-agent"
  description      = "Deploy agent for ${each.key}: ${each.value.apps}."
  sign_in_audience = "AzureADMyOrg"
  owners           = var.owner_object_ids
  notes            = "Deploys app artifacts to ${each.key}."
  tags             = ["Environment=${each.key}", "ManagedBy=terraform"]
}

resource "azuread_service_principal" "deploy_agent" {
  for_each = local.deploy_agents

  client_id = azuread_application.deploy_agent[each.key].client_id
  owners    = var.owner_object_ids
  notes     = azuread_application.deploy_agent[each.key].notes
  tags      = azuread_application.deploy_agent[each.key].tags
}
`

// agentCertificateTF registers one certificate per file in certs/, of which
// the change adds none: the resource has no instance yet.
const agentCertificateTF = `
resource "azuread_application_certificate" "deploy_agent" {
  for_each = fileset("${path.module}/certs", "*-deploy-agent-*.crt")

  application_id = azuread_application.deploy_agent[split("-", each.key)[0]].id
  type           = "AsymmetricX509Cert"
  value          = file("${path.module}/certs/${each.key}")
}
`

// TestAccessWidenedNeedsAGrant drives the built binary over a change that
// only creates identities: a new application and service principal that hold
// nothing are identity_rights (Medium) and never access_widened, since a
// revert of the merge removes them. What gives an identity something keeps
// the change High: a permission request, a credential (a certificate
// resource included, with no instance yet or many), an adopted principal, a
// role assignment, a changed line in an application that already exists, a
// renamed one, and a variable the new blocks use.
func TestAccessWidenedNeedsAGrant(t *testing.T) {
	bin := buildRiskelle(t, t.TempDir())
	run := func(t *testing.T, base, topic map[string]string) (report.Report, string) {
		t.Helper()
		code, out, stderr := runScore(t, bin, rulesFixture(t, base, topic), "--base", "main", "--head", "topic")
		if code != 0 {
			t.Fatalf("score exit %d: %s", code, stderr)
		}
		return decodeReport(t, out), out
	}
	consequences := func(t *testing.T, r report.Report, want string) {
		t.Helper()
		if b, _ := json.Marshal(r.Consequences); string(b) != want {
			t.Errorf("consequences = %s\nwant %s", b, want)
		}
	}
	const file = "agents/deploy-agents.tf"
	base := map[string]string{"agents/main.tf": agentsBaseTF}
	identity := func(p, types string) string {
		return `{"class":"identity_rights","paths":["` + p + `"],"types":` + types + `,"floor":"Medium"}`
	}
	widened := func(p, types string) string {
		return `{"class":"access_widened","paths":["` + p + `"],"types":` + types + `,"floor":"High"}`
	}
	both := `["azuread_application","azuread_service_principal"]`
	replace := func(old, new string) string {
		if !strings.Contains(agentsTF, old) {
			t.Fatalf("fixture has no %q", old)
		}
		return strings.Replace(agentsTF, old, new, 1)
	}
	owners := "  owners           = var.owner_object_ids\n"

	medium := func(t *testing.T, topic map[string]string, p string) {
		t.Helper()
		got, out := run(t, base, topic)
		if got.Level != score.Medium || strings.Contains(out, "access_widened") {
			t.Errorf("level %s, want Medium with no access_widened:\n%s", got.Level, out)
		}
		if len(got.FlooredBy) != 1 || got.FlooredBy[0] != "identity_rights" {
			t.Errorf("floored_by %v, want [identity_rights]", got.FlooredBy)
		}
		consequences(t, got, `[`+identity(p, both)+`]`)
	}
	t.Run("a new application and service principal that hold nothing", func(t *testing.T) {
		medium(t, map[string]string{file: agentsTF}, file)
	})
	t.Run("the same blocks appended to an existing file", func(t *testing.T) {
		medium(t, map[string]string{"agents/main.tf": agentsBaseTF + "\n" + agentsTF}, "agents/main.tf")
	})

	for _, tt := range []struct {
		name            string
		topic           map[string]string
		path            string
		widens, carries string // the types of access_widened and of identity_rights
	}{
		{"a certificate resource with no instance yet", map[string]string{file: agentsTF + agentCertificateTF}, file,
			`["azuread_application_certificate"]`, `["azuread_application","azuread_application_certificate","azuread_service_principal"]`},
		{"a permission request", map[string]string{file: replace(owners, owners+"\n  required_resource_access {\n    resource_app_id = \"00000003-0000-0000-c000-000000000000\"\n  }\n")}, file,
			`["azuread_application"]`, both},
		{"a credential", map[string]string{file: replace(owners, owners+"\n  password {\n    display_name = \"deploy\"\n  }\n")}, file,
			`["azuread_application"]`, both},
		{"a dynamic block", map[string]string{file: replace(owners, owners+"\n  dynamic \"required_resource_access\" {\n    for_each = local.access\n    content {}\n  }\n")}, file,
			`["azuread_application"]`, both},
		{"an adopted principal", map[string]string{file: replace("  client_id = ", "  use_existing = true\n  client_id = ")}, file,
			`["azuread_service_principal"]`, both},
		{"a role assignment to the new principal", map[string]string{file: agentsTF + "\nresource \"azurerm_role_assignment\" \"deploy\" {\n  scope                = var.app_id\n  role_definition_name = \"Website Contributor\"\n  principal_id         = azuread_service_principal.deploy_agent[\"prd\"].object_id\n}\n"}, file,
			`["azurerm_role_assignment"]`, `["azuread_application","azuread_service_principal","azurerm_role_assignment"]`},
		{"an app role assignment to the new principal", map[string]string{file: agentsTF + "\nresource \"azuread_app_role_assignment\" \"deploy\" {\n  app_role_id         = var.role_id\n  principal_object_id = azuread_service_principal.deploy_agent[\"prd\"].object_id\n  resource_object_id  = var.resource_id\n}\n"}, file,
			`["azuread_app_role_assignment"]`, `["azuread_app_role_assignment","azuread_application","azuread_service_principal"]`},
		{"owners changed in an application that exists", map[string]string{"agents/main.tf": strings.Replace(agentsBaseTF, "= var.owner_object_ids", "= [var.other]", 1)}, "agents/main.tf",
			`["azuread_application"]`, `["azuread_application"]`},
		{"an application renamed with its owners changed", map[string]string{"agents/main.tf": strings.NewReplacer(`"legacy" {`, `"renamed" {`, "= var.owner_object_ids", "= [var.other]").Replace(agentsBaseTF) +
			"\nmoved {\n  from = azuread_application.legacy\n  to   = azuread_application.renamed\n}\n"}, "agents/main.tf",
			`["azuread_application"]`, `["azuread_application"]`},
		{"the variable the new blocks use changed", map[string]string{file: agentsTF, "agents/main.tf": strings.Replace(agentsBaseTF, "list(string)", "set(string)", 1)}, "",
			"", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, out := run(t, base, tt.topic)
			if got.Level != score.High || len(got.FlooredBy) != 1 || got.FlooredBy[0] != "access_widened" {
				t.Errorf("level %s floored by %v, want High by access_widened:\n%s", got.Level, got.FlooredBy, out)
			}
			if tt.path == "" {
				via := `["azuread_application via var.owner_object_ids","azuread_service_principal via var.owner_object_ids"]`
				all := `["azuread_application","azuread_application via var.owner_object_ids","azuread_service_principal","azuread_service_principal via var.owner_object_ids"]`
				consequences(t, got, `[`+widened("agents/main.tf", via)+`,`+
					`{"class":"identity_rights","paths":["agents/deploy-agents.tf","agents/main.tf"],"types":`+all+`,"floor":"Medium"}]`)
				return
			}
			consequences(t, got, `[`+widened(tt.path, tt.widens)+`,`+identity(tt.path, tt.carries)+`]`)
		})
	}
}
