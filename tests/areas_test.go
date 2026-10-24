//go:build integration

package tests

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/bobmcallan/riskelle/internal/score"
)

// TestAreasFromTargetRules: a change lands in every built-in sensitive area
// and in one the target branch's .risk/rules.json adds. The result lists each
// with its paths, and the score is the one the same change gets with no areas.
func TestAreasFromTargetRules(t *testing.T) {
	dir := rulesFixture(t,
		map[string]string{"go.mod": "module example.com/m\n", ".risk/rules.json": `{"areas":{"payments":["billing/**"]}}` + "\n"},
		map[string]string{
			// the head tries to add an area of its own; only the target's rules count
			".risk/rules.json":           `{"areas":{"payments":["billing/**"],"ui":["src/**"]}}` + "\n",
			"internal/auth/login.go":     numbered("package auth // ", 20),
			"db/migrations/001_init.sql": numbered("-- ", 10),
			"infra/main.tf":              numbered("# ", 10),
			"config/server.pem":          "-----BEGIN CERTIFICATE-----\n",
			"billing/charge.go":          numbered("package billing // ", 10),
			"src/app.go":                 numbered("package src // ", 10),
		})

	f, res, err := collectAndScore(t, dir)
	if err != nil {
		t.Fatalf("rules.Score: %v", err)
	}
	out, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	want := `"areas":[{"name":"auth","paths":["internal/auth/login.go"]},` +
		`{"name":"infrastructure","paths":["infra/main.tf"]},` +
		`{"name":"migrations","paths":["db/migrations/001_init.sql"]},` +
		`{"name":"payments","paths":["billing/charge.go"]},` +
		`{"name":"secrets","paths":["config/server.pem"]}],"provenance":`
	if !strings.Contains(string(out), want) {
		t.Errorf("result =\n%s\nwant it to contain\n%s", out, want)
	}

	// Areas never score: the built-in rules with no areas at all give the
	// same total, level and breakdown.
	plain := score.Score(f, score.Default())
	if res.Total != plain.Total || res.Level != plain.Level || !reflect.DeepEqual(res.Breakdown, plain.Breakdown) {
		t.Errorf("areas changed the score: got %d %s %+v, want %d %s %+v",
			res.Total, res.Level, res.Breakdown, plain.Total, plain.Level, plain.Breakdown)
	}
}

// TestAreasNoneTouched: a change in no sensitive area has no areas key.
func TestAreasNoneTouched(t *testing.T) {
	dir := rulesFixture(t,
		map[string]string{"go.mod": "module example.com/m\n", ".risk/rules.json": `{"areas":{"payments":["billing/**"]}}` + "\n"},
		map[string]string{"src/app.go": numbered("package src // ", 10), "README.md": "# m\n"})

	_, res, err := collectAndScore(t, dir)
	if err != nil {
		t.Fatalf("rules.Score: %v", err)
	}
	out, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	if res.Areas != nil || strings.Contains(string(out), `"areas"`) {
		t.Errorf("result %s lists areas, want none", out)
	}
}
