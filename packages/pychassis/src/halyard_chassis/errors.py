"""The chassis error vocabulary, and the only place that builds a wire error.

This is the Python half of packages/chassis/errs. The safety property is the
same one, and it is structural rather than disciplinary: a handler raises an
exception and has no way to render one itself; the single adapter in asgi.py
maps anything that is not an `Error` to a canned internal envelope; and
`Error.wire` builds the response body only from authored `message` and `fix`
strings -- never from the cause. So a handler that lets an asyncpg exception
escape cannot leak a connection string, because there is no code path from a
cause to a response body.

The catalogue below is a client-visible contract shared with the Go services:
the console branches on `code` and shows `message` and `fix` to the user, and it
must not matter which language served the request. `tests/test_errors.py`
asserts this catalogue renders byte-identically to
packages/chassis/errs/testdata/catalog.golden, so a message edited on one side
and not the other fails the build rather than shipping as an inconsistency.
"""

from __future__ import annotations

import asyncio
import errno
import re
from collections.abc import Iterable, Mapping
from dataclasses import dataclass
from datetime import timedelta
from typing import Any, Final, Self

from halyard_schema import agent_events, common

__all__ = [
    "CHASSIS",
    "CHASSIS_ENTRIES",
    "CODE_CLIENT_CLOSED",
    "CODE_CONFLICT",
    "CODE_FORBIDDEN",
    "CODE_INTERNAL",
    "CODE_INVALID",
    "CODE_METHOD_NOT_ALLOWED",
    "CODE_NOT_FOUND",
    "CODE_NOT_READY",
    "CODE_PAYLOAD_TOO_LARGE",
    "CODE_PAYMENT_REQUIRED",
    "CODE_RATE_LIMITED",
    "CODE_TIMEOUT",
    "CODE_UNAUTHENTICATED",
    "CODE_UNAVAILABLE",
    "CODE_UNSUPPORTED_MEDIA_TYPE",
    "Catalog",
    "Entry",
    "Error",
    "client_closed",
    "code_of",
    "conflict",
    "forbidden",
    "from_exception",
    "internal",
    "invalid",
    "invalid_body",
    "invalid_field",
    "is_client_closed",
    "method_not_allowed",
    "not_found",
    "not_ready",
    "payload_too_large",
    "payment_required",
    "rate_limited",
    "sanitize_ident",
    "status_of",
    "timeout",
    "unauthenticated",
    "unavailable",
    "unsupported_media_type",
    "valid_code",
]

# The pattern the GENERATED decoder enforces on the wire. A code that fails it
# decodes as an error in every client, so `wire` degrades rather than emitting
# one.
_CODE_PATTERN: Final = re.compile(r"^[a-z][a-z0-9_]*$")

# Transport codes owned by the chassis. Services own domain codes.
CODE_INVALID: Final = "invalid"
CODE_UNAUTHENTICATED: Final = "unauthenticated"
CODE_PAYMENT_REQUIRED: Final = "payment_required"
CODE_FORBIDDEN: Final = "forbidden"
CODE_NOT_FOUND: Final = "not_found"
CODE_METHOD_NOT_ALLOWED: Final = "method_not_allowed"
CODE_CONFLICT: Final = "conflict"
CODE_PAYLOAD_TOO_LARGE: Final = "payload_too_large"
CODE_UNSUPPORTED_MEDIA_TYPE: Final = "unsupported_media_type"
CODE_RATE_LIMITED: Final = "rate_limited"
CODE_NOT_READY: Final = "not_ready"
CODE_INTERNAL: Final = "internal"
CODE_UNAVAILABLE: Final = "unavailable"
CODE_TIMEOUT: Final = "timeout"

# Never rendered: the client is gone, so there is nobody to render for. It
# exists so the access log can distinguish a disconnect from a fault, and so a
# disconnect is not counted as a 5xx against the availability SLO.
CODE_CLIENT_CLOSED: Final = "client_closed"

# Replaces an empty authored message.
_FALLBACK_MESSAGE: Final = "Halyard could not complete this request."


def valid_code(code: str) -> bool:
    """Report whether `code` satisfies the wire contract."""
    return _CODE_PATTERN.match(code) is not None


class Error(Exception):
    """The chassis error.

    `message` is authored and client-safe. It is never derived from the cause:
    the cause reaches `str(self)`, which reaches the log and
    `span.record_exception`, and nothing else.

    The `with_*` builders return copies rather than mutating in place, for the
    same reason the Go implementation clones: catalogue-derived errors are
    handed out repeatedly, and two concurrent tasks calling `with_detail` on a
    shared base would cross-contaminate.
    """

    def __init__(
        self,
        *,
        code: str,
        status: int,
        message: str,
        fix: str | None = None,
        retriable: bool = False,
        retry_after: timedelta | None = None,
        details: Mapping[str, Any] | None = None,
        cause: BaseException | None = None,
    ) -> None:
        super().__init__(code)
        self.code = code
        self.status = status
        self.message = message
        self.fix = fix
        self.retriable = retriable
        self.retry_after = retry_after
        self.details: dict[str, Any] | None = dict(details) if details else None
        self.cause = cause

    def __str__(self) -> str:
        """For the log and the span. Never a response body."""
        out = self.code
        if self.message:
            out = f"{out}: {self.message}"
        if self.cause is not None:
            out = f"{out}: {self.cause}"
        return out

    def __repr__(self) -> str:
        return f"Error(code={self.code!r}, status={self.status}, message={self.message!r})"

    def _clone(self, **changes: Any) -> Self:
        out = type(self)(
            code=self.code,
            status=self.status,
            message=self.message,
            fix=self.fix,
            retriable=self.retriable,
            retry_after=self.retry_after,
            details=self.details,
            cause=self.cause,
        )
        for k, v in changes.items():
            setattr(out, k, v)
        return out

    def with_cause(self, cause: BaseException | None) -> Self:
        """Attach a cause.

        No stack is captured here, unlike the Go implementation: an exception
        raised in Python already carries __traceback__, and the logging layer
        formats it for a 5xx.
        """
        return self._clone(cause=cause)

    def with_message(self, message: str) -> Self:
        """Replace the authored, client-safe message."""
        return self._clone(message=message)

    def with_fix(self, fix: str) -> Self:
        """Replace the remedial half of the message (SPEC 18)."""
        return self._clone(fix=fix)

    def with_status(self, status: int) -> Self:
        """Override the HTTP status."""
        return self._clone(status=status)

    def with_retry_after(self, retry_after: timedelta) -> Self:
        """Set the Retry-After header value."""
        return self._clone(retry_after=retry_after)

    def with_detail(self, key: str, value: Any) -> Self:
        """Add one detail. Never a secret value (SPEC 17.2)."""
        merged = dict(self.details or {})
        merged[key] = value
        return self._clone(details=merged)

    def with_details(self, details: Mapping[str, Any]) -> Self:
        """Merge details. Never secret values (SPEC 17.2)."""
        if not details:
            return self._clone()
        merged = dict(self.details or {})
        merged.update(details)
        return self._clone(details=merged)

    # ---------------------------------------------------------------- wire

    def wire(self, request_id: str | None = None) -> common.Error:
        """Render for the HTTP envelope.

        This method and `event` below are the only constructors of a generated
        wire error type in the Python tree, which is what makes the encode-side
        invariants enforceable at all. The body is built only from authored
        `message` and `fix`; there is no path from `self.cause` to any field
        here, and that is structural rather than a rule to remember.

        Verified: the generated `common.Error` validates code against
        ^[a-z][a-z0-9_]*$ and message against minLength 1 -- pydantic validates
        on construction, so an invalid code would raise here rather than
        serialise happily and fail inside a client's decoder. Degrading is still
        the right call: this runs while something is already broken, and raising
        a second error out of the error renderer loses the first one.
        """
        code = self.code if valid_code(self.code) else CODE_INTERNAL
        return common.Error(
            code=code,
            message=self.message or _FALLBACK_MESSAGE,
            fix=self.fix or None,
            retriable=self.retriable,
            details=dict(self.details) if self.details else None,
            request_id=request_id or None,
        )

    def response(self, request_id: str | None = None) -> common.ErrorResponse:
        """Wrap `wire` in the envelope every endpoint returns."""
        return common.ErrorResponse(error=self.wire(request_id))

    def event(self, turn: int) -> agent_events.ErrorEvent:
        """Render as an agent-events error frame.

        For a failure after an SSE stream has already opened, when a status code
        is no longer available. The generated ErrorEvent carries only type,
        turn, code, message and retriable -- no fix, no details, no request id.
        """
        code = self.code if valid_code(self.code) else CODE_INTERNAL
        return agent_events.ErrorEvent(
            type=agent_events.ErrorEventType.error,
            turn=turn,
            code=code,
            message=self.message or _FALLBACK_MESSAGE,
            retriable=self.retriable,
        )


# ------------------------------------------------------------------ catalogue


@dataclass(frozen=True, slots=True)
class Entry:
    """One row of an error catalogue."""

    code: str
    status: int
    retriable: bool
    message: str
    fix: str = ""


class Catalog:
    """An immutable set of entries.

    Immutable, and constructed rather than registered at import time,
    deliberately. A module-level registry populated by import side effects is
    process-global mutable state: it makes tests order-dependent, and makes
    "what codes exist" untestable in isolation.
    """

    __slots__ = ("_entries",)

    def __init__(self, entries: Iterable[Entry]) -> None:
        built: dict[str, Entry] = {}
        for e in entries:
            if not valid_code(e.code):
                raise ValueError(
                    f"errors: code {e.code!r} must match ^[a-z][a-z0-9_]*$, "
                    "which is what the generated decoder enforces"
                )
            if not e.message:
                # The generated schema sets minLength 1 on message. An entry
                # without one fails inside pydantic at render time, at the exact
                # moment something is already broken.
                raise ValueError(f"errors: code {e.code!r} needs a message")
            if not 400 <= e.status <= 599:
                raise ValueError(f"errors: code {e.code!r} has status {e.status}, want 400..599")
            if e.code in built:
                raise ValueError(f"errors: duplicate code {e.code!r}")
            built[e.code] = e
        self._entries = built

    def lookup(self, code: str) -> Entry | None:
        return self._entries.get(code)

    def new(self, code: str) -> Error:
        """Build an error from the catalogue, prefilled."""
        e = self._entries.get(code)
        if e is None:
            # An unknown code must not become a silently mislabelled 500 with a
            # code no client can branch on.
            return internal(LookupError(f"errors: code {code!r} is not in the catalogue"))
        return Error(
            code=e.code,
            status=e.status,
            message=e.message,
            fix=e.fix or None,
            retriable=e.retriable,
        )

    def codes(self) -> tuple[str, ...]:
        """Every code, sorted."""
        return tuple(sorted(self._entries))

    def extend(self, *entries: Entry) -> Catalog:
        """A new catalogue with the service's own domain codes added.

        Returns a new value; the chassis catalogue is never mutated, so two
        services in one test session cannot see each other's codes.
        """
        return Catalog([*self._entries.values(), *entries])

    def dump(self) -> str:
        """Render for a golden file.

        The format is the Go implementation's, exactly: a change to any
        client-facing message or status is visible in review, and the two
        languages' catalogues can be compared byte for byte.
        """
        rows = ["code\tstatus\tretriable\tmessage\tfix"]
        for code in self.codes():
            e = self._entries[code]
            rows.append(f"{e.code}\t{e.status}\t{str(e.retriable).lower()}\t{e.message}\t{e.fix}")
        return "\n".join(rows) + "\n"


# The transport vocabulary.
#
# internal is retriable=False deliberately. The field promises that repeating
# the identical request could succeed, and for an unknown fault we do not know
# -- False makes the console say "contact support with this request id" instead
# of spinning a retry loop against a broken dependency.
CHASSIS_ENTRIES: Final[tuple[Entry, ...]] = (
    Entry(
        CODE_INVALID, 422, False, "That request is not valid.", "Check the fields and try again."
    ),
    Entry(CODE_UNAUTHENTICATED, 401, False, "You are not signed in.", "Sign in and try again."),
    Entry(
        CODE_PAYMENT_REQUIRED,
        402,
        False,
        "This organisation is out of credits.",
        "Top up credits to continue.",
    ),
    Entry(
        CODE_FORBIDDEN,
        403,
        False,
        "Your role does not allow this.",
        "Ask an owner or admin to do it, or to change your role.",
    ),
    Entry(CODE_NOT_FOUND, 404, False, "That does not exist.", ""),
    Entry(CODE_METHOD_NOT_ALLOWED, 405, False, "That method is not allowed here.", ""),
    Entry(
        CODE_CONFLICT,
        409,
        False,
        "That conflicts with the current state.",
        "Reload and try again.",
    ),
    Entry(CODE_PAYLOAD_TOO_LARGE, 413, False, "That request body is too large.", "Send less data."),
    Entry(
        CODE_UNSUPPORTED_MEDIA_TYPE,
        415,
        False,
        "That content type is not supported.",
        "Send application/json.",
    ),
    Entry(CODE_RATE_LIMITED, 429, True, "Too many requests.", "Wait a moment and try again."),
    Entry(
        CODE_NOT_READY,
        503,
        True,
        "This service is still starting up.",
        "Try again in a few seconds.",
    ),
    Entry(
        CODE_INTERNAL,
        500,
        False,
        "Halyard could not complete this request.",
        "Contact support with the request id.",
    ),
    Entry(
        CODE_UNAVAILABLE,
        503,
        True,
        "A service Halyard depends on is unavailable.",
        "Try again shortly.",
    ),
    Entry(CODE_TIMEOUT, 504, True, "That took too long.", "Try again."),
)

CHASSIS: Final = Catalog(CHASSIS_ENTRIES)


# ----------------------------------------------------------- constructors
#
# Each carries its own authored literals, so rendering needs no catalogue
# lookup and `wire` stays a pure function of the error.
#
# These are per-language on purpose, unlike CHASSIS_ENTRIES above. The
# catalogue is the contract a client branches on and a user reads; a
# constructor's message override is local convenience, and requiring the two
# languages to share a format-string dialect would cost more than it protects.


def invalid(message: str) -> Error:
    """A request that was understood but is not valid."""
    return CHASSIS.new(CODE_INVALID).with_message(message)


def invalid_field(field: str, want: str) -> Error:
    """Name the offending field and the shape wanted."""
    return (
        CHASSIS.new(CODE_INVALID)
        .with_message(f"{field} {want}.")
        .with_fix("Correct it and try again.")
        .with_detail("field", field)
    )


def invalid_body(cause: BaseException | None = None) -> Error:
    """An unparseable body.

    The message is generic on purpose: a JSON decoder's offsets describe our own
    schema, and echoing the parser's complaint tells a caller more about our
    internals than about their mistake. The detail goes to the log.
    """
    return (
        CHASSIS.new(CODE_INVALID)
        .with_message("That request body could not be read as JSON.")
        .with_fix("Send a valid JSON object.")
        .with_cause(cause)
    )


def unauthenticated() -> Error:
    """A missing or invalid session."""
    return CHASSIS.new(CODE_UNAUTHENTICATED)


def forbidden(action: str = "") -> Error:
    """A role that may not perform `action`.

    A permission check that fails because of TENANCY is a not_found, never a
    forbidden: api.openapi.yaml makes cross-tenant reads indistinguishable from
    absence on purpose, and a 403 confirms the resource exists.
    """
    e = CHASSIS.new(CODE_FORBIDDEN)
    if not action:
        return e
    return e.with_message(f"Your role does not allow you to {action}.")


def not_found(kind: str = "", ident: str = "") -> Error:
    """An absent -- or invisible -- resource.

    `details` is fixed to {"resource": kind}. There is deliberately no way to
    attach a reason, an owner, an org id or an "exists elsewhere" hint: any of
    those turns this into an existence oracle across tenants. `ident` is
    sanitised, and dropped entirely if it does not survive.
    """
    e = CHASSIS.new(CODE_NOT_FOUND)
    message = "That does not exist."
    if kind:
        message = f"That {kind} does not exist."
        e = e.with_detail("resource", kind)
    clean = sanitize_ident(ident)
    if clean is not None:
        head = kind[:1].upper() + kind[1:] if kind else "That"
        message = f'{head} "{clean}" does not exist.'
    return e.with_message(message)


def method_not_allowed(method: str = "", allowed: Iterable[str] = ()) -> Error:
    """A wrong method, advertising the right ones."""
    e = CHASSIS.new(CODE_METHOD_NOT_ALLOWED)
    if method:
        e = e.with_message(f"{method.upper()} is not allowed on that path.")
    names = list(allowed)
    if names:
        e = e.with_fix(f"Use {', '.join(names)}.").with_detail("allowed", names)
    return e


def conflict(code: str = CODE_CONFLICT, message: str = "") -> Error:
    """A state conflict under a specific code, so a service can distinguish its own 409s."""
    e = CHASSIS.new(CODE_CONFLICT)
    if code and valid_code(code):
        e = e._clone(code=code)
    if message:
        e = e.with_message(message)
    return e


def payment_required(needed: str = "", available: str = "") -> Error:
    """Insufficient credits.

    SPEC 16.3: exhausting the build meter pauses the builder, which is harmless
    because the user is at the keyboard.
    """
    return (
        CHASSIS.new(CODE_PAYMENT_REQUIRED)
        .with_fix("Top up credits, or turn on auto top-up.")
        .with_details({"credits_needed": needed, "credits_available": available})
    )


def payload_too_large(limit: int) -> Error:
    """A body over the limit."""
    return (
        CHASSIS.new(CODE_PAYLOAD_TOO_LARGE)
        .with_fix(f"Send at most {limit} bytes.")
        .with_detail("limit_bytes", limit)
    )


def unsupported_media_type(want: Iterable[str] = ()) -> Error:
    """A content type we do not accept."""
    e = CHASSIS.new(CODE_UNSUPPORTED_MEDIA_TYPE)
    names = list(want)
    if names:
        e = e.with_fix(f"Send {' or '.join(names)}.")
    return e


def rate_limited(retry_after: timedelta) -> Error:
    """Throttling, with a Retry-After."""
    return CHASSIS.new(CODE_RATE_LIMITED).with_retry_after(retry_after)


def unavailable(display: str = "", cause: BaseException | None = None) -> Error:
    """A dependency being down.

    `display` is USER-FACING: "the model provider", never a hostname. A hostname
    in a client-visible message is internal topology, and it is the kind of
    detail that ends up in a screenshot on a public forum.
    """
    e = CHASSIS.new(CODE_UNAVAILABLE)
    if display:
        head = display[:1].upper() + display[1:]
        e = e.with_message(f"{head} is unavailable right now.")
    return e.with_cause(cause)


def timeout(display: str = "", cause: BaseException | None = None) -> Error:
    """A dependency exceeding its budget."""
    e = CHASSIS.new(CODE_TIMEOUT)
    if display:
        head = display[:1].upper() + display[1:]
        e = e.with_message(f"{head} did not respond in time.")
    return e.with_cause(cause)


def internal(cause: BaseException | None = None) -> Error:
    """An unexpected fault. The cause never reaches the client."""
    return CHASSIS.new(CODE_INTERNAL).with_cause(cause)


def not_ready(failed: Iterable[str] = ()) -> Error:
    """Failing readiness probes, by NAME only.

    Never their error strings: /readyz is unauthenticated.
    """
    e = CHASSIS.new(CODE_NOT_READY)
    names = list(failed)
    if names:
        e = e.with_detail("failing", names)
    return e


def client_closed(cause: BaseException | None = None) -> Error:
    """A disconnect.

    Never rendered -- there is nobody to render for. It exists so the access log
    can tell a disconnect from a fault, and so a disconnect is not counted as a
    5xx against the availability SLO.
    """
    return Error(
        code=CODE_CLIENT_CLOSED,
        status=499,
        message="The client closed the connection.",
        cause=cause,
    )


def sanitize_ident(value: str) -> str | None:
    """Guard the one caller-supplied string reflected into a message.

    Without it a caller controls text that lands in a JSON body and in log
    lines: unbounded length, newlines for log forging, and anything a console
    renders. Returns None when the value does not survive, and callers drop it.
    """
    if not value or len(value) > 64:
        return None
    for ch in value:
        if not (ch.isascii() and (ch.isalnum() or ch in "._-")):
            return None
    return value


def is_client_closed(exc: BaseException | None) -> bool:
    """Whether `exc` is a disconnect rather than a fault.

    asyncio.CancelledError is included because that is how a disconnect reaches
    a handler under an ASGI server -- and because uvicorn additionally cancels
    in-flight tasks when the graceful-shutdown timeout expires, which is a
    shutdown rather than a fault and must not page anyone.
    """
    if exc is None:
        return False
    if isinstance(exc, Error):
        return exc.code == CODE_CLIENT_CLOSED
    if isinstance(exc, asyncio.CancelledError | BrokenPipeError | ConnectionResetError):
        return True
    if isinstance(exc, OSError) and exc.errno in (errno.EPIPE, errno.ECONNRESET):
        return True
    # Starlette raises this from request.body() when the peer vanishes.
    return type(exc).__name__ == "ClientDisconnect"


def from_exception(exc: BaseException | None) -> Error | None:
    """The leak-proofing entry point.

    A typed error passes through; anything else becomes an internal fault whose
    cause reaches only the log. Every rendering path calls this, so a handler
    letting an asyncpg exception escape produces a canned envelope by
    construction rather than by review.
    """
    if exc is None:
        return None
    if isinstance(exc, Error):
        return exc
    if is_client_closed(exc):
        return client_closed(exc)
    return internal(exc)


def status_of(exc: BaseException | None) -> int:
    """The HTTP status for `exc`, defaulting to 500."""
    return exc.status if isinstance(exc, Error) and exc.status else 500


def code_of(exc: BaseException | None) -> str:
    """The code for `exc`, defaulting to internal."""
    return exc.code if isinstance(exc, Error) and exc.code else CODE_INTERNAL
