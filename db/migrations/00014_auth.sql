-- Console authentication. NOT from SPEC §6 — see docs/open-questions.md Q3.
--
-- §6's `sessions` table is AGENT sessions: a project, a branch, a sandbox. §8
-- separately requires console sign-in with "httpOnly, `SameSite=Lax`, rotating
-- cookies", which needs durable server-side session state and, for magic links,
-- single-use tokens. Neither exists in §6, so these are added under its "Not
-- exhaustive" licence and named to avoid the collision.
--
-- ## No row-level security on any table here, deliberately
--
-- Every other tenant table gets a policy keyed on `app.org_id`. These three
-- cannot: sign-in happens BEFORE any org is known, and a user may belong to
-- many orgs (§8). A policy would make authentication impossible rather than
-- safer. This is the same exclusion `users` already has, for the same reason,
-- and it is why `packages/db.Pool.Unscoped` is named to be conspicuous — these
-- tables are only reachable through it.
--
-- The compensating controls are in the columns rather than in a policy:
--
--   * Only token HASHES are stored, never a token. A dump of these tables
--     yields no usable session and no usable magic link. This is the whole
--     reason `token_hash` is `bytea` and unique rather than the token itself
--     being the primary key.
--   * Every row has an expiry the database enforces the shape of, so a bug
--     that forgets to check one still cannot mint something eternal.

-- +goose Up

-- +goose StatementBegin
-- A magic link. Single-use, short-lived, and never joined to `users` — the
-- email is stored as text because the point of the flow is that the user may
-- not exist yet, and creating a user row before the address is proven would let
-- anyone squat any email.
create table login_tokens (
  id uuid primary key default gen_random_uuid(),
  email text not null,
  -- sha256 of the token. Unique so a hash collision or a replay of a captured
  -- hash cannot create a second live token for the same secret.
  token_hash bytea not null unique,
  expires_at timestamptz not null,
  consumed_at timestamptz,
  -- Recorded for the audit log and for throttling. inet, so a /64 of IPv6
  -- addresses can be reasoned about rather than compared as strings.
  requested_ip inet,
  requested_user_agent text,
  created_at timestamptz not null default now(),
  -- A token that never expires is a permanent credential sitting in an inbox.
  constraint login_tokens_expires_after_creation check (expires_at > created_at)
);
-- +goose StatementEnd

-- The verify path looks a token up by hash, which the unique index serves. The
-- sweeper that deletes spent and expired tokens reads by deadline, and the
-- throttle counts recent requests per email.
-- +goose StatementBegin
create index login_tokens_expires_at_idx on login_tokens (expires_at);
-- +goose StatementEnd

-- +goose StatementBegin
create index login_tokens_email_created_at_idx on login_tokens (email, created_at desc);
-- +goose StatementEnd

-- +goose StatementBegin
-- A federated identity. `subject` is the provider's stable id, NOT the email:
-- an email can be reassigned inside a Google Workspace, and keying on it would
-- hand the new holder the old holder's account.
create table oauth_identities (
  provider text not null check (provider in ('google')),
  subject text not null,
  user_id uuid not null references users(id) on delete cascade,
  -- The email as the provider last asserted it, for display and for support.
  -- Never used to look the identity up.
  email text not null,
  created_at timestamptz not null default now(),
  last_login_at timestamptz,
  primary key (provider, subject)
);
-- +goose StatementEnd

-- +goose StatementBegin
create index oauth_identities_user_id_idx on oauth_identities (user_id);
-- +goose StatementEnd

-- +goose StatementBegin
-- A console session. §8: httpOnly, SameSite=Lax, rotating.
--
-- Two expiries, and the distinction is the point. `expires_at` is the IDLE
-- deadline and moves forward as the session is used. `absolute_expires_at` is
-- set once at sign-in and is never extended, so a session that is used every
-- day still ends — otherwise "rotating" would mean "immortal, with a new name
-- each time", which is the opposite of what rotation is for.
create table user_sessions (
  id uuid primary key default gen_random_uuid(),
  user_id uuid not null references users(id) on delete cascade,
  token_hash bytea not null unique,

  issued_at timestamptz not null default now(),
  last_seen_at timestamptz not null default now(),
  expires_at timestamptz not null,
  absolute_expires_at timestamptz not null,

  -- Set when this session replaced another. Keeps the chain auditable: a stolen
  -- cookie that is rotated by the attacker shows up as a rotation from a
  -- session the real user was still using.
  rotated_from uuid references user_sessions(id) on delete set null,
  -- When this row was itself superseded. A rotated row stays valid for a short
  -- grace window (see internal/auth), because two concurrent requests carrying
  -- the same cookie must not sign the user out.
  rotated_at timestamptz,

  revoked_at timestamptz,
  revoked_reason text,

  ip inet,
  user_agent text,

  constraint user_sessions_idle_within_absolute
    check (expires_at <= absolute_expires_at),
  constraint user_sessions_expires_after_issue
    check (expires_at > issued_at)
);
-- +goose StatementEnd

-- The hot path is lookup by hash, served by the unique index. These serve
-- "sign me out everywhere", the session list in account settings, and the
-- sweeper.
-- +goose StatementBegin
create index user_sessions_user_id_idx on user_sessions (user_id)
  where revoked_at is null;
-- +goose StatementEnd

-- +goose StatementBegin
create index user_sessions_absolute_expires_at_idx on user_sessions (absolute_expires_at);
-- +goose StatementEnd

-- +goose StatementBegin
-- The app role needs these, and gets no policy on them for the reasons in the
-- header. Kept as an explicit grant rather than a default privilege so that a
-- future table added without a deliberate grant fails loudly.
grant select, insert, update, delete on login_tokens, oauth_identities, user_sessions
  to halyard_app;
-- +goose StatementEnd
