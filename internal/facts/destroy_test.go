package facts

import (
	"context"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestTfAddresses pins which resource addresses a .tf file declares and which
// it names in the from of a moved or removed block: quoted and unquoted
// resource headers declare; a data block, and a header-looking line in a
// heredoc body, do not; a whole-resource from in a moved or removed block is
// gone, an instance, a module address, a to line and a commented from are not.
func TestTfAddresses(t *testing.T) {
	src := strings.Join([]string{
		`resource "azurerm_storage_account" "s" {`,
		`  name = "one"`,
		`  policy = <<EOT`,
		`resource "aws_s3_bucket" "in_heredoc" {`,
		`EOT`,
		`}`,
		`resource aws_s3_bucket b {`,
		`}`,
		`data "azurerm_key_vault" "kv" {`,
		`  name = "kv"`,
		`}`,
		`locals {`,
		`  doc = <<EOT`,
		`resource "aws_kms_key" "in_locals" {`,
		`EOT`,
		`}`,
		`moved {`,
		`  from = azurerm_storage_account.old`,
		`  to   = azurerm_storage_account.s`,
		`}`,
		`removed {`,
		`  from = aws_s3_bucket.gone # kept outside`,
		`  lifecycle {`,
		`    destroy = false`,
		`  }`,
		`}`,
		`moved {`,
		`  from = aws_s3_bucket.counted[0]`,
		`  to   = aws_s3_bucket.single`,
		`}`,
		`moved {`,
		`  from = module.m.aws_s3_bucket.x`,
		`  to   = aws_s3_bucket.x`,
		`}`,
		`removed {`,
		`  # from = aws_kms_key.commented`,
		`  from = aws_kms_key.k`,
		`}`,
		`moved {`,
		`  to = aws_s3_bucket.to_only`,
		`}`,
	}, "\n")
	declared, gone := tfAddresses([]byte(src))
	wantDeclared := map[string]string{
		"azurerm_storage_account.s": "azurerm_storage_account",
		"aws_s3_bucket.b":           "aws_s3_bucket",
	}
	if !reflect.DeepEqual(declared, wantDeclared) {
		t.Errorf("declared = %v, want %v", declared, wantDeclared)
	}
	wantGone := map[string]bool{
		"azurerm_storage_account.old": true,
		"aws_s3_bucket.gone":          true,
		"aws_kms_key.k":               true,
	}
	if !reflect.DeepEqual(gone, wantGone) {
		t.Errorf("gone = %v, want %v", gone, wantGone)
	}
}

// TestStatefulDestroyCatalog pins the stateful types: one per category and
// provider carries stateful_destroy on removal; configuration-only, identity
// and compute types carry none; editing a stateful type carries nothing.
func TestStatefulDestroyCatalog(t *testing.T) {
	for _, typ := range []string{
		// databases and servers
		"azurerm_mssql_database", "azurerm_postgresql_flexible_server", "aws_db_instance", "aws_dynamodb_table",
		"google_sql_database_instance", "google_spanner_database",
		// storage accounts, containers, shares, buckets
		"azurerm_storage_account", "azurerm_storage_container", "azurerm_storage_share", "aws_s3_bucket",
		"aws_efs_file_system", "google_storage_bucket",
		// key vaults and their keys, secrets, certificates; KMS keys
		"azurerm_key_vault", "azurerm_key_vault_key", "azurerm_key_vault_secret", "azurerm_key_vault_certificate",
		"aws_kms_key", "aws_secretsmanager_secret", "google_kms_crypto_key", "google_secret_manager_secret",
		// DNS zones
		"azurerm_dns_zone", "azurerm_private_dns_zone", "aws_route53_zone", "google_dns_managed_zone",
		// caches
		"azurerm_redis_cache", "aws_elasticache_cluster", "google_redis_instance",
		// message namespaces
		"azurerm_servicebus_namespace", "azurerm_eventhub_namespace", "aws_sqs_queue", "google_pubsub_topic",
		// log workspaces
		"azurerm_log_analytics_workspace", "aws_cloudwatch_log_group", "google_logging_project_bucket_config",
		// recovery vaults
		"azurerm_recovery_services_vault", "aws_backup_vault", "google_backup_dr_backup_vault",
	} {
		if got := removalConsequencesOf(typ); !reflect.DeepEqual(got, []string{ConsequenceStatefulDestroy}) {
			t.Errorf("removalConsequencesOf(%q) = %v, want [stateful_destroy]", typ, got)
		}
		if got := consequencesOf(typ); got != nil {
			t.Errorf("consequencesOf(%q) = %v, want none", typ, got)
		}
	}
	for _, typ := range []string{"azurerm_virtual_network", "azurerm_role_assignment", "aws_instance",
		"google_kms_crypto_key_iam_member", "azurerm_storage_account_network_rules", ""} {
		if got := removalConsequencesOf(typ); got != nil {
			t.Errorf("removalConsequencesOf(%q) = %v, want none", typ, got)
		}
	}
}

// TestCollectStatefulDestroy collects stateful_destroy from a real diff: a
// catalogued resource block whose address leaves its directory carries it
// with its merge-base path and type; a move within the directory, a moved or
// removed block for it, a data block, an uncatalogued type and an edit carry
// nothing.
func TestCollectStatefulDestroy(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	const (
		vnet     = "resource \"azurerm_virtual_network\" \"n\" {\n  name = \"n\"\n}\n"
		storage  = "resource \"azurerm_storage_account\" \"s\" {\n  name = \"one\"\n}\n"
		role     = "resource \"azurerm_role_assignment\" \"r\" {\n  role = \"Reader\"\n}\n"
		moved    = "moved {\n  from = azurerm_storage_account.s\n  to   = azurerm_storage_account.t\n}\n"
		removed  = "removed {\n  from = azurerm_storage_account.s\n\n  lifecycle {\n    destroy = false\n  }\n}\n"
		dataSA   = "data \"azurerm_storage_account\" \"s\" {\n  name = \"one\"\n}\n"
		renamedS = "resource \"azurerm_storage_account\" \"t\" {\n  name = \"one\"\n}\n"
	)
	destroyed := func(p string) []Consequence {
		return []Consequence{{Class: ConsequenceStatefulDestroy, Paths: []string{p}, Types: []string{"azurerm_storage_account"}}}
	}
	for _, tt := range []struct {
		name        string
		base, topic map[string]string
		rm          []string
		mv          [2]string
		want        []Consequence
	}{
		{name: "block deleted",
			base:  map[string]string{"infra/main.tf": vnet + "\n" + storage},
			topic: map[string]string{"infra/main.tf": vnet},
			want:  destroyed("infra/main.tf")},
		{name: "file deleted",
			base: map[string]string{"infra/main.tf": vnet, "infra/storage.tf": storage},
			rm:   []string{"infra/storage.tf"},
			want: destroyed("infra/storage.tf")},
		{name: "file renamed into another directory",
			base: map[string]string{"infra/main.tf": vnet, "infra/storage.tf": storage},
			mv:   [2]string{"infra/storage.tf", "data/storage.tf"},
			want: destroyed("infra/storage.tf")},
		{name: "renamed without a moved block",
			base:  map[string]string{"infra/main.tf": vnet + "\n" + storage},
			topic: map[string]string{"infra/main.tf": vnet + "\n" + renamedS},
			want:  destroyed("infra/main.tf")},
		{name: "moved block in another directory",
			base:  map[string]string{"infra/main.tf": vnet + "\n" + storage},
			topic: map[string]string{"infra/main.tf": vnet, "other/main.tf": renamedS + "\n" + moved},
			want:  destroyed("infra/main.tf")},
		{name: "with an identity grant altered",
			base:  map[string]string{"infra/main.tf": vnet + "\n" + storage + "\n" + role},
			topic: map[string]string{"infra/main.tf": vnet + "\n" + strings.Replace(role, "Reader", "Owner", 1)},
			want: []Consequence{
				{Class: ConsequenceAccessWidened, Paths: []string{"infra/main.tf"}, Types: []string{"azurerm_role_assignment"}},
				{Class: ConsequenceIdentityRights, Paths: []string{"infra/main.tf"}, Types: []string{"azurerm_role_assignment"}},
				{Class: ConsequenceStatefulDestroy, Paths: []string{"infra/main.tf"}, Types: []string{"azurerm_storage_account"}},
			}},
		{name: "moved to another file in the directory",
			base:  map[string]string{"infra/main.tf": vnet + "\n" + storage},
			topic: map[string]string{"infra/main.tf": vnet, "infra/storage.tf": storage}},
		{name: "renamed with a moved block",
			base:  map[string]string{"infra/main.tf": vnet + "\n" + storage},
			topic: map[string]string{"infra/main.tf": vnet + "\n" + renamedS + "\n" + moved}},
		{name: "removed block",
			base:  map[string]string{"infra/main.tf": vnet + "\n" + storage},
			topic: map[string]string{"infra/main.tf": vnet + "\n" + removed}},
		{name: "data block removed",
			base:  map[string]string{"infra/main.tf": vnet + "\n" + dataSA},
			topic: map[string]string{"infra/main.tf": vnet}},
		{name: "non-stateful type removed",
			base:  map[string]string{"infra/main.tf": vnet + "\n" + storage},
			topic: map[string]string{"infra/main.tf": storage}},
		{name: "stateful block only edited",
			base:  map[string]string{"infra/main.tf": vnet + "\n" + storage},
			topic: map[string]string{"infra/main.tf": vnet + "\n" + strings.Replace(storage, "one", "two", 1)}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			runGit(t, dir, "init", "--quiet", "--initial-branch=main")
			for name, content := range tt.base {
				write(t, dir, name, content)
			}
			runGit(t, dir, "add", ".")
			runGit(t, dir, "commit", "--quiet", "-m", "base")
			runGit(t, dir, "switch", "--quiet", "-c", "feature")
			for name, content := range tt.topic {
				write(t, dir, name, content)
			}
			for _, p := range tt.rm {
				runGit(t, dir, "rm", "--quiet", p)
			}
			if tt.mv[0] != "" {
				if err := os.MkdirAll(filepath.Join(dir, path.Dir(tt.mv[1])), 0o755); err != nil {
					t.Fatal(err)
				}
				runGit(t, dir, "mv", tt.mv[0], tt.mv[1])
			}
			runGit(t, dir, "add", ".")
			runGit(t, dir, "commit", "--quiet", "-m", "feature")
			got, err := Collect(context.Background(), dir, "main", "feature")
			if err != nil {
				t.Fatalf("Collect: %v", err)
			}
			if !reflect.DeepEqual(got.Consequences, tt.want) {
				t.Errorf("Consequences = %+v, want %+v", got.Consequences, tt.want)
			}
		})
	}
}
