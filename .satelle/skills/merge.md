---
name: merge
scope: project
type: skill
tags: [type:skill]
evidence_commands: satelle trunk publish
description: In-loop executor rubric for riskelle's merge step. Takes the slice the integration step committed on its story branch, merges in the latest main, re-proves the tests on the combined tree, fast-forwards main and publishes it with satelle trunk publish. Never force-pushes, never tags, never advances status.
---

# Merge (in-loop, merge)

riskelle is trunk based: every story lands on `main` as soon as it is proven.
The story was built on `story/<id>` in its own worktree so siblings could run in
parallel; this step brings it home.

## Do

1. **The slice is committed.** The integration step committed it on
   `story/<id>` and scored it. If anything changed since, commit it the same
   way (only the story's own paths, a conventional subject ending in the story
   id, e.g. `feat(collect): count lines per file (sty_57ddda69)`).
2. **Bring in the latest main.**

   ```bash
   git fetch origin
   git merge origin/main        # a merge, never a rebase of pushed history
   ```

   Resolve any conflict in favour of both changes, then re-prove the combined
   tree:

   ```bash
   gofmt -l . && go vet ./... && go test ./... && go test -tags integration ./tests/...
   ```

3. **Land it on main, starting from the worktree, under the landing lock.**
   Sibling stories land into the same main tree, so landing holds a lock file
   in the repository's git directory. Start the command in the story's
   worktree, not the main tree: satelle's hook attributes a command to the
   seat whose worktree it starts in, and the main tree is no seat's worktree,
   so a command started there is refused while siblings hold seats.

   ```bash
   # one Bash call, started in the story's worktree
   common=$(git rev-parse --path-format=absolute --git-common-dir)
   flock "$common/riskelle-land.lock" -c "
     cd '$(dirname "$common")' &&
     git switch main && git merge --ff-only origin/main &&
     git merge --ff-only story/<id> &&
     SATELLE_SESSION=$CLAUDE_CODE_SESSION_ID satelle trunk publish --story <id>"
   ```

   If `--ff-only story/<id>` fails, main moved since step 2: repeat from
   step 2. `satelle trunk publish` fetches again, merges anything another
   machine pushed meanwhile, runs `[trunk] prove` from satelle.toml on the
   combined head and pushes with a plain push. It never forces. If it stops
   (conflict, failed proof, bound spent), main is left at your merge head and
   nothing is pushed: fix it on the story branch and repeat from step 2.
4. **Watch CI on main and record it.**

   ```bash
   gh run list --workflow ci.yml --branch main --commit <pushed sha> --limit 1 --json databaseId,url
   gh run watch <run id> --exit-status
   satelle story log <id> --kind merge --data commit=<story commit sha> --data main=<pushed sha> --data ci_main_run=<run id>
   ```

   A failed run on main goes back to `in_progress`: request it.

5. Stop. Request `merge → release`; `riskelle-merged-check` confirms the story
   branch is on `origin/main`.

## Never

`git push --force`, `git reset --hard`, rebasing main, tagging, or committing
files outside the story's slice.
