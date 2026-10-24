// Package report renders a scored change for people and pipelines: JSON for
// machines, Markdown for a PR comment. It formats a score.Result and never
// changes it. Both formats come from one Report, so they cannot disagree.
package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/bobmcallan/riskelle/internal/score"
)

// Kinds of scored change.
const (
	KindScore    = "score"    // a real diff, base..head
	KindEstimate = "estimate" // facts predicted from a plan file
)

// Formats are the output formats, the default first.
var Formats = []string{"json", "markdown"}

// Note labels where a change lands, in every format.
const Note = "Where the change lands is context for reviewers; it does not count towards the score."

// Report is what riskelle prints for one scored change.
type Report struct {
	Kind       string           `json:"kind"`           // KindScore or KindEstimate
	Plan       string           `json:"plan,omitempty"` // estimate only: the plan file as given
	Base       string           `json:"base,omitempty"` // score only: full SHA the base ref resolved to
	Head       string           `json:"head,omitempty"` // score only: full SHA the head ref resolved to
	Total      int              `json:"total"`
	Level      score.Level      `json:"level"`
	Breakdown  []score.Point    `json:"breakdown"`
	Context    Context          `json:"context"`
	Provenance score.Provenance `json:"provenance"`
}

// Context is reviewer context: never part of the total or the breakdown.
type Context struct {
	Note  string       `json:"note"`  // always Note
	Areas []score.Area `json:"areas"` // sorted by name, as score gives them; [] when none
}

// New builds the report of a scored result, moving its areas into Context.
func New(kind string, res score.Result) Report {
	areas := res.Areas
	if areas == nil {
		areas = []score.Area{}
	}
	breakdown := res.Breakdown
	if breakdown == nil {
		breakdown = []score.Point{}
	}
	return Report{
		Kind: kind, Total: res.Total, Level: res.Level, Breakdown: breakdown,
		Context: Context{Note: Note, Areas: areas}, Provenance: res.Provenance,
	}
}

// Write renders r in format, one of Formats.
func Write(w io.Writer, format string, r Report) error {
	switch format {
	case "json":
		return r.JSON(w)
	case "markdown":
		return r.Markdown(w)
	}
	return fmt.Errorf("unknown format %q (want %s)", format, strings.Join(Formats, " or "))
}

// JSON writes r as indented JSON with one trailing newline. Report holds no
// maps and every slice arrives sorted, so the same report always gives the
// same bytes.
func (r Report) JSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(r)
}

// Markdown writes r as a summary for a PR comment.
func (r Report) Markdown(w io.Writer) error {
	var b bytes.Buffer
	fmt.Fprintf(&b, "## riskelle %s: %d / 100, %s\n\n", r.Kind, r.Total, r.Level)
	if r.Plan != "" {
		fmt.Fprintf(&b, "Estimate from plan %s, not a measured diff.\n\n", code(r.Plan))
	}
	if r.Base != "" {
		fmt.Fprintf(&b, "Diff of head %s against base %s, from their merge base.\n\n", code(r.Head), code(r.Base))
	}

	b.WriteString("| Measure | Fact | Value | Band from | Points |\n| --- | --- | ---: | ---: | ---: |\n")
	for _, p := range r.Breakdown {
		fmt.Fprintf(&b, "| %s | %s | %d | %d | %d |\n", p.Measure, p.Fact, p.Value, p.Min, p.Points)
	}
	b.WriteString("\n<details><summary>Facts behind each point</summary>\n\n")
	for _, p := range r.Breakdown {
		fmt.Fprintf(&b, "- %s / %s: %s", p.Measure, p.Fact, counted(p))
		if len(p.Excluded) > 0 {
			fmt.Fprintf(&b, "; excluded: %s", codes(p.Excluded))
		}
		b.WriteString("\n")
	}
	b.WriteString("\n</details>\n\n")

	b.WriteString("### Where it lands (context, not scored)\n\n" + Note + "\n\n")
	if len(r.Context.Areas) == 0 {
		b.WriteString("None: the change lands in no sensitive area.\n")
	}
	for _, a := range r.Context.Areas {
		fmt.Fprintf(&b, "- %s: %s\n", a.Name, codes(a.Paths))
	}

	pv := r.Provenance
	fmt.Fprintf(&b, "\n### Provenance\n\n- riskelle %s\n- rules %s at %s\n- rules hash %s\n",
		code(pv.Version), code(pv.RulesSource), code(pv.RulesCommit), code(pv.RulesHash))
	_, err := w.Write(b.Bytes())
	return err
}

// counted lists what one point counted: its dependents with their counts when
// it has them, otherwise its From, or "none".
func counted(p score.Point) string {
	if len(p.Dependents) > 0 {
		items := make([]string, len(p.Dependents))
		for i, d := range p.Dependents {
			items[i] = code(d.Name) + " (" + strconv.Itoa(d.Dependents) + ")"
		}
		return strings.Join(items, ", ")
	}
	if len(p.From) == 0 {
		return "none"
	}
	return codes(p.From)
}

func codes(items []string) string {
	out := make([]string, len(items))
	for i, s := range items {
		out[i] = code(s)
	}
	return strings.Join(out, ", ")
}

// code renders s as a Markdown code span that nothing in s can break: the
// fence is one backtick longer than the longest run of backticks in s, padded
// with spaces when s starts or ends with a backtick (CommonMark).
func code(s string) string {
	longest, run := 0, 0
	for _, c := range s {
		if c == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	fence := strings.Repeat("`", longest+1)
	if strings.HasPrefix(s, "`") || strings.HasSuffix(s, "`") {
		s = " " + s + " "
	}
	return fence + s + fence
}
