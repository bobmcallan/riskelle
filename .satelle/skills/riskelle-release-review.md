---
name: riskelle-release-review
scope: project
type: skill
tags: [type:skill, type:reviewer]
description: Gate on release → done (Claude). Isolated read-only reviewer judges the recorded release evidence (version tag on the merged main head, a successful release run, the archives and checksums, and the Pages deploy once the release workflow has one) and the merge commit's convention, or confirms the no-release-pipeline outcome is legitimate. Never tags, pushes or fetches.
---

# Release review (release → done)

May the story close? You get `{story, from, to}` on stdin and read-only tools.
Judge recorded evidence; you have no shell, so you never fetch live CI.

## Evidence

Open the payload `docs` entry `release-evidence`. Missing is a reject.

### Outcome: released

Accept only when all of these are recorded:

- a tag `vX.Y.Z` that sorts after the previous tag, on the `origin/main` sha;
- the release run's URL with conclusion `success` (absent, failed or still
  running is a reject);
- release assets for the binary (archives and a checksums file).

When `.github/workflows/release.yml` at the recorded sha deploys GitHub Pages
(it uses `actions/deploy-pages`; read the tree), also require:

- a `Pages:` line naming https://bobmcallan.github.io/riskelle/, the deploy
  job's conclusion `success`, and HTTP 200. Absent or failed is a reject.

When that workflow has no Pages deploy, ask for no Pages evidence.

### Outcome: no-release-pipeline

Accept only when the repository at the recorded sha has no
`.github/workflows/release.yml` (read the tree). If the file exists, reject:
the story must be released.

## Commit

The story's commit (from the `merge` ledger row or `git log`) has a
conventional subject ending in the story id.

## Verdict

```json
{"decision": "accept", "notes": "", "reviewed": "<exact words this verdict rests on>"}
```
