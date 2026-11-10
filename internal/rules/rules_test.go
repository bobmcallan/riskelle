package rules

import (
	"encoding/json"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/bobmcallan/riskelle/internal/facts"
	"github.com/bobmcallan/riskelle/internal/score"
)

func ptr(b bool) *bool { return &b }

func TestParse(t *testing.T) {
	tests := []struct {
		in      string
		want    File
		wantErr string // substring; empty means no error
	}{
		{`{"generated":["gen/**","**/*.g.ts"]}`, File{Generated: []string{"gen/**", "**/*.g.ts"}}, ""},
		{"{}\n\n", File{}, ""},
		{`{"size_lines":[]}`, File{}, `refused "size_lines": .risk/rules.json may only add risk or context`},
		{`{"generated":["gen/**"],"levels":[],"size_lines":[]}`, File{}, `refused "levels", "size_lines": `},
		{`{"Size_Lines":[]}`, File{}, `refused "Size_Lines": `},
		{`{"consequence_floors":[]}`, File{}, `refused "consequence_floors": .risk/rules.json may only add risk or context`},
		{`{"Consequence_Floors":[{"class":"identity_rights","level":"Low"}]}`, File{}, `refused "Consequence_Floors": `},
		{`{"levels":[],"generated":[""]}`, File{}, `refused "levels": `},
		{`{"generatd":[]}`, File{}, `unknown field "generatd"`},
		{`{"generated":["a/**",""]}`, File{}, "generated[1]: empty pattern"},
		{`{"generated":["/x"]}`, File{}, `generated[0] "/x": patterns are repo-relative`},
		{`{} {}`, File{}, "trailing data"},
		{`{"generated":`, File{}, "unexpected EOF"},
		{`{"areas":{"payments":["billing/**"],"auth":["src/login/**"]}}`,
			File{Areas: map[string][]string{"payments": {"billing/**"}, "auth": {"src/login/**"}}}, ""},
		{`{"areas":{"Pay":["x"]}}`, File{}, `areas: invalid name "Pay"`},
		{`{"areas":{"pay":[]}}`, File{}, "areas.pay: no patterns"},
		{`{"areas":{"pay":["ok/**",""]}}`, File{}, "areas.pay[1]: empty pattern"},
		{`{"areas":{"pay":["/x"]}}`, File{}, `areas.pay[0] "/x": patterns are repo-relative`},
		{`{"production_roots":["prod-tenant/**"]}`, File{ProductionRoots: []string{"prod-tenant/**"}}, ""},
		{`{"production_roots":[]}`, File{ProductionRoots: []string{}}, ""},
		{`{"production_roots":["a/**",""]}`, File{}, "production_roots[1]: empty pattern"},
		{`{"production_roots":["/sub/**"]}`, File{}, `production_roots[0] "/sub/**": patterns are repo-relative`},
		{`{"production_roots":["prod-tenant"]}`, File{}, `production_roots[0] "prod-tenant": a production root names a directory and ends in /**`},
		{`{"production_roots":["prod-tenant/"]}`, File{}, `production_roots[0] "prod-tenant/": a production root names a directory`},
		{`{"production_roots":"prod-tenant/**"}`, File{}, "production_roots"},
		{`{"areas":["x"]}`, File{}, "cannot unmarshal"},
		{`{"production_environments":["live","PRD-AU"]}`, File{ProductionEnvironments: []string{"live", "PRD-AU"}}, ""},
		{`{"production_environments":[]}`, File{ProductionEnvironments: []string{}}, ""},
		{`{"Production_Environments":["live"]}`, File{ProductionEnvironments: []string{"live"}}, ""},
		{`{"production_environments":["live",""]}`, File{}, "production_environments[1]: empty name"},
		{`{"production_environments":[" "]}`, File{}, "production_environments[0]: empty name"},
		{`{"production_environments":[null]}`, File{}, "production_environments[0]: empty name"},
		{`{"consequence_classes":{"release_guard":{"paths":["scripts/plan-guard-allow*"],"floor":"Medium"}}}`,
			File{ConsequenceClasses: map[string]ConsequenceClass{"release_guard": {Paths: []string{"scripts/plan-guard-allow*"}, Floor: "Medium"}}}, ""},
		{`{"consequence_classes":{"release_guard":{"paths":["x/**"],"floor":"High"}}}`,
			File{ConsequenceClasses: map[string]ConsequenceClass{"release_guard": {Paths: []string{"x/**"}, Floor: "High"}}}, ""},
		{`{"consequence_classes":{"identity_rights":{"paths":["x"],"floor":"Medium"}}}`, File{},
			`consequence_classes: refused "identity_rights": a built-in class; .risk/rules.json may not redefine or lower it`},
		{`{"consequence_classes":{"pipeline_identity":{"paths":["x"],"floor":"High"}}}`, File{}, `consequence_classes: refused "pipeline_identity"`},
		{`{"consequence_classes":{"g":{"paths":["x"],"floor":"Low"}}}`, File{},
			`consequence_classes.g: floor "Low" refused: a repo class may only raise the level, to Medium or High`},
		{`{"consequence_classes":{"g":{"paths":["x"],"floor":"Critical"}}}`, File{}, `consequence_classes.g: unknown floor "Critical" (want Medium or High)`},
		{`{"consequence_classes":{"g":{"paths":["x"],"floor":"medium"}}}`, File{}, `consequence_classes.g: unknown floor "medium"`},
		{`{"consequence_classes":{"g":{"paths":["x"]}}}`, File{}, `consequence_classes.g: unknown floor ""`},
		{`{"consequence_classes":{"g":{"paths":[],"floor":"High"}}}`, File{}, "consequence_classes.g: no patterns"},
		{`{"consequence_classes":{"g":{"paths":[""],"floor":"High"}}}`, File{}, "consequence_classes.g.paths[0]: empty pattern"},
		{`{"consequence_classes":{"g":{"paths":["/x"],"floor":"High"}}}`, File{}, `consequence_classes.g.paths[0] "/x": patterns are repo-relative`},
		{`{"consequence_classes":{"Guard":{"paths":["x"],"floor":"High"}}}`, File{}, `consequence_classes: invalid name "Guard"`},
		{`{"consequence_classes":{"g":{"paths":["x"],"floor":"High","level":"Low"}}}`, File{}, `unknown field "level"`},
		{`{"exclude_log_files":false}`, File{ExcludeLogFiles: ptr(false)}, ""},
		{`{"exclude_log_files":true,"exclude_non_code_files":false}`,
			File{ExcludeLogFiles: ptr(true), ExcludeNonCodeFiles: ptr(false)}, ""},
		{`{"exclude_log_files":"no"}`, File{}, `refused "exclude_log_files" = "no": .risk/rules.json may set an exclusion toggle only to false`},
		{`{"exclude_non_code_files":null}`, File{}, `refused "exclude_non_code_files" = null`},
		{`{"exclude_log_files":0}`, File{}, `refused "exclude_log_files" = 0`},
		{`{"exclude_log_files":"false"}`, File{}, `refused "exclude_log_files" = "false"`},
		{`{"Exclude_Non_Code_Files":1,"exclude_log_files":[]}`, File{}, `refused "Exclude_Non_Code_Files" = 1, "exclude_log_files" = []`},
		{`{"size_lines":[],"exclude_log_files":"x"}`, File{}, `refused "size_lines": `},
	}
	for _, tt := range tests {
		got, err := Parse([]byte(tt.in))
		if tt.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Parse(%s) error = %v, want it to contain %q", tt.in, err, tt.wantErr)
			}
		} else if err != nil || !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Parse(%s) = %+v, %v; want %+v", tt.in, got, err, tt.want)
		}
	}
}

// TestRefusedCoversEveryBuiltinSetting: every built-in setting except the
// additive generated paths and production environment names is refused by
// name. The keys come from the
// built-in rules' JSON, not from the reflection that builds the refused set.
func TestRefusedCoversEveryBuiltinSetting(t *testing.T) {
	data, err := json.Marshal(score.Default())
	if err != nil {
		t.Fatal(err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(data, &keys); err != nil {
		t.Fatal(err)
	}
	for key := range keys {
		_, err := Parse([]byte(`{"` + key + `":null}`))
		if key == "generated" || key == "production_environments" {
			if err != nil {
				t.Errorf("Parse(%s) error = %v, want it accepted", key, err)
			}
			continue
		}
		if want := `refused "` + key + `"`; err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Parse(%s) error = %v, want it to contain %q", key, err, want)
		}
	}
}

// TestAdditiveKeysNotRefused: every key File holds is an addition, never in
// the refused set, and the refused set is not empty.
func TestAdditiveKeysNotRefused(t *testing.T) {
	if len(refused) == 0 {
		t.Fatal("refused is empty, want the built-in settings")
	}
	for _, k := range jsonKeys(reflect.TypeFor[File]()) {
		if refused[strings.ToLower(k)] {
			t.Errorf("additive key %q is refused", k)
		}
	}
	if got, err := Parse([]byte(`{"generated":["gen/**"]}`)); err != nil || !reflect.DeepEqual(got, File{Generated: []string{"gen/**"}}) {
		t.Errorf("Parse(generated) = %+v, %v; want it accepted", got, err)
	}
}

func TestApply(t *testing.T) {
	want := score.Default()
	want.SizeExcludes = append(want.SizeExcludes, facts.ClassNonCode, facts.ClassRunRecord)
	if got := Apply(score.Default(), File{}); !reflect.DeepEqual(got, want) {
		t.Errorf("Apply with an empty file = %+v, want the built-in rules with the default toggles", got)
	}
	base := score.Default()
	base.Generated = []string{"z/**", "a/**"}
	got := Apply(base, File{Generated: []string{"m/**", "a/**"}})
	if want := []string{"a/**", "m/**", "z/**"}; !reflect.DeepEqual(got.Generated, want) {
		t.Errorf("Apply Generated = %v, want %v", got.Generated, want)
	}
	if want := []string{"z/**", "a/**"}; !reflect.DeepEqual(base.Generated, want) {
		t.Errorf("Apply changed its input: %v, want %v", base.Generated, want)
	}

	base = score.Default()
	base.Areas = map[string][]string{"auth": {"z/**"}}
	got = Apply(base, File{Areas: map[string][]string{"auth": {"z/**", "a/**"}, "pay": {"b/**"}}})
	if want := map[string][]string{"auth": {"a/**", "z/**"}, "pay": {"b/**"}}; !reflect.DeepEqual(got.Areas, want) {
		t.Errorf("Apply Areas = %v, want %v", got.Areas, want)
	}
	if want := map[string][]string{"auth": {"z/**"}}; !reflect.DeepEqual(base.Areas, want) {
		t.Errorf("Apply changed its input: %v, want %v", base.Areas, want)
	}
	if got := Apply(score.Default(), File{Areas: map[string][]string{}}); got.Areas != nil {
		t.Errorf("Apply with no areas = %v, want nil", got.Areas)
	}
}

func TestHash(t *testing.T) {
	def := Hash(score.Default())
	if !regexp.MustCompile(`^sha256:[0-9a-f]{64}$`).MatchString(def) || def != Hash(score.Default()) {
		t.Errorf("Hash = %q, want a stable sha256:<64 hex>", def)
	}
	// The same additions in another order are the same effective rules.
	a := Apply(score.Default(), File{Generated: []string{"a/**", "b/**"}})
	b := Apply(score.Default(), File{Generated: []string{"b/**", "a/**", "a/**"}})
	if Hash(a) != Hash(b) || Hash(a) == def {
		t.Errorf("Hash(a) = %s, Hash(b) = %s, default %s: want a == b != default", Hash(a), Hash(b), def)
	}
	// Added areas are part of the effective rules, in any order.
	c := Apply(score.Default(), File{Areas: map[string][]string{"pay": {"a/**", "b/**"}, "auth": {"x/**"}}})
	d := Apply(score.Default(), File{Areas: map[string][]string{"auth": {"x/**"}, "pay": {"b/**", "a/**"}}})
	if Hash(c) != Hash(d) || Hash(c) == def {
		t.Errorf("Hash(c) = %s, Hash(d) = %s, default %s: want c == d != default", Hash(c), Hash(d), def)
	}
}

// TestApplyToggles: an absent or true toggle leaves its class out of size,
// degree and untested; false counts it, removing the class even when the base
// rules already hold it, and changes the rules hash.
func TestApplyToggles(t *testing.T) {
	const (
		docs, gen, lock, test = facts.ClassDocs, facts.ClassGenerated, facts.ClassLock, facts.ClassTest
		nonCode, runRecord    = facts.ClassNonCode, facts.ClassRunRecord
	)
	def := Apply(score.Default(), File{})
	if want := []facts.Class{docs, gen, lock, test, nonCode, runRecord}; !reflect.DeepEqual(def.SizeExcludes, want) {
		t.Errorf("default SizeExcludes = %v, want %v", def.SizeExcludes, want)
	}
	if got := Apply(score.Default(), File{ExcludeNonCodeFiles: ptr(true), ExcludeLogFiles: ptr(true)}); !reflect.DeepEqual(got, def) {
		t.Errorf("Apply with both toggles true = %+v, want the default %+v", got, def)
	}
	if got := Apply(def, File{}); !reflect.DeepEqual(got.SizeExcludes, def.SizeExcludes) {
		t.Errorf("Apply twice = %v, want no duplicate class", got.SizeExcludes)
	}
	if want := []facts.Class{docs, gen, lock, test}; !reflect.DeepEqual(score.Default().SizeExcludes, want) {
		t.Errorf("score.Default().SizeExcludes = %v, want it unchanged %v", score.Default().SizeExcludes, want)
	}

	for _, tt := range []struct {
		name string
		f    File
		want []facts.Class
	}{
		{"log false", File{ExcludeLogFiles: ptr(false)}, []facts.Class{docs, gen, lock, test, nonCode}},
		{"non-code false", File{ExcludeNonCodeFiles: ptr(false)}, []facts.Class{docs, gen, lock, test, runRecord}},
		{"both false", File{ExcludeNonCodeFiles: ptr(false), ExcludeLogFiles: ptr(false)}, []facts.Class{docs, gen, lock, test}},
	} {
		got := Apply(score.Default(), tt.f)
		if !reflect.DeepEqual(got.SizeExcludes, tt.want) {
			t.Errorf("%s: SizeExcludes = %v, want %v", tt.name, got.SizeExcludes, tt.want)
		}
		if Hash(got) == Hash(def) {
			t.Errorf("%s: hash equals the default's, want it to differ", tt.name)
		}
	}

	base := score.Default()
	base.SizeExcludes = append(base.SizeExcludes, nonCode)
	got := Apply(base, File{ExcludeNonCodeFiles: ptr(false)})
	if want := []facts.Class{docs, gen, lock, test, runRecord}; !reflect.DeepEqual(got.SizeExcludes, want) {
		t.Errorf("false on a base holding non_code: SizeExcludes = %v, want %v", got.SizeExcludes, want)
	}
	if want := []facts.Class{docs, gen, lock, test, nonCode}; !reflect.DeepEqual(base.SizeExcludes, want) {
		t.Errorf("Apply changed its input: %v, want %v", base.SizeExcludes, want)
	}
}

// TestHashRecordsFloors pins that the rules hash records the consequence
// floors: lowering one changes the hash.
func TestHashRecordsFloors(t *testing.T) {
	d := score.Default()
	d.ConsequenceFloors[0].Level = score.Low
	if Hash(d) == Hash(score.Default()) {
		t.Error("lowering a floor did not change the rules hash")
	}
	if Hash(score.Default()) != Hash(score.Default()) {
		t.Error("the built-in rules hash is not stable")
	}
}

// TestApplyProductionRoots pins that roots merge as a sorted, de-duplicated
// union, nil when empty, without changing the input.
func TestApplyProductionRoots(t *testing.T) {
	base := score.Default()
	base.ProductionRoots = []string{"b/**", "a/**"}
	got := Apply(base, File{ProductionRoots: []string{"c/**", "a/**"}})
	if want := []string{"a/**", "b/**", "c/**"}; !slices.Equal(got.ProductionRoots, want) {
		t.Errorf("roots = %v, want %v", got.ProductionRoots, want)
	}
	if !slices.Equal(base.ProductionRoots, []string{"b/**", "a/**"}) {
		t.Errorf("input changed: %v", base.ProductionRoots)
	}
	if got := Apply(score.Default(), File{ProductionRoots: []string{}}); got.ProductionRoots != nil {
		t.Errorf("empty roots = %#v, want nil", got.ProductionRoots)
	}
}

// TestHashRecordsProductionRoots pins that roots are hashed, in any order the
// same, and that without roots the built-in hash is unchanged.
func TestHashRecordsProductionRoots(t *testing.T) {
	one := Apply(score.Default(), File{ProductionRoots: []string{"a/**", "b/**"}})
	two := Apply(score.Default(), File{ProductionRoots: []string{"b/**", "a/**"}})
	if Hash(one) != Hash(two) {
		t.Error("roots in another order hash differently")
	}
	if Hash(one) == Hash(score.Default()) {
		t.Error("roots do not change the hash")
	}
	for _, r := range []score.Rules{score.Default(), Apply(score.Default(), File{})} {
		if b, _ := json.Marshal(r); strings.Contains(string(b), "production_roots") {
			t.Errorf("rules without roots marshal a production_roots key: %s", b)
		}
	}
}

// TestReadmeDocumentsProductionRoots pins the README's production roots,
// repo consequence classes and production environments wording in Rules,
// Consequences, Report and Plan file.
func TestReadmeDocumentsProductionRoots(t *testing.T) {
	data, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	section := func(heading string) string {
		s := string(data)
		i := strings.Index(s, "\n"+heading+"\n")
		if i < 0 {
			t.Fatalf("README has no %q section", heading)
		}
		s = s[i+len(heading)+2:]
		depth := strings.Index(heading, " ")
		for j := 0; j < len(s); j++ {
			if (j == 0 || s[j-1] == '\n') && strings.HasPrefix(s[j:], "#") {
				if len(s[j:])-len(strings.TrimLeft(s[j:], "#")) <= depth {
					return s[:j]
				}
			}
		}
		return s
	}
	for heading, wants := range map[string][]string{
		"## Rules":         {"`production_roots`", "/**", "`consequence_classes`", `{"production_environments": ["live"]}`},
		"### Consequences": {"`production_roots`", "High", "`consequence_classes`", "Medium or High", "`production_environments`"},
		"### Report":       {`"production_roots"`},
		"### Plan file":    {"repo class"},
	} {
		sec := section(heading)
		for _, w := range wants {
			if !strings.Contains(sec, w) {
				t.Errorf("README %s lacks %s", heading, w)
			}
		}
	}
}

func TestApplyRepoClasses(t *testing.T) {
	f := File{ConsequenceClasses: map[string]ConsequenceClass{
		"release_roots": {Paths: []string{"release-roots"}, Floor: "Medium"},
		"guard":         {Paths: []string{"b/**", "a/**", "b/**"}, Floor: "High"},
	}}
	got := Apply(score.Default(), f)
	want := []score.RepoClass{
		{Class: "guard", Paths: []string{"a/**", "b/**"}, Floor: score.High},
		{Class: "release_roots", Paths: []string{"release-roots"}, Floor: score.Medium},
	}
	if !reflect.DeepEqual(got.RepoClasses, want) {
		t.Errorf("RepoClasses = %+v, want %+v", got.RepoClasses, want)
	}
	if f.ConsequenceClasses["guard"].Paths[0] != "b/**" {
		t.Error("Apply changed its input")
	}
	if Apply(score.Default(), File{}).RepoClasses != nil {
		t.Error("RepoClasses not nil with none")
	}
}

func TestHashRecordsRepoClasses(t *testing.T) {
	cls := func(floor string, paths ...string) File {
		return File{ConsequenceClasses: map[string]ConsequenceClass{"guard": {Paths: paths, Floor: floor}}}
	}
	one, two := Apply(score.Default(), cls("Medium", "a/**", "b/**")), Apply(score.Default(), cls("Medium", "b/**", "a/**"))
	if Hash(one) != Hash(two) {
		t.Error("globs in another order hash differently")
	}
	if Hash(one) == Hash(score.Default()) {
		t.Error("a repo class does not change the hash")
	}
	if Hash(one) == Hash(Apply(score.Default(), cls("High", "a/**", "b/**"))) {
		t.Error("a repo class's floor does not change the hash")
	}
	for _, r := range []score.Rules{score.Default(), Apply(score.Default(), File{})} {
		if b, _ := json.Marshal(r); strings.Contains(string(b), "consequence_classes") {
			t.Errorf("rules without classes marshal a consequence_classes key: %s", b)
		}
	}
}

// TestParseProductionEnvironmentTypes: an entry that is not a string, or a
// value that is not a list, is refused by the strict decode naming the key.
func TestParseProductionEnvironmentTypes(t *testing.T) {
	for _, v := range []string{`[5]`, `[true]`, `[["live"]]`, `"live"`, `{"a":"live"}`} {
		in := `{"production_environments":` + v + `}`
		_, err := Parse([]byte(in))
		if err == nil || !strings.Contains(err.Error(), "production_environments") || !strings.Contains(err.Error(), "cannot unmarshal") {
			t.Errorf("Parse(%s) error = %v, want it to name production_environments and cannot unmarshal", in, err)
		}
	}
}

// TestApplyProductionEnvironments: a file's names are added to the built-in
// ones trimmed and lower-cased, as a sorted, de-duplicated union; nothing is
// replaced or removed, and the input is not changed.
func TestApplyProductionEnvironments(t *testing.T) {
	def := score.Default().ProductionEnvironments
	withExtra := score.Default()
	withExtra.ProductionEnvironments = append(withExtra.ProductionEnvironments, "staging-prod")
	for _, tt := range []struct {
		name string
		base score.Rules
		f    File
		want []string
	}{
		{"added", score.Default(), File{ProductionEnvironments: []string{"Live", "uat-prod", "PROD", " live "}},
			[]string{"live", "prd", "prod", "production", "uat-prod"}},
		{"no file", score.Default(), File{}, def},
		{"empty list", score.Default(), File{ProductionEnvironments: []string{}}, def},
		{"built-in only", score.Default(), File{ProductionEnvironments: []string{"PRD", "production"}}, def},
		{"base extra kept", withExtra, File{ProductionEnvironments: []string{"live"}},
			[]string{"live", "prd", "prod", "production", "staging-prod"}},
	} {
		got := Apply(tt.base, tt.f)
		if !slices.Equal(got.ProductionEnvironments, tt.want) {
			t.Errorf("%s: ProductionEnvironments = %q, want %q", tt.name, got.ProductionEnvironments, tt.want)
		}
	}
	f := File{ProductionEnvironments: []string{"Live"}}
	base := score.Default()
	Apply(base, f)
	if f.ProductionEnvironments[0] != "Live" || !slices.Equal(base.ProductionEnvironments, def) {
		t.Errorf("Apply changed its input: file %q, base %q", f.ProductionEnvironments, base.ProductionEnvironments)
	}
}

// TestHashRecordsProductionEnvironments: an added name changes the rules
// hash; the same names in another order or case hash the same, and a name
// already built in hashes as no file.
func TestHashRecordsProductionEnvironments(t *testing.T) {
	none := Hash(Apply(score.Default(), File{}))
	if none != Hash(Apply(score.Default(), File{ProductionEnvironments: []string{}})) {
		t.Error("an empty list changes the hash")
	}
	one := Hash(Apply(score.Default(), File{ProductionEnvironments: []string{"live", "uat"}}))
	two := Hash(Apply(score.Default(), File{ProductionEnvironments: []string{"UAT", " Live"}}))
	if one == none {
		t.Error("an added name does not change the hash")
	}
	if one != two {
		t.Error("the same names in another order or case hash differently")
	}
	if Hash(Apply(score.Default(), File{ProductionEnvironments: []string{"PROD"}})) != none {
		t.Error("a built-in name changes the hash")
	}
}
