//go:build integration

package tests

import (
	"context"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/bobmcallan/riskelle/internal/rules"
	"github.com/bobmcallan/riskelle/internal/score"
)

// TestOwnRules: riskelle's own .risk/rules.json, as committed, is accepted on
// a target branch. A change touching every path it names lands in its scoring
// and release areas, leaves the report goldens out of size, and scores the
// same as with no areas at all.
func TestOwnRules(t *testing.T) {
	own, err := os.ReadFile("../.risk/rules.json")
	if err != nil {
		t.Fatal(err)
	}
	dir := rulesFixture(t,
		map[string]string{"go.mod": "module example.com/m\n", ".risk/rules.json": string(own)},
		map[string]string{
			"internal/score/s.go":                    numbered("package score // ", 10),
			"internal/rules/r.go":                    numbered("package rules // ", 10),
			"internal/facts/f.go":                    numbered("package facts // ", 10),
			".github/workflows/ci.yml":               numbered("# ", 10),
			"action.yml":                             numbered("# ", 10),
			"ado/riskelle.yml":                       numbered("# ", 10),
			"scripts/release.sh":                     numbered("# ", 10),
			"internal/report/testdata/estimate.json": numbered("// ", 10),
		})

	f, res, err := collectAndScore(t, dir)
	if err != nil {
		t.Fatalf("rules.Score: %v", err)
	}

	want := []score.Area{
		{Name: "infrastructure", Paths: []string{".github/workflows/ci.yml"}},
		{Name: "release", Paths: []string{".github/workflows/ci.yml", "action.yml", "ado/riskelle.yml", "scripts/release.sh"}},
		{Name: "scoring", Paths: []string{"internal/facts/f.go", "internal/rules/r.go", "internal/score/s.go"}},
	}
	if !reflect.DeepEqual(res.Areas, want) {
		t.Errorf("areas = %+v, want %+v", res.Areas, want)
	}

	if res.Provenance.RulesSource != ".risk/rules.json" || !strings.HasPrefix(res.Provenance.RulesHash, "sha256:") {
		t.Errorf("provenance = %+v, want rules from .risk/rules.json with a sha256 hash", res.Provenance)
	}

	for _, p := range res.Breakdown {
		if p.Measure != score.MeasureSize {
			continue
		}
		golden := "internal/report/testdata/estimate.json"
		if !slices.Contains(p.Excluded, golden) || slices.Contains(p.From, golden) {
			t.Errorf("size counts the report golden: from %v, excluded %v", p.From, p.Excluded)
		}
	}

	// Areas never score: the same rules with the areas dropped give the same
	// total, level and breakdown.
	r, _, err := rules.Load(context.Background(), dir, f.Base)
	if err != nil {
		t.Fatal(err)
	}
	r.Areas = nil
	plain := score.Score(f, r)
	if res.Total != plain.Total || res.Level != plain.Level || !reflect.DeepEqual(res.Breakdown, plain.Breakdown) {
		t.Errorf("areas changed the score: got %d %s %+v, want %d %s %+v",
			res.Total, res.Level, res.Breakdown, plain.Total, plain.Level, plain.Breakdown)
	}
}
