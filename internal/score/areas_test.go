package score

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/bobmcallan/riskelle/internal/facts"
)

func areaFacts() facts.Facts {
	return facts.Facts{
		Files: []facts.File{
			{Path: "db/migrations/001_init.sql", Added: 20, New: true},
			{Path: "internal/auth/login.go", Added: 30, Removed: 5},
			{Path: "internal/auth/token.go", Added: 10},
			{Path: "session/new.go", OldPath: "auth/old.go", Added: 1, Removed: 1},
			{Path: "src/app.go", Added: 3},
		},
		Directories: []string{"db/migrations", "internal/auth", "session", "src"},
		Projects:    []string{"."},
	}
}

func TestAreasListMatchedPaths(t *testing.T) {
	f := areaFacts()
	got := Score(f, Default()).Areas
	want := []Area{
		{Name: "auth", Paths: []string{"auth/old.go", "internal/auth/login.go", "internal/auth/token.go"}},
		{Name: "migrations", Paths: []string{"db/migrations/001_init.sql"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Areas = %+v, want %+v", got, want)
	}

	// The same files in another order give the same JSON.
	rev := areaFacts()
	slices.Reverse(rev.Files)
	a, _ := json.Marshal(Score(f, Default()))
	b, _ := json.Marshal(Score(rev, Default()))
	if string(a) != string(b) {
		t.Errorf("file order changed the result:\n%s\n%s", a, b)
	}
}

func TestAreasRepoAdded(t *testing.T) {
	r := Default()
	r.Areas = map[string][]string{"payments": {"billing/**"}, "auth": {"src/login/**"}}
	f := facts.Facts{Files: []facts.File{
		{Path: "billing/charge.go", Added: 1},
		{Path: "internal/auth/x.go", Added: 1},
		{Path: "src/login/form.go", Added: 1},
		{Path: "src/app.go", Added: 1},
	}}
	want := []Area{
		{Name: "auth", Paths: []string{"internal/auth/x.go", "src/login/form.go"}},
		{Name: "payments", Paths: []string{"billing/charge.go"}},
	}
	if got := Score(f, r).Areas; !reflect.DeepEqual(got, want) {
		t.Errorf("Areas = %+v, want %+v", got, want)
	}
}

func TestAreasNone(t *testing.T) {
	res := Score(mixed(), Default())
	if res.Areas != nil {
		t.Errorf("Areas = %+v, want nil", res.Areas)
	}
	out, _ := json.Marshal(res)
	if strings.Contains(string(out), `"areas"`) {
		t.Errorf("result JSON %s has an areas key, want none", out)
	}
}

func TestAreasNeverScore(t *testing.T) {
	// (a) The same facts with every path in a repo-added area score the same.
	f := areaFacts()
	every := Default()
	every.Areas = map[string][]string{"everything": {"**/*"}}
	plain, all := Score(f, Default()), Score(f, every)
	if plain.Total != all.Total || plain.Level != all.Level || !reflect.DeepEqual(plain.Breakdown, all.Breakdown) {
		t.Errorf("areas changed the score: %+v vs %+v", plain, all)
	}
	if reflect.DeepEqual(plain.Areas, all.Areas) || len(all.Areas) != 3 {
		t.Errorf("areas = %+v vs %+v, want the everything area added", plain.Areas, all.Areas)
	}

	// (b) Two changes alike but for landing in sensitive areas score alike.
	in := facts.Facts{
		Files:       []facts.File{{Path: "internal/auth/login.go", Added: 300, Removed: 40}, {Path: "db/migrations/1.sql", Added: 90}},
		Directories: []string{"db/migrations", "internal/auth"},
		Projects:    []string{"."},
	}
	out := facts.Facts{
		Files:       []facts.File{{Path: "internal/login/login.go", Added: 300, Removed: 40}, {Path: "db/schema/1.go", Added: 90}},
		Directories: []string{"db/schema", "internal/login"},
		Projects:    []string{"."},
	}
	sa, sb := Score(in, Default()), Score(out, Default())
	if len(sa.Areas) != 2 || sb.Areas != nil {
		t.Fatalf("fixture: areas %+v and %+v, want two and none", sa.Areas, sb.Areas)
	}
	if sa.Total != sb.Total || sa.Level != sb.Level || len(sa.Breakdown) != len(sb.Breakdown) {
		t.Fatalf("totals %d %s vs %d %s", sa.Total, sa.Level, sb.Total, sb.Level)
	}
	for i := range sa.Breakdown {
		pa, pb := sa.Breakdown[i], sb.Breakdown[i]
		if pa.Measure != pb.Measure || pa.Fact != pb.Fact || pa.Value != pb.Value || pa.Points != pb.Points {
			t.Errorf("breakdown %d: %+v vs %+v", i, pa, pb)
		}
	}

	// (c) No breakdown entry is about areas.
	measures := []string{MeasureSize, MeasureSpread, MeasureDegree, MeasureBranchAge, MeasureTests, MeasureImpact}
	for _, p := range all.Breakdown {
		if !slices.Contains(measures, p.Measure) {
			t.Errorf("breakdown entry %+v is not one of the measures %v", p, measures)
		}
	}
}
