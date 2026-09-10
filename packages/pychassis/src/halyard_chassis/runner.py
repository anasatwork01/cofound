"""`run` is the single entrypoint every Halyard Python service calls.

The counterpart of chassis.Main. A service's __main__ is expected to be about
fifteen lines: describe itself, name its settings class, register its probes and
routers, and hand over.

    from halyard_chassis import Service, run

    def main() -> int:
        return run(Service(
            name="sandboxd",
            version=__version__,
            settings_class=SandboxdSettings,
            build=build,
        ))

Exit codes follow the same convention as the Go chassis: 78 (EX_CONFIG) for a
configuration problem, so an orchestrator can tell "this will never start" from
"this crashed and might succeed on a retry".

One behaviour is worth knowing before reading a shutdown log: a graceful stop
exits 143, not 0. uvicorn's `capture_signals` restores the original handlers and
then re-raises the captured signal, so a service shut down by SIGTERM dies BY
SIGNAL. Verified. Treat 143 as success in dashboards and in tests; asserting
exit code 0 on a graceful shutdown asserts something that has never been true.
"""

from __future__ import annotations

import contextlib
import logging
import os
import socket
import sys
from collections.abc import AsyncIterator, Callable, Iterable
from dataclasses import dataclass, field
from datetime import timedelta
from typing import Any

from . import logs, telemetry
from .health import Registry
from .lifecycle import Shutdown, ShutdownConfig
from .logs import LogConfig
from .obs import logkey
from .settings import ChassisSettings, ConfigError, load
from .telemetry import TelemetryConfig

__all__ = ["EX_CONFIG", "Service", "Setup", "run"]

_log = logging.getLogger(__name__)

# sysexits.h. An orchestrator can distinguish "misconfigured, will never start"
# from "crashed, a restart might work".
EX_CONFIG = 78


@dataclass(slots=True)
class Setup:
    """What a service is handed, and what it fills in.

    Passed to `Service.build`, which registers probes, routers and closers. A
    value rather than a set of globals, so two services in one test session
    share nothing -- the same reason the Go chassis converted its config away
    from a package-level global.
    """

    settings: Any
    registry: Registry
    shutdown: Shutdown
    routers: list[Any] = field(default_factory=list)
    stream_routes: list[str] = field(default_factory=list)
    """Route paths exempt from the per-request deadline."""


@dataclass(slots=True)
class Service:
    """A service's description of itself."""

    name: str
    build: Callable[[Setup], None]
    version: str = "0.0.0"
    commit: str = ""
    settings_class: type[ChassisSettings] = ChassisSettings
    stream_routes: Iterable[str] = ()


def run(service: Service, *, argv: list[str] | None = None) -> int:
    """Read configuration, build the app, serve, and stop in order."""
    args = sys.argv[1:] if argv is None else argv
    if "--version" in args:
        print(f"{service.name} {service.version}")
        return 0

    try:
        settings = load(
            service.settings_class,
            service=service.name,
            version=service.version,
            commit=service.commit,
        )
    except ConfigError as exc:
        # Before logging is configured, so this goes to stderr directly. It is
        # safe to print: ConfigError carries key names and expected shapes,
        # never values.
        print(str(exc), file=sys.stderr)
        return EX_CONFIG

    level = logs.configure(
        LogConfig(
            service=settings.service,
            env=settings.env,
            version=settings.version,
            commit=settings.commit,
            instance_id=settings.instance_id or _default_instance_id(),
            level=settings.log_level,
            fmt=settings.log_format,
            add_source=bool(settings.log_source),
        )
    )

    tel = telemetry.setup(
        TelemetryConfig(
            service=settings.service,
            version=settings.version,
            env=settings.env,
            instance_id=settings.instance_id or _default_instance_id(),
            endpoint=settings.otlp_endpoint,
            protocol=settings.otlp_protocol,
            headers=settings.otlp_headers or None,
            sample_ratio=settings.otel_sample_ratio,
        )
    )

    registry = Registry(
        interval=settings.readiness_interval.total_seconds(),
        timeout=settings.readiness_timeout.total_seconds(),
        stale_after=settings.readiness_stale_after.total_seconds(),
        detail=settings.readiness_detail,
    )
    shutdown = Shutdown(
        ShutdownConfig(
            deregister_delay=(settings.shutdown_deregister_delay or timedelta(0)).total_seconds(),
            drain_timeout=settings.shutdown_drain_timeout.total_seconds(),
            server_timeout=settings.shutdown_server_timeout.total_seconds(),
            telemetry_timeout=settings.shutdown_telemetry_timeout.total_seconds(),
            budget=settings.shutdown_budget.total_seconds(),
        )
    )

    setup = Setup(settings=settings, registry=registry, shutdown=shutdown)
    setup.stream_routes.extend(service.stream_routes)
    service.build(setup)

    app = _build(setup, tel, level)
    return _serve(app, settings, shutdown)


def _default_instance_id() -> str:
    """Something stable per process, so log lines can be grouped by replica."""
    return f"{socket.gethostname()}-{os.getpid()}"


def _build(setup: Setup, tel: telemetry.Telemetry, level: logs.LevelKnob) -> Any:
    from .asgi import build_app

    settings = setup.settings
    registry = setup.registry
    shutdown = setup.shutdown

    @contextlib.asynccontextmanager
    async def lifespan(app: Any) -> AsyncIterator[dict[str, Any]]:
        """Startup, then -- after uvicorn has drained -- ordered teardown.

        Decorated with asynccontextmanager rather than written as a bare async
        generator: Starlette 1.6 deprecates the undecorated form and wraps it
        itself, which emits a warning that `make verify` would turn into a
        failure.
        """
        # Chained here, not before uvicorn.run: uvicorn installs its signal
        # handlers inside serve(), so anything installed earlier is replaced and
        # never runs. See lifecycle.py for why the chain is necessary at all.
        shutdown.install()

        await registry.start()

        # Again, now that uvicorn's own modules are imported: uvicorn.access
        # does not exist when configure() runs, because uvicorn is imported
        # after the configuration it needs has been read.
        logs.adopt_existing_loggers()

        stale = logs.audit_loggers()
        if stale:
            _log.warning(
                "loggers bypass the chassis handler and therefore SPEC 17.3 redaction",
                extra={logkey.COMPONENT: "logging", logkey.COUNT: len(stale), "loggers": stale},
            )

        _log.info(
            "listening",
            extra={
                logkey.ADDR: settings.addr,
                logkey.CONFIG_KEY: settings.resolved(),
            },
        )

        try:
            yield {"settings": settings, "registry": registry, "shutdown": shutdown}
        finally:
            # By the time this runs, uvicorn has closed the listener and
            # drained in-flight requests. Phases 1 and 2 already happened in
            # lifecycle.Shutdown; what is left is phases 4 and 5.
            #
            # Wrapped in its own timeouts because timeout_graceful_shutdown does
            # NOT bound this block: a hung pool close would otherwise hang the
            # process regardless of every uvicorn setting.
            await registry.stop()
            await shutdown.close()
            # Telemetry last, so the spans describing the shutdown are exported.
            await tel.shutdown(settings.shutdown_telemetry_timeout.total_seconds())
            shutdown.report(clean=True)
            shutdown.restore()

    app = build_app(
        settings=settings,
        registry=registry,
        shutdown=shutdown,
        lifespan=lifespan,
        stream_routes=setup.stream_routes,
        routers=setup.routers,
    )
    app.state.level = level
    app.state.telemetry = tel

    if tel.enabled:
        from opentelemetry.instrumentation.fastapi import FastAPIInstrumentor

        # exclude_spans drops the two INTERNAL "http send"/"http receive"
        # children the ASGI instrumentation emits by default. Without it a
        # Python service produces three spans per request where the equivalent
        # Go service produces one -- a silent 3x cost and a cross-language
        # inconsistency in every trace.
        FastAPIInstrumentor.instrument_app(
            app,
            excluded_urls="livez,readyz,healthz,metrics",
            exclude_spans=["receive", "send"],
        )
    return app


def _serve(app: Any, settings: Any, shutdown: Shutdown) -> int:
    import uvicorn

    host, port = _split_addr(settings.addr)

    config = uvicorn.Config(
        app,
        host=host,
        port=port,
        # The chassis emits its own structured access log through the redacting
        # handler. uvicorn's would bypass it and would log raw paths.
        access_log=False,
        log_config=None,
        # Not optional. The default is None, which is
        # asyncio.wait_for(..., timeout=None) -- unbounded -- so one hung stream
        # would block shutdown forever and the lifespan cleanup would never run.
        # Sized below the total budget so the phases that follow still have room.
        timeout_graceful_shutdown=max(
            1,
            int(
                settings.shutdown_budget.total_seconds()
                - settings.shutdown_server_timeout.total_seconds()
            ),
        ),
        timeout_keep_alive=int(settings.http_idle_timeout.total_seconds()),
        h11_max_incomplete_event_size=int(settings.http_max_header_bytes),
    )
    server = uvicorn.Server(config)
    try:
        server.run()
    except KeyboardInterrupt:  # pragma: no cover - interactive only
        shutdown.report(clean=False)
        return 130
    return 0


def _split_addr(addr: str) -> tuple[str, int]:
    """Parse a Go-style listen address.

    ":8080" means every interface, which is what a container wants and what the
    Go chassis's default says.
    """
    host, _, port = addr.rpartition(":")
    if not port.isdigit():
        raise ValueError(f"HTTP_ADDR={addr!r} has no port; expected something like :8080")
    return (host or "0.0.0.0", int(port))  # noqa: S104 - a container binds all interfaces
