---
name: riskelle-release-published-check
type: check
description: CI/CD gate on approved → done. The latest v* tag points at origin/main's head and sorts above every other v* tag, its release.yml run completed with success, and its GitHub release carries assets. Deterministic, no agent.
---

# Release published

riskelle's version is its git tag: the release run tags origin/main and pushes
the tag, which triggers `release.yml` (test, build archives and checksums,
publish the GitHub Release, deploy Pages).

```sh
set -u
git fetch -q origin --tags || { echo "git fetch origin failed"; exit 1; }
main=$(git rev-parse origin/main)
tag=$(git tag -l 'v[0-9]*' | sed 's/^v//' | sort -V | tail -1)
[ -n "$tag" ] || { echo "no v* tag exists"; exit 1; }
tag="v$tag"
[ "$(git rev-parse "$tag^{commit}")" = "$main" ] || { echo "the latest tag $tag does not point at origin/main $(git rev-parse --short=12 origin/main)"; exit 1; }
ok=$(gh run list --workflow release.yml --branch "$tag" --limit 5 --json status,conclusion,url |
  jq -r '[.[] | select(.status == "completed" and .conclusion == "success")][0].url // empty')
[ -n "$ok" ] || { echo "no successful release.yml run for $tag"; exit 1; }
n=$(gh release view "$tag" --json assets --jq '.assets | length' 2>/dev/null || echo 0)
[ "${n:-0}" -gt 0 ] || { echo "release $tag is missing or has no assets"; exit 1; }
echo "$tag on origin/main $(git rev-parse --short=12 origin/main): release.yml $ok succeeded; $n assets published"
```
