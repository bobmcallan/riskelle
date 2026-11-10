// Package score turns a change's facts into its risk score. Score is a pure,
// deterministic function of facts and rules: no file, network or clock, so the
// same facts and rules always give the same result, whether the facts were
// collected from git or predicted from a plan.
package score

import (
	"sort"

	"github.com/bobmcallan/riskelle/internal/facts"
)

// Level is the band a total is reported in.
type Level string

// The levels, lowest risk first.
const (
	Low    Level = "Low"
	Medium Level = "Medium"
	High   Level = "High"
)

// Measure names.
const (
	MeasureSize      = "size"
	MeasureSpread    = "spread"
	MeasureDegree    = "degree"
	MeasureBranchAge = "branch_age"
	MeasureTests     = "tests"
)

// Point is one entry of the breakdown: the band one fact of one measure fell
// in, and the facts that produced it.
type Point struct {
	Measure  string   `json:"measure"`            // one of the Measure names
	Fact     string   `json:"fact"`               // e.g. "lines", "files", "existing_lines", "days", "dependents"
	Value    int      `json:"value"`              // the fact's value
	Min      int      `json:"min"`                // lower edge of the band it fell in
	Points   int      `json:"points"`             // the band's points
	From     []string `json:"from"`               // paths, directories or projects counted; sorted, non-nil
	Excluded []string `json:"excluded,omitempty"` // size and impact: paths left out by class, or (size) as a deleted file nothing but docs depends on; sorted

	// Impact only: each counted package or file and its dependent count,
	// sorted by name.
	Dependents []facts.Dependency `json:"dependents,omitempty"`
}

// Result is a scored change.
type Result struct {
	Total        int           `json:"total"` // 0-100
	Level        Level         `json:"level"`
	ShapeLevel   Level         `json:"shape_level"`            // the level of the total alone, capped at the rules' shape cap, before any consequence floor
	FlooredBy    []string      `json:"floored_by,omitempty"`   // when a floor raised Level above ShapeLevel, the classes whose floor set it, sorted; nil otherwise
	Triggers     []Trigger     `json:"triggers,omitempty"`     // the consequences whose effective floor is the highest level, sorted by class; nil when none
	Breakdown    []Point       `json:"breakdown"`              // size/lines; spread/files, directories, projects; degree/existing_lines; branch_age/days, trunk_commits; tests/untested_production_files; impact/dependents
	Consequences []Consequence `json:"consequences,omitempty"` // consequence classes with their floors, sorted by class; nil when none
	Areas        []Area        `json:"areas,omitempty"`        // sensitive areas the change lands in, sorted by name; reviewer context only, never scored; nil when none
	Provenance   Provenance    `json:"provenance,omitzero"`    // set by the caller that loaded the rules; Score leaves it zero
}

// Consequence is one consequence class the change carries, with the floor
// the rules give it and, when a production root raised that floor, the roots
// its paths lie under.
type Consequence struct {
	facts.Consequence
	Floor           Level    `json:"floor"`
	ProductionRoots []string `json:"production_roots,omitempty"` // in rules order; nil when no root raised the floor
}

// Why a trigger's floor is the highest level.
const (
	WhyClass          = "class"           // the built-in class's own floor is the highest level
	WhyProductionRoot = "production_root" // a production root raised a lower floor to the highest level
	WhyRepoClass      = "repo_class"      // the repo class's own floor is the highest level
)

// Trigger is one consequence whose effective floor is the highest level: its
// class and evidence (paths and matched types), and why its floor is that high.
type Trigger struct {
	facts.Consequence
	Why string `json:"why"`
}

// Area is one sensitive area a change lands in and the changed paths that
// matched it, sorted.
type Area struct {
	Name  string   `json:"name"`
	Paths []string `json:"paths"`
}

// Provenance records what a result was scored with, so it can be reproduced.
type Provenance struct {
	Version     string `json:"riskelle_version"` // the riskelle build
	RulesSource string `json:"rules_source"`     // "built-in" or ".risk/rules.json"
	RulesCommit string `json:"rules_commit"`     // full SHA of the target-branch commit the rules were read at
	RulesHash   string `json:"rules_hash"`       // "sha256:" + hex of the effective rules' JSON

	ExcludeNonCodeFiles bool `json:"exclude_non_code_files"` // non-code files left out of size, degree and untested
	ExcludeLogFiles     bool `json:"exclude_log_files"`      // run records left out of size, degree and untested
}

// Score scores the facts of one change against rules. Every breakdown entry is
// kept, 0 points included, in a fixed order; later measures append theirs.
// The total's own level is capped at the rules' shape cap; the total and
// breakdown are not. Consequences are read after the total: they never add
// points and only raise the level to their floor. Areas are reviewer context: they are found after
// the total and never add points.
func Score(f facts.Facts, r Rules) Result {
	var paths []string
	for _, file := range f.Files {
		paths = append(paths, file.Path)
	}
	kept, excluded := counted(f.Files, f.Impact, r)
	breakdown := []Point{
		size(kept, excluded, r),
		point(MeasureSpread, "files", paths, r.SpreadFiles),
		point(MeasureSpread, "directories", f.Directories, r.SpreadDirs),
		point(MeasureSpread, "projects", f.Projects, r.SpreadProjs),
	}
	breakdown = append(breakdown, degree(kept, r))
	breakdown = append(breakdown, branchAge(f, r)...)
	breakdown = append(breakdown, untested(f.Files, kept, f.TestLanguages, r))
	breakdown = append(breakdown, impact(f, r))

	total := 0
	for _, p := range breakdown {
		total += p.Points
	}
	total = max(0, min(total, 100))
	cs, triggers := consequences(f, r)
	shape := capped(level(total, r.Levels), r.ShapeCap, r.Levels)
	lvl := floored(shape, cs, r.Levels)
	var by []string
	if lvl != shape {
		for _, c := range cs {
			if c.Floor == lvl {
				by = append(by, c.Class)
			}
		}
	}
	return Result{
		Total: total, Level: lvl, ShapeLevel: shape, FlooredBy: by, Triggers: triggers,
		Breakdown: breakdown, Consequences: cs, Areas: areas(f.Files, r),
	}
}

// consequences gives each consequence in the facts, production_deploy when a
// changed definition deploys to a production environment (deployConsequences),
// and each repo class a changed or planned path carries (repoConsequences),
// sorted by class, its
// floor from the rules: the built-in floor or the repo class's, copying its
// lists so the result aliases nothing. A class with no floor gets the lowest
// level, so it is reported and raises nothing. A consequence with any path
// under a production root is floored at the highest level instead, naming
// those roots. Nil when there are none.
//
// It also gives the triggers: each consequence whose effective floor is the
// highest level, in the same order, with fresh copies of its evidence and
// why: its built-in class's own floor, its repo class's own floor, or a
// production root that raised a lower floor. Nil when there are none.
func consequences(f facts.Facts, r Rules) ([]Consequence, []Trigger) {
	found := append(append([]facts.Consequence{}, f.Consequences...), deployConsequences(f.Files, r.ProductionEnvironments)...)
	found = append(found, repoConsequences(f.Files, r.RepoClasses)...)
	if len(found) == 0 {
		return nil, nil
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].Class < found[j].Class })
	lowest, top := Level(""), Level("")
	if len(r.Levels) > 0 {
		lowest, top = r.Levels[0].Level, r.Levels[len(r.Levels)-1].Level
	}
	roots := make([]facts.Globs, len(r.ProductionRoots))
	for i, root := range r.ProductionRoots {
		roots[i] = facts.CompileGlobs([]string{root})
	}
	out := make([]Consequence, 0, len(found))
	var triggers []Trigger
	for _, c := range found {
		floor, why := lowest, WhyClass
		for _, fl := range r.ConsequenceFloors {
			if fl.Class == c.Class {
				floor = fl.Level
			}
		}
		for _, rc := range r.RepoClasses {
			if rc.Class == c.Class {
				floor, why = rc.Floor, WhyRepoClass
			}
		}
		var in []string
		for i, g := range roots {
			for _, p := range c.Paths {
				if g.Match(p) {
					in = append(in, r.ProductionRoots[i])
					break
				}
			}
		}
		if len(in) > 0 && len(r.Levels) > 0 {
			if floor != top {
				why = WhyProductionRoot
			}
			floor = top
		}
		out = append(out, Consequence{
			Consequence: facts.Consequence{
				Class: c.Class,
				Paths: append([]string{}, c.Paths...),
				Types: append([]string{}, c.Types...),
			},
			Floor:           floor,
			ProductionRoots: in,
		})
		if len(r.Levels) > 0 && floor == top {
			triggers = append(triggers, Trigger{
				Consequence: facts.Consequence{
					Class: c.Class,
					Paths: append([]string{}, c.Paths...),
					Types: append([]string{}, c.Types...),
				},
				Why: why,
			})
		}
	}
	return out, triggers
}

// deployConsequences gives production_deploy the changed paths whose job
// environments (facts.File.Environments) name a production environment in
// names (facts.IsProduction), typed by those environments as written; both
// sorted and distinct. Nil when none does.
func deployConsequences(files []facts.File, names []string) []facts.Consequence {
	paths, types := map[string]bool{}, map[string]bool{}
	for _, file := range files {
		for _, env := range file.Environments {
			if facts.IsProduction(env, names) {
				paths[file.Path], types[env] = true, true
			}
		}
	}
	if len(paths) == 0 {
		return nil
	}
	return []facts.Consequence{{Class: facts.ConsequenceProductionDeploy, Paths: keys(paths), Types: keys(types)}}
}

// keys returns a set's members, sorted.
func keys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// repoConsequences gives each repo class, in rules order, the changed or
// planned paths its globs match, at the head path and the old path of a
// rename; a class read from paths alone has no types. Nil when none matches.
func repoConsequences(files []facts.File, classes []RepoClass) []facts.Consequence {
	var out []facts.Consequence
	for _, rc := range classes {
		g := facts.CompileGlobs(rc.Paths)
		set := map[string]bool{}
		for _, file := range files {
			for _, p := range []string{file.Path, file.OldPath} {
				if p != "" && g.Match(p) {
					set[p] = true
				}
			}
		}
		if len(set) == 0 {
			continue
		}
		paths := make([]string, 0, len(set))
		for p := range set {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		out = append(out, facts.Consequence{Class: rc.Class, Paths: paths, Types: []string{}})
	}
	return out
}

// floored returns the highest-ranked level among l and each consequence's
// floor, ranked by position in edges; a level not in edges never raises.
func floored(l Level, cs []Consequence, edges []LevelEdge) Level {
	for _, c := range cs {
		if rank(c.Floor, edges) > rank(l, edges) {
			l = c.Floor
		}
	}
	return l
}

// capped returns ceiling when l ranks above it in edges, else l; a ceiling not in
// edges (empty included) caps nothing.
func capped(l, ceiling Level, edges []LevelEdge) Level {
	if c := rank(ceiling, edges); c >= 0 && rank(l, edges) > c {
		return ceiling
	}
	return l
}

// rank is the position of x in edges, or -1 when it is not there.
func rank(x Level, edges []LevelEdge) int {
	for i, e := range edges {
		if e.Level == x {
			return i
		}
	}
	return -1
}

// counted splits files into those size, degree and tests count and the paths
// of the rest. A file is left out when it is in one of the rules' excluded
// classes, when its path matches the rules' Generated globs (it counts as
// generated), or when it is a deleted file nothing at head but docs depends
// on (unused). Size, degree and tests all count only the kept files.
func counted(files []facts.File, imp facts.Impact, r Rules) (kept []facts.File, excluded []string) {
	ex := map[facts.Class]bool{}
	for _, c := range r.SizeExcludes {
		ex[c] = true
	}
	var generated facts.Globs
	if ex[facts.ClassGenerated] {
		generated = facts.CompileGlobs(r.Generated)
	}
	for _, file := range files {
		if anyExcluded(file.Classes, ex) || generated.Match(file.Path) || unused(file, imp) {
			excluded = append(excluded, file.Path)
			continue
		}
		kept = append(kept, file)
	}
	return kept, excluded
}

// unused reports whether file is a deleted file nothing at head depends on:
// its impact entry (for a Go file, its package's entry) has 0 dependents, or,
// for a file impact counts as a file, every one of its dependents is a doc
// (docsOnly). A deleted file with no entry counts, since nothing shows that
// it is unused.
func unused(file facts.File, imp facts.Impact) bool {
	if !file.Deleted {
		return false
	}
	name, deps := file.Path, imp.Files
	if file.Package != "" {
		name, deps = file.Package, imp.Packages
	}
	for _, d := range deps {
		if d.Name == name {
			return d.Dependents == 0 || (file.Package == "" && docsOnly(file.Referrers, d.Dependents))
		}
	}
	return false
}

// docsOnly reports whether referrers accounts for all n dependents and each
// is in the built-in docs class: a doc naming a deleted file (a README saying
// it was removed, a ticket note) does not rely on it. Facts with dependents
// but no matching referrers count in full.
func docsOnly(referrers []string, n int) bool {
	if n == 0 || len(referrers) != n {
		return false
	}
	docs := map[facts.Class]bool{facts.ClassDocs: true}
	for _, p := range referrers {
		if !anyExcluded(facts.Classify(p), docs) {
			return false
		}
	}
	return true
}

// size sums the lines added and removed in the kept files, recording the
// paths counted() left out.
func size(kept []facts.File, excluded []string, r Rules) Point {
	p := Point{Measure: MeasureSize, Fact: "lines", From: []string{}, Excluded: excluded}
	for _, file := range kept {
		p.Value += file.Added + file.Removed
		p.From = append(p.From, file.Path)
	}
	sort.Strings(p.From)
	sort.Strings(p.Excluded)
	band := r.SizeLines.at(p.Value)
	p.Min, p.Points = band.Min, band.Points
	return p
}

func anyExcluded(classes []facts.Class, excluded map[facts.Class]bool) bool {
	for _, c := range classes {
		if excluded[c] {
			return true
		}
	}
	return false
}

// point scores a count of items, keeping a sorted copy of them.
func point(measure, fact string, items []string, bands Bands) Point {
	from := append([]string{}, items...)
	sort.Strings(from)
	band := bands.at(len(from))
	return Point{Measure: measure, Fact: fact, Value: len(from), Min: band.Min, Points: band.Points, From: from}
}

// level returns the level of the last edge the total reaches.
func level(total int, edges []LevelEdge) Level {
	var got Level
	for _, e := range edges {
		if total >= e.Min {
			got = e.Level
		}
	}
	return got
}
