"""Structured JSON logging with unbypassable redaction (SPEC 17.3).

The Python counterpart of packages/chassis/logging. Same output shape, same
redaction vocabulary -- generated from the same document -- and the same
guarantee: no secret values, and no user content above debug level.

Named logs.py rather than logging.py so `import logging` inside this package
still means the standard library.

## Where redaction happens, and why it has to be there

In Go the guarantee comes from wrapping slog.Handler, because that is the one
place every record passes through. Python's equivalent is
`logging.Handler.handle`, and picking anywhere else produces a chassis that
looks correct and is not. All four alternatives were measured (see
docs/verified.md):

  * A Filter on the root LOGGER looks global and is not. `Logger.callHandlers`
    walks ancestor loggers' HANDLERS but never their FILTERS, so records from
    httpx, uvicorn.access and asyncpg bypass it entirely while the app's own
    records are redacted. A unit test that only logs from app code passes on a
    broken chassis, which is the worst property a security test can have.
  * A Formatter has four independent escapes: a second handler with its own
    formatter; the `exc_text` cache, which holds the raw traceback for the next
    handler to reuse; `Handler.handleError`, which dumps raw msg and args to
    stderr when a formatter raises; and handlers that never call `format()` at
    all.
  * `setLoggerClass` misses every logger created before the call, which is
    essentially all library loggers, and never touches root.
  * `setLogRecordFactory` is a single global slot any library can clobber, and
    runs before `exc_text` or the interpolated message exist.

So redaction is a mixin over `Handler.handle`. Being at the base-class method
also means it runs before `QueueHandler.prepare` interpolates the message, and
before any sibling handler's formatter can populate the `exc_text` cache with
plaintext -- so handler ORDER cannot break it.

## The message, not the arguments

`log.info("token=%s", secret)` leaves the secret in `record.args`, not in
`record.msg`. The redactor therefore resolves `record.getMessage()` first, then
redacts, then clears `args`. Inspecting args element-wise would miss a non-str
`msg` whose `__str__` holds the secret, a value rendered through `%r`, and the
single-mapping args form.

## Two residual holes, stated rather than papered over

  * A credential interpolated into a message by code we do not own cannot be
    caught by key matching. `scan_message` catches known shapes; an unknown
    provider's format still leaks. This is the same hole the Go chassis states.
  * Raising the level to debug deliberately reveals user content. That is a
    privacy action, not a verbosity tweak, and `ChassisSettings` refuses
    LOG_LEVEL=debug in production for exactly that reason.
"""

from __future__ import annotations

import contextlib
import contextvars
import datetime as dt
import json
import logging
import sys
import threading
from collections.abc import Iterator, Mapping
from typing import Any, Final, TextIO

from .obs import logkey
from .obs.redaction import REDACTED, SUPPRESSED_MESSAGE, is_secret_key, scan_message

__all__ = [
    "OMITTED_USER_CONTENT",
    "JSONFormatter",
    "LogConfig",
    "Sink",
    "TextFormatter",
    "UserContent",
    "adopt_existing_loggers",
    "bind",
    "configure",
    "diff",
    "is_user_content",
    "output",
    "prompt",
    "reset",
    "user",
]

# Replaces user content above debug level.
OMITTED_USER_CONTENT: Final = "[user content omitted]"

# slog's vocabulary is debug/info/warn/error. Python's is DEBUG/INFO/WARNING/
# ERROR/CRITICAL. Mapping to slog's names rather than Python's is what lets one
# log query filter both languages' output; CRITICAL folds into error because the
# Go side has nothing above it and a level nobody can produce is not a level.
_LEVEL_NAMES: Final[dict[int, str]] = {
    logging.DEBUG: "debug",
    logging.INFO: "info",
    logging.WARNING: "warn",
    logging.ERROR: "error",
    logging.CRITICAL: "error",
}

_LEVELS: Final[dict[str, int]] = {
    "debug": logging.DEBUG,
    "info": logging.INFO,
    "warn": logging.WARNING,
    "warning": logging.WARNING,
    "error": logging.ERROR,
}

# Attributes the logging module puts on every record. Anything else came from a
# caller's `extra=` and is subject to redaction.
_STANDARD: Final[frozenset[str]] = frozenset(
    {
        "args",
        "asctime",
        "created",
        "exc_info",
        "exc_text",
        "filename",
        "funcName",
        "levelname",
        "levelno",
        "lineno",
        "module",
        "msecs",
        "msg",
        "message",
        "name",
        "pathname",
        "process",
        "processName",
        "relativeCreated",
        "stack_info",
        "taskName",
        "thread",
        "threadName",
    }
)


# ------------------------------------------------------------- user content


class UserContent:
    """Wraps a value that may contain user content.

    `__str__` and `__repr__` return the marker, so it fails CLOSED through any
    formatter -- including a plain json.dumps(default=str) that has never heard
    of this class. Only the redactor unwraps it, and only at debug level.

    That ordering is the whole design. A marker that failed open would leak the
    first time someone logged through a handler the chassis did not build, and
    the Go implementation's comment says the same thing about slog.LogValuer.
    """

    __slots__ = ("value",)

    def __init__(self, value: Any) -> None:
        self.value = value

    def __str__(self) -> str:
        return OMITTED_USER_CONTENT

    __repr__ = __str__


def user(value: Any) -> UserContent:
    """Mark a value as user content."""
    return UserContent(value)


def prompt(value: str) -> UserContent:
    """Mark an agent prompt as user content."""
    return UserContent(value)


def output(value: str) -> UserContent:
    """Mark model output as user content."""
    return UserContent(value)


def diff(value: str) -> UserContent:
    """Mark a code diff as user content."""
    return UserContent(value)


def is_user_content(value: Any) -> bool:
    return isinstance(value, UserContent)


# --------------------------------------------------------------- correlation

# A single dict in one ContextVar, rather than one ContextVar per field, so
# `bind` is one set-and-reset and cannot leave half a scope behind on an
# exception.
# default=None rather than {}: a mutable default on a ContextVar is shared
# across every context that never set it.
_FIELDS: Final[contextvars.ContextVar[Mapping[str, Any] | None]] = contextvars.ContextVar(
    "halyard_log_fields", default=None
)


@contextlib.contextmanager
def bind(**fields: Any) -> Iterator[None]:
    """Attach correlation fields to every record emitted inside the block.

    Including records from third-party loggers, because the stamping happens in
    the redactor rather than at the call site. That is the same property the Go
    chassis gets from reading the context inside Handle.

    Keys should come from `halyard_chassis.obs.logkey`. A None value is dropped
    rather than stamped, so an unknown org_id does not add a null column to
    every line.

    Caveat, verified: a ContextVar set here does NOT reach work handed to
    `loop.run_in_executor` or `ThreadPoolExecutor.submit` -- both start with an
    empty context, so a sync-def endpoint would silently log the default.
    `asyncio.to_thread` does propagate. Prefer it, or wrap submitted work with
    `contextvars.copy_context().run`.
    """
    merged = {**(_FIELDS.get() or {}), **{k: v for k, v in fields.items() if v is not None}}
    token = _FIELDS.set(merged)
    try:
        yield
    finally:
        _FIELDS.reset(token)


def _trace_fields() -> dict[str, Any]:
    """trace_id and span_id from the ambient span, if tracing is on.

    Separate fields rather than a synthesised correlation id: the join happens
    in the log, where it costs nothing. Imported lazily so this module stays
    usable in a process with no OTel installed.
    """
    try:
        from opentelemetry import trace
    except ImportError:  # pragma: no cover - OTel is a hard dependency
        return {}
    ctx = trace.get_current_span().get_span_context()
    if not ctx.is_valid:
        return {}
    return {
        logkey.TRACE_ID: format(ctx.trace_id, "032x"),
        logkey.SPAN_ID: format(ctx.span_id, "016x"),
        logkey.SAMPLED: bool(ctx.trace_flags.sampled),
    }


# ----------------------------------------------------------------- redaction


def _redact_value(value: Any, *, debug: bool) -> Any:
    """Redact one extra value, recursing into mappings.

    A mapping is slog's group: a secret-shaped key inside one is caught even
    though the outer key was clean, and a secret-shaped OUTER key redacts the
    whole subtree rather than dropping it -- the shape of what was logged stays
    visible, the values do not.
    """
    if isinstance(value, UserContent):
        return value.value if debug else OMITTED_USER_CONTENT
    if isinstance(value, Mapping):
        return {
            k: (REDACTED if is_secret_key(str(k)) else _redact_value(v, debug=debug))
            for k, v in value.items()
        }
    if isinstance(value, list | tuple):
        return [_redact_value(v, debug=debug) for v in value]
    if isinstance(value, str | int | float | bool | None):
        # A string value under a clean key can still be a credential a library
        # put there, so it is scanned even though its key passed.
        if isinstance(value, str) and scan_message(value):
            return REDACTED
        return value
    # An object whose __str__ holds the secret is the case that element-wise
    # inspection misses, so coerce and scan.
    rendered = str(value)
    return REDACTED if scan_message(rendered) else rendered


def _redact(record: logging.LogRecord) -> None:
    """Make `record` safe to emit. Mutates in place.

    In place, deliberately. Python 3.12's return-a-LogRecord filter form exists
    to modify a record "without having side effects on other handlers" -- the
    exact opposite of what is wanted here. The shared-record side effect IS the
    property being relied on.
    """
    debug = record.levelno <= logging.DEBUG

    # Force interpolation before doing anything else: until getMessage() runs,
    # a secret passed as a lazy argument lives in record.args and not in msg.
    message = record.getMessage()
    record.args = ()

    hit = scan_message(message)
    if hit:
        # Keep the fact, drop the payload. Naming the pattern rather than the
        # value tells an operator which integration to fix.
        record.msg = SUPPRESSED_MESSAGE
        for key in [k for k in record.__dict__ if k not in _STANDARD]:
            del record.__dict__[key]
        record.__dict__["suppressed_pattern"] = hit
    else:
        record.msg = message
        for key in [k for k in record.__dict__ if k not in _STANDARD]:
            value = record.__dict__[key]
            record.__dict__[key] = (
                REDACTED if is_secret_key(key) else _redact_value(value, debug=debug)
            )

    # Tracebacks, unconditionally and in this order.
    #
    # Unconditionally because `if not record.exc_text` would skip a cache
    # already populated with plaintext by an earlier handler -- verified to leak
    # from both handlers. And exc_info is cleared afterwards so no formatter can
    # re-render the original exception and undo this.
    #
    # Locals are not printed by the stdlib renderer, but the exception's own
    # str(), PEP 678 notes, the whole __cause__/__context__ chain and the source
    # line of the raise all are. Redacting the formatted string covers all four
    # in one pass.
    if record.exc_info or record.exc_text:
        text = record.exc_text
        if text is None and record.exc_info:
            text = logging.Formatter().formatException(record.exc_info)
        if text and scan_message(text):
            text = SUPPRESSED_MESSAGE
        record.exc_text = text
        record.exc_info = None
    if record.stack_info and scan_message(record.stack_info):
        record.stack_info = SUPPRESSED_MESSAGE

    # Stamp correlation last so a caller cannot shadow it with an extra, and so
    # third-party records get it too.
    for key, value in {**(_FIELDS.get() or {}), **_trace_fields()}.items():
        record.__dict__[key] = value


class RedactingMixin:
    """Applies redaction at Handler.handle, before filters or formatting.

    Mixed into a concrete handler rather than installed as a filter so it cannot
    be removed by `removeFilter`, and so it covers `QueueHandler`, whose
    `prepare` would otherwise interpolate the message before any listener-side
    redactor saw it.
    """

    def handle(self, record: logging.LogRecord) -> bool:
        _redact(record)
        return bool(super().handle(record))  # type: ignore[misc]


class RedactingStreamHandler(RedactingMixin, logging.StreamHandler):  # type: ignore[type-arg]
    """The only handler a chassis service installs."""


# ---------------------------------------------------------------- formatting


def _timestamp(created: float) -> str:
    """RFC 3339 in UTC, to nanosecond width.

    UTC so two replicas' logs interleave correctly. Nine fractional digits to
    match the Go side's format string exactly, even though time.time() carries
    only microseconds -- the last three are always zero, and a log pipeline that
    parses a fixed width should not have to special-case which service emitted
    the line.
    """
    moment = dt.datetime.fromtimestamp(created, tz=dt.UTC)
    return moment.strftime("%Y-%m-%dT%H:%M:%S.") + f"{moment.microsecond:06d}000Z"


class JSONFormatter(logging.Formatter):
    """One JSON object per line, keyed as the Go chassis keys it."""

    def __init__(self, *, base: Mapping[str, Any] | None = None, add_source: bool = False) -> None:
        super().__init__()
        self._base = dict(base or {})
        self._add_source = add_source

    def format(self, record: logging.LogRecord) -> str:
        out: dict[str, Any] = {
            "time": _timestamp(record.created),
            "level": _LEVEL_NAMES.get(record.levelno, record.levelname.lower()),
            "msg": record.getMessage(),
        }
        out.update(self._base)
        if self._add_source:
            # A full build path is noise and leaks the builder's home directory
            # into every line.
            out["source"] = f"{_trim_path(record.pathname)}:{record.lineno}"
        out["logger"] = record.name
        for key, value in record.__dict__.items():
            if key not in _STANDARD:
                out[key] = value
        if record.exc_text:
            out[logkey.STACK] = record.exc_text
        if record.stack_info:
            out[logkey.STACK] = record.stack_info
        # default=str is the last line of defence: an unredacted UserContent
        # that somehow reached the encoder renders as its marker rather than
        # raising, and an arbitrary object renders instead of crashing the
        # logger. Neither should happen; both are worse than a str().
        return json.dumps(out, default=str, separators=(",", ":"))


class TextFormatter(logging.Formatter):
    """Human-readable output for local development."""

    def __init__(self, *, base: Mapping[str, Any] | None = None, add_source: bool = False) -> None:
        super().__init__()
        self._base = dict(base or {})
        self._add_source = add_source

    def format(self, record: logging.LogRecord) -> str:
        level = _LEVEL_NAMES.get(record.levelno, record.levelname.lower())
        parts = [_timestamp(record.created), f"{level:<5}", record.getMessage()]
        fields = {
            **self._base,
            **{k: v for k, v in record.__dict__.items() if k not in _STANDARD},
        }
        if self._add_source:
            fields["source"] = f"{_trim_path(record.pathname)}:{record.lineno}"
        parts.extend(f"{k}={v}" for k, v in fields.items())
        line = " ".join(parts)
        if record.exc_text:
            line = f"{line}\n{record.exc_text}"
        return line


def _trim_path(path: str) -> str:
    marker = "cofound/"
    idx = path.rfind(marker)
    return path[idx + len(marker) :] if idx >= 0 else path.rsplit("/", 1)[-1]


# ------------------------------------------------------------------- install


class LogConfig:
    """What `configure` needs. Mirrors the Go chassis's logging.Config."""

    __slots__ = (
        "add_source",
        "commit",
        "env",
        "fmt",
        "instance_id",
        "level",
        "out",
        "service",
        "version",
    )

    def __init__(
        self,
        *,
        service: str,
        env: str = "",
        version: str = "",
        commit: str = "",
        instance_id: str = "",
        level: str = "info",
        fmt: str = "json",
        add_source: bool = False,
        out: TextIO | None = None,
    ) -> None:
        self.service = service
        self.env = env
        self.version = version
        self.commit = commit
        self.instance_id = instance_id
        self.level = level
        self.fmt = fmt
        self.add_source = add_source
        self.out = out


class LevelKnob:
    """The live log level, so an incident can raise verbosity without a restart."""

    __slots__ = ("_handler", "_logger")

    def __init__(self, logger: logging.Logger, handler: logging.Handler) -> None:
        self._logger = logger
        self._handler = handler

    def get(self) -> str:
        return _LEVEL_NAMES.get(self._logger.level, "info")

    def set(self, level: str) -> None:
        """Raise or lower verbosity. Raising to debug reveals user content."""
        value = _LEVELS.get(level.lower())
        if value is None:
            raise ValueError(f"unknown log level {level!r}; want debug, info, warn or error")
        self._logger.setLevel(value)
        self._handler.setLevel(value)


def configure(cfg: LogConfig) -> LevelKnob:
    """Install the one handler every record goes through.

    Five things happen here that are not obvious, and each closes a verified
    path around redaction:

      * `lastResort` is removed. A library that sets propagate=False without
        adding a handler does not go silent -- `logging.lastResort` writes its
        records straight to stderr at WARNING with the default formatter,
        entirely around this handler.
      * `raiseExceptions` is disabled. Otherwise a formatting error routes to
        `Handler.handleError`, which dumps raw msg and args to stderr; a
        cosmetic bug would become a plaintext leak.
      * Existing root handlers are removed, and any added later is wrapped so
        it redacts too. A library calling `basicConfig(force=True)` otherwise
        removes AND CLOSES this handler, leaving an unredacted StreamHandler in
        its place.
      * Output goes to stdout, not slog's or logging's stderr. 12-factor, and it
        keeps one ordered stream for the collector.
      * The root logger's level is set as well as the handler's, because
        `Logger.isEnabledFor` short-circuits before any handler is consulted.
    """
    level = _LEVELS.get(cfg.level.lower(), logging.INFO)

    base: dict[str, Any] = {}
    for key, value in (
        (logkey.SERVICE, cfg.service),
        (logkey.ENV, cfg.env),
        (logkey.VERSION, cfg.version),
        (logkey.COMMIT, cfg.commit),
        (logkey.INSTANCE, cfg.instance_id),
    ):
        if value:
            base[key] = value

    handler = RedactingStreamHandler(cfg.out or sys.stdout)
    handler.setLevel(level)
    formatter: logging.Formatter
    if cfg.fmt == "text":
        formatter = TextFormatter(base=base, add_source=cfg.add_source)
    else:
        formatter = JSONFormatter(base=base, add_source=cfg.add_source)
    handler.setFormatter(formatter)

    root = logging.getLogger()
    for existing in list(root.handlers):
        root.removeHandler(existing)
    root.addHandler(handler)
    root.setLevel(level)

    logging.lastResort = None
    logging.raiseExceptions = False
    _cover_root_handlers()
    adopt_existing_loggers()

    return LevelKnob(root, handler)


def adopt_existing_loggers() -> int:
    """Make every logger that already exists route through the root handler.

    Libraries imported before `configure` may have set `propagate = False` or
    attached their own handler, and either one is a bypass: with
    `lastResort` removed, a non-propagating logger's records simply vanish, and
    a logger with its own handler emits through something this chassis did not
    build.

    uvicorn is the concrete case. It sets `propagate = False` on
    `uvicorn.access`, which showed up as a boot warning on the first real run of
    this module. Adopting it is better than warning about it: the records either
    reach the redacting handler or they do not exist, and there is no third
    state where they reach stderr unredacted.

    Any handler taken off a logger is wrapped rather than discarded, so a
    library that genuinely needed its own sink keeps it -- redacting.

    Called by `configure`, and again once the server's own modules are imported:
    `uvicorn.access` does not exist at configure time, because uvicorn is
    imported after configuration is read. Returns how many loggers it adopted,
    so a caller can log it.
    """
    adopted = 0
    for logger in list(logging.Logger.manager.loggerDict.values()):
        if not isinstance(logger, logging.Logger):
            continue
        if not logger.propagate or logger.handlers:
            adopted += 1
        for hdlr in list(logger.handlers):
            logger.removeHandler(hdlr)
            _redacting_proxy(hdlr)
            logger.addHandler(hdlr)
        logger.propagate = True
    return adopted


_ORIGINAL_ADD_HANDLER: Final = logging.Logger.addHandler
_ORIGINAL_LAST_RESORT: Final = logging.lastResort


def _redacting_proxy(handler: logging.Handler) -> logging.Handler:
    """Make `handler` redact, whoever installed it.

    Wrapping rather than refusing. The first version of this raised on any
    foreign root handler, which upheld the guarantee and made the chassis
    unusable: pytest's logging plugin, `caplog`, a debugger and any log-shipping
    sidecar all legitimately attach one. Refusing them would push people to call
    `configure` late or not at all, which is worse for the property than
    covering them.

    Idempotent, because redaction is: once a record has been sanitised, scanning
    it again finds nothing. So it does not matter whether the chassis handler or
    a foreign one runs first, and handler ORDER stops being load-bearing --
    which also closes the `exc_text` cache-poisoning path, where a handler
    running before the redactor leaves a plaintext traceback for everyone else
    to reuse.
    """
    if getattr(handler, "_halyard_redacting", False):
        return handler
    original = handler.handle

    def handle(record: logging.LogRecord) -> bool:
        _redact(record)
        return bool(original(record))

    handler.handle = handle  # type: ignore[method-assign]
    handler._halyard_redacting = True  # type: ignore[attr-defined]
    return handler


def _cover_root_handlers() -> None:
    """Ensure every handler on root redacts, including ones added later.

    Not paranoia about hostile code: `logging.basicConfig(force=True)` in a
    dependency's import path removes AND CLOSES existing root handlers and
    installs its own, and every record after that point would be emitted by a
    handler nobody chose. Verified.
    """
    if getattr(logging.Logger.addHandler, "_halyard_guarded", False):
        return

    def guarded(self: logging.Logger, hdlr: logging.Handler) -> None:
        if self is logging.getLogger():
            hdlr = _redacting_proxy(hdlr)
        _ORIGINAL_ADD_HANDLER(self, hdlr)

    guarded._halyard_guarded = True  # type: ignore[attr-defined]
    logging.Logger.addHandler = guarded  # type: ignore[method-assign]


def reset() -> None:
    """Undo `configure`. For tests, and for nothing else.

    A process that configured logging once and then reset it has no redaction,
    so this is deliberately not part of the public surface described in the
    package docstring.
    """
    root = logging.getLogger()
    for handler in list(root.handlers):
        root.removeHandler(handler)
    logging.Logger.addHandler = _ORIGINAL_ADD_HANDLER  # type: ignore[method-assign]
    logging.lastResort = _ORIGINAL_LAST_RESORT
    logging.raiseExceptions = True


def audit_loggers() -> list[str]:
    """Loggers whose records could reach a sink the chassis does not control.

    A logger with `propagate=False` or its own handlers bypasses the root
    handler, and therefore bypasses redaction. Called at boot and logged as a
    warning rather than raised: the right response is usually to reconfigure the
    dependency, and refusing to start would make the chassis hostile to adopt.
    """
    out: list[str] = []
    for name, logger in list(logging.Logger.manager.loggerDict.items()):
        if not isinstance(logger, logging.Logger):
            continue
        if logger.disabled or not logger.propagate or logger.handlers:
            out.append(name)
    return sorted(out)


class Sink:
    """Collects emitted lines for assertions.

    In a non-test module deliberately, exactly as the Go chassis's Sink is: every
    package's tests then assert against real JSON produced by the real
    production handler chain, rather than against a stub that might not share
    the behaviour under test. That is what makes the redaction tests proof
    instead of approximation.
    """

    def __init__(self) -> None:
        self._buf: list[str] = []
        self._lock = threading.Lock()

    def write(self, text: str) -> int:
        with self._lock:
            self._buf.append(text)
        return len(text)

    def flush(self) -> None:
        return None

    @property
    def raw(self) -> str:
        with self._lock:
            return "".join(self._buf)

    def lines(self) -> list[dict[str, Any]]:
        out: list[dict[str, Any]] = []
        for line in self.raw.splitlines():
            if not line.strip():
                continue
            with contextlib.suppress(json.JSONDecodeError):
                out.append(json.loads(line))
        return out

    def last(self) -> dict[str, Any] | None:
        lines = self.lines()
        return lines[-1] if lines else None

    def contains(self, needle: str) -> bool:
        """Whether the raw bytes contain `needle`.

        The assertion that matters for a leak test: it looks at what was
        actually written, not at a parsed view that might normalise something
        away.
        """
        return needle in self.raw

    def reset(self) -> None:
        with self._lock:
            self._buf.clear()
