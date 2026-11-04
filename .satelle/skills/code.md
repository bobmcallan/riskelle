---
name: code
scope: project
type: skill
tags: [type:skill]
evidence_commands: go test
description: In-loop executor rubric for riskelle's in_progress step. The driving Claude session implements the accepted plan in the story's own worktree, with unit tests, leaves the tree gofmt/vet clean, attaches per-AC evidence, and stops for the Claude + GPT code-review panel. Never commits, merges or advances status.
---

# Code (in-loop, in_progress)

You are the driving session performing the `in_progress` step. Work happens in
the story's **own worktree**, never in the main tree.

## Before requesting `plan → in_progress`: the riskelle estimate

riskelle scores its own stories with the **installed, released** riskelle CLI
on the PATH (the one developers and satelle use), never one built from the
story's branch. First check it is at least the pinned release, v0.19.0:

```bash
riskelle version   # prints "riskelle vX.Y.Z (commit …)"
printf 'v0.19.0\n%s\n' "$(riskelle version | cut -d' ' -f2)" | sort -V -C && echo ok
```

When `riskelle` is missing or the check prints nothing, install the latest
release (it lands in `~/.local/bin`) and check again:

```bash
curl -fsSL https://github.com/bobmcallan/riskelle/releases/latest/download/install.sh | sh
```

Then estimate the accepted plan's `## riskelle plan` block and record it
(`riskelle-risk-review` refuses the edge without it, and sends a High back to
be split). The files go under `/tmp/riskelle-<id>/`, written with literal paths:
satelle's hook refuses a write into the repository or its git directory at the
plan step, and cannot resolve a path held in a shell variable.

```bash
mkdir -p /tmp/riskelle-<id>
satelle story doc <id> plan | jq -r .body | sed -n '/^## riskelle plan/,/^## /p' \
  | sed -n '/^```json/,/^```/p' | sed '1d;$d' > /tmp/riskelle-<id>/plan.json
riskelle estimate /tmp/riskelle-<id>/plan.json > /tmp/riskelle-<id>/estimate.json
satelle story set <id> --remove-tags estimate-risk:,estimate-risk-level:
satelle story set <id> --add-tags "estimate-risk:$(jq .total /tmp/riskelle-<id>/estimate.json),estimate-risk-level:$(jq -r '.level|ascii_downcase' /tmp/riskelle-<id>/estimate.json)"
satelle story attach <id> --name risk-estimate --type output --file /tmp/riskelle-<id>/estimate.json
```

Run the estimate from the story's worktree, so its target (`main`) is the
trunk the story was cut from. Raising the pinned minimum is a substrate story
that changes it here and in integrate.md.

## Before the first edit

1. Be in the story's worktree. It is opened before the story is engaged (see the
   constitution, "Where work happens"); `git rev-parse --abbrev-ref HEAD` must
   print `story/<id>`.
2. Pull the plan and record that you are following it:

   ```bash
   satelle story doc <id> plan
   satelle story log <id> --kind plan-consumed --data steps="<short list>"
   ```

## Implement

- Only the plan's slice. Sibling work belongs to its own story.
- Unit tests for every behavioural criterion, table-driven where the input space
  has bands or edges. A test's expected value comes from the criterion, never
  from the code under test.
- Keep it small. If the slice grows past the plan's `## Size`, stop and say so:
  the story should be split, not stretched.

## Hand-off

1. Format, vet, build and test must be clean (the entry gate re-runs them):

   ```bash
   gofmt -l .        # no output
   go vet ./...
   go build ./...
   go test ./...
   ```

2. Attach the per-AC evidence: for each criterion, the test that proves it and
   any divergence from the plan.

   ```bash
   satelle story attach <id> --name ac-evidence --type output --file - <<'EOF'
   | AC | Realised test | Divergence from plan |
   | --- | --- | --- |
   | 1 | pkg/foo: TestBar | none |
   EOF
   ```

3. Stop. Request `in_progress → integration`; the panel decides.

Do not commit, merge or push here: the integration step commits the slice and
the merge step lands it.
