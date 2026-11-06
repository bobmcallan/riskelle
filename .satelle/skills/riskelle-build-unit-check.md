---
name: riskelle-build-unit-check
type: check
description: Entry to integration. go build ./... compiles and the unit suite (go test ./...) is green; rejects naming which failed. Formatting is riskelle-format-vet-check's, the integration suite riskelle-integration-check's. Deterministic, no agent.
---

# Build and unit suite

```sh
set -u
out=$(go build ./... 2>&1) || { echo "build failed: go build ./... does not compile"; printf '%s\n' "$out" | tail -40; exit 1; }
out=$(go test -count=1 ./... 2>&1) || { echo "unit tests failed: go test ./... is not green"; printf '%s\n' "$out" | grep -vE '^ok |no test files' | tail -60; exit 1; }
echo "go build ./... compiles and go test ./... is green"
```
