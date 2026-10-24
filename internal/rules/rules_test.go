package rules

import (
	"encoding/json"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/bobmcallan/riskelle/internal/score"
)

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
	if got := Apply(score.Default(), File{}); !reflect.DeepEqual(got, score.Default()) {
		t.Errorf("Apply with an empty file = %+v, want the built-in rules", got)
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
}
