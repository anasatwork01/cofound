-- SPEC §6, "secrets + connectors".
--
-- Both tables hold ciphertext and a key id, never a plaintext credential —
-- SPEC §17.2. `dek_id` names the data encryption key so a rotation can
-- re-wrap without re-encrypting, and so a compromised key has a blast radius
-- that can be enumerated.
--
-- §6 gives `connectors` an `org_id` but not `secrets`, so its literal rule
-- ("enable RLS on every table with org_id") would leave the most sensitive
-- table in the schema with no policy at all. `secrets` therefore carries a
-- denormalised `org_id` with a composite foreign key onto its project, and gets
-- the same one-column policy as everything else. See 00003.

-- +goose Up

-- +goose StatementBegin
create table secrets (
  id uuid primary key default gen_random_uuid(),
  project_id uuid not null,
  org_id uuid not null,
  env text not null check (env in ('development','production')),
  key text not null,
  ciphertext bytea not null,
  dek_id text not null,
  rotated_at timestamptz,
  created_at timestamptz not null default now(),
  unique (project_id, env, key),
  foreign key (project_id, org_id) references projects (id, org_id) on delete cascade
);
-- +goose StatementEnd

-- +goose StatementBegin
create table connectors (
  id uuid primary key default gen_random_uuid(),
  org_id uuid not null references orgs(id) on delete cascade,
  provider text not null,            -- google_ads | meta | gsc | slack | ...
  external_account_id text,
  scopes text[] not null,
  refresh_ciphertext bytea not null,
  dek_id text not null,
  status text not null default 'active',
  created_at timestamptz not null default now(),
  unique (org_id, provider, external_account_id),
  unique (id, org_id)
);
-- +goose StatementEnd

-- +goose StatementBegin
create index secrets_org_id_idx on secrets (org_id);
-- +goose StatementEnd

-- +goose StatementBegin
create index connectors_org_id_idx on connectors (org_id);
-- +goose StatementEnd
