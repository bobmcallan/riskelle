//go:build integration

package tests

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/bobmcallan/riskelle/internal/report"
	"github.com/bobmcallan/riskelle/internal/score"
)

// devopsMainTF is a delivery root whose required-template check takes its
// ref from a variable, beside an uncatalogued repository using another.
const devopsMainTF = `resource "azuredevops_check_required_template" "release" {
  project_id           = "p"
  target_resource_id   = "env"
  target_resource_type = "environment"
  required_template {
    repository_type = "azuregit"
    repository_name = "pipelines"
    repository_ref  = var.release_template_ref
    template_path   = "templates/release.yml"
  }
}

resource "azuredevops_git_repository" "pipelines" {
  project_id = "p"
  name       = var.repo_name
}
`

const devopsVariablesTF = `variable "release_template_ref" {
  description = "Which ref of the templates repository may run as the deploy identity."
  type        = string
  default     = "refs/heads/main"
  validation {
    condition     = can(regex("^refs/heads/", var.release_template_ref))
    error_message = "A branch ref."
  }
}

variable "repo_name" {
  type    = string
  default = "pipelines"
}
`

const devopsTfvars = `release_template_ref = "refs/heads/main"
repo_name            = "pipelines"
`

// TestVariableReferenceFloorsTheLevel drives the built binary over a change
// modelled on a real one: moving a variable that a required-template check
// uses, in its tfvars and its declaration, is Low by shape and reported
// High, as access_widened (a changed value counts as altered), and under a
// production root every consequence names the root; removing the assignment
// is Medium, identity_rights alone; a variable used only by an uncatalogued block, one set in another directory,
// a description-only change and an unused name carry nothing.
func TestVariableReferenceFloorsTheLevel(t *testing.T) {
	bin := buildRiskelle(t, t.TempDir())
	run := func(t *testing.T, dir string) (report.Report, string) {
		t.Helper()
		code, out, stderr := runScore(t, bin, dir, "--base", "main", "--head", "topic")
		if code != 0 {
			t.Fatalf("score exit %d: %s", code, stderr)
		}
		return decodeReport(t, out), out
	}
	base := func(extra map[string]string) map[string]string {
		b := map[string]string{
			"delivery/main.tf":          devopsMainTF,
			"delivery/variables.tf":     devopsVariablesTF,
			"delivery/terraform.tfvars": devopsTfvars,
			"sandbox/variables.tf":      devopsVariablesTF,
			"sandbox/terraform.tfvars":  devopsTfvars,
		}
		for k, v := range extra {
			b[k] = v
		}
		return b
	}
	moved := strings.Replace(devopsTfvars, "refs/heads/main", "refs/tags/release-2026.11", 1)
	via := `"azuredevops_check_required_template via var.release_template_ref"`

	t.Run("31885b0", func(t *testing.T) {
		vars := strings.Replace(strings.Replace(devopsVariablesTF, `default     = "refs/heads/main"`, `default     = "refs/tags/release-2026.11"`, 1),
			`"^refs/heads/"`, `"^refs/(heads|tags)/"`, 1)
		got, _ := run(t, rulesFixture(t, base(nil), map[string]string{"delivery/terraform.tfvars": moved, "delivery/variables.tf": vars}))
		n := 0
		for _, p := range got.Breakdown {
			n += p.Points
		}
		if got.Total >= 20 || got.Total != n || got.ShapeLevel != score.Low || got.Level != score.High ||
			len(got.FlooredBy) != 1 || got.FlooredBy[0] != "access_widened" {
			t.Errorf("total %d (sum %d) shape %s level %s floored_by %v, want Low floored High by access_widened",
				got.Total, n, got.ShapeLevel, got.Level, got.FlooredBy)
		}
		paths := `"paths":["delivery/terraform.tfvars","delivery/variables.tf"],"types":[` + via + `]`
		want := `[{"class":"access_widened",` + paths + `,"floor":"High"},{"class":"identity_rights",` + paths + `,"floor":"Medium"}]`
		if b, _ := json.Marshal(got.Consequences); string(b) != want {
			t.Errorf("consequences = %s\nwant %s", b, want)
		}
	})

	t.Run("assignment removed", func(t *testing.T) {
		removed := strings.Replace(devopsTfvars, "release_template_ref = \"refs/heads/main\"\n", "", 1)
		got, _ := run(t, rulesFixture(t, base(nil), map[string]string{"delivery/terraform.tfvars": removed}))
		want := `[{"class":"identity_rights","paths":["delivery/terraform.tfvars"],"types":[` + via + `],"floor":"Medium"}]`
		if b, _ := json.Marshal(got.Consequences); got.Level != score.Medium || string(b) != want {
			t.Errorf("level %s consequences = %s\nwant Medium %s", got.Level, b, want)
		}
	})

	t.Run("under a production root", func(t *testing.T) {
		got, _ := run(t, rulesFixture(t, base(map[string]string{".risk/rules.json": `{"production_roots":["delivery/**"]}`}),
			map[string]string{"delivery/terraform.tfvars": moved}))
		if got.Level != score.High || len(got.Consequences) != 2 {
			t.Errorf("level %s consequences %+v, want High with two", got.Level, got.Consequences)
		}
		for _, c := range got.Consequences {
			if c.Floor != score.High || len(c.ProductionRoots) != 1 || c.ProductionRoots[0] != "delivery/**" {
				t.Errorf("consequence %+v, want High naming delivery/**", c)
			}
		}
	})

	for name, topic := range map[string]map[string]string{
		"uncatalogued user":  {"delivery/terraform.tfvars": strings.Replace(devopsTfvars, `"pipelines"`, `"pipelines2"`, 1)},
		"another directory":  {"sandbox/terraform.tfvars": moved},
		"description only":   {"delivery/variables.tf": strings.Replace(devopsVariablesTF, "may run as", "may run with", 1)},
		"unused prefix name": {"delivery/terraform.tfvars": devopsTfvars + "release_template_ref_v2 = \"x\"\n"},
	} {
		t.Run(name, func(t *testing.T) {
			got, out := run(t, rulesFixture(t, base(nil), topic))
			if got.Level != score.Low || !strings.Contains(out, `"consequences": []`) {
				t.Errorf("level %s consequences %+v, want Low and none", got.Level, got.Consequences)
			}
		})
	}
}
