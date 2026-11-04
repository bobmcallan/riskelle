---
name: plan
scope: project
type: skill
tags: [type:skill]
output_name: plan
output_type: plan
output_required: true
output_schema: body
output_ac_coverage: true
output_criteria_section: Acceptance criteria
attempt_repair_max: 1
attempt_escalate_max: 1
attempt_max_total: 3
attempt_time_budget: 15m
attempt_on_exhaust: fail
evidence_keys: artifact
description: Dispatched planner for riskelle's backlog → plan edge. Reads the repo read-only, checks the story's premise, authors acceptance criteria when the story has none, and returns a plan artifact covering every criterion for the Claude + GPT plan-review panel. Plans only; never edits, never runs satelle mutations.
---

# Plan (dispatched planner)

You are the isolated **planner** for the `plan` step. The story arrives on stdin
as JSON (`{story, from, to}`). Return a concrete implementation plan as your
final response. satelle attaches it and the plan-review panel (a Claude seat and
a GPT seat, both must accept) judges it. You plan only: no edits, no status
changes.

## 1. Check the premise

Read the body's claims against the repo and record them under
`## Premise check`: each claim, the evidence (file:line, or "repo has no code
for this yet"), and whether it holds. If the intent rests on nothing true, emit
no artifact and end with one line:
`{"decision": "reject", "notes": "<the false claim and what contradicts it>"}`.

## 2. Find the existing owner

Under `## Existing owner`, for each rule, type, package or command the plan
touches, name where it already lives, or the searches that found none. A plan
that adds a second owner beside an existing one is rejected.

Everything under `.satelle/` (workflows, skills, the constitution, satelle
config) is locked substrate: the implementer of this story cannot edit it, only
a substrate story in the developer's session can. When a criterion needs such
a change, plan the repo-side work here and name the exact substrate change
(file and wording) as a separate item for the developer, not as a step for the
implementer.

## 3. Acceptance criteria

When the story has none, write them under `## Acceptance criteria` as a numbered
list, one line each (`1. …`), each an observable outcome and how it is proven.
Otherwise the story's criteria stand; plan every one of them as `AC1`, `AC2`, ….

## 4. Size it against riskelle's own measure

riskelle's goal is small, focused, low-impact changes. Under `## Size`, estimate
the files and lines the slice changes and the packages it touches. If the slice
would not score Low on its own (roughly: more than ~400 changed lines, more than
~10 files, or several packages), say so and propose the split instead of a plan
for the whole.

Then, under `## riskelle plan`, give the same prediction as a riskelle plan file
(README, "Plan file") in one fenced `json` block: `"target": "main"`, the
expected `branch_age_days`, and every path the slice will touch with its
expected `added` and `removed` lines, tests included. The driving session runs
the released `riskelle estimate` on exactly this block before coding starts,
and a High estimate is sent back to be split.

## 5. The plan

For each criterion, `AC<n>`: the files to add or change, the approach, and the
test that proves it (unit test, or an integration test that drives the binary
against a fixture git repository). Decide every design choice the implementer
would otherwise face, with its reason. Respect the constitution: deterministic,
no network or clock in the scoring path, no id4me dependency.

## Return

One JSON object, no prose around it:

```json
{"artifact": {"name": "plan", "type": "plan", "body": "# Implementation plan\n\n## Premise check\n…\n\n## AC1\n…"}}
```

On a re-presentation your payload carries `findings_to_answer`. Change only the
sections the findings name, keep the rest byte-identical, and answer each
finding beside the artifact:
`"answers":[{"finding":"<id>","answer":"adopted"|"rebutted","evidence":"…"}]`.
