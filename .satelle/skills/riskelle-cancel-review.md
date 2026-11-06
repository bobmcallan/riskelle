---
name: riskelle-cancel-review
type: skill
description: Gate for cancel (any → cancelled). Isolated read-only reviewer requiring a recorded reason; a claim that the work was delivered elsewhere must name the delivering story or commit and cover this item's criteria; freeing a seat is never a reason.
---

# Cancel review

May this item be cancelled? satelle hands you the item and its record (a
read-only folder under `.satelle/runs/` named in your prompt). Cancelling is a
normal operator outcome, but it must be deliberate and on record. Judge; never
cancel anything yourself.

1. **A reason is on record** (body note or a `cancel-reason` attachment).
   None is a reject. Requirement withdrawn by the operator, a duplicate, or
   work no longer relevant are legitimate reasons.
2. **Delivered elsewhere?** The reason must name the delivering story id or
   commit, and that delivery must cover this item's numbered criteria (read the
   repo to check). A vague "covered elsewhere" or an unverifiable claim is a
   reject.
3. **Sibling bundling.** Reject a cancel claiming a sibling (same parent)
   delivered this item's work when the sibling's criteria do not cover it:
   that is a scope breach, not a supersede. Name the ids and the uncovered
   criteria.
4. **Seat contention is never a reason.** Freeing a seat or making way for
   other work is parking (`blocked`), not cancelling.

Cancelled is terminal: revival is a new item.

## Verdict

```json
{"verdict": "accept", "notes": ""}
```
