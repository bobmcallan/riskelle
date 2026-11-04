---
name: release
scope: project
type: skill
tags: [type:skill]
evidence_commands: gh run watch
description: In-loop executor rubric for riskelle's release step. Tags the next version on the merged main head, pushes the tag, watches the GitHub release run (binaries and checksums on GitHub Releases) to completion, verifies the release, and attaches the evidence for the Claude release reviewer. Never moves main, never advances status.
---

# Release (in-loop, release)

The story is on `origin/main`. Ship it: a GitHub Release with the binaries and
a checksums file, built by the release workflow from a version tag, and, once
that workflow deploys it, the project site on GitHub Pages
(https://bobmcallan.github.io/riskelle/).

## Before the release pipeline exists

Until `.github/workflows/release.yml` exists on `origin/main`, there is nothing
to release. Check with `git cat-file -e origin/main:.github/workflows/release.yml`.
If it is absent, attach the evidence with `Outcome: no-release-pipeline` and the
`origin/main` sha, cut no tag, and request `done`. The release reviewer accepts
this only while the file is absent.

## Do

1. **Pick the version.** Latest tag: `git describe --tags --abbrev=0 origin/main`
   (none means start at `v0.1.0`). Before 1.0, a `feature` story bumps the
   minor version and anything else the patch.
2. **Tag the merged head and push the tag, under the landing lock.** Sibling
   stories release too, so read the latest tag and push the next one while
   holding the lock merge.md lands under, in one Bash call started in the
   story's worktree:

   ```bash
   common=$(git rev-parse --path-format=absolute --git-common-dir)
   flock "$common/riskelle-land.lock" -c '
     git fetch origin --tags &&
     git describe --tags --abbrev=0 origin/main &&
     git tag -a vX.Y.Z origin/main -m "vX.Y.Z (<story id>)" &&
     git push origin vX.Y.Z'
   ```

   Pick vX.Y.Z from the tag `git describe` prints inside the lock (run it
   once inside the lock first if unsure); a push refused because the tag
   exists means a sibling took it: take the next one.

3. **Watch the release run to the end.**

   ```bash
   gh run list --workflow release.yml --branch vX.Y.Z --limit 1 --json databaseId,url
   gh run watch <run id> --exit-status
   ```

   A failed run goes back to `in_progress` with the run's log: request it. Never
   move or delete a pushed tag. Fix forward with a new patch version.
4. **Verify what shipped.**

   ```bash
   gh release view vX.Y.Z --json tagName,assets
   ```

   When the release workflow at the tag deploys Pages
   (`git show vX.Y.Z:.github/workflows/release.yml | grep -q actions/deploy-pages`),
   also check the site: the run's Pages deploy job concluded `success`, and the
   site answers 200.

   ```bash
   gh run view <run id> --json jobs --jq '.jobs[] | "\(.name) \(.conclusion)"'
   curl -fsS -o /dev/null -w '%{http_code}\n' https://bobmcallan.github.io/riskelle/
   ```

5. **Attach the evidence.** Add the `Pages:` line only when the workflow
   deploys Pages.

   ```bash
   satelle story attach <id> --name release-evidence --type output --file - <<'EOF'
   Outcome: released
   Tag: vX.Y.Z  (on origin/main <sha>)
   Release run: <url>  conclusion: success
   Assets: <names>
   Pages: https://bobmcallan.github.io/riskelle/  deploy job: success  HTTP 200
   EOF
   ```

6. **Leave nothing on the remote.** If `story/<id>` was ever pushed, delete it
   now, while the story is still engaged (a push after done has no story to
   be attributed to and is refused):

   ```bash
   git ls-remote --exit-code --heads origin story/<id> && git push origin --delete story/<id>
   ```

7. Stop. Request `release → done`.

## After done

Remove the story's worktree and local branch, from the main tree:
`git worktree remove <path>` and `git branch -d story/<id>`. Both are local and
need no seat.
