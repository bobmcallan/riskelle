---
name: riskelle-integration-review
scope: project
type: skill
tags: [type:skill, type:reviewer]
description: Gate on integration → merge (Claude). Isolated read-only reviewer judges whether the presented integration tests drive the built binary end to end against fixture git repositories and assert outcomes that would fail if the story regressed. riskelle-integration-check runs the suite; this judges adequacy only.
---

# Integration test review (integration → merge)

May this story merge, from a test-adequacy view? You get `{story, from, to}` on
stdin and read-only tools. Do not run tests; `riskelle-integration-check` does.

## Judge

1. Open the payload `docs` entry `integration-evidence`, and the tests it names
   under `tests/` (build tag `integration`). Missing evidence is a reject.
2. For each behavioural criterion: does a test build and run the binary as a
   user would, against a fixture git repository it creates, and assert on the
   real output (exit code, JSON fields, Markdown) so that it would fail if the
   behaviour regressed?
3. Where scoring is involved: does a test show the same input scores the same
   twice?
4. Open the payload `docs` entry `risk-actual`: riskelle's report on the
   committed story branch against main (the `riskelle-risk-review` line in the
   verdict notes puts it beside the estimate). Quote in your notes its total,
   level, and each breakdown entry's measure and points. When the level is
   High, the evidence must say why the slice could not be split smaller;
   reject a High with no reason.
5. Accept when every behavioural criterion is covered, or when the slice has no
   user-visible behaviour and the evidence says why.
6. Reject a missing, trivial (asserts only "no error"), self-confirming, or
   unrelated test, naming the criterion it leaves uncovered.

Coverage of the stated criteria is the bar, not your ideal suite.

A criterion that only a later step can make true (CI passing on `main`, a
pushed tag, a published release or Pages site) is not an integration-test gap:
the merge and release steps come after this gate, and the done review walks
every criterion after release.

## Verdict

```json
{"decision": "accept", "notes": "", "reviewed": "<exact words this verdict rests on>"}
```
