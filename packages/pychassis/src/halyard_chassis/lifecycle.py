"""Ordered shutdown, and the drain signal ASGI does not provide.

The Go chassis shuts down in four phases -- deregister, drain, stop the server,
close dependencies -- with telemetry last, because http.Server.Shutdown does
not cancel in-flight request contexts and so nothing else would tell a live
stream to wind down.

Reproducing that under uvicorn takes more than translating it, and the reason
is worth stating because it looks like an implementation detail and is not.
Verified against uvicorn 0.52.4 (see docs/verified.md):

  1. On SIGTERM uvicorn closes the listening socket IMMEDIATELY. There is no
     lame-duck window in which it still accepts traffic, so a readiness probe
     cannot be flipped first from inside the server's own shutdown.
  2. The FastAPI lifespan shutdown block runs LAST -- after the listener is
     closed and after in-flight requests have drained. So phase 1 and phase 2
     cannot live there. By the time the lifespan shutdown runs, there is
     nothing left to drain and no probe left to serve.
  3. uvicorn never tells an in-flight handler that the SERVER is stopping.
     `receive()` yields http.disconnect only when the CLIENT went away.
     `Protocol.shutdown()` for a request whose response has started merely sets
     keep_alive = False; it does not interrupt the response.

Taken together those make a circular dependency: an SSE handler can only learn
about shutdown from the lifespan block, and the lifespan block does not run
until that handler finishes. A long-lived stream would therefore hold shutdown
open until the graceful timeout cancelled it mid-write -- exactly the unclean
cut that Last-Event-ID resume exists to make unnecessary.

`Shutdown` closes the loop by chaining in front of uvicorn's own SIGTERM
handler. It captures the installed handler (which is uvicorn's bound
`Server.handle_exit`), installs its own, and calls the captured one only once
the deregister and drain phases have finished. Streams select on
`Shutdown.draining`, so they wind themselves down during phase 2, before
uvicorn is ever asked to stop.

Two further verified facts shape the rest:

  * uvicorn's `timeout_graceful_shutdown` defaults to None, which is
    `asyncio.wait_for(..., timeout=None)` -- unbounded. The runner always sets
    it, or a single hung stream blocks shutdown forever and the lifespan cleanup
    never executes at all.
  * `timeout_graceful_shutdown` does NOT bound the lifespan shutdown block, so a
    hung `pool.close()` or telemetry flush hangs the process regardless of every
    uvicorn setting. Each phase here carries its own `asyncio.timeout`.
"""

from __future__ import annotations

import asyncio
import contextlib
import logging
import signal
import time
from collections.abc import AsyncIterator, Awaitable, Callable
from dataclasses import dataclass
from types import FrameType
from typing import Any

from .obs import logkey

__all__ = ["Phase", "Shutdown", "ShutdownConfig"]

_log = logging.getLogger(__name__)

Hook = Callable[[], Awaitable[None]]


@dataclass(frozen=True, slots=True)
class ShutdownConfig:
    """Per-phase budgets, from ChassisSettings."""

    deregister_delay: float = 0.0
    drain_timeout: float = 10.0
    server_timeout: float = 5.0
    telemetry_timeout: float = 5.0
    budget: float = 25.0


class Phase:
    """Names for the ordered steps, so a log line says where it stopped."""

    DEREGISTER = "deregister"
    DRAIN = "drain"
    SERVER = "server"
    CLOSE = "close"
    TELEMETRY = "telemetry"


class Shutdown:
    """Coordinates the ordered stop, and exposes the drain signal.

    One instance per process, owned by the runner. Not a singleton: a test
    builds its own, and two in one session share nothing.
    """

    def __init__(self, cfg: ShutdownConfig) -> None:
        self._cfg = cfg
        self._draining = asyncio.Event()
        self._open_streams = 0
        self._streams_idle = asyncio.Event()
        self._streams_idle.set()
        self._closers: list[tuple[str, Hook]] = []
        self._chained: Any = None
        self._on_ready: Callable[[], None] | None = None
        self._task: asyncio.Task[None] | None = None
        self._installed_for: int | None = None
        self._started = 0.0

    # ------------------------------------------------------------- the signal

    @property
    def draining(self) -> asyncio.Event:
        """Set as soon as a stop begins, before the listener closes.

        Every long-lived response must select on this. The canonical shape:

            while True:
                done, _ = await asyncio.wait(
                    [asyncio.ensure_future(next_event()), asyncio.ensure_future(drain.wait())],
                    return_when=asyncio.FIRST_COMPLETED,
                )
                if drain.is_set():
                    yield close_frame()
                    return

        A stream that ignores it is not broken, it is just cut off by the
        graceful timeout instead of closing cleanly.
        """
        return self._draining

    @property
    def is_draining(self) -> bool:
        return self._draining.is_set()

    @contextlib.asynccontextmanager
    async def stream(self) -> AsyncIterator[asyncio.Event]:
        """Account for one long-lived response, and hand it the drain signal.

        Wrap every streaming endpoint in this. It does two things: the drain
        phase ends as soon as the last stream closes rather than always waiting
        out its whole timeout, and the yielded event is what the stream selects
        on to wind itself down.

            @router.get("/v1/sessions/{id}/events")
            async def events(id: str):
                async with shutdown.stream() as draining:
                    return StreamingResponse(frames(id, draining))
        """
        self._open_streams += 1
        self._streams_idle.clear()
        try:
            yield self._draining
        finally:
            self._open_streams -= 1
            if self._open_streams <= 0:
                self._open_streams = 0
                self._streams_idle.set()

    @property
    def open_streams(self) -> int:
        return self._open_streams

    def on_close(self, name: str, hook: Hook) -> None:
        """Register a dependency to close in phase 4.

        Closed in REVERSE registration order, so a pool registered after the
        thing that uses it is torn down second.
        """
        self._closers.append((name, hook))

    # -------------------------------------------------------------- the chain

    def install(self, *, on_ready_to_stop: Callable[[], None] | None = None) -> None:
        """Chain in front of the handler uvicorn installed.

        Must be called from the lifespan STARTUP block, not before
        `uvicorn.run`: uvicorn installs its handlers inside `serve()`, so a
        handler installed earlier is simply replaced and never runs at signal
        time.

        `on_ready_to_stop` defaults to the captured handler -- uvicorn's
        `Server.handle_exit` -- which is what actually begins closing the
        listener.
        """
        captured = signal.getsignal(signal.SIGTERM)
        if captured in (signal.SIG_DFL, signal.SIG_IGN, None):
            _log.debug(
                "no SIGTERM handler to chain; running without a drain phase",
                extra={logkey.COMPONENT: "lifecycle"},
            )
        self._chained = captured
        self._on_ready = on_ready_to_stop

        loop = asyncio.get_running_loop()

        def handler(signum: int, frame: FrameType | None) -> None:
            # Signal handlers run between bytecodes on the main thread, so the
            # only safe thing to do here is hand work to the loop.
            loop.call_soon_threadsafe(lambda: self._begin(signum, frame))

        for sig in (signal.SIGTERM, signal.SIGINT):
            signal.signal(sig, handler)
        self._installed_for = signal.SIGTERM

    def _begin(self, signum: int, frame: FrameType | None) -> None:
        if self._draining.is_set():
            # A second SIGTERM does not escalate anything in uvicorn either --
            # handle_exit only promotes to force_exit for SIGINT -- so absorbing
            # repeats here matches the behaviour an operator already has.
            return
        self._started = time.monotonic()
        self._draining.set()
        _log.info(
            "shutdown starting",
            extra={
                logkey.PHASE: Phase.DEREGISTER,
                logkey.REASON: signal.Signals(signum).name,
                logkey.COMPONENT: "lifecycle",
            },
        )
        task = asyncio.create_task(self._phases(signum, frame), name="chassis:shutdown")
        # A strong reference, for the same reason the health pollers need one.
        self._task = task

    async def _phases(self, signum: int, frame: FrameType | None) -> None:
        """Phases 1 and 2, then hand over to uvicorn.

        Phases 3, 4 and 5 happen in the lifespan shutdown block, which uvicorn
        runs after the listener is closed and in-flight work has drained.
        """
        cfg = self._cfg

        # Phase 1: deregister. Nothing to call -- readiness already reports not
        # ready, because `draining` is set and the probe consults it -- so this
        # is purely the wait for a load balancer to notice. Zero in development,
        # because waiting three seconds for a load balancer that is not there
        # makes every local restart feel broken.
        if cfg.deregister_delay > 0:
            await asyncio.sleep(cfg.deregister_delay)

        # Phase 2: drain. Streams saw `draining` the moment the signal arrived
        # and are closing themselves. This is the window in which they may.
        if cfg.drain_timeout > 0:
            _log.info(
                "draining",
                extra={logkey.PHASE: Phase.DRAIN, logkey.COMPONENT: "lifecycle"},
            )
            await self._await_drain(cfg.drain_timeout)

        # Hand over. uvicorn closes the listener, waits for in-flight requests
        # up to timeout_graceful_shutdown, then runs the lifespan shutdown.
        _log.info(
            "handing over to the server",
            extra={logkey.PHASE: Phase.SERVER, logkey.COMPONENT: "lifecycle"},
        )
        if self._on_ready is not None:
            self._on_ready()
        elif callable(self._chained):
            self._chained(signum, frame)
        else:
            # Nothing to chain to: re-raise so the default disposition applies.
            signal.signal(signum, signal.SIG_DFL)
            signal.raise_signal(signum)

    async def _await_drain(self, window: float) -> None:
        """Wait for open streams to close, up to `window`.

        A ceiling, not a fixed delay. The first version slept the whole window
        unconditionally, which put a ten-second pause on every restart of a
        service with nothing streaming -- and made the drain phase feel like a
        bug rather than a safeguard. A service with no open streams now hands
        over immediately.

        Streams that have not been taught to watch `draining` are not accounted
        for and so do not extend this; they are cut off by uvicorn's graceful
        timeout instead, which is why `stream()` exists.
        """
        if self._open_streams == 0:
            return
        _log.info(
            "waiting for open streams",
            extra={
                logkey.PHASE: Phase.DRAIN,
                logkey.STREAMS_OPEN: self._open_streams,
                logkey.COMPONENT: "lifecycle",
            },
        )
        try:
            async with asyncio.timeout(window):
                await self._streams_idle.wait()
        except TimeoutError:
            _log.warning(
                "streams did not close within the drain window",
                extra={
                    logkey.PHASE: Phase.DRAIN,
                    logkey.STREAMS_REMAINING: self._open_streams,
                    logkey.COMPONENT: "lifecycle",
                },
            )

    # ------------------------------------------------- phases 4 and 5

    async def close(self) -> None:
        """Close registered dependencies, in reverse order.

        Called from the lifespan shutdown block. Each hook gets its OWN timeout
        rather than inheriting a shared deadline: a cancellation delivered while
        a hook is inside a `finally` interrupts the cleanup mid-way, which is
        how a connection ends up neither released nor closed.

        A hook that raises is logged and the rest still run. Refusing to
        continue would leave later dependencies open on every shutdown where one
        of them was already broken.
        """
        for name, hook in reversed(self._closers):
            try:
                async with asyncio.timeout(self._cfg.server_timeout):
                    await hook()
            except TimeoutError:
                _log.warning(
                    "dependency did not close within its budget",
                    extra={
                        logkey.PHASE: Phase.CLOSE,
                        logkey.COMPONENT: name,
                    },
                )
            except Exception:
                _log.exception(
                    "dependency failed to close",
                    extra={logkey.PHASE: Phase.CLOSE, logkey.COMPONENT: name},
                )

    def elapsed(self) -> float:
        """Seconds since the stop began, for the final log line."""
        return time.monotonic() - self._started if self._started else 0.0

    def report(self, clean: bool) -> None:
        """The one line that says how the process ended."""
        _log.info(
            "shutdown complete",
            extra={
                logkey.CLEAN: clean,
                logkey.DURATION_MS: round(self.elapsed() * 1000),
                logkey.COMPONENT: "lifecycle",
            },
        )

    def restore(self) -> None:
        """Put the captured handlers back.

        uvicorn's own `capture_signals` does this and then RE-RAISES the
        captured signal, which is why a healthy graceful shutdown exits 143
        rather than 0. Restoring here keeps that behaviour intact instead of
        leaving our handler installed to swallow it.
        """
        if self._installed_for is None:
            return
        with contextlib.suppress(ValueError, TypeError):
            if self._chained is not None:
                signal.signal(signal.SIGTERM, self._chained)
        self._installed_for = None
