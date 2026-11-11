//go:build integration

package tests

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/bobmcallan/riskelle/internal/report"
	"github.com/bobmcallan/riskelle/internal/score"
)

// siteRolesTF gives a principal a role on each of two sites, each scoped by
// a direct reference to that one site.
const siteRolesTF = `locals {
  sites = { one = "app-one", two = "app-two" }
}

data "azurerm_linux_web_app" "site" {
  for_each = local.sites

  name                = each.value
  resource_group_name = "apps"
}

resource "azurerm_role_assignment" "site_reader" {
  for_each = local.sites

  scope                = data.azurerm_linux_web_app.site[each.key].id
  role_definition_name = "Reader"
  principal_id         = var.principal_id
}
`

// sitesViaVarTF gives the same roles with each scope built in a local from a
// variable: riskelle reads no local, so the scope is not one resource to it.
const sitesViaVarTF = `data "azurerm_linux_web_app" "site" {
  for_each = var.sites

  name                = each.value
  resource_group_name = "apps"
}

locals {
  site_roles = { for k, v in var.sites : k => { scope = data.azurerm_linux_web_app.site[k].id } }
}

resource "azurerm_role_assignment" "site_reader" {
  for_each = local.site_roles

  scope                = each.value.scope
  role_definition_name = "Reader"
  principal_id         = var.principal_id
}
`

// TestRoleOnOneResource drives the built binary over role assignments: one
// scoped by a direct reference to one resource is identity_rights (Medium),
// and High under a production root, never access_widened; a scope riskelle
// cannot read as one resource (a local, a resource group, a subscription)
// and a changed variable the block uses stay High by access_widened.
func TestRoleOnOneResource(t *testing.T) {
	bin := buildRiskelle(t, t.TempDir())
	run := func(t *testing.T, base, topic map[string]string) (report.Report, string) {
		t.Helper()
		code, out, stderr := runScore(t, bin, rulesFixture(t, base, topic), "--base", "main", "--head", "topic")
		if code != 0 {
			t.Fatalf("score exit %d: %s", code, stderr)
		}
		got := decodeReport(t, out)
		if got.ShapeLevel != score.Low {
			t.Errorf("shape %s, want Low", got.ShapeLevel)
		}
		return got, out
	}
	consequences := func(t *testing.T, r report.Report, want string) {
		t.Helper()
		if b, _ := json.Marshal(r.Consequences); string(b) != want {
			t.Errorf("consequences = %s\nwant %s", b, want)
		}
	}
	base := map[string]string{
		".risk/rules.json":        `{"production_roots":["rooted/**"]}`,
		"store/main.tf":           tfStorage,
		"apps/variables.tf":       "variable \"principal_id\" {}\n",
		"apps/terraform.tfvars":   "principal_id = \"0000\"\n",
		"rooted/variables.tf":     "variable \"principal_id\" {}\n",
		"rooted/terraform.tfvars": "principal_id = \"0000\"\n",
	}
	with := func(files map[string]string) map[string]string {
		out := map[string]string{}
		for k, v := range base {
			out[k] = v
		}
		for k, v := range files {
			out[k] = v
		}
		return out
	}
	const role = `["azurerm_role_assignment"]`
	widened := func(p, types string) string {
		return `[{"class":"access_widened","paths":["` + p + `"],"types":` + types + `,"floor":"High"},` +
			`{"class":"identity_rights","paths":["` + p + `"],"types":` + types + `,"floor":"Medium"}]`
	}
	high := func(t *testing.T, got report.Report, out string) {
		t.Helper()
		if got.Level != score.High || len(got.FlooredBy) != 1 || got.FlooredBy[0] != "access_widened" {
			t.Errorf("level %s floored by %v, want High by access_widened:\n%s", got.Level, got.FlooredBy, out)
		}
	}

	t.Run("roles scoped to one resource each", func(t *testing.T) {
		got, out := run(t, base, map[string]string{"apps/roles.tf": siteRolesTF})
		if got.Level != score.Medium || strings.Contains(out, "access_widened") {
			t.Errorf("level %s, want Medium with no access_widened:\n%s", got.Level, out)
		}
		consequences(t, got, `[{"class":"identity_rights","paths":["apps/roles.tf"],"types":`+role+`,"floor":"Medium"}]`)
	})
	t.Run("the same under a production root", func(t *testing.T) {
		got, out := run(t, base, map[string]string{"rooted/roles.tf": siteRolesTF})
		if got.Level != score.High || strings.Contains(out, "access_widened") {
			t.Errorf("level %s, want High with no access_widened:\n%s", got.Level, out)
		}
		consequences(t, got, `[{"class":"identity_rights","paths":["rooted/roles.tf"],"types":`+role+
			`,"floor":"High","production_roots":["rooted/**"]}]`)
	})
	t.Run("scopes built in a local from a variable", func(t *testing.T) {
		got, out := run(t, base, map[string]string{
			"apps/variables.tf":     "variable \"principal_id\" {}\nvariable \"sites\" {}\n",
			"apps/terraform.tfvars": "principal_id = \"0000\"\nsites = { one = \"app-one\", two = \"app-two\", three = \"app-three\" }\n",
			"apps/roles.tf":         sitesViaVarTF,
		})
		high(t, got, out)
		// var.sites is used only in a data block and a local, never in the role block
		consequences(t, got, widened("apps/roles.tf", role))
	})
	for _, tt := range []struct{ name, scope string }{
		{"a resource group", "azurerm_resource_group.g.id"},
		{"a subscription", "data.azurerm_subscription.current.id"},
	} {
		t.Run("a role on "+tt.name, func(t *testing.T) {
			src := strings.Replace(siteRolesTF, "data.azurerm_linux_web_app.site[each.key].id", tt.scope, 1)
			got, out := run(t, base, map[string]string{"apps/roles.tf": src})
			high(t, got, out)
			consequences(t, got, widened("apps/roles.tf", role))
		})
	}
	t.Run("a changed variable the role block uses", func(t *testing.T) {
		got, out := run(t, with(map[string]string{"apps/roles.tf": siteRolesTF}),
			map[string]string{"apps/terraform.tfvars": "principal_id = \"1111\"\n"})
		high(t, got, out)
		consequences(t, got, widened("apps/terraform.tfvars", `["azurerm_role_assignment via var.principal_id"]`))
	})
}
