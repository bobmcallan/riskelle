package facts

import (
	"os"
	"reflect"
	"sort"
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
	lines := terraformBlocks([]byte(src))
	if got := blocksOf(lines); !reflect.DeepEqual(got, want) {
		t.Errorf("terraformBlocks =\n%q\nwant\n%q", got, want)
	}
	for n, attr := range map[int]string{2: "scope", 7: "policy", 8: "policy", 9: "policy", 15: ""} {
		if lines[n-1].attr != attr {
			t.Errorf("line %d attr %q, want %q", n, lines[n-1].attr, attr)
		}
	}
}

// TestTerraformInertAndData pins the lines that change nothing (blank lines
// and full-line comments, at column 0 or indented, but never a heredoc body
// line) and the lines of a data block.
func TestTerraformInertAndData(t *testing.T) {
	src := []struct {
		line        string
		inert, data bool
	}{
		{`# above`, true, false},
		{`resource "azuredevops_check_approval" "a" {`, false, false},
		{`  # the approvers`, true, false},
		{`  approvers = [1] # trailing`, false, false},
		{``, true, false},
		{`// column 0, inside`, true, false},
		{`  /* block */`, true, false},
		{`  instructions = <<EOT`, false, false},
		{`# a heredoc body line`, false, false},
		{``, false, false},
		{`EOT`, false, false},
		{`}`, false, false},
		{`data "azuread_service_principal" "sp" {`, false, true},
		{`  display_name = "x"`, false, true},
		{`  # which one`, true, true},
		{`}`, false, true},
		{`resource "aws_iam_role" "r" {`, false, false},
		{`}`, false, false},
	}
	var text []string
	for _, l := range src {
		text = append(text, l.line)
	}
	lines := terraformBlocks([]byte(strings.Join(text, "\n")))
	for i, l := range src {
		if lines[i].inert != l.inert || lines[i].data != l.data {
			t.Errorf("line %d %q: inert %v data %v, want %v %v", i+1, l.line, lines[i].inert, lines[i].data, l.inert, l.data)
		}
	}
}

// TestHCLText pins what two lines must share to differ in style alone:
// whitespace outside a string and a trailing comment are dropped; anything
// inside a string, a template's nested string included, is kept.
func TestHCLText(t *testing.T) {
	for _, tt := range []struct {
		a, b string
		same bool
	}{
		{`a = 1`, `a=1`, true},
		{`ref    = "refs/heads/main" # must match`, `ref = "refs/heads/main"                      # must match`, true},
		{`a = 1 # one`, `a = 1 // another`, true},
		{`a = 1 # one`, `a = 1`, true},
		{"\ta = [1,2]\r", `a = [ 1, 2 ]`, true},
		{`a = 1`, `a = 2`, false},
		{`a = "x  y"`, `a = "x y"`, false},
		{`a = "x # y"`, `a = "x # z"`, false},
		{`a = "x // y"`, `a = "x"`, false},
		{`a = "${f("#x")}"`, `a = "${f("#y")}"`, false},
		{`a = "${ f( "x" ) }"`, `a = "${f("x")}"`, true},
		{`a = "${ {k = "v"}["k"] } z"`, `a = "${{k="v"}["k"]} z"`, true},
		{`a = "${x} y"`, `a = "${x}  y"`, false},
		{`a = "q\" # r"`, `a = "q\" # s"`, false},
		{`a = "$${x} # y"`, `a = "$${x} # z"`, false},
		{`a = "%{if x} y%{endif}"`, `a = "%{ if x } y%{ endif }"`, true},
		{`# one`, `# two`, true},
	} {
		if got := hclText(tt.a) == hclText(tt.b); got != tt.same {
			t.Errorf("hclText(%q) == hclText(%q) is %v, want %v (%q, %q)", tt.a, tt.b, got, tt.same, hclText(tt.a), hclText(tt.b))
		}
	}
	if got := hclText(`  # only a comment`); got != "" {
		t.Errorf("hclText of a comment = %q, want empty", got)
	}
}

// TestRestyledLines pins the lines of a hunk that changed in style alone.
func TestRestyledLines(t *testing.T) {
	for _, tt := range []struct {
		name     string
		patch    []string
		old, new []int
	}{
		{"a variable removed and the next assignment realigned", []string{
			"diff --git a/terraform.tfvars b/terraform.tfvars",
			"--- a/terraform.tfvars",
			"+++ b/terraform.tfvars",
			`@@ -13,2 +13 @@ subscription = "s"`,
			`-release_approver        = "owner@example.com"`,
			`-release_template_ref    = "refs/heads/main" # must match`,
			`+release_template_ref    = "refs/heads/main"                      # must match`,
		}, []int{14}, []int{13}},
		{"a value changed beside a realignment", []string{
			"@@ -13 +13 @@",
			`-release_template_ref    = "refs/heads/main" # must match`,
			`+release_template_ref    = "refs/heads/release"                   # must match`,
		}, nil, nil},
		{"two lines swapped: one pair matches, in order", []string{
			"@@ -1,2 +1,2 @@",
			"-a = 1",
			"-b = 2",
			"+b  = 2",
			"+a  = 1",
		}, []int{1}, []int{2}},
		{"content that looks like a header, comments never matched, no pairing across hunks", []string{
			"@@ -3,3 +3,2 @@",
			"--- x = 1",
			"-# one",
			"-",
			"+# two",
			"+c = 3",
			`\ No newline at end of file`,
			"@@ -9 +8,0 @@",
			"-c = 3",
			"@@ -20 +19 @@",
			"-d   = 4",
			"+d = 4",
		}, []int{20}, []int{19}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			old, new := restyledLines([]byte(strings.Join(tt.patch, "\n")))
			for _, side := range []struct {
				name string
				got  map[int]bool
				want []int
			}{{"old", old, tt.old}, {"new", new, tt.new}} {
				var nums []int
				for n := range side.got {
					nums = append(nums, n)
				}
				sort.Ints(nums)
				if !reflect.DeepEqual(nums, side.want) {
					t.Errorf("%s = %v, want %v", side.name, nums, side.want)
				}
			}
		})
	}
}

// TestLabel pins the attributes whose lines never widen a class: description
// everywhere, display_name except where it selects what a grant targets.
func TestLabel(t *testing.T) {
	for _, tt := range []struct {
		l    tfLine
		want bool
	}{
		{tfLine{block: "azuredevops_environment", attr: "description"}, true},
		{tfLine{block: "azuread_application", attr: "display_name"}, true},
		{tfLine{block: "azuread_service_principal", attr: "description", data: true}, true},
		{tfLine{block: "azuread_service_principal", attr: "display_name", data: true}, false},
		{tfLine{block: "azuread_directory_role", attr: "display_name"}, false},
		{tfLine{block: "azuredevops_check_approval", attr: "approvers"}, false},
		{tfLine{block: "azuredevops_environment", attr: "name"}, false},
		{tfLine{block: "azuredevops_environment"}, false},
	} {
		if got := label(tt.l); got != tt.want {
			t.Errorf("label(%+v) = %v, want %v", tt.l, got, tt.want)
		}
	}
}

// blocksOf projects terraformBlocks' lines to their block types.
func blocksOf(lines []tfLine) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = l.block
	}
	return out
}

// TestTerraformAttributes pins which top-level attribute each line of a block
// falls in: an argument line and every line of its multi-line value up to its
// closing bracket, a heredoc body wherever it sits, a nested block; never a
// blank line or a comment between attributes; any attribute column.
func TestTerraformAttributes(t *testing.T) {
	src := []struct{ line, attr string }{
		{`resource "azuread_group" "g" {`, ""},
		{`  display_name = "g"`, "display_name"},
		{`  members = [`, "members"},
		{`    "a",`, "members"},
		{``, ""},
		{`    # b left`, "members"},
		{`    "c",`, "members"},
		{`  ]`, "members"},
		{`  owners = data.azuread_users.o.object_ids`, "owners"},
		{`  description = <<EOT`, "description"},
		{`members = ["x"]`, "description"},
		{`EOT`, "description"},
		{`  lifecycle {`, "lifecycle"},
		{`    ignore_changes = [members]`, "lifecycle"},
		{`  }`, "lifecycle"},
		{`  # owners below`, ""},
		{`  owners = concat(`, "owners"},
		{`    local.a,`, "owners"},
		{`  )`, "owners"},
		{`}`, ""},
		{`resource "azuread_group" "four" {`, ""},
		{`    members = ["a"]`, "members"},
		{`    display_name = "four"`, "display_name"},
		{`}`, ""},
	}
	var text []string
	for _, l := range src {
		text = append(text, l.line)
	}
	lines := terraformBlocks([]byte(strings.Join(text, "\n")))
	for i, l := range src {
		if lines[i].attr != l.attr {
			t.Errorf("line %d %q: attr %q, want %q", i+1, l.line, lines[i].attr, l.attr)
		}
		if lines[i].block != "azuread_group" {
			t.Errorf("line %d %q: block %q, want azuread_group", i+1, l.line, lines[i].block)
		}
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
		// group membership
		"azuread_group_member", "azuread_group_role_management_policy", "azuread_administrative_unit_member",
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
	for _, tt := range []struct {
		typ, attr string
		want      []string
	}{
		{"azuread_group", "members", []string{ConsequenceIdentityRights}},
		{"azuread_group", "owners", []string{ConsequenceIdentityRights}},
		{"azuread_group", "description", nil},
		{"azuread_group", "display_name", nil},
		{"azuread_group", "lifecycle", nil},
		{"azuread_group", "", nil},
		{"azurerm_role_assignment", "members", nil},
	} {
		if got := attributeConsequencesOf(tt.typ, tt.attr); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("attributeConsequencesOf(%q, %q) = %v, want %v", tt.typ, tt.attr, got, tt.want)
		}
	}
	if !IsConsequence(ConsequenceAccessWidened) || !IsConsequence(ConsequenceDefinitionWrite) ||
		!IsConsequence(ConsequenceDestructiveMigration) || !IsConsequence(ConsequenceIdentityRights) ||
		!IsConsequence(ConsequencePipelineIdentity) || !IsConsequence(ConsequenceProductionDeploy) ||
		!IsConsequence(ConsequenceSecretAdded) || !IsConsequence(ConsequenceStatefulDestroy) ||
		IsConsequence("root_access") {
		t.Error("IsConsequence does not match the catalog")
	}
}

// TestWidenedBy pins which classes accompany a class on a line added at
// head: access_widened accompanies identity_rights, nothing else is widened.
func TestWidenedBy(t *testing.T) {
	for _, tt := range []struct {
		class string
		want  []string
	}{
		{ConsequenceIdentityRights, []string{ConsequenceAccessWidened}},
		{ConsequencePipelineIdentity, nil},
		{ConsequenceAccessWidened, nil},
		{"", nil},
	} {
		if got := widenedBy(tt.class); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("widenedBy(%q) = %v, want %v", tt.class, got, tt.want)
		}
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
		names := append(append(append([]string{c.class}, c.patterns...), c.keys...), c.commands...)
		names = append(append(names, c.operations...), c.removed...)
		for _, r := range c.secrets {
			names = append(names, r.name)
		}
		for typ, attrs := range c.attributes {
			names = append(names, typ)
			for _, a := range attrs {
				names = append(names, typ+"."+a)
			}
		}
		for _, s := range names {
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
	for _, s := range []string{".tfvars", "variable", "description", "**/Migrations/**/*.cs", ".sql", "migrationBuilder", "Down"} {
		if !strings.Contains(cons, "`"+s+"`") {
			t.Errorf("README Consequences does not name `%s`", s)
		}
	}
	if !strings.Contains(cons, "via var.") {
		t.Error("README Consequences does not describe the via var. type")
	}
	if !strings.Contains(cons, "| `access_widened` | High |") || !strings.Contains(cons, "added at head") {
		t.Error("README Consequences does not give access_widened its High row and rule")
	}
	if !strings.Contains(cons, "| `secret_added` | High |") || !strings.Contains(cons, "**Added secrets.**") {
		t.Error("README Consequences does not give secret_added its High row and rules")
	}
	for _, s := range []string{"resource", "data", ".tf", "moved", "removed"} {
		if !strings.Contains(cons, "`"+s+"`") {
			t.Errorf("README Consequences does not name `%s`", s)
		}
	}
	if !strings.Contains(section("### Plan file"), "`consequences`") {
		t.Error("README Plan file does not name `consequences`")
	}
}

func TestTfvarsAssignments(t *testing.T) {
	src := []struct{ line, name string }{
		{`ref = "refs/heads/main"`, "ref"},
		{`roots = [`, "roots"},
		{`  "a",`, "roots"},
		{`  # note`, ""},
		{`]`, "roots"},
		{``, ""},
		{`# a comment`, ""},
		{`tags = {`, "tags"},
		{`  env = "prod"`, "tags"},
		{`}`, "tags"},
		{`policy = <<EOT`, "policy"},
		{`x = 1`, "policy"},
		{`EOT`, "policy"},
		{`count = 2`, "count"},
	}
	var text []string
	for _, l := range src {
		text = append(text, l.line)
	}
	got := tfvarsAssignments([]byte(strings.Join(text, "\n")))
	for i, l := range src {
		if got[i] != l.name {
			t.Errorf("line %d %q: %q, want %q", i+1, l.line, got[i], l.name)
		}
	}
}

func TestTerraformVariables(t *testing.T) {
	src := []struct{ line, variable, attr, block string }{
		{`variable "ref" {`, "ref", "", ""},
		{`  description = "the ref"`, "ref", "description", ""},
		{`  type        = string`, "ref", "type", ""},
		{`  default     = "refs/heads/main"`, "ref", "default", ""},
		{`  validation {`, "ref", "validation", ""},
		{`    condition     = can(regex("^refs/", var.ref))`, "ref", "validation", ""},
		{`    error_message = <<EOT`, "ref", "validation", ""},
		{`bad`, "ref", "validation", ""},
		{`EOT`, "ref", "validation", ""},
		{`  }`, "ref", "validation", ""},
		{`}`, "ref", "", ""},
		{`variable "y" {}`, "y", "", ""},
		{`resource "azuredevops_check_required_template" "r" {`, "", "", "azuredevops_check_required_template"},
		{`  required_template {`, "", "required_template", "azuredevops_check_required_template"},
		{`    repository_ref = var.ref`, "", "required_template", "azuredevops_check_required_template"},
		{`  }`, "", "required_template", "azuredevops_check_required_template"},
		{`}`, "", "", "azuredevops_check_required_template"},
	}
	var text []string
	for _, l := range src {
		text = append(text, l.line)
	}
	got := terraformBlocks([]byte(strings.Join(text, "\n")))
	for i, l := range src {
		want := tfLine{block: l.block, attr: l.attr, variable: l.variable}
		if strings.HasPrefix(l.line, "resource ") {
			want.address = "azuredevops_check_required_template.r"
		}
		if got[i] != want {
			t.Errorf("line %d %q: %+v, want %+v", i+1, l.line, got[i], want)
		}
	}
}

func TestRefersTo(t *testing.T) {
	for line, want := range map[string]bool{
		`  repository_ref = var.x`:         true,
		`  name = "${var.x}"`:              true,
		`  a = var.x.y`:                    true,
		`  a = var.x[0]`:                   true,
		`  a = upper(var.x)`:               true,
		`var.x`:                            true,
		`  a = var.x_y`:                    false,
		`  a = var.x-y`:                    false,
		`  a = local.var.x`:                false,
		`  # a = var.x`:                    false,
		`  // var.x`:                       false,
		`  a = var.xy`:                     false,
		`  a = var.xy, b = var.x`:          true,
		`  a = "var.x"  # textual matches`: true,
	} {
		if got := refersTo(line, "x"); got != want {
			t.Errorf("refersTo(%q, x) = %v, want %v", line, got, want)
		}
	}
}
