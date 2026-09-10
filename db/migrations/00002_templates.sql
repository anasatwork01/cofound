-- Templates. NOT from SPEC §6 — see docs/open-questions.md Q2.
--
-- §6 declares `projects.template_version_id references template_versions(id)`
-- and §7.1's POST /v1/projects takes a `template_version_id`, but §6 never
-- defines the table. Without it, migration 00003 cannot create `projects` at
-- all, so this is not an enhancement.
--
-- The shape is taken from packages/schema/api.openapi.yaml, which models both
-- `Template` and `TemplateVersion` and was itself derived from §7. §6 opens
-- with "Not exhaustive — add columns as needed", which licenses the addition;
-- Q2 records it so that someone diffing a fresh §6 against this directory
-- knows why there is a table here that is not there.

-- +goose Up

-- +goose StatementBegin
create table templates (
  id text primary key,                    -- 'saas-starter', matching capabilities' style
  display_name text not null,
  summary text not null,
  framework text not null,
  status text not null default 'available'
    check (status in ('available','beta','deprecated')),
  created_at timestamptz not null default now()
);
-- +goose StatementEnd

-- +goose StatementBegin
create table template_versions (
  id uuid primary key default gen_random_uuid(),
  template_id text not null references templates(id),
  version text not null,
  framework_version text not null,
  -- The commit in the template's own repository that this version pins.
  source_sha text,
  released_at timestamptz not null default now(),
  unique (template_id, version)
);
-- +goose StatementEnd
