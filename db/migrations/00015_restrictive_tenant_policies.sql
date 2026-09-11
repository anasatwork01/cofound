-- A RESTRICTIVE tenant policy beside each permissive one.
--
-- Migration 00013 gives every tenant table one PERMISSIVE policy, which is what
-- actually filters rows today. This adds a second, RESTRICTIVE policy with the
-- same expression, and the redundancy is the entire point.
--
-- ## What this defends against
--
-- Postgres combines policies by mode. Permissive policies are OR-ed together;
-- restrictive policies are AND-ed with the result. So with only permissive
-- policies, ANY policy added later widens access:
--
--     create policy support_read on secrets using (true);   -- for a support tool
--
-- That one line, added in good faith by someone building an internal admin
-- view, makes every tenant's secrets readable by every tenant -- because it is
-- OR-ed with the tenant check rather than AND-ed. Nothing fails, no test
-- notices unless it happens to cover that table, and the isolation is simply
-- gone.
--
-- A restrictive policy cannot be widened that way. Whatever permissive policies
-- exist, the org comparison below is AND-ed with all of them, so the worst a
-- careless addition can do is grant access the tenant check then removes again.
--
-- This came out of the row-level security verification for task 0.6, after the
-- migration was already written. It is additive, so it lands as its own
-- migration rather than by editing 00013 -- which is what forward-only means in
-- practice.
--
-- ## Why not make 00013's policy restrictive instead
--
-- A table with only restrictive policies denies everything: Postgres needs at
-- least one permissive policy to admit a row before the restrictive ones narrow
-- it. So both are required, and having them carry the same expression is what
-- makes the pair safe in either direction.

-- +goose Up

-- +goose StatementBegin
create policy tenant_isolation_guard on ad_accounts
  as restrictive
  using (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation_guard on app_databases
  as restrictive
  using (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation_guard on audit_log
  as restrictive
  using (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation_guard on branches
  as restrictive
  using (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation_guard on campaigns
  as restrictive
  using (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation_guard on checkpoints
  as restrictive
  using (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation_guard on connectors
  as restrictive
  using (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation_guard on deployments
  as restrictive
  using (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation_guard on domains
  as restrictive
  using (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation_guard on github_installations
  as restrictive
  using (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation_guard on grants
  as restrictive
  using (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation_guard on holds
  as restrictive
  using (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation_guard on ledger_entries
  as restrictive
  using (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation_guard on org_balances
  as restrictive
  using (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation_guard on org_members
  as restrictive
  using (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation_guard on project_capabilities
  as restrictive
  using (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation_guard on projects
  as restrictive
  using (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation_guard on proposals
  as restrictive
  using (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation_guard on rank_snapshots
  as restrictive
  using (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation_guard on repos
  as restrictive
  using (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation_guard on secrets
  as restrictive
  using (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation_guard on seo_issues
  as restrictive
  using (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation_guard on sessions
  as restrictive
  using (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation_guard on sync_jobs
  as restrictive
  using (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation_guard on timeline_events
  as restrictive
  using (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation_guard on turns
  as restrictive
  using (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation_guard on usage_events
  as restrictive
  using (org_id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation_guard on orgs
  as restrictive
  using (id = nullif(current_setting('app.org_id', true), '')::uuid);
-- +goose StatementEnd
