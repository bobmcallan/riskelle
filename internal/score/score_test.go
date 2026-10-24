package score

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/bobmcallan/riskelle/internal/facts"
)

// mixed is a change touching code, tests, docs, a lock file and generated
// code across two projects.
func mixed() facts.Facts {
	return facts.Facts{
		Files: []facts.File{
			{Path: "README.md", Added: 500, Classes: []facts.Class{facts.ClassDocs}},
			{Path: "api/x.pb.go", Added: 400, Classes: []facts.Class{facts.ClassGenerated}},
			{Path: "go.sum", Added: 300, Classes: []facts.Class{facts.ClassLock}},
			{Path: "main.go", Added: 30, Removed: 10},
			{Path: "main_test.go", Added: 15, Classes: []facts.Class{facts.ClassTest}},
			{Path: "ui/app.js", Added: 200, Removed: 20, New: true},
		},
		Directories: []string{".", "api", "ui"},
		Projects:    []string{".", "ui"},
	}
}

func entry(t *testing.T, r Result, measure, fact string) Point {
	t.Helper()
	for _, p := range r.Breakdown {
		if p.Measure == measure && p.Fact == fact {
			return p
		}
	}
	t.Fatalf("no breakdown entry %s/%s in %+v", measure, fact, r.Breakdown)
	return Point{}
}

func names(prefix string, n int) []string {
	out := []string{}
	for i := 0; i < n; i++ {
		out = append(out, fmt.Sprintf("%s%03d", prefix, i))
	}
	return out
}

func TestBands(t *testing.T) {
	type row struct {
		measure, fact string
		value         int
		wantMin       int
		wantPoints    int
	}
	// Each band's lower edge and the value just below it.
	tests := []row{
		{"size", "lines", 0, 0, 0}, {"size", "lines", 50, 0, 0},
		{"size", "lines", 51, 51, 5}, {"size", "lines", 200, 51, 5},
		{"size", "lines", 201, 201, 10}, {"size", "lines", 400, 201, 10},
		{"size", "lines", 401, 401, 20}, {"size", "lines", 800, 401, 20},
		{"size", "lines", 801, 801, 30},
		{"spread", "files", 0, 0, 0}, {"spread", "files", 5, 0, 0},
		{"spread", "files", 6, 6, 2}, {"spread", "files", 10, 6, 2},
		{"spread", "files", 11, 11, 4}, {"spread", "files", 15, 11, 4},
		{"spread", "files", 16, 16, 8},
		{"spread", "directories", 0, 0, 0}, {"spread", "directories", 2, 0, 0},
		{"spread", "directories", 3, 3, 2}, {"spread", "directories", 4, 3, 2},
		{"spread", "directories", 5, 5, 4}, {"spread", "directories", 8, 5, 4},
		{"spread", "directories", 9, 9, 6},
		{"spread", "projects", 0, 0, 0}, {"spread", "projects", 1, 0, 0},
		{"spread", "projects", 2, 2, 3}, {"spread", "projects", 3, 3, 6},
		{"degree", "existing_lines", 0, 0, 0}, {"degree", "existing_lines", 25, 0, 0},
		{"degree", "existing_lines", 26, 26, 2}, {"degree", "existing_lines", 100, 26, 2},
		{"degree", "existing_lines", 101, 101, 5}, {"degree", "existing_lines", 250, 101, 5},
		{"degree", "existing_lines", 251, 251, 10}, {"degree", "existing_lines", 500, 251, 10},
		{"degree", "existing_lines", 501, 501, 15},
		{"branch_age", "days", 0, 0, 0}, {"branch_age", "days", 2, 0, 0},
		{"branch_age", "days", 3, 3, 3}, {"branch_age", "days", 7, 3, 3},
		{"branch_age", "days", 8, 8, 6}, {"branch_age", "days", 14, 8, 6},
		{"branch_age", "days", 15, 15, 10},
		{"branch_age", "trunk_commits", 0, 0, 0}, {"branch_age", "trunk_commits", 10, 0, 0},
		{"branch_age", "trunk_commits", 11, 11, 2}, {"branch_age", "trunk_commits", 50, 11, 2},
		{"branch_age", "trunk_commits", 51, 51, 3}, {"branch_age", "trunk_commits", 100, 51, 3},
		{"branch_age", "trunk_commits", 101, 101, 5},
		{"tests", "untested_production_files", 0, 0, 0},
		{"tests", "untested_production_files", 1, 1, 10},
		{"tests", "untested_production_files", 2, 1, 10},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s/%s=%d", tt.measure, tt.fact, tt.value), func(t *testing.T) {
			var f facts.Facts
			switch tt.fact {
			case "lines":
				f.Files = []facts.File{{Path: "main.go", Added: tt.value}}
			case "files":
				for _, p := range names("f", tt.value) {
					f.Files = append(f.Files, facts.File{Path: p})
				}
			case "directories":
				f.Directories = names("d", tt.value)
			case "projects":
				f.Projects = names("p", tt.value)
			case "existing_lines":
				f.Files = []facts.File{{Path: "main.go", Added: tt.value}}
			case "days":
				f.BranchAgeDays = tt.value
			case "trunk_commits":
				f.TrunkCommits = tt.value
			case "untested_production_files":
				for _, p := range names("f", tt.value) {
					f.Files = append(f.Files, facts.File{Path: p})
				}
			}
			p := entry(t, Score(f, Default()), tt.measure, tt.fact)
			if p.Value != tt.value || p.Min != tt.wantMin || p.Points != tt.wantPoints {
				t.Errorf("value %d min %d points %d, want value %d min %d points %d",
					p.Value, p.Min, p.Points, tt.value, tt.wantMin, tt.wantPoints)
			}
		})
	}
}

func TestLevelEdges(t *testing.T) {
	tests := []struct {
		total int
		want  Level
	}{
		{0, Low}, {19, Low}, {20, Medium}, {39, Medium}, {40, High}, {100, High},
	}
	for _, tt := range tests {
		if got := level(tt.total, Default().Levels); got != tt.want {
			t.Errorf("level(%d) = %q, want %q", tt.total, got, tt.want)
		}
	}
}

// TestTotalLevel drives the total to each side of each level edge through
// Score itself. files counts main_test.go, which carries the lines; every
// file is new and a test is present, so degree and tests add nothing.
func TestTotalLevel(t *testing.T) {
	tests := []struct {
		lines, files, dirs, projects int
		wantTotal                    int
		want                         Level
	}{
		{0, 1, 0, 0, 0, Low},
		{201, 6, 3, 2, 17, Low},    // 10+2+2+3
		{201, 11, 3, 2, 19, Low},   // 10+4+2+3: highest Low
		{401, 1, 0, 0, 20, Medium}, // lowest Medium
		{801, 1, 0, 0, 30, Medium},
		{801, 6, 5, 2, 39, Medium}, // 30+2+4+3: highest Medium
		{801, 6, 3, 3, 40, High},   // 30+2+2+6: lowest High
		{801, 16, 9, 3, 50, High},  // every built-in band at its top
	}
	for _, tt := range tests {
		f := facts.Facts{Directories: names("d", tt.dirs), Projects: names("p", tt.projects)}
		f.Files = []facts.File{{Path: "main_test.go", Added: tt.lines, New: true, Classes: []facts.Class{facts.ClassTest}}}
		for _, p := range names("f", tt.files-1) {
			f.Files = append(f.Files, facts.File{Path: p, New: true})
		}
		r := Score(f, Default())
		if r.Total != tt.wantTotal || r.Level != tt.want {
			t.Errorf("%+v: total %d level %q, want %d %q", tt, r.Total, r.Level, tt.wantTotal, tt.want)
		}
	}
}

// TestTotalLevelDegreeAgeTests crosses the level edges with degree, branch
// age and tests. main.go is one existing, untested production file.
func TestTotalLevelDegreeAgeTests(t *testing.T) {
	tests := []struct {
		existing, days, trunk int
		wantTotal             int
		want                  Level
	}{
		{0, 0, 0, 10, Low},        // tests 10: any untested production change
		{26, 3, 0, 15, Low},       // degree 2 + days 3 + tests 10
		{26, 8, 10, 18, Low},      // 2+6+0+10
		{26, 8, 11, 20, Medium},   // 2+6+2+10: one trunk band crosses into Medium
		{51, 15, 101, 32, Medium}, // size 5 + degree 2 + days 10 + trunk 5 + tests 10
		{501, 0, 0, 45, High},     // size 20 + degree 15 + tests 10
	}
	for _, tt := range tests {
		f := facts.Facts{BranchAgeDays: tt.days, TrunkCommits: tt.trunk}
		f.Files = []facts.File{{Path: "main.go", Added: tt.existing}}
		r := Score(f, Default())
		if r.Total != tt.wantTotal || r.Level != tt.want {
			t.Errorf("%+v: total %d level %q, want %d %q", tt, r.Total, r.Level, tt.wantTotal, tt.want)
		}
	}
}

func TestTotalBounds(t *testing.T) {
	if r := Score(facts.Facts{}, Default()); r.Total != 0 || r.Level != Low {
		t.Errorf("empty facts: total %d level %q, want 0 Low", r.Total, r.Level)
	}

	sat := facts.Facts{Directories: names("d", 20), Projects: names("p", 5), BranchAgeDays: 100, TrunkCommits: 1000}
	sat.Files = []facts.File{{Path: "big.go", Added: 10000}}
	for _, p := range names("f", 49) {
		sat.Files = append(sat.Files, facts.File{Path: p})
	}
	// Every built-in band at its top: size and spread 50, degree 15, branch
	// age 15, tests 10 (the files are existing and untested).
	if r := Score(sat, Default()); r.Total != 90 || r.Level != High {
		t.Errorf("saturating facts: total %d level %q, want 90 High", r.Total, r.Level)
	}

	heavy := Default()
	heavy.SizeLines = Bands{{0, 80}}
	heavy.SpreadFiles = Bands{{0, 80}}
	if r := Score(facts.Facts{}, heavy); r.Total != 100 || r.Level != High {
		t.Errorf("160 raw points: total %d level %q, want 100 High", r.Total, r.Level)
	}
}

func TestSizeExcludesDocsGeneratedLock(t *testing.T) {
	f := facts.Facts{Files: []facts.File{
		{Path: "README.md", Added: 500, Classes: []facts.Class{facts.ClassDocs}},
		{Path: "go.sum", Added: 300, Classes: []facts.Class{facts.ClassLock}},
		{Path: "api/x.pb.go", Added: 400, Classes: []facts.Class{facts.ClassGenerated}},
		{Path: "main.go", Added: 30, Removed: 10},
		{Path: "main_test.go", Added: 15, Classes: []facts.Class{facts.ClassTest}},
	}}
	p := entry(t, Score(f, Default()), "size", "lines")
	if p.Value != 55 || p.Points != 5 {
		t.Errorf("size value %d points %d, want 55 and 5", p.Value, p.Points)
	}
	if want := []string{"main.go", "main_test.go"}; !reflect.DeepEqual(p.From, want) {
		t.Errorf("size from %v, want %v", p.From, want)
	}
	if want := []string{"README.md", "api/x.pb.go", "go.sum"}; !reflect.DeepEqual(p.Excluded, want) {
		t.Errorf("size excluded %v, want %v", p.Excluded, want)
	}

	// A file in an excluded class and another class is still excluded.
	snap := facts.Facts{Files: []facts.File{
		{Path: "__snapshots__/a.snap", Added: 900, Classes: []facts.Class{facts.ClassGenerated, facts.ClassTest}},
	}}
	p = entry(t, Score(snap, Default()), "size", "lines")
	if p.Value != 0 || !reflect.DeepEqual(p.Excluded, []string{"__snapshots__/a.snap"}) {
		t.Errorf("snapshot: value %d excluded %v, want 0 and the snapshot", p.Value, p.Excluded)
	}
}

func TestSpreadCountsAllFiles(t *testing.T) {
	r := Score(mixed(), Default())
	files := entry(t, r, "spread", "files")
	want := []string{"README.md", "api/x.pb.go", "go.sum", "main.go", "main_test.go", "ui/app.js"}
	if files.Value != 6 || !reflect.DeepEqual(files.From, want) || files.Points != 2 {
		t.Errorf("spread/files = %+v, want 6 files %v for 2 points", files, want)
	}
	if d := entry(t, r, "spread", "directories"); d.Value != 3 || !reflect.DeepEqual(d.From, []string{".", "api", "ui"}) {
		t.Errorf("spread/directories = %+v", d)
	}
	if p := entry(t, r, "spread", "projects"); p.Value != 2 || p.Points != 3 || !reflect.DeepEqual(p.From, []string{".", "ui"}) {
		t.Errorf("spread/projects = %+v", p)
	}
}

func TestBreakdownNamesMeasureAndFacts(t *testing.T) {
	f := mixed()
	r := Score(f, Default())

	var order [][2]string
	sum := 0
	for _, p := range r.Breakdown {
		order = append(order, [2]string{p.Measure, p.Fact})
		sum += p.Points
		if p.Measure == "" || p.Fact == "" || p.From == nil {
			t.Errorf("entry %+v lacks a measure, fact or from list", p)
		}
	}
	want := [][2]string{
		{"size", "lines"}, {"spread", "files"}, {"spread", "directories"}, {"spread", "projects"},
		{"degree", "existing_lines"}, {"branch_age", "days"}, {"branch_age", "trunk_commits"},
		{"tests", "untested_production_files"},
	}
	if !reflect.DeepEqual(order, want) {
		t.Errorf("breakdown order %v, want %v", order, want)
	}
	if r.Total != sum {
		t.Errorf("total %d, want the sum of the breakdown %d", r.Total, sum)
	}

	size := r.Breakdown[0]
	lines := 0
	for _, file := range f.Files {
		if slices.Contains(size.From, file.Path) {
			lines += file.Added + file.Removed
		}
	}
	if size.Value != lines || size.Value != 275 || size.Min != 201 || size.Points != 10 {
		t.Errorf("size = %+v, want 275 lines from its files, band 201 for 10 points", size)
	}
	for _, p := range r.Breakdown {
		if (p.Measure == "spread" || p.Measure == "tests") && p.Value != len(p.From) {
			t.Errorf("%s/%s value %d, want len(from) %d", p.Measure, p.Fact, p.Value, len(p.From))
		}
	}
	// Degree counts the lines of the existing, non-excluded files it names.
	deg := entry(t, r, "degree", "existing_lines")
	if want := []string{"main.go", "main_test.go"}; !reflect.DeepEqual(deg.From, want) {
		t.Errorf("degree from %v, want %v", deg.From, want)
	}
	existing := 0
	for _, file := range f.Files {
		if slices.Contains(deg.From, file.Path) {
			existing += file.Added + file.Removed
		}
	}
	if deg.Value != existing || deg.Value != 55 || deg.Min != 26 || deg.Points != 2 {
		t.Errorf("degree = %+v, want 55 existing lines, band 26 for 2 points", deg)
	}
	// main_test.go is a test change, so tests scores nothing.
	if p := entry(t, r, "tests", "untested_production_files"); p.Value != 0 || p.Points != 0 || len(p.From) != 0 {
		t.Errorf("tests = %+v, want 0 with a test change", p)
	}
	// 10 size + 2 files + 2 directories + 3 projects + 2 degree.
	if r.Total != 19 || r.Level != Low {
		t.Errorf("total %d level %q, want 19 Low", r.Total, r.Level)
	}
}

func TestScoreIsDeterministic(t *testing.T) {
	first := Score(mixed(), Default())
	second := Score(mixed(), Default())
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("two runs differ:\n%+v\n%+v", first, second)
	}
	a, _ := json.Marshal(first)
	b, _ := json.Marshal(second)
	if string(a) != string(b) {
		t.Fatalf("two runs marshal differently:\n%s\n%s", a, b)
	}

	reversed := mixed()
	slices.Reverse(reversed.Files)
	slices.Reverse(reversed.Directories)
	slices.Reverse(reversed.Projects)
	c, _ := json.Marshal(Score(reversed, Default()))
	if string(a) != string(c) {
		t.Errorf("input order changes the result:\n%s\n%s", a, c)
	}
}

// TestScoreDoesNotAliasInput pins that the result owns its slices, so a
// caller changing its facts afterwards cannot change a result already given.
func TestScoreDoesNotAliasInput(t *testing.T) {
	f := mixed()
	r := Score(f, Default())
	f.Directories[0] = "changed"
	if got := entry(t, r, "spread", "directories").From[0]; got != "." {
		t.Errorf("result changed with its input: %q", got)
	}
}

func TestTestsMeasure(t *testing.T) {
	prod := func(p string) facts.File { return facts.File{Path: p, Added: 5} }
	of := func(p string, c ...facts.Class) facts.File { return facts.File{Path: p, Added: 5, Classes: c} }
	tests := []struct {
		name       string
		files      []facts.File
		wantValue  int
		wantPoints int
		wantFrom   []string
	}{
		{"production only", []facts.File{prod("main.go")}, 1, 10, []string{"main.go"}},
		{"production and a test", []facts.File{prod("main.go"), of("main_test.go", facts.ClassTest)}, 0, 0, []string{}},
		{"test only", []facts.File{of("main_test.go", facts.ClassTest)}, 0, 0, []string{}},
		{"docs only", []facts.File{of("README.md", facts.ClassDocs)}, 0, 0, []string{}},
		{"lock only", []facts.File{of("go.sum", facts.ClassLock)}, 0, 0, []string{}},
		{"generated only", []facts.File{of("api/x.pb.go", facts.ClassGenerated)}, 0, 0, []string{}},
		{"production and a snapshot", []facts.File{prod("app.js"), of("__snapshots__/a.snap", facts.ClassGenerated, facts.ClassTest)}, 0, 0, []string{}},
		{"production with docs", []facts.File{prod("main.go"), of("README.md", facts.ClassDocs)}, 1, 10, []string{"main.go"}},
		{"two production files", []facts.File{prod("b.go"), prod("a.go")}, 2, 10, []string{"a.go", "b.go"}},
		{"deleted production file", []facts.File{{Path: "old.go", Removed: 9}}, 1, 10, []string{"old.go"}},
		{"empty change", nil, 0, 0, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := entry(t, Score(facts.Facts{Files: tt.files}, Default()), "tests", "untested_production_files")
			if p.Value != tt.wantValue || p.Points != tt.wantPoints || !reflect.DeepEqual(p.From, tt.wantFrom) {
				t.Errorf("tests = %+v, want value %d points %d from %v", p, tt.wantValue, tt.wantPoints, tt.wantFrom)
			}
		})
	}
}

// TestDegreeLeavesOutNewAndExcludedFiles pins that degree counts only lines
// in existing files that size counts, and names exactly those files.
func TestDegreeLeavesOutNewAndExcludedFiles(t *testing.T) {
	f := facts.Facts{Files: []facts.File{
		{Path: "a.go", Added: 20, Removed: 10},
		{Path: "new.go", Added: 400, New: true},
		{Path: "go.sum", Added: 300, Classes: []facts.Class{facts.ClassLock}},
		{Path: "README.md", Added: 300, Classes: []facts.Class{facts.ClassDocs}},
		{Path: "logo.png", Binary: true},
	}}
	p := entry(t, Score(f, Default()), "degree", "existing_lines")
	if p.Value != 30 || p.Min != 26 || p.Points != 2 || !reflect.DeepEqual(p.From, []string{"a.go", "logo.png"}) {
		t.Errorf("degree = %+v, want 30 lines from [a.go logo.png], band 26 for 2 points", p)
	}
}

func TestBranchAgeFrom(t *testing.T) {
	f := facts.Facts{Base: "bbb", MergeBase: "mmm", Head: "hhh", BranchAgeDays: 4, TrunkCommits: 12}
	r := Score(f, Default())
	days, trunk := entry(t, r, "branch_age", "days"), entry(t, r, "branch_age", "trunk_commits")
	if days.Value != 4 || days.Points != 3 || !reflect.DeepEqual(days.From, []string{"mmm..hhh"}) {
		t.Errorf("days = %+v, want 4 for 3 points from mmm..hhh", days)
	}
	if trunk.Value != 12 || trunk.Points != 2 || !reflect.DeepEqual(trunk.From, []string{"mmm..bbb"}) {
		t.Errorf("trunk_commits = %+v, want 12 for 2 points from mmm..bbb", trunk)
	}

	// Facts predicted from a plan carry no commits: the values still score,
	// and From is empty, not nil.
	planned := Score(facts.Facts{BranchAgeDays: 4, TrunkCommits: 12}, Default())
	for _, fact := range []string{"days", "trunk_commits"} {
		if p := entry(t, planned, "branch_age", fact); p.From == nil || len(p.From) != 0 || p.Points == 0 {
			t.Errorf("planned %s = %+v, want points and an empty from", fact, p)
		}
	}
}
