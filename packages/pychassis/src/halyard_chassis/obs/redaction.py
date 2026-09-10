"""Classifies a log field name as secret-bearing, and scans message text.

This is the logic; the lists it applies are generated into vocab.py from
packages/schema/observability.json, which is the same document the Go chassis
compiles. Behaviour is deliberately identical to
packages/chassis/logging/redact.go, down to the ordering: allowlist first, then
denylist substrings. Reversing that order is not a refactor, it is a bug --
"key" alone would then swallow idempotency_key.
"""

from __future__ import annotations

from .vocab import (
    ALLOW_EXACT,
    DENY_SUBSTRINGS,
    REDACTED,
    SECRET_PREFIXES,
    SUPPRESSED_MESSAGE,
)

__all__ = [
    "ALLOW_EXACT",
    "DENY_SUBSTRINGS",
    "REDACTED",
    "SECRET_PREFIXES",
    "SUPPRESSED_MESSAGE",
    "is_secret_key",
    "scan_message",
]


def is_secret_key(key: str) -> bool:
    """Report whether a value logged under `key` must be redacted."""
    k = key.lower()
    if k in ALLOW_EXACT:
        return False
    return any(d in k for d in DENY_SUBSTRINGS)


def scan_message(msg: str) -> str | None:
    """Return the first credential shape found in `msg`, or None.

    Returns the matched prefix rather than a bool so the caller can record which
    pattern fired without echoing the message that contained it.
    """
    for p in SECRET_PREFIXES:
        if p in msg:
            return p
    return None
