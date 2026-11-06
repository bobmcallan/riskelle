---
name: riskelle-release-run-check
type: check
description: CI/CD's first gate on approved → done. Accepts only while an in-progress run of the release task (the task tagged release) lists the item among its inputs and the item's pull-request head is on origin/main. A hand-presented done move has no such run and is refused. Deterministic, no agent.
---

# Release run (only a release run closes an item)

`done` is terminal and reached only by a run of the release task, after the
item's head is on main, the version is tagged, the release is published and
installed. The release task is the task tagged `release`; its procedure is
`.satelle/skills/release.md`.

```sh
set -u
for tool in gh jq git satelle; do
  command -v "$tool" >/dev/null 2>&1 || { echo "$tool is not installed"; exit 1; }
done
task=$(satelle task list --tag release --json | jq -r '.[0].id // empty')
[ -n "$task" ] || { echo "no task is tagged release"; exit 1; }
listed=""
for exe in $(satelle execution list --parent "$task" --status in_progress --json | jq -r '.[].id'); do
  if satelle ledger list "$exe" --kind task.run --json | jq -e --arg id "$SATELLE_ITEM" 'any(.[]; .payload.inputs | index($id))' >/dev/null; then
    listed=$exe
    break
  fi
done
if [ -z "$listed" ]; then
  echo "no in-progress run of $task lists $SATELLE_ITEM: an item reaches done only through a release run (git fetch origin, then satelle task run $task --base origin/main)"
  exit 1
fi
git fetch -q origin || { echo "git fetch origin failed"; exit 1; }
head=$(gh pr list --state all --base main --json headRefOid,title --limit 200 |
  jq -r --arg id "($SATELLE_ITEM)" '[.[] | select(.title | endswith($id))][0].headRefOid // empty')
[ -n "$head" ] || { echo "no pull request against main has a title ending ($SATELLE_ITEM)"; exit 1; }
git merge-base --is-ancestor "$head" origin/main || { echo "$SATELLE_ITEM's head $(git rev-parse --short=12 "$head") is not on origin/main yet"; exit 1; }
echo "release run $listed lists $SATELLE_ITEM; its head $(git rev-parse --short=12 "$head") is on origin/main"
```
