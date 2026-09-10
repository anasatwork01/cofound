-- SPEC §6, "timeline (renders the history UI)".
--
-- See 00003 for why `org_id` is denormalised onto this table.

-- +goose Up

-- +goose StatementBegin
-- `actor_kind` repeats packages/schema/common.schema.json's `ActorKind`, and
-- tests/integration asserts the two agree.
create table timeline_events (
  id bigserial primary key,
  project_id uuid not null,
  org_id uuid not null,
  kind text not null check (kind in (
    'agent_session','human_edit','deploy','rollback','restore',
    'external_push','template_update','capability_install','capability_remove'
  )),
  actor_user_id uuid references users(id),
  actor_kind text not null check (actor_kind in ('user','agent','system','external')),
  sha text,
  summary text not null,
  detail jsonb not null default '{}',
  credits numeric(14,4),
  created_at timestamptz not null default now(),
  foreign key (project_id, org_id) references projects (id, org_id) on delete cascade
);
-- +goose StatementEnd

-- Declared explicitly in §6. It is the only access path the history UI uses.
-- +goose StatementBegin
create index timeline_events_project_id_created_at_idx
  on timeline_events (project_id, created_at desc);
-- +goose StatementEnd

-- +goose StatementBegin
create index timeline_events_org_id_idx on timeline_events (org_id);
-- +goose StatementEnd
