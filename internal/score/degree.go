package score

import (
	"sort"

	"github.com/bobmcallan/riskelle/internal/facts"
)

// degree scores how much existing code the change alters: the lines added and
// removed in kept files that existed at the merge base. Lines in new files
// score on size only, so altering existing code costs more than writing the
// same lines fresh.
func degree(kept []facts.File, r Rules) Point {
	d := facts.DegreeOf(kept)
	p := Point{Measure: MeasureDegree, Fact: "existing_lines", Value: d.Existing.Added + d.Existing.Removed, From: []string{}}
	for _, file := range kept {
		if !file.New {
			p.From = append(p.From, file.Path)
		}
	}
	sort.Strings(p.From)
	band := r.DegreeExisting.at(p.Value)
	p.Min, p.Points = band.Min, band.Points
	return p
}
