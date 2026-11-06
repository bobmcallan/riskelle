package facts

import (
	"bytes"
	"context"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// The built-in consequence classes. A consequence is what a change does, read
// from its changed lines, not where it lands: a class sets a floor on the
// level the change is reported at.
const (
	ConsequenceIdentityRights = "identity_rights"
)

// Consequence is one class a change carries, the changed or planned paths
// that triggered it and the block types that did. Both lists are sorted,
// distinct and non-nil; Types is empty for a plan, which has no content.
type Consequence struct {
	Class string   `json:"class"`
	Paths []string `json:"paths"`
	Types []string `json:"types"`
}

// builtinConsequences is the one catalog of consequence classes: for each, the
// globs over Terraform resource and data types that carry it. Sorted by class
// and versioned with each release, beside builtin (classes.go) and
// builtinAreas (areas.go).
var builtinConsequences = []struct {
	class    string
	patterns []string
}{
	{ConsequenceIdentityRights, []string{
		"aws_iam_*",
		"azuread_app_role_assignment",
		"azuread_application*",
		"azuread_directory_role*",
		"azuread_service_principal*",
		"azurerm_federated_identity_credential",
		"azurerm_role_assignment",
		"azurerm_role_definition",
		"azurerm_user_assigned_identity",
		"google_*_iam_*",
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

// blockHeader matches a resource or data block header at column 0.
var blockHeader = regexp.MustCompile(`^(?:resource|data)[ \t]+"?([A-Za-z][A-Za-z0-9_-]*)"?`)

// terraformBlocks returns, for each line of content (index 0 is line 1), the
// resource or data type of the top-level block it belongs to, or "" when none.
// A block is found by its header at column 0, as terraform fmt writes every
// file: a column-0 line other than "}", or a "#", "//" or "/*" comment, opens
// a new block, and a column-0 "}" closes the open one. Indented and blank
// lines and column-0 comments belong to the open block, and so does a
// heredoc's body, up to its closing marker, wherever it starts: an IAM policy
// is often a heredoc of JSON at column 0. No braces are counted, so braces in
// strings and heredocs cannot mislead it.
func terraformBlocks(content []byte) []string {
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
		case l == "" || l[0] == ' ' || l[0] == '\t' ||
			strings.HasPrefix(l, "#") || strings.HasPrefix(l, "//") || strings.HasPrefix(l, "/*"):
			out[i] = open
		case l[0] == '}':
			out[i] = open
			open = ""
		default:
			open = ""
			if m := blockHeader.FindStringSubmatch(l); m != nil {
				open = m[1]
			}
			out[i] = open
			if strings.HasSuffix(strings.TrimRight(l, " \t\r"), "}") {
				open = "" // a one-line block
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

// collectConsequences finds the consequence classes in the .tf files of a
// change: each changed line takes the type of the block it sits in, at head
// for added lines and at the merge base for removed ones, and a catalogued
// type adds its class with that side's path and the type. Only files whose
// path ends in .tf are read. Nil when there are none.
func collectConsequences(ctx context.Context, repo, mergeBase, head string, files []File) ([]Consequence, error) {
	type side struct {
		path  string
		lines []int
	}
	var heads, bases []side
	for _, f := range files {
		if f.Binary || !(isTerraform(f.Path) || isTerraform(f.OldPath)) {
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
		if len(new) > 0 && isTerraform(f.Path) {
			heads = append(heads, side{f.Path, new})
		}
		if len(old) > 0 && isTerraform(oldPath) {
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
		err = catBlobs(ctx, repo, blobs, func(p string, content []byte) {
			blocks := terraformBlocks(bytes.TrimSuffix(content, []byte("\n")))
			for _, n := range want[p] {
				if n < 1 || n > len(blocks) || blocks[n-1] == "" {
					continue
				}
				for _, class := range consequencesOf(blocks[n-1]) {
					h := hits[class]
					if h == nil {
						h = &struct{ paths, types map[string]bool }{map[string]bool{}, map[string]bool{}}
						hits[class] = h
					}
					h.paths[p] = true
					h.types[blocks[n-1]] = true
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
