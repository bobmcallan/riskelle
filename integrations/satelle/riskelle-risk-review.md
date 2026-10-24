---
name: riskelle-risk-review
scope: project
type: skill
tags: [type:skill, type:reviewer, type:functional-check]
description: CODED gate for a satelle repo that scores its changes with riskelle. Entering in_progress requires the story's riskelle estimate tags and sends a High estimate back to be split (the one check a developer can waive, high-estimate). Any later edge it is wired on requires the actual riskelle score tags and prints the estimate and actual side by side; it never rejects on an overrun.
---

# riskelle risk gate (coded functional check)

Install it beside satelle's own `satelle-estimate-actual-review`, not instead of
it: satelle's gate judges the token estimate, this one the riskelle risk. The
check below IS the decision: deterministic shell, no LLM, no network, and it
needs neither satelle nor riskelle on the PATH. It reads `{story, from, to,
review_skill, ..., waivers}` on stdin; the edge is the `to` marshalled just
before `review_skill`, never a message's `to`.

The figures are story TAGS the driver writes from riskelle's JSON report (see
the riskelle README, "With satelle"):

- `estimate-risk:<total>` / `estimate-risk-level:<low|medium|high>`, from
  `riskelle estimate <plan.json>` at planning.
- `actual-risk:<total>` / `actual-risk-level:<low|medium|high>`, from
  `riskelle score --base <target> --head HEAD` in the story's tree, before the
  branch lands on the target (after it lands, the diff is empty).

A total is a whole number 0-100; a level is matched case-insensitively.

## Entering `in_progress`

- Both estimate tags must be present and well formed, or the edge is rejected
  with the commands to run. satelle's own `estimate-*` token tags do not count.
- A `high` estimate is rejected: split the story into smaller ones and estimate
  each.
- Waiver: the High rejection is the only check a developer can waive, as check
  `high-estimate` (`satelle story waive <id> --gate riskelle-risk-review
  --check high-estimate --reason "..."`). satelle hands the gate its own
  waivers on stdin; when one names `high-estimate`, the High estimate is
  accepted and the waiver id and reason are printed into the verdict notes.
  Nothing else is waivable.
- On accept it prints `riskelle estimate: <n> (<level>)`.

## Entering any other state it is wired on

- Both actual tags must be present and well formed, or the edge is rejected
  with the `riskelle score` command to run.
- On accept it prints one line:
  `riskelle risk — estimate: <n> (<level>) | actual: <n> (<level>)`, with
  `estimate: none recorded` when there is no estimate. The engine keeps a
  passing check's stdout in the verdict notes, so later reviewers see it.
- It never rejects on the difference between estimate and actual.

```check
# Coded riskelle risk gate. Reads {story, from, to, review_skill, ...,
# waivers} on stdin; exit 0 accepts, non-zero rejects with the reason on stdout.
IN=$(cat)
# The transition's own to: satelle always marshals review_skill straight after
# it, while later objects (messages) carry "to" keys of their own.
to=$(printf '%s' "$IN" | grep -o '"to":"[^"]*","review_skill":' | head -1 | cut -d'"' -f4)

# The story's own tags array (the story is the first object in the payload),
# so text quoted in a doc body or verdict note is never read as a tag.
TAGS=$(printf '%s' "$IN" | grep -o '"tags":\[[^]]*\]' | head -1)

# tagval <tag-name> — the value after "<tag-name>:", empty + failure when absent.
tagval() {
  hit=$(printf '%s' "$TAGS" | grep -o "\"$1:[^\"]*\"" | head -1)
  [ -n "$hit" ] || return 1
  hit=${hit#\"$1:}
  printf '%s' "${hit%\"}"
}

# figure <prefix> — sets n and lvl from <prefix>-risk and <prefix>-risk-level;
# returns 1 when either is absent, 2 when either is not a figure.
figure() {
  n=$(tagval "$1-risk") || return 1
  lvl=$(tagval "$1-risk-level") || return 1
  lvl=$(printf '%s' "$lvl" | tr '[:upper:]' '[:lower:]')
  case "$n" in ''|*[!0-9]*) return 2;; esac
  [ "$n" -le 100 ] || return 2
  case "$lvl" in low|medium|high) ;; *) return 2;; esac
  return 0
}

case "$to" in
 in_progress)
  figure estimate; rc=$?
  if [ "$rc" -eq 1 ]; then
   echo "no riskelle estimate recorded — run: riskelle estimate plan.json > risk-estimate.json, then satelle story set <id> --add-tags \"estimate-risk:<total>,estimate-risk-level:<level>\" from its total and level, then retry the edge; riskelle-risk-review enforces it"
   exit 1
  fi
  if [ "$rc" -eq 2 ]; then
   echo "riskelle estimate tags are not a figure (estimate-risk must be a whole number 0-100, estimate-risk-level low, medium or high) — re-record them from riskelle estimate's report, then retry the edge"
   exit 1
  fi
  echo "riskelle estimate: $n ($lvl)"
  if [ "$lvl" = high ]; then
   w=$(printf '%s' "$IN" | grep -q '"waivers":\[' && printf '%s' "$IN" \
     | grep -oE '\{"id":"[^"]*","gate":"riskelle-risk-review","check":"high-estimate","reason":"([^"\\]|\\.)*"' | head -1)
   if [ -n "$w" ]; then
    wid=$(printf '%s' "$w" | sed 's/^{"id":"\([^"]*\)".*/\1/')
    why=$(printf '%s' "$w" | sed 's/.*"reason":"\(.*\)"$/\1/')
    echo "high estimate waived by developer: $wid — $why"
    exit 0
   fi
   echo "REJECT: the riskelle estimate is High — split the story into smaller ones and estimate each, then retry the edge (or a developer waives check high-estimate)"
   exit 1
  fi
  exit 0;;
 *)
  figure actual; rc=$?
  if [ "$rc" -ne 0 ]; then
   echo "no well-formed riskelle actual recorded — run in the story's tree, before the branch lands: riskelle score --base <target> --head HEAD > risk-actual.json, then satelle story set <id> --add-tags \"actual-risk:<total>,actual-risk-level:<level>\" from its total and level (actual-risk a whole number 0-100), then retry the edge; riskelle-risk-review enforces it"
   exit 1
  fi
  act="actual: $n ($lvl)"
  if figure estimate; then est="estimate: $n ($lvl)"; else est="estimate: none recorded"; fi
  echo "riskelle risk — $est | $act"
  exit 0;;
esac
```
