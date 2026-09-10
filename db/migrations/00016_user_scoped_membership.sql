-- Make `orgs` and `org_members` readable by the user they belong to.
--
-- ## The circularity this fixes
--
-- Migration 00013 gives every tenant table a policy keyed on `app.org_id`.
-- For two tables that is circular: resolving WHICH org a request is about means
-- reading `orgs` by slug and `org_members` by user, and both of those reads
-- happen BEFORE any org is known. With an org-keyed policy they return zero
-- rows, so the tenancy middleware reports "that org does not exist" for every
-- org that does. Found by task 0.8's middleware tests.
--
-- The same circularity is why `users` has no policy at all (00013's footer). The
-- difference is that `orgs` and `org_members` carry tenant data worth
-- protecting — org names and member lists — so dropping their policies would
-- give up more than necessary.
--
-- ## The fix
--
-- A second per-transaction setting, `app.user_id`, alongside `app.org_id`. The
-- user is known before the org, so a policy keyed on it is not circular. These
-- two tables' policies then admit a row when EITHER the org matches the current
-- scope, or the row belongs to an org the current user is a member of.
--
-- `org_members`' own policy is deliberately keyed only on columns and settings,
-- never on a subquery, so `orgs`' policy can read it without recursion.
--
-- ## Why this replaces rather than adds
--
-- A policy is not data. Migrations here are forward-only and additive with
-- respect to SCHEMA and ROWS (SPEC §0 rule 6); replacing a policy loses
-- nothing and cannot be expressed as an addition, because two permissive
-- policies would OR together and the org-keyed one would then be pointless.
-- The restrictive guards from 00015 are replaced for the same reason and with
-- the same shape, so the pairing survives.

-- +goose Up

-- +goose StatementBegin
-- The current user, when one has been established. NULL before sign-in, and a
-- policy comparing against NULL matches nothing — the same fail-closed
-- behaviour as app.org_id, for the same reason (see 00013).
create or replace function halyard_current_user_id() returns uuid
  language sql stable parallel safe
  as $$ select nullif(current_setting('app.user_id', true), '')::uuid $$;
-- +goose StatementEnd

-- +goose StatementBegin
create or replace function halyard_current_org_id() returns uuid
  language sql stable parallel safe
  as $$ select nullif(current_setting('app.org_id', true), '')::uuid $$;
-- +goose StatementEnd

-- +goose StatementBegin
grant execute on function halyard_current_user_id() to halyard_app;
-- +goose StatementEnd

-- +goose StatementBegin
grant execute on function halyard_current_org_id() to halyard_app;
-- +goose StatementEnd

-- ------------------------------------------------------------ org_members

-- +goose StatementBegin
drop policy tenant_isolation on org_members;
-- +goose StatementEnd

-- +goose StatementBegin
drop policy tenant_isolation_guard on org_members;
-- +goose StatementEnd

-- +goose StatementBegin
-- "Rows about me, or rows in the org I am scoped to." No subquery, so `orgs`'
-- policy below can read this table without recursion.
create policy tenant_isolation on org_members
  using (
    user_id = halyard_current_user_id()
    or org_id = halyard_current_org_id()
  );
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation_guard on org_members
  as restrictive
  using (
    user_id = halyard_current_user_id()
    or org_id = halyard_current_org_id()
  );
-- +goose StatementEnd

-- ------------------------------------------------------------------- orgs

-- +goose StatementBegin
drop policy tenant_isolation on orgs;
-- +goose StatementEnd

-- +goose StatementBegin
drop policy tenant_isolation_guard on orgs;
-- +goose StatementEnd

-- +goose StatementBegin
-- The org I am scoped to, or any org I am a member of.
--
-- The second clause is what makes the org switcher (§8: "Org switching in the
-- project picker") and the sign-in response possible: both list every org the
-- user belongs to, before any one of them is current.
--
-- `id = halyard_current_org_id()` is kept as the first clause so that creating
-- an org still works. A brand-new org has no members yet, so only the scoped
-- form admits the INSERT — and it admits ONLY the id the caller scoped to,
-- which is a property worth having: a request cannot create an org it did not
-- declare. Verified both ways.
create policy tenant_isolation on orgs
  using (
    id = halyard_current_org_id()
    or id in (select org_id from org_members where user_id = halyard_current_user_id())
  );
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation_guard on orgs
  as restrictive
  using (
    id = halyard_current_org_id()
    or id in (select org_id from org_members where user_id = halyard_current_user_id())
  );
-- +goose StatementEnd
