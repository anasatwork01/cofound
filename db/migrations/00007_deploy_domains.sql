-- SPEC §6, "deploy + domains".
--
-- See 00003 for why `org_id` is denormalised onto these tables.

-- +goose Up

-- +goose StatementBegin
create table deployments (
  id uuid primary key default gen_random_uuid(),
  project_id uuid not null,
  branch_id uuid not null,
  org_id uuid not null,
  version int not null,
  sha text not null,
  artifact_key text not null,
  worker_name text,
  env text not null check (env in ('preview','production')),
  status text not null check (status in ('building','ready','failed','superseded')),
  is_live boolean not null default false,
  built_at timestamptz,
  created_at timestamptz not null default now(),
  unique (project_id, version),
  unique (id, org_id),
  foreign key (project_id, org_id) references projects (id, org_id) on delete cascade,
  foreign key (branch_id, org_id) references branches (id, org_id)
);
-- +goose StatementEnd

-- Exactly one live production deployment per project. SPEC §14's rollback
-- "repoints the edge, no rebuild", which means two rows briefly want to be live
-- at once; a partial unique index makes the swap a single atomic UPDATE instead
-- of a window in which a project has two production versions or none.
-- +goose StatementBegin
create unique index deployments_one_live_production_idx on deployments (project_id)
  where is_live and env = 'production';
-- +goose StatementEnd

-- +goose StatementBegin
create index deployments_branch_id_idx on deployments (branch_id);
-- +goose StatementEnd

-- The foreign key §6 leaves off `branches.preview_deployment_id`; see 00003.
-- `on delete set null` rather than cascade: losing a preview deployment must
-- not delete the branch it was built from.
-- +goose StatementBegin
alter table branches
  add constraint branches_preview_deployment_id_fkey
  foreign key (preview_deployment_id, org_id) references deployments (id, org_id)
  on delete set null;
-- +goose StatementEnd

-- +goose StatementBegin
create index branches_preview_deployment_id_idx on branches (preview_deployment_id);
-- +goose StatementEnd

-- +goose StatementBegin
create table domains (
  id uuid primary key default gen_random_uuid(),
  project_id uuid not null,
  org_id uuid not null,
  hostname text not null unique,
  kind text not null check (kind in ('apex','www','subdomain')),
  source text not null check (source in ('registered','connected')),
  cf_custom_hostname_id text,
  dns_status text not null default 'pending',
  tls_status text not null default 'pending',
  redirect_to text,
  verified_at timestamptz,
  created_at timestamptz not null default now(),
  foreign key (project_id, org_id) references projects (id, org_id) on delete cascade
);
-- +goose StatementEnd

-- +goose StatementBegin
create index domains_project_id_idx on domains (project_id);
-- +goose StatementEnd

-- +goose StatementBegin
create index deployments_org_id_idx on deployments (org_id);
-- +goose StatementEnd

-- +goose StatementBegin
create index domains_org_id_idx on domains (org_id);
-- +goose StatementEnd
