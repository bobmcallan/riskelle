---
name: riskelle-blocked-review
type: skill
description: Gate for parking an item (any → blocked). Isolated read-only reviewer allowing the move when a reason is recorded (a dependency, the world not ready, or preemption) and refusing a bare block with no reason on record.
---

# Blocked review (park gate)

May this item be parked? satelle hands you the item and its record (a
read-only folder under `.satelle/runs/` named in your prompt). Parking is for
work that cannot proceed yet, or must yield to higher-priority work; the
criteria stay as they are and the item resumes later.

- **Accept** when a reason is on record: a note in the body, an attachment
  (e.g. `blocked-reason`), naming the dependency (`blocked-by:<id>`), the
  missing capability, or the work that preempts it. The bar is low: a clear,
  human-readable reason is enough.
- **Reject** a bare block with nothing explaining why; ask for a one-line
  reason.

## Verdict

```json
{"verdict": "accept", "notes": ""}
```
