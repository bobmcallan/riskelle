---
name: riskelle-task-validate-before-check
type: check
description: Entry gate for a task run (backlog → in_progress). Accepts an execution whose parent is an existing task item; refuses one without, naming why. Deterministic, no agent.
---

# Task run: validate before

A run executes a task: `satelle task run <task>` creates the execution with the
task as its parent. This refuses a run that names no task, or a task that does
not exist, before any work starts.

```sh
set -eu
parent=$(satelle execution get "$SATELLE_ITEM" --json | jq -r '.parent // ""')
[ -n "$parent" ] || { echo "execution $SATELLE_ITEM names no parent task: a run is created by 'satelle task run <task>'"; exit 1; }
kind=$(satelle task get "$parent" --json 2>/dev/null | jq -r '.kind // ""') || kind=""
[ "$kind" = task ] || { echo "execution $SATELLE_ITEM's parent $parent is not an existing task"; exit 1; }
echo "runs task $parent"
```
