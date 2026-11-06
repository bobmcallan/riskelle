---
name: coder
description: Performer skill for in_progress (delivery and chore), worked by the interactive session in the item's own worktree. Estimate the plan with the released riskelle, implement the slice with unit and integration tests, attach ac-evidence, then commit, push, open the pull request, score the head and present approved. DEV ends at approved; a release run ships it.
---

# Coder (in_progress → integration → approved)

You are the **executor**: the interactive session, editing under the edit gate.
DEV ends at `approved`: a tested head, pushed, with an open pull request. A run
of the release task lands, tags, releases and closes it; never push to main,
tag, or present `done` yourself.

## 0. The released riskelle

riskelle scores its own items with the **installed, released** riskelle on the
PATH, never one built from the branch. It must be at least v0.19.0:

```bash
printf 'v0.19.0\n%s\n' "$(riskelle version | cut -d' ' -f2)" | sort -V -C && echo ok
# missing or older:
curl -fsSL https://github.com/bobmcallan/riskelle/releases/latest/download/install.sh | sh
```

## 1. Estimate the plan (delivery only, before presenting plan → in_progress)

`riskelle-risk-check` refuses the edge without the estimate and sends a High
back to be split. Literal paths under `/tmp/riskelle-<id>/`:

```bash
mkdir -p /tmp/riskelle-<id>
satelle story attachment <id> plan | sed -n '/^## riskelle plan/,/^## /p' \
  | sed -n '/^```json/,/^```/p' | sed '1d;$d' > /tmp/riskelle-<id>/plan.json
riskelle estimate /tmp/riskelle-<id>/plan.json > /tmp/riskelle-<id>/estimate.json
satelle story set <id> --add-tag "estimate-risk:$(jq .total /tmp/riskelle-<id>/estimate.json)" \
  --add-tag "estimate-risk-level:$(jq -r '.level|ascii_downcase' /tmp/riskelle-<id>/estimate.json)"
satelle story attach <id> --name risk-estimate --file /tmp/riskelle-<id>/estimate.json
satelle story set <id> --status in_progress
```

Remove stale `estimate-risk*` tags with `--remove-tag` before re-recording. Only
the developer may add `risk-waiver:high-estimate`.

## 2. Open the worktree and seat

```bash
git fetch origin
git worktree add ../riskelle-trees/<id> -b <id> origin/main
cd ../riskelle-trees/<id>
satelle story seat <id> --base origin/main
satelle story get <id>                 # body and criteria
satelle story attachment <id> plan     # the slice (delivery)
satelle story route <id>               # where it is, and every gate result
```

When a release run sent the item back, its `release-sendback` attachment says
why: rebase onto `origin/main` and come through again.

## 3. Implement the slice, and only that

- Sibling work belongs to its own item. If the slice grows past the plan's
  `## Size`, stop and say so: split, don't stretch.
- Serve the criteria literally; never lower one to converge. One that cannot be
  met is a reason to park the item (`blocked`) with the reason attached.
- Unit tests for every behavioural criterion, table-driven where the input has
  bands or edges; expected values come from the criterion, never the code.
- Integration tests for user-visible behaviour: a Go test under `tests/` with
  the `integration` build tag that builds the binary, creates a fixture git
  repository, runs the binary against it and asserts on its output.

## 4. Present integration

```bash
satelle story attach <id> --name ac-evidence --file - <<'EOF'
| AC | Proof (test path + name) | Divergence from plan |
| --- | --- | --- |
| 1 | pkg/foo: TestBar; tests: TestBarCLI | none |
EOF
satelle story set <id> --status integration
```

Exit 2 means a gate rejected it: fix what its notes name and present again.

## 5. Commit, push, open the PR, score, present approved

Stage exactly the paths `satelle story diff <id>` lists (never `git add -A`),
commit with a conventional subject ending in the item id, then:

```bash
git push -u origin <id>
gh pr create --base main --title "<title> (<id>)" --body "<id>: <criteria>"
mkdir -p /tmp/riskelle-<id>
riskelle score --base origin/main --head HEAD > /tmp/riskelle-<id>/actual.json
satelle story set <id> --add-tag "actual-risk:$(jq .total /tmp/riskelle-<id>/actual.json)" \
  --add-tag "actual-risk-level:$(jq -r '.level|ascii_downcase' /tmp/riskelle-<id>/actual.json)"
satelle story attach <id> --name risk-actual --file /tmp/riskelle-<id>/actual.json
satelle story set <id> --status approved
```

`integration → approved` runs the full suite, checks the pull request (pushed,
contains `origin/main`, title ends with the id) and the riskelle actual. A later
fix is a new commit: remove the old `actual-risk*` tags and score again. Then
release the seat: `satelle story release <id>`.
