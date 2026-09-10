"""Tracing setup, and the SPEC 17.3 tenancy tuple on spans.

The Python counterpart of packages/chassis/telemetry. It keeps that package's
two load-bearing properties:

  * It boots with no collector. An empty endpoint yields no provider at all --
    not a provider with a disabled exporter. OTEL_SDK_DISABLED is not used, for
    the same reason the Go chassis does not use it and a different underlying
    fault: in Go it is unimplemented, and in Python it is implemented but
    (a) only recognises the literal string "true", so OTEL_SDK_DISABLED=1
    leaves the SDK fully enabled while the operator believes telemetry is off,
    and (b) still constructs the provider, runs resource detection, builds the
    exporter and starts the batch processor's daemon thread. Neither is a
    disabled mode worth having.
  * The composite propagator is installed in EVERY mode, including no-op, so an
    inbound traceparent still passes through a service with tracing off and
    outbound hops keep carrying it.

Verified 2026-09-10 against opentelemetry-python 1.44.0 / 0.65b0; see
docs/verified.md. Four findings changed this code:

  * There is no set_error_handler. GlobalErrorHandler and the
    `opentelemetry_error_handler` entry point -- the documented mechanism --
    have zero call sites in the SDK, exporter or instrumentation packages. The
    real seam is a logging.Handler on the "opentelemetry" logger, so porting
    Go's otel.SetErrorHandler directly would have produced dead code.
  * TracerProvider.shutdown() takes no timeout and is registered with atexit by
    default, so a hanging collector can add up to 30 seconds to termination --
    past most SIGTERM grace periods, turning a clean deploy into a SIGKILL. The
    provider is built with shutdown_on_exit=False and the batch processor's own
    timeout-bearing shutdown is called instead.
  * force_flush(timeout_millis=N) ignores N: it blocks for as long as the export
    takes and returns True regardless. It is confined to the shutdown path and
    its return value is not treated as proof the deadline was met.
  * The OTLP/HTTP exporter is a blocking requests.Session.post, so
    SimpleSpanProcessor -- the obvious choice, and easy to leave in an example
    -- blocks the event loop on an HTTP POST per span. The processor choice is a
    property of this module, not a caller's parameter.
"""

from __future__ import annotations

import logging
from dataclasses import dataclass
from typing import Any

from opentelemetry import baggage, propagate, trace
from opentelemetry.baggage.propagation import W3CBaggagePropagator
from opentelemetry.context import Context, attach, detach
from opentelemetry.propagators.composite import CompositePropagator
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor, SpanExporter
from opentelemetry.sdk.trace.sampling import ALWAYS_ON, ParentBased, TraceIdRatioBased
from opentelemetry.trace.propagation.tracecontext import TraceContextTextMapPropagator

from .obs import attrs, logkey

__all__ = [
    "Telemetry",
    "TelemetryConfig",
    "annotate_span",
    "carry_baggage",
    "install_propagators",
    "read_baggage",
    "setup",
]

_INTERNAL_LOGGER = "opentelemetry"


@dataclass(frozen=True, slots=True)
class TelemetryConfig:
    """What `setup` needs, taken from ChassisSettings."""

    service: str
    version: str = ""
    env: str = ""
    instance_id: str = ""
    endpoint: str = ""
    protocol: str = "http/protobuf"
    headers: dict[str, str] | None = None
    sample_ratio: float | None = None


class Telemetry:
    """A configured (or deliberately absent) tracing pipeline."""

    __slots__ = ("_processor", "_provider", "enabled")

    def __init__(
        self,
        *,
        provider: TracerProvider | None = None,
        processor: BatchSpanProcessor | None = None,
    ) -> None:
        self._provider = provider
        self._processor = processor
        self.enabled = provider is not None

    async def shutdown(self, budget: float) -> None:
        """Flush and stop, within `budget` seconds.

        Named a budget rather than a timeout because it is forwarded INTO a
        blocking SDK call. An `asyncio.timeout` around this would return control
        to the caller without stopping the worker thread, so the deadline has to
        be the SDK's own.

        Called last in the shutdown sequence, after the server has stopped, so
        that the spans describing the shutdown itself are exported. Runs the
        blocking SDK calls on a worker thread: the exporter is
        requests-based, and doing this on the event loop would stall any
        remaining task -- including the ones being drained.

        Bounding it is the awkward part, and the awkwardness is upstream's.
        Measured against 1.44.0: TracerProvider.shutdown() takes no timeout,
        BatchSpanProcessor.shutdown() takes no timeout either, and
        force_flush(timeout_millis=N) accepts N and then ignores it -- it blocks
        for as long as the export takes and returns True regardless. So there is
        no API here that can be asked to give up.

        What bounds it is `wait_for` over a worker thread: the flush and stop
        run off the event loop, and if the budget expires the coroutine returns
        while the thread finishes on its own. Abandoning it is safe because the
        processor's worker is a daemon thread, so it cannot hold up process
        exit -- verified. Some spans may be lost, which is the correct trade
        against a SIGKILL mid-shutdown.
        """
        if self._provider is None:
            return
        import asyncio

        def stop() -> None:
            if self._processor is not None:
                self._processor.force_flush(int(budget * 1000))
            if self._provider is not None:
                self._provider.shutdown()

        try:
            await asyncio.wait_for(asyncio.to_thread(stop), timeout=budget)
        except TimeoutError:
            logging.getLogger(__name__).warning(
                "telemetry shutdown exceeded its budget; spans may be lost",
                extra={logkey.COMPONENT: "telemetry", logkey.PHASE: "shutdown"},
            )


def install_propagators() -> None:
    """Install the composite propagator, programmatically.

    Not via OTEL_PROPAGATORS: that variable is read once at import time of
    opentelemetry.propagate into a module global, so setting it from a Python
    settings module -- the natural place -- is a silent no-op, and a service
    intended to emit traceparent only would keep emitting baggage across its
    trust boundary.

    Called in every mode, including with tracing disabled, so a service with no
    collector still forwards an inbound traceparent rather than breaking the
    trace at itself.
    """
    propagate.set_global_textmap(
        CompositePropagator([TraceContextTextMapPropagator(), W3CBaggagePropagator()])
    )


def setup(cfg: TelemetryConfig, *, exporter: SpanExporter | None = None) -> Telemetry:
    """Build the tracing pipeline, or deliberately none of it.

    Pass `exporter` in tests: an InMemorySpanExporter with this same batch
    processor exercises the real pipeline without a collector.
    """
    _install_internal_error_logging()
    install_propagators()

    if not cfg.endpoint and exporter is None:
        # No provider at all. The API's default no-op tracer is already
        # installed, so trace.get_tracer(...) works and costs nothing.
        return Telemetry()

    if exporter is None:
        exporter = _build_exporter(cfg)

    # service.name comes from the chassis's own configuration rather than being
    # left to OTEL_SERVICE_NAME, because the service identity is passed in by
    # the service's __main__ and is not something the environment may override.
    # Explicit attributes merge last and outrank the env var, so this makes
    # OTEL_SERVICE_NAME inert -- deliberately, and documented in the env
    # reference.
    #
    # No schema_url: Resource.merge with conflicting non-empty schema URLs
    # discards the incoming resource's attributes entirely and reports it only
    # as an ERROR log record, so resource attributes vanish with no exception
    # and no failing test.
    resource = Resource.create(
        {
            k: v
            for k, v in {
                "service.name": cfg.service,
                "service.version": cfg.version,
                "service.instance.id": cfg.instance_id,
                "deployment.environment.name": cfg.env,
            }.items()
            if v
        }
    )

    sampler = (
        ParentBased(TraceIdRatioBased(cfg.sample_ratio))
        if cfg.sample_ratio is not None
        else ParentBased(ALWAYS_ON)
    )

    # shutdown_on_exit=False: the atexit hook cannot be given a timeout, and a
    # hanging collector would add up to 30 seconds to termination.
    provider = TracerProvider(resource=resource, sampler=sampler, shutdown_on_exit=False)
    processor = BatchSpanProcessor(exporter)
    provider.add_span_processor(processor)
    trace.set_tracer_provider(provider)
    return Telemetry(provider=provider, processor=processor)


def _build_exporter(cfg: TelemetryConfig) -> SpanExporter:
    """The OTLP exporter, HTTP only.

    grpc is accepted by the configuration because the OTel specification
    defines it and an operator may have a collector that wants it, but only the
    HTTP exporter is a dependency: opentelemetry-exporter-otlp-proto-grpc pulls
    grpcio, which is the bloat the Go chassis restructured a package to avoid.
    A grpc request therefore fails loudly at boot rather than silently falling
    back to a protocol the collector is not listening for.
    """
    if cfg.protocol == "grpc":
        raise RuntimeError(
            "OTEL_EXPORTER_OTLP_PROTOCOL=grpc needs "
            "opentelemetry-exporter-otlp-proto-grpc, which is not a dependency of "
            "this chassis because it pulls grpcio. Use http/protobuf, or add the "
            "package and record the SPEC 22 verification in docs/verified.md."
        )
    from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter

    # The endpoint is passed through as configured. OTEL_EXPORTER_OTLP_ENDPOINT
    # gets /v1/traces appended by the SDK; the signal-specific variable is used
    # verbatim, so a bare host there 404s on every export and surfaces only as
    # an ERROR on the exporter's logger. The env reference documents both forms.
    return OTLPSpanExporter(endpoint=cfg.endpoint or None, headers=cfg.headers or None)


class _DedupeFilter(logging.Filter):
    """Collapse repeats of the SDK's own error records.

    Trace-export failures are logged once per failed batch with no
    deduplication -- the SDK's DuplicateFilter exists but is wired only to the
    logs-export path -- so a collector outage produces an unbounded stream of
    identical ERROR records. That is noise, and cost if those logs ship
    anywhere.
    """

    def __init__(self) -> None:
        super().__init__()
        self._seen: set[str] = set()

    def filter(self, record: logging.LogRecord) -> bool:
        key = f"{record.name}:{record.levelno}:{record.msg}"
        if key in self._seen:
            return False
        self._seen.add(key)
        return True


def _install_internal_error_logging() -> None:
    """Route the SDK's internal errors into the chassis logger.

    This is the seam Go gets from otel.SetErrorHandler. Python has no
    equivalent: GlobalErrorHandler is never called by the SDK, exporter or
    instrumentation. The "opentelemetry" logger is where those errors actually
    go, and it propagates to root, so the only work here is deduplication and a
    component tag.
    """
    logger = logging.getLogger(_INTERNAL_LOGGER)
    if any(isinstance(f, _DedupeFilter) for f in logger.filters):
        return
    logger.addFilter(_DedupeFilter())


# ------------------------------------------------------------ span attributes


def annotate_span(
    *,
    org_id: str | None = None,
    project_id: str | None = None,
    session_id: str | None = None,
    turn: int | None = None,
    request_id: str | None = None,
    error_code: str | None = None,
    stream_kind: str | None = None,
) -> None:
    """Put the SPEC 17.3 tenancy tuple on the current span.

    The keys are generated from packages/schema/observability.json, so they are
    the same keys the Go services use and the two halves of a trace join.
    """
    span = trace.get_current_span()
    if not span.is_recording():
        return
    for key, value in (
        (attrs.SPAN_ORG_ID, org_id),
        (attrs.SPAN_PROJECT_ID, project_id),
        (attrs.SPAN_SESSION_ID, session_id),
        (attrs.SPAN_TURN, turn),
        (attrs.SPAN_REQUEST_ID, request_id),
        (attrs.SPAN_ERROR_CODE, error_code),
        (attrs.SPAN_STREAM_KIND, stream_kind),
    ):
        if value is not None:
            span.set_attribute(key, value)


def read_baggage() -> dict[str, str]:
    """The tenancy members carried in inbound baggage."""
    out: dict[str, str] = {}
    for member, _ in attrs.BAGGAGE_MEMBERS:
        value = baggage.get_baggage(member)
        if value is not None:
            out[member] = str(value)
    return out


def carry_baggage(**values: Any) -> object:
    """Attach tenancy values to baggage so outbound calls carry them.

    Returns a token to pass to `release_baggage`. The two-call shape is not
    ceremony: `baggage.set_baggage` returns a NEW Context and does not mutate
    the ambient one, so code that calls it and drops the return value
    type-checks, runs, and propagates nothing.

        token = carry_baggage(**{attrs.BAGGAGE_ORG_ID: org_id})
        try:
            ...
        finally:
            release_baggage(token)
    """
    ctx: Context | None = None
    for key, value in values.items():
        if value is None:
            continue
        ctx = baggage.set_baggage(key, str(value), context=ctx)
    if ctx is None:
        return None
    return attach(ctx)


def release_baggage(token: object) -> None:
    """Detach what `carry_baggage` attached."""
    if token is not None:
        detach(token)  # type: ignore[arg-type]
