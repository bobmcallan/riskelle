---
name: riskelle-build-check
scope: project
type: skill
tags: [type:skill, type:reviewer, type:functional-check]
description: Functional-check gate on entry to integration. In the story's worktree, requires gofmt-clean Go files, a clean go vet, a compiling tree (integration-tagged tests included) and a green unit suite. Rejects naming the step that failed. Deterministic, zero LLM tokens.
---

# Build check (entry to integration)

Deterministic gate beside the Claude + GPT code panel, so a reviewer never
spends tokens on a tree that does not build. It runs in the story's worktree
(branch `story/<id>`), falling back to the current directory.

```check
#!/usr/bin/env bash
set -uo pipefail
payload=$(cat)
sid=$(printf '%s' "$payload" | grep -oE 'sty_[a-f0-9]{8}' | head -1)
tree=$(git worktree list --porcelain 2>/dev/null | awk -v b="refs/heads/story/$sid" '/^worktree /{w=$2} $0=="branch "b{print w}')
cd "${tree:-.}" || { echo "BUILD CHECK FAILED: cannot enter $tree"; exit 1; }

if [ ! -f go.mod ]; then
  echo "BUILD CHECK FAILED: no go.mod in $(pwd)"
  exit 1
fi

fmt=$(gofmt -l $(git ls-files '*.go') 2>&1)
if [ -n "$fmt" ]; then
  echo "BUILD CHECK FAILED (format): gofmt -l lists files"
  printf '%s\n' "$fmt"
  exit 1
fi

out=$(go vet ./... 2>&1) || { echo "BUILD CHECK FAILED (vet)"; printf '%s\n' "$out"; exit 1; }
out=$(go build ./... 2>&1) || { echo "BUILD CHECK FAILED (build)"; printf '%s\n' "$out"; exit 1; }
out=$(go vet -tags integration ./... 2>&1) || { echo "BUILD CHECK FAILED (integration tests do not compile)"; printf '%s\n' "$out"; exit 1; }
out=$(go test ./... 2>&1) || { echo "BUILD CHECK FAILED (unit tests)"; printf '%s\n' "$out" | grep -vE '^ok |no test files'; exit 1; }

echo "build check PASS in $(pwd): gofmt clean, vet clean, builds, unit tests green"
```
