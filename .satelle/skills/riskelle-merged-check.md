---
name: riskelle-merged-check
scope: project
type: skill
tags: [type:skill, type:reviewer, type:functional-check]
description: Functional-check gate on entry to release. Fetches origin and passes only when origin/main carries a commit whose subject ends in the story id and the story branch story/<id> is an ancestor of origin/main, so no tag is cut for work that has not landed. Deterministic, zero LLM tokens.
---

# Merged check (entry to release)

The merge step published the story to `main`. Confirm it before a version is
tagged: the story branch's head must be contained in `origin/main`.

```check
#!/usr/bin/env bash
set -uo pipefail
payload=$(cat)
sid=$(printf '%s' "$payload" | grep -oE 'sty_[a-f0-9]{8}' | head -1)
if [ -z "$sid" ]; then echo "MERGED CHECK FAILED: no story id in the payload"; exit 1; fi
branch="story/$sid"

git fetch --quiet origin 2>&1 || { echo "MERGED CHECK FAILED: git fetch origin failed"; exit 1; }
head=$(git rev-parse --verify --quiet "refs/heads/$branch") || { echo "MERGED CHECK FAILED: no local branch $branch"; exit 1; }

# Capture the log first: piping it into `grep -q` lets grep exit early, git log
# then dies of SIGPIPE and pipefail fails the test even when the commit is there.
subjects=$(git log --format=%s origin/main)
if ! grep -qF "($sid)" <<< "$subjects"; then
  echo "MERGED CHECK FAILED: no commit whose subject ends in ($sid) on origin/main"
  exit 1
fi

if ! git merge-base --is-ancestor "$head" origin/main; then
  echo "MERGED CHECK FAILED: $branch ($head) is not on origin/main; run the merge step's publish"
  exit 1
fi
echo "merged check PASS: $branch $head is on origin/main $(git rev-parse --short origin/main)"
```
