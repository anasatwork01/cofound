-- SPEC §6, "platform".
--
-- SPEC §8 requires a full audit log for nine actions: sign-in, role change,
-- secret read/write, proposal decision, publish, rollback, domain change,
-- credit purchase, and member removal. Task 0.9 builds the writer and asserts
-- that list is covered; this is the table it writes to.

-- +goose Up

-- +goose StatementBegin
-- `org_id` and `actor_user_id` are both nullable, deliberately. A failed
-- sign-in has no user and no org yet, and it is precisely the event you most
-- want recorded. `actor_kind` is never null: something acted.
create table audit_log (
  id bigserial primary key,
  org_id uuid references orgs(id) on delete set null,
  actor_user_id uuid references users(id) on delete set null,
  actor_kind text not null check (actor_kind in ('user','agent','system','external')),
  action text not null,
  target jsonb not null,
  ip inet,
  user_agent text,
  created_at timestamptz not null default now()
);
-- +goose StatementEnd

-- `on delete set null` rather than cascade on both references: deleting an org
-- or a user must not erase the record of what they did. That is the whole point
-- of an audit log, and a cascade here would make "member removal" — one of §8's
-- nine audited actions — delete its own audit trail.

-- +goose StatementBegin
create index audit_log_org_id_created_at_idx on audit_log (org_id, created_at desc);
-- +goose StatementEnd

-- +goose StatementBegin
create index audit_log_actor_user_id_created_at_idx on audit_log (actor_user_id, created_at desc);
-- +goose StatementEnd

-- "Show me every role change" across orgs, for support and for incident review.
-- +goose StatementBegin
create index audit_log_action_created_at_idx on audit_log (action, created_at desc);
-- +goose StatementEnd
