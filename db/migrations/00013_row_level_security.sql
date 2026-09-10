-- Row-level security. SPEC §6: "enable on every table with `org_id`".
--
-- This file is generated; see the generator's own comment for why. It is
-- committed and reviewed as ordinary SQL because a security control should be
-- readable statement by statement.
--
-- ## The thing that makes RLS silently do nothing
--
-- A table's OWNER bypasses its own policies, and a role with BYPASSRLS or
-- SUPERUSER bypasses them even when the table is FORCEd. Verified on Postgres
-- 18.6: connected as the owner, a policy-scoped query returned every row of
-- every tenant, with no error and no warning. The local `halyard` role in
-- compose.yaml is `rolsuper=t, rolbypassrls=t`, so a service that connects with
-- the migration credentials has NO tenant isolation at all while appearing to.
--
-- Hence two things below, and neither is optional:
--
--   1. A dedicated `halyard_app` role that is not the owner, not a superuser,
--      and has no BYPASSRLS. Services connect as that role. It gets explicit
--      grants, never ownership.
--   2. `force row level security` on every tenant table, so that even a
--      connection that somehow arrives as the owner is still filtered.
--
-- tests/integration asserts both, and asserts that the role the tests connect
-- as cannot read another org's rows -- which is the only assertion that would
-- actually fail if this arrangement broke.
--
-- ## Why the policy is spelled with nullif
--
-- §6 writes `current_setting('app.org_id')::uuid`. After a `SET LOCAL`
-- transaction ends, that setting is the EMPTY STRING rather than NULL, and
-- `''::uuid` raises `invalid input syntax for type uuid`. Verified both ways:
-- the naive form errors, and the `nullif(..., '')` form yields NULL, which
-- matches zero rows. Failing closed with no rows beats failing closed with an
-- exception, because the exception surfaces as a 500 on an unrelated endpoint.
--
-- ## Why USING has no matching WITH CHECK
--
-- For a `FOR ALL` policy Postgres reuses the USING expression as the check on
-- rows being written when WITH CHECK is omitted. Verified: with USING only, an
-- INSERT naming another org is refused with "new row violates row-level
-- security policy", and so is an UPDATE that would move a row out of the
-- current org. Spelling it twice would add no protection and would create two
-- places to keep in step.

-- +goose Up

-- +goose StatementBegin
-- Idempotent, because role existence is cluster-wide rather than per-database:
-- a second database in the same cluster running this migration must not fail.
--
-- NOLOGIN and no password on purpose. A credential in a migration is a
-- credential in git. The operator grants login out of band; locally
-- `make db-setup` does it from the environment.
do $$
begin
  if not exists (select 1 from pg_roles where rolname = 'halyard_app') then
    create role halyard_app nologin;
  end if;
end $$;
-- +goose StatementEnd

-- +goose StatementBegin
-- Never `alter default privileges`. A table added by a later migration should
-- fail loudly with "permission denied" until someone grants on it explicitly,
-- because the alternative failure -- a new table that the app can read and that
-- has no policy yet -- is a silent cross-tenant read. Loud beats silent.
grant usage on schema public to halyard_app;
-- +goose StatementEnd

-- ============================================================ grants

-- +goose StatementBegin
grant select, insert, update, delete on
  ad_accounts,
  app_databases,
  audit_log,
  branches,
  campaigns,
  checkpoints,
  connectors,
  deployments,
  domains,
  github_installations,
  grants,
  holds,
  ledger_entries,
  org_balances,
  org_members,
  project_capabilities,
  projects,
  proposals,
  rank_snapshots,
  repos,
  secrets,
  seo_issues,
  sessions,
  sync_jobs,
  timeline_events,
  turns,
  usage_events
to halyard_app;
-- +goose StatementEnd

-- +goose StatementBegin
-- `orgs` and `users` are written by sign-up and by org creation, so the app
-- needs more than read on them. Neither carries an org_id; see below.
grant select, insert, update on orgs, users to halyard_app;
-- +goose StatementEnd

-- +goose StatementBegin
-- The global catalogue is read-only to the application. New templates and
-- capability versions are published by a deliberate operator action, not by a
-- request, so the app role has no write path to them at all.
grant select on capabilities, capability_versions, price_books, template_versions, templates to halyard_app;
-- +goose StatementEnd

-- +goose StatementBegin
-- bigserial columns need their sequences.
grant usage, select on all sequences in schema public to halyard_app;
-- +goose StatementEnd

-- ==================================================== tenant policies
--
-- One policy per table, identical in shape. `orgs` compares `id` because it IS
-- the org row; everything else compares `org_id`.

-- +goose StatementBegin
alter table orgs enable row level security;
-- +goose StatementEnd

-- +goose StatementBegin
alter table orgs force row level security;
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation on orgs
  using (id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- +goose StatementBegin
alter table ad_accounts enable row level security;
-- +goose StatementEnd

-- +goose StatementBegin
alter table ad_accounts force row level security;
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation on ad_accounts
  using (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- +goose StatementBegin
alter table app_databases enable row level security;
-- +goose StatementEnd

-- +goose StatementBegin
alter table app_databases force row level security;
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation on app_databases
  using (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- +goose StatementBegin
alter table audit_log enable row level security;
-- +goose StatementEnd

-- +goose StatementBegin
alter table audit_log force row level security;
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation on audit_log
  using (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- +goose StatementBegin
alter table branches enable row level security;
-- +goose StatementEnd

-- +goose StatementBegin
alter table branches force row level security;
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation on branches
  using (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- +goose StatementBegin
alter table campaigns enable row level security;
-- +goose StatementEnd

-- +goose StatementBegin
alter table campaigns force row level security;
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation on campaigns
  using (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- +goose StatementBegin
alter table checkpoints enable row level security;
-- +goose StatementEnd

-- +goose StatementBegin
alter table checkpoints force row level security;
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation on checkpoints
  using (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- +goose StatementBegin
alter table connectors enable row level security;
-- +goose StatementEnd

-- +goose StatementBegin
alter table connectors force row level security;
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation on connectors
  using (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- +goose StatementBegin
alter table deployments enable row level security;
-- +goose StatementEnd

-- +goose StatementBegin
alter table deployments force row level security;
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation on deployments
  using (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- +goose StatementBegin
alter table domains enable row level security;
-- +goose StatementEnd

-- +goose StatementBegin
alter table domains force row level security;
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation on domains
  using (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- +goose StatementBegin
alter table github_installations enable row level security;
-- +goose StatementEnd

-- +goose StatementBegin
alter table github_installations force row level security;
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation on github_installations
  using (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- +goose StatementBegin
alter table grants enable row level security;
-- +goose StatementEnd

-- +goose StatementBegin
alter table grants force row level security;
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation on grants
  using (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- +goose StatementBegin
alter table holds enable row level security;
-- +goose StatementEnd

-- +goose StatementBegin
alter table holds force row level security;
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation on holds
  using (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- +goose StatementBegin
alter table ledger_entries enable row level security;
-- +goose StatementEnd

-- +goose StatementBegin
alter table ledger_entries force row level security;
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation on ledger_entries
  using (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- +goose StatementBegin
alter table org_balances enable row level security;
-- +goose StatementEnd

-- +goose StatementBegin
alter table org_balances force row level security;
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation on org_balances
  using (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- +goose StatementBegin
alter table org_members enable row level security;
-- +goose StatementEnd

-- +goose StatementBegin
alter table org_members force row level security;
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation on org_members
  using (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- +goose StatementBegin
alter table project_capabilities enable row level security;
-- +goose StatementEnd

-- +goose StatementBegin
alter table project_capabilities force row level security;
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation on project_capabilities
  using (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- +goose StatementBegin
alter table projects enable row level security;
-- +goose StatementEnd

-- +goose StatementBegin
alter table projects force row level security;
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation on projects
  using (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- +goose StatementBegin
alter table proposals enable row level security;
-- +goose StatementEnd

-- +goose StatementBegin
alter table proposals force row level security;
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation on proposals
  using (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- +goose StatementBegin
alter table rank_snapshots enable row level security;
-- +goose StatementEnd

-- +goose StatementBegin
alter table rank_snapshots force row level security;
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation on rank_snapshots
  using (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- +goose StatementBegin
alter table repos enable row level security;
-- +goose StatementEnd

-- +goose StatementBegin
alter table repos force row level security;
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation on repos
  using (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- +goose StatementBegin
alter table secrets enable row level security;
-- +goose StatementEnd

-- +goose StatementBegin
alter table secrets force row level security;
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation on secrets
  using (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- +goose StatementBegin
alter table seo_issues enable row level security;
-- +goose StatementEnd

-- +goose StatementBegin
alter table seo_issues force row level security;
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation on seo_issues
  using (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- +goose StatementBegin
alter table sessions enable row level security;
-- +goose StatementEnd

-- +goose StatementBegin
alter table sessions force row level security;
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation on sessions
  using (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- +goose StatementBegin
alter table sync_jobs enable row level security;
-- +goose StatementEnd

-- +goose StatementBegin
alter table sync_jobs force row level security;
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation on sync_jobs
  using (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- +goose StatementBegin
alter table timeline_events enable row level security;
-- +goose StatementEnd

-- +goose StatementBegin
alter table timeline_events force row level security;
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation on timeline_events
  using (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- +goose StatementBegin
alter table turns enable row level security;
-- +goose StatementEnd

-- +goose StatementBegin
alter table turns force row level security;
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation on turns
  using (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- +goose StatementBegin
alter table usage_events enable row level security;
-- +goose StatementEnd

-- +goose StatementBegin
alter table usage_events force row level security;
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation on usage_events
  using (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- ============================================ deliberately unprotected
--
-- `users` has no org_id and gets no policy. A user is a global identity that
-- may belong to many orgs (§8: "Users may belong to many orgs"), and sign-in
-- must find a user by email BEFORE any org is known -- so an org-scoped policy
-- would make authentication impossible. §6's rule is "every table with
-- `org_id`", and this is the one table where that exclusion is a design
-- decision rather than an oversight.
--
-- The consequence is stated rather than mitigated: a query bug on `users` can
-- read an email address belonging to someone outside the caller's orgs. The
-- application only ever reaches users through `org_members`, and §6 is explicit
-- that RLS "is defence in depth, not the primary control".
--
-- The global catalogue -- templates, template_versions, capabilities,
-- capability_versions, price_books -- is identical for every tenant, so there
-- is nothing to isolate. They are granted SELECT only, which is what stops a
-- request from editing a price book.

