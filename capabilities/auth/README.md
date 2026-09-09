# `auth` capability

|                  |                                                                         |
| ---------------- | ----------------------------------------------------------------------- |
| Credential owner | `none`                                                                  |
| Provisioning     | Tables in the app's own database. No third-party tenant is provisioned. |
| Implemented by   | task 5.9 in [docs/TASKS.md](../../docs/TASKS.md)                        |

SPEC §5.2 rejects a hosted per-project tenant (Clerk, WorkOS, Auth0)
deliberately: at scale that means provisioning tens of thousands of third-party
tenants, each with its own billing relationship, rate limits and failure modes,
and the user's own user table becomes hostage to a vendor.

A code-first library keeps users and sessions in the project's own Postgres, so
the agent can read and extend the schema like any other code and the app runs
entirely offline in the sandbox.

Library choice is **SPEC §21 decision 4** — Auth.js or Better Auth, pinned per
template, pending §22 item 14 (Drizzle adapter support, behaviour on Cloudflare
Workers).

## Expected contents

Nothing here yet. When implemented, this directory holds a `manifest.json`
validated against `packages/schema/capability-manifest.schema.json` (SPEC §7.4),
the file templates it writes, its migrations, and its smoke check.

Installation must be **idempotent** — re-running is a no-op — and **removable**:
files and env go, data tables stay with a warning. Migrations must be additive;
a destructive one requires an approved `proposals` row (SPEC §12.5).
