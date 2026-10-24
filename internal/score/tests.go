package score

import (
	"sort"

	"github.com/bobmcallan/riskelle/internal/facts"
)

// untested scores a production change with no test change. A production file
// is a kept file (not docs, generated or lock) that is not a test; a test
// change is any changed file classed test, whatever its other classes. With
// production files and no test change, the value is the number of production
// files and From lists them; otherwise the value is 0 and From is empty.
func untested(all, kept []facts.File, r Rules) Point {
	p := Point{Measure: MeasureTests, Fact: "untested_production_files", From: []string{}}
	if !anyTest(all) {
		for _, file := range kept {
			p.From = append(p.From, file.Path)
		}
		sort.Strings(p.From)
		p.Value = len(p.From)
	}
	band := r.TestsUntested.at(p.Value)
	p.Min, p.Points = band.Min, band.Points
	return p
}

func anyTest(files []facts.File) bool {
	for _, file := range files {
		for _, c := range file.Classes {
			if c == facts.ClassTest {
				return true
			}
		}
	}
	return false
}
