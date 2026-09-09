"""Entry point placeholder: ``python -m halyard_sandboxd``."""

import json
import sys

from . import SERVICE, __version__


def main(argv: list[str] | None = None) -> int:
    args = sys.argv[1:] if argv is None else argv
    if "--version" in args:
        print(f"{SERVICE} {__version__}")
        return 0
    print(
        json.dumps({"level": "info", "service": SERVICE, "msg": "scaffolded, not yet implemented"})
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
