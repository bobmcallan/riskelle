---
name: riskelle-integration-check
scope: project
type: skill
tags: [type:skill, type:reviewer, type:functional-check]
description: Functional-check gate on entry to merge. In the story's worktree, runs the integration suite (go test -tags integration ./tests/...). A repo with no tests/ directory yet passes with a note. Deterministic, zero LLM tokens.
---

# Integration check (entry to merge)

Runs the suite that `riskelle-integration-review` judges for adequacy. It runs
in the story's worktree (branch `story/<id>`), falling back to the current
directory.

```check
#!/usr/bin/env bash
set -uo pipefail
payload=$(cat)
sid=$(printf '%s' "$payload" | grep -oE 'sty_[a-f0-9]{8}' | head -1)
tree=$(git worktree list --porcelain 2>/dev/null | awk -v b="refs/heads/story/$sid" '/^worktree /{w=$2} $0=="branch "b{print w}')
cd "${tree:-.}" || { echo "INTEGRATION CHECK FAILED: cannot enter $tree"; exit 1; }

if [ ! -d tests ]; then
  echo "integration check PASS in $(pwd): no tests/ directory yet, nothing to run"
  exit 0
fi

out=$(go test -count=1 -tags integration ./tests/... 2>&1)
if [ $? -ne 0 ]; then
  echo "INTEGRATION CHECK FAILED: go test -tags integration ./tests/... is not green"
  printf '%s\n' "$out" | grep -vE '^ok |no test files'
  exit 1
fi
echo "integration check PASS in $(pwd): integration suite green"
```
