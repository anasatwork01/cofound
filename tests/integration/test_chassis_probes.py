"""The readiness probes, against real Postgres and Redis.

The unit tests in packages/pychassis prove the registry's state machine with
fake checks. These prove the checks themselves: that the query and the command
each service will actually issue work against the containers, and — more
usefully — that an unreachable dependency is reported as unready promptly
rather than after redis-py's ten retries with exponential backoff.
"""

from __future__ import annotations

import asyncio
import os
import time

import pytest
from halyard_chassis.health import Registry, State
from halyard_sandboxd.probes import PostgresProbe, RedisProbe

pytestmark = pytest.mark.integration


def _require(var: str) -> str:
    value = os.environ.get(var)
    if value:
        return value
    if os.environ.get("CI"):
        pytest.fail(f"{var} is unset in CI — the service containers are misconfigured")
    pytest.skip(f"{var} unset; run `make services-up` first")
    raise AssertionError("unreachable")


async def test_both_probes_report_up_against_the_containers() -> None:
    registry = Registry(interval=10.0, timeout=2.0, stale_after=60.0)
    postgres = PostgresProbe(_require("DATABASE_URL"))
    redis = RedisProbe(_require("REDIS_URL"))
    registry.register("postgres", postgres.check)
    registry.register("redis", redis.check)
    await registry.start()
    try:
        ok, failing = registry.ready()
        assert (ok, failing) == (True, [])
        snapshot = registry.snapshot()
        assert snapshot["postgres"].state is State.UP
        assert snapshot["redis"].state is State.UP
    finally:
        await registry.stop()
        await postgres.close()
        await redis.close()


async def test_the_postgres_probe_is_fast_enough_to_poll() -> None:
    """It runs every READINESS_INTERVAL on every replica, so its cost is
    multiplied by the fleet. `execute` with no arguments takes the simple-query
    path, which also keeps it out of the statement cache."""
    probe = PostgresProbe(_require("DATABASE_URL"))
    try:
        await probe.check()  # builds the pool
        started = time.perf_counter()
        for _ in range(5):
            await probe.check()
        per_op = (time.perf_counter() - started) / 5 * 1000
        # A fresh connection per poll measured 26.5 ms here; through the pool it
        # is under 1 ms. The threshold is loose enough not to be flaky and tight
        # enough to fail if the pool is ever dropped.
        assert per_op < 5.0, f"{per_op:.1f} ms/op — is the probe reconnecting each time?"
    finally:
        await probe.close()


async def test_an_unreachable_redis_fails_within_the_probe_budget() -> None:
    """The reason the probe builds a dedicated client.

    redis-py defaults every connection to ten retries with exponential jitter
    backoff, which makes a "cheap" ping take many seconds against an
    unreachable host regardless of socket_connect_timeout — long after the
    registry's timeout has given up on it.
    """
    _require("REDIS_URL")
    probe = RedisProbe("redis://127.0.0.1:1/0")
    check = probe.check
    started = time.perf_counter()
    # Any failure will do; what is being measured is how long it takes to
    # arrive, not which exception type redis-py chose.
    with pytest.raises(Exception):
        async with asyncio.timeout(5):
            await check()
    elapsed = time.perf_counter() - started
    assert elapsed < 3.0, f"the probe took {elapsed:.1f}s; retries are not disabled"


async def test_an_unreachable_dependency_reports_unready_not_hung() -> None:
    _require("DATABASE_URL")
    registry = Registry(interval=10.0, timeout=0.5, stale_after=60.0, failure_threshold=1)
    registry.register("postgres", PostgresProbe("postgres://nobody@127.0.0.1:1/nothing").check)
    started = time.perf_counter()
    await registry.start()
    try:
        ok, failing = registry.ready()
        assert (ok, failing) == (False, ["postgres"])
        assert time.perf_counter() - started < 3.0
    finally:
        await registry.stop()
