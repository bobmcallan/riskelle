//go:build integration

package tests

import (
	"context"
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/bobmcallan/riskelle/internal/facts"
	"github.com/bobmcallan/riskelle/internal/rules"
	"github.com/bobmcallan/riskelle/internal/score"
	"github.com/bobmcallan/riskelle/internal/version"
)

// rulesFixture commits files on main, branches topic and commits topic's
// files there, then returns to main.
func rulesFixture(t *testing.T, base, topic map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	fixtureGit(t, dir, "init", "--quiet", "--initial-branch=main")
	for _, step := range []struct {
		files  map[string]string
		branch []string
	}{{base, nil}, {topic, []string{"switch", "--quiet", "-c", "topic"}}} {
		if step.branch != nil {
			fixtureGit(t, dir, step.branch...)
		}
		for name, content := range step.files {
			writeFile(t, dir, name, content)
		}
		fixtureGit(t, dir, "add", ".")
		fixtureGit(t, dir, "commit", "--quiet", "-m", "commit")
	}
	fixtureGit(t, dir, "switch", "--quiet", "main")
	return dir
}

// collectAndScore collects main..topic and scores it with the repository's
// rules, the way the score command will, from an empty working directory.
func collectAndScore(t *testing.T, dir string) (facts.Facts, score.Result, error) {
	t.Helper()
	t.Chdir(t.TempDir())
	f, err := facts.Collect(context.Background(), dir, "main", "topic")
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	res, err := rules.Score(context.Background(), dir, f)
	return f, res, err
}

// TestRulesReadFromTargetBranch: .risk/rules.json differs at the merge base,
// on the head (which tries to hide its own code as generated), at the target
// tip and in the working tree. Only the target tip's rules score the change.
func TestRulesReadFromTargetBranch(t *testing.T) {
	dir := rulesFixture(t,
		map[string]string{"go.mod": "module example.com/m\n", ".risk/rules.json": `{"generated":["gen/**"]}` + "\n"},
		map[string]string{
			".risk/rules.json": `{"generated":["gen/**","src/**"]}` + "\n",
			"src/app.go":       numbered("package src // ", 100),
			"gen/api.go":       numbered("package gen // ", 300),
			"assets/app.js":    numbered("// ", 100),
		})
	writeFile(t, dir, ".risk/rules.json", `{"generated":["gen/**","assets/**"]}`+"\n")
	fixtureGit(t, dir, "commit", "--quiet", "-am", "trunk moves the rules")
	writeFile(t, dir, ".risk/rules.json", `{"generated":["**"]}`+"\n") // uncommitted

	f, res, err := collectAndScore(t, dir)
	if err != nil {
		t.Fatalf("rules.Score: %v", err)
	}
	effective := rules.Apply(score.Default(), rules.File{Generated: []string{"assets/**", "gen/**"}})
	out, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	// size: src/app.go's 100 lines plus the rules file's +1 -1 = 102 (5
	// points); assets/** from the target tip leaves out assets/app.js.
	// Other measures' entries are pinned by their own tests.
	for _, want := range []string{
		`{"total":` + strconv.Itoa(score.Score(f, effective).Total) + `,`,
		`{"measure":"size","fact":"lines","value":102,"min":51,"points":5,` +
			`"from":[".risk/rules.json","src/app.go"],"excluded":["assets/app.js","gen/api.go"]}`,
		`"provenance":{"riskelle_version":"` + version.Get().String() + `","rules_source":".risk/rules.json",` +
			`"rules_commit":"` + f.Base + `","rules_hash":"` + rules.Hash(effective) + `"}}`,
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("result =\n%s\nwant it to contain\n%s", out, want)
		}
	}
	if f.Base == f.MergeBase || version.Get().String() == "" {
		t.Errorf("fixture: target tip %s must differ from merge base %s, and version be set", f.Base, f.MergeBase)
	}
}

// TestRulesDefaultWithoutFile: the target branch has no .risk/rules.json, so
// the built-in rules apply unchanged though the head adds one.
func TestRulesDefaultWithoutFile(t *testing.T) {
	dir := rulesFixture(t,
		map[string]string{"go.mod": "module example.com/m\n"},
		map[string]string{".risk/rules.json": `{"generated":["src/**"]}`, "src/app.go": numbered("package src // ", 100)})

	f, res, err := collectAndScore(t, dir)
	if err != nil {
		t.Fatalf("rules.Score: %v", err)
	}
	builtin := score.Score(f, score.Default())
	builtin.Provenance = score.Provenance{
		Version: version.Get().String(), RulesSource: "built-in",
		RulesCommit: f.Base, RulesHash: rules.Hash(score.Default()),
	}
	if !reflect.DeepEqual(res, builtin) || res.Breakdown[0].Value != 101 {
		t.Errorf("result %+v, want the built-in result counting src/app.go %+v", res, builtin)
	}
}

// TestRulesInvalidFileFails: an invalid rules file at the target branch fails
// the run, naming the file and commit, rather than falling back.
func TestRulesInvalidFileFails(t *testing.T) {
	dir := rulesFixture(t, map[string]string{".risk/rules.json": `{"generated":[""]}`}, map[string]string{"a.go": "package a\n"})
	f, _, err := collectAndScore(t, dir)
	if err == nil || !strings.Contains(err.Error(), ".risk/rules.json") || !strings.Contains(err.Error(), f.Base) {
		t.Errorf("rules.Score error = %v, want one naming .risk/rules.json and %s", err, f.Base)
	}
}
