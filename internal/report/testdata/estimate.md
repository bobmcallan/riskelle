## riskelle estimate: 7 / 100, Low

Estimate from plan `plan.json`, not a measured diff.

| Measure | Fact | Value | Band from | Points |
| --- | --- | ---: | ---: | ---: |
| size | lines | 101 | 51 | 5 |
| impact | dependents | 4 | 3 | 2 |

<details><summary>Facts behind each point</summary>

- size / lines: `core/calc.go`, `ui/a&b<c>.js`; excluded: `docs/guide.md`
- impact / dependents: `example.com/m/core` (4), `ui/a&b<c>.js` (1)

</details>

### Where it lands (context, not scored)

Where the change lands is context for reviewers; it does not count towards the score.

- auth: `internal/auth/login.go`
- payments: `pay/card.go`, `pay/refund.go`

### Provenance

- riskelle `v0.11.0`
- rules `.risk/rules.json` at `0123456789abcdef0123456789abcdef01234567`
- rules hash `sha256:00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff`
