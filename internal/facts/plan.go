package facts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
)

// Plan is a planned change, from which Predict derives its facts. The file
// format is described in README.md, "Plan file".
type Plan struct {
	Target        string
	BranchAgeDays int
	Files         []PlannedFile
}

// PlannedFile is one path a plan expects to touch and its expected lines.
type PlannedFile struct {
	Path           string
	Added, Removed int
}

// planJSON is the file's JSON; pointers tell an absent field from a zero one.
type planJSON struct {
	Target        string `json:"target"`
	BranchAgeDays int    `json:"branch_age_days"`
	Files         *[]struct {
		Path    string `json:"path"`
		Added   *int   `json:"added"`
		Removed *int   `json:"removed"`
	} `json:"files"`
}

// ReadPlan decodes and validates a plan file, refusing an invalid one with
// every problem named in a fixed order. It reads no git and no clock.
func ReadPlan(r io.Reader) (Plan, error) {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	var pj planJSON
	if err := dec.Decode(&pj); errors.Is(err, io.EOF) {
		return Plan{}, errors.New("empty plan file")
	} else if err != nil {
		return Plan{}, fmt.Errorf("not valid JSON: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return Plan{}, errors.New("not valid JSON: data after the plan object")
	}

	p := Plan{Target: pj.Target, BranchAgeDays: pj.BranchAgeDays}
	var problems []string
	bad := func(format string, a ...any) { problems = append(problems, fmt.Sprintf(format, a...)) }
	if p.Target == "" {
		bad(`missing "target"`)
	}
	if p.BranchAgeDays < 0 {
		bad(`"branch_age_days" is negative`)
	}
	if pj.Files == nil {
		bad(`missing "files"`)
	} else if len(*pj.Files) == 0 {
		bad(`"files" is empty`)
	}
	seen := map[string]bool{}
	for i, f := range derefOr(pj.Files) {
		if f.Path == "" {
			bad(`files[%d]: missing "path"`, i)
			continue
		}
		at := fmt.Sprintf("files[%d] %q", i, f.Path)
		if !cleanRelative(f.Path) {
			bad("%s: not a clean, repo-relative, slash-separated path", at)
		}
		if seen[f.Path] {
			bad("%s: duplicate path", at)
		}
		seen[f.Path] = true
		if f.Added == nil && f.Removed == nil {
			bad(`%s: missing "added" or "removed"`, at)
		}
		pf := PlannedFile{Path: f.Path, Added: derefOr(f.Added), Removed: derefOr(f.Removed)}
		if pf.Added < 0 {
			bad(`%s: "added" is negative`, at)
		}
		if pf.Removed < 0 {
			bad(`%s: "removed" is negative`, at)
		}
		p.Files = append(p.Files, pf)
	}
	if len(problems) > 0 {
		return Plan{}, errors.New(strings.Join(problems, "; "))
	}
	return p, nil
}

// derefOr returns *p, or the zero value when p is nil.
func derefOr[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}

// cleanRelative reports whether p is a clean, slash-separated path below the
// repository root.
func cleanRelative(p string) bool {
	return !strings.HasPrefix(p, "/") && !strings.Contains(p, `\`) &&
		path.Clean(p) == p && p != "." && p != ".." && !strings.HasPrefix(p, "../")
}

// Predict turns a plan into the facts Collect would gather for the change,
// with the same helpers, reading only the target commit: its tree says which
// planned paths are new and holds the project manifests, and impact is counted
// there. Base is the target, so rules are read there as for a real diff. A
// plan has no commits of its own: MergeBase and Head are empty and
// TrunkCommits is 0, as trunk drift cannot be planned.
func Predict(ctx context.Context, repo string, p Plan) (Facts, error) {
	target, err := resolve(ctx, repo, p.Target)
	if err != nil {
		return Facts{}, err
	}
	tree, err := treePaths(ctx, repo, target)
	if err != nil {
		return Facts{}, err
	}
	atTarget := map[string]bool{}
	for _, t := range tree {
		atTarget[t] = true
	}
	files, planned := make([]File, 0, len(p.Files)), make([]string, 0, len(p.Files))
	for _, pf := range p.Files {
		files = append(files, File{Path: pf.Path, Added: pf.Added, Removed: pf.Removed, Classes: Classify(pf.Path)})
		planned = append(planned, pf.Path)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	markNew(files, atTarget)

	roots := map[string]bool{}
	for _, r := range append(manifestRoots(tree), manifestRoots(planned)...) {
		roots[r] = true
	}
	impact, err := CollectImpact(ctx, repo, target, planned)
	if err != nil {
		return Facts{}, err
	}
	return Facts{
		Base: target, Files: files,
		Directories: Directories(planned), Projects: Projects(planned, sorted(roots)),
		Degree: DegreeOf(files), Impact: impact, BranchAgeDays: p.BranchAgeDays,
	}, nil
}
