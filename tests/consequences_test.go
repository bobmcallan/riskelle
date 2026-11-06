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

// The base Terraform root: a storage account and a role assignment, laid out
// as terraform fmt writes them.
const (
	tfStorage = "resource \"azurerm_storage_account\" \"s\" {\n  name = \"one\"\n}\n"
	tfRole    = "resource \"azurerm_role_assignment\" \"r\" {\n  role = \"Reader\"\n}\n"
)

// decodeReport decodes a JSON report, refusing unknown fields.
func decodeReport(t *testing.T, out string) report.Report {
	t.Helper()
	var r report.Report
	dec := json.NewDecoder(strings.NewReader(out))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&r); err != nil {
		t.Fatalf("report: %v\n%s", err, out)
	}
	return r
}

// TestConsequenceFloorsTheLevel drives the built binary: a change to an
// identity grant's block is reported Medium though its total is Low, the
// total is the sum of its points, and the Markdown names the consequence;
// estimate with a plan naming the class agrees; a change to an uncatalogued
// block scores at its own level with no consequence.
func TestConsequenceFloorsTheLevel(t *testing.T) {
	bin := buildRiskelle(t, t.TempDir())
	run := func(t *testing.T, dir string, args ...string) string {
		t.Helper()
		code, out, stderr := runScore(t, bin, dir, append([]string{"--base", "main", "--head", "topic"}, args...)...)
		if code != 0 {
			t.Fatalf("score exit %d: %s", code, stderr)
		}
		return out
	}
	sum := func(r report.Report) int {
		n := 0
		for _, p := range r.Breakdown {
			n += p.Points
		}
		return n
	}
	base := map[string]string{"iam/main.tf": tfStorage + "\n" + tfRole}

	t.Run("identity grant", func(t *testing.T) {
		dir := rulesFixture(t, base, map[string]string{"iam/main.tf": tfStorage + "\n" + strings.Replace(tfRole, "Reader", "Owner", 1)})
		got := decodeReport(t, run(t, dir))
		if got.Level != score.Medium || got.Total >= 20 || got.Total != sum(got) {
			t.Errorf("level %s total %d (points sum %d), want Medium with a Low total equal to the sum", got.Level, got.Total, sum(got))
		}
		want := `[{"class":"identity_rights","paths":["iam/main.tf"],"types":["azurerm_role_assignment"],"floor":"Medium"}]`
		if b, _ := json.Marshal(got.Consequences); string(b) != want {
			t.Errorf("consequences = %s, want %s", b, want)
		}

		md := run(t, dir, "--format", "markdown")
		for _, w := range []string{"### " + report.FloorHeading, report.FloorNote,
			"- identity_rights, floor Medium: paths `iam/main.tf`; types `azurerm_role_assignment`"} {
			if !strings.Contains(md, w) {
				t.Errorf("markdown lacks %q:\n%s", w, md)
			}
		}

		for _, tt := range []struct {
			name, consequences string
			level              score.Level
		}{
			{"plan naming the class", `,"consequences":["identity_rights"]`, score.Medium},
			{"plan naming none", ``, score.Low},
		} {
			plan := filepath.Join(t.TempDir(), "plan.json")
			os.WriteFile(plan, []byte(`{"target":"main","files":[{"path":"iam/main.tf","added":1,"removed":1`+tt.consequences+`}]}`), 0o644)
			code, out, stderr := runEstimate(t, bin, dir, plan)
			if code != 0 {
				t.Fatalf("%s: estimate exit %d: %s", tt.name, code, stderr)
			}
			est := decodeReport(t, out)
			if est.Total != got.Total || est.Level != tt.level {
				t.Errorf("%s: estimate total %d level %s, want %d %s", tt.name, est.Total, est.Level, got.Total, tt.level)
			}
			if tt.level == score.Medium {
				wantC := []score.Consequence{{Floor: score.Medium}}
				wantC[0].Class, wantC[0].Paths, wantC[0].Types = "identity_rights", []string{"iam/main.tf"}, []string{}
				if !reflect.DeepEqual(est.Consequences, wantC) {
					t.Errorf("%s: consequences = %+v, want %+v", tt.name, est.Consequences, wantC)
				}
			}
		}
	})

	t.Run("no catalogued type", func(t *testing.T) {
		dir := rulesFixture(t, base, map[string]string{"iam/main.tf": strings.Replace(tfStorage, "one", "two", 1) + "\n" + tfRole})
		out := run(t, dir)
		got := decodeReport(t, out)
		if got.Level != score.Low || len(got.Consequences) != 0 || !strings.Contains(out, `"consequences": []`) {
			t.Errorf("level %s consequences %+v, want Low and []", got.Level, got.Consequences)
		}
		if md := run(t, dir, "--format", "markdown"); strings.Contains(md, report.FloorHeading) {
			t.Errorf("markdown has a consequences section:\n%s", md)
		}
	})
}
