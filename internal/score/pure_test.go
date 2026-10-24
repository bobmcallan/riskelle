package score

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/bobmcallan/riskelle/internal/facts"
)

func TestDefaultRulesValid(t *testing.T) {
	r := Default()
	maxTotal := 0
	for name, bands := range map[string]Bands{
		"size_lines": r.SizeLines, "spread_files": r.SpreadFiles,
		"spread_directories": r.SpreadDirs, "spread_projects": r.SpreadProjs,
		"degree_existing_lines": r.DegreeExisting, "branch_age_days": r.BranchAgeDays,
		"branch_trunk_commits": r.BranchTrunkCommits, "tests_untested": r.TestsUntested,
		"impact_dependents": r.ImpactDependents,
	} {
		if len(bands) == 0 || bands[0] != (Band{0, 0}) {
			t.Errorf("%s must start at {0 0}: %v", name, bands)
			continue
		}
		for i := 1; i < len(bands); i++ {
			if bands[i].Min <= bands[i-1].Min || bands[i].Points < bands[i-1].Points {
				t.Errorf("%s band %d %v does not ascend from %v", name, i, bands[i], bands[i-1])
			}
		}
		maxTotal += bands[len(bands)-1].Points
	}
	if maxTotal > 100 {
		t.Errorf("highest bands sum to %d, want 100 or less", maxTotal)
	}
	if len(r.Levels) == 0 || r.Levels[0].Min != 0 {
		t.Errorf("levels must start at 0: %v", r.Levels)
	}
	for i := 1; i < len(r.Levels); i++ {
		if r.Levels[i].Min <= r.Levels[i-1].Min {
			t.Errorf("level %d %v does not ascend from %v", i, r.Levels[i], r.Levels[i-1])
		}
	}

	// Default builds a fresh value each call: changing one leaves the next alone.
	r.SizeLines[1].Points = 99
	r.SizeExcludes[0] = facts.ClassTest
	r.Levels[1].Min = 1
	if again := Default(); again.SizeLines[1].Points != 5 || again.SizeExcludes[0] != facts.ClassDocs || again.Levels[1].Min != 20 {
		t.Errorf("changing one Default() changed the next: %+v", again)
	}
}

// TestScoreNeedsNoDisk scores from an empty working directory: the built-in
// rules are in the binary, not in a file beside it.
func TestScoreNeedsNoDisk(t *testing.T) {
	t.Chdir(t.TempDir())
	if r := Score(mixed(), Default()); r.Total != 19 || r.Level != Low {
		t.Errorf("total %d level %q, want 19 Low", r.Total, r.Level)
	}
}

// TestScorePackageImportsArePure keeps file, network, clock and randomness
// out of the scoring path by construction: the package's non-test files may
// import only what is listed here.
func TestScorePackageImportsArePure(t *testing.T) {
	allowed := map[string]bool{
		"sort": true,
		"github.com/bobmcallan/riskelle/internal/facts": true,
	}
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, p := range paths {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		seen++
		f, err := parser.ParseFile(token.NewFileSet(), p, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			if !allowed[path] {
				t.Errorf("%s imports %q, which the scoring path may not use", p, path)
			}
		}
	}
	if seen == 0 {
		t.Fatal("found no package source files")
	}
}
