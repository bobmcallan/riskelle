package facts

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestTerraformBlocks(t *testing.T) {
	src := strings.Join([]string{
		`resource "azurerm_role_assignment" "a" {`, // 1
		`  scope = "x"`,                       // 2
		``,                                    // 3
		`}`,                                   // 4
		`# a comment`,                         // 5
		`data azuread_service_principal sp {`, // 6
		`  policy = <<EOT`,                    // 7
		`{"a": {"b": 1}}`,                     // 8: heredoc body at column 0, ending in "}"
		`EOT`,                                 // 9
		`}`,                                   // 10
		`module "m" {`,                        // 11
		`  source = "./m"`,                    // 12
		`}`,                                   // 13
		`resource "aws_iam_role" "r" {}`,      // 14: one-line block
		`  stray = 1`,                         // 15
		`locals {`,                            // 16
		`// note`,                             // 17
		`}`,                                   // 18
	}, "\n")
	want := []string{
		"azurerm_role_assignment", "azurerm_role_assignment", "azurerm_role_assignment", "azurerm_role_assignment",
		"",
		"azuread_service_principal", "azuread_service_principal",
		"azuread_service_principal", "azuread_service_principal", "azuread_service_principal", // a heredoc body belongs to its block
		"", "", "",
		"aws_iam_role", "",
		"", "", "",
	}
	if got := terraformBlocks([]byte(src)); !reflect.DeepEqual(got, want) {
		t.Errorf("terraformBlocks =\n%q\nwant\n%q", got, want)
	}
}

func TestHunkLines(t *testing.T) {
	patch := strings.Join([]string{
		"diff --git a/x.tf b/x.tf",
		"--- a/x.tf",
		"+++ b/x.tf",
		"@@ -3 +3 @@",
		"-a",
		"+b",
		"@@ -10,0 +11,2 @@ resource \"x\" \"y\" {",
		"+c",
		"+d",
		"@@ -20,2 +22,0 @@",
		"--- x",
		"-e",
	}, "\n")
	old, new := hunkLines([]byte(patch))
	if want := []int{3, 20, 21}; !reflect.DeepEqual(old, want) {
		t.Errorf("old = %v, want %v", old, want)
	}
	if want := []int{3, 11, 12}; !reflect.DeepEqual(new, want) {
		t.Errorf("new = %v, want %v", new, want)
	}
}

func TestIdentityRightsCatalog(t *testing.T) {
	for _, typ := range []string{
		"aws_iam_role", "aws_iam_policy_attachment",
		"azuread_app_role_assignment", "azuread_application", "azuread_application_owner",
		"azuread_application_federated_identity_credential", "azuread_application_api_access",
		"azuread_directory_role", "azuread_directory_role_assignment",
		"azuread_service_principal", "azuread_service_principal_password",
		"azurerm_federated_identity_credential", "azurerm_role_assignment",
		"azurerm_role_definition", "azurerm_user_assigned_identity",
		"google_project_iam_member", "google_storage_bucket_iam_binding",
		// delivery rights: Azure DevOps
		"azuredevops_serviceendpoint_azurerm", "azuredevops_serviceendpoint_github", "azuredevops_serviceendpoint_permissions",
		"azuredevops_pipeline_authorization", "azuredevops_resource_authorization",
		"azuredevops_check_approval", "azuredevops_check_exclusive_lock", "azuredevops_check_required_template",
		"azuredevops_check_branch_control", "azuredevops_check_business_hours",
		"azuredevops_environment", "azuredevops_environment_resource_kubernetes",
		"azuredevops_branch_policy_min_reviewers", "azuredevops_branch_policy_build_validation",
		"azuredevops_git_permissions", "azuredevops_project_permissions", "azuredevops_build_definition_permissions",
		"azuredevops_group_membership", "azuredevops_securityrole_assignment",
		// delivery rights: GitHub
		"github_actions_environment_secret", "github_actions_environment_variable",
		"github_repository_environment", "github_repository_environment_deployment_policy",
		"github_branch_protection", "github_branch_protection_v3",
		"github_repository_ruleset", "github_organization_ruleset",
		"github_repository_collaborator", "github_repository_collaborators",
		"github_team_repository", "github_team_membership", "github_team_members",
		"github_actions_repository_permissions", "github_actions_organization_permissions",
		"github_actions_repository_oidc_subject_claim_customization_template",
		"github_actions_organization_oidc_subject_claim_customization_template",
	} {
		if got := consequencesOf(typ); !reflect.DeepEqual(got, []string{ConsequenceIdentityRights}) {
			t.Errorf("consequencesOf(%q) = %v, want [identity_rights]", typ, got)
		}
	}
	for _, typ := range []string{"azurerm_storage_account", "azuread_group", "google_iam_workload_identity_pool", "aws_s3_bucket", "",
		"azuredevops_git_repository", "azuredevops_project", "azuredevops_build_definition", "azuredevops_variable_group",
		"github_repository", "github_repository_file", "github_team", "github_actions_secret", "github_actions_variable",
		// not a provider type: pins the literal customization_template suffix
		"github_actions_repository_oidc_subject_claim_customize_template",
	} {
		if got := consequencesOf(typ); got != nil {
			t.Errorf("consequencesOf(%q) = %v, want none", typ, got)
		}
	}
	if !IsConsequence(ConsequenceIdentityRights) || !IsConsequence(ConsequencePipelineIdentity) || IsConsequence("root_access") {
		t.Error("IsConsequence does not match the catalog")
	}
}

// TestReadmeListsConsequences pins the README's Consequences section to the
// catalog, the detection rule and the plan file key.
func TestReadmeListsConsequences(t *testing.T) {
	data, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	section := func(heading string) string {
		s := string(data)
		i := strings.Index(s, "\n"+heading+"\n")
		if i < 0 {
			t.Fatalf("README has no %q section", heading)
		}
		s = s[i+len(heading)+2:]
		if j := strings.Index(s, "\n#"); j >= 0 {
			s = s[:j]
		}
		return s
	}
	cons := section("### Consequences")
	for _, c := range builtinConsequences {
		for _, s := range append(append([]string{c.class}, c.patterns...), c.keys...) {
			if !strings.Contains(cons, "`"+s+"`") {
				t.Errorf("README Consequences does not list `%s`", s)
			}
		}
	}
	for _, g := range pipelineDefinitions {
		if !strings.Contains(cons, "`"+g+"`") {
			t.Errorf("README Consequences does not list pipeline path `%s`", g)
		}
	}
	for _, s := range []string{"resource", "data", ".tf"} {
		if !strings.Contains(cons, "`"+s+"`") {
			t.Errorf("README Consequences does not name `%s`", s)
		}
	}
	if !strings.Contains(section("### Plan file"), "`consequences`") {
		t.Error("README Plan file does not name `consequences`")
	}
}
