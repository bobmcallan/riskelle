//go:build integration

package tests

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/bobmcallan/riskelle/internal/report"
	"github.com/bobmcallan/riskelle/internal/score"
)

const rgTF = `resource "azurerm_resource_group" "rg" {
  name     = "rg"
  location = "uksouth"
}
`

// membershipTF holds a group member, a group role management policy and an
// administrative unit member.
const membershipTF = `resource "azuread_group_member" "reader" {
  group_object_id  = azuread_group.readers.object_id
  member_object_id = data.azuread_user.u.object_id
}

resource "azuread_group_role_management_policy" "readers" {
  group_id = azuread_group.readers.id
  role_id  = "member"
  activation_rules {
    maximum_duration = "PT1H"
  }
}

resource "azuread_administrative_unit_member" "u" {
  administrative_unit_object_id = azuread_administrative_unit.au.object_id
  member_object_id              = data.azuread_user.u.object_id
}
`

// groupsTF is a group with a multi-line members list and single-line owners.
const groupsTF = `resource "azuread_group" "platform" {
  display_name     = "platform"
  description      = "Platform team"
  security_enabled = true
  members = [
    "00000000-0000-0000-0000-000000000001",
    "00000000-0000-0000-0000-000000000002",
  ]
  owners = ["00000000-0000-0000-0000-000000000009"]
}
`

// pr2190TF models a new group whose members are computed from users.
const pr2190TF = `data "azuread_users" "platform_readers" {
  user_principal_names = [
    "a@example.com",
    "b@example.com",
  ]
}

resource "azuread_group" "platform_readers" {
  display_name     = "platform-readers"
  security_enabled = true
  members          = data.azuread_users.platform_readers.object_ids
}
`

// TestGroupMembershipFloorsTheLevel drives the built binary over changes
// modelled on real ones: a group member, a group role management policy and
// an administrative unit member are identity_rights whether added, altered
// or removed; an azuread_group carries it only when a changed line falls in
// its members or owners; a new group with members from data.azuread_users,
// Low by shape, is reported Medium.
func TestGroupMembershipFloorsTheLevel(t *testing.T) {
	bin := buildRiskelle(t, t.TempDir())
	run := func(t *testing.T, dir string) (report.Report, string) {
		t.Helper()
		code, out, stderr := runScore(t, bin, dir, "--base", "main", "--head", "topic")
		if code != 0 {
			t.Fatalf("score exit %d: %s", code, stderr)
		}
		return decodeReport(t, out), out
	}
	medium := func(t *testing.T, r report.Report, path, types string) {
		t.Helper()
		want := `[{"class":"identity_rights","paths":["` + path + `"],"types":` + types + `,"floor":"Medium"}]`
		if b, _ := json.Marshal(r.Consequences); r.Level != score.Medium || string(b) != want {
			t.Errorf("level %s consequences = %s\nwant Medium %s", r.Level, b, want)
		}
	}
	base := map[string]string{"identity/main.tf": rgTF}
	withMembership := map[string]string{"identity/main.tf": rgTF, "identity/membership.tf": membershipTF}
	withGroups := map[string]string{"identity/main.tf": rgTF, "identity/groups.tf": groupsTF}

	t.Run("member, role management policy and administrative unit member added", func(t *testing.T) {
		got, _ := run(t, rulesFixture(t, base, map[string]string{"identity/membership.tf": membershipTF}))
		medium(t, got, "identity/membership.tf",
			`["azuread_administrative_unit_member","azuread_group_member","azuread_group_role_management_policy"]`)
	})
	t.Run("administrative unit member altered", func(t *testing.T) {
		alt := strings.Replace(membershipTF, "member_object_id              = data.azuread_user.u.object_id",
			"member_object_id              = data.azuread_user.v.object_id", 1)
		got, _ := run(t, rulesFixture(t, withMembership, map[string]string{"identity/membership.tf": alt}))
		medium(t, got, "identity/membership.tf", `["azuread_administrative_unit_member"]`)
	})
	t.Run("group member block removed", func(t *testing.T) {
		i := strings.Index(membershipTF, `resource "azuread_group_role_management_policy"`)
		got, _ := run(t, rulesFixture(t, withMembership, map[string]string{"identity/membership.tf": membershipTF[i:]}))
		medium(t, got, "identity/membership.tf", `["azuread_group_member"]`)
	})

	t.Run("member added to a multi-line list", func(t *testing.T) {
		alt := strings.Replace(groupsTF, "    \"00000000-0000-0000-0000-000000000002\",\n",
			"    \"00000000-0000-0000-0000-000000000002\",\n    \"00000000-0000-0000-0000-000000000003\",\n", 1)
		got, _ := run(t, rulesFixture(t, withGroups, map[string]string{"identity/groups.tf": alt}))
		medium(t, got, "identity/groups.tf", `["azuread_group.members"]`)
	})
	t.Run("owners altered", func(t *testing.T) {
		alt := strings.Replace(groupsTF, "000000000009", "000000000008", 1)
		got, _ := run(t, rulesFixture(t, withGroups, map[string]string{"identity/groups.tf": alt}))
		medium(t, got, "identity/groups.tf", `["azuread_group.owners"]`)
	})
	t.Run("member removed", func(t *testing.T) {
		alt := strings.Replace(groupsTF, "    \"00000000-0000-0000-0000-000000000001\",\n", "", 1)
		got, _ := run(t, rulesFixture(t, withGroups, map[string]string{"identity/groups.tf": alt}))
		medium(t, got, "identity/groups.tf", `["azuread_group.members"]`)
	})
	t.Run("description and display_name only", func(t *testing.T) {
		alt := strings.Replace(strings.Replace(groupsTF, "Platform team", "Platform engineering", 1), `"platform"`, `"platform-eng"`, 1)
		got, out := run(t, rulesFixture(t, withGroups, map[string]string{"identity/groups.tf": alt}))
		if got.Level != score.Low || !strings.Contains(out, `"consequences": []`) || !strings.Contains(out, `"floored_by": []`) {
			t.Errorf("level %s consequences %+v, want Low and none", got.Level, got.Consequences)
		}
	})

	t.Run("PR 2190: new group with members from data.azuread_users", func(t *testing.T) {
		got, _ := run(t, rulesFixture(t, base, map[string]string{"identity/groups.tf": pr2190TF}))
		n := 0
		for _, p := range got.Breakdown {
			n += p.Points
		}
		if got.Total >= 20 || got.Total != n || got.ShapeLevel != score.Low {
			t.Errorf("total %d (sum %d) shape %s, want a Low total equal to the sum", got.Total, n, got.ShapeLevel)
		}
		medium(t, got, "identity/groups.tf", `["azuread_group.members"]`)
	})
}
