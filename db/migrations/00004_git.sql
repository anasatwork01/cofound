-- SPEC §6, "git + github". See 00003 for why `org_id` appears on child tables.

-- +goose Up

-- +goose StatementBegin
create table github_installations (
  id uuid primary key default gen_random_uuid(),
  org_id uuid not null references orgs(id) on delete cascade,
  installation_id bigint not null unique,
  account_login text not null,
  suspended_at timestamptz,
  created_at timestamptz not null default now(),
  unique (id, org_id)
);
-- +goose StatementEnd

-- +goose StatementBegin
create index github_installations_org_id_idx on github_installations (org_id);
-- +goose StatementEnd

-- +goose StatementBegin
-- One repo per project: §6 marks project_id `unique`, which makes the internal
-- git authority and an optional GitHub mirror two columns of one row rather
-- than two rows to keep in step.
--
-- `installation_id` is constrained by org as well, so a project cannot be
-- attached to another tenant's GitHub App installation — which would let one
-- org's pushes reach another org's repositories.
create table repos (
  id uuid primary key default gen_random_uuid(),
  project_id uuid not null unique,
  org_id uuid not null,
  internal_path text not null,
  provider text check (provider in ('github')),
  installation_id uuid,
  owner text,
  name text,
  last_synced_sha text,
  pending_push_count int not null default 0,
  created_at timestamptz not null default now(),
  unique (id, org_id),
  foreign key (project_id, org_id) references projects (id, org_id) on delete cascade,
  foreign key (installation_id, org_id)
    references github_installations (id, org_id) on delete set null
);
-- +goose StatementEnd

-- +goose StatementBegin
create index repos_org_id_idx on repos (org_id);
-- +goose StatementEnd

-- +goose StatementBegin
create index repos_installation_id_idx on repos (installation_id);
-- +goose StatementEnd

-- +goose StatementBegin
create table sync_jobs (
  id bigserial primary key,
  repo_id uuid not null,
  org_id uuid not null,
  direction text not null check (direction in ('outbound','inbound')),
  status text not null check (status in ('queued','running','failed','done')),
  attempts int not null default 0,
  last_error text,
  created_at timestamptz not null default now(),
  foreign key (repo_id, org_id) references repos (id, org_id) on delete cascade
);
-- +goose StatementEnd

-- The queue's only hot query is "what is still to do for this repo", so the
-- index is partial: `done` rows accumulate forever and are never scanned by the
-- worker, and keeping them out of the index keeps it the size of the backlog
-- rather than the size of history.
-- +goose StatementBegin
create index sync_jobs_pending_idx on sync_jobs (repo_id, created_at)
  where status in ('queued','running','failed');
-- +goose StatementEnd

-- +goose StatementBegin
create index sync_jobs_org_id_idx on sync_jobs (org_id);
-- +goose StatementEnd
