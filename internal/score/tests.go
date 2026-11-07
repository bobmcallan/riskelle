package score

import (
	"path"
	"sort"

	"github.com/bobmcallan/riskelle/internal/facts"
)

// untested scores production code files changed without their test. A
// production file is a kept file (not docs, generated, lock or test); a test
// change is any changed file classed test, whatever its other classes. Three
// cases, by the file's language:
//   - a paired language (facts.HasPairing) counts unless a changed test pairs
//     with it: the test's subject (facts.TestSubject) is the file's base name,
//     in any directory;
//   - an unpaired language (facts.UnpairedLanguage: Shell, Terraform) is tested
//     when any file at all is classed test, and otherwise counts only when
//     langs, the unpaired languages with a test in the head tree, holds its
//     language: a repo with no tests in that language has no way to write one;
//   - any other file (YAML, JSON, XML, other config or data, an extensionless
//     file, a language with no row) is not code riskelle can test and never
//     counts.
//
// The value is the number of untested production files and From lists them,
// sorted; with none, the value is 0 and From is empty.
func untested(all, kept []facts.File, langs []string, r Rules) Point {
	p := Point{Measure: MeasureTests, Fact: "untested_production_files", From: []string{}}
	subjects, anyTest := testSubjects(all)
	for _, file := range kept {
		if facts.HasPairing(file.Path) {
			if subjects[path.Base(file.Path)] {
				continue
			}
		} else if lang := facts.UnpairedLanguage(file.Path); lang == "" || anyTest || !contains(langs, lang) {
			continue
		}
		p.From = append(p.From, file.Path)
	}
	sort.Strings(p.From)
	p.Value = len(p.From)
	band := r.TestsUntested.at(p.Value)
	p.Min, p.Points = band.Min, band.Points
	return p
}

// testSubjects returns the base names of the production files the changed
// tests pair with, and whether any changed file is classed test.
func testSubjects(files []facts.File) (map[string]bool, bool) {
	subjects := map[string]bool{}
	anyTest := false
	for _, file := range files {
		for _, c := range file.Classes {
			if c == facts.ClassTest {
				subjects[facts.TestSubject(file.Path)] = true
				anyTest = true
				break
			}
		}
	}
	return subjects, anyTest
}

// contains reports whether list holds s.
func contains(list []string, s string) bool {
	for _, l := range list {
		if l == s {
			return true
		}
	}
	return false
}
