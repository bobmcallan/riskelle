package score

import (
	"sort"

	"github.com/bobmcallan/riskelle/internal/facts"
)

// MeasureImpact names the impact measure: how many places depend on what
// changed.
const MeasureImpact = "impact"

// impact scores the widest reach of the change: the largest dependent count
// among the changed Go packages and files. It takes the largest, not the
// sum, because the facts count dependents per item rather than their union
// (a sum would count shared dependents twice) and how many items changed is
// spread's to score. Every counted package and file is named with its count.
//
// A changed file in one of the rules' excluded classes (docs, generated,
// lock) does not count; it is listed in Excluded. A rename's old path takes
// the classes of its file. A file name with no File in the facts, as planned
// facts may have, counts. Packages are never excluded.
func impact(f facts.Facts, r Rules) Point {
	excluded := map[facts.Class]bool{}
	for _, c := range r.ImpactExcludes {
		excluded[c] = true
	}
	classes := map[string][]facts.Class{}
	for _, file := range f.Files {
		classes[file.Path] = file.Classes
		if file.OldPath != "" {
			classes[file.OldPath] = file.Classes
		}
	}

	p := Point{Measure: MeasureImpact, Fact: "dependents", From: []string{}}
	count := func(d facts.Dependency) {
		p.Dependents = append(p.Dependents, d)
		p.From = append(p.From, d.Name)
		p.Value = max(p.Value, d.Dependents)
	}
	for _, d := range f.Impact.Packages {
		count(d)
	}
	for _, d := range f.Impact.Files {
		if anyExcluded(classes[d.Name], excluded) {
			p.Excluded = append(p.Excluded, d.Name)
			continue
		}
		count(d)
	}
	sort.Strings(p.From)
	sort.Strings(p.Excluded)
	sort.Slice(p.Dependents, func(i, j int) bool { return p.Dependents[i].Name < p.Dependents[j].Name })
	band := r.ImpactDependents.at(p.Value)
	p.Min, p.Points = band.Min, band.Points
	return p
}
