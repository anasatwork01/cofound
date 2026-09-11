-- Idempotency keys. SPEC §7.1: "Every mutating endpoint accepts
-- `Idempotency-Key`." §17.2 is why it matters: a client that times out and
-- retries must not create two projects or spend twice.
--
-- Not in §6, and implied by §7.1 in the same way `invites` is by the invite
-- endpoint. Same "Not exhaustive" licence.
--
-- ## The three states, and why the middle one exists
--
-- A key is claimed before the handler runs and completed after. So a row is in
-- one of three states, and each needs a different answer:
--
--   claimed, not completed   a request with this key is IN FLIGHT. A second one
--                            gets 409 rather than being allowed to run
--                            concurrently — which is the whole point, since two
--                            concurrent creates is exactly what the header
--                            exists to prevent.
--   completed                replay the stored response verbatim.
--   absent                   first time; run it.
--
-- Without the in-flight state, a client that retries after a timeout while the
-- original is still running gets a second execution — the failure mode the
-- header is supposed to close.
--
-- ## Why the request is fingerprinted
--
-- Reusing one key for a DIFFERENT request is a client bug, and replaying the
-- first response would hide it behind a plausible answer. `request_hash` makes
-- it a 422 instead.

-- +goose Up

-- +goose StatementBegin
create table idempotency_keys (
  org_id uuid not null references orgs(id) on delete cascade,
  -- The caller's key, verbatim. Bounded by the middleware before it gets here.
  key text not null,

  -- Method and path so that the same key on a different endpoint is a
  -- different operation rather than a collision.
  method text not null,
  path text not null,
  -- sha256 of the request body. The body itself is NOT stored: it can contain
  -- a prompt, and §17.3 treats user content as something that does not sit in
  -- an operational table indefinitely.
  request_hash bytea not null,

  -- Filled in on completion. Null while in flight.
  status int,
  response_body bytea,
  response_headers jsonb,

  created_at timestamptz not null default now(),
  completed_at timestamptz,
  -- Reclaim deadline. A process that dies mid-request would otherwise leave the
  -- key claimed forever, and the client could never retry.
  expires_at timestamptz not null,

  primary key (org_id, key),

  -- A completed row has all three, an in-flight row has none. Half a response
  -- is not a state this table should be able to represent.
  constraint idempotency_complete_together check (
    (completed_at is null and status is null and response_body is null)
    or (completed_at is not null and status is not null and response_body is not null)
  )
);
-- +goose StatementEnd

-- The sweeper reads by deadline.
-- +goose StatementBegin
create index idempotency_keys_expires_at_idx on idempotency_keys (expires_at);
-- +goose StatementEnd

-- +goose StatementBegin
alter table idempotency_keys enable row level security;
-- +goose StatementEnd

-- +goose StatementBegin
alter table idempotency_keys force row level security;
-- +goose StatementEnd

-- Scoped like every other tenant table. It matters here beyond tidiness: a key
-- is chosen by the client, so without the org in the primary key AND in the
-- policy, one tenant could guess another's key and be handed their response.
-- +goose StatementBegin
create policy tenant_isolation on idempotency_keys
  using (org_id = halyard_current_org_id());
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation_guard on idempotency_keys
  as restrictive
  using (org_id = halyard_current_org_id());
-- +goose StatementEnd

-- +goose StatementBegin
grant select, insert, update, delete on idempotency_keys to halyard_app;
-- +goose StatementEnd
