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

With [satelle](https://github.com/bobmcallan/satelle), the estimate and the
actual are recorded on the story, so the developer and the agent work the
number down together, and a plan reviewer can send a high estimate back to be
split.

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

## Status

Starting point. The design is carried over from a Python scorer running in
shadow mode on Azure DevOps; riskelle is its standalone Go successor.
