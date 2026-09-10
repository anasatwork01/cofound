-- SPEC §6, "agent sessions".
--
-- Note for anyone arriving from SPEC §8: this `sessions` table is AGENT
-- sessions — a project, a branch, a sandbox. The rotating console sign-in
-- sessions §8 requires are a different thing entirely and are added by task
-- 0.7 under a different name. docs/open-questions.md Q3 records the collision.
--
-- See 00003 for why `org_id` is denormalised onto these tables.

-- +goose Up

-- +goose StatementBegin
create table sessions (
  id uuid primary key default gen_random_uuid(),
  project_id uuid not null,
  branch_id uuid not null,
  org_id uuid not null,
  user_id uuid not null references users(id),
  sandbox_id text,
  state text not null check (state in ('starting','live','idle','stopped','failed')),
  started_at timestamptz not null default now(),
  ended_at timestamptz,
  unique (id, org_id),
  foreign key (project_id, org_id) references projects (id, org_id) on delete cascade,
  -- Composite here too, so a session cannot point at a branch of another
  -- tenant's project even though both columns look individually valid.
  foreign key (branch_id, org_id) references branches (id, org_id)
);
-- +goose StatementEnd

-- +goose StatementBegin
create index sessions_project_id_started_at_idx on sessions (project_id, started_at desc);
-- +goose StatementEnd

-- +goose StatementBegin
create index sessions_branch_id_idx on sessions (branch_id);
-- +goose StatementEnd

-- +goose StatementBegin
create index sessions_user_id_idx on sessions (user_id);
-- +goose StatementEnd

-- At most one live session per branch. SPEC §12 gives a branch a single write
-- lease, and two 'live' sessions on one branch would be two agents writing the
-- same worktree — a partial unique index enforces that in the database rather
-- than trusting every caller to check first.
-- +goose StatementBegin
create unique index sessions_one_live_per_branch_idx on sessions (branch_id)
  where state in ('starting','live');
-- +goose StatementEnd

-- +goose StatementBegin
-- `hold_id` is declared in §6 as a bare uuid. Migration 00011 adds the foreign
-- key once `holds` exists — a credit hold that vanishes while a turn still
-- points at it is precisely the drift §16.5's reconciliation is meant to catch,
-- and it is cheaper to make it impossible.
create table turns (
  id uuid primary key default gen_random_uuid(),
  session_id uuid not null,
  org_id uuid not null,
  seq int not null,
  actor text not null check (actor in ('user','agent')),
  prompt text,
  status text not null check (status in ('running','done','failed','aborted','budget_exceeded')),
  checkpoint_sha text,
  hold_id uuid,
  credits_charged numeric(14,4),
  tokens_in bigint,
  tokens_out bigint,
  tokens_cache_read bigint,
  tokens_cache_write bigint,
  sandbox_seconds numeric(10,2),
  started_at timestamptz not null default now(),
  ended_at timestamptz,
  unique (session_id, seq),
  foreign key (session_id, org_id) references sessions (id, org_id) on delete cascade
);
-- +goose StatementEnd

-- At most one running turn per session. services/api/internal/apierrs already
-- defines `turn_running` and `no_turn_running` as domain errors, which means the
-- API is expected to refuse a second concurrent turn; this is the same rule
-- where it cannot be raced.
-- +goose StatementBegin
create unique index turns_one_running_per_session_idx on turns (session_id)
  where status = 'running';
-- +goose StatementEnd

-- +goose StatementBegin
create table checkpoints (
  id bigserial primary key,
  session_id uuid not null,
  org_id uuid not null,
  turn_seq int not null,
  sha text not null,
  parent_sha text,
  files_touched text[],
  created_at timestamptz not null default now(),
  foreign key (session_id, org_id) references sessions (id, org_id) on delete cascade
);
-- +goose StatementEnd

-- +goose StatementBegin
create index checkpoints_session_id_turn_seq_idx on checkpoints (session_id, turn_seq);
-- +goose StatementEnd

-- +goose StatementBegin
create index sessions_org_id_idx on sessions (org_id);
-- +goose StatementEnd

-- +goose StatementBegin
create index turns_org_id_idx on turns (org_id);
-- +goose StatementEnd

-- +goose StatementBegin
create index checkpoints_org_id_idx on checkpoints (org_id);
-- +goose StatementEnd
