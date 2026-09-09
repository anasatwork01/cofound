# `uploads` capability

|                  |                                                   |
| ---------------- | ------------------------------------------------- |
| Credential owner | `none`                                            |
| Provisioning     | An R2 bucket per project, with signed URLs.       |
| Implemented by   | task 5.13 in [docs/TASKS.md](../../docs/TASKS.md) |

No user-supplied credential, so nothing here can be exfiltrated from a
sandbox beyond a scoped development bucket.

Signed URLs rather than public objects, because a generated app's uploads are
its users' data and an unlisted URL is not access control — the same reasoning
that gates preview deployments in SPEC §14.3.

## Expected contents

Nothing here yet. When implemented, this directory holds a `manifest.json`
validated against `packages/schema/capability-manifest.schema.json` (SPEC §7.4),
the file templates it writes, its migrations, and its smoke check.

Installation must be **idempotent** — re-running is a no-op — and **removable**:
files and env go, data tables stay with a warning. Migrations must be additive;
a destructive one requires an approved `proposals` row (SPEC §12.5).
