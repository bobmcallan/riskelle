// Package rules loads the effective scoring rules for a change: the built-in
// rules plus what the repository adds in .risk/rules.json, read at the target
// branch commit so a change cannot alter the rules that score it. It records
// where the rules came from, so every result can be reproduced.
package rules

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/bobmcallan/riskelle/internal/facts"
	"github.com/bobmcallan/riskelle/internal/score"
	"github.com/bobmcallan/riskelle/internal/version"
)

// Path is where a repository keeps its rules; it is also the rules source a
// provenance names when the file was read.
const Path = ".risk/rules.json"

// SourceBuiltin is the rules source when the target commit holds no file.
const SourceBuiltin = "built-in"

// File is what .risk/rules.json may hold: additions to the built-in rules
// only. No field replaces a built-in setting; a key naming one is refused by
// name (see Parse).
type File struct {
	Generated []string            `json:"generated"` // extra generated-path globs in the built-in syntax; size leaves them out
	Areas     map[string][]string `json:"areas"`     // extra sensitive areas: globs by area name, added to the built-in ones; report context, never scored

	// The exclusion toggles: nil (absent) or true leaves the class's files out
	// of size, degree and untested, the default; false counts them, which can
	// only add risk. Any other value is refused by name (see Parse).
	ExcludeNonCodeFiles *bool `json:"exclude_non_code_files"` // the non_code catalog
	ExcludeLogFiles     *bool `json:"exclude_log_files"`      // the run_record catalog

	// Production roots: directory globs ending in /**. A consequence whose
	// triggering path lies under one is floored at the highest level, so a
	// root can only raise a floor.
	ProductionRoots []string `json:"production_roots"`
}

// toggle is one exclusion toggle: its key, the class it governs and its value
// in a File.
type toggle struct {
	key   string
	class facts.Class
	get   func(File) *bool
}

// toggles names each exclusion toggle's key, the class it governs and its
// value in a File, in Class order, so the effective rules have one fixed order.
var toggles = []toggle{
	{"exclude_non_code_files", facts.ClassNonCode, func(f File) *bool { return f.ExcludeNonCodeFiles }},
	{"exclude_log_files", facts.ClassRunRecord, func(f File) *bool { return f.ExcludeLogFiles }},
}

// areaName is what an area name may be.
var areaName = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

// refused holds the lower-cased JSON keys of the built-in settings that File
// does not add to: bands, level edges and exclusion classes. Replacing any of
// them could lower a score, so .risk/rules.json may not set them. It is
// derived from score.Rules, so a built-in setting added later is refused too.
var refused = func() map[string]bool {
	m := map[string]bool{}
	for _, k := range jsonKeys(reflect.TypeFor[score.Rules]()) {
		m[strings.ToLower(k)] = true
	}
	for _, k := range jsonKeys(reflect.TypeFor[File]()) {
		delete(m, strings.ToLower(k))
	}
	return m
}()

// jsonKeys returns the JSON names of a struct type's tagged fields.
func jsonKeys(t reflect.Type) []string {
	var keys []string
	for f := range t.Fields() {
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name != "" && name != "-" {
			keys = append(keys, name)
		}
	}
	return keys
}

// Parse decodes a rules file strictly: a key naming a built-in setting is
// refused by name, and an unknown key, trailing data or an invalid pattern is
// an error, so no setting is silently ignored.
func Parse(data []byte) (File, error) {
	if err := refuse(data); err != nil {
		return File{}, err
	}
	if err := checkToggles(data); err != nil {
		return File{}, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var f File
	if err := dec.Decode(&f); err != nil {
		return File{}, err
	}
	if err := dec.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		return File{}, errors.New("trailing data after the rules object")
	}
	if err := checkPatterns("generated", f.Generated); err != nil {
		return File{}, err
	}
	if err := checkPatterns("production_roots", f.ProductionRoots); err != nil {
		return File{}, err
	}
	for i, p := range f.ProductionRoots {
		if !strings.HasSuffix(p, "/**") || p == "/**" {
			return File{}, fmt.Errorf("production_roots[%d] %q: a production root names a directory and ends in /**", i, p)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(f.Areas)) {
		if !areaName.MatchString(name) {
			return File{}, fmt.Errorf("areas: invalid name %q", name)
		}
		if len(f.Areas[name]) == 0 {
			return File{}, fmt.Errorf("areas.%s: no patterns", name)
		}
		if err := checkPatterns("areas."+name, f.Areas[name]); err != nil {
			return File{}, err
		}
	}
	return f, nil
}

// checkPatterns refuses an empty or absolute pattern, naming field and index.
func checkPatterns(field string, patterns []string) error {
	for i, p := range patterns {
		if p == "" {
			return fmt.Errorf("%s[%d]: empty pattern", field, i)
		}
		if strings.HasPrefix(p, "/") {
			return fmt.Errorf("%s[%d] %q: patterns are repo-relative", field, i, p)
		}
	}
	return nil
}

// refuse returns an error naming, in sorted order, every top-level key of
// data that would replace a built-in setting. Keys match case-insensitively,
// as encoding/json matches fields. Data that is not a JSON object is left to
// Parse's strict decode to report.
func refuse(data []byte) error {
	var obj map[string]json.RawMessage
	if json.NewDecoder(bytes.NewReader(data)).Decode(&obj) != nil {
		return nil
	}
	var names []string
	for k := range obj {
		if refused[strings.ToLower(k)] {
			names = append(names, strconv.Quote(k))
		}
	}
	if len(names) == 0 {
		return nil
	}
	slices.Sort(names)
	return fmt.Errorf("refused %s: %s may only add risk or context; these would replace built-in settings and could lower a score",
		strings.Join(names, ", "), Path)
}

// checkToggles returns an error naming, in sorted order, every exclusion
// toggle in data whose value is not exactly true or false, with that value.
// Keys match case-insensitively, as encoding/json matches fields, and null is
// refused though encoding/json would accept it into a *bool. Data that is not
// a JSON object is left to Parse's strict decode to report.
func checkToggles(data []byte) error {
	var obj map[string]json.RawMessage
	if json.NewDecoder(bytes.NewReader(data)).Decode(&obj) != nil {
		return nil
	}
	var items []string
	for k, raw := range obj {
		isToggle := slices.ContainsFunc(toggles, func(t toggle) bool { return t.key == strings.ToLower(k) })
		if v := string(bytes.TrimSpace(raw)); isToggle && v != "true" && v != "false" {
			items = append(items, strconv.Quote(k)+" = "+v)
		}
	}
	if len(items) == 0 {
		return nil
	}
	slices.Sort(items)
	return fmt.Errorf("refused %s: %s may set an exclusion toggle only to false, which counts those files, or to true, the default",
		strings.Join(items, ", "), Path)
}

// Apply returns r with the file's additions merged in: Generated becomes the
// sorted, de-duplicated union of both, nil when empty, as does
// ProductionRoots, and so does each area's
// patterns in Areas, a fresh map nil when empty. Each exclusion toggle that
// is absent or true adds its class to SizeExcludes once; one set to false
// removes it, so its files count. r is not changed.
func Apply(r score.Rules, f File) score.Rules {
	ex := slices.Clone(r.SizeExcludes)
	for _, t := range toggles {
		if v := t.get(f); v != nil && !*v {
			ex = slices.DeleteFunc(ex, func(c facts.Class) bool { return c == t.class })
		} else if !slices.Contains(ex, t.class) {
			ex = append(ex, t.class)
		}
	}
	r.SizeExcludes = ex
	r.Generated = union(r.Generated, f.Generated)
	r.ProductionRoots = union(r.ProductionRoots, f.ProductionRoots)
	var areas map[string][]string
	for _, m := range []map[string][]string{r.Areas, f.Areas} {
		for name, ps := range m {
			if areas == nil {
				areas = map[string][]string{}
			}
			areas[name] = union(areas[name], ps)
		}
	}
	r.Areas = areas
	return r
}

// union returns the sorted, de-duplicated patterns of a and b in a new slice,
// nil when empty.
func union(a, b []string) []string {
	u := slices.Concat(a, b)
	slices.Sort(u)
	if u = slices.Compact(u); len(u) == 0 {
		return nil
	}
	return u
}

// Hash returns "sha256:" and the hex SHA-256 of the rules' JSON, whose field
// order is fixed and slices ordered, so equal rules always hash equally.
func Hash(r score.Rules) string {
	data, err := json.Marshal(r)
	if err != nil {
		panic(err) // Rules holds only strings, ints, and slices and string-keyed maps of them
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Load returns the built-in rules plus .risk/rules.json as it is at commit (a
// full SHA, recorded as given), and their provenance. With no file the
// built-in rules apply with their default toggles; a file that does not parse
// is an error, never a silent fall back.
func Load(ctx context.Context, repo, commit string) (score.Rules, score.Provenance, error) {
	var f File
	prov := score.Provenance{Version: version.Get().String(), RulesSource: SourceBuiltin, RulesCommit: commit}
	data, found, err := facts.ReadFileAt(ctx, repo, commit, Path)
	if err == nil && found {
		if f, err = Parse(data); err == nil {
			prov.RulesSource = Path
		}
	}
	if err != nil {
		return score.Rules{}, score.Provenance{}, fmt.Errorf("rules: %s at %s: %w", Path, commit, err)
	}
	r := Apply(score.Default(), f)
	prov.RulesHash = Hash(r)
	prov.ExcludeNonCodeFiles = slices.Contains(r.SizeExcludes, facts.ClassNonCode)
	prov.ExcludeLogFiles = slices.Contains(r.SizeExcludes, facts.ClassRunRecord)
	return r, prov, nil
}

// Score scores f with the rules read at f.Base, the target branch commit,
// never f.Head or the working tree, and records their provenance.
func Score(ctx context.Context, repo string, f facts.Facts) (score.Result, error) {
	r, prov, err := Load(ctx, repo, f.Base)
	if err != nil {
		return score.Result{}, err
	}
	res := score.Score(f, r)
	res.Provenance = prov
	return res, nil
}
