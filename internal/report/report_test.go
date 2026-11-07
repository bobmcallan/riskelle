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
	Version: "v0.11.0", RulesSource: ".risk/rules.json", ExcludeNonCodeFiles: true,
	RulesCommit: "0123456789abcdef0123456789abcdef01234567",
	RulesHash:   "sha256:00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff",
}

// estimateFixture is hand-built, not score.Score output, so recalibrating
// the bands does not churn the goldens. Fresh slices on every call.
func estimateFixture() Report {
	r := New(KindEstimate, score.Result{
		Total: 7, Level: score.Medium, // floored by identity_rights
		ShapeLevel: score.Low, FlooredBy: []string{"identity_rights"},
		Breakdown: []score.Point{
			{Measure: score.MeasureSize, Fact: "lines", Value: 101, Min: 51, Points: 5,
				From: []string{"core/calc.go", "ui/a&b<c>.js"}, Excluded: []string{"docs/guide.md"}},
			{Measure: "impact", Fact: "dependents", Value: 4, Min: 3, Points: 2,
				From:       []string{"example.com/m/core", "ui/a&b<c>.js"},
				Dependents: []facts.Dependency{{Name: "example.com/m/core", Dependents: 4}, {Name: "ui/a&b<c>.js", Dependents: 1}}},
		},
		Consequences: []score.Consequence{{
			Consequence: facts.Consequence{Class: "identity_rights", Paths: []string{"iam/roles.tf"}, Types: []string{}},
			Floor:       score.Medium,
		}},
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
	r := New(KindScore, score.Result{
		Total: 0, Level: score.Low, ShapeLevel: score.Low, FlooredBy: []string{},
		Breakdown: []score.Point{
			{Measure: score.MeasureTests, Fact: "untested_production_files", From: []string{}},
		},
		Provenance: score.Provenance{Version: "v0.11.0", RulesSource: "built-in",
			RulesCommit: provenance.RulesCommit, RulesHash: provenance.RulesHash},
	})
	r.Base = "1111111111111111111111111111111111111111"
	r.Head = "2222222222222222222222222222222222222222"
	return r
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
	for _, s := range []string{`"kind": "score"`, `"base": "` + strings.Repeat("1", 40) + `"`,
		`"head": "` + strings.Repeat("2", 40) + `"`, `"areas": []`, `"consequences": []`, `"note": "` + Note,
		`"shape_level": "Low"`, `"floored_by": []`} {
		if !bytes.Contains(none, []byte(s)) {
			t.Errorf("no-areas JSON lacks %s:\n%s", s, none)
		}
	}
	if bytes.Contains(none, []byte(`"plan"`)) {
		t.Errorf("score JSON names a plan:\n%s", none)
	}
	if est := render(t, "json", estimateFixture()); bytes.Contains(est, []byte(`"base"`)) || bytes.Contains(est, []byte(`"head"`)) {
		t.Errorf("estimate JSON names a base or head:\n%s", est)
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

// TestMarkdownListsConsequences pins the consequences section: shown with its
// note, one line per class, only when there is one.
func TestMarkdownListsConsequences(t *testing.T) {
	md := string(render(t, "markdown", estimateFixture()))
	for _, want := range []string{"### " + FloorHeading, FloorNote,
		"- identity_rights, floor Medium: paths `iam/roles.tf`; types none (planned)"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
	if none := string(render(t, "markdown", noAreasFixture())); strings.Contains(none, FloorHeading) {
		t.Errorf("markdown with no consequence has the section:\n%s", none)
	}
}

// TestReadmeDocumentsReportConsequences pins the README's Report section to
// the consequences key and the Markdown heading.
func TestReadmeDocumentsReportConsequences(t *testing.T) {
	data, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	i := strings.Index(s, "\n### Report\n")
	if i < 0 {
		t.Fatal("README has no ### Report section")
	}
	s = s[i+len("\n### Report\n"):]
	if j := strings.Index(s, "\n#"); j >= 0 {
		s = s[:j]
	}
	for _, want := range []string{"`\"consequences\"`", FloorHeading,
		"`\"shape_level\"`", "`\"floored_by\"`", "splitting the change will not lower it"} {
		if !strings.Contains(s, want) {
			t.Errorf("README Report section lacks %s", want)
		}
	}
}

// TestReportNamesProductionRoot pins that a floor raised by a production root
// names the root in JSON and Markdown, and that a plain floor names none.
func TestReportNamesProductionRoot(t *testing.T) {
	r := estimateFixture()
	r.Consequences[0].Floor = score.High
	r.Consequences[0].ProductionRoots = []string{"iam/**"}
	if md := string(render(t, "markdown", r)); !strings.Contains(md,
		"- identity_rights, floor High (production root `iam/**`): paths `iam/roles.tf`; types none (planned)") {
		t.Errorf("markdown does not name the root:\n%s", md)
	}
	if js := string(render(t, "json", r)); !strings.Contains(js, `"production_roots": [`) {
		t.Errorf("json does not name the root:\n%s", js)
	}
	if js := string(render(t, "json", estimateFixture())); strings.Contains(js, "production_roots") {
		t.Errorf("json names roots with none:\n%s", js)
	}
}

// TestMarkdownStatesFloor pins the floor statement: when a floor set the
// level, the Markdown states the shape level, the classes and that splitting
// will not lower it, before the breakdown table; otherwise it says nothing.
func TestMarkdownStatesFloor(t *testing.T) {
	want := "Shape level Low; the level is Medium, set by the consequence floor of identity_rights. " + SplitNote + "\n"
	md := string(render(t, "markdown", estimateFixture()))
	i := strings.Index(md, want)
	if i < 0 {
		t.Fatalf("markdown lacks the floor statement %q:\n%s", want, md)
	}
	if j := strings.Index(md, "| Measure |"); j < i {
		t.Errorf("floor statement comes after the table:\n%s", md)
	}
	two := estimateFixture()
	two.FlooredBy = []string{"a", "b"}
	if md := string(render(t, "markdown", two)); !strings.Contains(md, "set by the consequence floor of a, b. ") {
		t.Errorf("markdown does not list both classes:\n%s", md)
	}
	for name, r := range map[string]Report{"no areas": noAreasFixture(), "medium at its floor": func() Report {
		r := estimateFixture()
		r.ShapeLevel, r.FlooredBy = score.Medium, []string{}
		return r
	}()} {
		md := string(render(t, "markdown", r))
		if strings.Contains(md, "Shape level") || strings.Contains(md, SplitNote) {
			t.Errorf("%s: floor statement with no floor raise:\n%s", name, md)
		}
		if name == "medium at its floor" && !strings.Contains(md, FloorHeading) {
			t.Errorf("%s: consequences section missing:\n%s", name, md)
		}
	}
}
