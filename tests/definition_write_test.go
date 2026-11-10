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

// TestDefinitionWriteFloorsTheLevel drives the built binary: a ticket script
// whose continued az devops invoke PUTs a release definition is High though
// its shape is Low, with definition_write its only consequence and trigger;
// the GET, the commented DELETE and the README beside it add nothing.
// Without a write method the script carries nothing. az pipelines update and
// a continued curl PUT to a build definition carry it; a comment, a removed
// write and docs carry nothing; a line beside an unchanged write carries
// nothing, and a continuation line of the write does. The same write in a
// test file carries nothing, beside a script that carries it.
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

	t.Run("release-8 script", func(t *testing.T) {
		dir := rulesFixture(t, base, map[string]string{script: release8(release8Put), "tickets/T-8/README.md": ticketReadme})
		got := decodeReport(t, run(t, dir))
		if got.Level != score.High || got.ShapeLevel != score.Low || got.Total >= 20 || got.Total != sum(got) {
			t.Errorf("level %s shape %s total %d (points sum %d), want High, Low, a Low total equal to the sum",
				got.Level, got.ShapeLevel, got.Total, sum(got))
		}
		if !reflect.DeepEqual(got.FlooredBy, []string{"definition_write"}) {
			t.Errorf("floored_by %v, want [definition_write]", got.FlooredBy)
		}
		want := `[{"class":"definition_write","paths":["tickets/T-8/release-8.sh"],"types":["az devops invoke --area release --http-method PUT"],"floor":"High"}]`
		if c := marshal(got.Consequences); c != want {
			t.Errorf("consequences = %s, want %s", c, want)
		}
		want = `[{"class":"definition_write","paths":["tickets/T-8/release-8.sh"],"types":["az devops invoke --area release --http-method PUT"],"why":"class"}]`
		if tr := marshal(got.Triggers); tr != want {
			t.Errorf("triggers = %s, want %s", tr, want)
		}

		md := run(t, dir, "--format", "markdown")
		for _, w := range []string{
			"- definition_write, floor High: paths `tickets/T-8/release-8.sh`; types `az devops invoke --area release --http-method PUT`",
			"- definition_write, High by class: paths `tickets/T-8/release-8.sh`",
			report.SplitNote,
		} {
			if !strings.Contains(md, w) {
				t.Errorf("markdown lacks %q:\n%s", w, md)
			}
		}

		for _, tt := range []struct {
			name, consequences string
			level              score.Level
		}{
			{"plan naming the class", `,"consequences":["definition_write"]`, score.High},
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
		want := `[{"class":"definition_write","paths":["ops/put.sh","ops/repoint.sh"],"types":["az pipelines update","curl PUT _apis/build/definitions"],"floor":"High"}]`
		if c := marshal(got.Consequences); got.Level != score.High || c != want {
			t.Errorf("level %s consequences %s, want High, %s", got.Level, c, want)
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
		want := `[{"class":"definition_write","paths":["tickets/T-8/release-8.sh"],"types":["az devops invoke --area release --http-method PUT"],"floor":"High"}]`
		if c := marshal(got.Consequences); got.Level != score.High || c != want {
			t.Errorf("level %s consequences %s, want High, %s", got.Level, c, want)
		}
	})

	t.Run("test files", func(t *testing.T) {
		tests := map[string]string{
			"tests/release-8.sh":  release8(release8Put),
			"ops/repoint_test.go": "package ops\n\nconst put = `" + release8Put + "`\n",
		}
		nothing(t, rulesFixture(t, base, tests))

		alone := decodeReport(t, run(t, rulesFixture(t, base, map[string]string{script: release8(release8Put)})))
		tests[script] = release8(release8Put)
		got := decodeReport(t, run(t, rulesFixture(t, base, tests)))
		if got.Level != score.High || !reflect.DeepEqual(got.FlooredBy, []string{"definition_write"}) {
			t.Errorf("level %s floored_by %v, want High [definition_write]", got.Level, got.FlooredBy)
		}
		if len(got.Consequences) != 1 || !reflect.DeepEqual(got.Consequences[0].Paths, []string{script}) ||
			got.Consequences[0].Floor != score.High || marshal(got.Consequences) != marshal(alone.Consequences) {
			t.Errorf("consequences %s, want the script alone: %s", marshal(got.Consequences), marshal(alone.Consequences))
		}
	})
}
