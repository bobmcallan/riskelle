package facts

import (
	"bytes"
	"context"
	"path"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// The built-in consequence classes. A consequence is what a change does, read
// from its changed lines, not where it lands: a class sets a floor on the
// level the change is reported at.
const (
	ConsequenceAccessWidened        = "access_widened"
	ConsequenceDefinitionUpdate     = "definition_update"
	ConsequenceDefinitionWrite      = "definition_write"
	ConsequenceDestructiveMigration = "destructive_migration"
	ConsequenceIdentityRights       = "identity_rights"
	ConsequencePipelineIdentity     = "pipeline_identity"
	ConsequenceProductionDeploy     = "production_deploy"
	ConsequenceSecretAdded          = "secret_added"
	ConsequenceStatefulDestroy      = "stateful_destroy"
)

// Consequence is one class a change carries, the changed or planned paths
// that triggered it and the Terraform block types (with ".<attribute>" when
// an attribute of the block carried it, and " via var.<name>" when a changed
// variable that the block uses did), pipeline keys, commands, destructive
// operations or secret rule names that did; a secret's matched text is never
// kept. Both lists are sorted,
// distinct and non-nil; Types is empty for a plan, which has no content.
// access_widened carries the paths and types of the identity_rights lines
// added at head that carried it, less those in a label attribute or a
// delivery gate.
type Consequence struct {
	Class string   `json:"class"`
	Paths []string `json:"paths"`
	Types []string `json:"types"`
}

// builtinConsequences is the one catalog of consequence classes: for each, the
// globs over Terraform resource and data types that carry it, the pipeline
// keys that do (pipelines.go), the command forms that do (definitions.go), the
// destructive operations that do (destructive.go), or the secret rules that do
// (secrets.go). Sorted by class
// and versioned with each release, beside builtin (classes.go) and
// builtinAreas (areas.go).
//
// identity_rights includes delivery rights: service connections and their
// authorisations, deployment environments and their checks, branch policies
// and protections, and permissions decide who can act as a deploy identity,
// as a federated credential does. Generic secret and variable stores do not.
//
// access_widened has no patterns of its own: a line added at head that
// carries identity_rights (a grant added or altered, a variable it uses
// included) carries access_widened too, with the same path and type. Its
// labels are the top-level attributes that say what a block is for, not what
// it grants: a line in one carries identity_rights but never access_widened.
// Its gates are the delivery gates: which pipeline may use an environment or
// resource, the environment, its checks, branch policies, protections and
// rulesets. A gate takes effect only when a pipeline then runs, and a revert
// of the merge puts it back, so a line in one never carries access_widened
// either. Rights over one piece of delivery machinery are gates too: the
// permissions on one build definition, and (gateWhen) a role assignment
// whose scope is one environment. They take effect only when someone then
// acts on that environment or pipeline. Every other identity and what it may
// do (service connections, other permissions, memberships, role assignments)
// is a grant and widens.
//
// definition_update is a script that updates an existing release or build
// definition out of band, with a PUT or PATCH: the service keeps every
// revision, so restoring the prior one puts a failed update right.
// definition_write is every other definition write a script makes: reverting
// the merge does not undo it, and restoring a revision does not either.
//
// production_deploy has no patterns or keys: score reads it from the
// job environments of a changed pipeline definition (File.Environments) and
// the rules' production environment names.
//
// secret_added is read from the lines added at head of any text file that
// match one of its secret rules; the rule's name is the type.
//
// stateful_destroy is read from the resource blocks a directory declares, not
// from changed lines: its removed types are the stateful resource types whose
// block, removed from its directory, destroys data (destroy.go). Exact names:
// configuration-only types beside them destroy nothing.
var builtinConsequences = []struct {
	class      string
	patterns   []string
	keys       []string
	commands   []string             // the command forms a definition_update or definition_write type starts with
	operations []string             // the operation types destructiveOperations reports
	attributes map[string][]string  // exact type: the top-level attributes whose lines carry the class
	widens     string               // the class this one accompanies on a line added at head
	labels     []string             // the top-level attributes on whose lines a widening class does not accompany
	gates      []string             // the block types whose lines a widening class does not accompany: delivery gates
	gateWhen   map[string][2]string // exact type: the top-level attribute and the literal value it must hold for the block to be a delivery gate
	removed    []string             // resource types whose block removed from its directory carries the class
	secrets    []secretRule         // the rules whose match on a line added at head carries the class
}{
	{class: ConsequenceAccessWidened, widens: ConsequenceIdentityRights, labels: []string{"description", "display_name"}, gates: []string{
		"azuredevops_branch_policy_*",
		"azuredevops_build_definition_permissions",
		"azuredevops_check_*",
		"azuredevops_environment*",
		"azuredevops_pipeline_authorization",
		"azuredevops_resource_authorization",
		"github_actions_environment_*",
		"github_branch_protection*",
		"github_organization_ruleset",
		"github_repository_environment*",
		"github_repository_ruleset",
	}, gateWhen: map[string][2]string{
		// a role on one environment, not on a service connection, a
		// library, an agent pool or a project
		"azuredevops_securityrole_assignment": {"scope", "distributedtask.environmentreferencerole"},
	}},
	{class: ConsequenceDefinitionUpdate, commands: []string{
		"az devops invoke", "az pipelines", "az rest", "curl", "Invoke-RestMethod",
	}},
	{class: ConsequenceDefinitionWrite, commands: []string{
		"az devops invoke", "az pipelines", "az pipelines release", "az rest", "curl", "Invoke-RestMethod",
	}},
	{class: ConsequenceDestructiveMigration, operations: []string{
		"DeleteData", "DropColumn", "DropSchema", "DropTable", "RenameColumn", "RenameTable",
		"ALTER TABLE DROP", "DELETE FROM", "DROP COLUMN", "DROP DATABASE", "DROP SCHEMA",
		"DROP TABLE", "DROP VIEW", "TRUNCATE",
	}},
	{class: ConsequenceIdentityRights, patterns: []string{
		"aws_iam_*",
		"azuread_administrative_unit_member",
		"azuread_app_role_assignment",
		"azuread_application*",
		"azuread_directory_role*",
		"azuread_group_member",
		"azuread_group_role_management_policy",
		"azuread_service_principal*",
		"azuredevops_*_permissions",
		"azuredevops_branch_policy_*",
		"azuredevops_check_*",
		"azuredevops_environment*",
		"azuredevops_group_membership",
		"azuredevops_pipeline_authorization",
		"azuredevops_resource_authorization",
		"azuredevops_securityrole_assignment",
		"azuredevops_serviceendpoint_*",
		"azurerm_federated_identity_credential",
		"azurerm_role_assignment",
		"azurerm_role_definition",
		"azurerm_user_assigned_identity",
		"github_actions_*_oidc_subject_claim_customization_template",
		"github_actions_*_permissions",
		"github_actions_environment_*",
		"github_branch_protection*",
		"github_organization_ruleset",
		"github_repository_collaborator*",
		"github_repository_environment*",
		"github_repository_ruleset",
		"github_team_members",
		"github_team_membership",
		"github_team_repository",
		"google_*_iam_*",
	}, attributes: map[string][]string{
		// who is in a group decides who holds the roles granted to it
		"azuread_group": {"members", "owners"},
	}},
	{class: ConsequencePipelineIdentity, keys: []string{
		"*serviceConnection", "azureSubscription*", "connectedServiceName*",
		"environment", "id-token: write", "permissions: write-all",
		// references, only in a definition that carries one of the above
		"resources.repositories.ref", "extends.template", "stages.template", "jobs.template", "steps.template",
	}},
	{class: ConsequenceProductionDeploy},
	{class: ConsequenceSecretAdded, secrets: secretRules},
	{class: ConsequenceStatefulDestroy, removed: []string{
		"aws_backup_vault", "aws_cloudwatch_log_group", "aws_db_instance", "aws_docdb_cluster",
		"aws_dynamodb_table", "aws_efs_file_system", "aws_elasticache_cluster", "aws_elasticache_replication_group",
		"aws_kms_key", "aws_neptune_cluster", "aws_opensearch_domain", "aws_rds_cluster",
		"aws_redshift_cluster", "aws_route53_zone", "aws_s3_bucket", "aws_secretsmanager_secret", "aws_sqs_queue",
		"azurerm_cosmosdb_account", "azurerm_cosmosdb_mongo_collection", "azurerm_cosmosdb_mongo_database",
		"azurerm_cosmosdb_sql_container", "azurerm_cosmosdb_sql_database", "azurerm_data_protection_backup_vault",
		"azurerm_dns_zone", "azurerm_eventhub_namespace", "azurerm_key_vault", "azurerm_key_vault_certificate",
		"azurerm_key_vault_key", "azurerm_key_vault_secret", "azurerm_log_analytics_workspace",
		"azurerm_mssql_database", "azurerm_mssql_managed_database", "azurerm_mssql_managed_instance",
		"azurerm_mssql_server", "azurerm_mysql_flexible_database", "azurerm_mysql_flexible_server",
		"azurerm_postgresql_database", "azurerm_postgresql_flexible_server",
		"azurerm_postgresql_flexible_server_database", "azurerm_postgresql_server", "azurerm_private_dns_zone",
		"azurerm_recovery_services_vault", "azurerm_redis_cache", "azurerm_servicebus_namespace",
		"azurerm_storage_account", "azurerm_storage_container", "azurerm_storage_data_lake_gen2_filesystem",
		"azurerm_storage_share",
		"google_backup_dr_backup_vault", "google_bigquery_dataset", "google_bigquery_table",
		"google_bigtable_instance", "google_bigtable_table", "google_dns_managed_zone", "google_filestore_instance",
		"google_firestore_database", "google_kms_crypto_key", "google_kms_key_ring",
		"google_logging_project_bucket_config", "google_pubsub_topic", "google_redis_instance",
		"google_secret_manager_secret", "google_spanner_database", "google_spanner_instance",
		"google_sql_database", "google_sql_database_instance", "google_storage_bucket",
	}},
}

type compiledConsequence struct {
	class   string
	globs   Globs
	removed Globs
	gates   Globs
}

// compiledConsequences holds builtinConsequences compiled once.
var compiledConsequences = func() []compiledConsequence {
	out := make([]compiledConsequence, 0, len(builtinConsequences))
	for _, c := range builtinConsequences {
		out = append(out, compiledConsequence{c.class, CompileGlobs(c.patterns), CompileGlobs(c.removed), CompileGlobs(c.gates)})
	}
	return out
}()

// IsConsequence reports whether class is a built-in consequence class.
func IsConsequence(class string) bool {
	for _, c := range builtinConsequences {
		if c.class == class {
			return true
		}
	}
	return false
}

// widenedBy returns the catalog classes that accompany class on a line added
// at head, in catalog order; nil when none do.
func widenedBy(class string) []string {
	var out []string
	for _, c := range builtinConsequences {
		if c.widens != "" && c.widens == class {
			out = append(out, c.class)
		}
	}
	return out
}

// label reports whether l falls in a label attribute of a widening class: a
// class read on it is recorded, but not the class that widens it.
// display_name is no label where it decides access: in a data block it
// selects the object read (which principal a grant targets), and in
// azuread_directory_role it names the role activated.
func label(l tfLine) bool {
	if l.attr == "" || l.attr == "display_name" && (l.data || l.block == "azuread_directory_role") {
		return false
	}
	for _, c := range builtinConsequences {
		if c.widens != "" && slices.Contains(c.labels, l.attr) {
			return true
		}
	}
	return false
}

// gate reports whether block is a delivery gate: which pipeline may use an
// environment or resource, the environment, its checks, branch policies,
// protections and rulesets. A class read on its lines is recorded, but not
// the class that widens it: a gate takes effect only when a pipeline runs,
// and a revert of the merge puts it back. A permissions type grants, whatever
// it guards: it is a gate only when the catalog lists it by its exact name,
// as the permissions on one build definition are, never through a glob.
func gate(block string) bool {
	permissions := strings.HasSuffix(block, "_permissions")
	for i, c := range compiledConsequences {
		if permissions && slices.Contains(builtinConsequences[i].gates, block) || !permissions && c.gates.Match(block) {
			return true
		}
	}
	return false
}

// literalAttr matches a top-level attribute line whose whole value is one
// plain string literal, capturing the attribute and the string: no
// interpolation, reference or expression, and nothing after it but a comment.
var literalAttr = regexp.MustCompile(`^[ \t]*([A-Za-z_][A-Za-z0-9_-]*)[ \t]*=[ \t]*"([^"$%\\]*)"[ \t\r]*(?:(?:#|//).*)?$`)

// scopedGate reports whether line i is in a block the catalog makes a
// delivery gate by one of its attributes (gateWhen): the block's own line
// for that attribute holds exactly the catalogued string literal. Only the
// block's lines are read, at the side they were read at: a variable, a local,
// an expression, a multi-line value or a missing attribute is no gate.
func scopedGate(lines []tfLine, text []string, i int) bool {
	l := lines[i]
	if l.start == 0 {
		return false
	}
	for _, c := range builtinConsequences {
		when, ok := c.gateWhen[l.block]
		if !ok {
			continue
		}
		for j := l.start; j < len(lines) && lines[j].start == l.start; j++ {
			if m := literalAttr.FindStringSubmatch(text[j]); m != nil && lines[j].attr == when[0] && m[1] == when[0] {
				return m[2] == when[1]
			}
		}
	}
	return false
}

// inGate reports whether line i cannot widen its class because of the block
// it is in: a delivery gate by type (gate) or by an attribute (scopedGate).
func inGate(lines []tfLine, text []string, i int) bool {
	return gate(lines[i].block) || scopedGate(lines, text, i)
}

// hclComment reports whether a line, less its leading whitespace, is a
// full-line comment.
func hclComment(trimmed string) bool {
	return strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*")
}

// hclText returns a line of Terraform as its tokens alone: less every
// whitespace character outside a quoted string and less a trailing "#" or
// "//" comment outside one. Two lines with the same text differ only in
// style, as terraform fmt realigns them. A string is scanned with its "\"
// escapes and its "${" and "%{" templates, whose braces are counted and in
// which a quote opens a nested string, so whitespace or a "#" inside any
// string is kept.
func hclText(line string) string {
	var b strings.Builder
	var templates []int // the open-brace count of each open template
	inString := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		next := byte(0)
		if i+1 < len(line) {
			next = line[i+1]
		}
		if inString {
			b.WriteByte(c)
			switch {
			case c == '\\' && next != 0, (c == '$' || c == '%') && next == c:
				b.WriteByte(next)
				i++
			case c == '"':
				inString = false
			case (c == '$' || c == '%') && next == '{':
				b.WriteByte(next)
				i++
				templates, inString = append(templates, 0), false
			}
			continue
		}
		last := len(templates) - 1
		switch {
		case c == ' ' || c == '\t' || c == '\r':
			continue
		case c == '#' || c == '/' && next == '/':
			return b.String()
		case c == '"':
			inString = true
		case c == '{' && last >= 0:
			templates[last]++
		case c == '}' && last >= 0 && templates[last] > 0:
			templates[last]--
		case c == '}' && last >= 0:
			templates, inString = templates[:last], true
		}
		b.WriteByte(c)
	}
	return b.String()
}

// attributeConsequencesOf returns the classes a line in attr of a block of
// type typ carries by the catalog's attributes; nil when attr is "".
func attributeConsequencesOf(typ, attr string) []string {
	if attr == "" {
		return nil
	}
	var out []string
	for _, c := range builtinConsequences {
		for _, a := range c.attributes[typ] {
			if a == attr {
				out = append(out, c.class)
			}
		}
	}
	return out
}

// consequencesOf returns the classes a block type carries.
func consequencesOf(typ string) []string {
	var out []string
	for _, c := range compiledConsequences {
		if c.globs.Match(typ) {
			out = append(out, c.class)
		}
	}
	return out
}

// variableHeader matches a variable block header at column 0.
var variableHeader = regexp.MustCompile(`^variable[ \t]+"?([A-Za-z_][A-Za-z0-9_-]*)"?`)

// tfvarsAssign matches a column-0 variable assignment in a .tfvars file.
var tfvarsAssign = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_-]*)[ \t]*=`)

// tfvarsAssignments returns, for each line of a .tfvars file, the variable
// whose assignment it falls in, or "": a column-0 "X =" line opens X;
// indented lines and a heredoc body continue it; a column-0 line starting
// with "]", "}" or ")" continues and closes it; blank and comment lines carry
// none; any other column-0 line closes it.
func tfvarsAssignments(content []byte) []string {
	lines := strings.Split(string(content), "\n")
	out := make([]string, len(lines))
	open, heredoc := "", ""
	for i, l := range lines {
		switch {
		case heredoc != "":
			out[i] = open
			if strings.TrimSpace(l) == heredoc {
				heredoc = ""
			}
			continue
		case strings.TrimSpace(l) == "" || hclComment(strings.TrimSpace(l)):
		case l[0] == ' ' || l[0] == '\t':
			out[i] = open
		case strings.ContainsAny(l[:1], "])}"):
			out[i], open = open, ""
		default:
			open = ""
			if m := tfvarsAssign.FindStringSubmatch(l); m != nil {
				open = m[1]
			}
			out[i] = open
		}
		if m := heredocStart.FindStringSubmatch(l); m != nil && out[i] != "" {
			heredoc = m[1]
		}
	}
	return out
}

// refersTo reports whether a line that is not a full-line comment references
// var.<name> as a whole identifier: not preceded by an identifier character
// or ".", nor followed by an identifier character.
func refersTo(line, name string) bool {
	if hclComment(strings.TrimSpace(line)) {
		return false
	}
	ref := "var." + name
	ident := func(b byte) bool {
		return b == '_' || b == '-' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
	}
	for i := 0; ; {
		j := strings.Index(line[i:], ref)
		if j < 0 {
			return false
		}
		j += i
		end := j + len(ref)
		if (j == 0 || !ident(line[j-1]) && line[j-1] != '.') && (end == len(line) || !ident(line[end])) {
			return true
		}
		i = j + 1
	}
}

// blockHeader matches a resource or data block header at column 0.
var blockHeader = regexp.MustCompile(`^(resource|data)[ \t]+"?([A-Za-z][A-Za-z0-9_-]*)"?`)

// tfLine is one line of a Terraform file: the resource or data type of the
// top-level block it belongs to, and the block's top-level attribute it falls
// in ("" when none). variable names the column-0 variable "X" block a line
// belongs to, whose attributes are tracked the same way; "" otherwise.
// address is "type.name" on the header line of a resource block only. gone
// is "moved" or "removed" on every line of a column-0 moved or removed block,
// whose attributes are tracked as a variable block's are; "" otherwise. data
// is set on every line of a data block. inert is set on a blank line and on a
// full-line comment outside a heredoc body: it changes nothing, so it carries
// no class and sets no variable. start is the 1-based line number of the
// header of the resource or data block the line belongs to, 0 when none: two
// adjacent blocks of one type differ by it.
type tfLine struct {
	block, attr, variable, address, gone string
	data, inert                          bool
	start                                int
}

// tfAttr matches the identifier that opens a top-level attribute line.
var tfAttr = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*`)

// terraformBlocks returns, for each line of content (index 0 is line 1), the
// resource or data type of the top-level block it belongs to, or "" when none,
// and the top-level attribute of that block it falls in.
//
// A block is found by its header at column 0, as terraform fmt writes every
// file: a column-0 line other than "}", or a "#", "//" or "/*" comment, opens
// a new block, and a column-0 "}" closes the open one. Indented and blank
// lines and column-0 comments belong to the open block, and so does a
// heredoc's body, up to its closing marker, wherever it starts: an IAM policy
// is often a heredoc of JSON at column 0. No braces are counted, so braces in
// strings and heredocs cannot mislead it.
//
// The attribute column is the indent of the block's first indented line. A
// line at that column opens the attribute its leading identifier names (an
// argument or a nested block); a deeper line, a heredoc body, and a closing
// "]", ")" or "}" at that column continue the open one. Blank lines and
// column-0 comments carry none and change nothing.
func terraformBlocks(content []byte) []tfLine {
	lines := strings.Split(string(content), "\n")
	out := make([]tfLine, len(lines))
	open, vname, gone, heredoc, attr, col, data, start := "", "", "", "", "", -1, false, 0
	line := func(a string) tfLine {
		return tfLine{block: open, attr: a, variable: vname, gone: gone, data: data, start: start}
	}
	for i, l := range lines {
		trimmed := strings.TrimLeft(l, " \t")
		switch {
		case heredoc != "":
			out[i] = line(attr)
			if strings.TrimSpace(l) == heredoc {
				heredoc = ""
			}
			continue
		case strings.TrimSpace(l) == "" || hclComment(l):
			out[i] = line("")
			out[i].inert = true
		case l[0] == ' ' || l[0] == '\t':
			indent := len(l) - len(trimmed)
			if col < 0 {
				col = indent
			}
			tracked := open != "" || vname != "" || gone != ""
			switch {
			case !tracked || indent > col:
			case strings.ContainsAny(trimmed[:1], "])}"):
				out[i] = line(attr)
				attr = ""
				continue
			default:
				attr = tfAttr.FindString(trimmed)
			}
			if !tracked {
				attr = ""
			}
			out[i] = line(attr)
			out[i].inert = hclComment(trimmed)
		case l[0] == '}':
			out[i] = line("")
			open, vname, gone, attr, col, data, start = "", "", "", "", -1, false, 0
		default:
			open, vname, gone, attr, col, data, start = "", "", "", "", -1, false, 0
			if m := blockHeader.FindStringSubmatch(l); m != nil {
				open, data, start = m[2], m[1] == "data", i+1
			} else if m := variableHeader.FindStringSubmatch(l); m != nil {
				vname = m[1]
			} else if m := goneHeader.FindStringSubmatch(l); m != nil {
				gone = m[1]
			}
			out[i] = line("")
			if m := resourceHeader.FindStringSubmatch(l); m != nil {
				out[i].address = m[1] + "." + m[2]
			}
			if strings.HasSuffix(strings.TrimRight(l, " \t\r"), "}") {
				open, vname, gone, data, start = "", "", "", false, 0 // a one-line block
			}
		}
		if m := heredocStart.FindStringSubmatch(l); m != nil {
			heredoc = m[1]
		}
	}
	return out
}

// heredocStart matches a line that opens a heredoc, capturing its marker.
var heredocStart = regexp.MustCompile(`<<-?([A-Za-z_][A-Za-z0-9_]*)[ \t\r]*$`)

// hunkHeader matches a unified diff hunk header.
var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

// hunkLines returns the old-side and new-side line numbers a -U0 patch
// changes, from its hunk headers alone. A missing count means 1 and a count
// of 0 means no lines. Content lines are never parsed, so a removed line that
// looks like a header cannot be taken for one: a content line starts with
// " ", "+", "-" or "\\", never "@@".
func hunkLines(patch []byte) (old, new []int) {
	for _, l := range strings.Split(string(patch), "\n") {
		m := hunkHeader.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		old = append(old, span(m[1], m[2])...)
		new = append(new, span(m[3], m[4])...)
	}
	return old, new
}

// span expands a hunk's start and count into its line numbers.
func span(start, count string) []int {
	s, _ := strconv.Atoi(start)
	n := 1
	if count != "" {
		n, _ = strconv.Atoi(count)
	}
	out := make([]int, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, s+i)
	}
	return out
}

// restyledLines returns the old-side and new-side line numbers of a -U0
// Terraform patch whose change is style alone: within one hunk, a removed
// line and a later added line with the same hclText, matched in order (each
// removed line takes the first such added line after the last match). A line
// moved past another match, or to another hunk, stays a change; a line with
// no text (blank or a comment) is never matched. A hunk's content lines are
// counted off by its header, so none is taken for a header.
func restyledLines(patch []byte) (old, new map[int]bool) {
	old, new = map[int]bool{}, map[int]bool{}
	lines := strings.Split(string(patch), "\n")
	for i := 0; i < len(lines); i++ {
		m := hunkHeader.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		removedAt, addedAt := span(m[1], m[2]), span(m[3], m[4])
		var removed, added []string
		for i+1 < len(lines) && len(removed)+len(added) < len(removedAt)+len(addedAt) {
			i++
			switch l := lines[i]; {
			case strings.HasPrefix(l, "-") && len(removed) < len(removedAt):
				removed = append(removed, hclText(l[1:]))
			case strings.HasPrefix(l, "+"):
				added = append(added, hclText(l[1:]))
			}
		}
		from := 0
		for r, text := range removed {
			for a := from; text != "" && a < len(added); a++ {
				if added[a] == text {
					old[removedAt[r]], new[addedAt[a]] = true, true
					from = a + 1
					break
				}
			}
		}
	}
	return old, new
}

// less returns lines without those in drop.
func less(lines []int, drop map[int]bool) []int {
	out := lines[:0]
	for _, n := range lines {
		if !drop[n] {
			out = append(out, n)
		}
	}
	return out
}

// changedLines returns the old-side and new-side line numbers of f's
// merge-base..head -U0 diff, a rename diffed across its two paths.
func changedLines(ctx context.Context, repo, mergeBase, head string, f File) (old, new []int, err error) {
	patch, err := filePatch(ctx, repo, mergeBase, head, f)
	if err != nil {
		return nil, nil, err
	}
	old, new = hunkLines(patch)
	return old, new, nil
}

// filePatch returns f's merge-base..head -U0 diff, a rename diffed across its
// two paths.
func filePatch(ctx context.Context, repo, mergeBase, head string, f File) ([]byte, error) {
	args := []string{"--literal-pathspecs", "--attr-source=" + head, "diff", "-U0"}
	args = append(args, pinnedDiff...)
	args = append(args, mergeBase, head, "--")
	if f.OldPath != "" {
		args = append(args, f.OldPath)
	}
	return git(ctx, repo, append(args, f.Path)...)
}

// collectConsequences finds the consequence classes in the .tf files and
// pipeline definitions of a change, reading each changed line at head when
// added and at the merge base when removed. Every changed text file that is
// neither docs nor a test is also read at head for definition_update and
// definition_write: an added line whose logical command (definitionWrites)
// writes a release or build definition adds the command's class with the
// path and the command's type; removed lines carry none. In a .tf file a line takes the
// type of the block it sits in, and a catalogued type adds its class with
// that side's path and the type. In a pipeline definition a line's
// pipelineIdentity mark adds pipeline_identity with the path and the mark.
// In an EF Core migration or a .sql file only added lines are read, at head:
// each destructiveOperations operation on one adds destructive_migration with
// the path and the operation, and the merge base is never read for them.
// A changed line that sets variable X (an assignment in a .tfvars file, or a
// line in a variable "X" block's attributes other than description) adds the
// classes of the catalogued blocks in that directory's .tf files, at the same
// side, that reference var.X, with the changed path and "<type> via var.X".
// A class read on a line added at head (an altered line is one removed and
// one added) also adds each class that widens it, access_widened for
// identity_rights, with the same path and type; the merge-base side adds
// none, and neither does a line in a label attribute (label) or in a
// delivery gate (gate). In a .tf or
// .tfvars file a restyled line (restyledLines) is not a changed line at
// either side, and an inert one (blank or a full-line comment) carries no
// class and sets no variable. A catalogued resource block whose address leaves its directory adds
// its removal classes, stateful_destroy, with its merge-base path and type
// (removedBlocks). Every other text file is read only for secret_added: a
// line added at head that matches a secret rule adds it with the path and the
// rule name (collectSecrets). Nil when there are none.
//
// It also sets each changed pipeline definition's Environments in files: the
// job environments it names at each side it has changed lines on (head when
// lines were added, the merge base when removed), sorted and distinct.
func collectConsequences(ctx context.Context, repo, mergeBase, head string, files []File) ([]Consequence, error) {
	type side struct {
		path  string
		lines []int
		file  int // the index in files
	}
	var heads, bases []side
	// scripts are the head paths read for definition_update and
	// definition_write: neither docs nor tests.
	scripts := map[string]bool{}
	for i, f := range files {
		script := !f.Binary && !slices.Contains(f.Classes, ClassDocs) && !slices.Contains(f.Classes, ClassTest)
		if f.Binary || !(readable(f.Path) || readable(f.OldPath) || script) {
			continue
		}
		patch, err := filePatch(ctx, repo, mergeBase, head, f)
		if err != nil {
			return nil, err
		}
		old, new := hunkLines(patch)
		oldPath := f.Path
		if f.OldPath != "" {
			oldPath = f.OldPath
		}
		if hcl := func(p string) bool { return isTerraform(p) || isTfvars(p) }; hcl(f.Path) || hcl(oldPath) {
			restyledOld, restyledNew := restyledLines(patch)
			old, new = less(old, restyledOld), less(new, restyledNew)
		}
		if len(new) > 0 && (readable(f.Path) || script) {
			heads = append(heads, side{f.Path, new, i})
			if script {
				scripts[f.Path] = true
			}
		}
		if len(old) > 0 && readable(oldPath) && !readsDestructive(oldPath) {
			bases = append(bases, side{oldPath, old, i})
		}
	}

	hits := map[string]*struct{ paths, types map[string]bool }{}
	envs := map[int]map[string]bool{} // job environments by index in files
	record := func(class, p, typ string) {
		h := hits[class]
		if h == nil {
			h = &struct{ paths, types map[string]bool }{map[string]bool{}, map[string]bool{}}
			hits[class] = h
		}
		h.paths[p] = true
		h.types[typ] = true
	}
	for _, at := range []struct {
		commit string
		sides  []side
		added  bool
	}{{head, heads, true}, {mergeBase, bases, false}} {
		if len(at.sides) == 0 {
			continue
		}
		want, index := map[string][]int{}, map[string]int{}
		for _, s := range at.sides {
			want[s.path], index[s.path] = s.lines, s.file
		}
		all, err := treeBlobs(ctx, repo, at.commit)
		if err != nil {
			return nil, err
		}
		var blobs []blob
		for _, b := range all {
			if want[b.path] != nil {
				blobs = append(blobs, b)
			}
		}
		// widen is false for a line that cannot widen its class: one at the
		// merge base, in a label attribute, or in a delivery gate.
		hit := func(class, p, typ string, widen bool) {
			record(class, p, typ)
			if widen {
				for _, w := range widenedBy(class) {
					record(w, p, typ)
				}
			}
		}
		// vars records, by directory, each variable a changed line sets and
		// the changed paths that set it.
		vars := map[string]map[string]map[string]bool{}
		setVar := func(p, name string) {
			dir := path.Dir(p)
			if vars[dir] == nil {
				vars[dir] = map[string]map[string]bool{}
			}
			if vars[dir][name] == nil {
				vars[dir][name] = map[string]bool{}
			}
			vars[dir][name][p] = true
		}
		err = catBlobs(ctx, repo, blobs, func(p string, content []byte) {
			content = bytes.TrimSuffix(content, []byte("\n"))
			if at.added && scripts[p] {
				writes := definitionWrites(content)
				for _, n := range want[p] {
					if n >= 1 && n <= len(writes) && writes[n-1].typ != "" {
						hit(writes[n-1].class, p, writes[n-1].typ, at.added)
					}
				}
			}
			if readsDestructive(p) {
				ops := destructiveOperations(p, content)
				for _, n := range want[p] {
					if n >= 1 && n <= len(ops) {
						for _, op := range ops[n-1] {
							hit(ConsequenceDestructiveMigration, p, op, at.added)
						}
					}
				}
				return
			}
			if !readable(p) {
				return
			}
			if isTfvars(p) {
				names := tfvarsAssignments(content)
				for _, n := range want[p] {
					if n >= 1 && n <= len(names) && names[n-1] != "" {
						setVar(p, names[n-1])
					}
				}
				return
			}
			if !isTerraform(p) {
				marks, names := pipelineIdentity(content)
				for _, name := range names {
					if envs[index[p]] == nil {
						envs[index[p]] = map[string]bool{}
					}
					envs[index[p]][name] = true
				}
				for _, n := range want[p] {
					if n >= 1 && n <= len(marks) && marks[n-1] != "" {
						hit(ConsequencePipelineIdentity, p, marks[n-1], at.added)
					}
				}
				return
			}
			lines, text := terraformBlocks(content), strings.Split(string(content), "\n")
			for _, n := range want[p] {
				if n < 1 || n > len(lines) {
					continue
				}
				l := lines[n-1]
				if l.inert {
					continue
				}
				if l.variable != "" && l.attr != "" && l.attr != "description" {
					setVar(p, l.variable)
				}
				if l.block == "" {
					continue
				}
				widen := at.added && !label(l) && !inGate(lines, text, n-1)
				for _, class := range consequencesOf(l.block) {
					hit(class, p, l.block, widen)
				}
				for _, class := range attributeConsequencesOf(l.block, l.attr) {
					hit(class, p, l.block+"."+l.attr, widen)
				}
			}
		})
		if err != nil {
			return nil, err
		}
		if len(vars) == 0 {
			continue
		}
		// A changed variable carries the classes of the catalogued blocks
		// in the .tf files of its directory, at this side, that use it.
		var users []blob
		for _, b := range all {
			if isTerraform(b.path) && vars[path.Dir(b.path)] != nil {
				users = append(users, b)
			}
		}
		err = catBlobs(ctx, repo, users, func(p string, content []byte) {
			content = bytes.TrimSuffix(content, []byte("\n"))
			lines, text := terraformBlocks(content), strings.Split(string(content), "\n")
			for name, changed := range vars[path.Dir(p)] {
				for i, l := range lines {
					if l.block == "" || !refersTo(text[i], name) {
						continue
					}
					widen := at.added && !label(l) && !inGate(lines, text, i)
					for cp := range changed {
						for _, class := range consequencesOf(l.block) {
							hit(class, cp, l.block+" via var."+name, widen)
						}
						for _, class := range attributeConsequencesOf(l.block, l.attr) {
							hit(class, cp, l.block+"."+l.attr+" via var."+name, widen)
						}
					}
				}
			}
		})
		if err != nil {
			return nil, err
		}
	}
	for i, set := range envs {
		files[i].Environments = sorted(set)
	}
	if err := removedBlocks(ctx, repo, mergeBase, head, files, record); err != nil {
		return nil, err
	}
	if err := collectSecrets(ctx, repo, mergeBase, head, files, record); err != nil {
		return nil, err
	}
	if len(hits) == 0 {
		return nil, nil
	}
	out := make([]Consequence, 0, len(hits))
	for class, h := range hits {
		out = append(out, Consequence{Class: class, Paths: sorted(h.paths), Types: sorted(h.types)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Class < out[j].Class })
	return out, nil
}

// isTerraform reports whether a path is a Terraform configuration file.
func isTerraform(p string) bool { return strings.HasSuffix(p, ".tf") }

// isTfvars reports whether a path is a Terraform variable definitions file.
func isTfvars(p string) bool { return strings.HasSuffix(p, ".tfvars") }

// readable reports whether collectConsequences reads a path.
func readable(p string) bool {
	return isTerraform(p) || isTfvars(p) || isPipeline(p) || readsDestructive(p)
}

// plannedConsequences gives each class a planned path names that path, with
// no types. Nil when no path names one.
func plannedConsequences(files []PlannedFile) []Consequence {
	set := map[string]map[string]bool{}
	for _, f := range files {
		for _, c := range f.Consequences {
			if set[c] == nil {
				set[c] = map[string]bool{}
			}
			set[c][f.Path] = true
		}
	}
	if len(set) == 0 {
		return nil
	}
	out := make([]Consequence, 0, len(set))
	for class, paths := range set {
		out = append(out, Consequence{Class: class, Paths: sorted(paths), Types: []string{}})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Class < out[j].Class })
	return out
}
