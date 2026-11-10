package score

import "github.com/bobmcallan/riskelle/internal/facts"

// Band is one fixed point band of a measure: a value at or above Min scores
// Points, unless a later band also matches.
type Band struct {
	Min    int `json:"min"`
	Points int `json:"points"`
}

// Bands are a measure's bands in ascending Min order, the first at Min 0 with
// 0 points. A value scores the last band whose Min it reaches.
type Bands []Band

// at returns the band that v falls in.
func (b Bands) at(v int) Band {
	var got Band
	for _, band := range b {
		if v >= band.Min {
			got = band
		}
	}
	return got
}

// LevelEdge is the lowest total that is reported as Level.
type LevelEdge struct {
	Min   int   `json:"min"`
	Level Level `json:"level"`
}

// Floor is the lowest level a change carrying a consequence class is
// reported at. A floor only raises the level; it never adds points.
// RepoClass is a consequence class a repo names in .risk/rules.json: the path
// globs that carry it and its floor.
type RepoClass struct {
	Class string   `json:"class"`
	Paths []string `json:"paths"`
	Floor Level    `json:"floor"`
}

type Floor struct {
	Class string `json:"class"` // a facts consequence class
	Level Level  `json:"level"`
}

// Rules are everything Score needs besides the facts. They are a value passed
// in, so the built-in rules and (later) rules extended from a repository's
// .risk/rules.json are scored by the same function.
type Rules struct {
	SizeExcludes []facts.Class `json:"size_excludes"` // classes whose lines size leaves out
	Generated    []string      `json:"generated"`     // repo-added generated-path globs (.risk/rules.json); size leaves them out with ClassGenerated
	SizeLines    Bands         `json:"size_lines"`
	SpreadFiles  Bands         `json:"spread_files"`
	SpreadDirs   Bands         `json:"spread_directories"`
	SpreadProjs  Bands         `json:"spread_projects"`

	DegreeExisting     Bands `json:"degree_existing_lines"` // lines added and removed in files that existed at the merge base
	BranchAgeDays      Bands `json:"branch_age_days"`
	BranchTrunkCommits Bands `json:"branch_trunk_commits"`
	TestsUntested      Bands `json:"tests_untested"` // production code files (a paired or unpaired language) changed without their test

	ImpactExcludes   []facts.Class `json:"impact_excludes"` // classes of changed files impact leaves out
	ImpactDependents Bands         `json:"impact_dependents"`

	Levels []LevelEdge `json:"levels"` // ascending, the first at Min 0

	// The highest level the total alone is reported at. A total in a band
	// above it keeps its points and reports this level; only a consequence
	// floor raises the level past it. Always set and always hashed;
	// .risk/rules.json may not set it. A level not in Levels caps nothing.
	ShapeCap Level `json:"shape_cap"`

	// Built-in floors, one per consequence class, sorted by class. Always set
	// and always hashed; .risk/rules.json may not set them.
	ConsequenceFloors []Floor `json:"consequence_floors"`

	// Repo-named production roots (.risk/rules.json): directory globs. A
	// consequence with a path under one is floored at the highest level.
	// Omitted when empty, so the built-in rules hash as before.
	ProductionRoots []string `json:"production_roots,omitempty"`

	// Repo consequence classes (.risk/rules.json): a class whose path globs
	// match a changed or planned path carries it, floored at its level
	// (Medium or High). Sorted by class; nil when none.
	RepoClasses []RepoClass `json:"consequence_classes,omitempty"`

	// Repo-added sensitive areas (.risk/rules.json): globs by area name,
	// beside the built-in areas in facts. Report context only, never scored;
	// omitted when empty, so the built-in rules hash as before.
	Areas map[string][]string `json:"areas,omitempty"`
}

// Default returns riskelle's built-in rules: compiled into the binary and
// changed only by a riskelle release, so the scale is the same everywhere.
// Each call builds a fresh value, so no caller can change the built-in scale.
//
// The bands and level edges are provisional until they are calibrated from
// real merge history (sty_62b9ea06). The highest band of every measure must
// sum to 100 or less: size and spread take at most 50, degree 15, branch age
// 15 (days 10, trunk commits 5), tests 10 and impact 10.
//
// Test files' lines are left out of size and degree, so adding or changing
// tests never raises a score; the tests measure still sees them.
func Default() Rules {
	return Rules{
		SizeExcludes: []facts.Class{facts.ClassDocs, facts.ClassGenerated, facts.ClassLock, facts.ClassTest},
		SizeLines:    Bands{{0, 0}, {51, 5}, {201, 10}, {401, 20}, {801, 30}},
		SpreadFiles:  Bands{{0, 0}, {6, 2}, {11, 4}, {16, 8}},
		SpreadDirs:   Bands{{0, 0}, {3, 2}, {5, 4}, {9, 6}},
		SpreadProjs:  Bands{{0, 0}, {2, 3}, {3, 6}},

		DegreeExisting:     Bands{{0, 0}, {26, 2}, {101, 5}, {251, 10}, {501, 15}},
		BranchAgeDays:      Bands{{0, 0}, {3, 3}, {8, 6}, {15, 10}},
		BranchTrunkCommits: Bands{{0, 0}, {11, 2}, {51, 3}, {101, 5}},
		TestsUntested:      Bands{{0, 0}, {1, 10}},

		// The largest dependent count of a changed package or file.
		ImpactExcludes:   []facts.Class{facts.ClassDocs, facts.ClassGenerated, facts.ClassLock},
		ImpactDependents: Bands{{0, 0}, {3, 2}, {6, 4}, {11, 7}, {21, 10}},

		Levels: []LevelEdge{{0, Low}, {20, Medium}, {40, High}},
		// Volume alone is never destructive: High comes only from a floor.
		ShapeCap: Medium,

		// A consequence raises the level to at least its floor, never the total.
		// access_widened (a grant added or altered) and secret_added (a secret
		// committed, which a revert does not unleak) are the built-in High floors.
		ConsequenceFloors: []Floor{{facts.ConsequenceAccessWidened, High}, {facts.ConsequenceIdentityRights, Medium}, {facts.ConsequencePipelineIdentity, Medium}, {facts.ConsequenceSecretAdded, High}},
	}
}
