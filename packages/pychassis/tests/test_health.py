"""Readiness is polled in the background, and liveness reads none of it."""

from __future__ import annotations

import asyncio

import pytest
from halyard_chassis.health import Registry, State


async def _ok() -> None:
    return None


def _failing(message: str = "boom"):
    async def check() -> None:
        raise RuntimeError(message)

    return check


async def _hangs() -> None:
    await asyncio.sleep(10)


async def test_a_healthy_dependency_is_ready_after_the_first_poll() -> None:
    """The first poll is awaited, so a fast-starting service does not report
    unknown for its whole first interval."""
    r = Registry(interval=0.01, timeout=0.5, stale_after=1.0)
    r.register("postgres", _ok)
    await r.start()
    try:
        assert r.ready() == (True, [])
        assert r.snapshot()["postgres"].state is State.UP
    finally:
        await r.stop()


async def test_a_single_failure_does_not_take_a_healthy_replica_out_of_rotation() -> None:
    """asyncpg transparently recovers from a terminated backend -- verified, on
    the first retry -- so one failed poll against a dependency that was up is a
    failover blip, not an outage."""
    fail_next = False

    async def flaky() -> None:
        if fail_next:
            raise RuntimeError("backend terminated")

    r = Registry(interval=10.0, timeout=0.5, stale_after=60.0, failure_threshold=2)
    r.register("postgres", flaky)
    await r.start()
    try:
        assert r.ready() == (True, [])

        fail_next = True
        await r._poll(r._probes["postgres"])

        assert r.ready() == (True, []), "one failure should not unready a healthy dependency"
        assert r.snapshot()["postgres"].consecutive_failures == 1
    finally:
        await r.stop()


async def test_a_dependency_that_has_never_answered_is_not_ready() -> None:
    """The other half of the threshold rule. Tolerating a blip must not mean
    serving traffic before the database has answered even once."""
    r = Registry(interval=10.0, timeout=0.5, stale_after=60.0, failure_threshold=2)
    r.register("postgres", _failing())
    await r.start()
    try:
        assert r.ready() == (False, ["postgres"])
        assert r.snapshot()["postgres"].state is State.UNKNOWN
    finally:
        await r.stop()


async def test_repeated_failures_report_unready_by_name_only() -> None:
    """/readyz is unauthenticated, so the error strings are not in the answer."""
    r = Registry(interval=0.01, timeout=0.5, stale_after=5.0, failure_threshold=2)
    r.register("postgres", _failing("password authentication failed for user hal"))
    await r.start()
    try:
        for _ in range(200):
            if r.snapshot()["postgres"].state is State.DOWN:
                break
            await asyncio.sleep(0.01)
        ok, failing = r.ready()
        assert (ok, failing) == (False, ["postgres"])
        report = r.report()
        assert "password authentication failed" not in str(report)
    finally:
        await r.stop()


async def test_detail_is_served_only_when_enabled() -> None:
    r = Registry(interval=0.01, timeout=0.5, stale_after=5.0, failure_threshold=1, detail=True)
    r.register("redis", _failing("connection refused"))
    await r.start()
    try:
        assert "connection refused" in str(r.report())
    finally:
        await r.stop()


async def test_a_hanging_check_is_a_failure_not_a_hang() -> None:
    r = Registry(interval=0.01, timeout=0.02, stale_after=5.0, failure_threshold=1)
    r.register("slow", _hangs)
    await r.start()
    try:
        assert r.snapshot()["slow"].state is State.DOWN
        assert "did not respond" in r.snapshot()["slow"].detail
    finally:
        await r.stop()


async def test_a_non_critical_dependency_does_not_hold_readiness_down() -> None:
    """A cache that is down degrades latency; a database that is down means the
    service cannot serve."""
    r = Registry(interval=0.01, timeout=0.5, stale_after=5.0, failure_threshold=1)
    r.register("postgres", _ok)
    r.register("cache", _failing(), critical=False)
    await r.start()
    try:
        assert r.ready() == (True, [])
        assert r.snapshot()["cache"].state is State.DOWN
    finally:
        await r.stop()


async def test_a_stale_verdict_ages_into_unknown() -> None:
    """The one failure mode a cached probe has and an inline one does not: a
    dead poller leaves the verdict frozen at `up` for as long as the process
    lives, and the probe keeps reporting healthy."""
    r = Registry(interval=10.0, timeout=0.5, stale_after=0.05)
    r.register("postgres", _ok)
    await r.start()
    try:
        assert r.ready() == (True, [])
        await asyncio.sleep(0.1)
        assert r.snapshot()["postgres"].state is State.UNKNOWN
        assert r.ready() == (False, ["postgres"])
    finally:
        await r.stop()


async def test_a_poller_that_raises_does_not_die_silently() -> None:
    """A bare create_task poller that raises is COMPLETELY silent, and asyncio
    only mentions it when the task is collected."""
    calls = 0

    async def flaky() -> None:
        nonlocal calls
        calls += 1
        if calls == 2:
            raise RuntimeError("transient")

    r = Registry(interval=0.01, timeout=0.5, stale_after=5.0, failure_threshold=99)
    r.register("flaky", flaky)
    await r.start()
    try:
        await asyncio.sleep(0.1)
        assert calls > 3, f"the poller stopped after {calls} calls"
    finally:
        await r.stop()


async def test_stop_waits_for_the_pollers() -> None:
    """cancel() is only a request; not awaiting truncates the task's cleanup."""
    r = Registry(interval=0.01, timeout=0.5, stale_after=5.0)
    r.register("postgres", _ok)
    await r.start()
    await r.stop()
    assert r._tasks == set()


async def test_registering_after_start_is_refused() -> None:
    """A probe added later reports unknown until its first poll, so readiness
    would flap on a running service."""
    r = Registry(interval=0.01, timeout=0.5, stale_after=5.0)
    await r.start()
    try:
        with pytest.raises(RuntimeError, match="before the registry starts"):
            r.register("late", _ok)
    finally:
        await r.stop()
