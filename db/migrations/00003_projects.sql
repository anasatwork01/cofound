-- SPEC §6, "projects".
--
-- ## Why every tenant-scoped table below carries `org_id`
--
-- §6 puts `org_id` on ten tables and says "enable row level security on every
-- table with `org_id`". Taken literally that leaves `secrets`, `deployments`,
-- `domains`, `proposals` and eleven others with no policy at all, because they
-- reach the org through `projects` instead. `secrets` is the most sensitive
-- table in the schema; a defence-in-depth control that skips it is not one.
--
-- The alternative to denormalising is a policy that reaches through the parent.
-- Measured on this schema's shape (100k rows, 1000 projects, 200 orgs, Postgres
-- 18.6), reading one org's rows:
--
--     org_id = current_org()                        0.36 ms   Bitmap Index Scan
--     project_id in (select ... from projects)      2.71 ms   scan + filter, 99500 rows discarded
--     exists (select 1 from projects ...)          77.24 ms   Seq Scan + JIT
--
-- The subquery forms do not use an index to FIND the rows; they scan the table
-- and discard what does not match, so their cost is proportional to the whole
-- table rather than to the tenant. At 100k rows that is 2.7 ms. At 10M it is
-- not a policy, it is an outage.
--
-- Denormalising would normally trade that speed for an integrity hole — a row
-- whose `org_id` disagrees with its parent's is invisible to its real owner and
-- visible to someone else. That hole is closed by giving each child a COMPOSITE
-- foreign key onto its parent's `(id, org_id)`, which is why `projects` carries
-- the otherwise-redundant `unique (id, org_id)` below. Verified: a mismatched
-- insert is refused by the database, and so is moving a project between orgs
-- while children still reference it.
--
-- So `org_id` is added under §6's own "add columns as needed", and the result
-- satisfies §6's RLS rule on every tenant table rather than on ten of them.
-- docs/verified.md records the measurements.

-- +goose Up

-- +goose StatementBegin
create table projects (
  id uuid primary key default gen_random_uuid(),
  org_id uuid not null references orgs(id) on delete cascade,
  name text not null,
  slug text not null,
  template_version_id uuid references template_versions(id),
  default_branch text not null default 'main',
  git_authority text not null default 'internal'
    check (git_authority in ('internal','github')),
  archived_at timestamptz,
  created_at timestamptz not null default now(),
  unique (org_id, slug),
  -- The composite target every child's (project_id, org_id) foreign key needs.
  unique (id, org_id)
);
-- +goose StatementEnd

-- +goose StatementBegin
create index projects_template_version_id_idx on projects (template_version_id);
-- +goose StatementEnd

-- +goose StatementBegin
-- `preview_deployment_id` is declared in §6 as a bare uuid with no reference,
-- even though every other id column there is constrained. Migration 00007 adds
-- the foreign key once `deployments` exists: enforcing a reference the document
-- clearly intends is not a change of semantics, and an unconstrained pointer to
-- a deleted deployment is exactly the kind of dangling row that turns into a
-- console rendering half a preview.
create table branches (
  id uuid primary key default gen_random_uuid(),
  project_id uuid not null,
  org_id uuid not null,
  name text not null,
  head_sha text,
  app_db_branch_ref text,
  preview_deployment_id uuid,
  created_at timestamptz not null default now(),
  unique (project_id, name),
  -- Children of branches (sessions, deployments) need this composite target.
  unique (id, org_id),
  foreign key (project_id, org_id) references projects (id, org_id) on delete cascade
);
-- +goose StatementEnd

-- +goose StatementBegin
create index branches_org_id_idx on branches (org_id);
-- +goose StatementEnd
