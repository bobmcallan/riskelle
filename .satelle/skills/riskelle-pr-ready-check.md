---
name: riskelle-pr-ready-check
type: check
description: DEV's end (integration → approved). HEAD is committed and pushed, contains origin/main, and has an open, non-draft pull request against main whose head is HEAD and whose title ends with the item id. Approved automatically; no GitHub review is read. Deterministic, no agent.
---

# PR ready (DEV → CI/CD hand-off)

The coding gates judged the change and the integration check ran the full
suite on this HEAD. This check makes that run count for the release: the pull
request's head is HEAD, and HEAD already contains `origin/main`, so a release
run fast-forwards it without testing again. "Approved" means this check
accepted; the code review gate is the review.

```sh
set -u
for tool in gh jq git; do
  command -v "$tool" >/dev/null 2>&1 || { echo "$tool is not installed, so the pull request cannot be checked"; exit 1; }
done
if [ -n "$(git status --porcelain --untracked-files=no)" ]; then
  echo "the tree has uncommitted changes to tracked files; commit them, push, then present the move again"
  exit 1
fi
git fetch -q origin || { echo "git fetch origin failed"; exit 1; }
head=$(git rev-parse HEAD)
short=$(git rev-parse --short=12 HEAD)
if [ -z "$(git branch -r --contains "$head")" ]; then
  echo "HEAD $short is not pushed: git push -u origin <branch>"
  exit 1
fi
if ! git merge-base --is-ancestor origin/main "$head"; then
  echo "HEAD $short does not contain origin/main $(git rev-parse --short=12 origin/main): move the item back to in_progress, rebase onto origin/main and come through integration again"
  exit 1
fi
pr=$(gh pr list --state open --base main --json number,headRefOid,title,isDraft --limit 100 |
  jq -r --arg h "$head" --arg id "($SATELLE_ITEM)" '[.[] | select(.headRefOid == $h and .isDraft == false and (.title | endswith($id)))][0].number // empty')
if [ -z "$pr" ]; then
  echo "no open pull request against main has head $short and a title ending ($SATELLE_ITEM): gh pr create --base main --title \"<title> ($SATELLE_ITEM)\""
  exit 1
fi
echo "approved: PR #$pr ($short) open against main, contains origin/main $(git rev-parse --short=12 origin/main)"
```
