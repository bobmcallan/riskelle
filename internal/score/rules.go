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
	TestsUntested      Bands `json:"tests_untested"` // production files changed with no test change

	ImpactExcludes   []facts.Class `json:"impact_excludes"` // classes of changed files impact leaves out
	ImpactDependents Bands         `json:"impact_dependents"`

	Levels []LevelEdge `json:"levels"` // ascending, the first at Min 0

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
func Default() Rules {
	return Rules{
		SizeExcludes: []facts.Class{facts.ClassDocs, facts.ClassGenerated, facts.ClassLock},
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
	}
}
