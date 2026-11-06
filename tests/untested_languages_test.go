//go:build integration

package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/bobmcallan/riskelle/internal/report"
	"github.com/bobmcallan/riskelle/internal/score"
)

// TestUnpairedLanguageNeedsTestsInTheRepo drives the built binary over a
// change modelled on a real one: two hand-run shell scripts (213 lines in two
// directories) in a repo with no shell tests. Neither counts as untested,
// from score or estimate; with a shell test in the repo both do, and a
// Terraform file follows the same rule with its own tests.
func TestUnpairedLanguageNeedsTestsInTheRepo(t *testing.T) {
	bin := buildRiskelle(t, t.TempDir())
	scripts := map[string]string{
		"ops/rotate-keys.sh": numbered("# ", 120),
		"tools/backfill.sh":  numbered("# ", 93),
	}
	withBase := func(extra map[string]string) map[string]string {
		base := map[string]string{"README.md": "# fixture\n"}
		for k, v := range extra {
			base[k] = v
		}
		return base
	}
	untested := func(t *testing.T, out string) score.Point {
		t.Helper()
		var got report.Report
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("report: %v\n%s", err, out)
		}
		for _, p := range got.Breakdown {
			if p.Measure == score.MeasureTests && p.Fact == "untested_production_files" {
				return p
			}
		}
		t.Fatalf("no untested_production_files in %s", out)
		return score.Point{}
	}
	entry := func(t *testing.T, out, measure, fact string) score.Point {
		t.Helper()
		var got report.Report
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("report: %v", err)
		}
		for _, p := range got.Breakdown {
			if p.Measure == measure && p.Fact == fact {
				return p
			}
		}
		t.Fatalf("no %s/%s", measure, fact)
		return score.Point{}
	}

	t.Run("no shell tests in the repo", func(t *testing.T) {
		dir := rulesFixture(t, withBase(nil), scripts)
		code, out, stderr := runScore(t, bin, dir, "--base", "main", "--head", "topic")
		if code != 0 {
			t.Fatalf("score exit %d: %s", code, stderr)
		}
		if p := untested(t, out); p.Value != 0 || p.Points != 0 || len(p.From) != 0 {
			t.Errorf("untested = %+v, want 0", p)
		}
		if p := entry(t, out, score.MeasureSize, "lines"); p.Value != 213 {
			t.Errorf("size = %d, want 213", p.Value)
		}
		if p := entry(t, out, score.MeasureSpread, "directories"); p.Value != 2 {
			t.Errorf("directories = %d, want 2", p.Value)
		}

		plan := filepath.Join(t.TempDir(), "plan.json")
		os.WriteFile(plan, []byte(`{"target":"main","files":[`+
			`{"path":"ops/rotate-keys.sh","added":120},{"path":"tools/backfill.sh","added":93}]}`), 0o644)
		code, out, stderr = runEstimate(t, bin, dir, plan)
		if code != 0 {
			t.Fatalf("estimate exit %d: %s", code, stderr)
		}
		if p := untested(t, out); p.Value != 0 {
			t.Errorf("estimate untested = %+v, want 0", p)
		}
	})

	for _, tt := range []struct {
		name      string
		base      map[string]string
		topic     map[string]string
		wantValue int
		wantFrom  []string
	}{
		{"shell tests in the repo", map[string]string{"tests/smoke.bats": "@test x { true; }\n"}, scripts,
			2, []string{"ops/rotate-keys.sh", "tools/backfill.sh"}},
		{"terraform, no terraform tests", nil, map[string]string{"infra/main.tf": "resource \"a\" \"b\" {}\n"},
			0, []string{}},
		{"terraform tests in the repo", map[string]string{"infra/main.tftest.hcl": "run \"x\" {}\n"},
			map[string]string{"infra/main.tf": "resource \"a\" \"b\" {}\n"}, 1, []string{"infra/main.tf"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := rulesFixture(t, withBase(tt.base), tt.topic)
			code, out, stderr := runScore(t, bin, dir, "--base", "main", "--head", "topic")
			if code != 0 {
				t.Fatalf("score exit %d: %s", code, stderr)
			}
			if p := untested(t, out); p.Value != tt.wantValue || !reflect.DeepEqual(p.From, tt.wantFrom) {
				t.Errorf("untested = %+v, want %d from %v", p, tt.wantValue, tt.wantFrom)
			}
		})
	}
}
