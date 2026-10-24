#!/usr/bin/env bash
# github-pr.sh scores a GitHub pull request with riskelle and posts the result:
# a commit status named "riskelle" on the head carrying the band, and one
# summary comment on the PR, found by its marker and updated in place.
#
# It runs in a checkout with full history (actions/checkout fetch-depth: 0)
# and reads, from the environment:
#
#   RISKELLE_BIN       the riskelle binary (default: riskelle on PATH)
#   GITHUB_REPOSITORY  owner/repo
#   PR_NUMBER          the pull request number
#   BASE_REF           the target branch name; its tip, origin/<BASE_REF>, is
#                      the base, and the rules are read there
#   HEAD_SHA           the PR head commit (not the synthetic merge commit)
#   GITHUB_SERVER_URL, GITHUB_RUN_ID  for the run link on a failure
#   GH_TOKEN           for gh: statuses: write, pull-requests: write
#   GITHUB_STEP_SUMMARY, GITHUB_OUTPUT  optional, set by Actions
#
# The scoring is riskelle's alone: this script never re-renders or re-scores.
# The comment body is the marker line followed by the exact output of
# `riskelle score --format markdown`. Needs bash, git, gh and jq.
set -euo pipefail

marker='<!-- riskelle:summary -->'

for v in GITHUB_REPOSITORY PR_NUMBER BASE_REF HEAD_SHA GITHUB_SERVER_URL GITHUB_RUN_ID; do
	[[ -n ${!v:-} ]] || { echo "github-pr.sh: $v is not set" >&2; exit 2; }
done
bin=${RISKELLE_BIN:-riskelle}
repo=$GITHUB_REPOSITORY
run_url="$GITHUB_SERVER_URL/$repo/actions/runs/$GITHUB_RUN_ID"
tmp=$(mktemp -d "${RUNNER_TEMP:-${TMPDIR:-/tmp}}/riskelle-pr.XXXXXX")
trap 'rm -rf "$tmp"' EXIT

# status posts the riskelle commit status on the head.
status() { # state description target_url
	jq -n --arg state "$1" --arg description "$2" --arg url "$3" \
		'{state: $state, context: "riskelle", description: $description, target_url: $url}' >"$tmp/status.json"
	gh api --method POST "repos/$repo/statuses/$HEAD_SHA" --input "$tmp/status.json" >/dev/null
}

# fail marks the head errored, pointing at this run, and leaves the comment.
fail() {
	echo "github-pr.sh: $1" >&2
	status error "riskelle failed; see the run" "$run_url" || true
	exit 1
}

base=$(git rev-parse --verify --quiet "refs/remotes/origin/$BASE_REF^{commit}") ||
	fail "no origin/$BASE_REF in this checkout (check out with fetch-depth: 0)"
"$bin" score --format json --base "$base" --head "$HEAD_SHA" >"$tmp/report.json" || fail "riskelle score failed"
"$bin" score --format markdown --base "$base" --head "$HEAD_SHA" >"$tmp/report.md" || fail "riskelle score failed"
cat "$tmp/report.json"
[[ -z ${GITHUB_STEP_SUMMARY:-} ]] || cat "$tmp/report.md" >>"$GITHUB_STEP_SUMMARY"

# One summary comment: update the oldest marked comment, delete any others (a
# past race), create one when there is none. Other comments are never touched.
jq -n --arg marker "$marker" --rawfile md "$tmp/report.md" '{body: ($marker + "\n" + $md)}' >"$tmp/comment.json"
gh api --paginate "repos/$repo/issues/$PR_NUMBER/comments" >"$tmp/pages.json" || fail "cannot list the PR's comments"
mapfile -t ids < <(jq -s -r --arg marker "$marker" \
	'add // [] | map(select(.body | startswith($marker))) | sort_by(.id) | .[].id' "$tmp/pages.json")
if [[ ${#ids[@]} -eq 0 ]]; then
	gh api --method POST "repos/$repo/issues/$PR_NUMBER/comments" --input "$tmp/comment.json" >"$tmp/posted.json" ||
		fail "cannot create the summary comment"
else
	gh api --method PATCH "repos/$repo/issues/comments/${ids[0]}" --input "$tmp/comment.json" >"$tmp/posted.json" ||
		fail "cannot update the summary comment"
	for id in "${ids[@]:1}"; do
		gh api --method DELETE "repos/$repo/issues/comments/$id" >/dev/null || fail "cannot delete duplicate comment $id"
	done
fi

total=$(jq -r .total "$tmp/report.json")
level=$(jq -r .level "$tmp/report.json")
status success "$level · $total / 100" "$(jq -r .html_url "$tmp/posted.json")" || fail "cannot post the status"
if [[ -n ${GITHUB_OUTPUT:-} ]]; then
	printf 'total=%s\nlevel=%s\n' "$total" "$level" >>"$GITHUB_OUTPUT"
fi
