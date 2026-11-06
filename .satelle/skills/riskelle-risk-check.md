---
name: riskelle-risk-check
type: check
description: riskelle scores its own stories. Entering in_progress needs the estimate tags and rejects a High estimate (split the story); entering any later state needs the actual tags and prints estimate beside actual. Never rejects on an overrun.
---

# riskelle risk check

The figures are story tags the driver writes from riskelle's JSON report:

- `estimate-risk:<total>` / `estimate-risk-level:<low|medium|high>`, from
  `riskelle estimate <plan.json>` (see the `coder` skill).
- `actual-risk:<total>` / `actual-risk-level:<low|medium|high>`, from
  `riskelle score --base origin/main --head HEAD` in the story's tree, before the
  branch lands (see the `coder` skill).

A total is a whole number 0-100. A High estimate is rejected unless the
developer (never the agent) has tagged the story `risk-waiver:high-estimate`.

```sh
set -u
tags=$(satelle story get "$SATELLE_ITEM" --json | jq -r '.tags[]?') || { echo "could not read $SATELLE_ITEM"; exit 1; }
tagval() { printf '%s\n' "$tags" | sed -n "s/^$1://p" | head -1; }
figure() {
  n=$(tagval "$1-risk"); lvl=$(tagval "$1-risk-level" | tr '[:upper:]' '[:lower:]')
  [ -n "$n" ] && [ -n "$lvl" ] || return 1
  case "$n" in *[!0-9]*) return 2;; esac
  [ "$n" -le 100 ] || return 2
  case "$lvl" in low|medium|high) return 0;; *) return 2;; esac
}

if [ "$SATELLE_TO" = in_progress ]; then
  figure estimate; rc=$?
  [ $rc -eq 1 ] && { echo "no riskelle estimate recorded: run riskelle estimate on the plan and tag estimate-risk:<total>, estimate-risk-level:<level> (see the coder skill)"; exit 1; }
  [ $rc -eq 2 ] && { echo "estimate tags are not a figure (0-100, low|medium|high): re-record them from riskelle estimate"; exit 1; }
  echo "riskelle estimate: $n ($lvl)"
  if [ "$lvl" = high ]; then
    if printf '%s\n' "$tags" | grep -qx 'risk-waiver:high-estimate'; then
      echo "high estimate waived by the developer (risk-waiver:high-estimate)"; exit 0
    fi
    echo "REJECT: the estimate is High: split the story into smaller ones and estimate each"; exit 1
  fi
  exit 0
fi

figure actual || { echo "no well-formed riskelle actual recorded: in the story's tree, before it lands, run riskelle score --base origin/main --head HEAD and tag actual-risk:<total>, actual-risk-level:<level> (see the coder skill)"; exit 1; }
act="actual: $n ($lvl)"
if figure estimate; then est="estimate: $n ($lvl)"; else est="estimate: none recorded"; fi
echo "riskelle risk: $est | $act"
```
