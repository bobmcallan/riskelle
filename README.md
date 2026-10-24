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
  to guard against a dishonest developer.

Every report records the riskelle version and the rules hash it was scored with.

## Where it runs

- Locally, by developers and coding agents.
- Azure DevOps pipelines and GitHub Actions, posting a PR status and one summary
  comment kept up to date.
- Results go to a ledger keyed by developer, from which the KPI (share of low
  risk changes, alongside volume) is reported per period with the rules pinned
  for that period.

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
