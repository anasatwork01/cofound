"""The FastAPI application factory, middleware order, and error rendering.

The Python counterpart of packages/chassis/httpx. Middleware ORDER is the part
that carries the safety properties, and it is the same order for the same
reasons -- but Starlette's stack runs OUTSIDE-IN in the order middleware is
ADDED via `add_middleware`, reversed: the last added is the outermost. So the
list below reads in execution order and is registered in reverse.

Execution order, outermost first:

  1. request id      -- so every later layer, including the panic handler, has
                        one to log and to put in the response
  2. tracing         -- so the span exists before anything can fail inside it
  3. access log      -- so a request that panics is still logged with its status
  4. exception guard -- INSIDE tracing and logging, deliberately. An unhandled
                        exception caught outside them would produce a 500 with
                        no span and no log line, which is the one failure mode
                        that leaves nothing to debug.
  5. timeout         -- innermost, so its cancellation is what the guard sees

Two Go-specific pieces have no Python equivalent and are absent rather than
translated:

  * There is no response-writer wrapper and no Unwrap invariant. Starlette
    responses are objects, not an interface a wrapper can accidentally hide.
  * There is no clearing of a write deadline for SSE. Verified: uvicorn 0.52.4
    has NO write timeout, NO response timeout and NO total-request timeout
    anywhere in its codebase, so the Go trap where http.Server.WriteTimeout
    severs a live stream simply does not exist. `timeout_keep_alive` cannot do
    it either -- the keep-alive timer is armed only between requests and is
    explicitly disarmed when a request begins.

The flip side of that finding is that the chassis gets no free per-request
deadline, which is why layer 5 exists at all and why it exempts streams.
"""

from __future__ import annotations

import asyncio
import logging
import time
import uuid
from collections.abc import Iterable
from typing import Any, Final

from fastapi import FastAPI, Request, Response
from fastapi.responses import JSONResponse
from starlette.middleware.base import BaseHTTPMiddleware, RequestResponseEndpoint
from starlette.types import ASGIApp

from . import errors, logs, telemetry
from .health import Registry
from .lifecycle import Shutdown
from .obs import attrs, logkey

__all__ = [
    "REQUEST_ID_HEADER",
    "build_app",
    "error_response",
    "mount_probes",
]

_log = logging.getLogger(__name__)

REQUEST_ID_HEADER: Final = "X-Request-Id"

# Paths excluded from tracing and from the access log. A probe polled every two
# seconds by every replica is the single highest-cardinality, lowest-value span
# a control plane emits.
_QUIET_PATHS: Final[frozenset[str]] = frozenset({"/livez", "/readyz", "/healthz", "/metrics"})


def error_response(exc: BaseException, request_id: str | None) -> JSONResponse:
    """Render any exception as the wire envelope.

    Everything client-visible goes through `errors.from_exception`, so a handler
    that lets an asyncpg exception escape produces a canned internal envelope
    rather than a driver message containing a connection string. That is
    structural: there is no code path from a cause to a response body.
    """
    err = errors.from_exception(exc)
    assert err is not None  # from_exception returns None only for None
    body = err.response(request_id)
    headers = {}
    if err.retry_after is not None:
        headers["Retry-After"] = str(int(err.retry_after.total_seconds()))
    if request_id:
        headers[REQUEST_ID_HEADER] = request_id
    return JSONResponse(
        status_code=err.status,
        content=body.model_dump(mode="json", exclude_none=True),
        headers=headers,
    )


class RequestIDMiddleware(BaseHTTPMiddleware):
    """Assign or adopt a request id, and bind it to the log context.

    An inbound id is adopted only when TRUST_INBOUND_REQUEST_ID is on. Off by
    default because the id reaches log lines and span attributes: a caller who
    can choose it can forge correlation, or pass something enormous, or pass a
    newline.
    """

    def __init__(self, app: ASGIApp, *, trust_inbound: bool = False) -> None:
        super().__init__(app)
        self._trust = trust_inbound

    async def dispatch(self, request: Request, call_next: RequestResponseEndpoint) -> Response:
        request_id = ""
        if self._trust:
            candidate = request.headers.get(REQUEST_ID_HEADER, "")
            # Same sanitiser the error path uses: bounded, and no character
            # that could forge a log line.
            request_id = errors.sanitize_ident(candidate) or ""
        if not request_id:
            request_id = uuid.uuid4().hex
        request.state.request_id = request_id
        with logs.bind(**{logkey.REQUEST_ID: request_id}):
            response = await call_next(request)
        response.headers[REQUEST_ID_HEADER] = request_id
        return response


class TracingMiddleware(BaseHTTPMiddleware):
    """Adopt inbound baggage onto the local span and the log context.

    FastAPIInstrumentor creates the span; this puts the SPEC 17.3 tenancy tuple
    on it using the generated attribute keys, so a Python span and a Go span
    carry identical attributes and the trace joins. The same tuple is bound to
    the log context, so the log search joins too -- the pairing between a
    baggage member and its log field is generated from a shared ident in
    packages/schema/observability.json rather than derived from the strings.
    """

    async def dispatch(self, request: Request, call_next: RequestResponseEndpoint) -> Response:
        if request.url.path in _QUIET_PATHS:
            return await call_next(request)

        carried = telemetry.read_baggage()
        fields = {
            log_field: carried[member]
            for member, log_field in attrs.BAGGAGE_LOG_FIELDS
            if carried.get(member)
        }
        telemetry.annotate_span(
            request_id=getattr(request.state, "request_id", None),
            org_id=carried.get(attrs.BAGGAGE_ORG_ID),
            project_id=carried.get(attrs.BAGGAGE_PROJECT_ID),
            session_id=carried.get(attrs.BAGGAGE_SESSION_ID),
        )
        with logs.bind(**fields):
            return await call_next(request)


class AccessLogMiddleware(BaseHTTPMiddleware):
    """One line per request, with route rather than path.

    Route, never path: a raw path is unbounded cardinality and can carry a token
    or a user-chosen slug. Starlette resolves the route after the handler runs,
    so it is read from the scope on the way out.
    """

    async def dispatch(self, request: Request, call_next: RequestResponseEndpoint) -> Response:
        if request.url.path in _QUIET_PATHS:
            return await call_next(request)
        started = time.perf_counter()
        status = 500
        try:
            response = await call_next(request)
            status = response.status_code
            return response
        finally:
            route = request.scope.get("route")
            _log.info(
                "request",
                extra={
                    logkey.HTTP_METHOD: request.method,
                    logkey.HTTP_ROUTE: getattr(route, "path", "unmatched"),
                    logkey.HTTP_STATUS: status,
                    logkey.DURATION_MS: round((time.perf_counter() - started) * 1000, 3),
                    logkey.CLIENT_IP: request.client.host if request.client else "",
                },
            )


class ExceptionGuardMiddleware(BaseHTTPMiddleware):
    """Turn any escaping exception into the wire envelope.

    Positioned INSIDE tracing and the access log so a failure still gets a span
    and a log line. A disconnect is not a fault: it is logged at info and
    reported as 499, so it does not count against the availability SLO.
    """

    async def dispatch(self, request: Request, call_next: RequestResponseEndpoint) -> Response:
        request_id = getattr(request.state, "request_id", None)
        try:
            return await call_next(request)
        except asyncio.CancelledError:
            # Reached either because the client vanished or because uvicorn's
            # graceful-shutdown timeout cancelled the task. Neither is a fault,
            # and uvicorn would otherwise log it as "Exception in ASGI
            # application" with a full traceback -- on every rolling deploy.
            _log.info(
                "client closed the connection",
                extra={logkey.ERROR_CODE: errors.CODE_CLIENT_CLOSED},
            )
            raise
        except Exception as exc:
            err = errors.from_exception(exc)
            assert err is not None
            telemetry.annotate_span(error_code=err.code, request_id=request_id)
            if err.status >= 500:
                _log.exception(
                    "request failed",
                    extra={logkey.ERROR_CODE: err.code, logkey.HTTP_STATUS: err.status},
                )
            else:
                _log.warning(
                    "request rejected",
                    extra={logkey.ERROR_CODE: err.code, logkey.HTTP_STATUS: err.status},
                )
            return error_response(err, request_id)


class TimeoutMiddleware(BaseHTTPMiddleware):
    """A per-request deadline, because uvicorn has none.

    Streaming responses are exempt: a deadline that kills an SSE connection
    after fifteen seconds is not a safety feature, it is an outage. Exemption is
    by route rather than by inspecting the response, because by the time a
    StreamingResponse object exists the deadline is already running.
    """

    def __init__(self, app: ASGIApp, *, budget: float, exempt: Iterable[str] = ()) -> None:
        super().__init__(app)
        self._budget = budget
        self._exempt = frozenset(exempt)

    async def dispatch(self, request: Request, call_next: RequestResponseEndpoint) -> Response:
        route = request.scope.get("route")
        path = getattr(route, "path", request.url.path)
        if self._budget <= 0 or path in self._exempt or path in _QUIET_PATHS:
            return await call_next(request)
        try:
            async with asyncio.timeout(self._budget):
                return await call_next(request)
        except TimeoutError as exc:
            raise errors.timeout("this request", exc) from exc


def mount_probes(app: FastAPI, registry: Registry, shutdown: Shutdown) -> None:
    """Add /livez and /readyz.

    Two endpoints, not one shared handler, and /livez deliberately reads NONE of
    the dependency verdicts. The standard mistake is a liveness probe that
    checks the database: a database blip then restarts every replica, which does
    not fix the database and makes recovery slower. Kubernetes' own docs warn
    that this causes cascading failures.
    """

    @app.get("/livez", include_in_schema=False)
    async def livez() -> Response:
        # 200 for as long as the event loop can run this coroutine. That is the
        # entire question liveness answers.
        return JSONResponse({"alive": True})

    @app.get("/readyz", include_in_schema=False)
    async def readyz() -> Response:
        if shutdown.is_draining:
            # Reported before the listener closes, which is the only window in
            # which a load balancer can act on it.
            return JSONResponse(
                {"ready": False, "failing": ["draining"], "probes": {}}, status_code=503
            )
        report = registry.report()
        return JSONResponse(report, status_code=200 if report["ready"] else 503)


def build_app(
    *,
    settings: Any,
    registry: Registry,
    shutdown: Shutdown,
    lifespan: Any,
    stream_routes: Iterable[str] = (),
    routers: Iterable[Any] = (),
) -> FastAPI:
    """Assemble the application with the chassis middleware stack.

    `stream_routes` names the route paths exempt from the request deadline.
    """
    app = FastAPI(
        title=settings.service,
        version=settings.version or "0.0.0",
        lifespan=lifespan,
        # The chassis renders every error; FastAPI's own handlers would emit a
        # shape no generated client can decode.
        default_response_class=JSONResponse,
    )

    for router in routers:
        app.include_router(router)

    # Registered in reverse of execution order: Starlette makes the LAST added
    # middleware the OUTERMOST.
    app.add_middleware(
        TimeoutMiddleware,
        budget=settings.http_handler_timeout.total_seconds(),
        exempt=stream_routes,
    )
    app.add_middleware(ExceptionGuardMiddleware)
    app.add_middleware(AccessLogMiddleware)
    app.add_middleware(TracingMiddleware)
    app.add_middleware(RequestIDMiddleware, trust_inbound=settings.trust_inbound_request_id)

    mount_probes(app, registry, shutdown)
    _install_error_handlers(app)
    return app


def _install_error_handlers(app: FastAPI) -> None:
    """Route FastAPI's own failures through the chassis envelope.

    Without these, a validation failure returns FastAPI's `{"detail": [...]}`
    and a 404 returns `{"detail": "Not Found"}` -- neither of which any
    generated client can decode, because the schema says the body is
    `ErrorResponse`.
    """
    from fastapi.exceptions import RequestValidationError
    from starlette.exceptions import HTTPException as StarletteHTTPException

    @app.exception_handler(errors.Error)
    async def _chassis_error(request: Request, exc: errors.Error) -> Response:
        return error_response(exc, getattr(request.state, "request_id", None))

    @app.exception_handler(RequestValidationError)
    async def _validation(request: Request, exc: RequestValidationError) -> Response:
        # The field names are safe to echo; the submitted values are not, and
        # pydantic's own rendering includes them.
        fields = [".".join(str(p) for p in e["loc"][1:]) for e in exc.errors()]
        err = errors.invalid("That request is not valid.").with_fix(
            "Check the fields and try again."
        )
        if fields:
            err = err.with_detail("fields", fields)
        return error_response(err, getattr(request.state, "request_id", None))

    @app.exception_handler(StarletteHTTPException)
    async def _http(request: Request, exc: StarletteHTTPException) -> Response:
        request_id = getattr(request.state, "request_id", None)
        if exc.status_code == 404:
            return error_response(
                errors.not_found()
                .with_message("There is no endpoint at that path.")
                .with_fix("Check the path and the API version."),
                request_id,
            )
        if exc.status_code == 405:
            return error_response(errors.method_not_allowed(request.method), request_id)
        entry = next(
            (e for e in errors.CHASSIS_ENTRIES if e.status == exc.status_code),
            None,
        )
        err = errors.CHASSIS.new(entry.code) if entry else errors.internal(exc)
        return error_response(err, request_id)
