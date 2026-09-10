-- Invites, and a column the contract requires. Neither is in SPEC §6.
--
-- `invites` is absent from §6 entirely, yet §7.1 defines
-- `POST /v1/orgs/:org/invites` and api.openapi.yaml models the `Invite`
-- response — so the table is implied by the contract rather than optional.
-- Same licence as templates and the auth tables: §6's "Not exhaustive".
--
-- `template_versions.image_ready` is required by api.openapi.yaml's
-- TemplateVersion, and it is not cosmetic: SPEC §9 and §17.3 put a p50 < 10s
-- SLO on reaching a first preview, which is only achievable from a pre-baked
-- sandbox image. A version whose image is not built cannot meet it, so the
-- console has to be able to tell.

-- +goose Up

-- +goose StatementBegin
-- An invitation to join an org.
--
-- Keyed on the EMAIL rather than on a user, because the whole point is that the
-- invitee may not have an account yet — the same reason login_tokens stores an
-- address. Accepting one creates or links the user.
create table invites (
  id uuid primary key default gen_random_uuid(),
  org_id uuid not null references orgs(id) on delete cascade,
  email text not null,
  role text not null check (role in ('owner','admin','editor','viewer')),

  -- sha256 of the invite token, never the token. Same reasoning as
  -- login_tokens and user_sessions: a dump of this table yields nothing that
  -- can be redeemed.
  token_hash bytea not null unique,

  invited_by uuid references users(id) on delete set null,
  expires_at timestamptz not null,
  accepted_at timestamptz,
  accepted_by uuid references users(id) on delete set null,
  revoked_at timestamptz,
  created_at timestamptz not null default now(),

  constraint invites_expires_after_creation check (expires_at > created_at)
);
-- +goose StatementEnd

-- At most one live invite per address per org. Without this, clicking "invite"
-- twice sends two links, and accepting the older one after the newer has been
-- revoked is a confusing way to end up with the wrong role.
-- +goose StatementBegin
create unique index invites_one_live_per_email_idx on invites (org_id, lower(email))
  where accepted_at is null and revoked_at is null;
-- +goose StatementEnd

-- +goose StatementBegin
create index invites_org_id_created_at_idx on invites (org_id, created_at desc);
-- +goose StatementEnd

-- +goose StatementBegin
create index invites_expires_at_idx on invites (expires_at)
  where accepted_at is null and revoked_at is null;
-- +goose StatementEnd

-- Tenant-scoped like everything else carrying an org_id, permissive plus the
-- restrictive guard from 00015.
-- +goose StatementBegin
alter table invites enable row level security;
-- +goose StatementEnd

-- +goose StatementBegin
alter table invites force row level security;
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation on invites
  using (org_id = halyard_current_org_id());
-- +goose StatementEnd

-- +goose StatementBegin
create policy tenant_isolation_guard on invites
  as restrictive
  using (org_id = halyard_current_org_id());
-- +goose StatementEnd

-- +goose StatementBegin
grant select, insert, update, delete on invites to halyard_app;
-- +goose StatementEnd

-- +goose StatementBegin
-- Whether the pre-baked sandbox image exists for this template version. False
-- until the image build completes, so the console can say "preparing" rather
-- than start a project that will miss its SLO.
alter table template_versions add column image_ready boolean not null default false;
-- +goose StatementEnd
