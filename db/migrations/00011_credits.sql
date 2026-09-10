-- SPEC §6, "credits".
--
-- This migration creates `ledger_entries`. SPEC §19.2 puts a review gate on
-- anything touching that table, and db/migrations/README.md repeats it. This is
-- its initial creation rather than a change to it, but the gate applies to the
-- PR regardless — the shape of the ledger is the one thing here that cannot be
-- fixed forward, because every later row depends on it.
--
-- Two properties of the money model are enforced in the DDL rather than in
-- application code, because §16.5 reconciles nightly and a reconciliation that
-- finds drift cannot tell you which write caused it:
--
--   * `numeric(14,4)`, never a float. §16.1 requires a ledger rather than a
--     counter, and routing money through an IEEE-754 double loses exactness
--     silently. packages/schema/common.schema.json types Credits as a decimal
--     STRING for the same reason; docs/open-questions.md Q1 records that §7.2's
--     examples spell it as a JSON number, which cannot also be right.
--   * `ledger_entries.idempotency_key` is `unique` and `not null`. A retried
--     charge that lands twice is a customer-visible billing error, and the
--     database is the only place that can refuse it under concurrency.

-- +goose Up

-- +goose StatementBegin
create table price_books (
  id uuid primary key default gen_random_uuid(),
  version text not null unique,
  effective_from timestamptz not null,
  items jsonb not null,          -- meter -> {unit, credits_per_unit}
  created_at timestamptz not null default now()
);
-- +goose StatementEnd

-- "Which price book was in force at this instant" is the only query, and it is
-- on the rating path for every metered event.
-- +goose StatementBegin
create index price_books_effective_from_idx on price_books (effective_from desc);
-- +goose StatementEnd

-- +goose StatementBegin
create table holds (
  id uuid primary key default gen_random_uuid(),
  org_id uuid not null references orgs(id) on delete cascade,
  amount numeric(14,4) not null check (amount > 0),
  reason text not null,
  ref jsonb not null,
  status text not null check (status in ('active','settled','expired','released')),
  expires_at timestamptz not null,
  created_at timestamptz not null default now(),
  unique (id, org_id)
);
-- +goose StatementEnd

-- The balance calculation sums active holds per org, and the sweeper that
-- expires them scans by deadline. Both are partial: settled holds are the
-- overwhelming majority and neither query looks at them.
-- +goose StatementBegin
create index holds_active_idx on holds (org_id, expires_at)
  where status = 'active';
-- +goose StatementEnd

-- +goose StatementBegin
create table ledger_entries (
  id bigserial primary key,
  org_id uuid not null references orgs(id) on delete cascade,
  amount numeric(14,4) not null,   -- negative = charge, positive = grant/refund
  kind text not null check (kind in (
    'grant','purchase','charge','refund','reversal','expiry','adjustment'
  )),
  meter text,
  price_book_version text,
  ref jsonb not null default '{}',
  idempotency_key text not null unique,
  created_at timestamptz not null default now()
);
-- +goose StatementEnd

-- Declared explicitly in §6: the ledger is read as a per-org reverse-chronological
-- statement (GET /v1/orgs/:org/credits/ledger).
-- +goose StatementBegin
create index ledger_entries_org_id_created_at_idx on ledger_entries (org_id, created_at desc);
-- +goose StatementEnd

-- An amount of zero is not a transaction. Allowing it means the ledger
-- accumulates rows that change nothing and that a reconciliation has to explain.
-- +goose StatementBegin
alter table ledger_entries add constraint ledger_entries_amount_nonzero
  check (amount <> 0);
-- +goose StatementEnd

-- The sign follows from the kind, so a charge recorded as a positive number —
-- which would silently GRANT credits — cannot be written at all. `adjustment`
-- is deliberately unconstrained: it exists for the cases nobody predicted, and
-- a correction that can only go one way is not a correction.
-- +goose StatementBegin
alter table ledger_entries add constraint ledger_entries_sign_matches_kind
  check (
    case kind
      when 'grant'     then amount > 0
      when 'purchase'  then amount > 0
      when 'refund'    then amount > 0
      when 'charge'    then amount < 0
      when 'expiry'    then amount < 0
      else true
    end
  );
-- +goose StatementEnd

-- +goose StatementBegin
create table grants (
  id uuid primary key default gen_random_uuid(),
  org_id uuid not null references orgs(id) on delete cascade,
  amount numeric(14,4) not null check (amount > 0),
  expires_at timestamptz,          -- null = purchased, never expires
  consumed numeric(14,4) not null default 0,
  created_at timestamptz not null default now(),
  -- A grant cannot be consumed beyond its face value; without this, an
  -- over-consumed grant reads as a negative remaining balance that the
  -- materialised org_balances row would quietly absorb.
  constraint grants_consumed_within_amount check (consumed >= 0 and consumed <= amount)
);
-- +goose StatementEnd

-- Grant selection burns soonest-expiring first (§16.2), and never-expiring
-- purchased credits last. `nulls last` matches that order exactly.
-- +goose StatementBegin
create index grants_org_id_expires_at_idx on grants (org_id, expires_at nulls last)
  where consumed < amount;
-- +goose StatementEnd

-- +goose StatementBegin
create table usage_events (
  id bigserial primary key,
  org_id uuid not null references orgs(id) on delete cascade,
  -- Nullable, and composite: a usage event may be org-level with no project
  -- (an edge meter), but if it names a project that project must belong to the
  -- same org. Otherwise one tenant's usage could be rated against another's
  -- balance, which is the one bug in this table that spends real money.
  project_id uuid,
  source text not null,          -- agentd | sandboxd | aigw | worker | edge
  event_key text not null,       -- idempotency key from the emitter
  meter text not null,           -- tokens_in | sandbox_seconds | build | ...
  quantity numeric(20,6) not null,
  occurred_at timestamptz not null,
  rated_at timestamptz,
  ledger_entry_id bigint references ledger_entries(id),
  unique (source, event_key),
  foreign key (project_id, org_id) references projects (id, org_id) on delete set null
);
-- +goose StatementEnd

-- §6 declares `ledger_entry_id bigint` with no reference; the foreign key is
-- added above. A usage event pointing at a ledger entry that does not exist is
-- unreconcilable by definition, which defeats the purpose of recording it.

-- The rating worker's only query: events not yet rated, oldest first. Partial,
-- because rated events are permanent and vastly outnumber unrated ones.
-- +goose StatementBegin
create index usage_events_unrated_idx on usage_events (occurred_at)
  where rated_at is null;
-- +goose StatementEnd

-- +goose StatementBegin
create index usage_events_org_id_occurred_at_idx on usage_events (org_id, occurred_at desc);
-- +goose StatementEnd

-- +goose StatementBegin
-- Materialised and rebuildable from the ledger, exactly as §6 says. It is a
-- cache: §16.5's nightly reconciliation recomputes it, and any disagreement is
-- the signal that something upstream is wrong.
create table org_balances (
  org_id uuid primary key references orgs(id) on delete cascade,
  available numeric(14,4) not null default 0,
  held numeric(14,4) not null default 0,
  build_used_period numeric(14,4) not null default 0,
  runtime_used_period numeric(14,4) not null default 0,
  updated_at timestamptz not null default now(),
  -- Held is a sum of active holds and cannot be negative. Available CAN be:
  -- §16.3 pauses the builder when the meter is exhausted rather than refusing
  -- the write that took it under, so a small overshoot is expected and must be
  -- representable rather than clamped into invisibility.
  constraint org_balances_held_nonnegative check (held >= 0)
);
-- +goose StatementEnd

-- The foreign key §6 leaves off `turns.hold_id`; see 00005.
-- `on delete restrict`: a hold that a turn still points at must be settled or
-- released, never deleted out from under the row that explains what it paid for.
-- +goose StatementBegin
alter table turns
  add constraint turns_hold_id_fkey
  foreign key (hold_id, org_id) references holds (id, org_id) on delete restrict;
-- +goose StatementEnd

-- +goose StatementBegin
create index turns_hold_id_idx on turns (hold_id);
-- +goose StatementEnd
