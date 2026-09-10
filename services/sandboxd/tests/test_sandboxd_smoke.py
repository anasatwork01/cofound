"""sandboxd is a real deployable before it has any routes.

It reads its configuration, reports readiness honestly, and shuts down cleanly.
That is worth testing now: the chassis wiring is the part every later task
depends on, and a mistake in it would surface as a mysterious failure in task
1.3 rather than here.
"""

from __future__ import annotations

import contextlib

import pytest
from fastapi.testclient import TestClient
from halyard_chassis import Registry, Shutdown, logs
from halyard_chassis.asgi import build_app
from halyard_chassis.lifecycle import ShutdownConfig
from halyard_chassis.settings import ConfigError, load
from halyard_sandboxd import SERVICE, __version__
from halyard_sandboxd.__main__ import build, main
from halyard_sandboxd.settings import SandboxdSettings

DSN = "postgres://hal:hunter2-correct-horse@db:5432/halyard"


def test_version_flag_needs_no_configuration(capsys: pytest.CaptureFixture[str]) -> None:
    assert main(["--version"]) == 0
    assert capsys.readouterr().out.strip() == f"{SERVICE} {__version__}"


def test_it_reads_its_own_variables_and_the_chassis_ones() -> None:
    s = load(SandboxdSettings, environ={}, service=SERVICE, DATABASE_URL=DSN, HTTP_ADDR=":9000")
    assert s.database_url == DSN
    assert s.addr == ":9000"
    assert s.shutdown_budget.total_seconds() == 25


def test_the_database_url_is_fingerprinted_in_the_boot_line() -> None:
    """It carries a password inline, so it is on the service's secret list."""
    resolved = load(SandboxdSettings, environ={}, service=SERVICE, DATABASE_URL=DSN).resolved()
    assert "hunter2-correct-horse" not in str(resolved)
    assert resolved["DATABASE_URL"].startswith("set (sha256:")


def test_a_typo_in_a_service_variable_is_caught() -> None:
    with pytest.raises(ConfigError) as caught:
        load(SandboxdSettings, environ={"DATABASE_ULR": DSN}, service=SERVICE)
    problem = next(p for p in caught.value.problems if p.env == "DATABASE_ULR")
    assert "DATABASE_URL" in problem.provenance


def test_no_probes_are_registered_when_nothing_is_configured() -> None:
    """A local run needs neither Postgres nor Redis, and says so honestly."""
    setup = _setup(load(SandboxdSettings, environ={}, service=SERVICE))
    build(setup)
    assert setup.registry.ready() == (True, [])


def test_a_configured_dependency_becomes_a_probe() -> None:
    """And an unreachable one reports unready rather than failing later."""
    setup = _setup(load(SandboxdSettings, environ={}, service=SERVICE, DATABASE_URL=DSN))
    build(setup)
    ok, failing = setup.registry.ready()
    assert (ok, failing) == (False, ["postgres"])


def test_the_app_serves_its_probes() -> None:
    sink = logs.Sink()
    logs.configure(logs.LogConfig(service=SERVICE, level="info", out=sink))
    settings = load(SandboxdSettings, environ={}, service=SERVICE)
    setup = _setup(settings)
    build(setup)

    @contextlib.asynccontextmanager
    async def lifespan(app):  # type: ignore[no-untyped-def]
        await setup.registry.start()
        yield {}
        await setup.registry.stop()

    app = build_app(
        settings=settings,
        registry=setup.registry,
        shutdown=setup.shutdown,
        lifespan=lifespan,
        routers=setup.routers,
    )
    try:
        with TestClient(app) as c:
            assert c.get("/livez").status_code == 200
            assert c.get("/readyz").json()["ready"] is True
            # No /v1 surface yet, and the 404 is still the wire envelope.
            assert c.get("/v1/sandboxes").json()["error"]["code"] == "not_found"
    finally:
        logs.reset()


def _setup(settings: SandboxdSettings):  # type: ignore[no-untyped-def]
    from halyard_chassis import Setup

    return Setup(
        settings=settings,
        registry=Registry(interval=10.0, timeout=0.1, stale_after=60.0),
        shutdown=Shutdown(ShutdownConfig()),
    )
