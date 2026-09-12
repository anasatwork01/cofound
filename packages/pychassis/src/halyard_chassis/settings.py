"""Configuration: read the environment once, report every problem at once.

This is the Python counterpart of packages/chassis/config, and it reads the
SAME environment variables with the SAME defaults. That is the point of the
module, not a detail of it: one deployment manifest configures a Go service and
a Python service, so `HTTP_READ_TIMEOUT=45s` has to mean the same thing in both
or the manifest is lying about one of them. tests/test_settings.py parses the
key constants out of packages/chassis/config/chassis.go and asserts this module
reads exactly that set, so a variable added on the Go side fails the Python
build.

Note the spelling: only HALYARD_ENV carries a prefix. The rest are bare
(HTTP_ADDR, LOG_LEVEL, SHUTDOWN_BUDGET), and the OTel ones use the OTel
specification's own names because the SDK reads some of them itself and an
operator who knows OTel should not have to learn our synonyms.

Three properties are load-bearing.

Accumulating, not fail-fast. An operator with three variables wrong should
learn all three from one restart. A pydantic ValidationError already reports
every field, so most of the work here is rendering it usefully and closing the
two paths where pydantic-settings stops early.

Never echoes a value. A problem names the key and the shape wanted, never what
was received (SPEC 17.2). The boot line renders resolved values, with anything
marked secret reduced to a fingerprint.

Not a singleton. `Settings()` reads os.environ at instantiation, not at class
definition, so a test constructs its own with keyword overrides and two tests in
one session cannot see each other's environment. The Go chassis learned this
the hard way -- a package-level apiconfig global made parallel tests overwrite
each other -- and the Python version of that mistake, `settings = Settings()` at
module scope, additionally crashes at import time, before logging exists to
report it.

Verified 2026-09-10 against pydantic 2.13.5 / pydantic-settings 2.15.0; see
docs/verified.md. Four findings shaped the code and none are obvious from the
documentation:

  * A ValidationError's `input` field holds the entire pre-validation input
    dict, so str(e) on a settings failure prints raw environment values --
    verified leaking a live-shaped API key. Rendering goes exclusively through
    errors(include_input=False, include_url=False), and the original exception
    is never logged or re-raised.
  * SettingsError, raised when a complex field fails to JSON-decode, is not a
    ValidationError subclass and aborts before validation, reporting one problem
    and hiding the rest. Every complex field here is therefore declared NoDecode
    with its own comma-splitting validator, which restores full accumulation.
  * extra="forbid" is inert for os.environ, so a typo'd variable is silently
    ignored and the service starts on a default. Unknown-variable detection is
    built here instead.
  * Go duration strings do not parse. `30s` is a perfectly good
    time.ParseDuration input and a timedelta parse failure, and both chassis
    read the same variables, so this module would reject every duration the Go
    defaults document. parse_go_duration closes that gap.
"""

from __future__ import annotations

import difflib
import hashlib
import ipaddress
import math
import os
import re
import urllib.parse
from collections.abc import Iterable, Mapping
from dataclasses import dataclass
from datetime import timedelta
from typing import Annotated, Any, ClassVar, Literal, Self

from pydantic import AfterValidator, BeforeValidator, Field, ValidationError, model_validator
from pydantic.fields import FieldInfo
from pydantic_settings import BaseSettings, NoDecode, SettingsConfigDict
from pydantic_settings.exceptions import SettingsError

__all__ = [
    "ByteSize",
    "ChassisSettings",
    "ConfigError",
    "ConfigProblem",
    "GoDuration",
    "env_names",
    "fingerprint",
    "format_go_duration",
    "load",
    "parse_bytes",
    "parse_go_duration",
]

Env = Literal["development", "staging", "production"]

# ------------------------------------------------------------------ durations

_DURATION_RE = re.compile(r"(\d+(?:\.\d*)?|\.\d+)(ns|us|µs|μs|ms|s|m|h)")
_DURATION_UNITS: dict[str, float] = {
    "ns": 1e-9,
    "us": 1e-6,
    "µs": 1e-6,
    "μs": 1e-6,
    "ms": 1e-3,
    "s": 1.0,
    "m": 60.0,
    "h": 3600.0,
}


def parse_go_duration(value: Any) -> Any:
    """Accept Go's time.ParseDuration syntax as a timedelta.

    `30s`, `1h30m`, `500ms`, `-1.5h`. Anything this does not recognise passes
    through untouched, so ISO 8601 (`PT30S`) still works and the message for a
    genuinely malformed value is pydantic's own. A bare number is seconds, which
    is what an operator means by `SHUTDOWN_BUDGET=30`.
    """
    if isinstance(value, timedelta | int | float) or not isinstance(value, str):
        return value
    text = value.strip()
    if not text:
        return value
    sign, body = 1.0, text
    if body[0] in "+-":
        sign = -1.0 if body[0] == "-" else 1.0
        body = body[1:]
    if body == "0":
        return timedelta(0)
    matches = list(_DURATION_RE.finditer(body))
    # Every character must be consumed, or "30 seconds" parses as 30 seconds and
    # "30x" as 30 of nothing.
    if not matches or "".join(m.group(0) for m in matches) != body:
        return value
    total = sum(float(m.group(1)) * _DURATION_UNITS[m.group(2)] for m in matches)
    return timedelta(seconds=sign * total)


GoDuration = Annotated[timedelta, BeforeValidator(parse_go_duration)]


def format_go_duration(d: timedelta) -> str:
    """Render a timedelta the way Go's Duration.String does.

    So that `/debug/config` on a Python service and on a Go service can be read
    side by side, and so the boot line echoes the syntax the operator typed.
    Sub-nanosecond precision is not reproduced; timedelta has none.
    """
    total = d.total_seconds()
    if total == 0:
        return "0s"
    sign = "-" if total < 0 else ""
    total = abs(total)
    if total < 1e-6:
        return f"{sign}{round(total * 1e9)}ns"
    if total < 1e-3:
        return f"{sign}{_trim(total * 1e6)}µs"
    if total < 1:
        return f"{sign}{_trim(total * 1e3)}ms"
    hours, rem = divmod(total, 3600.0)
    minutes, seconds = divmod(rem, 60.0)
    out = ""
    if hours:
        out += f"{int(hours)}h"
    if hours or minutes:
        out += f"{int(minutes)}m"
    return f"{sign}{out}{_trim(seconds)}s"


def _trim(v: float) -> str:
    """Render a float without a trailing .0, as Go does."""
    if math.isclose(v, round(v), abs_tol=1e-9):
        return str(round(v))
    return f"{v:g}"


# ---------------------------------------------------------------- byte counts

_BYTE_UNITS: dict[str, int] = {"b": 1, "kb": 1 << 10, "mb": 1 << 20, "gb": 1 << 30}
_BYTES_RE = re.compile(r"^(\d+)\s*(b|kb|mb|gb)$", re.IGNORECASE)


def parse_bytes(value: Any) -> Any:
    """Accept a bare integer or a B/KB/MB/GB suffix, with binary multipliers.

    Matches the Go loader's Bytes accessor, including that KB is 1024 rather
    than 1000 -- the two must agree or HTTP_MAX_BODY_BYTES=1MB admits a
    different body size depending on which service received the request.
    """
    if not isinstance(value, str):
        return value
    text = value.strip()
    if text.isdigit():
        return int(text)
    m = _BYTES_RE.match(text)
    if not m:
        return value
    return int(m.group(1)) * _BYTE_UNITS[m.group(2).lower()]


ByteSize = Annotated[int, BeforeValidator(parse_bytes)]


# ------------------------------------------------------------------ Sentry DSN


def validate_sentry_dsn(value: Any) -> Any:
    """Refuse a SENTRY_DSN that is not an absolute http(s) URL.

    Mirrors the Go loader's SecretURL check on the same key, and for the same
    reason: a DSN that is present but wrong must fail boot rather than start a
    service that reports nothing, forever, silently. The message describes the
    shape wanted and never the value received -- the userinfo is an ingest key.
    """
    if not isinstance(value, str) or value == "":
        return value
    parsed = urllib.parse.urlsplit(value)
    if parsed.scheme not in ("https", "http") or not parsed.hostname:
        raise ValueError("expect an absolute URL with scheme https or http")
    return value


SentryDSN = Annotated[str, AfterValidator(validate_sentry_dsn)]


def _split_csv(value: Any) -> Any:
    if not isinstance(value, str):
        return value
    return [p.strip() for p in value.split(",") if p.strip()]


def _split_key_values(value: Any) -> Any:
    """Split a comma-separated key=value list.

    Never raises: a malformed entry is left as the raw string, so pydantic
    reports it as a dict-shape error on that field while every other field is
    still validated.
    """
    if not isinstance(value, str):
        return value
    out: dict[str, str] = {}
    for part in value.split(","):
        part = part.strip()
        if not part:
            continue
        key, sep, val = part.partition("=")
        if not sep or not key.strip():
            return value
        out[key.strip()] = val.strip()
    return out


# ------------------------------------------------------------------- problems


@dataclass(frozen=True, slots=True)
class ConfigProblem:
    """One thing wrong with the environment."""

    env: str
    """The variable name, as an operator would type it. Empty for a cross-field rule."""

    problem: str
    """The shape wanted. Never contains the value received (SPEC 17.2)."""

    provenance: str
    """"set in the environment", "unset", or a hint."""

    def __str__(self) -> str:
        head = self.env or "(configuration)"
        return f"{head}: {self.problem} ({self.provenance})"


class ConfigError(Exception):
    """Every problem found while reading the environment.

    Deliberately not wrapping the pydantic ValidationError: that object carries
    the raw input values, and this exception exists to be safe to log.
    """

    def __init__(self, problems: Iterable[ConfigProblem]) -> None:
        self.problems = tuple(problems)
        super().__init__(str(self))

    def __str__(self) -> str:
        n = len(self.problems)
        head = f"{n} configuration problem{'s' if n != 1 else ''}:"
        return "\n".join([head, *(f"  {p}" for p in self.problems)])


def fingerprint(rendered: str) -> str:
    """Reduce a secret to something safe to log but still comparable.

    Identical output to the Go loader's, so the two services' boot lines can be
    compared to confirm they were handed the same credential without either
    printing it.
    """
    if rendered in ("", "0"):
        return "unset"
    return f"set (sha256:{hashlib.sha256(rendered.encode()).hexdigest()[:8]})"


def _env_name(prefix: str, field_name: str, info: FieldInfo) -> str:
    alias = info.validation_alias if isinstance(info.validation_alias, str) else info.alias
    return (alias or f"{prefix}{field_name}").upper()


def env_names(cls: type[BaseSettings]) -> dict[str, str]:
    """Map each field name to the environment variable it reads.

    Derived from the fields rather than read out of
    EnvSettingsSource._extract_field_info, a private API that changed behaviour
    in a pydantic-settings minor release. The cost is that AliasChoices is
    unsupported, which is deliberate: a field fed from one of several aliases
    cannot report which one supplied the value, and an error naming the wrong
    variable is worse than none.
    """
    prefix = cls.model_config.get("env_prefix") or ""
    out: dict[str, str] = {}
    for name, info in cls.model_fields.items():
        if info.validation_alias is not None and not isinstance(info.validation_alias, str):
            raise TypeError(
                f"{cls.__name__}.{name} uses a non-string validation_alias. The "
                "chassis does not support AliasChoices: an error message cannot "
                "say which alias supplied the value."
            )
        out[name] = _env_name(prefix, name, info)
    return out


def _render(
    exc: ValidationError, names: Mapping[str, str], environ: Mapping[str, str]
) -> list[ConfigProblem]:
    problems: list[ConfigProblem] = []
    # include_input=False is the load-bearing argument. Left at its default,
    # `input` carries the whole pre-validation dict and this list is not safe
    # to log.
    for err in exc.errors(include_input=False, include_url=False):
        loc = err.get("loc") or ()
        field = str(loc[0]) if loc else ""
        env = names.get(field, field.upper()) if field else ""
        msg = err.get("msg", "is not valid")
        if err.get("type") == "missing":
            problems.append(ConfigProblem(env, "is required but not set", "unset"))
            continue
        where = "set in the environment" if env in environ else "from a default"
        problems.append(ConfigProblem(env, msg, where))
    return problems


def _unknown(
    names: Mapping[str, str], prefixes: Iterable[str], environ: Mapping[str, str]
) -> list[ConfigProblem]:
    """Variables that look like ours and that no field reads.

    extra="forbid" does not do this -- verified inert for os.environ -- so
    HTTP_ADDDR is dropped without comment and the service starts on :8080 while
    the typo sits in the environment. Reported as a problem rather than a
    warning, because a variable an operator believes is taking effect and is not
    is the same class of failure as a missing one.

    Scoped to the prefixes the chassis owns. A bare namespace like HTTP_ is
    shared with nothing else in a container we build, but PATH and HOME are not
    ours to complain about.
    """
    known = set(names.values())
    pfx = tuple(prefixes)
    out: list[ConfigProblem] = []
    for key in sorted(environ):
        upper = key.upper()
        if upper in known or not upper.startswith(pfx):
            continue
        near = difflib.get_close_matches(upper, sorted(known), n=1, cutoff=0.75)
        hint = f"did you mean {near[0]}?" if near else "not a known setting"
        out.append(ConfigProblem(upper, "is not read by this service", hint))
    return out


def load[S: "ChassisSettings"](
    cls: type[S], *, environ: Mapping[str, str] | None = None, **overrides: Any
) -> S:
    """Read `cls` from the environment, reporting every problem at once.

    Raises ConfigError, whose message is safe to log and print. `overrides` go
    straight to the constructor, which is how a test supplies configuration
    without touching os.environ.
    """
    env = os.environ if environ is None else environ
    names = env_names(cls)
    problems = _unknown(names, cls.owned_prefixes, env)
    try:
        settings = cls(**overrides)
    except ValidationError as exc:
        problems.extend(_render(exc, names, env))
    except SettingsError as exc:
        # Not a ValidationError subclass, and raised before validation runs.
        # Every complex field below is NoDecode specifically so this path stays
        # unreachable; it is caught anyway so a future field cannot make the
        # loader raise a bare ValueError naming no variable.
        problems.append(ConfigProblem("", str(exc), "could not be decoded"))
    else:
        if problems:
            raise ConfigError(problems)
        return settings
    raise ConfigError(problems)


class ChassisSettings(BaseSettings):
    """The configuration every Halyard Python service has.

    A service subclasses this, adds its own fields with explicit aliases, and
    extends `secret_env` and `owned_prefixes`. Subclasses must restate any
    model_config key they mean to change, including to a falsy value:
    SettingsConfigDict merges across the MRO, so omitting a key keeps the base's
    value rather than clearing it.
    """

    model_config = SettingsConfigDict(
        # No prefix. Every field carries an explicit alias, because the names
        # are a contract with the Go chassis rather than something derived.
        env_prefix="",
        case_sensitive=False,
        populate_by_name=True,
        # The Go loader's raw() treats a value that is blank after TrimSpace as
        # absent. This is the same rule, and it matters: Docker Compose,
        # Kubernetes and shell templating all emit FOO= for an unset value, and
        # without it HTTP_ADDR= raises against a field with a good default.
        env_ignore_empty=True,
        # Not "forbid". A shared env file holding several services' variables
        # would make every service refuse to start over a key it does not read;
        # _unknown() above does prefix-scoped detection instead, which is what
        # forbid does not do for os.environ anyway.
        extra="ignore",
    )

    # Variables whose resolved value is fingerprinted rather than logged. The
    # Go loader marks KeyValues secret automatically; this is the explicit list.
    secret_env: ClassVar[frozenset[str]] = frozenset({"OTEL_EXPORTER_OTLP_HEADERS", "SENTRY_DSN"})

    # Namespaces this service owns, for unknown-variable detection.
    owned_prefixes: ClassVar[tuple[str, ...]] = (
        "HALYARD_",
        "HTTP_",
        "ADMIN_",
        "LOG_",
        "SHUTDOWN_",
        "READINESS_",
        "TRUSTED_PROXY_",
        "TRUST_INBOUND_",
    )

    # ---- identity. Not read from the environment: the Makefile's -X ldflags
    # equivalents are passed in by the service's __main__, so a running binary
    # cannot be told it is a different service by its environment.
    service: Annotated[str, Field(min_length=1, exclude=True)]
    version: Annotated[str, Field(exclude=True)] = "0.0.0"
    commit: Annotated[str, Field(exclude=True)] = ""

    env: Annotated[Env, Field(validation_alias="HALYARD_ENV")] = "development"
    instance_id: Annotated[str, Field(validation_alias="INSTANCE_ID")] = ""

    addr: Annotated[str, Field(validation_alias="HTTP_ADDR")] = ":8080"
    admin_addr: Annotated[str, Field(validation_alias="ADMIN_ADDR")] = ""
    """Probes, pprof-equivalents and the resolved config. Empty disables it."""

    log_level: Annotated[
        Literal["debug", "info", "warn", "error"], Field(validation_alias="LOG_LEVEL")
    ] = "info"
    log_format: Annotated[Literal["json", "text"], Field(validation_alias="LOG_FORMAT")] = "json"
    log_source: Annotated[bool | None, Field(validation_alias="LOG_SOURCE")] = None
    """None means "default by environment": on outside production."""

    otlp_endpoint: Annotated[str, Field(validation_alias="OTEL_EXPORTER_OTLP_ENDPOINT")] = ""
    """Empty means tracing is off. OTEL_SDK_DISABLED is not used; see telemetry.py."""

    otlp_protocol: Annotated[
        Literal["http/protobuf", "grpc"], Field(validation_alias="OTEL_EXPORTER_OTLP_PROTOCOL")
    ] = "http/protobuf"

    otlp_headers: Annotated[
        dict[str, str],
        NoDecode,
        Field(validation_alias="OTEL_EXPORTER_OTLP_HEADERS", default_factory=dict),
    ]

    otel_sample_ratio: Annotated[
        float | None, Field(validation_alias="OTEL_TRACES_SAMPLER_ARG", ge=0.0, le=1.0)
    ] = None
    """None means "inherit the parent's decision", which is not the same as 1.0."""

    otel_shutdown_timeout: Annotated[
        GoDuration, Field(validation_alias="OTEL_SHUTDOWN_TIMEOUT")
    ] = timedelta(seconds=5)

    sentry_dsn: Annotated[SentryDSN, Field(validation_alias="SENTRY_DSN")] = ""
    """SPEC 17.3's error reporting. Empty means no reporter, and that is the
    only switch -- see packages/chassis/observability/sentry.Init, where an
    empty DSN must never reach the SDK because a "disabled" sentry-go client
    still runs a 100ms ticker for the life of the process.

    Bound here so the two chassis read one manifest. Task 0.13 wires the Go
    reporter only; no Python service constructs one yet, so on sandboxd and
    workers this currently resolves and is reported in the boot line and
    nothing more."""

    http_read_header_timeout: Annotated[
        GoDuration, Field(validation_alias="HTTP_READ_HEADER_TIMEOUT")
    ] = timedelta(seconds=5)
    http_read_timeout: Annotated[GoDuration, Field(validation_alias="HTTP_READ_TIMEOUT")] = (
        timedelta(seconds=30)
    )
    http_write_timeout: Annotated[GoDuration, Field(validation_alias="HTTP_WRITE_TIMEOUT")] = (
        timedelta(seconds=30)
    )
    """Non-zero on purpose: it protects every JSON endpoint from a slow client.
    A streaming response clears it per-request, because only a write deadline
    cuts a live stream and only the stream knows it is one."""

    http_idle_timeout: Annotated[GoDuration, Field(validation_alias="HTTP_IDLE_TIMEOUT")] = (
        timedelta(seconds=120)
    )
    http_handler_timeout: Annotated[GoDuration, Field(validation_alias="HTTP_HANDLER_TIMEOUT")] = (
        timedelta(seconds=15)
    )
    http_max_header_bytes: Annotated[
        ByteSize, Field(validation_alias="HTTP_MAX_HEADER_BYTES", gt=0)
    ] = 1 << 20
    http_max_header_values: Annotated[
        int, Field(validation_alias="HTTP_MAX_HEADER_VALUES", ge=1, le=100_000)
    ] = 500
    http_max_body_bytes: Annotated[
        ByteSize, Field(validation_alias="HTTP_MAX_BODY_BYTES", gt=0)
    ] = 1 << 20

    shutdown_deregister_delay: Annotated[
        GoDuration | None, Field(validation_alias="SHUTDOWN_DEREGISTER_DELAY")
    ] = None
    """None means "default by environment": zero in development, because waiting
    three seconds for a load balancer that is not there makes every local
    restart feel broken."""

    shutdown_drain_timeout: Annotated[
        GoDuration, Field(validation_alias="SHUTDOWN_DRAIN_TIMEOUT")
    ] = timedelta(seconds=10)
    shutdown_server_timeout: Annotated[
        GoDuration, Field(validation_alias="SHUTDOWN_SERVER_TIMEOUT")
    ] = timedelta(seconds=5)
    shutdown_telemetry_timeout: Annotated[
        GoDuration, Field(validation_alias="SHUTDOWN_TELEMETRY_TIMEOUT")
    ] = timedelta(seconds=5)
    shutdown_budget: Annotated[GoDuration, Field(validation_alias="SHUTDOWN_BUDGET")] = timedelta(
        seconds=25
    )

    readiness_interval: Annotated[GoDuration, Field(validation_alias="READINESS_INTERVAL")] = (
        timedelta(seconds=2)
    )
    readiness_timeout: Annotated[GoDuration, Field(validation_alias="READINESS_TIMEOUT")] = (
        timedelta(seconds=1)
    )
    readiness_stale_after: Annotated[
        GoDuration, Field(validation_alias="READINESS_STALE_AFTER")
    ] = timedelta(seconds=10)
    readiness_detail: Annotated[bool, Field(validation_alias="READINESS_DETAIL")] = False

    trusted_proxies: Annotated[
        list[ipaddress.IPv4Network | ipaddress.IPv6Network],
        NoDecode,
        Field(validation_alias="TRUSTED_PROXY_CIDRS", default_factory=list),
    ]
    trust_inbound_request_id: Annotated[
        bool, Field(validation_alias="TRUST_INBOUND_REQUEST_ID")
    ] = False

    # ------------------------------------------------------------- validators

    @model_validator(mode="before")
    @classmethod
    def _normalise(cls, data: Any) -> Any:
        """Strip whitespace and split the comma-separated fields.

        Stripping matters because int parsing tolerates " 42 " and bool parsing
        rejects " true ", so a stray trailing space in a compose file would
        break only the boolean settings and produce an error that contradicts
        the visibly-correct value in the file.

        The splitting is here rather than in a field validator because these are
        NoDecode fields: without an explicit decode, pydantic-settings would try
        JSON and raise SettingsError, which aborts before validation and hides
        every other problem.
        """
        if not isinstance(data, dict):
            return data
        # A value that is blank AFTER stripping is dropped, not passed through
        # as "". env_ignore_empty only sees the untrimmed string, so without
        # this HTTP_ADDR="   " would bind the empty string while the Go
        # chassis -- whose raw() trims first -- falls back to the default.
        out: dict[str, Any] = {}
        for k, v in data.items():
            if isinstance(v, str):
                stripped = v.strip()
                if not stripped:
                    continue
                out[k] = stripped
            else:
                out[k] = v
        # Split only -- never validate. A model validator that raises aborts
        # before any field is checked, which is the same accumulation failure
        # SettingsError causes and the reason these fields are NoDecode in the
        # first place. Splitting produces a list or dict that pydantic then
        # validates per item, so a bad CIDR is reported alongside every other
        # problem instead of instead of them.
        for key in ("TRUSTED_PROXY_CIDRS", "trusted_proxies"):
            if key in out:
                out[key] = _split_csv(out[key])
        for key in ("OTEL_EXPORTER_OTLP_HEADERS", "otlp_headers"):
            if key in out:
                out[key] = _split_key_values(out[key])
        return out

    @model_validator(mode="after")
    def _apply_env_defaults(self) -> Self:
        """Resolve the two defaults that depend on HALYARD_ENV."""
        if self.log_source is None:
            object.__setattr__(self, "log_source", self.env != "production")
        if self.shutdown_deregister_delay is None:
            delay = timedelta(0) if self.env == "development" else timedelta(seconds=3)
            object.__setattr__(self, "shutdown_deregister_delay", delay)
        return self

    @model_validator(mode="after")
    def _cross_field_rules(self) -> Self:
        """The rules no per-field constraint can express.

        These are the Go chassis's six, in its order, with its reasons. They are
        product decisions rather than type checks, which is why they live in
        ordinary code in both languages.
        """
        problems: list[str] = []

        # 1. Debug logging is where user content is revealed (SPEC 17.3), so in
        #    production it is a privacy setting, not a verbosity one.
        if self.env == "production" and self.log_level == "debug":
            problems.append(
                "LOG_LEVEL: expect info or higher when HALYARD_ENV=production; "
                "debug reveals user content (SPEC 17.3)"
            )

        # 2. The phases must fit inside the orchestrator's termination grace
        #    period. Overrunning it means SIGKILL mid-write, which is exactly
        #    the unclean cut that Last-Event-ID resume exists to make
        #    unnecessary.
        total = (
            (self.shutdown_deregister_delay or timedelta(0))
            + self.shutdown_drain_timeout
            + self.shutdown_server_timeout
            + self.shutdown_telemetry_timeout
        )
        if total > self.shutdown_budget:
            problems.append(
                f"shutdown phases total {format_go_duration(total)} but SHUTDOWN_BUDGET is "
                f"{format_go_duration(self.shutdown_budget)}; lower "
                "SHUTDOWN_DEREGISTER_DELAY, SHUTDOWN_DRAIN_TIMEOUT, "
                "SHUTDOWN_SERVER_TIMEOUT or SHUTDOWN_TELEMETRY_TIMEOUT, or raise the budget"
            )

        # 3. An idle timeout below the header timeout closes keep-alive
        #    connections before a slow client finishes its headers.
        if self.http_idle_timeout <= self.http_read_header_timeout:
            problems.append(
                "HTTP_IDLE_TIMEOUT: expect a value greater than HTTP_READ_HEADER_TIMEOUT "
                f"({format_go_duration(self.http_read_header_timeout)})"
            )

        # 4. A stale window under two intervals marks healthy probes unknown on
        #    a single slow poll, which reads as a flapping outage.
        if self.readiness_stale_after <= 2 * self.readiness_interval:
            problems.append(
                "READINESS_STALE_AFTER: expect a value greater than twice READINESS_INTERVAL "
                f"({format_go_duration(2 * self.readiness_interval)})"
            )
        if self.readiness_timeout >= self.readiness_interval:
            problems.append(
                "READINESS_TIMEOUT: expect a value below READINESS_INTERVAL "
                f"({format_go_duration(self.readiness_interval)})"
            )

        # 5. The resolved configuration and the log-level knob are not public
        #    surfaces.
        if (
            self.env == "production"
            and self.admin_addr
            and not _is_loopback_or_private(self.admin_addr)
        ):
            problems.append(
                "ADMIN_ADDR: expect a loopback or private address in production; "
                "it exposes the resolved configuration and the log-level control"
            )

        # 6. /readyz is unauthenticated, and detail includes probe error strings.
        if self.env == "production" and self.readiness_detail:
            problems.append(
                "READINESS_DETAIL: expect false in production; /readyz is "
                "unauthenticated and detail includes error strings"
            )

        if problems:
            raise ValueError("; ".join(problems))
        return self

    # ---------------------------------------------------------------- output

    def resolved(self) -> dict[str, str]:
        """The effective configuration, keyed by environment variable name.

        For the one boot line that records what the process actually read, and
        for the admin server's config endpoint. Anything in `secret_env` is
        reduced to a fingerprint; a collection renders as its size, because the
        contents of TRUSTED_PROXY_CIDRS are operational detail and the contents
        of OTEL_EXPORTER_OTLP_HEADERS are a credential.
        """
        names = env_names(type(self))
        out: dict[str, str] = {}
        for field, env in names.items():
            if type(self).model_fields[field].exclude:
                continue
            value = getattr(self, field)
            if isinstance(value, timedelta):
                rendered = format_go_duration(value)
            elif isinstance(value, dict | list | tuple | set):
                rendered = str(len(value))
            elif isinstance(value, bool):
                rendered = "true" if value else "false"
            elif value is None:
                rendered = ""
            else:
                rendered = str(value)
            out[env] = fingerprint(rendered) if env in self.secret_env else rendered
        return out

    @property
    def is_production(self) -> bool:
        return self.env == "production"


def _is_loopback_or_private(addr: str) -> bool:
    host = addr.rsplit(":", 1)[0] if ":" in addr else addr
    host = host.strip("[]")
    if host == "":
        # A bare ":9090" binds every interface.
        return False
    if host == "localhost":
        return True
    try:
        ip = ipaddress.ip_address(host)
    except ValueError:
        # A hostname we cannot classify; assume it is routable.
        return False
    if ip.is_unspecified:
        # 0.0.0.0 and :: bind EVERY interface, which is the most routable an
        # address gets. Checked before is_private because Python puts 0.0.0.0
        # in 0.0.0.0/8 and reports it private, while Go's netip.IsPrivate --
        # RFC 1918 only -- does not. Without this the production guard passes
        # for the one value it most needs to catch.
        return False
    return ip.is_loopback or ip.is_private or ip.is_link_local
