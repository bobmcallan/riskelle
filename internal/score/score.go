// Package score turns a change's facts into its risk score. Score is a pure,
// deterministic function of facts and rules: no file, network or clock, so the
// same facts and rules always give the same result, whether the facts were
// collected from git or predicted from a plan.
package score

import (
	"sort"

	"github.com/bobmcallan/riskelle/internal/facts"
)

// Level is the band a total is reported in.
type Level string

// The levels, lowest risk first.
const (
	Low    Level = "Low"
	Medium Level = "Medium"
	High   Level = "High"
)

// Measure names.
const (
	MeasureSize   = "size"
	MeasureSpread = "spread"
)

// Point is one entry of the breakdown: the band one fact of one measure fell
// in, and the facts that produced it.
type Point struct {
	Measure  string   `json:"measure"`            // "size" or "spread"
	Fact     string   `json:"fact"`               // "lines", "files", "directories" or "projects"
	Value    int      `json:"value"`              // the fact's value
	Min      int      `json:"min"`                // lower edge of the band it fell in
	Points   int      `json:"points"`             // the band's points
	From     []string `json:"from"`               // paths, directories or projects counted; sorted, non-nil
	Excluded []string `json:"excluded,omitempty"` // size only: paths left out by class; sorted
}

// Result is a scored change.
type Result struct {
	Total     int     `json:"total"` // 0-100
	Level     Level   `json:"level"`
	Breakdown []Point `json:"breakdown"` // size/lines, then spread/files, directories, projects
}

// Score scores the facts of one change against rules. Every breakdown entry is
// kept, 0 points included, in a fixed order; later measures append theirs.
func Score(f facts.Facts, r Rules) Result {
	var paths []string
	for _, file := range f.Files {
		paths = append(paths, file.Path)
	}
	breakdown := []Point{
		size(f.Files, r),
		point(MeasureSpread, "files", paths, r.SpreadFiles),
		point(MeasureSpread, "directories", f.Directories, r.SpreadDirs),
		point(MeasureSpread, "projects", f.Projects, r.SpreadProjs),
	}

	total := 0
	for _, p := range breakdown {
		total += p.Points
	}
	total = max(0, min(total, 100))
	return Result{Total: total, Level: level(total, r.Levels), Breakdown: breakdown}
}

// size sums the lines added and removed in every file in none of the rules'
// excluded classes.
func size(files []facts.File, r Rules) Point {
	excluded := map[facts.Class]bool{}
	for _, c := range r.SizeExcludes {
		excluded[c] = true
	}
	p := Point{Measure: MeasureSize, Fact: "lines", From: []string{}}
	for _, file := range files {
		if anyExcluded(file.Classes, excluded) {
			p.Excluded = append(p.Excluded, file.Path)
			continue
		}
		p.Value += file.Added + file.Removed
		p.From = append(p.From, file.Path)
	}
	sort.Strings(p.From)
	sort.Strings(p.Excluded)
	band := r.SizeLines.at(p.Value)
	p.Min, p.Points = band.Min, band.Points
	return p
}

func anyExcluded(classes []facts.Class, excluded map[facts.Class]bool) bool {
	for _, c := range classes {
		if excluded[c] {
			return true
		}
	}
	return false
}

// point scores a count of items, keeping a sorted copy of them.
func point(measure, fact string, items []string, bands Bands) Point {
	from := append([]string{}, items...)
	sort.Strings(from)
	band := bands.at(len(from))
	return Point{Measure: measure, Fact: fact, Value: len(from), Min: band.Min, Points: band.Points, From: from}
}

// level returns the level of the last edge the total reaches.
func level(total int, edges []LevelEdge) Level {
	var got Level
	for _, e := range edges {
		if total >= e.Min {
			got = e.Level
		}
	}
	return got
}
