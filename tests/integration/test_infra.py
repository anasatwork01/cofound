"""Integration tests for the local and CI service containers.

These exist so the Postgres and Redis wiring is proven now rather than being
unverified configuration until task 0.6 needs it. They assert the specific
capabilities the control plane depends on, not merely that a port is open — a
`select 1` would pass against a Postgres that cannot do what SPEC section 6
requires.
"""

import os

import asyncpg
import pytest
from redis import asyncio as aioredis

pytestmark = pytest.mark.integration


def _require(var: str) -> str:
    value = os.environ.get(var)
    if value:
        return value
    if os.environ.get("CI"):
        # A skip here would be a false green: CI defines these, so a missing
        # value means the harness is broken and the suite never ran.
        pytest.fail(f"{var} is unset in CI — the service containers are misconfigured")
    pytest.skip(f"{var} unset; run `make services-up` first")


async def test_postgres_generates_uuids_without_an_extension() -> None:
    """SPEC section 6 defaults every primary key to gen_random_uuid().

    It moved into core in Postgres 13. Asserting it here means a future image
    change that breaks the assumption fails in this suite rather than inside a
    migration.
    """
    conn = await asyncpg.connect(_require("DATABASE_URL"))
    try:
        first = await conn.fetchval("select gen_random_uuid()")
        second = await conn.fetchval("select gen_random_uuid()")
        assert first != second
    finally:
        await conn.close()


async def test_postgres_rls_session_variable_resets_to_empty_string() -> None:
    """SPEC section 6 scopes row-level security to current_setting('app.org_id').

    The reset value is not what you would guess, and it decides how the policy
    must be written:

    - on a connection where the setting has never been set, the missing_ok form
      returns NULL;
    - after any `set local` has ended, it returns the empty string, not NULL;
    - and `''::uuid` raises `invalid input syntax for type uuid`.

    So the policy shape in SPEC section 6 fails *closed* on an unscoped
    connection — an error returns no rows — but it fails with a 500 rather than
    an empty result, and on a pooled connection that has served one scoped
    request before. Task 0.6 should therefore wrap the cast in `nullif`, which
    the last assertion here pins down.
    """
    conn = await asyncpg.connect(_require("DATABASE_URL"))
    try:
        assert await conn.fetchval("select current_setting('app.org_id', true)") is None

        org = "a3f1c2d4-0000-0000-0000-000000000000"
        async with conn.transaction():
            await conn.execute(f"set local app.org_id = '{org}'")
            assert await conn.fetchval("select current_setting('app.org_id', true)") == org

        # `set local` does not leak a value past its transaction — but it does
        # leave the setting defined and empty rather than undefined.
        assert await conn.fetchval("select current_setting('app.org_id', true)") == ""

        # The cast SPEC section 6 uses raises on that empty string.
        with pytest.raises(asyncpg.exceptions.InvalidTextRepresentationError):
            await conn.fetchval("select current_setting('app.org_id', true)::uuid")

        # `nullif` is the expression a policy can evaluate in both states: NULL
        # matches no row, so an unscoped connection sees nothing instead of an
        # error.
        assert (
            await conn.fetchval("select nullif(current_setting('app.org_id', true), '')::uuid")
            is None
        )
        async with conn.transaction():
            await conn.execute(f"set local app.org_id = '{org}'")
            scoped = await conn.fetchval(
                "select nullif(current_setting('app.org_id', true), '')::uuid"
            )
            assert str(scoped) == org
    finally:
        await conn.close()


async def test_redis_honours_key_expiry() -> None:
    """SPEC section 3.3 puts the write lease in Redis with a TTL.

    A lease that never expires means a crashed sandbox locks a project's editor
    permanently, so TTL support is a correctness requirement, not a nicety.
    """
    client = aioredis.from_url(_require("REDIS_URL"), decode_responses=True)
    key = "halyard:test:lease:0.2"
    try:
        await client.set(key, "agent", ex=30)
        assert await client.get(key) == "agent"
        ttl = await client.ttl(key)
        assert 0 < ttl <= 30

        # SETNX is how the lease is claimed: a second holder must be refused.
        assert await client.set(key, "human", nx=True) is None
        assert await client.get(key) == "agent"
    finally:
        await client.delete(key)
        await client.aclose()
