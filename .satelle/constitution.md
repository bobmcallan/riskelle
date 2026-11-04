---
type: constitution
title: riskelle constitution
description: Order-zero context for riskelle — the goal, the scoring invariants, and how work is sized here.
---

# riskelle constitution

- **What this repo is:** riskelle, a standalone, deterministic tool that scores
  the risk of a code change. One Go binary that runs the same locally, in
  GitHub Actions and in Azure DevOps pipelines. `README.md` is the source of
  truth for intent, measures and command names; read it before designing
  anything.
- **The goal:** the score is each developer's KPI — the share of their changes
  that are low risk. Low risk means a small, focused change with the lowest
  impact that merges quickly. Many small stories beat one large one.
- **One scorer, two inputs:** `score --base <ref> --head <ref>` measures the
  real diff from git; `estimate` runs the *same* scoring function over facts
  predicted from a plan. Keep the split: `collect` gathers facts, `score` is a
  pure function of facts and rules.
- **The score is about the change only.** Six measures: size (lines added plus
  removed, excluding generated, lock and doc files), spread (files, directories,
  projects), degree (existing code altered), impact (dependents of what
  changed), branch age, and tests. Where a change lands (auth, migrations,
  infrastructure, secrets) is reported as reviewer context and never scores.
- **Invariants — never trade these away:**
  - Deterministic and on one scale for everyone. No LLM or network call in the
    scoring path; same inputs, same score.
  - Not lowerable from inside a change. `.risk/rules.json` is read from the
    target branch, never the PR branch; it may only add risk or context, and
    anything that would lower a score is refused by name. A change to `.risk/`
    is scored like any other; guarding against a dishonest developer is not
    riskelle's concern.
  - Every result is kept and reproducible: each report records the riskelle
    version and the rules source, commit and hash.
- **Independence:** riskelle must not depend on id4me or any one consumer. The
  Python scorer in id4me.pipelines (`risk/risk_score.py`, `rules.default.json`)
  is prior art to port in shape, not code to copy; leave its ADO posting layer
  behind.
- **Ground rules:** practise what we measure — each story is one small, focused
  slice that would score low on its own. If a story's estimate is high, split
  it. Keep agent config (`.claude/`, `.satelle/`, `CLAUDE.md`, `AGENTS.md`)
  gitignored; never commit a `CLAUDE.md`.
- **Where work happens:** trunk based, one story per branch and worktree. Before
  engaging a story, open its tree with `satelle story worktree <id> --base main`
  (branch `story/<id>` under `../riskelle-trees/`) and drive the story from
  there. The merge step lands the story on `main`; the release step tags it and
  watches the release run.
- **Implementing an epic** means driving every story `satelle story wave`
  names in parallel, each in its own worktree, through to done, then closing
  the epic. The developer does not need to ask for parallel or for the work to
  continue: it does not stop to ask until it is blocked, meaning a gate that
  cannot be satisfied, a decision only the developer can make, or a failure
  that two attempts did not fix. A blocked story is surfaced while its
  siblings carry on.
- **Every tracked change reaches main through its story, before it closes, in
  every lane** (substrate and docs included). Stage only the story's own paths
  (never `git add -A`), commit with a conventional subject ending in the story
  id (`docs: … (sty_…)`), and publish with `satelle trunk publish --story <id>`.
  Never force-push.
- **Where the process lives:** authored substrate under `.satelle/` (workflows,
  principles, skills) — edited without a binary release.
