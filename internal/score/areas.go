package score

import (
	"sort"

	"github.com/bobmcallan/riskelle/internal/facts"
)

// areas lists the sensitive areas the changed files land in: the built-in
// areas plus the rules' repo-added ones. A rename is checked at both its old
// and new path, so moving a file out of an area still touches it. Every file
// is checked, whatever its classes.
func areas(files []facts.File, r Rules) []Area {
	names := make([]string, 0, len(r.Areas))
	for name := range r.Areas {
		names = append(names, name)
	}
	sort.Strings(names)
	added := make([]facts.Globs, len(names))
	for i, name := range names {
		added[i] = facts.CompileGlobs(r.Areas[name])
	}

	hits := map[string]map[string]bool{}
	hit := func(name, p string) {
		if hits[name] == nil {
			hits[name] = map[string]bool{}
		}
		hits[name][p] = true
	}
	for _, file := range files {
		for _, p := range []string{file.Path, file.OldPath} {
			if p == "" {
				continue
			}
			for _, name := range facts.Areas(p) {
				hit(name, p)
			}
			for i, g := range added {
				if g.Match(p) {
					hit(names[i], p)
				}
			}
		}
	}
	if len(hits) == 0 {
		return nil
	}
	out := make([]Area, 0, len(hits))
	for name, set := range hits {
		a := Area{Name: name, Paths: make([]string, 0, len(set))}
		for p := range set {
			a.Paths = append(a.Paths, p)
		}
		sort.Strings(a.Paths)
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
