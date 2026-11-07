package score

import (
	"reflect"
	"testing"

	"github.com/bobmcallan/riskelle/internal/facts"
)

// TestUntestedUnpairedLanguages pins the rule for Shell and Terraform files:
// with no test changed, one counts as untested only when the repo holds a test
// in its language; files with a pairing convention score as before; a file in
// no known language (YAML, JSON, XML, data) never counts.
func TestUntestedUnpairedLanguages(t *testing.T) {
	prod := func(p string) facts.File { return facts.File{Path: p, Added: 5} }
	test := func(p string) facts.File {
		return facts.File{Path: p, Added: 5, Classes: []facts.Class{facts.ClassTest}}
	}
	script := func(p string) facts.File { return facts.File{Path: p, Added: 5, Language: "Shell"} }
	tests := []struct {
		name       string
		files      []facts.File
		langs      []string
		wantValue  int
		wantPoints int
		wantFrom   []string
	}{
		// AC1: no shell test in the repo
		{"shell, no shell tests", []facts.File{prod("scripts/a.sh")}, []string{}, 0, 0, []string{}},
		{"bash, no shell tests", []facts.File{prod("scripts/a.bash")}, nil, 0, 0, []string{}},
		{"two shell scripts, no shell tests", []facts.File{prod("ops/a.sh"), prod("tools/b.sh")}, []string{}, 0, 0, []string{}},
		// AC2: the repo has shell tests
		{"shell, shell tests in repo", []facts.File{prod("scripts/a.sh")}, []string{"Shell"}, 1, 10, []string{"scripts/a.sh"}},
		{"shell, a test changed", []facts.File{prod("scripts/a.sh"), test("x_test.go")}, []string{"Shell"}, 0, 0, []string{}},
		// AC3: Terraform
		{"terraform, no terraform tests", []facts.File{prod("infra/main.tf")}, []string{}, 0, 0, []string{}},
		{"terraform, only shell tests", []facts.File{prod("infra/main.tf")}, []string{"Shell"}, 0, 0, []string{}},
		{"terraform, terraform tests in repo", []facts.File{prod("infra/main.tf")}, []string{"Terraform"}, 1, 10, []string{"infra/main.tf"}},
		{"tfvars, terraform tests in repo", []facts.File{prod("env/prod.tfvars")}, []string{"Terraform"}, 1, 10, []string{"env/prod.tfvars"}},
		// AC4: a pairing convention is unaffected by langs
		{"go, no langs", []facts.File{prod("main.go")}, []string{}, 1, 10, []string{"main.go"}},
		{"go, shell langs", []facts.File{prod("main.go")}, []string{"Shell"}, 1, 10, []string{"main.go"}},
		// a file in no known language never counts
		{"yaml, no langs", []facts.File{prod("deploy.yaml")}, []string{}, 0, 0, []string{}},
		{"shell and yaml", []facts.File{prod("a.sh"), prod("deploy.yaml")}, []string{}, 0, 0, []string{}},
		{"data and config, no test", []facts.File{prod("deploy.yaml"), prod("config/app.json"), prod("build/app.xml"), prod("release-roots")},
			[]string{"Shell", "Terraform"}, 0, 0, []string{}},
		// an extensionless script whose first line is a shell shebang is Shell
		{"shebang script, shell tests in repo", []facts.File{script("scripts/deploy")}, []string{"Shell"}, 1, 10, []string{"scripts/deploy"}},
		{"shebang script, no shell tests", []facts.File{script("scripts/deploy")}, []string{}, 0, 0, []string{}},
		{"shebang script, a test changed", []facts.File{script("scripts/deploy"), test("x_test.go")}, []string{"Shell"}, 0, 0, []string{}},
		{"go and yaml, no test", []facts.File{prod("main.go"), prod("deploy.yaml")}, []string{}, 1, 10, []string{"main.go"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := facts.Facts{Files: tt.files, TestLanguages: tt.langs}
			p := entry(t, Score(f, Default()), "tests", "untested_production_files")
			if p.Value != tt.wantValue || p.Points != tt.wantPoints || !reflect.DeepEqual(p.From, tt.wantFrom) {
				t.Errorf("tests = %+v, want value %d points %d from %v", p, tt.wantValue, tt.wantPoints, tt.wantFrom)
			}
		})
	}
}

// TestUntestedShellScriptTests scores script changes with classes taken from
// facts.Classify, so classification and scoring are proved together. An
// extensionless file (a script, a Makefile, a Dockerfile) names no language
// and never counts as untested; a .sh script keeps the Shell rule.
func TestUntestedShellScriptTests(t *testing.T) {
	file := func(p string, added int) facts.File {
		return facts.File{Path: p, Added: added, Classes: facts.Classify(p)}
	}
	tests := []struct {
		name      string
		files     []facts.File
		langs     []string
		wantValue int
		wantFrom  []string
	}{
		{"script with its test", []facts.File{file("scripts/plan-guard", 50), file("scripts/plan-guard-test", 30)}, []string{"Shell"}, 0, []string{}},
		{"script with its test, no langs", []facts.File{file("scripts/plan-guard", 50), file("scripts/plan-guard-test", 30)}, []string{}, 0, []string{}},
		{"extensionless script alone", []facts.File{file("scripts/plan-guard", 50)}, []string{"Shell"}, 0, []string{}},
		{".sh script alone", []facts.File{file("scripts/run.sh", 50)}, []string{"Shell"}, 1, []string{"scripts/run.sh"}},
		{"Makefile alone", []facts.File{file("Makefile", 5)}, []string{}, 0, []string{}},
		{"Dockerfile alone", []facts.File{file("Dockerfile", 5)}, []string{"Shell"}, 0, []string{}},
		{"Makefile with a script test", []facts.File{file("Makefile", 5), file("scripts/plan-guard-test", 3)}, []string{"Shell"}, 0, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := Score(facts.Facts{Files: tt.files, TestLanguages: tt.langs}, Default())
			p := entry(t, res, "tests", "untested_production_files")
			if p.Value != tt.wantValue || !reflect.DeepEqual(p.From, tt.wantFrom) {
				t.Errorf("tests = %+v, want %d from %v", p, tt.wantValue, tt.wantFrom)
			}
		})
	}
	res := Score(facts.Facts{Files: tests[0].files}, Default())
	if size := entry(t, res, "size", "lines"); size.Value != 50 {
		t.Errorf("size = %d, want 50: the script test's lines are not production", size.Value)
	}
}
