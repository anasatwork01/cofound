"""The observability vocabulary: log field names, redaction, span attributes.

Three of the four modules here are generated from
packages/schema/observability.json by scripts/gen.sh, and the Go chassis is
generated from that same document. That is the only reason a trace starting in
the Go `api` service and continuing into Python `sandboxd` comes back as one
trace, and the only reason the redaction denylist cannot be updated in one
language and forgotten in the other.

Log field names are reached through the module, not re-exported here -- there
are thirty-seven of them and a package-level list of that size is a list that
stops being complete:

    from halyard_chassis.obs import logkey
"""

from __future__ import annotations

from . import attrs, logkey, vocab
from .redaction import (
    ALLOW_EXACT,
    DENY_SUBSTRINGS,
    REDACTED,
    SECRET_PREFIXES,
    SUPPRESSED_MESSAGE,
    is_secret_key,
    scan_message,
)

__all__ = [
    "ALLOW_EXACT",
    "DENY_SUBSTRINGS",
    "REDACTED",
    "SECRET_PREFIXES",
    "SUPPRESSED_MESSAGE",
    "attrs",
    "is_secret_key",
    "logkey",
    "scan_message",
    "vocab",
]
