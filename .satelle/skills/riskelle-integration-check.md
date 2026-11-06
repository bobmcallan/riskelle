---
name: riskelle-integration-check
type: check
description: The full local suite, once, on the edge to approved (integration → approved, for a story and a chore alike): the unit suite and the integration suite. This is the run the release trusts. Deterministic, no agent.
---

# Integration check (the full suite, once, before approved)

Runs the unit suite and the integration suite (`-tags integration`, under
`tests/`) on the head that becomes the pull request's head. The release run
fast-forwards this tested head without testing again.

```sh
set -u
out=$(go test -count=1 ./... 2>&1) || { echo "unit suite failed"; printf '%s\n' "$out" | grep -vE '^ok |no test files' | tail -60; exit 1; }
out=$(go test -count=1 -tags integration ./... 2>&1) || { echo "integration suite failed"; printf '%s\n' "$out" | grep -vE '^ok |no test files' | tail -60; exit 1; }
echo "full suite green (unit and integration) on $(git rev-parse --short=12 HEAD)"
```
