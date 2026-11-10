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

// release8Get reads release definition 8: a read that writes nothing.
const release8Get = `az devops invoke --org "$org" --area release --resource definitions \
  --route-parameters project="$project" definitionId=8 -o json > "$out/before.json"
`

// release8Put writes release definition 8 back, its method on a continuation
// line.
const release8Put = `az devops invoke --org "$org" --area release --resource definitions \
  --route-parameters project="$project" --http-method PUT \
  --in-file "$out/intended.json" --api-version 7.1 -o json > "$out/put.json"
`

// release8 is a ticket script shaped like one that repoints a release
// definition out of band: a GET, a jq edit, a commented DELETE and a PUT.
func release8(put string) string {
	return "#!/usr/bin/env bash\nset -euo pipefail\norg=https://dev.azure.com/example\nproject=platform\nout=$(mktemp -d)\n\n" +
		release8Get +
		"\njq '.variables.Ring.value = \"2\"' \"$out/before.json\" > \"$out/intended.json\"\n\n" +
		"# az devops invoke --area release --resource definitions --http-method DELETE\n" +
		put
}

// ticketReadme describes the PUT in prose and spells the command: docs.
const ticketReadme = "# T-8\n\nRepoint release 8 to ring 2 with a PUT:\n\n" +
	"    az devops invoke --area release --resource definitions --http-method PUT --in-file intended.json\n" +
	"    az pipelines update --id 12\n"

// guarded is a ticket script shaped like a real one that narrows a release
// definition out of band: it reads the definition, refuses unless it is at
// the recorded revision, edits only the triggers and the stages' conditions,
// refuses if anything else differs, and writes only under --apply.
func guarded(put string) string {
	return "#!/usr/bin/env bash\nset -euo pipefail\norg=https://dev.azure.com/example\nproject=platform\nout=$1\n\n" +
		release8Get +
		"expected_revision=41\n" +
		"rev=$(jq -r .revision \"$out/before.json\")\n" +
		"[ \"$rev\" = \"$expected_revision\" ] || { echo \"revision $rev, want $expected_revision\" >&2; exit 1; }\n\n" +
		"jq '.triggers = [] | .environments[].conditions |= map(select(.conditionType != \"artifact\"))' \"$out/before.json\" > \"$out/intended.json\"\n\n" +
		"strip='del(.triggers, .environments[].conditions)'\n" +
		"[ \"$(jq -S \"$strip\" \"$out/before.json\")\" = \"$(jq -S \"$strip\" \"$out/intended.json\")\" ] \\\n" +
		"  || { echo \"refusing: more than triggers and conditions changed\" >&2; exit 1; }\n\n" +
		"[ \"${2:-}\" = \"--apply\" ] || { echo \"dry run\"; exit 0; }\n\n" +
		put
}

// guardedReadme documents the revert beside the guarded script: docs.
const guardedReadme = "# T-8\n\nTo revert, PUT before.json back, or restore the prior revision in the definition's history.\n"

// TestDefinitionWriteFloorsTheLevel drives the built binary: a ticket script
// whose continued az devops invoke PUTs a release definition is Medium though
// its shape is Low, with definition_update its only consequence and no
// trigger; the GET, the commented DELETE and the README beside it add
// nothing. A script that guards the same PUT behind a revision check and a
// strip-diff refusal reads the same, Medium; with a DELETE, or a PATCH of
// approvals, it is High as definition_write, and an update beside a delete
// carries both. Without a write method the script carries nothing. az
// pipelines update and a continued curl PUT to a build definition are
// updates; the create verbs and a curl DELETE are writes; a comment, a
// removed write and docs carry nothing; a line beside an unchanged write
// carries nothing, and a continuation line of the write does. The same
// writes in a test file carry nothing, beside a script that carries them.
func TestDefinitionWriteFloorsTheLevel(t *testing.T) {
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
	marshal := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	// nothing asserts a Low report with no consequence and no trigger.
	nothing := func(t *testing.T, dir string) {
		t.Helper()
		out := run(t, dir)
		got := decodeReport(t, out)
		if got.Level != score.Low || len(got.Consequences) != 0 || len(got.Triggers) != 0 ||
			!strings.Contains(out, `"consequences": []`) {
			t.Errorf("level %s consequences %s triggers %s, want Low, [], []", got.Level, marshal(got.Consequences), marshal(got.Triggers))
		}
	}
	base := map[string]string{"go.mod": "module example.com/m\n"}
	const script = "tickets/T-8/release-8.sh"
	const putType = "az devops invoke --area release --http-method PUT"
	release8Delete := strings.Replace(release8Put, "PUT", "DELETE", 1)
	release8Approve := strings.Replace(strings.Replace(release8Put, "definitions", "approvals", 1), "PUT", "PATCH", 1)
	// updated asserts the script's PUT of the definition reads as an update:
	// Medium by its floor though Low by shape, with no trigger.
	updated := func(t *testing.T, dir string) {
		t.Helper()
		out := run(t, dir)
		got := decodeReport(t, out)
		if got.Level != score.Medium || got.ShapeLevel != score.Low || got.Total >= 20 || got.Total != sum(got) {
			t.Errorf("level %s shape %s total %d (points sum %d), want Medium, Low, a Low total equal to the sum",
				got.Level, got.ShapeLevel, got.Total, sum(got))
		}
		if !reflect.DeepEqual(got.FlooredBy, []string{"definition_update"}) || len(got.Triggers) != 0 {
			t.Errorf("floored_by %v triggers %s, want [definition_update] and none", got.FlooredBy, marshal(got.Triggers))
		}
		want := `[{"class":"definition_update","paths":["` + script + `"],"types":["` + putType + `"],"floor":"Medium"}]`
		if c := marshal(got.Consequences); c != want {
			t.Errorf("consequences = %s, want %s", c, want)
		}
		if strings.Contains(out, "definition_write") {
			t.Errorf("report names definition_write:\n%s", out)
		}
		md := run(t, dir, "--format", "markdown")
		for _, w := range []string{"- definition_update, floor Medium: paths `" + script + "`; types `" + putType + "`", report.SplitNote} {
			if !strings.Contains(md, w) {
				t.Errorf("markdown lacks %q:\n%s", w, md)
			}
		}
		if strings.Contains(md, "High by class") {
			t.Errorf("markdown lists a High trigger:\n%s", md)
		}
	}
	// written asserts High, floored by definition_write alone, with want as
	// the consequences.
	written := func(t *testing.T, dir, want string) {
		t.Helper()
		got := decodeReport(t, run(t, dir))
		if got.Level != score.High || !reflect.DeepEqual(got.FlooredBy, []string{"definition_write"}) {
			t.Errorf("level %s floored_by %v, want High [definition_write]", got.Level, got.FlooredBy)
		}
		if c := marshal(got.Consequences); c != want {
			t.Errorf("consequences = %s, want %s", c, want)
		}
		if len(got.Triggers) != 1 || got.Triggers[0].Class != "definition_write" {
			t.Errorf("triggers %s, want definition_write alone", marshal(got.Triggers))
		}
	}
	write := func(typ string) string {
		return `{"class":"definition_write","paths":["` + script + `"],"types":["` + typ + `"],"floor":"High"}`
	}

	t.Run("release-8 script", func(t *testing.T) {
		dir := rulesFixture(t, base, map[string]string{script: release8(release8Put), "tickets/T-8/README.md": ticketReadme})
		updated(t, dir)
	})

	t.Run("guarded PUT", func(t *testing.T) {
		dir := rulesFixture(t, base, map[string]string{script: guarded(release8Put), "tickets/T-8/README.md": guardedReadme})
		updated(t, dir)

		for _, tt := range []struct {
			name, consequences string
			level              score.Level
		}{
			{"plan naming the update", `,"consequences":["definition_update"]`, score.Medium},
			{"plan naming the write", `,"consequences":["definition_write"]`, score.High},
			{"plan naming none", ``, score.Low},
		} {
			plan := filepath.Join(t.TempDir(), "plan.json")
			if err := os.WriteFile(plan, []byte(`{"target":"main","files":[{"path":"`+script+`","added":20`+tt.consequences+`}]}`), 0o644); err != nil {
				t.Fatal(err)
			}
			code, out, stderr := runEstimate(t, bin, dir, plan)
			if code != 0 {
				t.Fatalf("%s: estimate exit %d: %s", tt.name, code, stderr)
			}
			if est := decodeReport(t, out); est.Level != tt.level {
				t.Errorf("%s: estimate level %s, want %s", tt.name, est.Level, tt.level)
			}
		}
	})

	t.Run("guarded DELETE", func(t *testing.T) {
		written(t, rulesFixture(t, base, map[string]string{script: guarded(release8Delete)}),
			"["+write("az devops invoke --area release --http-method DELETE")+"]")
	})

	t.Run("guarded approvals PATCH", func(t *testing.T) {
		written(t, rulesFixture(t, base, map[string]string{script: guarded(release8Approve)}),
			"["+write("az devops invoke --area release --http-method PATCH")+"]")
	})

	t.Run("update beside a delete", func(t *testing.T) {
		written(t, rulesFixture(t, base, map[string]string{script: guarded(release8Put + release8Delete)}),
			`[{"class":"definition_update","paths":["`+script+`"],"types":["`+putType+`"],"floor":"Medium"},`+
				write("az devops invoke --area release --http-method DELETE")+"]")
	})

	t.Run("creates and deletes", func(t *testing.T) {
		dir := rulesFixture(t, base, map[string]string{script: "#!/bin/sh\naz pipelines create --name x --yml-path a.yml\n" +
			"az pipelines release create --definition-id 8\n" +
			"curl -X DELETE https://dev.azure.com/example/platform/_apis/build/definitions/12\n"})
		written(t, dir, `[{"class":"definition_write","paths":["`+script+`"],"types":["az pipelines create","az pipelines release create","curl DELETE _apis/build/definitions"],"floor":"High"}]`)
	})

	t.Run("no write method", func(t *testing.T) {
		for name, put := range map[string]string{
			"PUT turned to GET":    strings.Replace(release8Put, "PUT", "GET", 1),
			"http-method removed":  strings.Replace(release8Put, " --http-method PUT", "", 1),
			"only the GET present": "",
		} {
			t.Run(name, func(t *testing.T) {
				nothing(t, rulesFixture(t, base, map[string]string{script: release8(put)}))
			})
		}
	})

	t.Run("pipelines update and curl PUT", func(t *testing.T) {
		dir := rulesFixture(t, base, map[string]string{
			"ops/repoint.sh": "#!/bin/sh\naz pipelines update --id 12 --branch refs/heads/ring2\n",
			"ops/put.sh": "#!/bin/sh\ncurl -sS -X PUT \\\n  -H 'Content-Type: application/json' \\\n" +
				"  -d @def.json \\\n  \"https://dev.azure.com/example/platform/_apis/build/definitions/12?api-version=7.1\"\n",
		})
		got := decodeReport(t, run(t, dir))
		want := `[{"class":"definition_update","paths":["ops/put.sh","ops/repoint.sh"],"types":["az pipelines update","curl PUT _apis/build/definitions"],"floor":"Medium"}]`
		if c := marshal(got.Consequences); got.Level != score.Medium || c != want {
			t.Errorf("level %s consequences %s, want Medium, %s", got.Level, c, want)
		}
	})

	t.Run("comment, removal and docs", func(t *testing.T) {
		dir := rulesFixture(t, map[string]string{"go.mod": "module example.com/m\n", script: release8(release8Put)}, map[string]string{
			script:            release8("# az pipelines update --id 12\n"),
			"docs/runbook.md": "Run:\n\n" + release8Put + "\nthen `az pipelines update --id 12`.\n",
			"NOTES.md":        "curl -X PUT https://dev.azure.com/o/p/_apis/build/definitions/12\n",
		})
		nothing(t, dir)
	})

	t.Run("beside an unchanged write", func(t *testing.T) {
		withBase := map[string]string{"go.mod": "module example.com/m\n", script: release8(release8Put)}
		jq := strings.Replace(release8(release8Put), `= "2"`, `= "3"`, 1)
		if jq == release8(release8Put) {
			t.Fatal("jq line not changed")
		}
		nothing(t, rulesFixture(t, withBase, map[string]string{script: jq}))

		inFile := release8(strings.Replace(release8Put, "intended.json", "intended-v2.json", 1))
		got := decodeReport(t, run(t, rulesFixture(t, withBase, map[string]string{script: inFile})))
		want := `[{"class":"definition_update","paths":["tickets/T-8/release-8.sh"],"types":["az devops invoke --area release --http-method PUT"],"floor":"Medium"}]`
		if c := marshal(got.Consequences); got.Level != score.Medium || c != want {
			t.Errorf("level %s consequences %s, want Medium, %s", got.Level, c, want)
		}
	})

	t.Run("test files", func(t *testing.T) {
		tests := map[string]string{
			"tests/release-8.sh":  release8(release8Put + release8Delete),
			"ops/repoint_test.go": "package ops\n\nconst put = `" + release8Put + release8Delete + "`\n",
		}
		nothing(t, rulesFixture(t, base, tests))

		alone := decodeReport(t, run(t, rulesFixture(t, base, map[string]string{script: release8(release8Put + release8Delete)})))
		tests[script] = release8(release8Put + release8Delete)
		got := decodeReport(t, run(t, rulesFixture(t, base, tests)))
		if got.Level != score.High || !reflect.DeepEqual(got.FlooredBy, []string{"definition_write"}) {
			t.Errorf("level %s floored_by %v, want High [definition_write]", got.Level, got.FlooredBy)
		}
		if len(got.Consequences) != 2 || marshal(got.Consequences) != marshal(alone.Consequences) {
			t.Errorf("consequences %s, want the script alone: %s", marshal(got.Consequences), marshal(alone.Consequences))
		}
		for _, c := range got.Consequences {
			if !reflect.DeepEqual(c.Paths, []string{script}) {
				t.Errorf("%s paths %v, want the script only", c.Class, c.Paths)
			}
		}
	})
}
