---
name: task-run
description: Performer skill for a task run's in_progress state (the runbook workflow). Perform the execution's declared ACTION and attach the VERIFICATION evidence; the validate-after gate judges the close.
---

# Task run (in_progress)

You are the **executor** for a task **execution**. Its parent task carries the
ACTION and how success is VERIFIED.

1. Read the execution and its task: `satelle execution get <exe>`,
   `satelle task get <task>`.
2. Carry out the **ACTION** exactly, with no scope beyond it. When the task
   names a skill (the release task names `.satelle/skills/release.md`), follow
   that skill.
3. Attach the **VERIFICATION** evidence the task asks for
   (`satelle execution attach <exe> --name <name> --file -`).
4. Present `satelle execution set <exe> --status done`; validate-after judges
   it. A re-run is a new execution, never a reopen.
