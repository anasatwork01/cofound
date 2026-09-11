"""`python -m halyard_sandboxd.bench` -- see `resume_latency` for what it costs."""

from __future__ import annotations

import sys

from .resume_latency import main

if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
