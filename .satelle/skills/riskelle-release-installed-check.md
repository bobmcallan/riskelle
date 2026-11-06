---
name: riskelle-release-installed-check
type: check
description: CI/CD's last gate on approved → done. The riskelle on the PATH is the released build at the latest v* tag on origin/main. Deterministic, no agent.
---

# Release installed

The release run ends with the published build installed here with the release
install script; riskelle then scores its own next stories with it.

```sh
set -u
git fetch -q origin --tags || { echo "git fetch origin failed"; exit 1; }
tag="v$(git tag -l 'v[0-9]*' | sed 's/^v//' | sort -V | tail -1)"
sha=$(git rev-parse --short=7 origin/main)
command -v riskelle >/dev/null 2>&1 || { echo "riskelle is not on the PATH"; exit 1; }
v=$(riskelle version)
printf '%s\n' "$v" | grep -qE "^riskelle $tag " || { echo "installed riskelle is not $tag: $v"; exit 1; }
printf '%s\n' "$v" | grep -q "$sha" || { echo "installed riskelle does not name origin/main $sha: $v"; exit 1; }
echo "installed: $v"
```
