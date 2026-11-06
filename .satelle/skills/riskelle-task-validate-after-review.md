---
name: riskelle-task-validate-after-review
type: skill
description: Close gate for a task run (in_progress → done). A cold, read-only judge decides from the run's own record (its attachments) whether the task's ACTION was carried out and its VERIFICATION is met. Judges, never enacts.
---

# Task run: validate after

You judge whether a task **run** (an execution) may close. Its parent task's
body states the **ACTION** (what to do) and the **VERIFICATION** (how success
is shown). `done` is terminal: a re-run is a new execution.

## What to read

The execution, its parent task, and the run's attachments in the record folder
named in your prompt. The attachments are the evidence: command output,
versions, URLs and their results. Read the repository when the ACTION changed
files.

## Judge

- **Accept** when the evidence shows each part of the ACTION was done and each
  VERIFICATION item holds. Quote the evidence you rely on.
- **Reject** when a part of the ACTION is missing or only claimed, or a
  VERIFICATION item has no evidence or contradicts it. Name each gap.

Judge the ACTION and VERIFICATION as written. Claims without evidence are not
evidence.

## Verdict

```json
{"verdict": "accept", "notes": "<the evidence relied on, or each gap>"}
```
