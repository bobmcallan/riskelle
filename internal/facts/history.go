package facts

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

const secondsPerDay = 86400

// branchHistory reads the branch's age and the trunk's drift from commit
// history alone, never the clock. Age runs from the earliest author timestamp
// among the commits in mergeBase..head (rebase, amend and cherry-pick keep it,
// so it marks when the branch's work was first written) to the head commit's
// committer timestamp (when the branch was last committed). A branch with no
// commits of its own has age 0. Drift is the number of commits in
// mergeBase..base.
func branchHistory(ctx context.Context, repo, base, mergeBase, head string) (age, drift int, err error) {
	out, err := git(ctx, repo, "rev-list", "--count", mergeBase+".."+base)
	if err != nil {
		return 0, 0, err
	}
	drift, err = strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil || drift < 0 {
		return 0, 0, fmt.Errorf("facts: malformed rev-list count %q", strings.TrimSpace(string(out)))
	}

	out, err = git(ctx, repo, "log", "--no-show-signature", "--format=%at", mergeBase+".."+head)
	if err != nil {
		return 0, 0, err
	}
	authored, err := timestamps(out)
	if err != nil {
		return 0, 0, err
	}
	if len(authored) == 0 {
		return 0, drift, nil
	}
	first := authored[0]
	for _, ts := range authored[1:] {
		first = min(first, ts)
	}

	out, err = git(ctx, repo, "log", "-1", "--no-show-signature", "--format=%ct", head)
	if err != nil {
		return 0, 0, err
	}
	committed, err := timestamps(out)
	if err != nil {
		return 0, 0, err
	}
	if len(committed) != 1 {
		return 0, 0, fmt.Errorf("facts: want one committer timestamp for %s, got %d", head, len(committed))
	}
	return ageDays(first, committed[0]), drift, nil
}

// ageDays returns the whole days from first to head, both Unix seconds,
// floored; a head before first (clock skew between machines) gives 0.
func ageDays(first, head int64) int {
	if head <= first {
		return 0
	}
	return int((head - first) / secondsPerDay)
}

// timestamps parses one non-negative Unix timestamp per non-empty line.
func timestamps(out []byte) ([]int64, error) {
	var ts []int64
	for _, line := range strings.Split(string(out), "\n") {
		if line == "" {
			continue
		}
		n, err := strconv.ParseInt(line, 10, 64)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("facts: malformed commit timestamp %q", line)
		}
		ts = append(ts, n)
	}
	return ts, nil
}
