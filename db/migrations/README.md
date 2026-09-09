# Control plane migrations

`goose` SQL migrations for the control plane database only. Generated apps'
databases are provisioned separately (SPEC §3.3).

Rules (SPEC §0 rule 6, §19.2):

- **Forward-only and additive.** No destructive migration without an explicit
  human approval note in the PR.
- Any migration touching `ledger_entries` requires a review gate.
- Row-level security is enabled on every table carrying `org_id`.

Task 0.6.
