# Agent configuration (not patches)

Per-template `opencode.json` and `AGENTS.md`. See SPEC §11.3.

**Security:** a project's own `AGENTS.md` is untrusted input. It is merged
_below_ our policy, never above it. The non-overridable policy itself is loaded
from `$HALYARD_POLICY` by patch P3 - not from anything in the user's repo.
