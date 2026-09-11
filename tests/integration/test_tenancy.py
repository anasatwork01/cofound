"""Cross-tenant denial, proven against the real schema as the real app role.

This is the assertion the whole row-level security arrangement exists to
satisfy, and it is worth understanding why it connects as `halyard_app` rather
than as the migration role: **as the owner, every check in this file passes for
the wrong reason.** A table's owner bypasses its own policies, and a superuser
or a role with BYPASSRLS bypasses them even when the table is FORCEd. Verified
on Postgres 18.6 — connected as `halyard` (which compose.yaml creates as
`rolsuper=t, rolbypassrls=t`), a policy-scoped query returns every row of every
tenant with no error and no warning.

So `test_the_app_role_is_not_privileged` is the most important test here. If it
fails, none of the others mean anything, and the isolation they appear to prove
is absent in production.

Run with `make test-integration`, which supplies both URLs.
"""

from __future__ import annotations

import json
import os
import re
import uuid
from collections.abc import AsyncIterator
from pathlib import Path

import asyncpg
import pytest

pytestmark = pytest.mark.integration

# Tables that hold tenant data and must never be readable across orgs. Not the
# full list — the completeness check below derives that from the catalog — but
# the ones whose leaking would matter most, named explicitly so a reader can see
# what is being asserted.
SENSITIVE = ["projects", "secrets", "ledger_entries", "org_members", "audit_log"]

# Tables deliberately without a policy, each for a stated reason. Kept here so
# that adding a table to this set is a visible decision in review rather than a
# silent omission from a query.
NO_POLICY_BY_DESIGN = {
    # A global identity that may belong to many orgs (SPEC §8), and sign-in must
    # find a user by email before any org is known.
    "users",
    # Console authentication (migration 00014). Sign-in happens before any org
    # is known and a user may belong to many orgs, so a policy would make
    # authentication impossible rather than safer. These store only token
    # HASHES, so a dump yields no usable credential -- that is the compensating
    # control, and it is in the columns rather than in a policy.
    "login_tokens",
    "oauth_identities",
    "user_sessions",
    # A global catalogue, identical for every tenant, granted SELECT only.
    "templates",
    "template_versions",
    "capabilities",
    "capability_versions",
    "price_books",
    # goose's own bookkeeping.
    "goose_db_version",
}


def _role_enum() -> list[str]:
    """The Role vocabulary from the shared contract.

    Read in a plain function rather than inside the async test, so the blocking
    file read does not sit on the event loop.
    """
    schema = json.loads(
        (Path(__file__).resolve().parents[2] / "packages/schema/common.schema.json").read_text()
    )
    return list(schema["$defs"]["Role"]["enum"])


def _require(var: str) -> str:
    value = os.environ.get(var)
    if value:
        return value
    if os.environ.get("CI"):
        pytest.fail(f"{var} is unset in CI — the integration harness is misconfigured")
    pytest.skip(f"{var} unset; run `make db-setup` then use `make test-integration`")
    raise AssertionError("unreachable")


@pytest.fixture
async def owner() -> AsyncIterator[asyncpg.Connection]:
    """The migration role. Bypasses RLS, which is why it is used only to seed."""
    conn = await asyncpg.connect(_require("DATABASE_URL"))
    try:
        yield conn
    finally:
        await conn.close()


@pytest.fixture
async def app() -> AsyncIterator[asyncpg.Connection]:
    """The role services actually connect as."""
    conn = await asyncpg.connect(_require("APP_DATABASE_URL"))
    try:
        yield conn
    finally:
        await conn.close()


@pytest.fixture
async def two_orgs(owner: asyncpg.Connection) -> AsyncIterator[tuple[str, str]]:
    """Two fully populated tenants, torn down afterwards.

    Seeded as the owner precisely because the owner bypasses RLS: the fixture
    has to be able to write rows for both orgs, which is the one thing the app
    role must not be able to do.
    """
    a, b = uuid.uuid4(), uuid.uuid4()
    suffix = uuid.uuid4().hex[:8]
    for org, label in ((a, "alpha"), (b, "beta")):
        await owner.execute(
            "insert into orgs (id, name, slug) values ($1, $2, $3)",
            org,
            label.title(),
            f"{label}-{suffix}",
        )
        user = uuid.uuid4()
        await owner.execute(
            "insert into users (id, email) values ($1, $2)", user, f"{label}-{suffix}@test.invalid"
        )
        await owner.execute(
            "insert into org_members (org_id, user_id, role) values ($1, $2, 'owner')", org, user
        )
        project = uuid.uuid4()
        await owner.execute(
            """insert into projects (id, org_id, name, slug)
               values ($1, $2, $3, $4)""",
            project,
            org,
            f"{label} app",
            f"{label}-app",
        )
        await owner.execute(
            """insert into secrets (project_id, org_id, env, key, ciphertext, dek_id)
               values ($1, $2, 'production', 'STRIPE_SECRET_KEY', $3, 'dek-test')""",
            project,
            org,
            b"\x00",
        )
        await owner.execute(
            """insert into ledger_entries (org_id, amount, kind, idempotency_key)
               values ($1, 100, 'grant', $2)""",
            org,
            f"seed-{label}-{suffix}",
        )
        await owner.execute(
            """insert into audit_log (org_id, actor_user_id, actor_kind, action, target)
               values ($1, $2, 'user', 'sign_in', '{}')""",
            org,
            user,
        )
    try:
        yield str(a), str(b)
    finally:
        # orgs cascades to everything tenant-scoped.
        await owner.execute("delete from orgs where id = any($1::uuid[])", [a, b])
        await owner.execute("delete from users where email like $1", f"%-{suffix}@test.invalid")


async def _scoped(conn: asyncpg.Connection, org: str, sql: str, *args: object) -> list:
    """Run sql inside a transaction scoped to org, as the app role does."""
    async with conn.transaction():
        await conn.execute("select set_config('app.org_id', $1, true)", org)
        return await conn.fetch(sql, *args)


# ----------------------------------------------------------- the premise


async def test_the_app_role_is_not_privileged(app: asyncpg.Connection) -> None:
    """If this fails, every other test in this file is meaningless.

    A superuser or a role with BYPASSRLS ignores policies entirely, including
    on tables marked FORCE. There is no way to detect that from a query's
    results — they simply contain more rows than they should.
    """
    row = await app.fetchrow(
        "select current_user as role, rolsuper, rolbypassrls "
        "from pg_roles where rolname = current_user"
    )
    assert row["rolsuper"] is False, (
        f"{row['role']} is a superuser, so row-level security is inert and this "
        "service has no tenant isolation"
    )
    assert row["rolbypassrls"] is False, (
        f"{row['role']} has BYPASSRLS, so row-level security is inert"
    )


async def test_the_owner_would_see_everything(
    owner: asyncpg.Connection, two_orgs: tuple[str, str]
) -> None:
    """Pins the reason the app role exists.

    Documents the trap as an executable fact: connected as the migration role,
    the same scoped query returns both tenants' rows. A future change that
    "simplifies" the two roles into one fails here.
    """
    a, _ = two_orgs
    rows = await _scoped(owner, a, "select org_id from projects where slug like '%-app'")
    assert len(rows) >= 2, (
        "the migration role no longer bypasses RLS — if that is deliberate, this "
        "test should be deleted along with the two-role arrangement"
    )


# --------------------------------------------------------------- reads


@pytest.mark.parametrize("table", SENSITIVE)
async def test_each_org_sees_only_its_own_rows(
    app: asyncpg.Connection, two_orgs: tuple[str, str], table: str
) -> None:
    a, b = two_orgs
    for org, other in ((a, b), (b, a)):
        rows = await _scoped(app, org, f"select org_id from {table}")
        assert rows, f"{table}: org {org} should see its own seeded row"
        owners = {str(r["org_id"]) for r in rows}
        assert owners == {org}, f"{table}: scoped to {org} but saw rows for {owners}"
        assert other not in owners


async def test_an_unset_org_returns_nothing_rather_than_erroring(
    app: asyncpg.Connection, two_orgs: tuple[str, str]
) -> None:
    """The reason the policy is spelled with nullif.

    SPEC §6 writes `current_setting('app.org_id')::uuid`. After a transaction
    that set it ends, the setting is the empty string rather than NULL, and
    `''::uuid` raises `invalid input syntax for type uuid`. Failing closed with
    zero rows beats failing closed with an exception, because the exception
    surfaces as a 500 on an unrelated endpoint.
    """
    for table in SENSITIVE:
        rows = await app.fetch(f"select 1 from {table}")
        assert rows == [], f"{table} returned rows with no org scope set"


async def test_the_naive_policy_expression_would_have_raised(
    app: asyncpg.Connection, two_orgs: tuple[str, str]
) -> None:
    """Shows what §6's literal spelling does, so the deviation stays justified.

    The order of these two assertions is the whole finding, and getting it wrong
    is why the first version of this test passed for the wrong reason. On a
    connection that has never had `app.org_id` set, `current_setting(..., true)`
    returns NULL, and `NULL::uuid` is simply NULL -- harmless. The empty string
    only appears AFTER a transaction has set and released it, which on a pooled
    connection is the normal state for every request after the first.

    So the naive policy works on a cold connection and starts raising once the
    pool is warm -- the worst possible failure shape, because it passes in
    testing and breaks under traffic.
    """
    a, _ = two_orgs

    cold = await app.fetchval("select current_setting('app.org_id', true)::uuid")
    assert cold is None, "expected NULL on a connection that has never been scoped"

    async with app.transaction():
        await app.execute("select set_config('app.org_id', $1, true)", a)

    assert await app.fetchval("select current_setting('app.org_id', true)") == ""
    with pytest.raises(asyncpg.PostgresError, match="invalid input syntax for type uuid"):
        await app.fetchval("select current_setting('app.org_id', true)::uuid")

    # And the spelling actually used stays safe in the same state.
    assert (
        await app.fetchval("select nullif(current_setting('app.org_id', true), '')::uuid") is None
    )


# -------------------------------------------------------------- writes


async def test_inserting_for_another_org_is_refused(
    app: asyncpg.Connection, two_orgs: tuple[str, str]
) -> None:
    """A `FOR ALL` policy's USING expression also governs writes.

    Verified: Postgres reuses USING as the check on new rows when WITH CHECK is
    omitted, so no second expression is needed and there is only one place to
    keep correct.
    """
    a, b = two_orgs
    with pytest.raises(asyncpg.InsufficientPrivilegeError):
        await _scoped(
            app,
            a,
            "insert into projects (org_id, name, slug) values ($1, 'Smuggled', 'smuggled')",
            uuid.UUID(b),
        )


async def test_deleting_everything_only_deletes_your_own(
    app: asyncpg.Connection, owner: asyncpg.Connection, two_orgs: tuple[str, str]
) -> None:
    """An unqualified DELETE is the shape a bug actually takes."""
    a, b = two_orgs
    async with app.transaction():
        await app.execute("select set_config('app.org_id', $1, true)", a)
        await app.execute("delete from secrets")

    remaining = await owner.fetch(
        "select org_id from secrets where org_id = any($1::uuid[])",
        [uuid.UUID(a), uuid.UUID(b)],
    )
    assert [str(r["org_id"]) for r in remaining] == [b], (
        "deleting all secrets as one org must leave the other org's untouched"
    )


async def test_updating_another_orgs_row_affects_nothing(
    app: asyncpg.Connection, owner: asyncpg.Connection, two_orgs: tuple[str, str]
) -> None:
    a, b = two_orgs
    async with app.transaction():
        await app.execute("select set_config('app.org_id', $1, true)", a)
        await app.execute("update projects set name = 'hijacked'")

    names = await owner.fetch("select name from projects where org_id = $1", uuid.UUID(b))
    assert [r["name"] for r in names] == ["beta app"]


# --------------------------------------------- the denormalised org_id


async def test_a_child_row_cannot_disagree_with_its_parents_org(
    owner: asyncpg.Connection, two_orgs: tuple[str, str]
) -> None:
    """The integrity hole denormalising org_id would otherwise open.

    A row whose org_id disagrees with its parent's is invisible to its real
    owner and visible to someone else. The composite foreign key onto
    `projects (id, org_id)` makes it unrepresentable, which is what earns the
    30x policy speed-up. Attempted as the OWNER, because RLS would refuse this
    for a different reason and prove nothing about the constraint.
    """
    a, b = two_orgs
    project = await owner.fetchval("select id from projects where org_id = $1", uuid.UUID(a))
    with pytest.raises(asyncpg.ForeignKeyViolationError):
        await owner.execute(
            """insert into secrets (project_id, org_id, env, key, ciphertext, dek_id)
               values ($1, $2, 'production', 'MISMATCHED', $3, 'dek')""",
            project,
            uuid.UUID(b),
            b"\x00",
        )


async def test_a_project_cannot_be_moved_between_orgs_behind_its_children(
    owner: asyncpg.Connection, two_orgs: tuple[str, str]
) -> None:
    """The same constraint, from the other direction.

    Reparenting a project would silently leave its secrets, deployments and
    ledger references pointing at the old org. The foreign key forces such a
    move to be an explicit multi-table operation instead.
    """
    a, b = two_orgs
    with pytest.raises(asyncpg.ForeignKeyViolationError):
        await owner.execute(
            "update projects set org_id = $1 where org_id = $2", uuid.UUID(b), uuid.UUID(a)
        )


# ----------------------------------------------------- completeness


async def test_no_readable_table_is_left_unprotected(owner: asyncpg.Connection) -> None:
    """The check that catches the migration nobody remembered to protect.

    Asserting each table individually would mean a table added in task 0.9 is
    simply absent from the test rather than failing it. This derives the list
    from the catalog: anything the app role can SELECT must have row-level
    security enabled, FORCEd, and at least one policy — unless it is named in
    NO_POLICY_BY_DESIGN, which requires editing this file and so shows up in
    review.
    """
    rows = await owner.fetch(
        """
        select c.relname as name,
               c.relrowsecurity as rls,
               c.relforcerowsecurity as forced,
               (select count(*) from pg_policy p where p.polrelid = c.oid) as policies
        from pg_class c
        join pg_namespace n on n.oid = c.relnamespace
        where n.nspname = 'public'
          and c.relkind = 'r'
          and has_table_privilege('halyard_app', c.oid, 'SELECT')
        order by 1
        """
    )
    assert rows, "the app role can read nothing; is the schema migrated?"

    unprotected = [
        f"{r['name']}(rls={r['rls']} forced={r['forced']} policies={r['policies']})"
        for r in rows
        if r["name"] not in NO_POLICY_BY_DESIGN
        and not (r["rls"] and r["forced"] and r["policies"] >= 1)
    ]
    assert not unprotected, (
        "these tables are readable by the app role with no effective row-level "
        f"security: {unprotected}. Add a policy in a migration, or add the table "
        "to NO_POLICY_BY_DESIGN with a reason."
    )


async def test_the_by_design_exclusions_still_exist(owner: asyncpg.Connection) -> None:
    """Stops NO_POLICY_BY_DESIGN silently accumulating names.

    A table that was renamed or dropped would leave an entry here that quietly
    excuses nothing, and the next table to take that name inherits the excuse.
    """
    present = {
        r["tablename"]
        for r in await owner.fetch("select tablename from pg_tables where schemaname = 'public'")
    }
    stale = sorted(NO_POLICY_BY_DESIGN - present)
    assert not stale, f"NO_POLICY_BY_DESIGN names tables that do not exist: {stale}"


async def test_the_role_vocabulary_matches_the_shared_schema(
    owner: asyncpg.Connection,
) -> None:
    """org_members.role and common.schema.json's Role must agree.

    SPEC §8 defines four roles and the check constraint repeats them. A fifth
    role added to one and not the other produces either an API that can write a
    value the console cannot render, or a constraint violation on a legitimate
    request.
    """
    constraint = await owner.fetchval(
        """select pg_get_constraintdef(c.oid)
           from pg_constraint c
           join pg_class t on t.oid = c.conrelid
           where t.relname = 'org_members' and c.contype = 'c'"""
    )
    in_db = set(re.findall(r"'([a-z]+)'", constraint or ""))
    in_schema = set(_role_enum())

    assert in_db == in_schema, (
        f"org_members.role allows {sorted(in_db)} but common.schema.json's Role "
        f"is {sorted(in_schema)}"
    )


# ------------------------------------------------- policy mode hardening


async def test_every_tenant_table_has_a_restrictive_guard(
    owner: asyncpg.Connection,
) -> None:
    """Migration 00015. Permissive policies OR; restrictive ones AND.

    With only permissive policies, any policy added later WIDENS access. A
    restrictive policy carrying the same org comparison is AND-ed with whatever
    else exists, so the worst a careless addition can do is grant access the
    tenant check then removes again.
    """
    rows = await owner.fetch(
        """
        select c.relname as name,
               count(*) filter (where p.polpermissive)     as permissive,
               count(*) filter (where not p.polpermissive) as restrictive
        from pg_class c
        join pg_namespace n on n.oid = c.relnamespace
        join pg_policy p on p.polrelid = c.oid
        where n.nspname = 'public' and c.relkind = 'r'
        group by 1
        order by 1
        """
    )
    assert rows, "no policies found at all; is the schema migrated?"
    missing = [r["name"] for r in rows if r["restrictive"] < 1]
    assert not missing, (
        f"these tables have no restrictive tenant guard: {missing}. A later "
        "permissive policy could widen access on them."
    )


async def test_a_careless_permissive_policy_cannot_widen_access(
    owner: asyncpg.Connection, app: asyncpg.Connection, two_orgs: tuple[str, str]
) -> None:
    """The failure migration 00015 exists to prevent, performed.

    Adds exactly the policy someone would write for an internal admin view, and
    asserts the tenant boundary survives it. Without the restrictive guard this
    test would see both orgs' secrets.
    """
    a, b = two_orgs
    await owner.execute("create policy careless_support_view on secrets using (true)")
    try:
        rows = await _scoped(app, a, "select org_id from secrets")
        owners = {str(r["org_id"]) for r in rows}
        assert owners == {a}, (
            f"a permissive `using (true)` policy widened access to {owners}; "
            "the restrictive guard from migration 00015 is missing or ineffective"
        )
        assert b not in owners
    finally:
        await owner.execute("drop policy careless_support_view on secrets")


# ------------------------------------------- user-scoped membership (00016)


async def _as_user(conn: asyncpg.Connection, user: str, sql: str, *args: object) -> list:
    """Run sql scoped to a USER but not to an org, as the tenancy resolver does."""
    async with conn.transaction():
        await conn.execute("select set_config('app.user_id', $1, true)", user)
        return await conn.fetch(sql, *args)


async def test_a_user_can_list_their_own_orgs_without_an_org_scope(
    app: asyncpg.Connection, owner: asyncpg.Connection, two_orgs: tuple[str, str]
) -> None:
    """The circularity migration 00016 exists to break.

    Resolving which org a request is about means reading `orgs` by slug and
    `org_members` by user, and both happen BEFORE any org is current. With an
    org-keyed policy those reads return nothing, and the tenancy middleware
    reports "that org does not exist" for every org that does.
    """
    a, b = two_orgs
    user_a = await owner.fetchval("select user_id from org_members where org_id = $1", uuid.UUID(a))

    orgs = await _as_user(app, str(user_a), "select id from orgs")
    assert [str(r["id"]) for r in orgs] == [a], (
        "a user scoped to themselves should see exactly the orgs they belong to"
    )

    members = await _as_user(app, str(user_a), "select org_id, user_id from org_members")
    assert [str(r["user_id"]) for r in members] == [str(user_a)]
    assert b not in {str(r["org_id"]) for r in members}


async def test_a_user_scope_does_not_expose_another_users_membership(
    app: asyncpg.Connection, owner: asyncpg.Connection, two_orgs: tuple[str, str]
) -> None:
    """The widened policy must widen by exactly one dimension and no more."""
    a, b = two_orgs
    user_a = await owner.fetchval("select user_id from org_members where org_id = $1", uuid.UUID(a))
    user_b = await owner.fetchval("select user_id from org_members where org_id = $1", uuid.UUID(b))

    rows = await _as_user(app, str(user_a), "select user_id from org_members")
    seen = {str(r["user_id"]) for r in rows}
    assert str(user_b) not in seen, "scoping to one user exposed another user's membership rows"


async def test_a_user_scope_alone_opens_no_other_table(
    app: asyncpg.Connection, owner: asyncpg.Connection, two_orgs: tuple[str, str]
) -> None:
    """00016 widened `orgs` and `org_members`. Nothing else.

    Every other tenant table still compares against app.org_id, which is unset
    in a user scope, so it matches nothing. That is the correct failure: a query
    that needs an org must say which.
    """
    a, _ = two_orgs
    user_a = await owner.fetchval("select user_id from org_members where org_id = $1", uuid.UUID(a))
    for table in ("projects", "secrets", "ledger_entries", "audit_log"):
        rows = await _as_user(app, str(user_a), f"select 1 from {table}")
        assert rows == [], f"{table} was readable with only a user scope"


async def test_creating_an_org_requires_scoping_to_its_own_new_id(
    app: asyncpg.Connection, owner: asyncpg.Connection
) -> None:
    """How `POST /v1/orgs` can work at all, and a property it gets for free.

    The policy on `orgs` is keyed on the org's own id, so an unscoped insert is
    refused — you cannot create an org without saying which org you are
    creating. Scoping to the id you are about to insert satisfies it, and an
    insert naming any OTHER id is refused, so a request cannot create an org it
    did not declare.
    """
    new = uuid.uuid4()
    other = uuid.uuid4()
    slug = f"created-{new.hex[:8]}"
    try:
        # Unscoped: refused.
        with pytest.raises(asyncpg.InsufficientPrivilegeError):
            await app.execute(
                "insert into orgs (id, name, slug) values ($1, 'Unscoped', $2)", new, slug
            )

        # Scoped, but naming a different id: refused.
        async with app.transaction():
            await app.execute("select set_config('app.org_id', $1, true)", str(new))
            with pytest.raises(asyncpg.InsufficientPrivilegeError):
                await app.execute(
                    "insert into orgs (id, name, slug) values ($1, 'Mismatched', $2)",
                    other,
                    slug,
                )

        # Scoped to its own id: allowed.
        async with app.transaction():
            await app.execute("select set_config('app.org_id', $1, true)", str(new))
            await app.execute(
                "insert into orgs (id, name, slug) values ($1, 'Scoped To Itself', $2)",
                new,
                slug,
            )

        # And it is there — read back in the same scope, since that is the only
        # scope in which it is visible.
        async with app.transaction():
            await app.execute("select set_config('app.org_id', $1, true)", str(new))
            assert await app.fetchval("select slug from orgs where id = $1", new) == slug

        # The other id was never created.
        assert await owner.fetchval("select count(*) from orgs where id = $1", other) == 0
    finally:
        await owner.execute("delete from orgs where id = any($1::uuid[])", [new, other])
