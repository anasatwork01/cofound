-- SPEC §6, "capabilities".
--
-- `capabilities` and `capability_versions` are a GLOBAL catalogue, not tenant
-- data: every org sees the same list, and neither carries an org_id or gets a
-- row-level security policy. `project_capabilities` and `app_databases` are
-- per-tenant and do; see 00003.

-- +goose Up

-- +goose StatementBegin
-- `credential_owner` is the load-bearing column of the whole capability system
-- (SPEC §13): 'runtime' means the generated app holds the credential, 'builder'
-- means the control plane holds it and the sandbox never sees it. SPEC §17's
-- rule — the sandbox never holds a credential that can spend money or reach
-- another tenant — is enforced by reading this.
create table capabilities (
  id text primary key,                    -- 'payments'
  display_name text not null,
  summary text not null,
  credential_owner text not null
    check (credential_owner in ('none','builder','runtime')),
  status text not null default 'available'
    check (status in ('available','beta','coming_soon'))
);
-- +goose StatementEnd

-- +goose StatementBegin
create table capability_versions (
  id uuid primary key default gen_random_uuid(),
  capability_id text not null references capabilities(id),
  version text not null,
  manifest jsonb not null,
  framework_range text not null,
  unique (capability_id, version)
);
-- +goose StatementEnd

-- +goose StatementBegin
create table project_capabilities (
  project_id uuid not null,
  org_id uuid not null,
  capability_id text not null references capabilities(id),
  version_id uuid not null references capability_versions(id),
  state text not null check (state in (
    'provisioning','needs_action','installed','failed','removing'
  )),
  external_refs jsonb not null default '{}',  -- stripe acct id, slack team id
  installed_at timestamptz,
  primary key (project_id, capability_id),
  foreign key (project_id, org_id) references projects (id, org_id) on delete cascade
);
-- +goose StatementEnd

-- +goose StatementBegin
create index project_capabilities_version_id_idx on project_capabilities (version_id);
-- +goose StatementEnd

-- +goose StatementBegin
create index project_capabilities_org_id_idx on project_capabilities (org_id);
-- +goose StatementEnd

-- +goose StatementBegin
create table app_databases (
  project_id uuid primary key,
  org_id uuid not null,
  provider text not null default 'neon',
  external_project_id text not null,
  production_branch_ref text not null,
  migrations_applied int not null default 0,
  foreign key (project_id, org_id) references projects (id, org_id) on delete cascade
);
-- +goose StatementEnd

-- +goose StatementBegin
create index app_databases_org_id_idx on app_databases (org_id);
-- +goose StatementEnd
