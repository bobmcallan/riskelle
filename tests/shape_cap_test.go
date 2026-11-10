//go:build integration

package tests

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/bobmcallan/riskelle/internal/report"
	"github.com/bobmcallan/riskelle/internal/score"
)

// TestShapeCapsAtMedium drives the built binary over a change whose total is
// in the High band: 510 lines added to an existing, untested Go file. With no
// consequence, or only a Medium floor, the shape level and the level are
// Medium and the total is still the sum of every breakdown entry; with a High
// floor (a production root or a repo class) the level is High and floored_by
// names it. estimate agrees.
func TestShapeCapsAtMedium(t *testing.T) {
	bin := buildRiskelle(t, t.TempDir())
	calc := numbered("package core // ", 10)
	allow := numbered("allow ", 5)
	base := map[string]string{
		"go.mod":              "module example.com/m\n",
		"core/calc.go":        calc,
		"prod-tenant/main.tf": tfStorage,
		"sandbox/main.tf":     tfStorage,
		"scripts/guard-allow": allow,
		".risk/rules.json":    `{"production_roots":["prod-tenant/**"],"consequence_classes":{"release_guard":{"paths":["scripts/guard-allow*"],"floor":"High"}}}`,
	}
	big := calc + numbered("package core // more ", 510)
	topic := func(extra map[string]string) map[string]string {
		m := map[string]string{"core/calc.go": big}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	score1 := func(t *testing.T, dir string, args ...string) (report.Report, string) {
		t.Helper()
		code, out, stderr := runScore(t, bin, dir, append([]string{"--base", "main", "--head", "topic"}, args...)...)
		if code != 0 {
			t.Fatalf("score exit %d: %s", code, stderr)
		}
		if len(args) > 0 {
			return report.Report{}, out
		}
		return decodeReport(t, out), out
	}
	estimate := func(t *testing.T, dir, plan string) report.Report {
		t.Helper()
		path := filepath.Join(t.TempDir(), "plan.json")
		if err := os.WriteFile(path, []byte(plan), 0o644); err != nil {
			t.Fatal(err)
		}
		code, out, stderr := runEstimate(t, bin, dir, path)
		if code != 0 {
			t.Fatalf("estimate exit %d: %s", code, stderr)
		}
		return decodeReport(t, out)
	}
	// highBand checks the total is in the High band and is the sum of its
	// breakdown, so the cap left every point in place.
	highBand := func(t *testing.T, r report.Report) {
		t.Helper()
		sum := 0
		for _, p := range r.Breakdown {
			sum += p.Points
		}
		if r.Total < 40 || r.Total != sum {
			t.Errorf("total %d (sum %d), want a High-band total equal to the sum", r.Total, sum)
		}
	}
	levels := func(t *testing.T, r report.Report, shape, level score.Level, by []string) {
		t.Helper()
		if r.ShapeLevel != shape || r.Level != level || !reflect.DeepEqual(r.FlooredBy, by) {
			t.Errorf("shape %s level %s floored_by %v, want %s %s %v", r.ShapeLevel, r.Level, r.FlooredBy, shape, level, by)
		}
	}

	t.Run("no consequence", func(t *testing.T) {
		dir := rulesFixture(t, base, topic(nil))
		got, out := score1(t, dir)
		highBand(t, got)
		levels(t, got, score.Medium, score.Medium, []string{})
		if !strings.Contains(out, `"floored_by": []`) || !strings.Contains(out, `"consequences": []`) {
			t.Errorf("want empty floored_by and consequences:\n%s", out)
		}
		_, md := score1(t, dir, "--format", "markdown")
		if heading, _, _ := strings.Cut(md, "\n"); !strings.HasSuffix(heading, "/ 100, Medium") || strings.Contains(md, "Shape level") {
			t.Errorf("markdown, want a Medium heading and no shape level line:\n%s", md)
		}
	})

	t.Run("Medium floor", func(t *testing.T) {
		got, _ := score1(t, rulesFixture(t, base, topic(map[string]string{"sandbox/identity.tf": tfRole})))
		highBand(t, got)
		levels(t, got, score.Medium, score.Medium, []string{})
	})

	t.Run("estimate", func(t *testing.T) {
		got := estimate(t, rulesFixture(t, base, topic(nil)), `{"target":"main","files":[{"path":"core/calc.go","added":510}]}`)
		highBand(t, got)
		levels(t, got, score.Medium, score.Medium, []string{})
	})

	t.Run("production root", func(t *testing.T) {
		dir := rulesFixture(t, base, topic(map[string]string{"prod-tenant/identity.tf": tfRole}))
		got, _ := score1(t, dir)
		highBand(t, got)
		levels(t, got, score.Medium, score.High, []string{"identity_rights"})
		var floor score.Level
		var roots []string
		for _, c := range got.Consequences {
			if c.Class == "identity_rights" {
				floor, roots = c.Floor, c.ProductionRoots
			}
		}
		if floor != score.High || !reflect.DeepEqual(roots, []string{"prod-tenant/**"}) {
			t.Errorf("identity_rights floor %s roots %v, want High under prod-tenant/**", floor, roots)
		}
		_, md := score1(t, dir, "--format", "markdown")
		if !strings.Contains(md, "Shape level Medium; the level is High, set by the consequence floor of identity_rights.") {
			t.Errorf("markdown lacks the shape level line:\n%s", md)
		}
	})

	t.Run("repo class", func(t *testing.T) {
		got, _ := score1(t, rulesFixture(t, base, topic(map[string]string{"scripts/guard-allow": allow + "new 1\nnew 2\n"})))
		highBand(t, got)
		levels(t, got, score.Medium, score.High, []string{"release_guard"})
	})

	t.Run("estimate under the root", func(t *testing.T) {
		got := estimate(t, rulesFixture(t, base, topic(nil)),
			`{"target":"main","files":[{"path":"core/calc.go","added":510},{"path":"prod-tenant/identity.tf","added":3,"consequences":["identity_rights"]}]}`)
		highBand(t, got)
		levels(t, got, score.Medium, score.High, []string{"identity_rights"})
	})
}
