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

// Apply returns r with the file's additions merged in: Generated becomes the
// sorted, de-duplicated union of both, nil when empty, and so does each area's
// patterns in Areas, a fresh map nil when empty. r is not changed.
func Apply(r score.Rules, f File) score.Rules {
	r.Generated = union(r.Generated, f.Generated)
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
// built-in rules apply unchanged; a file that does not parse is an error,
// never a silent fall back.
func Load(ctx context.Context, repo, commit string) (score.Rules, score.Provenance, error) {
	r := score.Default()
	prov := score.Provenance{Version: version.Get().String(), RulesSource: SourceBuiltin, RulesCommit: commit}
	data, found, err := facts.ReadFileAt(ctx, repo, commit, Path)
	if err == nil && found {
		var f File
		if f, err = Parse(data); err == nil {
			r, prov.RulesSource = Apply(r, f), Path
		}
	}
	if err != nil {
		return score.Rules{}, score.Provenance{}, fmt.Errorf("rules: %s at %s: %w", Path, commit, err)
	}
	prov.RulesHash = Hash(r)
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
