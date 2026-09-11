"""sandboxd's measurement tools. Not imported by the service at runtime.

Only one so far: `resume_latency`, which measures Modal snapshot and restore
latency. `docs/verified.md` §22 item 5 records that Modal publishes **no timing
at all** for taking or restoring a snapshot -- only "optimized for performance"
and "mounted instantly" -- and that the single hard signal available is the
SDK's own 55-second default snapshot timeout, plus a changelog entry adding
support for longer. SPEC §17's "p50 < 10s, p95 < 25s" therefore rests on a
guess, and this package exists to replace that guess with a row in
`docs/verified.md`.

Nothing here has ever been run against a real Modal account, and nothing here
runs by accident: `modal` is not in the default install, the harness refuses to
act without credentials, and it refuses again without an explicit spend opt-in.
Read `resume_latency`'s module docstring before running it -- it costs money.
"""

from __future__ import annotations

__all__ = ["resume_latency", "stats"]
