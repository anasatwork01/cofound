-- SPEC §6, "tenancy". Orgs, users, and the membership row that carries the role.
--
-- No migration in this directory has a rollback section. SPEC §0 rule 6 makes
-- migrations forward-only and additive, and the most reliable way to enforce
-- that is for the rollback simply not to exist: a section that drops a table is
-- one command away from deleting a tenant's history, and no amount of
-- documentation makes that safe. `make db-validate` fails the build if one
-- appears. Local development resets instead, with `make db-reset`.
--
-- (This comment deliberately does not spell the goose rollback marker out.
-- goose parses any line beginning with its annotation prefix, comment or not,
-- so writing it here made the migration itself unparseable -- caught by
-- db-validate, which is a pleasing way to find out the check works.)

-- +goose Up

-- +goose StatementBegin
create table orgs (
  id uuid primary key default gen_random_uuid(),
  name text not null,
  slug text not null unique,
  plan text not null default 'free',
  suspended_at timestamptz,
  created_at timestamptz not null default now()
);
-- +goose StatementEnd

-- +goose StatementBegin
create table users (
  id uuid primary key default gen_random_uuid(),
  email text not null unique,
  name text,
  created_at timestamptz not null default now()
);
-- +goose StatementEnd

-- +goose StatementBegin
-- The role vocabulary is SPEC §8's, and it is repeated in
-- packages/schema/common.schema.json as `Role`. tests/integration asserts the
-- two agree, so adding a role in one place fails the build rather than
-- producing a check constraint the API can violate.
create table org_members (
  org_id uuid not null references orgs(id) on delete cascade,
  user_id uuid not null references users(id) on delete cascade,
  role text not null check (role in ('owner','admin','editor','viewer')),
  created_at timestamptz not null default now(),
  primary key (org_id, user_id)
);
-- +goose StatementEnd

-- Postgres indexes the primary key (org_id, user_id), which serves "who is in
-- this org". "Which orgs am I in" — the org switcher in SPEC §8, on every
-- console page load — needs the other direction.
-- +goose StatementBegin
create index org_members_user_id_idx on org_members (user_id);
-- +goose StatementEnd
