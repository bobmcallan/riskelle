---
name: integrate
scope: project
type: skill
tags: [type:skill]
evidence_commands: go test
description: In-loop executor rubric for riskelle's integration step. The driving session writes or updates integration tests that drive the built binary end to end against fixture git repositories, runs them, and attaches the evidence, then commits the slice on its story branch and records the installed riskelle CLI's score of that branch against main for the Claude integration reviewer. Never merges, pushes or advances status.
---

# Integrate (in-loop, integration)

The code panel accepted the slice. Now prove it works end to end, in the story's
worktree.

## What an integration test is here

A Go test under `tests/` with the `integration` build tag that builds the
binary, creates a fixture git repository (commits on a base and a branch),
runs the binary against it as a user would, and asserts on its output (exit
code, JSON report, Markdown). It must fail if the story's behaviour regressed.
A slice with no user-visible behaviour (pure refactor, docs, CI config) may
have none: say so and why in the evidence.

## Do

1. Add or update the integration tests for each behavioural criterion.
2. Run them:

   ```bash
   go test -tags integration ./tests/...
   ```

3. Repair only fallout from this story's own change. A design flaw found here
   goes back to `in_progress` (request it), not a redesign in this step.
4. Attach the evidence:

   ```bash
   satelle story attach <id> --name integration-evidence --type output --file - <<'EOF'
   | AC | Integration test | What it drives and asserts |
   | --- | --- | --- |
   EOF
   ```

5. **Commit the slice and score it.** riskelle scores commits, so the slice is
   committed here, on `story/<id>` (not pushed): enumerate it with
   `satelle story diff <id>`, stage exactly those paths (never `git add -A`),
   and commit with a conventional subject ending in the story id. Then score
   the branch against main with the installed riskelle CLI, checked as in
   code.md (at least v0.19.0, installed with the release one-liner when
   missing or older), and record it (`riskelle-risk-review` refuses
   `integration → merge` without it). The report goes under
   `/tmp/riskelle-<id>/` with a literal path, as in code.md:

   ```bash
   printf 'v0.19.0\n%s\n' "$(riskelle version | cut -d' ' -f2)" | sort -V -C && echo ok
   git fetch origin
   mkdir -p /tmp/riskelle-<id>
   riskelle score --base origin/main --head HEAD > /tmp/riskelle-<id>/actual.json
   satelle story set <id> --remove-tags actual-risk:,actual-risk-level:
   satelle story set <id> --add-tags "actual-risk:$(jq .total /tmp/riskelle-<id>/actual.json),actual-risk-level:$(jq -r '.level|ascii_downcase' /tmp/riskelle-<id>/actual.json)"
   satelle story attach <id> --name risk-actual --type output --file /tmp/riskelle-<id>/actual.json
   ```

   A later fix in this step is a new commit; score again after it.
6. Stop. Request `integration → merge`. `riskelle-integration-check` runs the
   suite, then a Claude reviewer judges the tests and the riskelle score.
