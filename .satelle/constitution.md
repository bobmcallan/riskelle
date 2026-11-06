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
  Python scorer in id4me.pipelines is prior art to port in shape, not code to
  copy.
- **Ground rules:** practise what we measure — each story is one small, focused
  slice that would score low on its own. If a story's estimate is high, split
  it. `CLAUDE.md` and `AGENTS.md` are never committed; `.satelle/` is tracked.
- **Where work happens:** one item per branch and worktree: branch `<id>` under
  `../riskelle-trees/<id>`, seated with `satelle story seat <id> --base
  origin/main` (the `coder` skill). DEV (plan, code, integrate) ends at
  `approved`: a tested head, pushed, with an open pull request. CI/CD is a run
  of the release task (tagged `release`; `.satelle/skills/release.md`): it
  fast-forwards main to the approved heads, tags the next version, waits for
  the GitHub release, installs it, and only then closes the items.
- **Implementing an epic** (an item tagged `epic`) means driving every open
  child in parallel, each in its own worktree, to approved, then releasing them
  together. Do not stop to ask until blocked: a gate that cannot be satisfied,
  a decision only the developer can make, or a failure two attempts did not
  fix. Park a blocked item while its siblings carry on.
- **Every change reaches main through an item's pull request and a release
  run.** Stage only the item's own paths (never `git add -A`), commit with a
  conventional subject ending in the item id, never force-push. Process-only
  changes under `.satelle/` are a `chore`.
- **Where the process lives:** `.satelle/` — workflows, skills, agents and this
  constitution — edited without a binary release.
