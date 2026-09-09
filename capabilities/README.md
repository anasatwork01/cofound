# Capability modules

One directory per capability, each containing a `manifest.json` validated
against `packages/schema/capability-manifest.schema.json`, file templates,
migrations and a smoke check. See SPEC §7.4 and §13.

Installation must be **idempotent** (re-running is a no-op) and **removable**
(files and env go, data tables stay with a warning).

v1 catalogue: `auth`, `payments`, `email`, `uploads`, `slack`, and `ai`
(phase 7). Builder-time credentials only in v1 - see SPEC §13.2.
