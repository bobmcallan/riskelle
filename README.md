# riskelle

A deterministic risk score for every code change, so developers and agents can
see — before, during and after the work — how risky a change is, and aim to make
each one smaller.

## Intent

Risk is about **the change itself**: how big it is, how much existing code it
alters, how far it reaches and how long it stayed away from the trunk. Every one
of those is in the hands of the person (or agent) making it.

Low risk is focused development: the smallest change with the lowest impact,
merged quickly. Many small stories are better than one large one.

The score is the fundamental measure for developers. Each developer's KPI is the
share of their changes that are low risk, so the score must be:

- **Deterministic** — the same change always gets the same score. No LLM in the
  scoring path.
- **Transparent** — every point names the measure and the facts behind it, and
  anyone can re-run it locally and get the same number.
- **One scale** — the same bands for every repo and team, so percentages compare.
- **Not lowerable from inside a change** — a pull request cannot change the
  rules that score it.

## Measures

| Measure | What it captures |
| --- | --- |
| Size | Lines added and removed, excluding generated files, lock files and docs |
| Spread | Files, directories and projects (`go.mod`, `package.json`, `.csproj`) touched |
| Degree | How much *existing* code is altered, against code in new files |
| Impact | How many places depend on what changed (for Go, packages importing a changed package; otherwise files referencing a changed file) |
| Branch age | Days from the branch's first commit to merge, and how far the trunk moved meanwhile |
| Tests | A production change with no test change |

Each measure scores points in fixed bands; the total is 0–100, reported as Low,
Medium or High. The band edges are calibrated from real merge history before
anyone is measured.

Where a change lands (auth, migrations, infrastructure, secrets) is shown on the
report as context for reviewers, but does not count towards the score: it is the
area's risk, not the developer's.

## Estimate, then score

The same scorer runs over two sources of facts:

- **`riskelle estimate`** — at planning, from the files the plan says it will
  change, the expected size and the target branch age. Most of it is exact (the
  planned paths give spread and impact); size is the guess. A high estimate is
  the cheapest point to split a story.
- **`riskelle score --base <ref> --head <ref>`** — the real diff. Run locally
  while working to bring the score down, and in CI on every pull request (or
  push) as the official figure.

### Plan file

`riskelle estimate plan.json`, run in the repository the change targets, reads
a JSON plan file that predicts the change:

```json
{
  "target": "main",
  "branch_age_days": 1,
  "files": [
    {"path": "internal/facts/plan.go", "added": 110},
    {"path": "cmd/riskelle/main.go", "added": 40, "removed": 2},
    {"path": "internal/facts/plan_test.go", "added": 95}
  ]
}
```

- `target` (required): the branch the change will merge into. Its tree says
  which planned paths are new (new files, and so new packages), holds the
  project manifests, and is where impact is counted and `.risk/rules.json` is
  read, as for a real score.
- `files` (required, not empty): every path the change will touch, each listed
  once, repo-relative, slash-separated and clean (no leading `/`, no `..`).
  - `added`, `removed`: expected line counts, non-negative. At least one must
    be given; an absent one is 0.
  - Planned tests are the test files listed (they are classed from the path,
    as in a real diff).
  - A move is its old path with the removed lines plus its new path with the
    added lines.
- `branch_age_days` (optional, default 0, non-negative): the expected days
  from the branch's first commit to merge.

Any other field is refused. Trunk drift cannot be planned and is 0. A plan
that is invalid or incomplete is refused with every problem named, exit code 1.

It prints the same report as a real score, marked `"kind": "estimate"` and
naming the plan file in `"plan"`.

### Report

`riskelle score` and `riskelle estimate` print their report as JSON by
default, or as a Markdown summary for a PR comment with `--format markdown`
(flags go before the plan file). Both carry the total and level, every
breakdown entry with the facts behind it, and the provenance (riskelle
version, rules source, rules commit and hash). A score names the full base
and head commits it measured (`"base"`, `"head"`); an estimate is marked as
one and names its plan file. The sensitive areas the change lands in sit apart,
under `"context"` (JSON) or "Where it lands (context, not scored)" (Markdown),
labelled as context that does not count towards the score. The same result
always prints the same bytes.

## With satelle

With [satelle](https://github.com/bobmcallan/satelle), a story carries its
riskelle estimate from planning, which the developer and the agent work down,
and its actual score from the real diff; a High estimate is sent back to be
split. riskelle itself knows nothing of satelle: the wiring below is a few
shell commands (they need `jq`) and one coded satelle gate.

**Install.** Copy `integrations/satelle/riskelle-risk-review.md` into the
repo's `.satelle/skills/`, then add its gate to the workflow beside satelle's
own `satelle-estimate-actual-review` gate (keep that one too):

```toml
[[gate]]
skill = "riskelle-risk-review"
on = ["in_progress", "<last state before the branch lands>", "done"]
for = ["*"]
```

and run `satelle workflow validate`.

**Tags.** The figures sit in the story's tags, next to satelle's own
`estimate-*` and `actual-*`:

| Tag | Holds |
| --- | --- |
| `estimate-risk:<total>` | the estimate's total, 0-100 |
| `estimate-risk-level:<level>` | the estimate's level: low, medium or high |
| `actual-risk:<total>` | the real diff's total, 0-100 |
| `actual-risk-level:<level>` | the real diff's level |

**Estimate at planning**, before requesting `plan → in_progress` (re-run it
whenever the plan changes; it replaces the previous figure):

```sh
riskelle estimate plan.json > risk-estimate.json
satelle story set <id> --remove-tags estimate-risk:,estimate-risk-level:
satelle story set <id> --add-tags "estimate-risk:$(jq .total risk-estimate.json),estimate-risk-level:$(jq -r '.level|ascii_downcase' risk-estimate.json)"
satelle story attach <id> --name risk-estimate --type output --file risk-estimate.json
```

**Actual before it lands**, in the story's tree, before the branch merges into
the target (once it has landed, the merge base is the head and the diff is
empty):

```sh
riskelle score --base <target> --head HEAD > risk-actual.json
satelle story set <id> --remove-tags actual-risk:,actual-risk-level:
satelle story set <id> --add-tags "actual-risk:$(jq .total risk-actual.json),actual-risk-level:$(jq -r '.level|ascii_downcase' risk-actual.json)"
satelle story attach <id> --name risk-actual --type output --file risk-actual.json
```

The attached reports keep the riskelle version and rules hash behind each
figure.

**A High estimate** is refused at `in_progress` until the story is split. The
one exit is a developer's decision, taken from their own terminal (satelle
refuses it from an agent session):
`satelle story waive <id> --gate riskelle-risk-review --check high-estimate --reason "..."`.
The gate reads that waiver from its payload and records it in its verdict
notes; no other check of the gate can be waived.

The gate's exact behaviour on each edge, including the estimate-beside-actual
line it leaves for reviewers, is in the skill file itself.

## Rules

- **Built in, versioned with each release:** the measures, weights, bands and
  default patterns. Changed only by a riskelle release, so the scale is the same
  everywhere.
- **`.risk/rules.json` in the assessed repo:** settings that can only *add*
  risk or context (extra generated paths for the report, extra sensitive
  areas). It is read from the **target branch**, so a change cannot alter its
  own rules. Anything that would lower a score is refused by name. A change
  that edits `.risk/` is scored like any other change: riskelle does not try
  to guard against a dishonest developer. riskelle's own
  [`.risk/rules.json`](.risk/rules.json) is an example: it adds areas for the
  scorer and the release pipelines, and marks the report goldens generated.

Every report records the riskelle version and the rules hash it was scored with.

## Where it runs

- Locally, by developers and coding agents.
- Azure DevOps pipelines and GitHub Actions, posting a PR status and one summary
  comment kept up to date (GitHub: see [GitHub pull requests](#github-pull-requests)).
- Results go to a ledger keyed by developer, from which the KPI (share of low
  risk changes, alongside volume) is reported per period with the rules pinned
  for that period.

### GitHub pull requests

The action in `action.yml` scores every pull request with a **released**
riskelle binary (downloaded from GitHub Releases and checked against the
release's checksums file, never built from the PR), posts a `riskelle` commit
status on the PR head, and keeps one summary comment on the PR, updated in
place on each push. riskelle's own `.github/workflows/riskelle.yml` runs it and
is the workflow to copy.

To adopt it in a repository:

1. While `bobmcallan/riskelle` is private, its releases cannot be downloaded
   without a token (an unauthenticated download returns 404). Its owner allows
   Actions access from the owner's other repositories (Settings, Actions,
   General, Access), and the adopting repository stores a token that can read
   riskelle's releases as a secret, passed as the action's `release-token`.
   Inside `bobmcallan/riskelle` itself the default token is enough.
2. Copy `.github/workflows/riskelle.yml` to `.github/workflows/` and replace
   `uses: ./` with `uses: bobmcallan/riskelle@vX.Y.Z`, setting `version` to the
   same tag. Keep its permissions (`pull-requests: write` and `statuses: write`
   for the default token), `fetch-depth: 0` (the merge base, branch age and
   trunk drift need history) and its concurrency group.
3. Inputs and outputs (`total`, `level`) are described in `action.yml`.

On the pull request:

- The `riskelle` status reads `<Level> · <total> / 100` and links to the
  summary comment. It is always `success`: the score is a measure, not a merge
  gate (a branch rule can still require the status). It is `error`, linking to
  the run, when riskelle cannot score; the comment is then left as it was.
- The summary comment is the `riskelle score --format markdown` report behind a
  hidden `<!-- riskelle:summary -->` marker. Duplicate marked comments are
  deleted; other comments are never touched.
- The base is the target branch's tip and the head is the PR head commit (not
  GitHub's merge commit), both named in full on the report, so
  `riskelle score --base <base> --head <head>` with the same release reproduces
  it locally byte for byte. The rules are read from the target branch.

Pull requests from forks are not supported: their token cannot post.

### Azure DevOps

[`ado/riskelle.yml`](ado/riskelle.yml) is a job template that scores every
pull request with a released riskelle. Its parameters and the permissions it
needs are documented in the template's header comments. To adopt it:

1. Reference the template from your pipeline, at the same release tag you run.
   Through a GitHub service connection (`riskelle-github` here):

   ```yaml
   resources:
     repositories:
       - repository: riskelle
         type: github
         endpoint: riskelle-github
         name: bobmcallan/riskelle
         ref: refs/tags/vX.Y.Z

   jobs:
     - template: ado/riskelle.yml@riskelle
       parameters:
         version: vX.Y.Z
   ```

   Or copy `ado/riskelle.yml` into your repository and use
   `- template: ado/riskelle.yml` with the same `version` parameter.
2. Make the pipeline run on pull requests: on Azure Repos, add it as a
   **Build validation** policy on the target branch (the job is skipped on any
   build that is not for a pull request).
3. Grant the project's build service identity **Contribute to pull requests**
   on the repository, so the job can post the status and the comment.

Each pull request build then gives:

- a PR status `riskelle/score` whose description carries the band and total
  (`Low, 7/100`). It is `succeeded` for every band (riskelle measures, it does
  not gate) and `error` when the change could not be scored;
- one summary comment, the Markdown report, updated in place on every push;
- the JSON report as the build artifact `riskelle`.

The pipeline scores the PR's own head commit against its target branch tip,
not the merge commit ADO builds, so the result is what you get locally. To
reproduce it, run the same release with the `base` and `head` SHAs from the
report: `riskelle score --base <base> --head <head>`.

Self-hosted agents need Linux (amd64 or arm64) with bash, git, curl, jq, tar
and sha256sum, as Microsoft-hosted Ubuntu agents have.

## Releasing

A release is cut by pushing a `vX.Y.Z` tag on a commit on `main`:

```sh
git tag -a vX.Y.Z origin/main -m vX.Y.Z
git push origin vX.Y.Z
```

The tag runs `.github/workflows/release.yml`. It builds every target with
`scripts/release.sh` (the owner of the target list and archive layout), checks
that the linux/amd64 binary reports the tag as its version, and attaches the
archives and `riskelle_vX.Y.Z_checksums.txt` to a GitHub Release named after
the tag. A tag whose commit is not on `main` fails the run. A pushed tag is
never moved: a broken release is fixed forward with the next patch version.
Once the Release is published, the workflow's `pages` job publishes this README
as the project site at https://bobmcallan.github.io/riskelle/.

Verify a download:

```sh
sha256sum -c --ignore-missing riskelle_vX.Y.Z_checksums.txt
```

Reproduce a release on Linux: `git checkout vX.Y.Z && scripts/release.sh vX.Y.Z /tmp/out`,
then compare the checksums file with the one on the Release. The Go toolchain
is pinned by `go.mod` and every timestamp is the commit time, so the archives
match byte for byte.

## Status

Starting point. The design is carried over from a Python scorer running in
shadow mode on Azure DevOps; riskelle is its standalone Go successor.
