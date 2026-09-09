# `email` capability

|                  |                                                                |
| ---------------- | -------------------------------------------------------------- |
| Credential owner | `builder`                                                      |
| Provisioning     | Sending domain, with SPF and DKIM records for the user to add. |
| Implemented by   | task 5.12 in [docs/TASKS.md](../../docs/TASKS.md)              |

Per-domain SPF/DKIM setup is required, not optional: SPEC §19.3 warns that
shared sending reputations get poisoned, so the capability must surface a
warning about that rather than quietly putting every generated app behind one
reputation.

DNS records are surfaced the same way domains are (SPEC §14.2): copyable, with
the observed value alongside the expected one.

## Expected contents

Nothing here yet. When implemented, this directory holds a `manifest.json`
validated against `packages/schema/capability-manifest.schema.json` (SPEC §7.4),
the file templates it writes, its migrations, and its smoke check.

Installation must be **idempotent** — re-running is a no-op — and **removable**:
files and env go, data tables stay with a warning. Migrations must be additive;
a destructive one requires an approved `proposals` row (SPEC §12.5).
