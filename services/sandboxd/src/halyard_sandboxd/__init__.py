"""Modal sandbox lifecycle, warm pool, snapshots, tunnels.

The HTTP surface is defined by packages/schema/sandboxd.openapi.yaml and is
implemented in task 1.3. What exists today is the chassis wiring: configuration,
logging, tracing, probes and ordered shutdown, with no routes of its own, plus
the `bench` package (task 1.19).

Read README.md before implementing task 1.3. Three of SPEC 9's seven lifecycle
steps cannot work as written, because Modal has no resume primitive, because
`volumes=` is create-time only (so Volumes and the warm pool are mutually
exclusive), and because Modal's `idle_timeout` has no pre-termination hook and
does not count a running daemon as activity. The warm pool is also ours to
build: Modal has no pool primitive. See docs/verified.md, SPEC 22 item 5.
"""

from __future__ import annotations

__all__ = ["SERVICE", "__version__"]

SERVICE = "sandboxd"
__version__ = "0.0.0"
