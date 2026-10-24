package report

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/bobmcallan/riskelle/internal/facts"
	"github.com/bobmcallan/riskelle/internal/score"
)

var update = flag.Bool("update", false, "rewrite the golden files")

// golden compares got with testdata/name byte for byte; -update rewrites it.
func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s differs from the golden:\n%s\nwant:\n%s", name, got, want)
	}
}

var provenance = score.Provenance{
	Version: "v0.11.0", RulesSource: ".risk/rules.json",
	RulesCommit: "0123456789abcdef0123456789abcdef01234567",
	RulesHash:   "sha256:00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff",
}

// estimateFixture is hand-built, not score.Score output, so recalibrating
// the bands does not churn the goldens. Fresh slices on every call.
func estimateFixture() Report {
	r := New(KindEstimate, score.Result{
		Total: 7, Level: score.Low,
		Breakdown: []score.Point{
			{Measure: score.MeasureSize, Fact: "lines", Value: 101, Min: 51, Points: 5,
				From: []string{"core/calc.go", "ui/a&b<c>.js"}, Excluded: []string{"docs/guide.md"}},
			{Measure: "impact", Fact: "dependents", Value: 4, Min: 3, Points: 2,
				From:       []string{"example.com/m/core", "ui/a&b<c>.js"},
				Dependents: []facts.Dependency{{Name: "example.com/m/core", Dependents: 4}, {Name: "ui/a&b<c>.js", Dependents: 1}}},
		},
		Areas: []score.Area{
			{Name: "auth", Paths: []string{"internal/auth/login.go"}},
			{Name: "payments", Paths: []string{"pay/card.go", "pay/refund.go"}},
		},
		Provenance: provenance,
	})
	r.Plan = "plan.json"
	return r
}

func noAreasFixture() Report {
	return New(KindScore, score.Result{
		Total: 0, Level: score.Low,
		Breakdown: []score.Point{
			{Measure: score.MeasureTests, Fact: "untested_production_files", From: []string{}},
		},
		Provenance: score.Provenance{Version: "v0.11.0", RulesSource: "built-in",
			RulesCommit: provenance.RulesCommit, RulesHash: provenance.RulesHash},
	})
}

func render(t *testing.T, format string, r Report) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := Write(&buf, format, r); err != nil {
		t.Fatalf("Write %s: %v", format, err)
	}
	return buf.Bytes()
}

func TestGolden(t *testing.T) {
	for _, tt := range []struct {
		fixture func() Report
		format  string
		golden  string
	}{
		{estimateFixture, "json", "estimate.json"},
		{estimateFixture, "markdown", "estimate.md"},
		{noAreasFixture, "markdown", "no-areas.md"},
	} {
		t.Run(tt.golden, func(t *testing.T) { golden(t, tt.golden, render(t, tt.format, tt.fixture())) })
	}
}

// TestJSONStable: two independently built equal reports give the same bytes.
func TestJSONStable(t *testing.T) {
	a, b := render(t, "json", estimateFixture()), render(t, "json", estimateFixture())
	if !bytes.Equal(a, b) {
		t.Errorf("same report, different bytes:\n%s\n%s", a, b)
	}
}

// TestReportCarriesResult: the JSON decodes back to the report, so no field
// is dropped, and the areas sit under context with the note, never at the top.
func TestReportCarriesResult(t *testing.T) {
	want := estimateFixture()
	out := render(t, "json", want)
	var got Report
	dec := json.NewDecoder(bytes.NewReader(out))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("decoded %+v, want %+v", got, want)
	}
	var top map[string]json.RawMessage
	json.Unmarshal(out, &top)
	if _, ok := top["areas"]; ok {
		t.Error("areas at the top level, want them only under context")
	}
	if got.Context.Note != Note {
		t.Errorf("context note = %q, want %q", got.Context.Note, Note)
	}
	none := render(t, "json", noAreasFixture())
	for _, s := range []string{`"kind": "score"`, `"areas": []`, `"note": "` + Note} {
		if !bytes.Contains(none, []byte(s)) {
			t.Errorf("no-areas JSON lacks %s:\n%s", s, none)
		}
	}
	if bytes.Contains(none, []byte(`"plan"`)) {
		t.Errorf("score JSON names a plan:\n%s", none)
	}
}

func TestWriteUnknownFormat(t *testing.T) {
	err := Write(&bytes.Buffer{}, "xml", estimateFixture())
	if err == nil || !strings.Contains(err.Error(), `"xml"`) {
		t.Errorf("Write xml: err = %v, want one naming \"xml\"", err)
	}
}

func TestCode(t *testing.T) {
	for in, want := range map[string]string{
		"a/b.go":   "`a/b.go`",
		"a`b.go":   "``a`b.go``",
		"`a.go":    "`` `a.go ``",
		"a``b`.go": "```a``b`.go```",
	} {
		if got := code(in); got != want {
			t.Errorf("code(%q) = %q, want %q", in, got, want)
		}
	}
}
