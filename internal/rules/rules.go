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
	"slices"
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
// only. No field replaces a built-in setting.
type File struct {
	Generated []string `json:"generated"` // extra generated-path globs in the built-in syntax; size leaves them out
}

// Parse decodes a rules file strictly: an unknown key, trailing data or an
// invalid pattern is an error, so no setting is silently ignored.
func Parse(data []byte) (File, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var f File
	if err := dec.Decode(&f); err != nil {
		return File{}, err
	}
	if err := dec.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		return File{}, errors.New("trailing data after the rules object")
	}
	for i, p := range f.Generated {
		if p == "" {
			return File{}, fmt.Errorf("generated[%d]: empty pattern", i)
		}
		if strings.HasPrefix(p, "/") {
			return File{}, fmt.Errorf("generated[%d] %q: patterns are repo-relative", i, p)
		}
	}
	return f, nil
}

// Apply returns r with the file's additions merged in: Generated becomes the
// sorted, de-duplicated union of both, nil when empty. r is not changed.
func Apply(r score.Rules, f File) score.Rules {
	gen := slices.Concat(r.Generated, f.Generated)
	slices.Sort(gen)
	if r.Generated = slices.Compact(gen); len(r.Generated) == 0 {
		r.Generated = nil
	}
	return r
}

// Hash returns "sha256:" and the hex SHA-256 of the rules' JSON, whose field
// order is fixed and slices ordered, so equal rules always hash equally.
func Hash(r score.Rules) string {
	data, err := json.Marshal(r)
	if err != nil {
		panic(err) // Rules holds only strings, ints and slices of them
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
