# `payments` capability

|                  |                                                                           |
| ---------------- | ------------------------------------------------------------------------- |
| Credential owner | `builder` — the project owner connects their own Stripe account           |
| Provisioning     | Stripe Connect account plus hosted onboarding. May return `needs_action`. |
| Implemented by   | task 5.10 in [docs/TASKS.md](../../docs/TASKS.md)                         |

SPEC §5.3: the money belongs to the user, not us. Connect means we never
hold their secret key or touch card data, hosted onboarding handles KYC, and
`application_fee_amount` leaves a revenue-share option open (**§21 decision 8**).

Storing a user's live Stripe secret key would put custody and liability on us
for no benefit.

Only the **test-mode** key ever reaches a development sandbox (SPEC §17.1).
Confirm SAQ-A applicability with Stripe rather than assuming it (§19.1).

## Expected contents

Nothing here yet. When implemented, this directory holds a `manifest.json`
validated against `packages/schema/capability-manifest.schema.json` (SPEC §7.4),
the file templates it writes, its migrations, and its smoke check.

Installation must be **idempotent** — re-running is a no-op — and **removable**:
files and env go, data tables stay with a warning. Migrations must be additive;
a destructive one requires an approved `proposals` row (SPEC §12.5).
