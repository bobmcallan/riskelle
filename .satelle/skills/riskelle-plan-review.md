---
name: riskelle-plan-review
type: skill
description: Gate on plan → in_progress. Isolated read-only reviewer judges the attached plan against the story's criteria, the repo and riskelle's constitution: premise checked, existing owner named, every AC planned with a proving test, sized to score Low, and a riskelle plan block present.
---

# Plan review (plan → in_progress)

May coding start on this plan? satelle hands you the item (title, body,
criteria) and its record (attachments, in a read-only folder under
`.satelle/runs/` named in your prompt). Read `plan` there, and the repo.
Read-only.

## Judge

1. **Premise.** `## Premise check` tests the body's claims against the repo, and
   the claims it relies on hold (spot-check them).
2. **Existing owner.** `## Existing owner` names where each rule, type, package
   or command already lives. Reject a plan that adds a second owner beside an
   existing one.
3. **Every criterion is planned.** Each AC has an `AC<n>` entry naming the files,
   the approach and a test that would fail without the change. A missing AC is a
   reject.
4. **Small.** `## Size` predicts a slice that scores Low on its own (roughly
   ≤ ~400 changed lines, ≤ ~10 files, one or two packages), or proposes the
   split. `## riskelle plan` holds one fenced `json` block with
   `"target": "main"` and every path with its expected added and removed lines.
5. **Constitution.** No network, LLM, randomness or wall clock in the scoring
   path; nothing lets a change lower its own score; no id4me dependency; where a
   change lands is context, never score.
6. **Decided.** Every design choice the implementer would face is made, with
   its reason.

Judge the criteria as written; add no requirement the story never stated.

## Verdict

```json
{"verdict": "accept", "notes": ""}
```

On a reject, `notes` names each gap and the AC or rule it breaks.
