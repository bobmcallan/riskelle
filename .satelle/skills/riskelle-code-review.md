---
name: riskelle-code-review
type: skill
description: Gate on in_progress → integration. Isolated read-only reviewer judges the slice against its acceptance criteria, the per-AC test evidence, proof honesty and riskelle's constitution. Verdict only; riskelle-build-check runs the suite.
---

# Code review (in_progress → integration)

May this slice advance to integration? satelle hands you the item, the change
it judges, and its record (`plan`, `ac-evidence`, in a read-only folder under
`.satelle/runs/` named in your prompt). Read-only.

## Judge

1. **Each criterion is met.** Walk the numbered ACs; the slice implements each,
   not stubbed or TODO. For a "never" or "always" criterion, check every place
   that could break it; a compliant sample is not proof.
2. **The evidence holds.** Each test named in `ac-evidence` exists and would
   fail if the behaviour were absent. A test whose expected value comes from the
   code under test, or that asserts only on data it built, proves nothing. A
   missing `ac-evidence` attachment is a reject.
3. **The constitution holds.** Reject, citing file:line, when the slice:
   - puts a network call, an LLM, randomness or the wall clock into the scoring
     path (collect or score), or makes output order depend on map iteration;
   - lets a change's own head influence the rules that score it;
   - imports or names id4me;
   - scores where a change lands (auth, migrations, infrastructure, secrets).
4. **Small and focused.** Reject a slice carrying another story's work, or one
   well past the plan's `## Size` without saying why.
5. **Clean.** Avoidable duplication, dead code and swallowed errors are reject
   reasons; style preferences are not.

Judge the ACs as written; add no requirement the story never stated.

A criterion only a later step can make true (CI green on `main`, a pushed tag,
a published release or Pages site) is judged by whether the slice puts in place
what will make it true; the release run makes it true.

## Verdict

```json
{"verdict": "accept", "notes": ""}
```

On a reject, `notes` names each gap and the AC or rule it breaks.
