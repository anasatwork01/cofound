"""Modal sandbox lifecycle, warm pool, snapshots, tunnels.

The HTTP surface is defined by packages/schema/sandboxd.openapi.yaml and is
implemented in task 1.3. What exists today is the chassis wiring: configuration,
logging, tracing, probes and ordered shutdown, with no routes of its own.
"""

from __future__ import annotations

__all__ = ["SERVICE", "__version__"]

SERVICE = "sandboxd"
__version__ = "0.0.0"
