//go:build integration

package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/bobmcallan/riskelle/internal/report"
	"github.com/bobmcallan/riskelle/internal/score"
)

// TestRepoClassFloorsAllowList drives the built binary over a change modelled
// on a real one: a few lines added to a release guard's allow-list, Low by
// shape, are reported Medium once the target's rules name the allow-list as a
// repo class, or High under a production root; without the class, or with a
// class the change itself adds, it stays Low; estimate finds the class from
// the planned path; a rules file that names a built-in class or a Low floor
// is refused.
func TestRepoClassFloorsAllowList(t *testing.T) {
	bin := buildRiskelle(t, t.TempDir())
	const guardRules = `{"consequence_classes":{"release_guard":{"paths":["scripts/plan-guard-allow*"],"floor":"Medium"}}}`
	allowList := numbered("allow ", 20)
	base := func(rules string) map[string]string {
		b := map[string]string{"scripts/plan-guard": "#!/bin/sh\nexit 0\n", "scripts/plan-guard-allow": allowList}
		if rules != "" {
			b[".risk/rules.json"] = rules
		}
		return b
	}
	topic := map[string]string{"scripts/plan-guard-allow": allowList + "new 1\nnew 2\nnew 3\nnew 4\n"}
	run := func(t *testing.T, dir string, args ...string) (int, string, string) {
		t.Helper()
		return runScore(t, bin, dir, append([]string{"--base", "main", "--head", "topic"}, args...)...)
	}
	score1 := func(t *testing.T, dir string) (report.Report, string) {
		t.Helper()
		code, out, stderr := run(t, dir)
		if code != 0 {
			t.Fatalf("score exit %d: %s", code, stderr)
		}
		return decodeReport(t, out), out
	}

	var classHash string
	t.Run("5edbdb5", func(t *testing.T) {
		dir := rulesFixture(t, base(guardRules), topic)
		got, _ := score1(t, dir)
		classHash = got.Provenance.RulesHash
		if got.Total >= 20 || got.ShapeLevel != score.Low || got.Level != score.Medium ||
			!reflect.DeepEqual(got.FlooredBy, []string{"release_guard"}) || got.Provenance.RulesSource != ".risk/rules.json" {
			t.Errorf("total %d shape %s level %s floored_by %v source %s, want Low floored Medium by release_guard",
				got.Total, got.ShapeLevel, got.Level, got.FlooredBy, got.Provenance.RulesSource)
		}
		want := `[{"class":"release_guard","paths":["scripts/plan-guard-allow"],"types":[],"floor":"Medium"}]`
		if b, _ := json.Marshal(got.Consequences); string(b) != want {
			t.Errorf("consequences = %s\nwant %s", b, want)
		}
		if _, md, _ := run(t, dir, "--format", "markdown"); !strings.Contains(md, "- release_guard, floor Medium") {
			t.Errorf("markdown lacks the class:\n%s", md)
		}

		plan := filepath.Join(t.TempDir(), "plan.json")
		os.WriteFile(plan, []byte(`{"target":"main","files":[{"path":"scripts/plan-guard-allow","added":4}]}`), 0o644)
		code, out, stderr := runEstimate(t, bin, dir, plan)
		if code != 0 {
			t.Fatalf("estimate exit %d: %s", code, stderr)
		}
		if est := decodeReport(t, out); est.Level != score.Medium || !reflect.DeepEqual(est.FlooredBy, []string{"release_guard"}) {
			t.Errorf("estimate level %s floored_by %v, want Medium [release_guard]", est.Level, est.FlooredBy)
		}
		os.WriteFile(plan, []byte(`{"target":"main","files":[{"path":"scripts/plan-guard-allow","added":4,"consequences":["release_guard"]}]}`), 0o644)
		if code, _, stderr := runEstimate(t, bin, dir, plan); code != 1 || !strings.Contains(stderr, `unknown consequence "release_guard"`) {
			t.Errorf("plan naming a repo class: exit %d, stderr %s", code, stderr)
		}
	})

	t.Run("without the class", func(t *testing.T) {
		got, out := score1(t, rulesFixture(t, base(""), topic))
		if got.Level != score.Low || !strings.Contains(out, `"consequences": []`) || got.Provenance.RulesHash == classHash {
			t.Errorf("level %s, hash %s; want Low with no consequence and another hash", got.Level, got.Provenance.RulesHash)
		}
	})

	t.Run("under a production root", func(t *testing.T) {
		rules := `{"production_roots":["scripts/**"],"consequence_classes":{"release_guard":{"paths":["scripts/plan-guard-allow*"],"floor":"Medium"}}}`
		got, _ := score1(t, rulesFixture(t, base(rules), topic))
		if got.Level != score.High || len(got.Consequences) != 1 || !reflect.DeepEqual(got.Consequences[0].ProductionRoots, []string{"scripts/**"}) {
			t.Errorf("level %s consequences %+v, want High naming scripts/**", got.Level, got.Consequences)
		}
	})

	t.Run("added by the change itself", func(t *testing.T) {
		withRules := map[string]string{".risk/rules.json": guardRules}
		for k, v := range topic {
			withRules[k] = v
		}
		if got, _ := score1(t, rulesFixture(t, base(""), withRules)); got.Level != score.Low {
			t.Errorf("level %s, want Low: rules are read from the target", got.Level)
		}
	})

	for _, tt := range []struct{ rules, want string }{
		{`{"consequence_classes":{"identity_rights":{"paths":["x"],"floor":"Medium"}}}`, `refused "identity_rights"`},
		{`{"consequence_classes":{"release_guard":{"paths":["x"],"floor":"Low"}}}`, `floor "Low" refused`},
	} {
		t.Run("refused "+tt.want, func(t *testing.T) {
			code, _, stderr := run(t, rulesFixture(t, base(tt.rules), topic))
			if code != 1 || !strings.Contains(stderr, tt.want) {
				t.Errorf("exit %d stderr %s, want 1 naming %s", code, stderr, tt.want)
			}
		})
	}
}
