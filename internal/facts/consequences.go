package facts

import (
	"bytes"
	"context"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// The built-in consequence classes. A consequence is what a change does, read
// from its changed lines, not where it lands: a class sets a floor on the
// level the change is reported at.
const (
	ConsequenceIdentityRights   = "identity_rights"
	ConsequencePipelineIdentity = "pipeline_identity"
)

// Consequence is one class a change carries, the changed or planned paths
// that triggered it and the Terraform block types (with ".<attribute>" when
// an attribute of the block carried it, and " via var.<name>" when a changed
// variable that the block uses did) or pipeline keys that did. Both lists are sorted,
// distinct and non-nil; Types is empty for a plan, which has no content.
type Consequence struct {
	Class string   `json:"class"`
	Paths []string `json:"paths"`
	Types []string `json:"types"`
}

// builtinConsequences is the one catalog of consequence classes: for each, the
// globs over Terraform resource and data types that carry it, or the pipeline
// keys that do (pipelines.go). Sorted by class
// and versioned with each release, beside builtin (classes.go) and
// builtinAreas (areas.go).
//
// identity_rights includes delivery rights: service connections and their
// authorisations, deployment environments and their checks, branch policies
// and protections, and permissions decide who can act as a deploy identity,
// as a federated credential does. Generic secret and variable stores do not.
var builtinConsequences = []struct {
	class      string
	patterns   []string
	keys       []string
	attributes map[string][]string // exact type: the top-level attributes whose lines carry the class
}{
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
}

type compiledConsequence struct {
	class string
	globs Globs
}

// compiledConsequences holds builtinConsequences compiled once.
var compiledConsequences = func() []compiledConsequence {
	out := make([]compiledConsequence, 0, len(builtinConsequences))
	for _, c := range builtinConsequences {
		out = append(out, compiledConsequence{c.class, CompileGlobs(c.patterns)})
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
		case strings.TrimSpace(l) == "" || strings.HasPrefix(strings.TrimSpace(l), "#") ||
			strings.HasPrefix(strings.TrimSpace(l), "//") || strings.HasPrefix(strings.TrimSpace(l), "/*"):
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
	t := strings.TrimSpace(line)
	if strings.HasPrefix(t, "#") || strings.HasPrefix(t, "//") || strings.HasPrefix(t, "/*") {
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
var blockHeader = regexp.MustCompile(`^(?:resource|data)[ \t]+"?([A-Za-z][A-Za-z0-9_-]*)"?`)

// tfLine is one line of a Terraform file: the resource or data type of the
// top-level block it belongs to, and the block's top-level attribute it falls
// in ("" when none). variable names the column-0 variable "X" block a line
// belongs to, whose attributes are tracked the same way; "" otherwise.
type tfLine struct{ block, attr, variable string }

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
	open, vname, heredoc, attr, col := "", "", "", "", -1
	for i, l := range lines {
		trimmed := strings.TrimLeft(l, " \t")
		switch {
		case heredoc != "":
			out[i] = tfLine{open, attr, vname}
			if strings.TrimSpace(l) == heredoc {
				heredoc = ""
			}
			continue
		case strings.TrimSpace(l) == "" || strings.HasPrefix(l, "#") || strings.HasPrefix(l, "//") || strings.HasPrefix(l, "/*"):
			out[i] = tfLine{open, "", vname}
		case l[0] == ' ' || l[0] == '\t':
			indent := len(l) - len(trimmed)
			if col < 0 {
				col = indent
			}
			tracked := open != "" || vname != ""
			switch {
			case !tracked || indent > col:
			case strings.ContainsAny(trimmed[:1], "])}"):
				out[i] = tfLine{open, attr, vname}
				attr = ""
				continue
			default:
				attr = tfAttr.FindString(trimmed)
			}
			if !tracked {
				attr = ""
			}
			out[i] = tfLine{open, attr, vname}
		case l[0] == '}':
			out[i] = tfLine{open, "", vname}
			open, vname, attr, col = "", "", "", -1
		default:
			open, vname, attr, col = "", "", "", -1
			if m := blockHeader.FindStringSubmatch(l); m != nil {
				open = m[1]
			} else if m := variableHeader.FindStringSubmatch(l); m != nil {
				vname = m[1]
			}
			out[i] = tfLine{open, "", vname}
			if strings.HasSuffix(strings.TrimRight(l, " \t\r"), "}") {
				open, vname = "", "" // a one-line block
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

// collectConsequences finds the consequence classes in the .tf files and
// pipeline definitions of a change, reading each changed line at head when
// added and at the merge base when removed. In a .tf file a line takes the
// type of the block it sits in, and a catalogued type adds its class with
// that side's path and the type. In a pipeline definition a line's
// pipelineIdentity mark adds pipeline_identity with the path and the mark.
// A changed line that sets variable X (an assignment in a .tfvars file, or a
// line in a variable "X" block's attributes other than description) adds the
// classes of the catalogued blocks in that directory's .tf files, at the same
// side, that reference var.X, with the changed path and "<type> via var.X".
// No other file is read. Nil when there are none.
func collectConsequences(ctx context.Context, repo, mergeBase, head string, files []File) ([]Consequence, error) {
	type side struct {
		path  string
		lines []int
	}
	var heads, bases []side
	for _, f := range files {
		if f.Binary || !(readable(f.Path) || readable(f.OldPath)) {
			continue
		}
		args := []string{"--literal-pathspecs", "--attr-source=" + head, "diff", "-U0"}
		args = append(args, pinnedDiff...)
		args = append(args, mergeBase, head, "--")
		if f.OldPath != "" {
			args = append(args, f.OldPath)
		}
		patch, err := git(ctx, repo, append(args, f.Path)...)
		if err != nil {
			return nil, err
		}
		old, new := hunkLines(patch)
		oldPath := f.Path
		if f.OldPath != "" {
			oldPath = f.OldPath
		}
		if len(new) > 0 && readable(f.Path) {
			heads = append(heads, side{f.Path, new})
		}
		if len(old) > 0 && readable(oldPath) {
			bases = append(bases, side{oldPath, old})
		}
	}

	hits := map[string]*struct{ paths, types map[string]bool }{}
	for _, at := range []struct {
		commit string
		sides  []side
	}{{head, heads}, {mergeBase, bases}} {
		if len(at.sides) == 0 {
			continue
		}
		want := map[string][]int{}
		for _, s := range at.sides {
			want[s.path] = s.lines
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
		hit := func(class, p, typ string) {
			h := hits[class]
			if h == nil {
				h = &struct{ paths, types map[string]bool }{map[string]bool{}, map[string]bool{}}
				hits[class] = h
			}
			h.paths[p] = true
			h.types[typ] = true
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
				marks := pipelineIdentity(content)
				for _, n := range want[p] {
					if n >= 1 && n <= len(marks) && marks[n-1] != "" {
						hit(ConsequencePipelineIdentity, p, marks[n-1])
					}
				}
				return
			}
			lines := terraformBlocks(content)
			for _, n := range want[p] {
				if n < 1 || n > len(lines) {
					continue
				}
				if v := lines[n-1]; v.variable != "" && v.attr != "" && v.attr != "description" {
					setVar(p, v.variable)
				}
				if lines[n-1].block == "" {
					continue
				}
				l := lines[n-1]
				for _, class := range consequencesOf(l.block) {
					hit(class, p, l.block)
				}
				for _, class := range attributeConsequencesOf(l.block, l.attr) {
					hit(class, p, l.block+"."+l.attr)
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
					for cp := range changed {
						for _, class := range consequencesOf(l.block) {
							hit(class, cp, l.block+" via var."+name)
						}
						for _, class := range attributeConsequencesOf(l.block, l.attr) {
							hit(class, cp, l.block+"."+l.attr+" via var."+name)
						}
					}
				}
			}
		})
		if err != nil {
			return nil, err
		}
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
func readable(p string) bool { return isTerraform(p) || isTfvars(p) || isPipeline(p) }

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
