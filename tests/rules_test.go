//go:build integration

package tests

import (
	"context"
	"encoding/json"
	"os"
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
			`"rules_commit":"` + f.Base + `","rules_hash":"` + rules.Hash(effective) + `",` +
			`"exclude_non_code_files":true,"exclude_log_files":true}}`,
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
// the built-in rules apply with their default toggles though the head adds one.
func TestRulesDefaultWithoutFile(t *testing.T) {
	dir := rulesFixture(t,
		map[string]string{"go.mod": "module example.com/m\n"},
		map[string]string{".risk/rules.json": `{"generated":["src/**"]}`, "src/app.go": numbered("package src // ", 100)})

	f, res, err := collectAndScore(t, dir)
	if err != nil {
		t.Fatalf("rules.Score: %v", err)
	}
	def := rules.Apply(score.Default(), rules.File{})
	builtin := score.Score(f, def)
	builtin.Provenance = score.Provenance{
		Version: version.Get().String(), RulesSource: "built-in",
		RulesCommit: f.Base, RulesHash: rules.Hash(def),
		ExcludeNonCodeFiles: true, ExcludeLogFiles: true,
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

// TestRulesLoweringSettingRefused: the target branch's .risk/rules.json
// beside an additive generated path tries to replace the built-in size bands,
// which could lower a score. The run fails naming the file, the commit and
// the refused setting, and yields no result.
func TestRulesLoweringSettingRefused(t *testing.T) {
	dir := rulesFixture(t,
		map[string]string{".risk/rules.json": `{"generated":["gen/**"],"size_lines":[{"min":0,"points":0}]}` + "\n"},
		map[string]string{"src/app.go": numbered("package src // ", 100)})

	f, res, err := collectAndScore(t, dir)
	if err == nil {
		t.Fatalf("rules.Score = %+v, want the lowering setting refused", res)
	}
	for _, want := range []string{".risk/rules.json", f.Base, `refused "size_lines"`, "could lower a score"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("rules.Score error = %v, want it to contain %q", err, want)
		}
	}
	if !reflect.DeepEqual(res, score.Result{}) {
		t.Errorf("result = %+v, want none", res)
	}
}

// TestRulesExclusionToggles: a change adds 30 lines to code, 20 to a log and
// 15 to a TOML config. By default, and with both toggles true, the log and the
// config leave size and degree but count for spread; a toggle set to false
// counts its files for size and degree, which never lowers the total. Being
// non-code, they never count as untested. The report
// records each toggle's value with the rules hash.
func TestRulesExclusionToggles(t *testing.T) {
	all := []string{"config/app.toml", "run/app.log", "src/app.go"}
	tests := []struct {
		name          string
		rules         string // the target branch's .risk/rules.json; empty means none
		size          int
		from          []string
		excluded      []string
		nonCode, logs bool
	}{
		{"no rules file", "", 30, []string{"src/app.go"}, []string{"config/app.toml", "run/app.log"}, true, true},
		{"both true", `{"exclude_non_code_files":true,"exclude_log_files":true}`,
			30, []string{"src/app.go"}, []string{"config/app.toml", "run/app.log"}, true, true},
		{"log false", `{"exclude_log_files":false}`,
			50, []string{"run/app.log", "src/app.go"}, []string{"config/app.toml"}, true, false},
		{"non-code false", `{"exclude_non_code_files":false}`,
			45, []string{"config/app.toml", "src/app.go"}, []string{"run/app.log"}, false, true},
		{"both false", `{"exclude_non_code_files":false,"exclude_log_files":false}`, 65, all, nil, false, false},
	}
	noFileTotal := -1
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := map[string]string{
				"go.mod":          "module example.com/m\n",
				"src/app.go":      numbered("package src // ", 10),
				"run/app.log":     numbered("line ", 10),
				"config/app.toml": numbered("# ", 10),
			}
			file := rules.File{}
			if tt.rules != "" {
				base[".risk/rules.json"] = tt.rules + "\n"
				var err error
				if file, err = rules.Parse([]byte(tt.rules)); err != nil {
					t.Fatalf("Parse(%s): %v", tt.rules, err)
				}
			}
			dir := rulesFixture(t, base, map[string]string{
				"src/app.go":      numbered("package src // ", 40),
				"run/app.log":     numbered("line ", 30),
				"config/app.toml": numbered("# ", 25),
			})
			_, res, err := collectAndScore(t, dir)
			if err != nil {
				t.Fatalf("rules.Score: %v", err)
			}
			byFact := map[string]score.Point{}
			for _, p := range res.Breakdown {
				byFact[p.Measure+"/"+p.Fact] = p
			}
			size := byFact["size/lines"]
			if size.Value != tt.size || !reflect.DeepEqual(size.From, tt.from) || !reflect.DeepEqual(size.Excluded, tt.excluded) {
				t.Errorf("size = %d from %v excluding %v, want %d from %v excluding %v",
					size.Value, size.From, size.Excluded, tt.size, tt.from, tt.excluded)
			}
			if d := byFact["degree/existing_lines"]; d.Value != tt.size || !reflect.DeepEqual(d.From, tt.from) {
				t.Errorf("degree = %d from %v, want %d from %v", d.Value, d.From, tt.size, tt.from)
			}
			if u := byFact["tests/untested_production_files"]; !reflect.DeepEqual(u.From, []string{"src/app.go"}) {
				t.Errorf("untested = %v, want [src/app.go]", u.From)
			}
			if s := byFact["spread/files"]; !reflect.DeepEqual(s.From, all) {
				t.Errorf("spread files = %v, want %v", s.From, all)
			}
			pv := res.Provenance
			if pv.ExcludeNonCodeFiles != tt.nonCode || pv.ExcludeLogFiles != tt.logs {
				t.Errorf("provenance toggles = non_code %v, log %v; want %v, %v", pv.ExcludeNonCodeFiles, pv.ExcludeLogFiles, tt.nonCode, tt.logs)
			}
			if want := rules.Hash(rules.Apply(score.Default(), file)); pv.RulesHash != want {
				t.Errorf("rules hash = %s, want %s", pv.RulesHash, want)
			}
			if tt.rules == "" {
				noFileTotal = res.Total
			} else if res.Total < noFileTotal {
				t.Errorf("total = %d, want at least the no-file total %d", res.Total, noFileTotal)
			}
		})
	}
}

// TestRulesToggleValueRefused: the target branch's .risk/rules.json sets an
// exclusion toggle to a value other than true or false. The run fails naming
// the file, the commit and the toggle, and yields no result.
func TestRulesToggleValueRefused(t *testing.T) {
	dir := rulesFixture(t,
		map[string]string{".risk/rules.json": `{"exclude_log_files":"no"}` + "\n"},
		map[string]string{"src/app.go": numbered("package src // ", 100)})

	f, res, err := collectAndScore(t, dir)
	if err == nil {
		t.Fatalf("rules.Score = %+v, want the toggle value refused", res)
	}
	for _, want := range []string{".risk/rules.json", f.Base, `refused "exclude_log_files"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("rules.Score error = %v, want it to contain %q", err, want)
		}
	}
	if !reflect.DeepEqual(res, score.Result{}) {
		t.Errorf("result = %+v, want none", res)
	}
}

// TestReadmeDocumentsToggles: the README's Rules section names both
// exclusion toggles.
func TestReadmeDocumentsToggles(t *testing.T) {
	data, err := os.ReadFile("../README.md")
	if err != nil {
		t.Fatal(err)
	}
	_, section, ok := strings.Cut(string(data), "\n## Rules\n")
	if !ok {
		t.Fatal("README has no ## Rules section")
	}
	section, _, _ = strings.Cut(section, "\n## ")
	for _, key := range []string{"exclude_non_code_files", "exclude_log_files"} {
		if !strings.Contains(section, key) {
			t.Errorf("README Rules section does not name %s", key)
		}
	}
}
