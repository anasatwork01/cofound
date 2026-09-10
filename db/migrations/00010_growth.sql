-- SPEC §6, "growth".
--
-- See 00003 for why `org_id` is denormalised onto these tables.

-- +goose Up

-- +goose StatementBegin
-- `connector_id` is constrained by org as well: a project must not be able to
-- spend through another tenant's ad connector, which is a money-moving
-- cross-tenant path rather than merely a data one.
create table ad_accounts (
  id uuid primary key default gen_random_uuid(),
  project_id uuid not null,
  org_id uuid not null,
  channel text not null check (channel in ('google','meta')),
  external_id text not null,
  connector_id uuid not null,
  created_at timestamptz not null default now(),
  unique (channel, external_id),
  unique (id, org_id),
  foreign key (project_id, org_id) references projects (id, org_id) on delete cascade,
  foreign key (connector_id, org_id) references connectors (id, org_id)
);
-- +goose StatementEnd

-- +goose StatementBegin
create index ad_accounts_project_id_idx on ad_accounts (project_id);
-- +goose StatementEnd

-- +goose StatementBegin
create index ad_accounts_connector_id_idx on ad_accounts (connector_id);
-- +goose StatementEnd

-- +goose StatementBegin
create table campaigns (
  id uuid primary key default gen_random_uuid(),
  ad_account_id uuid not null,
  org_id uuid not null,
  external_id text,
  name text not null,
  state text not null check (state in ('draft','pending_approval','running','paused','ended')),
  daily_budget_cents bigint,
  mirror jsonb not null default '{}',
  created_at timestamptz not null default now(),
  foreign key (ad_account_id, org_id) references ad_accounts (id, org_id) on delete cascade
);
-- +goose StatementEnd

-- +goose StatementBegin
create index campaigns_ad_account_id_idx on campaigns (ad_account_id);
-- +goose StatementEnd

-- +goose StatementBegin
-- Every proposal expires (§15): an approval queue with no expiry becomes a list
-- of decisions nobody can safely make months later, because the projection they
-- were based on has gone stale.
create table proposals (
  id uuid primary key default gen_random_uuid(),
  project_id uuid not null,
  org_id uuid not null,
  kind text not null,               -- campaign_create | budget_change | seo_fix | destructive_migration
  target_ref jsonb not null,
  rationale text not null,
  diff jsonb not null,
  projected jsonb,
  status text not null check (status in ('pending','approved','declined','expired','executed','failed')),
  decided_by uuid references users(id),
  decided_at timestamptz,
  expires_at timestamptz not null,
  created_at timestamptz not null default now(),
  foreign key (project_id, org_id) references projects (id, org_id) on delete cascade
);
-- +goose StatementEnd

-- GET /v1/proposals?status=pending (§7.1) is the console's approval queue, and
-- the sweeper that marks proposals expired reads the same shape.
-- +goose StatementBegin
create index proposals_pending_idx on proposals (expires_at)
  where status = 'pending';
-- +goose StatementEnd

-- +goose StatementBegin
create index proposals_project_id_created_at_idx on proposals (project_id, created_at desc);
-- +goose StatementEnd

-- +goose StatementBegin
create table seo_issues (
  id bigserial primary key,
  project_id uuid not null,
  org_id uuid not null,
  audit_id uuid not null,
  code text not null,
  pages text[] not null,
  impact int not null check (impact between 0 and 100),
  agent_fixable boolean not null,
  status text not null default 'open'
    check (status in ('open','queued','fixed','dismissed','needs_user')),
  created_at timestamptz not null default now(),
  foreign key (project_id, org_id) references projects (id, org_id) on delete cascade
);
-- +goose StatementEnd

-- §6 annotates impact as "0..100" in a comment. Stating it as a constraint
-- instead means a scoring bug is caught at the write rather than rendered as a
-- bar chart running off the side of the page.
-- +goose StatementBegin
create index seo_issues_project_id_impact_idx on seo_issues (project_id, impact desc)
  where status = 'open';
-- +goose StatementEnd

-- +goose StatementBegin
-- §6 declares `project_id uuid not null` here with no reference, while every
-- other project_id in the document has one. The omission looks like an
-- oversight rather than a decision — nothing in §15 suggests rank history
-- should outlive its project — so the constraint is added.
create table rank_snapshots (
  project_id uuid not null,
  org_id uuid not null,
  query text not null,
  position int,
  clicks int,
  impressions int,
  captured_on date not null,
  primary key (project_id, query, captured_on),
  foreign key (project_id, org_id) references projects (id, org_id) on delete cascade
);
-- +goose StatementEnd

-- +goose StatementBegin
create index ad_accounts_org_id_idx on ad_accounts (org_id);
-- +goose StatementEnd

-- +goose StatementBegin
create index campaigns_org_id_idx on campaigns (org_id);
-- +goose StatementEnd

-- +goose StatementBegin
create index proposals_org_id_idx on proposals (org_id);
-- +goose StatementEnd

-- +goose StatementBegin
create index seo_issues_org_id_idx on seo_issues (org_id);
-- +goose StatementEnd

-- +goose StatementBegin
create index rank_snapshots_org_id_idx on rank_snapshots (org_id);
-- +goose StatementEnd
