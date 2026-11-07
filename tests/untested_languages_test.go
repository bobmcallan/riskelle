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
		if p := untestedPoint(t, out); p.Value != 0 || p.Points != 0 || len(p.From) != 0 {
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
		if p := untestedPoint(t, out); p.Value != 0 {
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
			if p := untestedPoint(t, out); p.Value != tt.wantValue || !reflect.DeepEqual(p.From, tt.wantFrom) {
				t.Errorf("untested = %+v, want %d from %v", p, tt.wantValue, tt.wantFrom)
			}
		})
	}
}

// untestedPoint returns the tests measure's breakdown entry from a JSON report.
func untestedPoint(t *testing.T, out string) score.Point {
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

// TestShellScriptTestConvention drives the built binary over a change modelled
// on a real one: an extensionless script and its extensionless -test script
// changed together score no untested file, from score and estimate; a script
// changed alone, in a repo with script tests, counts only as a .sh file: an
// extensionless script or a Makefile names no language and never counts.
func TestShellScriptTestConvention(t *testing.T) {
	bin := buildRiskelle(t, t.TempDir())
	score := func(t *testing.T, dir string) string {
		t.Helper()
		code, out, stderr := runScore(t, bin, dir, "--base", "main", "--head", "topic")
		if code != 0 {
			t.Fatalf("score exit %d: %s", code, stderr)
		}
		return out
	}

	t.Run("script with its test", func(t *testing.T) {
		dir := rulesFixture(t,
			map[string]string{"README.md": "# fixture\n", "scripts/isolation-test": "#!/bin/bash\ntrue\n"},
			map[string]string{"scripts/plan-guard": numbered("# ", 60), "scripts/plan-guard-test": numbered("# ", 40)})
		out := score(t, dir)
		if p := untestedPoint(t, out); p.Value != 0 || len(p.From) != 0 {
			t.Errorf("untested = %+v, want 0", p)
		}
		var got report.Report
		json.Unmarshal([]byte(out), &got)
		for _, p := range got.Breakdown {
			if p.Fact == "lines" && p.Value != 60 {
				t.Errorf("size = %d, want 60: the test's lines are not production", p.Value)
			}
		}
		plan := filepath.Join(t.TempDir(), "plan.json")
		os.WriteFile(plan, []byte(`{"target":"main","files":[`+
			`{"path":"scripts/plan-guard","added":60},{"path":"scripts/plan-guard-test","added":40}]}`), 0o644)
		code, eout, stderr := runEstimate(t, bin, dir, plan)
		if code != 0 {
			t.Fatalf("estimate exit %d: %s", code, stderr)
		}
		if p := untestedPoint(t, eout); p.Value != 0 {
			t.Errorf("estimate untested = %+v, want 0", p)
		}
	})

	for _, tt := range []struct {
		name  string
		base  map[string]string
		topic map[string]string
		want  []string
	}{
		{"script alone, script tests in repo", map[string]string{"scripts/plan-guard-test": "true\n", "scripts/plan-guard": "a\n"},
			map[string]string{"scripts/plan-guard": "b\n"}, []string{}},
		{".sh alone, extensionless script tests in repo", map[string]string{"scripts/plan-guard-test": "true\n"},
			map[string]string{"scripts/run.sh": "echo\n"}, []string{"scripts/run.sh"}},
		{"Makefile alone", map[string]string{"README.md": "# fixture\n"},
			map[string]string{"Makefile": "all:\n"}, []string{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := untestedPoint(t, score(t, rulesFixture(t, tt.base, tt.topic)))
			if !reflect.DeepEqual(p.From, tt.want) {
				t.Errorf("untested from %v, want %v", p.From, tt.want)
			}
		})
	}
}

// TestDataFilesAreNeverUntested drives the built binary over a change modelled
// on a real one: a release pipeline's YAML and an extensionless data file,
// both edited with no test, add no untested points from score or estimate,
// though they still count for size; a Go file beside them still counts.
func TestDataFilesAreNeverUntested(t *testing.T) {
	bin := buildRiskelle(t, t.TempDir())
	base := map[string]string{"README.md": "# fixture\n", "pipelines/release.yml": numbered("# ", 20), "release-roots": "a\n"}
	topic := map[string]string{"pipelines/release.yml": numbered("# ", 45), "release-roots": "a\nb\nc\n"}
	run := func(t *testing.T, dir string) report.Report {
		t.Helper()
		code, out, stderr := runScore(t, bin, dir, "--base", "main", "--head", "topic")
		if code != 0 {
			t.Fatalf("score exit %d: %s", code, stderr)
		}
		var r report.Report
		if err := json.Unmarshal([]byte(out), &r); err != nil {
			t.Fatal(err)
		}
		return r
	}
	point := func(r report.Report, fact string) score.Point {
		for _, p := range r.Breakdown {
			if p.Fact == fact {
				return p
			}
		}
		return score.Point{}
	}

	t.Run("pipeline YAML and a data file", func(t *testing.T) {
		dir := rulesFixture(t, base, topic)
		r := run(t, dir)
		if p := point(r, "untested_production_files"); p.Value != 0 || p.Points != 0 || len(p.From) != 0 {
			t.Errorf("untested = %+v, want 0", p)
		}
		if p := point(r, "lines"); !reflect.DeepEqual(p.From, []string{"pipelines/release.yml", "release-roots"}) {
			t.Errorf("size from %v, want both files", p.From)
		}
		plan := filepath.Join(t.TempDir(), "plan.json")
		os.WriteFile(plan, []byte(`{"target":"main","files":[`+
			`{"path":"pipelines/release.yml","added":25},{"path":"release-roots","added":2}]}`), 0o644)
		code, eout, stderr := runEstimate(t, bin, dir, plan)
		if code != 0 {
			t.Fatalf("estimate exit %d: %s", code, stderr)
		}
		if p := untestedPoint(t, eout); p.Value != 0 {
			t.Errorf("estimate untested = %+v, want 0", p)
		}
	})

	t.Run("with a Go file", func(t *testing.T) {
		withGo := map[string]string{"src/app.go": "package src\n"}
		for k, v := range topic {
			withGo[k] = v
		}
		r := run(t, rulesFixture(t, base, withGo))
		if p := point(r, "untested_production_files"); p.Points != 10 || !reflect.DeepEqual(p.From, []string{"src/app.go"}) {
			t.Errorf("untested = %+v, want 10 points from [src/app.go]", p)
		}
	})
}

// TestShebangScripts drives the built binary: an extensionless script whose
// first line is a shell shebang is a Shell script, so it counts as untested
// only when the repo holds Shell tests (an extensionless shebang script test
// is one); a non-shell shebang or none never counts; estimate reads an
// existing script at the target and names no language for a new path, the
// same every run.
func TestShebangScripts(t *testing.T) {
	bin := buildRiskelle(t, t.TempDir())
	scoreOut := func(t *testing.T, dir string) string {
		t.Helper()
		code, out, stderr := runScore(t, bin, dir, "--base", "main", "--head", "topic")
		if code != 0 {
			t.Fatalf("score exit %d: %s", code, stderr)
		}
		return out
	}
	withTest := map[string]string{"scripts/plan-guard-test": "#!/bin/sh\ntrue\n", "scripts/plan-guard": "#!/bin/sh\na\n"}
	for _, tt := range []struct {
		name  string
		base  map[string]string
		topic map[string]string
		want  []string
	}{
		{"shell shebang, shell tests in repo", withTest,
			map[string]string{"scripts/plan-guard": "#!/bin/sh\nb\n"}, []string{"scripts/plan-guard"}},
		{"shell shebang script test absent", map[string]string{"README.md": "# x\n", "scripts/plan-guard": "#!/bin/sh\na\n"},
			map[string]string{"scripts/plan-guard": "#!/bin/sh\nb\n"}, []string{}},
		{"env bash shebang, no shell tests", map[string]string{"README.md": "# x\n", "scripts/deploy": "#!/usr/bin/env bash\na\n"},
			map[string]string{"scripts/deploy": "#!/usr/bin/env bash\nb\n"}, []string{}},
		{"python shebang, shell tests in repo", map[string]string{"scripts/plan-guard-test": "#!/bin/sh\ntrue\n", "scripts/sync": "#!/usr/bin/env python3\na\n"},
			map[string]string{"scripts/sync": "#!/usr/bin/env python3\nb\n"}, []string{}},
		{"no shebang, shell tests in repo", map[string]string{"scripts/plan-guard-test": "#!/bin/sh\ntrue\n", "scripts/notes": "a\n"},
			map[string]string{"scripts/notes": "b\n"}, []string{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := untestedPoint(t, scoreOut(t, rulesFixture(t, tt.base, tt.topic)))
			if !reflect.DeepEqual(p.From, tt.want) {
				t.Errorf("untested from %v, want %v", p.From, tt.want)
			}
		})
	}

	t.Run("estimate", func(t *testing.T) {
		dir := rulesFixture(t, withTest, map[string]string{"scripts/plan-guard": "#!/bin/sh\nb\n"})
		for _, tt := range []struct {
			plan string
			want []string
		}{
			{`{"target":"main","files":[{"path":"scripts/plan-guard","added":1,"removed":1}]}`, []string{"scripts/plan-guard"}},
			{`{"target":"main","files":[{"path":"scripts/new-tool","added":20}]}`, []string{}},
		} {
			plan := filepath.Join(t.TempDir(), "plan.json")
			os.WriteFile(plan, []byte(tt.plan), 0o644)
			var outs []string
			for i := 0; i < 2; i++ {
				code, out, stderr := runEstimate(t, bin, dir, plan)
				if code != 0 {
					t.Fatalf("estimate exit %d: %s", code, stderr)
				}
				outs = append(outs, out)
			}
			if outs[0] != outs[1] {
				t.Errorf("estimate not deterministic for %s", tt.plan)
			}
			if p := untestedPoint(t, outs[0]); !reflect.DeepEqual(p.From, tt.want) {
				t.Errorf("%s: estimate untested from %v, want %v", tt.plan, p.From, tt.want)
			}
		}
	})
}
