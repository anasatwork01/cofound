"""End to end through a real application: probes, error rendering, correlation.

These go through the actual middleware stack and the actual redacting handler,
so an assertion here is about what a client and a log collector would really
see.
"""

from __future__ import annotations

import logging
from collections.abc import Iterator
from dataclasses import dataclass

import pytest
from fastapi import APIRouter
from fastapi.testclient import TestClient
from halyard_chassis import errors, logs
from halyard_chassis.asgi import REQUEST_ID_HEADER, build_app
from halyard_chassis.health import Registry
from halyard_chassis.lifecycle import Shutdown, ShutdownConfig
from halyard_chassis.obs import vocab
from halyard_chassis.settings import ChassisSettings, load

# Built at runtime, never written down. A literal shaped this much like a real
# Stripe key trips GitHub's push protection -- it validates shape, not liveness
# -- and asking it to allowlist a fake would blunt a tool that is doing its job.
# Deriving it from the generated prefix list is better anyway: the fixture
# follows the vocabulary instead of duplicating one entry from it.
LIVE_LOOKING_KEY = vocab.SECRET_PREFIXES[0] + "4eC39HqLyjWDarjtT1zdp7dc"


def _router() -> APIRouter:
    r = APIRouter()

    @r.get("/ok")
    async def ok() -> dict[str, bool]:
        return {"ok": True}

    @r.get("/typed-failure")
    async def typed_failure() -> None:
        raise errors.not_found("project", "acme-crm")

    @r.get("/raw-failure")
    async def raw_failure() -> None:
        # The case the whole error layer exists for: a driver-shaped exception
        # whose message contains a credential.
        raise RuntimeError(f"connection failed: postgres://hal:{LIVE_LOOKING_KEY}@db:5432/x")

    @r.get("/echo-id")
    async def echo_id() -> dict[str, str]:
        logging.getLogger("app").info("handled")
        return {"ok": "yes"}

    return r


@dataclass
class Harness:
    client: TestClient
    sink: logs.Sink
    shutdown: Shutdown
    registry: Registry


@pytest.fixture
def client() -> Iterator[Harness]:
    sink = logs.Sink()
    logs.configure(logs.LogConfig(service="test", env="test", level="info", out=sink))
    settings = load(ChassisSettings, environ={}, service="test")
    registry = Registry(interval=10.0, timeout=1.0, stale_after=60.0)
    shutdown = Shutdown(ShutdownConfig())

    import contextlib

    @contextlib.asynccontextmanager
    async def lifespan(app):  # type: ignore[no-untyped-def]
        await registry.start()
        yield {}
        await registry.stop()

    app = build_app(
        settings=settings,
        registry=registry,
        shutdown=shutdown,
        lifespan=lifespan,
        routers=[_router()],
    )
    with TestClient(app, raise_server_exceptions=False) as c:
        yield Harness(client=c, sink=sink, shutdown=shutdown, registry=registry)
    logs.reset()


def test_livez_is_200_with_no_dependencies_registered(client: Harness) -> None:
    assert client.client.get("/livez").status_code == 200


def test_livez_stays_200_when_a_dependency_is_down() -> None:
    """The standard mistake is a liveness probe that checks the database: a
    blip then restarts every replica, which does not fix the database."""
    import contextlib

    async def down() -> None:
        raise RuntimeError("refused")

    sink = logs.Sink()
    logs.configure(logs.LogConfig(service="test", level="info", out=sink))
    registry = Registry(interval=10.0, timeout=1.0, stale_after=60.0, failure_threshold=1)
    registry.register("postgres", down)
    shutdown = Shutdown(ShutdownConfig())

    @contextlib.asynccontextmanager
    async def lifespan(app):  # type: ignore[no-untyped-def]
        await registry.start()
        yield {}
        await registry.stop()

    app = build_app(
        settings=load(ChassisSettings, environ={}, service="test"),
        registry=registry,
        shutdown=shutdown,
        lifespan=lifespan,
    )
    try:
        with TestClient(app) as c:
            assert c.get("/livez").status_code == 200
            assert c.get("/readyz").status_code == 503
    finally:
        logs.reset()


def test_readyz_reports_draining_before_the_listener_closes(client: Harness) -> None:
    """The only window in which a load balancer can act on it.

    uvicorn closes the listening socket the instant it starts shutting down, and
    runs the lifespan shutdown block LAST -- so a probe flipped from inside the
    server's own teardown would be flipped after there was anything left to
    drain. This is why lifecycle.Shutdown chains in front of uvicorn's signal
    handler rather than doing its work in the lifespan.
    """
    assert client.client.get("/readyz").status_code == 200

    client.shutdown.draining.set()

    response = client.client.get("/readyz")
    assert response.status_code == 503
    assert response.json()["failing"] == ["draining"]

    # Liveness must NOT flip: the process is fine, it is just refusing new work.
    assert client.client.get("/livez").status_code == 200


def test_a_typed_error_renders_the_wire_envelope(client: Harness) -> None:
    c = client.client
    response = c.get("/typed-failure")
    assert response.status_code == 404
    body = response.json()
    assert body["error"]["code"] == "not_found"
    assert body["error"]["message"] == 'Project "acme-crm" does not exist.'
    assert body["error"]["retriable"] is False
    assert body["error"]["details"] == {"resource": "project"}
    assert body["error"]["request_id"]


def test_a_raw_exception_cannot_leak_its_cause(client: Harness) -> None:
    """The structural guarantee. There is no code path from a cause to a
    response body, so a driver error cannot put a connection string on the
    wire -- and the log line cannot either."""
    c, sink = client.client, client.sink
    response = c.get("/raw-failure")
    assert response.status_code == 500
    assert LIVE_LOOKING_KEY not in response.text
    assert "postgres://" not in response.text
    body = response.json()
    assert body["error"]["code"] == "internal"
    assert body["error"]["message"] == "Halyard could not complete this request."
    assert not sink.contains(LIVE_LOOKING_KEY)


def test_an_unknown_path_renders_the_envelope_not_fastapis_detail(client: Harness) -> None:
    """FastAPI's own {"detail": "Not Found"} is a shape no generated client can
    decode, because the schema says the body is ErrorResponse."""
    c = client.client
    response = c.get("/nope")
    assert response.status_code == 404
    assert response.json()["error"]["code"] == "not_found"
    assert "detail" not in response.json()


def test_a_request_id_is_generated_and_returned(client: Harness) -> None:
    c, sink = client.client, client.sink
    response = c.get("/echo-id")
    request_id = response.headers[REQUEST_ID_HEADER]
    assert len(request_id) == 32
    assert any(line.get("request_id") == request_id for line in sink.lines())


def test_an_inbound_request_id_is_ignored_by_default(client: Harness) -> None:
    """Off by default because the id reaches log lines and span attributes: a
    caller who can choose it can forge correlation."""
    c = client.client
    response = c.get("/echo-id", headers={REQUEST_ID_HEADER: "forged-by-caller"})
    assert response.headers[REQUEST_ID_HEADER] != "forged-by-caller"


def test_the_access_log_records_the_route_not_the_path(client: Harness) -> None:
    """A raw path is unbounded cardinality and can carry a token or a
    user-chosen slug."""
    c, sink = client.client, client.sink
    c.get("/ok")
    line = next(x for x in sink.lines() if x.get("msg") == "request")
    assert line["http_route"] == "/ok"
    assert line["http_status"] == 200
    assert line["http_method"] == "GET"


def test_probes_are_absent_from_the_access_log(client: Harness) -> None:
    """A probe polled every two seconds by every replica is the highest-volume,
    lowest-value line a control plane emits."""
    c, sink = client.client, client.sink
    sink.reset()
    c.get("/livez")
    c.get("/readyz")
    assert [x for x in sink.lines() if x.get("msg") == "request"] == []
