---
name: riskelle-code-review
scope: project
type: skill
tags: [type:skill, type:reviewer]
description: Gate on in_progress → integration, run on both panel seats (Claude and GPT). Isolated read-only reviewer judges the story's slice against its numbered acceptance criteria, the executor's per-AC test evidence, proof honesty, and riskelle's constitution (deterministic scoring, small slices, no id4me dependency). Verdict only; never runs the suite.
---

# Code review (in_progress → integration)

May this slice advance to integration? You are one seat of a two-seat panel;
judge independently. You get `{story, from, to}` on stdin and read-only tools.

## The slice

Use `payload.diff` when present: open `diff.patch_path` (JSON with `files`,
`stat`, `patch`). Judge those paths. Open the payload `docs` entries `plan` and
`ac-evidence`. With no diff, read the tree.

## Judge

1. **Each criterion is met.** Walk the numbered ACs; the code in the slice
   implements each, not stubbed or TODO. For a "never" or "always" criterion,
   find every place that could break it and check each one; a compliant sample
   is not proof.
2. **The evidence holds.** Each test named in `ac-evidence` exists and would
   fail if the behaviour were absent. A test whose expected value comes from
   the code under test, or that asserts only on data it built, proves nothing.
   A missing `ac-evidence` attachment is a reject.
3. **The constitution holds.** Reject, citing file:line, when the slice:
   - puts a network call, an LLM, randomness, or the wall clock into the
     scoring path (collect or score), or makes output order depend on map
     iteration;
   - lets a change's own head influence the rules that score it;
   - imports or names id4me;
   - scores where a change lands (auth, migrations, infrastructure, secrets).
4. **It is small and focused.** Reject a slice that carries work belonging to
   another story, or that grew well past the plan's `## Size` without saying
   why.
5. **It is clean.** Avoidable duplication, dead code, and errors that are
   swallowed are reject reasons; style preferences are not.

Judge the ACs as written; add no requirement the story never stated.

**Criteria that only a later step can make true.** Some criteria describe a fact
that cannot exist at this gate: CI passing on `main`, a pushed tag, a published
release or Pages site. The merge and release steps come after this one. For
those, judge whether the slice puts in place what will make the fact true (the
workflow file, the trigger, the steps it runs) and whatever pre-merge evidence
there is (a green run of the same commit on the story branch). Do not reject
because the fact is not yet true: the done review walks every criterion after
release and rejects the close if it is not.

## Verdict

```json
{"decision": "accept", "notes": "", "reviewed": "<exact words this verdict rests on>"}
```

On a reject, `notes` names each gap and the AC or rule it breaks, so the
executor can fix it without guessing.
