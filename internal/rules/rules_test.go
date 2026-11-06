package rules

import (
	"encoding/json"
	"reflect"
	"regexp"
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
		{`{"areas":["x"]}`, File{}, "cannot unmarshal"},
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
// additive generated paths is refused by name. The keys come from the
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
		if key == "generated" {
			if err != nil {
				t.Errorf("Parse(generated) error = %v, want it accepted", err)
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
