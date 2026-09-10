"""Background-polled readiness, and a liveness check that reads none of it.

The Python counterpart of packages/chassis/health. It keeps the Go design's
central decision -- each dependency is polled on a timer and the probe serves
the cached verdict -- for the reason that made it worth doing there: a probe
request that fans out to Postgres and Redis turns every readiness check into
load on the thing being checked, and a Kubernetes probe interval times the
number of replicas is a lot of SELECT 1.

Two things here differ from the Go implementation because asyncio forces them,
and both are verified (see docs/verified.md):

  * No TaskGroup. Holding one open across a FastAPI lifespan `yield` is actively
    dangerous: when a poller inside it crashes mid-serving, the TaskGroup
    cancels the PARENT task -- the lifespan task -- the ExceptionGroup surfaces
    at contextlib's athrow, and THE SHUTDOWN HALF OF THE LIFESPAN NEVER RUNS,
    while uvicorn keeps serving 200s. Telemetry flush and pool close are
    silently skipped. So pollers are plain tasks under a supervisor loop.
  * Strong references are held. The event loop keeps only WEAK references to
    tasks from `create_task`, so a fire-and-forget poller in a lifespan local
    can be garbage collected mid-execution. The registry owns a set of them.

A poller that raises is otherwise COMPLETELY silent -- the exception sits
unretrieved in the Task and asyncio only mentions it when the task is collected
-- and a dead poller leaves the cached verdict frozen at its last value forever,
which is the worst possible failure for a readiness probe: it reports healthy
for as long as the process lives. The supervisor loop catches `Exception` per
iteration, which on 3.13 is automatically cancellation-safe because
CancelledError derives from BaseException, and a stale verdict ages into
`unknown` regardless.
"""

from __future__ import annotations

import asyncio
import contextlib
import logging
import time
from collections.abc import Awaitable, Callable
from dataclasses import dataclass, field
from enum import StrEnum
from typing import Final

from .obs import logkey

__all__ = ["Probe", "ProbeResult", "Registry", "State"]

_log = logging.getLogger(__name__)

# How many consecutive failures before a dependency is reported unready.
#
# Not one. asyncpg transparently recovers from a server-side terminated backend
# -- verified: after pg_terminate_backend killed the pooled connection,
# pool.execute("SELECT 1") succeeded on the FIRST retry -- so a single failed
# poll is a normal event during a Postgres failover, not an outage. Reporting
# unready on it would take a replica out of rotation for a blip it had already
# recovered from.
DEFAULT_FAILURE_THRESHOLD: Final = 2


class State(StrEnum):
    """A dependency's last known condition."""

    UP = "up"
    DOWN = "down"
    UNKNOWN = "unknown"
    """Not yet polled, or the last verdict is older than the stale window."""


# A check returns None on success and raises on failure. Returning a bool would
# make `return False` and a forgotten `return` indistinguishable.
Check = Callable[[], Awaitable[None]]


@dataclass(slots=True)
class ProbeResult:
    """What the registry knows about one dependency."""

    name: str
    state: State = State.UNKNOWN
    since: float = 0.0
    """Monotonic time of the last state CHANGE, for "down for how long"."""

    last_polled: float = 0.0
    """Monotonic time of the last completed poll, for staleness. Distinct from
    `since` on purpose: a healthy dependency changes state once and is polled
    every interval, so measuring staleness from `since` would mark every
    long-healthy dependency stale."""

    latency: float = 0.0
    consecutive_failures: int = 0
    critical: bool = True
    detail: str = ""
    """The last failure's message. Served only when READINESS_DETAIL is on,
    which ChassisSettings refuses in production: /readyz is unauthenticated."""


@dataclass(slots=True)
class Probe:
    """A registered dependency check."""

    name: str
    check: Check
    critical: bool = True
    """A non-critical probe is reported but does not hold readiness down. A
    cache that is down degrades latency; a database that is down means the
    service cannot serve."""

    result: ProbeResult = field(init=False)

    def __post_init__(self) -> None:
        self.result = ProbeResult(name=self.name, critical=self.critical)


class Registry:
    """Polls each dependency on a timer and serves the cached verdicts."""

    def __init__(
        self,
        *,
        interval: float = 2.0,
        timeout: float = 1.0,
        stale_after: float = 10.0,
        failure_threshold: int = DEFAULT_FAILURE_THRESHOLD,
        detail: bool = False,
    ) -> None:
        self._probes: dict[str, Probe] = {}
        self._interval = interval
        self._timeout = timeout
        self._stale_after = stale_after
        self._threshold = failure_threshold
        self._detail = detail
        # Strong references. Without this set the loop's weak references let a
        # poller be collected mid-execution.
        self._tasks: set[asyncio.Task[None]] = set()
        self._started = False

    def register(self, name: str, check: Check, *, critical: bool = True) -> None:
        """Add a dependency. Must be called before `start`."""
        if self._started:
            raise RuntimeError(
                "probes must be registered before the registry starts; a probe "
                "added later would report unknown until its first poll, and "
                "readiness would flap on a running service"
            )
        if name in self._probes:
            raise ValueError(f"probe {name!r} is already registered")
        self._probes[name] = Probe(name=name, check=check, critical=critical)

    async def start(self) -> None:
        """Poll every dependency once, then leave a poller running for each.

        The first poll is awaited so a service does not report `unknown` for its
        whole first interval, which would otherwise make a fast-starting
        deployment look unready for two seconds for no reason.
        """
        if self._started:
            return
        self._started = True
        if self._probes:
            await asyncio.gather(*(self._poll(p) for p in self._probes.values()))
        for probe in self._probes.values():
            task = asyncio.create_task(self._run(probe), name=f"probe:{probe.name}")
            self._tasks.add(task)
            task.add_done_callback(self._tasks.discard)

    async def stop(self, budget: float = 2.0) -> None:
        """Cancel every poller and wait for it to finish.

        `cancel()` is only a request, and cancelling without awaiting truncates
        the task's own cleanup. The join gets a fresh budget of its own rather
        than inheriting the caller's deadline: a timeout that fires inside a
        `finally` block interrupts the cleanup mid-way, which is how a shutdown
        step ends up half-done.
        """
        tasks = list(self._tasks)
        for task in tasks:
            task.cancel()
        if not tasks:
            return
        with contextlib.suppress(TimeoutError):
            async with asyncio.timeout(budget):
                await asyncio.gather(*tasks, return_exceptions=True)

    async def _run(self, probe: Probe) -> None:
        """Poll `probe` forever.

        `except Exception` rather than `except BaseException`: on 3.13
        CancelledError derives from BaseException, so this cannot accidentally
        swallow the shutdown cancel. A bare `except:` would, which is why there
        is not one.
        """
        while True:
            try:
                await asyncio.sleep(self._interval)
                await self._poll(probe)
            except asyncio.CancelledError:
                raise
            except Exception:
                # A poller that dies leaves its verdict frozen at the last
                # value forever. Log and keep looping.
                _log.exception(
                    "readiness poller failed",
                    extra={logkey.PROBE: probe.name, logkey.COMPONENT: "health"},
                )

    async def _poll(self, probe: Probe) -> None:
        started = time.monotonic()
        failure: str | None = None
        try:
            # Never shielded. A timeout that fires around a shielded task
            # raises to the waiter while the shielded work keeps running
            # unsupervised, so the budget would be a lie and the poll would
            # pile up.
            async with asyncio.timeout(self._timeout):
                await probe.check()
        except asyncio.CancelledError:
            raise
        except TimeoutError:
            failure = f"did not respond within {self._timeout:g}s"
        except Exception as exc:
            failure = f"{type(exc).__name__}: {exc}"

        result = probe.result
        now = time.monotonic()
        result.latency = now - started
        result.last_polled = now

        if failure is None:
            if result.state is not State.UP:
                result.since = now
            result.state = State.UP
            result.consecutive_failures = 0
            result.detail = ""
            return

        result.consecutive_failures += 1
        result.detail = failure
        if result.consecutive_failures >= self._threshold and result.state is not State.DOWN:
            result.state = State.DOWN
            result.since = now
            _log.warning(
                "dependency is down",
                extra={
                    logkey.PROBE: probe.name,
                    logkey.PROBE_STATE: result.state.value,
                    logkey.COUNT: result.consecutive_failures,
                    logkey.COMPONENT: "health",
                    # The reason goes to the log, never to /readyz unless
                    # READINESS_DETAIL is on.
                    logkey.REASON: failure,
                },
            )

    def snapshot(self) -> dict[str, ProbeResult]:
        """The current verdicts, with stale ones aged into `unknown`.

        Ageing happens on read rather than on a timer, so a poller that has
        wedged or died produces `unknown` -- which fails readiness -- instead of
        a verdict that stays `up` for as long as the process lives. That is the
        one failure mode a cached health check has and an inline one does not,
        so it gets handled here rather than trusted not to happen.
        """
        now = time.monotonic()
        out: dict[str, ProbeResult] = {}
        for name, probe in self._probes.items():
            result = probe.result
            if result.state is not State.UNKNOWN and now - result.last_polled > self._stale_after:
                result.state = State.UNKNOWN
                result.since = now
                result.detail = (
                    f"no poll completed in {now - result.last_polled:.0f}s; "
                    "the poller may have stopped"
                )
            out[name] = result
        return out

    def ready(self) -> tuple[bool, list[str]]:
        """Whether every critical dependency is up, and the names that are not.

        Names only. /readyz is unauthenticated, so the failing probes' error
        strings are not part of the answer unless READINESS_DETAIL is on.
        """
        current = self.snapshot()
        failing = [
            name
            for name, result in current.items()
            if result.critical and result.state is not State.UP
        ]
        return not failing, failing

    def report(self) -> dict[str, object]:
        """The body /readyz serves."""
        ok, failing = self.ready()
        probes: dict[str, object] = {}
        for name, result in self.snapshot().items():
            entry: dict[str, object] = {
                "state": result.state.value,
                "critical": result.critical,
            }
            if self._detail and result.detail:
                entry["detail"] = result.detail
            probes[name] = entry
        return {"ready": ok, "failing": failing, "probes": probes}

    @property
    def detail(self) -> bool:
        return self._detail
