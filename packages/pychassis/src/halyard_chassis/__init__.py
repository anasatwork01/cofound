"""The Halyard Python service chassis.

Everything a Python service needs before it has any behaviour of its own:
configuration, structured logging with SPEC 17.3 redaction, tracing, readiness,
ordered shutdown, and the error envelope every endpoint returns.

The Go counterpart is packages/chassis. The two are deliberately not
independent implementations of the same idea: the observability vocabulary and
the error catalogue are shared contracts, checked in both directions by tests,
because a control-plane trace crosses from Go into Python and back and a console
branches on error codes without knowing which language served the request. See
packages/schema/observability.json.

A service's entrypoint should be short:

    from halyard_chassis import Service, Setup, run

    def build(setup: Setup) -> None:
        setup.registry.register("postgres", check_postgres)
        setup.routers.append(router)

    def main() -> int:
        return run(Service(name="sandboxd", version=__version__, build=build))
"""

from __future__ import annotations

from . import errors, health, lifecycle, logs, obs, settings, telemetry
from .errors import CHASSIS, Catalog, Entry, Error
from .health import Registry, State
from .lifecycle import Shutdown, ShutdownConfig
from .logs import LogConfig, bind, configure, diff, output, prompt, user
from .runner import EX_CONFIG, Service, Setup, run
from .settings import ChassisSettings, ConfigError, ConfigProblem, load

__all__ = [
    "CHASSIS",
    "EX_CONFIG",
    "Catalog",
    "ChassisSettings",
    "ConfigError",
    "ConfigProblem",
    "Entry",
    "Error",
    "LogConfig",
    "Registry",
    "Service",
    "Setup",
    "Shutdown",
    "ShutdownConfig",
    "State",
    "bind",
    "configure",
    "diff",
    "errors",
    "health",
    "learn_more",
    "lifecycle",
    "load",
    "logs",
    "obs",
    "output",
    "prompt",
    "run",
    "settings",
    "telemetry",
    "user",
]

# Documentation pointer, so `help(halyard_chassis)` leads somewhere useful.
learn_more = "packages/pychassis/README.md"
