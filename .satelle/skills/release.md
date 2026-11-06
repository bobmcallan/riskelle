---
name: release
description: CI/CD — the procedure a run of the release task (the task tagged release) follows over the approved items it took: fast-forward main to each tested pull-request head without re-testing, send back any head that no longer fast-forwards, tag the next version once, wait for release.yml, install the published build here, check it, only then move each item to done, and clear the finished worktrees. Never forces, never merges or rebases main.
---

# Release (CI/CD)

DEV ends at `approved`: an open pull request whose head contains `origin/main`
and passed the full local suite on exactly that tree. A release run does not
test again: it ships only heads that fast-forward `main`, which are the trees
that were tested. A head that no longer fast-forwards goes back through the
loop with the reason recorded on the item.

Items reach `done` only through a run: the `release-run` gate refuses any item
no in-progress run lists. Any step that fails stops the run: record the reason
in the run's `release-evidence`, leave the items as they are and end the turn.

Never: run a test suite, merge or rebase onto main, force-push, move or delete
a pushed tag, take an item not in the run's inputs, or close an item before
step 6 passes.

## 0. Start and read the run

```sh
git fetch -q origin --tags
task=$(satelle task list --tag release --json | jq -r '.[0].id')
exe=$(satelle task run "$task" --base origin/main --json | jq -r .id)
satelle execution set "$exe" --status in_progress                # validate-before
satelle execution seat "$exe"
satelle ledger list "$exe" --kind task.run --json | jq -r '.[0].payload.inputs[]'
```

For each input item find its pull request, the open PR against main whose
title ends `(<id>)` (`gh pr list --state open --base main --json number,headRefOid,title`),
and record `<id> PR #<n> head <sha>`. An item with no open pull request goes
on the send-back list.

## 1. Order and fast-forward, in git only

Sort the items by `git rev-list --count origin/main..<head>`, fewest first.
Start with `tip=origin/main`. For each item:

- its head is already on main (`git merge-base --is-ancestor <head> origin/main`): keep it (a resumed run);
- else `git merge-base --is-ancestor $tip <head>`: keep it, `tip=<head>`;
- else send it back: "head <sha> does not contain main <tip> after <earlier ids>; rebase onto origin/main and come through integration again".

Send back each item on the list; it reopens:

```sh
printf '%s\n' "<reason>" | satelle story attach <id> --name release-sendback --file -
satelle story set <id> --status in_progress
```

If no item is kept, stop: "nothing fast-forwards".

## 2. Push main once

```sh
git push origin "$tip:refs/heads/main"   # plain: refused unless it fast-forwards
```

GitHub marks each pull request merged, because main now contains its head.

## 3. Tag once

Latest tag: `git tag -l 'v[0-9]*' | sed 's/^v//' | sort -V | tail -1`. If it
already points at `$tip` (a resumed run), keep it. Otherwise bump: a `feature`
item in the run bumps the minor version, else the patch (before 1.0).

```sh
git tag -a "v<ver>" "$tip" -m "v<ver> (<exe>: <ids>)"
git push origin "v<ver>"      # refused if the tag exists: take the next one
```

## 4. Watch the release

The tag triggers `release.yml` (test, archives, checksums, GitHub Release,
Pages).

```sh
gh run list --workflow release.yml --branch "v<ver>" --limit 1 --json databaseId,url
gh run watch <id> --exit-status
gh release view "v<ver>" --json tagName,assets
curl -fsS -o /dev/null -w '%{http_code}\n' https://bobmcallan.github.io/riskelle/
```

A failed run stops the release; fix forward with a new item and a new patch.

## 5. Install here

```sh
curl -fsSL https://github.com/bobmcallan/riskelle/releases/latest/download/install.sh | sh
```

## 6. Check

`riskelle version` names `v<ver>` and origin/main's commit (the
release-installed gate's pattern). A mismatch stops the run.

## 7. Close

For each released item: `satelle story set <id> --status done` (its gates:
release-run, release-published, release-installed, judged while the run is in
progress). Then attach the evidence to the run:

```sh
satelle execution attach "$exe" --name release-evidence --file - <<'EOF'
<per item: id, PR, head sha, released or sent back with the reason>
<main before and after; tag v<ver>; release.yml run URL and conclusion>
<assets; Pages HTTP status; riskelle version line; each item's final status>
EOF
satelle execution set "$exe" --status done      # validate-after
satelle execution release "$exe"
```

## 8. Clear the finished worktrees

From the main checkout: for each released item, `git worktree remove
../riskelle-trees/<id>`, `git branch -d <id>`, `git push origin --delete <id>`;
then `git switch main && git merge --ff-only origin/main`. Never remove a
worktree with uncommitted or unpushed work: report it instead.
