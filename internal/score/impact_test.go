package score

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/bobmcallan/riskelle/internal/facts"
)

// TestImpactBands drives the largest dependent count to each band's lower
// edge and the value just below it, through a changed package and through a
// changed file, which fall in the same bands.
func TestImpactBands(t *testing.T) {
	tests := []struct {
		dependents, wantMin, wantPoints int
	}{
		{0, 0, 0}, {2, 0, 0},
		{3, 3, 2}, {5, 3, 2},
		{6, 6, 4}, {10, 6, 4},
		{11, 11, 7}, {20, 11, 7},
		{21, 21, 10}, {1000, 21, 10},
	}
	for _, tt := range tests {
		for _, kind := range []string{"package", "file"} {
			t.Run(fmt.Sprintf("%s=%d", kind, tt.dependents), func(t *testing.T) {
				var f facts.Facts
				d := []facts.Dependency{{Name: "example.com/m/p", Dependents: tt.dependents}}
				if kind == "package" {
					f.Impact.Packages = d
				} else {
					d[0].Name = "web/x.js" // no File: planned facts may name it alone
					f.Impact.Files = d
				}
				p := entry(t, Score(f, Default()), MeasureImpact, "dependents")
				if p.Value != tt.dependents || p.Min != tt.wantMin || p.Points != tt.wantPoints {
					t.Errorf("value %d min %d points %d, want value %d min %d points %d",
						p.Value, p.Min, p.Points, tt.dependents, tt.wantMin, tt.wantPoints)
				}
			})
		}
	}
}

// TestImpactNamesEachDependency pins that the entry is valued at the widest
// reach and names every counted package and file with its own count.
func TestImpactNamesEachDependency(t *testing.T) {
	f := facts.Facts{Impact: facts.Impact{
		Packages: []facts.Dependency{{Name: "example.com/m/b", Dependents: 12}, {Name: "example.com/m/a", Dependents: 4}},
		Files:    []facts.Dependency{{Name: "web/x.js", Dependents: 7}},
	}}
	p := entry(t, Score(f, Default()), MeasureImpact, "dependents")
	if p.Value != 12 || p.Min != 11 || p.Points != 7 {
		t.Errorf("value %d min %d points %d, want 12, 11, 7", p.Value, p.Min, p.Points)
	}
	wantDeps := []facts.Dependency{
		{Name: "example.com/m/a", Dependents: 4},
		{Name: "example.com/m/b", Dependents: 12},
		{Name: "web/x.js", Dependents: 7},
	}
	if !reflect.DeepEqual(p.Dependents, wantDeps) {
		t.Errorf("dependents %v, want %v", p.Dependents, wantDeps)
	}
	if want := []string{"example.com/m/a", "example.com/m/b", "web/x.js"}; !reflect.DeepEqual(p.From, want) {
		t.Errorf("from %v, want %v", p.From, want)
	}
	b, _ := json.Marshal(p)
	want := `{"measure":"impact","fact":"dependents","value":12,"min":11,"points":7,` +
		`"from":["example.com/m/a","example.com/m/b","web/x.js"],` +
		`"dependents":[{"name":"example.com/m/a","dependents":4},{"name":"example.com/m/b","dependents":12},{"name":"web/x.js","dependents":7}]}`
	if string(b) != want {
		t.Errorf("json\n%s\nwant\n%s", b, want)
	}
}

func TestImpactEmpty(t *testing.T) {
	p := entry(t, Score(facts.Facts{}, Default()), MeasureImpact, "dependents")
	if p.Value != 0 || p.Points != 0 || p.From == nil || len(p.From) != 0 || p.Dependents != nil || p.Excluded != nil {
		t.Errorf("empty impact = %+v, want 0 points, empty non-nil from, no dependents", p)
	}
}

// TestImpactExcludesDocsGeneratedLock leaves docs, generated and lock files
// out, by the classes of the changed file (a rename's old path included).
func TestImpactExcludesDocsGeneratedLock(t *testing.T) {
	f := facts.Facts{
		Files: []facts.File{
			{Path: "README.md", Classes: []facts.Class{facts.ClassDocs}},
			{Path: "go.sum", Classes: []facts.Class{facts.ClassLock}},
			{Path: "web/bundle.min.js", Classes: []facts.Class{facts.ClassGenerated}},
			{Path: "docs/guide/new.md", OldPath: "docs/old.md", Classes: []facts.Class{facts.ClassDocs}},
			{Path: "ui/app.js"},
		},
		Impact: facts.Impact{
			Packages: []facts.Dependency{{Name: "example.com/m", Dependents: 1}},
			Files: []facts.Dependency{
				{Name: "README.md", Dependents: 40},
				{Name: "docs/old.md", Dependents: 50},
				{Name: "go.sum", Dependents: 30},
				{Name: "planned.txt", Dependents: 3}, // no File: counts
				{Name: "ui/app.js", Dependents: 4},
				{Name: "web/bundle.min.js", Dependents: 25},
			},
		},
	}
	p := entry(t, Score(f, Default()), MeasureImpact, "dependents")
	if p.Value != 4 || p.Min != 3 || p.Points != 2 {
		t.Errorf("value %d min %d points %d, want 4, 3, 2", p.Value, p.Min, p.Points)
	}
	if want := []string{"README.md", "docs/old.md", "go.sum", "web/bundle.min.js"}; !reflect.DeepEqual(p.Excluded, want) {
		t.Errorf("excluded %v, want %v", p.Excluded, want)
	}
	if want := []string{"example.com/m", "planned.txt", "ui/app.js"}; !reflect.DeepEqual(p.From, want) {
		t.Errorf("from %v, want %v", p.From, want)
	}
}

// TestImpactInTotal drives the total across level edges with impact points,
// and to the top of every built-in band scored so far.
func TestImpactInTotal(t *testing.T) {
	tests := []struct {
		lines, files, dirs, dependents int
		wantTotal                      int
		want                           Level
	}{
		{201, 6, 3, 10, 18, Low},       // 10+2+2+4
		{201, 6, 3, 11, 21, Medium},    // 10+2+2+7
		{801, 1, 0, 20, 37, Medium},    // 30+7
		{801, 1, 0, 21, 40, High},      // 30+10
		{10000, 16, 9, 1000, 54, High}, // 30+8+6+10, no projects
	}
	for _, tt := range tests {
		f := facts.Facts{Directories: names("d", tt.dirs)}
		f.Files = []facts.File{{Path: "main.go", Added: tt.lines}}
		for _, p := range names("f", tt.files-1) {
			f.Files = append(f.Files, facts.File{Path: p})
		}
		f.Impact.Packages = []facts.Dependency{{Name: "example.com/m", Dependents: tt.dependents}}
		r := Score(f, Default())
		if r.Total != tt.wantTotal || r.Level != tt.want {
			t.Errorf("%+v: total %d level %q, want %d %q", tt, r.Total, r.Level, tt.wantTotal, tt.want)
		}
	}
}

func TestImpactIsDeterministicAndOwnsItsSlices(t *testing.T) {
	build := func() facts.Facts {
		return facts.Facts{Impact: facts.Impact{
			Packages: []facts.Dependency{{Name: "example.com/m/a", Dependents: 2}, {Name: "example.com/m/b", Dependents: 9}},
			Files:    []facts.Dependency{{Name: "a.txt", Dependents: 1}, {Name: "z.txt", Dependents: 3}},
		}}
	}
	f := build()
	a, _ := json.Marshal(Score(f, Default()))
	rev := build()
	slices.Reverse(rev.Impact.Packages)
	slices.Reverse(rev.Impact.Files)
	b, _ := json.Marshal(Score(rev, Default()))
	if string(a) != string(b) {
		t.Errorf("input order changes the result:\n%s\n%s", a, b)
	}

	r := Score(f, Default())
	f.Impact.Packages[0].Dependents = 99
	if got := entry(t, r, MeasureImpact, "dependents").Dependents[1]; got.Name != "example.com/m/a" || got.Dependents != 2 {
		t.Errorf("result changed with its input: %+v", got)
	}
}

func TestDefaultImpactRulesFresh(t *testing.T) {
	r := Default()
	r.ImpactExcludes[0] = facts.ClassTest
	r.ImpactDependents[1].Points = 99
	again := Default()
	if again.ImpactExcludes[0] != facts.ClassDocs || again.ImpactDependents[1].Points != 2 {
		t.Errorf("changing one Default() changed the next: %+v", again)
	}
	if top := again.ImpactDependents[len(again.ImpactDependents)-1].Points; top != 10 {
		t.Errorf("impact top band %d points, want its budget of 10", top)
	}
}
