//go:build integration

package tests

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/bobmcallan/riskelle/internal/report"
	"github.com/bobmcallan/riskelle/internal/score"
)

// grantTF is a Terraform file of about n+6 lines in terraform fmt layout: a
// locals block of n lines, then a role assignment granting rights.
func grantTF(n int) string {
	var b strings.Builder
	b.WriteString("locals {\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "  role_%03d = \"r%03d\"\n", i, i)
	}
	b.WriteString("}\n\n" + tfRole)
	return b.String()
}

// TestProductionRootFloorsHigh drives the built binary over a change modelled
// on a real one: about 220 lines of Terraform granting an identity rights in
// a production root, Low by shape, is reported High; the same grant outside
// every root is Medium; a root with no consequence changes nothing; roots
// added by the change itself are not read; estimate agrees.
func TestProductionRootFloorsHigh(t *testing.T) {
	bin := buildRiskelle(t, t.TempDir())
	const rootsJSON = `{"production_roots":["prod-tenant/**"]}`
	base := map[string]string{
		".risk/rules.json":    rootsJSON,
		"prod-tenant/main.tf": tfStorage,
		"sandbox/main.tf":     tfStorage,
	}
	run := func(t *testing.T, dir string, args ...string) (report.Report, string) {
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
	sum := func(r report.Report) (n int) {
		for _, p := range r.Breakdown {
			n += p.Points
		}
		return n
	}

	var rootedHash string
	t.Run("grant under the production root", func(t *testing.T) {
		dir := rulesFixture(t, base, map[string]string{"prod-tenant/identity.tf": grantTF(210)})
		got, _ := run(t, dir)
		for _, p := range got.Breakdown {
			if p.Fact == "lines" && (p.Value < 200 || p.Value > 240) {
				t.Errorf("size = %d, want about 220", p.Value)
			}
		}
		if got.Total >= 20 || got.Total != sum(got) || got.Level != score.High {
			t.Errorf("total %d (sum %d) level %s, want a Low total equal to the sum, level High", got.Total, sum(got), got.Level)
		}
		want := `[{"class":"identity_rights","paths":["prod-tenant/identity.tf"],"types":["azurerm_role_assignment"],"floor":"High","production_roots":["prod-tenant/**"]}]`
		if b, _ := json.Marshal(got.Consequences); string(b) != want {
			t.Errorf("consequences = %s, want %s", b, want)
		}
		if got.Provenance.RulesSource != ".risk/rules.json" {
			t.Errorf("rules source = %q", got.Provenance.RulesSource)
		}
		rootedHash = got.Provenance.RulesHash
		if _, md := run(t, dir, "--format", "markdown"); !strings.Contains(md, "floor High (production root `prod-tenant/**`)") {
			t.Errorf("markdown does not name the root:\n%s", md)
		}

		plan := filepath.Join(t.TempDir(), "plan.json")
		os.WriteFile(plan, []byte(`{"target":"main","files":[{"path":"prod-tenant/identity.tf","added":217,"consequences":["identity_rights"]}]}`), 0o644)
		code, out, stderr := runEstimate(t, bin, dir, plan)
		if code != 0 {
			t.Fatalf("estimate exit %d: %s", code, stderr)
		}
		est := decodeReport(t, out)
		if est.Level != score.High || len(est.Consequences) != 1 || !reflect.DeepEqual(est.Consequences[0].ProductionRoots, []string{"prod-tenant/**"}) {
			t.Errorf("estimate level %s consequences %+v, want High under prod-tenant/**", est.Level, est.Consequences)
		}
	})

	t.Run("same grant outside every root", func(t *testing.T) {
		got, out := run(t, rulesFixture(t, base, map[string]string{"sandbox/identity.tf": grantTF(210)}))
		if got.Level != score.Medium || strings.Contains(out, "production_roots") {
			t.Errorf("level %s, want Medium with no production_roots:\n%s", got.Level, out)
		}
	})

	t.Run("root with no consequence", func(t *testing.T) {
		got, out := run(t, rulesFixture(t, base, map[string]string{"prod-tenant/main.tf": strings.Replace(tfStorage, "one", "two", 1)}))
		if got.Level != score.Low || !strings.Contains(out, `"consequences": []`) {
			t.Errorf("level %s, want Low with no consequences:\n%s", got.Level, out)
		}
	})

	t.Run("roots added by the change are not read", func(t *testing.T) {
		got, _ := run(t, rulesFixture(t,
			map[string]string{"prod-tenant/main.tf": tfStorage},
			map[string]string{".risk/rules.json": rootsJSON, "prod-tenant/identity.tf": grantTF(210)}))
		if got.Level != score.Medium || got.Provenance.RulesHash == rootedHash {
			t.Errorf("level %s hash %s, want Medium under the built-in rules", got.Level, got.Provenance.RulesHash)
		}
	})
}
