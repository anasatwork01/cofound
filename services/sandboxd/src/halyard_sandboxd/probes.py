"""Readiness checks for sandboxd's dependencies.

Each holds its own connection rather than opening one per poll. That is not
premature optimisation: the registry polls every READINESS_INTERVAL on every
replica, and measured against the local containers a fresh asyncpg connection
costs 26.5 ms against 0.589 ms through a pool -- 45x, and a full TCP and
authentication handshake every two seconds per replica. A readiness probe that
is itself a load generator is a bad readiness probe.

Both expose `close`, registered with the chassis shutdown so the connection is
released in phase 4 rather than dropped.

Task 0.6 introduces the real database layer. When it does, these should take the
service's pool instead of building their own -- the shape is already right, only
the ownership changes.
"""

from __future__ import annotations

from typing import Any


class PostgresProbe:
    """`execute("SELECT 1")` over a small dedicated pool.

    With no query arguments this takes asyncpg's simple-query path, so unlike
    `fetchval` it does not add an entry to the statement cache -- a health check
    should not evict a real query's prepared statement.

    asyncpg has no built-in pool health check: its pool only tests a LOCAL
    `is_closed()` flag on acquire and recycles idle connections on a timer. So
    this poller IS the pool health check; there is nothing built in to lean on.
    """

    __slots__ = ("_dsn", "_pool")

    def __init__(self, dsn: str) -> None:
        self._dsn = dsn
        self._pool: Any = None

    async def check(self) -> None:
        import asyncpg

        if self._pool is None:
            # Built on first use, and left as None if that fails, so an
            # unreachable database is retried rather than latched.
            self._pool = await asyncpg.create_pool(
                self._dsn,
                min_size=1,
                max_size=2,
                # The probe's own budget is enforced by the registry; these stop
                # asyncpg waiting past it internally.
                timeout=2.0,
                command_timeout=2.0,
            )
        await self._pool.execute("SELECT 1")

    async def close(self) -> None:
        if self._pool is not None:
            await self._pool.close()
            self._pool = None


class RedisProbe:
    """`ping()` on a client with retries disabled.

    The disabled retries matter more than the command: redis-py defaults every
    connection to ten retries with exponential jitter backoff on
    ConnectionError, which makes a "cheap" ping take many seconds against an
    unreachable host regardless of `socket_connect_timeout` -- so the probe
    would report a timeout long after the registry had given up on it.
    `tests/integration/test_chassis_probes.py` asserts the failure arrives in
    under three seconds, which is what fails if this is ever "simplified".

    `health_check_interval` is deliberately NOT set here. On this client it
    would be redundant -- every use is already a ping -- and it belongs on the
    service's ordinary Redis client, where it validates request-path traffic.
    """

    __slots__ = ("_client", "_url")

    def __init__(self, url: str) -> None:
        self._url = url
        self._client: Any = None

    async def check(self) -> None:
        from redis.asyncio import Redis
        from redis.backoff import NoBackoff
        from redis.retry import Retry

        if self._client is None:
            self._client = Redis.from_url(
                self._url,
                retry=Retry(NoBackoff(), 0),
                socket_connect_timeout=1.0,
                socket_timeout=1.0,
            )
        await self._client.ping()

    async def close(self) -> None:
        if self._client is not None:
            await self._client.aclose()
            self._client = None
