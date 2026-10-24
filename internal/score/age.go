package score

import "github.com/bobmcallan/riskelle/internal/facts"

// branchAge scores how long the branch stayed away from the trunk, as two
// entries: the days from its first commit to head, and the commits the trunk
// gained meanwhile. Both come from commit history in the facts, never the
// clock. From names the commit range each was counted over, or is empty when
// the facts carry no commits (an estimate from a plan).
func branchAge(f facts.Facts, r Rules) []Point {
	days := r.BranchAgeDays.at(f.BranchAgeDays)
	trunk := r.BranchTrunkCommits.at(f.TrunkCommits)
	return []Point{
		{Measure: MeasureBranchAge, Fact: "days", Value: f.BranchAgeDays, Min: days.Min, Points: days.Points, From: commitRange(f.MergeBase, f.Head)},
		{Measure: MeasureBranchAge, Fact: "trunk_commits", Value: f.TrunkCommits, Min: trunk.Min, Points: trunk.Points, From: commitRange(f.MergeBase, f.Base)},
	}
}

// commitRange returns [from..to], or an empty list when either end is unknown.
func commitRange(from, to string) []string {
	if from == "" || to == "" {
		return []string{}
	}
	return []string{from + ".." + to}
}
