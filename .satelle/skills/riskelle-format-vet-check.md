---
name: riskelle-format-vet-check
type: check
description: Rejects when gofmt lists a Go file, go vet fails (integration-tagged tests included), or the windows cross-build fails. Deterministic, no agent.
---

# Format, vet and cross-build

riskelle ships Linux, macOS and Windows binaries, so the Windows cross-build is
checked here, before a release run trusts the head.

```sh
set -u
dirty=$(gofmt -l $(git ls-files '*.go') 2>&1)
[ -z "$dirty" ] || { echo "gofmt dirty files (run gofmt -w on them):"; printf '%s\n' "$dirty"; exit 1; }
out=$(go vet ./... 2>&1) || { echo "go vet failed:"; printf '%s\n' "$out" | tail -40; exit 1; }
out=$(go vet -tags integration ./... 2>&1) || { echo "go vet -tags integration failed (integration tests do not compile):"; printf '%s\n' "$out" | tail -40; exit 1; }
out=$(GOOS=windows go build ./... 2>&1) || { echo "windows cross-build failed:"; printf '%s\n' "$out" | tail -40; exit 1; }
echo "gofmt, go vet (with integration tests) and the windows cross-build clean"
```
