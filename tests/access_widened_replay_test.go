//go:build integration

package tests

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/bobmcallan/riskelle/internal/score"
)

// deliveryEnv is one release environment and its approval check, as the
// delivery/ fixture declares three of them.
func deliveryEnv(name string) string {
	return `# Releases to ` + name + ` wait here for approval.
resource "azuredevops_environment" "` + name + `" {
  project_id  = data.azuredevops_project.p.id
  name        = "` + name + `"
  description = "The ` + name + ` subscription. Every release waits here for approval."
}

resource "azuredevops_check_approval" "` + name + `" {
  project_id            = data.azuredevops_project.p.id
  target_resource_id    = azuredevops_environment.` + name + `.id
  target_resource_type  = "environment"
  approvers             = [data.azuredevops_identity_user.approver.id]
  requester_can_approve = true # the merge is queued by the flow
  instructions          = "Read the plan published on this run."
  timeout               = var.release_approval_timeout_minutes
}
`
}

// deliveryEnvApproved is deliveryEnv after the change: the approval check
// removed, and the comment and the description reworded.
func deliveryEnvApproved(name string) string {
	return `# Releases to ` + name + ` apply here; the pull request is the approval.
resource "azuredevops_environment" "` + name + `" {
  project_id  = data.azuredevops_project.p.id
  name        = "` + name + `"
  description = "The ` + name + ` subscription. Releases apply after the pull request's approval."
}
`
}

const (
	deliveryMain = `data "azuredevops_project" "p" {
  name = "p"
}
`
	deliveryRelease = `# The owner, who approves every release.
data "azuredevops_identity_user" "approver" {
  name          = var.release_approver
  search_filter = "MailAddress"
}

resource "azuredevops_check_required_template" "platform" {
  project_id           = data.azuredevops_project.p.id
  target_resource_id   = azuredevops_environment.platform.id
  target_resource_type = "environment"

  required_template {
    repository_name = "p/pipelines"
    repository_ref  = var.release_template_ref
    template_path   = "pipelines/release.yml"
  }
}
`
	deliveryReleaseApproved = `resource "azuredevops_check_required_template" "platform" {
  project_id           = data.azuredevops_project.p.id
  target_resource_id   = azuredevops_environment.platform.id
  target_resource_type = "environment"

  required_template {
    repository_name = "p/pipelines"
    repository_ref  = var.release_template_ref
    template_path   = "pipelines/release.yml"
  }
}
`
	deliveryVariables = `variable "release_subscription_id" {
  type = string
}

variable "release_approver" {
  description = "The owner's sign-in, the only release approver."
  type        = string
}

variable "release_template_ref" {
  type = string
}

variable "release_approval_timeout_minutes" {
  type    = number
  default = 60
}
`
	deliveryVariablesApproved = `variable "release_subscription_id" {
  type = string
}

variable "release_template_ref" {
  type = string
}

variable "release_approval_timeout_minutes" {
  type    = number
  default = 60
}
`
	deliveryTfvars = `release_subscription_id = "00000000-0000-0000-0000-000000000000" # platform
release_approver        = "owner@example.com"
release_template_ref    = "refs/heads/main" # must match pipelines/release.yml
`
	// terraform fmt realigns the comment of the unchanged assignment once
	// the shorter line beside it is gone.
	deliveryTfvarsApproved = `release_subscription_id = "00000000-0000-0000-0000-000000000000" # platform
release_template_ref    = "refs/heads/main"                      # must match pipelines/release.yml
`
	// deliveryAuthorization lets one pipeline use one environment: the id of
	// each is a literal in a local, so what the environment is cannot be read.
	deliveryAuthorization = `locals {
  app_environments = {
    app = { pipeline_id = 101, environment_id = 201 } # dev
  }
}

resource "azuredevops_pipeline_authorization" "app" {
  for_each = local.app_environments

  project_id  = data.azuredevops_project.p.id
  resource_id = tostring(each.value.environment_id)
  type        = "environment"
  pipeline_id = each.value.pipeline_id
}
`
	// deliveryRoleAssignment makes one identity Administrator on each of the
	// environments a local lists.
	deliveryRoleAssignment = `locals {
  dev_environments = {
    app = { environment_id = 201 } # dev
  }
}

data "azuredevops_identity_user" "applier" {
  name          = "applier@example.com"
  search_filter = "MailAddress"
}

resource "azuredevops_securityrole_assignment" "dev_applier" {
  for_each = local.dev_environments

  scope       = "distributedtask.environmentreferencerole"
  resource_id = "${data.azuredevops_project.p.id}_${each.value.environment_id}"
  identity_id = data.azuredevops_identity_user.applier.id
  role_name   = "Administrator"
}
`
	// deliveryBuildLease is a build definition, a pipeline authorised for an
	// environment, and the build service allowed to retain its runs.
	deliveryBuildLease = `resource "azuredevops_build_definition" "app" {
  project_id = data.azuredevops_project.p.id
  name       = "app promote"
}

resource "azuredevops_pipeline_authorization" "app_qa" {
  project_id  = data.azuredevops_project.p.id
  resource_id = "202"
  type        = "environment"
  pipeline_id = azuredevops_build_definition.app.id
}

resource "azuredevops_build_definition_permissions" "lease" {
  project_id          = data.azuredevops_project.p.id
  principal           = local.build_service
  build_definition_id = azuredevops_build_definition.app.id
  replace             = false

  permissions = {
    RetainIndefinitely = "Allow"
  }
}
`
	deliveryRoles = `resource "azuredevops_securityrole_assignment" "reader" {
  scope       = "distributedtask.environmentreferencerole"
  resource_id = "p_201"
  identity_id = "i"
  role_name   = "Reader"
}
`
	deliveryRetention = `variable "retain" {
  type    = string
  default = "Allow"
}

resource "azuredevops_build_definition_permissions" "retain" {
  project_id          = data.azuredevops_project.p.id
  principal           = "b"
  build_definition_id = "48"

  permissions = {
    RetainIndefinitely = var.retain
  }
}
`
	deliveryProjectRights = `resource "azuredevops_project_permissions" "builders" {
  project_id = data.azuredevops_project.p.id
  principal  = "b"

  permissions = {
    GENERIC_WRITE = "Allow"
  }
}
`
	deliveryIAM = `data "azuread_service_principal" "deployer" {
  display_name = "deployer-one"
}

resource "azuread_directory_role" "reader" {
  display_name = "Directory Readers"
}
`
)

// TestAccessWidenedNeedsAWidening drives the built binary over a delivery/
// directory shaped like a real change that was reported High: three release
// environments lose their approval checks, their descriptions and comments
// are reworded, a variable is removed and terraform fmt realigns the comment
// of the unchanged assignment beside it. Nothing is granted, so the change is
// Medium on identity_rights alone (the removed checks and the environments)
// and the report never names access_widened. Each restyle or label edit
// alone widens nothing. Neither does a delivery gate added or altered: a
// changed template ref, an approver added to a check, a pipeline authorised
// for an environment, a role on one environment or the permissions on one
// build definition is Medium; a role with another scope or one that cannot
// be read, and project permissions, are High. A display_name that selects what
// a grant targets still widens.
func TestAccessWidenedNeedsAWidening(t *testing.T) {
	bin := buildRiskelle(t, t.TempDir())
	base := map[string]string{
		"delivery/main.tf":          deliveryMain,
		"delivery/agents.tf":        deliveryEnv("agents"),
		"delivery/platform.tf":      deliveryEnv("platform"),
		"delivery/tenant.tf":        deliveryEnv("tenant"),
		"delivery/release.tf":       deliveryRelease,
		"delivery/variables.tf":     deliveryVariables,
		"delivery/terraform.tfvars": deliveryTfvars,
		"delivery/iam.tf":           deliveryIAM,
		"delivery/roles.tf":         deliveryRoles,
		"delivery/retention.tf":     deliveryRetention,
	}
	const (
		roleType = `"types":["azuredevops_securityrole_assignment"]`
		envScope = `"distributedtask.environmentreferencerole"`
	)
	replace := func(s, old, new string) string {
		t.Helper()
		if !strings.Contains(s, old) {
			t.Fatalf("fixture lacks %q", old)
		}
		return strings.Replace(s, old, new, 1)
	}
	const (
		none      = `[]`
		envOnly   = `[{"class":"identity_rights","paths":["delivery/agents.tf"],"types":["azuredevops_environment"],"floor":"Medium"}]`
		replayed  = `[{"class":"identity_rights","paths":["delivery/agents.tf","delivery/platform.tf","delivery/tenant.tf"],"types":["azuredevops_check_approval","azuredevops_environment"],"floor":"Medium"}]`
		viaVar    = `"types":["azuredevops_check_required_template via var.release_template_ref"]`
		approvers = `"types":["azuredevops_check_approval"]`
	)
	both := func(path, types string) string {
		return `[{"class":"access_widened","paths":["` + path + `"],` + types + `,"floor":"High"},` +
			`{"class":"identity_rights","paths":["` + path + `"],` + types + `,"floor":"Medium"}]`
	}

	alone := func(path, types string) string {
		return `[{"class":"identity_rights","paths":["` + path + `"],` + types + `,"floor":"Medium"}]`
	}

	for _, tt := range []struct {
		name         string
		topic        map[string]string
		level        score.Level
		flooredBy    []string
		consequences string
	}{
		{"the change replayed: checks removed, labels and comments reworded, a variable removed, an assignment realigned",
			map[string]string{
				"delivery/agents.tf":        deliveryEnvApproved("agents"),
				"delivery/platform.tf":      deliveryEnvApproved("platform"),
				"delivery/tenant.tf":        deliveryEnvApproved("tenant"),
				"delivery/release.tf":       deliveryReleaseApproved,
				"delivery/variables.tf":     deliveryVariablesApproved,
				"delivery/terraform.tfvars": deliveryTfvarsApproved,
			}, score.Medium, []string{"identity_rights"}, replayed},
		{"a description reworded",
			map[string]string{"delivery/agents.tf": replace(deliveryEnv("agents"), "Every release waits here for approval.", "Releases apply after the pull request's approval.")},
			score.Medium, []string{"identity_rights"}, envOnly},
		{"an assignment realigned by terraform fmt",
			map[string]string{"delivery/terraform.tfvars": replace(deliveryTfvars,
				`"refs/heads/main" # must match`, `"refs/heads/main"                      # must match`)},
			score.Low, []string{}, none},
		{"comments reworded and a blank line added inside a check",
			map[string]string{"delivery/agents.tf": replace(replace(deliveryEnv("agents"),
				"true # the merge is queued by the flow", "true # the flow queues the merge"),
				"  instructions ", "  # what the approver reads\n\n  instructions ")},
			score.Low, []string{}, none},
		{"a realigned assignment whose value also changed",
			map[string]string{"delivery/terraform.tfvars": replace(deliveryTfvars,
				`"refs/heads/main" # must match`, `"refs/heads/release"                   # must match`)},
			score.Medium, []string{"identity_rights"}, alone("delivery/terraform.tfvars", viaVar)},
		{"an approver added to a check",
			map[string]string{"delivery/agents.tf": replace(deliveryEnv("agents"),
				"[data.azuredevops_identity_user.approver.id]", "[data.azuredevops_identity_user.approver.id, data.azuredevops_identity_user.second.id]")},
			score.Medium, []string{"identity_rights"}, alone("delivery/agents.tf", approvers)},
		{"a pipeline authorised for an environment in a new file, through for_each over a local",
			map[string]string{"delivery/authorization.tf": deliveryAuthorization},
			score.Medium, []string{"identity_rights"}, alone("delivery/authorization.tf", `"types":["azuredevops_pipeline_authorization"]`)},
		{"an identity made Administrator on one environment, through for_each over a local",
			map[string]string{"delivery/applier.tf": deliveryRoleAssignment},
			score.Medium, []string{"identity_rights"}, alone("delivery/applier.tf", roleType)},
		{"a build service allowed to retain one definition's runs, beside a pipeline authorisation",
			map[string]string{"delivery/lease.tf": deliveryBuildLease},
			score.Medium, []string{"identity_rights"},
			alone("delivery/lease.tf", `"types":["azuredevops_build_definition_permissions","azuredevops_pipeline_authorization"]`)},
		{"a role changed within the environment scope",
			map[string]string{"delivery/roles.tf": replace(deliveryRoles, `"Reader"`, `"Administrator"`)},
			score.Medium, []string{"identity_rights"}, alone("delivery/roles.tf", roleType)},
		{"a variable only build-definition permissions use",
			map[string]string{"delivery/retention.tf": replace(deliveryRetention, `default = "Allow"`, `default = "Deny"`)},
			score.Medium, []string{"identity_rights"},
			alone("delivery/retention.tf", `"types":["azuredevops_build_definition_permissions via var.retain"]`)},
		{"a scope changed from the environment to a service connection",
			map[string]string{"delivery/roles.tf": replace(deliveryRoles, envScope, `"distributedtask.serviceendpointrole"`)},
			score.High, []string{"access_widened"}, both("delivery/roles.tf", roleType)},
		{"a role assignment scoped to a service connection",
			map[string]string{"delivery/applier.tf": replace(deliveryRoleAssignment, envScope, `"distributedtask.serviceendpointrole"`)},
			score.High, []string{"access_widened"}, both("delivery/applier.tf", roleType)},
		{"a role assignment whose scope is a local",
			map[string]string{"delivery/applier.tf": replace(deliveryRoleAssignment, envScope, `local.role_scope`)},
			score.High, []string{"access_widened"}, both("delivery/applier.tf", roleType)},
		{"project permissions",
			map[string]string{"delivery/project_rights.tf": deliveryProjectRights},
			score.High, []string{"access_widened"}, both("delivery/project_rights.tf", `"types":["azuredevops_project_permissions"]`)},
		{"a display_name that selects the principal a grant targets",
			map[string]string{"delivery/iam.tf": replace(deliveryIAM, "deployer-one", "deployer-two")},
			score.High, []string{"access_widened"}, both("delivery/iam.tf", `"types":["azuread_service_principal"]`)},
		{"a display_name that names the directory role activated",
			map[string]string{"delivery/iam.tf": replace(deliveryIAM, "Directory Readers", "Global Administrator")},
			score.High, []string{"access_widened"}, both("delivery/iam.tf", `"types":["azuread_directory_role"]`)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			code, out, stderr := runScore(t, bin, rulesFixture(t, base, tt.topic), "--base", "main", "--head", "topic")
			if code != 0 {
				t.Fatalf("score exit %d: %s", code, stderr)
			}
			got := decodeReport(t, out)
			if got.Level != tt.level || got.ShapeLevel != score.Low || !reflect.DeepEqual(got.FlooredBy, tt.flooredBy) {
				t.Errorf("level %s shape %s floored_by %v, want %s Low %v", got.Level, got.ShapeLevel, got.FlooredBy, tt.level, tt.flooredBy)
			}
			if b, _ := json.Marshal(got.Consequences); string(b) != tt.consequences {
				t.Errorf("consequences = %s\nwant %s", b, tt.consequences)
			}
			if tt.level != score.High && strings.Contains(out, "access_widened") {
				t.Errorf("report names access_widened:\n%s", out)
			}
		})
	}
}

// TestScopedGatesUnderAProductionRoot drives the built binary over the two
// gates added inside a repo's production root: neither widens, and the root
// raises identity_rights to High, naming itself.
func TestScopedGatesUnderAProductionRoot(t *testing.T) {
	bin := buildRiskelle(t, t.TempDir())
	dir := rulesFixture(t,
		map[string]string{".risk/rules.json": `{"production_roots":["prod/**"]}`, "prod/main.tf": deliveryMain},
		map[string]string{"prod/applier.tf": deliveryRoleAssignment, "prod/lease.tf": deliveryBuildLease})
	code, out, stderr := runScore(t, bin, dir, "--base", "main", "--head", "topic")
	if code != 0 {
		t.Fatalf("score exit %d: %s", code, stderr)
	}
	got := decodeReport(t, out)
	want := `[{"class":"identity_rights","paths":["prod/applier.tf","prod/lease.tf"],` +
		`"types":["azuredevops_build_definition_permissions","azuredevops_pipeline_authorization","azuredevops_securityrole_assignment"],` +
		`"floor":"High","production_roots":["prod/**"]}]`
	if b, _ := json.Marshal(got.Consequences); got.Level != score.High || string(b) != want {
		t.Errorf("level %s consequences = %s\nwant High %s", got.Level, b, want)
	}
	if strings.Contains(out, "access_widened") {
		t.Errorf("report names access_widened:\n%s", out)
	}
}
