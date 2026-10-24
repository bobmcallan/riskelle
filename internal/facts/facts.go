// Package facts holds the facts about a change that riskelle scores, and
// collects them from git. The scorer is a pure function of these facts, so the
// same facts can also be predicted from a plan.
package facts

import (
	"path"
	"sort"
	"strings"
)

// File is one changed file and its line counts.
type File struct {
	Path    string `json:"path"`               // path at head, slash-separated, repo-relative
	OldPath string `json:"old_path,omitempty"` // path at base when renamed
	Added   int    `json:"added"`
	Removed int    `json:"removed"`
	Binary  bool   `json:"binary,omitempty"` // git reported no line counts; Added and Removed are 0
}

// Facts are the collected facts about one change. Every slice is sorted and
// non-nil.
type Facts struct {
	Base        string   `json:"base"`       // full commit SHA the base ref resolved to
	MergeBase   string   `json:"merge_base"` // full SHA the diff is taken from
	Head        string   `json:"head"`       // full commit SHA the head ref resolved to
	Files       []File   `json:"files"`
	Directories []string `json:"directories"`
	Projects    []string `json:"projects"`

	// Commit history, never the clock.
	BranchAgeDays int `json:"branch_age_days"` // whole days from the branch's first commit to head; 0 with no commits of its own
	TrunkCommits  int `json:"trunk_commits"`   // commits on base since the merge base
}

// IsManifest reports whether a file's base name marks its directory as a
// project: go.mod, package.json or a .csproj.
func IsManifest(name string) bool {
	return name == "go.mod" || name == "package.json" || strings.HasSuffix(name, ".csproj")
}

// Directories returns the distinct, sorted parent directories of paths, with
// the repository root as ".".
func Directories(paths []string) []string {
	set := map[string]bool{}
	for _, p := range paths {
		set[path.Dir(p)] = true
	}
	return sorted(set)
}

// Projects returns the distinct, sorted project roots that paths belong to.
// Each path belongs to its nearest ancestor directory in roots (its own
// directory included); a path under no root belongs to no project.
func Projects(paths, roots []string) []string {
	rootSet := map[string]bool{}
	for _, r := range roots {
		rootSet[r] = true
	}
	set := map[string]bool{}
	for _, p := range paths {
		for d := path.Dir(p); ; d = path.Dir(d) {
			if rootSet[d] {
				set[d] = true
				break
			}
			if d == "." || d == "/" {
				break
			}
		}
	}
	return sorted(set)
}

func sorted(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
