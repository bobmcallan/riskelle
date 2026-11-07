package score

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
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
// Score itself. files counts main.go, which carries the lines, and an empty
// main_test.go; every file is new and a test is present, so degree and tests
// add nothing.
func TestTotalLevel(t *testing.T) {
	tests := []struct {
		lines, files, dirs, projects int
		wantTotal                    int
		want                         Level
	}{
		{0, 2, 0, 0, 0, Low},
		{201, 6, 3, 2, 17, Low},    // 10+2+2+3
		{201, 11, 3, 2, 19, Low},   // 10+4+2+3: highest Low
		{401, 2, 0, 0, 20, Medium}, // lowest Medium
		{801, 2, 0, 0, 30, Medium},
		{801, 6, 5, 2, 39, Medium}, // 30+2+4+3: highest Medium
		{801, 6, 3, 3, 40, High},   // 30+2+2+6: lowest High
		{801, 16, 9, 3, 50, High},  // every built-in band at its top
	}
	for _, tt := range tests {
		f := facts.Facts{Directories: names("d", tt.dirs), Projects: names("p", tt.projects)}
		f.Files = []facts.File{
			{Path: "main.go", Added: tt.lines, New: true},
			{Path: "main_test.go", New: true, Classes: []facts.Class{facts.ClassTest}},
		}
		for _, p := range names("f", tt.files-2) {
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

func TestSizeExcludesDocsGeneratedLockTest(t *testing.T) {
	f := facts.Facts{Files: []facts.File{
		{Path: "README.md", Added: 500, Classes: []facts.Class{facts.ClassDocs}},
		{Path: "go.sum", Added: 300, Classes: []facts.Class{facts.ClassLock}},
		{Path: "api/x.pb.go", Added: 400, Classes: []facts.Class{facts.ClassGenerated}},
		{Path: "main.go", Added: 30, Removed: 10},
		{Path: "main_test.go", Added: 15, Classes: []facts.Class{facts.ClassTest}},
	}}
	p := entry(t, Score(f, Default()), "size", "lines")
	if p.Value != 40 || p.Points != 0 {
		t.Errorf("size value %d points %d, want 40 and 0", p.Value, p.Points)
	}
	if want := []string{"main.go"}; !reflect.DeepEqual(p.From, want) {
		t.Errorf("size from %v, want %v", p.From, want)
	}
	if want := []string{"README.md", "api/x.pb.go", "go.sum", "main_test.go"}; !reflect.DeepEqual(p.Excluded, want) {
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

// TestTestLinesLeftOutOfSizeAndDegree pins that test files' lines never
// count toward size or degree: a change scores the same size and degree with
// or without its tests, while the tests measure still sees them.
func TestTestLinesLeftOutOfSizeAndDegree(t *testing.T) {
	calc := facts.File{Path: "calc.go", Added: 20, Removed: 5}
	alone := Score(facts.Facts{Files: []facts.File{calc}}, Default())
	tested := Score(facts.Facts{Files: []facts.File{
		calc,
		{Path: "calc_test.go", Added: 300, Removed: 100, Classes: []facts.Class{facts.ClassTest}},
		{Path: "tests/calc_test.go", Added: 200, New: true, Classes: []facts.Class{facts.ClassTest}},
	}}, Default())

	size := entry(t, tested, "size", "lines")
	if size.Value != 25 || !reflect.DeepEqual(size.From, []string{"calc.go"}) {
		t.Errorf("size = %+v, want 25 lines from [calc.go]", size)
	}
	if want := []string{"calc_test.go", "tests/calc_test.go"}; !reflect.DeepEqual(size.Excluded, want) {
		t.Errorf("size excluded %v, want %v", size.Excluded, want)
	}
	deg := entry(t, tested, "degree", "existing_lines")
	if deg.Value != 25 || !reflect.DeepEqual(deg.From, []string{"calc.go"}) {
		t.Errorf("degree = %+v, want 25 lines from [calc.go]", deg)
	}

	for _, m := range [][2]string{{"size", "lines"}, {"degree", "existing_lines"}} {
		a, b := entry(t, alone, m[0], m[1]), entry(t, tested, m[0], m[1])
		if a.Value != b.Value || a.Points != b.Points || !reflect.DeepEqual(a.From, b.From) {
			t.Errorf("%s/%s differs with tests: alone %+v, with tests %+v", m[0], m[1], a, b)
		}
	}

	if p := entry(t, alone, "tests", "untested_production_files"); p.Points != 10 {
		t.Errorf("tests alone = %+v, want 10 points", p)
	}
	if p := entry(t, tested, "tests", "untested_production_files"); p.Points != 0 {
		t.Errorf("tests with tests = %+v, want 0 points", p)
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
		{"tests", "untested_production_files"}, {"impact", "dependents"},
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
	// main.go 40 + ui/app.js 220; main_test.go is a test, left out.
	if size.Value != lines || size.Value != 260 || size.Min != 201 || size.Points != 10 {
		t.Errorf("size = %+v, want 260 lines from its files, band 201 for 10 points", size)
	}
	for _, p := range r.Breakdown {
		if (p.Measure == "spread" || p.Measure == "tests") && p.Value != len(p.From) {
			t.Errorf("%s/%s value %d, want len(from) %d", p.Measure, p.Fact, p.Value, len(p.From))
		}
	}
	// Degree counts the lines of the existing, non-excluded files it names;
	// main_test.go is a test, left out.
	deg := entry(t, r, "degree", "existing_lines")
	if want := []string{"main.go"}; !reflect.DeepEqual(deg.From, want) {
		t.Errorf("degree from %v, want %v", deg.From, want)
	}
	existing := 0
	for _, file := range f.Files {
		if slices.Contains(deg.From, file.Path) {
			existing += file.Added + file.Removed
		}
	}
	if deg.Value != existing || deg.Value != 40 || deg.Min != 26 || deg.Points != 2 {
		t.Errorf("degree = %+v, want 40 existing lines, band 26 for 2 points", deg)
	}
	// main_test.go covers main.go only; ui/app.js changes without app.test.js.
	if p := entry(t, r, "tests", "untested_production_files"); p.Value != 1 || p.Min != 1 || p.Points != 10 || !reflect.DeepEqual(p.From, []string{"ui/app.js"}) {
		t.Errorf("tests = %+v, want 1 from [ui/app.js], band 1 for 10 points", p)
	}
	// 10 size + 2 files + 2 directories + 3 projects + 2 degree + 10 tests.
	if r.Total != 29 || r.Level != Medium {
		t.Errorf("total %d level %q, want 29 Medium", r.Total, r.Level)
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
	f.Consequences = []facts.Consequence{{Class: facts.ConsequenceIdentityRights, Paths: []string{"a.tf"}, Types: []string{"aws_iam_role"}}}
	r = Score(f, Default())
	f.Consequences[0].Paths[0], f.Consequences[0].Types[0] = "changed", "changed"
	if c := r.Consequences[0]; c.Paths[0] != "a.tf" || c.Types[0] != "aws_iam_role" {
		t.Errorf("consequence changed with its input: %+v", c)
	}
}

func TestRepoGeneratedPathsLeftOutOfSize(t *testing.T) {
	f := facts.Facts{Files: []facts.File{{Path: "gen/x.go", Added: 300}, {Path: "main.go", Added: 40}}}
	r := Default()
	r.Generated = []string{"gen/**"}
	p := entry(t, Score(f, r), "size", "lines")
	if p.Value != 40 || p.Min != 0 || p.Points != 0 {
		t.Errorf("size = %d (band %d, %d points), want main.go's 40 lines alone (band 0, 0 points)", p.Value, p.Min, p.Points)
	}
	if !slices.Equal(p.Excluded, []string{"gen/x.go"}) || !slices.Equal(p.From, []string{"main.go"}) {
		t.Errorf("size from %v excluded %v, want from [main.go] excluded [gen/x.go]", p.From, p.Excluded)
	}
	// The repo's patterns mean "generated": when size counts generated files, it counts them too.
	r.SizeExcludes = []facts.Class{facts.ClassDocs, facts.ClassLock}
	p = entry(t, Score(f, r), "size", "lines")
	if p.Value != 340 || len(p.Excluded) != 0 {
		t.Errorf("size without excluding generated = %d excluded %v, want 340 and none", p.Value, p.Excluded)
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
		{"production and its paired test", []facts.File{prod("main.go"), of("main_test.go", facts.ClassTest)}, 0, 0, []string{}},
		{"test only", []facts.File{of("main_test.go", facts.ClassTest)}, 0, 0, []string{}},
		{"docs only", []facts.File{of("README.md", facts.ClassDocs)}, 0, 0, []string{}},
		{"lock only", []facts.File{of("go.sum", facts.ClassLock)}, 0, 0, []string{}},
		{"generated only", []facts.File{of("api/x.pb.go", facts.ClassGenerated)}, 0, 0, []string{}},
		{"production and another file's test", []facts.File{prod("main.go"), of("other_test.go", facts.ClassTest)}, 1, 10, []string{"main.go"}},
		{"paired test in another directory", []facts.File{prod("internal/calc.go"), of("tests/calc_test.go", facts.ClassTest)}, 0, 0, []string{}},
		{"one paired, one not", []facts.File{prod("a.go"), prod("b.go"), of("a_test.go", facts.ClassTest)}, 1, 10, []string{"b.go"}},
		{"origin PR 2149", []facts.File{prod("flow/ai_review.py"), prod("flow/risk_gate.py"), of("tests/test_ai_review.py", facts.ClassTest), of("tests/test_risk_gate.py", facts.ClassTest)}, 0, 0, []string{}},
		{"origin, half tested", []facts.File{prod("flow/ai_review.py"), prod("flow/risk_gate.py"), of("tests/test_ai_review.py", facts.ClassTest)}, 1, 10, []string{"flow/risk_gate.py"}},
		{"extension must match", []facts.File{prod("a.ts"), of("a.test.js", facts.ClassTest)}, 1, 10, []string{"a.ts"}},
		{"marker-less test pairs by its own name", []facts.File{prod("app.js"), of("__tests__/app.js", facts.ClassTest)}, 0, 0, []string{}},
		{"no convention, with a test", []facts.File{prod("deploy.yaml"), of("x_test.go", facts.ClassTest)}, 0, 0, []string{}},
		{"no convention, no test", []facts.File{prod("deploy.yaml")}, 1, 10, []string{"deploy.yaml"}},
		{"mixed", []facts.File{prod("a.go"), prod("deploy.yaml"), of("b_test.go", facts.ClassTest)}, 1, 10, []string{"a.go"}},
		{"deleted with its deleted test", []facts.File{{Path: "old.go", Removed: 9}, {Path: "old_test.go", Removed: 4, Classes: []facts.Class{facts.ClassTest}}}, 0, 0, []string{}},
		{"production and an unpaired snapshot", []facts.File{prod("app.js"), of("__snapshots__/a.snap", facts.ClassGenerated, facts.ClassTest)}, 1, 10, []string{"app.js"}},
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
// in existing files that size counts (not tests), and names exactly those
// files.
func TestDegreeLeavesOutNewAndExcludedFiles(t *testing.T) {
	f := facts.Facts{Files: []facts.File{
		{Path: "a.go", Added: 20, Removed: 10},
		{Path: "a_test.go", Added: 50, Classes: []facts.Class{facts.ClassTest}},
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

// TestProvenanceJSON pins the provenance keys, their order, and that a false
// exclusion toggle is recorded rather than omitted.
func TestProvenanceJSON(t *testing.T) {
	got, err := json.Marshal(Provenance{RulesHash: "h", ExcludeNonCodeFiles: true})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"riskelle_version":"","rules_source":"","rules_commit":"","rules_hash":"h","exclude_non_code_files":true,"exclude_log_files":false}`
	if string(got) != want {
		t.Errorf("Provenance JSON = %s, want %s", got, want)
	}
}

// TestScoreReadsTestLanguages pins that Score hands the facts' test languages
// to the tests measure: the same shell change counts only when the repo holds
// a shell test.
func TestScoreReadsTestLanguages(t *testing.T) {
	files := []facts.File{{Path: "ops/run.sh", Added: 5}}
	without := entry(t, Score(facts.Facts{Files: files}, Default()), "tests", "untested_production_files")
	with := entry(t, Score(facts.Facts{Files: files, TestLanguages: []string{"Shell"}}, Default()), "tests", "untested_production_files")
	if without.Value != 0 || with.Value != 1 {
		t.Errorf("untested without shell tests = %d, with = %d; want 0 and 1", without.Value, with.Value)
	}
}

// identityRights is one identity_rights consequence, as facts give it.
var identityRights = []facts.Consequence{{
	Class: facts.ConsequenceIdentityRights, Paths: []string{"iam/main.tf"}, Types: []string{"azurerm_role_assignment"},
}}

// levelFacts builds facts that total the given TestTotalLevel row.
func levelFacts(lines, files, dirs, projects int) facts.Facts {
	f := facts.Facts{Directories: names("d", dirs), Projects: names("p", projects)}
	f.Files = []facts.File{
		{Path: "main.go", Added: lines, New: true},
		{Path: "main_test.go", New: true, Classes: []facts.Class{facts.ClassTest}},
	}
	for _, p := range names("f", files-2) {
		f.Files = append(f.Files, facts.File{Path: p, New: true})
	}
	return f
}

func TestConsequenceFloorsLevel(t *testing.T) {
	tests := []struct {
		name string
		f    facts.Facts
		want Level
	}{
		{"Low total floored to Medium", levelFacts(0, 2, 0, 0), Medium},
		{"Medium total stays Medium", levelFacts(401, 2, 0, 0), Medium},
		{"High total stays High", levelFacts(801, 6, 3, 3), High},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			without := Score(tt.f, Default())
			tt.f.Consequences = identityRights
			with := Score(tt.f, Default())
			if with.Level != tt.want {
				t.Errorf("level = %s, want %s", with.Level, tt.want)
			}
			if with.Total != without.Total || !reflect.DeepEqual(with.Breakdown, without.Breakdown) {
				t.Errorf("a floor changed the total or breakdown: %d vs %d", with.Total, without.Total)
			}
			want := []Consequence{{Consequence: identityRights[0], Floor: Medium}}
			if !reflect.DeepEqual(with.Consequences, want) {
				t.Errorf("consequences = %+v, want %+v", with.Consequences, want)
			}
		})
	}
}

func TestNoConsequenceScoresAsBefore(t *testing.T) {
	for _, f := range []facts.Facts{mixed(), levelFacts(201, 11, 3, 2), levelFacts(401, 2, 0, 0), levelFacts(801, 16, 9, 3)} {
		r := Score(f, Default())
		if r.Consequences != nil || r.Level != level(r.Total, Default().Levels) {
			t.Errorf("total %d: level %s consequences %v, want the total's own level and none", r.Total, r.Level, r.Consequences)
		}
		if b, _ := json.Marshal(r); strings.Contains(string(b), `"consequences"`) {
			t.Errorf("result JSON has a consequences key: %s", b)
		}
	}
}

func TestConsequenceWithoutFloor(t *testing.T) {
	r := Default()
	r.ConsequenceFloors = nil
	f := levelFacts(0, 2, 0, 0)
	f.Consequences = identityRights
	res := Score(f, r)
	if res.Level != Low || len(res.Consequences) != 1 || res.Consequences[0].Floor != Low {
		t.Errorf("level %s consequences %+v, want Low with floor Low", res.Level, res.Consequences)
	}
}

func TestDefaultFloors(t *testing.T) {
	d := Default()
	if want := []Floor{{facts.ConsequenceIdentityRights, Medium}}; !reflect.DeepEqual(d.ConsequenceFloors, want) {
		t.Errorf("floors = %+v, want %+v", d.ConsequenceFloors, want)
	}
	for i, fl := range d.ConsequenceFloors {
		if !facts.IsConsequence(fl.Class) {
			t.Errorf("floor for unknown class %q", fl.Class)
		}
		known := false
		for _, e := range d.Levels {
			known = known || e.Level == fl.Level
		}
		if !known {
			t.Errorf("floor %q level %q is not a level", fl.Class, fl.Level)
		}
		if i > 0 && d.ConsequenceFloors[i-1].Class >= fl.Class {
			t.Error("floors are not sorted by class")
		}
	}
}

// TestReadmeDocumentsFloors pins the README's floor rule against the built-in
// floors.
func TestReadmeDocumentsFloors(t *testing.T) {
	data, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	section := func(heading string) string {
		s := string(data)
		i := strings.Index(s, "\n"+heading+"\n")
		if i < 0 {
			t.Fatalf("README has no %q section", heading)
		}
		s = s[i+len(heading)+2:]
		level := strings.Repeat("#", strings.Index(heading, " "))
		for j := 0; j < len(s); j++ {
			if (j == 0 || s[j-1] == '\n') && strings.HasPrefix(s[j:], "#") {
				hashes := len(s[j:]) - len(strings.TrimLeft(s[j:], "#"))
				if hashes <= len(level) {
					return s[:j]
				}
			}
		}
		return s
	}
	cons := section("### Consequences")
	for _, fl := range Default().ConsequenceFloors {
		for _, want := range []string{"`" + fl.Class + "`", string(fl.Level)} {
			if !strings.Contains(cons, want) {
				t.Errorf("README Consequences lacks %s", want)
			}
		}
	}
	for _, want := range []string{"floor", "rules hash", "refused", "never changes the total"} {
		if !strings.Contains(cons, want) {
			t.Errorf("README Consequences lacks %q", want)
		}
	}
	if strings.Contains(cons, "a later release") {
		t.Error("README Consequences still says a later release sets the floor")
	}
	if !strings.Contains(section("## Rules"), "`consequence_floors`") {
		t.Error("README Rules lacks `consequence_floors`")
	}
}

// rootRules are the built-in rules with one production root.
func rootRules() Rules {
	r := Default()
	r.ProductionRoots = []string{"prod-tenant/**"}
	return r
}

func TestProductionRootRaisesFloor(t *testing.T) {
	with := func(paths ...string) facts.Facts {
		f := levelFacts(0, 2, 0, 0)
		f.Consequences = []facts.Consequence{{Class: facts.ConsequenceIdentityRights, Paths: paths, Types: []string{"azurerm_role_assignment"}}}
		return f
	}
	tests := []struct {
		name      string
		f         facts.Facts
		r         Rules
		wantLevel Level
		wantRoots []string
	}{
		{"under the root", with("prod-tenant/main.tf"), rootRules(), High, []string{"prod-tenant/**"}},
		{"outside every root", with("other/main.tf"), rootRules(), Medium, nil},
		{"one path of two under the root (a rename's old path)", with("moved/main.tf", "prod-tenant/main.tf"), rootRules(), High, []string{"prod-tenant/**"}},
		{"under two roots, in rules order", with("a/x.tf", "b/x.tf"), func() Rules { r := Default(); r.ProductionRoots = []string{"a/**", "b/**"}; return r }(), High, []string{"a/**", "b/**"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Score(tt.f, tt.r)
			plain := Score(tt.f, Default())
			if got.Level != tt.wantLevel || !reflect.DeepEqual(got.Consequences[0].ProductionRoots, tt.wantRoots) {
				t.Errorf("level %s roots %v, want %s %v", got.Level, got.Consequences[0].ProductionRoots, tt.wantLevel, tt.wantRoots)
			}
			if got.Consequences[0].Floor != tt.wantLevel {
				t.Errorf("floor %s, want %s", got.Consequences[0].Floor, tt.wantLevel)
			}
			if got.Total != plain.Total || !reflect.DeepEqual(got.Breakdown, plain.Breakdown) {
				t.Error("a production root changed the total or breakdown")
			}
		})
	}
	r := rootRules()
	r.Levels = nil
	Score(with("prod-tenant/main.tf"), r) // no levels: must not panic
}

func TestProductionRootsWithoutConsequence(t *testing.T) {
	inRoot := levelFacts(201, 6, 3, 2)
	inRoot.Files = append(inRoot.Files, facts.File{Path: "prod-tenant/main.tf", Added: 5})
	for _, f := range []facts.Facts{mixed(), levelFacts(401, 2, 0, 0), inRoot} {
		if got, want := Score(f, rootRules()), Score(f, Default()); !reflect.DeepEqual(got, want) {
			t.Errorf("roots with no consequence changed the result:\n%+v\nwant\n%+v", got, want)
		}
	}
}

// TestShapeLevel pins shape_level and floored_by: the shape level is always
// the total's own level, and floored_by names the classes only when a floor
// raised the level above it.
func TestShapeLevel(t *testing.T) {
	under := func(f facts.Facts) facts.Facts {
		f.Consequences = []facts.Consequence{{Class: facts.ConsequenceIdentityRights, Paths: []string{"prod-tenant/iam.tf"}}}
		return f
	}
	noFloor := Default()
	noFloor.ConsequenceFloors = nil
	ir := func(f facts.Facts) facts.Facts { f.Consequences = identityRights; return f }
	tests := []struct {
		name      string
		f         facts.Facts
		r         Rules
		shape     Level
		level     Level
		flooredBy []string
	}{
		{"Low, no consequence", levelFacts(0, 2, 0, 0), Default(), Low, Low, nil},
		{"Low floored to Medium", ir(levelFacts(0, 2, 0, 0)), Default(), Low, Medium, []string{facts.ConsequenceIdentityRights}},
		{"Medium at its floor", ir(levelFacts(401, 2, 0, 0)), Default(), Medium, Medium, nil},
		{"High above its floor", ir(levelFacts(801, 6, 3, 3)), Default(), High, High, nil},
		{"Low floored High by a production root", under(levelFacts(0, 2, 0, 0)), rootRules(), Low, High, []string{facts.ConsequenceIdentityRights}},
		{"class with no floor", ir(levelFacts(0, 2, 0, 0)), noFloor, Low, Low, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Score(tt.f, tt.r)
			if got.ShapeLevel != tt.shape || got.Level != tt.level || !reflect.DeepEqual(got.FlooredBy, tt.flooredBy) {
				t.Errorf("shape %s, level %s, floored_by %v; want %s, %s, %v",
					got.ShapeLevel, got.Level, got.FlooredBy, tt.shape, tt.level, tt.flooredBy)
			}
			if want := level(got.Total, tt.r.Levels); got.ShapeLevel != want {
				t.Errorf("shape level %s, want the total's level %s", got.ShapeLevel, want)
			}
		})
	}
	js, err := json.Marshal(Score(levelFacts(0, 2, 0, 0), Default()))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(js), `"shape_level":"Low"`) || strings.Contains(string(js), "floored_by") {
		t.Errorf("unfloored JSON: %s", js)
	}
}
