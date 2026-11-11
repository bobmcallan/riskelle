//go:build integration

package tests

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/bobmcallan/riskelle/internal/report"
	"github.com/bobmcallan/riskelle/internal/score"
)

// pgServerTF is a Postgres flexible server, which grants nothing.
const pgServerTF = `
resource "azurerm_postgresql_flexible_server" "db" {
  name                = "db"
  resource_group_name = azurerm_resource_group.rg.name
  location            = azurerm_resource_group.rg.location
  version             = "16"
  sku_name            = "B_Standard_B1ms"
}
`

// pgAdminTF makes a person the Entra administrator of that server.
const pgAdminTF = `resource "azurerm_postgresql_flexible_server_active_directory_administrator" "admin" {
  server_name         = azurerm_postgresql_flexible_server.db.name
  resource_group_name = azurerm_resource_group.rg.name
  tenant_id           = var.tenant_id
  object_id           = var.admin_object_id
  principal_name      = "admin@example.com"
  principal_type      = "User"
}
`

// mssqlTF is a SQL server with an Entra administrator block.
const mssqlTF = `resource "azurerm_mssql_server" "sql" {
  name                = "sql"
  resource_group_name = azurerm_resource_group.rg.name
  location            = azurerm_resource_group.rg.location
  version             = "12.0"
  minimum_tls_version = "1.2"

  azuread_administrator {
    login_username = "sql-admins"
    object_id      = "00000000-0000-0000-0000-000000000001"
  }
}
`

// warehouseAdminsTF holds the Synapse and Kusto administrator types.
const warehouseAdminsTF = `resource "azurerm_synapse_workspace_aad_admin" "a" {
  synapse_workspace_id = var.workspace_id
  login                = "admins"
  object_id            = var.object_id
  tenant_id            = var.tenant_id
}

resource "azurerm_synapse_workspace_sql_aad_admin" "a" {
  synapse_workspace_id = var.workspace_id
  login                = "admins"
  object_id            = var.object_id
  tenant_id            = var.tenant_id
}

resource "azurerm_kusto_cluster_principal_assignment" "a" {
  name                = "admins"
  resource_group_name = azurerm_resource_group.rg.name
  cluster_name        = "kusto"
  tenant_id           = var.tenant_id
  principal_id        = var.object_id
  principal_type      = "Group"
  role                = "AllDatabasesAdmin"
}

resource "azurerm_kusto_database_principal_assignment" "a" {
  name                = "viewers"
  resource_group_name = azurerm_resource_group.rg.name
  cluster_name        = "kusto"
  database_name       = "db"
  tenant_id           = var.tenant_id
  principal_id        = var.object_id
  principal_type      = "Group"
  role                = "Viewer"
}
`

// TestDatabaseAdministratorsFloorTheLevel drives the built binary over
// database and data-explorer administrator grants, which are identity rights:
// an added or altered one also carries access_widened and is High, a removed
// one stays Medium, and a SQL server changed outside its administrator block
// carries no consequence.
func TestDatabaseAdministratorsFloorTheLevel(t *testing.T) {
	bin := buildRiskelle(t, t.TempDir())
	run := func(t *testing.T, base, topic map[string]string) (report.Report, string) {
		t.Helper()
		code, out, stderr := runScore(t, bin, rulesFixture(t, base, topic), "--base", "main", "--head", "topic")
		if code != 0 {
			t.Fatalf("score exit %d: %s", code, stderr)
		}
		return decodeReport(t, out), out
	}
	carries := func(t *testing.T, r report.Report, level score.Level, path, types string) {
		t.Helper()
		want := `{"class":"identity_rights","paths":["` + path + `"],"types":` + types + `,"floor":"Medium"}`
		if level == score.High {
			want = `{"class":"access_widened","paths":["` + path + `"],"types":` + types + `,"floor":"High"},` + want
		}
		want = "[" + want + "]"
		if b, _ := json.Marshal(r.Consequences); r.Level != level || string(b) != want {
			t.Errorf("level %s consequences = %s\nwant %s %s", r.Level, b, level, want)
		}
	}
	server := rgTF + pgServerTF
	const pgAdmin = `["azurerm_postgresql_flexible_server_active_directory_administrator"]`

	t.Run("a Postgres flexible server administrator added", func(t *testing.T) {
		got, _ := run(t, map[string]string{"data/main.tf": server}, map[string]string{"data/admin.tf": pgAdminTF})
		if got.ShapeLevel != score.Low {
			t.Errorf("shape %s, want Low", got.ShapeLevel)
		}
		carries(t, got, score.High, "data/admin.tf", pgAdmin)
	})
	t.Run("a Postgres flexible server administrator removed", func(t *testing.T) {
		got, _ := run(t, map[string]string{"data/main.tf": server + "\n" + pgAdminTF}, map[string]string{"data/main.tf": server})
		carries(t, got, score.Medium, "data/main.tf", pgAdmin)
	})
	t.Run("Synapse and Kusto administrators added", func(t *testing.T) {
		got, _ := run(t, map[string]string{"data/main.tf": rgTF}, map[string]string{"data/warehouse.tf": warehouseAdminsTF})
		carries(t, got, score.High, "data/warehouse.tf", `["azurerm_kusto_cluster_principal_assignment","azurerm_kusto_database_principal_assignment",`+
			`"azurerm_synapse_workspace_aad_admin","azurerm_synapse_workspace_sql_aad_admin"]`)
	})
	withSQL := map[string]string{"data/main.tf": rgTF, "data/sql.tf": mssqlTF}
	t.Run("a SQL server administrator altered", func(t *testing.T) {
		alt := strings.Replace(mssqlTF, "000000000001", "000000000002", 1)
		got, _ := run(t, withSQL, map[string]string{"data/sql.tf": alt})
		carries(t, got, score.High, "data/sql.tf", `["azurerm_mssql_server.azuread_administrator"]`)
	})
	t.Run("a SQL server altered outside its administrator", func(t *testing.T) {
		alt := strings.Replace(mssqlTF, `minimum_tls_version = "1.2"`, `minimum_tls_version = "1.3"`, 1)
		got, out := run(t, withSQL, map[string]string{"data/sql.tf": alt})
		if got.Level != score.Low || len(got.Consequences) != 0 {
			t.Errorf("level %s, want Low with no consequences:\n%s", got.Level, out)
		}
	})
}
