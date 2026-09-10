# Halyard — Requirements and Solution Specification

**Status:** implementation-ready draft
**Audience:** the coding agent implementing this system, and the humans reviewing it
**Product:** a SaaS where users build full-stack Next.js apps by prompting an agent, then deploy, domain, monetise and market them from the same console

---

## 0. How to use this document

Read sections 1–7 fully before writing any code. They contain the contracts everything else depends on. Then implement strictly in the phase order given in §20 — each phase has acceptance criteria and should end in a working, deployed, demonstrable state.

**Rules for the implementing agent:**

1. **Do not skip §5 (decision log).** Several obvious-looking choices are deliberately rejected there with reasons. Re-litigating them wastes a phase.
2. **Contracts before implementations.** Generate the JSON Schemas in `packages/schema` first and derive TypeScript, Go and Python types from them. Never hand-write the same type twice.
3. **Verify platform facts before depending on them.** Everything in §22 is marked as needing verification against current vendor documentation. My information may be out of date. Check before you build on it, and record what you found in `docs/verified.md`.
4. **Every phase ships tests.** Unit tests for pure logic, integration tests against a real Postgres in Docker, and one end-to-end test per user-visible flow. A phase with no tests is not complete.
5. **Never invent an API surface.** If a vendor endpoint you need isn't documented, stop and add it to `docs/open-questions.md` rather than writing hopeful code.
6. **Migrations are forward-only and additive.** No destructive migrations on the control plane database without an explicit human approval note in the PR.
7. **Ask, don't assume, on the items in §21.** Those are business decisions, not engineering ones.

---

## 1. Product scope

### 1.1 What the product does

A user signs up, picks a template or describes what they want, and gets a working full-stack Next.js application within seconds. They then iterate by talking to an agent, editing code directly, or both. When ready they publish it to a custom domain. From the same console they add features (accounts, payments, email, uploads, third-party integrations), run Google and Meta ad campaigns, fix search visibility issues, and see exactly what all of it costs in credits.

### 1.2 Core capability list

| # | Capability | Notes |
|---|---|---|
| C1 | Prompt-to-app generation | Agent-driven, in an isolated sandbox |
| C2 | Template library | Versioned, git-based, with pre-baked images |
| C3 | Live preview | Per-branch, hot reload |
| C4 | File browsing and direct code editing | Human and agent share one working tree |
| C5 | Version history and restore | Forward-only, every change attributed |
| C6 | GitHub two-way sync | Optional; GitHub becomes authoritative once linked |
| C7 | Deployment and rollback | Immutable artifacts, instant rollback |
| C8 | Custom domains | Registration or connection, automatic TLS |
| C9 | Capability installs | Auth, payments, email, uploads, integrations |
| C10 | Google Ads and Meta Ads management | Agent proposes, human approves, platform executes |
| C11 | SEO audit and automated fixes | Crawl, score, agent fixes in code, track results |
| C12 | Credits: metering, rating, ledger | Everything metered, reserve-and-settle |
| C13 | AI features for generated apps | Phase 7; gateway-metered |

### 1.3 Explicit non-goals for v1

- Not a general-purpose IDE. No terminal exposed to the user, no arbitrary language support. Next.js only.
- No multiplayer editing. One writer at a time, enforced by a lease.
- No mobile app builds.
- No self-hosting of the generated apps by the user in v1 (but see §19 on code export, which is mandatory).
- No AI features in the generated apps until phase 7.

---

## 2. Architecture overview

Five planes. The boundaries between them are security boundaries, not just organisational ones.

```
┌──────────────────────────────────────────────────────────────┐
│ PRODUCT SURFACE   Next.js console (Cloudflare Workers)       │
└────────────────────────────┬─────────────────────────────────┘
                             │ HTTPS + SSE
┌────────────────────────────▼─────────────────────────────────┐
│ CONTROL PLANE (trusted, containers)                          │
│  api (Go)   gitd (Go)   aigw (Go)   mcp (Go)                 │
│  sandboxd (Python)      workers (Python)                     │
└──────┬──────────────────┬───────────────────┬────────────────┘
       │                  │                   │
┌──────▼──────────┐ ┌─────▼──────────┐ ┌──────▼────────────────┐
│ AGENT PLANE     │ │ DATA PLANE     │ │ EXTERNAL              │
│ (UNTRUSTED)     │ │ Postgres       │ │ GitHub, Google Ads,   │
│ Modal sandboxes │ │ Redis          │ │ Meta, Stripe, Neon,   │
│ opencode+agentd │ │ R2             │ │ Cloudflare, registrar │
└─────────────────┘ │ ClickHouse     │ └───────────────────────┘
                    └────────────────┘
```

**The one rule that shapes everything:** the sandbox is untrusted. It runs code an LLM wrote, influenced by content the LLM read from the internet and from the user's own repository. It therefore never holds a credential that can spend money or reach another tenant's data. Everything privileged happens in the control plane, reached over authenticated MCP or HTTP with a short-lived, project-scoped token.

---

## 3. Technology stack

### 3.1 Frontend — Next.js

- **Next.js (App Router), TypeScript, React.**
- **Deployment:** Cloudflare Workers via `@opennextjs/cloudflare`. Verify current feature support (§22).
- **Styling:** Tailwind CSS with a token layer. The design system is defined by the approved mockup — read `docs/mockup.html` and extract tokens from it rather than inventing new ones.
- **Data fetching:** TanStack Query. Server Components for initial loads, client queries for anything that polls or streams.
- **Streaming:** native `EventSource`/`fetch` streaming against the SSE gateway. Do not use WebSockets in v1 — SSE is sufficient, survives proxies better, and needs no reconnect protocol of our own beyond `Last-Event-ID`.
- **Editor:** CodeMirror 6. Not Monaco — smaller bundle, better mobile behaviour, and we are not shipping a language server in v1.
- **Local state:** Zustand for the builder shell (open file, lease state, panel layout). Nothing else needs global state.
- **Forms:** react-hook-form + zod, with zod schemas generated from `packages/schema`.

### 3.2 Backend — Go and Python, split by workload

This is a deliberate split, not indecision. Justification in §5.1.

**Go services:**

| Service | Responsibility |
|---|---|
| `api` | REST API, auth, org/project CRUD, SSE gateway, approval gates |
| `gitd` | Git HTTP backend proxy, pre-receive policy, tree/blob read API, diffs |
| `aigw` | AI gateway — LLM provider proxy, token metering, spend caps, caching |
| `mcp` | MCP tool servers exposed to the sandbox over authenticated HTTP |

Stack: Go 1.23+, `chi` or stdlib `net/http` routing, `pgx` (no ORM — write SQL), `river` for the Postgres-backed job queue, `go-git` where it beats shelling out and plain `git` subprocesses where it doesn't (packfile operations, `git-upload-pack`).

**Python services:**

| Service | Responsibility |
|---|---|
| `sandboxd` | Modal sandbox lifecycle, warm pool, snapshots, tunnels |
| `workers` | Ads sync, SEO crawl and audit, rating, reconciliation, GitHub sync |

Stack: Python 3.12+, FastAPI, `modal`, `asyncpg`, `google-ads`, `facebook-business`, Playwright for crawling. Workers consume the same `river` queue tables that Go writes.

**Interface between them:** internal HTTP over mTLS (or a private network with bearer tokens), plus the shared Postgres queue. Define the `sandboxd` interface in `packages/schema/sandboxd.openapi.yaml` and generate both the Go client and the FastAPI models from it.

### 3.3 Database — Postgres

Two distinct roles, do not conflate them:

**Control plane database.** One Postgres cluster, ours. Every tenant table carries `org_id` and has row-level security enabled. Migrations with `goose` (SQL files, checked in, forward-only).

**Application databases.** One database per generated app, with a branch per preview. Provision via Neon's API — branching is the feature that makes preview migrations safe. Store only a reference in our control plane; the connection string lives in the secret store.

Fallback if Neon is rejected: schema-per-project on a shared Postgres cluster behind PgBouncer. Cheaper, but you lose branching and must build migration dry-runs yourself. Record the decision in `docs/verified.md`.

**Analytics.** ClickHouse for usage events, ad metrics and rank history. Phase 1–4 may use partitioned Postgres tables instead; introduce ClickHouse in phase 5 when the metering volume justifies it. Write the metering ingest behind an interface so the swap is a config change.

**Redis** for: write-lease TTLs, credit balance cache, warm-pool registry, rate limit counters, SSE fan-out pub/sub. Nothing durable.

### 3.4 Agent — opencode, forked

`opencode` runs headless inside each sandbox. We maintain a small patch set on a pinned upstream tag rather than a divergent fork. Details in §11.

### 3.5 Deployment — Cloudflare, with one honest caveat

**Read this before planning infrastructure.**

Cloudflare Workers runs JavaScript and WASM. It cannot run a Go or Python service of this shape. So "deploy on Cloudflare" resolves to:

| Component | Where it runs |
|---|---|
| Console (Next.js) | Cloudflare Workers via OpenNext |
| Generated user apps (Next.js) | Cloudflare Workers via OpenNext, one Worker per project |
| Custom hostnames + TLS for user apps | Cloudflare for SaaS |
| Object storage (artifacts, packfiles, blob cache) | Cloudflare R2 |
| Hostname → deployment routing table | Workers KV |
| Write lease + SSE coordination | Durable Objects (optional, see §5.4) |
| Postgres access from Workers | Cloudflare Hyperdrive |
| Edge rate limiting, WAF, DNS, DDoS | Cloudflare |
| Analytics ingest from generated apps | Cloudflare Queues + Worker |
| **`api`, `gitd`, `aigw`, `mcp`, `sandboxd`, `workers`** | **Containers** |
| Sandboxes | Modal |

For the containers, two options. Verify both (§22) before choosing:

- **Cloudflare Containers**, if its current limits on memory, request duration, persistent connections and long-lived SSE suit us. This keeps the vendor surface small.
- **A container host** (Fly.io, Railway, GCP Cloud Run with min-instances) fronted by Cloudflare DNS, WAF and Tunnel. More moving parts, fewer unknowns.

Do not attempt to port `gitd` or `aigw` to Workers. Long-lived streaming and git packfile handling are the wrong shape for that runtime.

---

## 4. Repository layout

Single monorepo, pnpm workspaces for JS, Go workspace for Go, uv for Python.

```
halyard/
├── apps/
│   └── console/                 # Next.js SaaS frontend
├── services/
│   ├── api/                     # Go
│   ├── gitd/                    # Go
│   ├── aigw/                    # Go
│   ├── mcp/                     # Go
│   ├── sandboxd/                # Python
│   └── workers/                 # Python
├── agent/
│   ├── opencode/                # git submodule, pinned tag
│   ├── patches/                 # our patch set, applied in CI
│   ├── agentd/                  # in-sandbox supervisor sidecar (Go)
│   ├── image/                   # sandbox Dockerfile + Modal image defs
│   └── config/                  # opencode.json + AGENTS.md per template
├── capabilities/                # capability modules (see §13)
│   ├── auth/
│   ├── payments/
│   ├── email/
│   └── uploads/
├── templates/                   # app templates, each a git repo
├── packages/
│   ├── schema/                  # JSON Schema + OpenAPI, single source of truth
│   └── ui/                      # shared React components
├── db/
│   └── migrations/              # control plane, goose
├── infra/
│   ├── cloudflare/              # wrangler configs, terraform
│   └── modal/
└── docs/
    ├── SPEC.md                  # this file
    ├── verified.md              # platform facts confirmed against vendor docs
    └── open-questions.md
```

---

## 5. Decision log — choices already made, with reasons

### 5.1 Why both Go and Python, rather than one language

Modal's SDK is Python. There is no supported Go SDK, so an all-Go backend would need a Python sidecar for sandbox orchestration anyway — you get the two-language cost with none of the benefit. Meanwhile the API and git proxy hold thousands of concurrent long-lived streams and do heavy git plumbing, where Go's memory-per-connection and process model are materially better than Python's.

The split is drawn so each language owns whole services with a narrow interface, not so they interleave.

**If your team is one or two engineers, go all-Python** (FastAPI + uvicorn, `arq` or Temporal for jobs) and accept worse streaming fan-out economics until it hurts. That is a legitimate trade. Do not go all-Go.

### 5.2 Why the app's auth lives in the app's own database

Not a hosted per-project tenant (Clerk, WorkOS, Auth0). At scale you would be provisioning tens of thousands of third-party tenants, each with its own billing relationship, rate limits and failure modes, and your users' user tables would be hostage to a vendor. A code-first library keeps users and sessions in the project's own Postgres, so the agent can read and extend the schema like any other code and the app runs entirely offline in the sandbox.

Use Auth.js or Better Auth. Verify current state of both (§22) and pin a version per template.

### 5.3 Why Stripe Connect, not "paste your API key"

The money belongs to the user, not us. Connect means we never hold their secret key or touch card data, hosted onboarding handles KYC, and `application_fee_amount` leaves a revenue-share option open. Storing users' live Stripe secret keys would put custody and liability on us for no benefit.

### 5.4 Durable Objects for the write lease — optional, not required

Redis with a TTL is sufficient for v1 and keeps the lease logic in one language. Durable Objects are a better long-term fit (single-threaded per session, colocated with the edge) but add a second coordination system. Build on Redis, keep the lease behind an interface, revisit in phase 6.

### 5.5 Why the git store is ours and GitHub is a mirror-then-authority

Users who never connect GitHub still need history, undo and rollback, so an internal store is mandatory. Once GitHub is linked it becomes authoritative and the internal store is a cache plus outbound queue — a single owner per project at all times. Dual authority on one branch is a split-brain bug factory; do not build it.

### 5.6 Why revert is a new commit, never a reset

See §12.4. Forward-only history means GitHub sync stays append-only (no force-push permission needed, no risk of clobbering a collaborator), undoing an undo is free, and every checkpoint and commit is a valid target through one code path.

---

## 6. Data model

Control plane DDL sketch. Not exhaustive — add columns as needed, but do not rename these or change their semantics without updating this document.

```sql
-- ============ tenancy ============
create table orgs (
  id uuid primary key default gen_random_uuid(),
  name text not null,
  slug text not null unique,
  plan text not null default 'free',
  suspended_at timestamptz,
  created_at timestamptz not null default now()
);

create table users (
  id uuid primary key default gen_random_uuid(),
  email text not null unique,
  name text,
  created_at timestamptz not null default now()
);

create table org_members (
  org_id uuid not null references orgs(id) on delete cascade,
  user_id uuid not null references users(id) on delete cascade,
  role text not null check (role in ('owner','admin','editor','viewer')),
  primary key (org_id, user_id)
);

-- ============ projects ============
create table projects (
  id uuid primary key default gen_random_uuid(),
  org_id uuid not null references orgs(id) on delete cascade,
  name text not null,
  slug text not null,
  template_version_id uuid references template_versions(id),
  default_branch text not null default 'main',
  git_authority text not null default 'internal'
    check (git_authority in ('internal','github')),
  archived_at timestamptz,
  created_at timestamptz not null default now(),
  unique (org_id, slug)
);

create table branches (
  id uuid primary key default gen_random_uuid(),
  project_id uuid not null references projects(id) on delete cascade,
  name text not null,
  head_sha text,
  app_db_branch_ref text,
  preview_deployment_id uuid,
  unique (project_id, name)
);

-- ============ git + github ============
create table github_installations (
  id uuid primary key default gen_random_uuid(),
  org_id uuid not null references orgs(id) on delete cascade,
  installation_id bigint not null unique,
  account_login text not null,
  suspended_at timestamptz
);

create table repos (
  id uuid primary key default gen_random_uuid(),
  project_id uuid not null unique references projects(id) on delete cascade,
  internal_path text not null,
  provider text check (provider in ('github')),
  installation_id uuid references github_installations(id),
  owner text, name text,
  last_synced_sha text,
  pending_push_count int not null default 0
);

create table sync_jobs (
  id bigserial primary key,
  repo_id uuid not null references repos(id) on delete cascade,
  direction text not null check (direction in ('outbound','inbound')),
  status text not null check (status in ('queued','running','failed','done')),
  attempts int not null default 0,
  last_error text,
  created_at timestamptz not null default now()
);

-- ============ agent sessions ============
create table sessions (
  id uuid primary key default gen_random_uuid(),
  project_id uuid not null references projects(id) on delete cascade,
  branch_id uuid not null references branches(id),
  user_id uuid not null references users(id),
  sandbox_id text,
  state text not null check (state in ('starting','live','idle','stopped','failed')),
  started_at timestamptz not null default now(),
  ended_at timestamptz
);

create table turns (
  id uuid primary key default gen_random_uuid(),
  session_id uuid not null references sessions(id) on delete cascade,
  seq int not null,
  actor text not null check (actor in ('user','agent')),
  prompt text,
  status text not null check (status in ('running','done','failed','aborted','budget_exceeded')),
  checkpoint_sha text,
  hold_id uuid,
  credits_charged numeric(14,4),
  tokens_in bigint, tokens_out bigint,
  tokens_cache_read bigint, tokens_cache_write bigint,
  sandbox_seconds numeric(10,2),
  started_at timestamptz not null default now(),
  ended_at timestamptz,
  unique (session_id, seq)
);

create table checkpoints (
  id bigserial primary key,
  session_id uuid not null references sessions(id) on delete cascade,
  turn_seq int not null,
  sha text not null,
  parent_sha text,
  files_touched text[],
  created_at timestamptz not null default now()
);

-- ============ timeline (renders the history UI) ============
create table timeline_events (
  id bigserial primary key,
  project_id uuid not null references projects(id) on delete cascade,
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
  created_at timestamptz not null default now()
);
create index on timeline_events (project_id, created_at desc);

-- ============ deploy + domains ============
create table deployments (
  id uuid primary key default gen_random_uuid(),
  project_id uuid not null references projects(id) on delete cascade,
  branch_id uuid not null references branches(id),
  version int not null,
  sha text not null,
  artifact_key text not null,
  worker_name text,
  env text not null check (env in ('preview','production')),
  status text not null check (status in ('building','ready','failed','superseded')),
  is_live boolean not null default false,
  built_at timestamptz,
  unique (project_id, version)
);

create table domains (
  id uuid primary key default gen_random_uuid(),
  project_id uuid not null references projects(id) on delete cascade,
  hostname text not null unique,
  kind text not null check (kind in ('apex','www','subdomain')),
  source text not null check (source in ('registered','connected')),
  cf_custom_hostname_id text,
  dns_status text not null default 'pending',
  tls_status text not null default 'pending',
  redirect_to text,
  verified_at timestamptz
);

-- ============ capabilities ============
create table capabilities (
  id text primary key,                    -- 'payments'
  display_name text not null,
  summary text not null,
  credential_owner text not null
    check (credential_owner in ('none','builder','runtime')),
  status text not null default 'available'
    check (status in ('available','beta','coming_soon'))
);

create table capability_versions (
  id uuid primary key default gen_random_uuid(),
  capability_id text not null references capabilities(id),
  version text not null,
  manifest jsonb not null,
  framework_range text not null,
  unique (capability_id, version)
);

create table project_capabilities (
  project_id uuid not null references projects(id) on delete cascade,
  capability_id text not null references capabilities(id),
  version_id uuid not null references capability_versions(id),
  state text not null check (state in (
    'provisioning','needs_action','installed','failed','removing'
  )),
  external_refs jsonb not null default '{}',  -- stripe acct id, slack team id
  installed_at timestamptz,
  primary key (project_id, capability_id)
);

create table app_databases (
  project_id uuid primary key references projects(id) on delete cascade,
  provider text not null default 'neon',
  external_project_id text not null,
  production_branch_ref text not null,
  migrations_applied int not null default 0
);

-- ============ secrets + connectors ============
create table secrets (
  id uuid primary key default gen_random_uuid(),
  project_id uuid not null references projects(id) on delete cascade,
  env text not null check (env in ('development','production')),
  key text not null,
  ciphertext bytea not null,
  dek_id text not null,
  rotated_at timestamptz,
  unique (project_id, env, key)
);

create table connectors (
  id uuid primary key default gen_random_uuid(),
  org_id uuid not null references orgs(id) on delete cascade,
  provider text not null,            -- google_ads | meta | gsc | slack | ...
  external_account_id text,
  scopes text[] not null,
  refresh_ciphertext bytea not null,
  dek_id text not null,
  status text not null default 'active',
  unique (org_id, provider, external_account_id)
);

-- ============ growth ============
create table ad_accounts (
  id uuid primary key default gen_random_uuid(),
  project_id uuid not null references projects(id) on delete cascade,
  channel text not null check (channel in ('google','meta')),
  external_id text not null,
  connector_id uuid not null references connectors(id),
  unique (channel, external_id)
);

create table campaigns (
  id uuid primary key default gen_random_uuid(),
  ad_account_id uuid not null references ad_accounts(id) on delete cascade,
  external_id text,
  name text not null,
  state text not null check (state in ('draft','pending_approval','running','paused','ended')),
  daily_budget_cents bigint,
  mirror jsonb not null default '{}'
);

create table proposals (
  id uuid primary key default gen_random_uuid(),
  project_id uuid not null references projects(id) on delete cascade,
  kind text not null,               -- campaign_create | budget_change | seo_fix | destructive_migration
  target_ref jsonb not null,
  rationale text not null,
  diff jsonb not null,
  projected jsonb,
  status text not null check (status in ('pending','approved','declined','expired','executed','failed')),
  decided_by uuid references users(id),
  decided_at timestamptz,
  expires_at timestamptz not null,
  created_at timestamptz not null default now()
);

create table seo_issues (
  id bigserial primary key,
  project_id uuid not null references projects(id) on delete cascade,
  audit_id uuid not null,
  code text not null,
  pages text[] not null,
  impact int not null,             -- 0..100
  agent_fixable boolean not null,
  status text not null default 'open'
    check (status in ('open','queued','fixed','dismissed','needs_user'))
);

create table rank_snapshots (
  project_id uuid not null,
  query text not null,
  position int, clicks int, impressions int,
  captured_on date not null,
  primary key (project_id, query, captured_on)
);

-- ============ credits ============
create table price_books (
  id uuid primary key default gen_random_uuid(),
  version text not null unique,
  effective_from timestamptz not null,
  items jsonb not null           -- meter -> {unit, credits_per_unit}
);

create table usage_events (
  id bigserial primary key,
  org_id uuid not null references orgs(id) on delete cascade,
  project_id uuid references projects(id) on delete set null,
  source text not null,          -- agentd | sandboxd | aigw | worker | edge
  event_key text not null,       -- idempotency key from the emitter
  meter text not null,           -- tokens_in | sandbox_seconds | build | ...
  quantity numeric(20,6) not null,
  occurred_at timestamptz not null,
  rated_at timestamptz,
  ledger_entry_id bigint,
  unique (source, event_key)
);

create table holds (
  id uuid primary key default gen_random_uuid(),
  org_id uuid not null references orgs(id) on delete cascade,
  amount numeric(14,4) not null,
  reason text not null,
  ref jsonb not null,
  status text not null check (status in ('active','settled','expired','released')),
  expires_at timestamptz not null,
  created_at timestamptz not null default now()
);

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
create index on ledger_entries (org_id, created_at desc);

create table grants (
  id uuid primary key default gen_random_uuid(),
  org_id uuid not null references orgs(id) on delete cascade,
  amount numeric(14,4) not null,
  expires_at timestamptz,          -- null = purchased, never expires
  consumed numeric(14,4) not null default 0
);

create table org_balances (           -- materialised, rebuildable from ledger
  org_id uuid primary key references orgs(id) on delete cascade,
  available numeric(14,4) not null default 0,
  held numeric(14,4) not null default 0,
  build_used_period numeric(14,4) not null default 0,
  runtime_used_period numeric(14,4) not null default 0,
  updated_at timestamptz not null default now()
);

-- ============ platform ============
create table audit_log (
  id bigserial primary key,
  org_id uuid, actor_user_id uuid, actor_kind text not null,
  action text not null, target jsonb not null,
  ip inet, user_agent text,
  created_at timestamptz not null default now()
);
```

**Row-level security:** enable on every table with `org_id`. Policy shape:

```sql
alter table projects enable row level security;
create policy tenant_isolation on projects
  using (org_id = current_setting('app.org_id')::uuid);
```

`api` sets `app.org_id` per transaction from the verified session. This is defence in depth, not the primary control — application queries must still filter explicitly.

---

## 7. Contracts

### 7.1 REST API surface (`api`)

All under `/v1`. Auth via session cookie (console) or bearer PAT (future CLI). Every mutating endpoint accepts `Idempotency-Key`.

```
POST   /v1/orgs                                   create org
GET    /v1/orgs/:org/members
POST   /v1/orgs/:org/invites

GET    /v1/templates
POST   /v1/projects                               {template_version_id | prompt}
GET    /v1/projects/:p
DELETE /v1/projects/:p

POST   /v1/projects/:p/sessions                   acquire sandbox, returns session
GET    /v1/projects/:p/sessions/:s/events         SSE — agent event stream
POST   /v1/projects/:p/sessions/:s/turns          send a prompt
POST   /v1/projects/:p/sessions/:s/abort
POST   /v1/projects/:p/sessions/:s/lease          acquire/release write lease

GET    /v1/projects/:p/tree?ref=&path=            file listing (from git)
GET    /v1/projects/:p/blob?ref=&path=            file content
PUT    /v1/projects/:p/file                       human edit → sandbox overlay
GET    /v1/projects/:p/diff?base=&head=

GET    /v1/projects/:p/timeline
POST   /v1/projects/:p/restore                    {target_sha, paths?}

GET    /v1/projects/:p/deployments
POST   /v1/projects/:p/deployments                publish current branch
POST   /v1/projects/:p/deployments/:d/rollback    repoint edge, no rebuild

GET    /v1/projects/:p/domains
POST   /v1/projects/:p/domains                    {hostname, source}
GET    /v1/projects/:p/domains/:d/records         DNS records to add

GET    /v1/projects/:p/capabilities
POST   /v1/projects/:p/capabilities/:cap          install
DELETE /v1/projects/:p/capabilities/:cap

GET    /v1/projects/:p/repo                       github link state, pending count
POST   /v1/projects/:p/repo/link
POST   /v1/projects/:p/repo/retry-sync

GET    /v1/projects/:p/ads/campaigns
GET    /v1/projects/:p/ads/metrics?from=&to=
POST   /v1/projects/:p/ads/draft                  agent drafts a campaign

GET    /v1/projects/:p/seo/issues
POST   /v1/projects/:p/seo/fix                    {issue_ids[]}
GET    /v1/projects/:p/seo/ranks

GET    /v1/proposals?status=pending
POST   /v1/proposals/:id/approve
POST   /v1/proposals/:id/decline

GET    /v1/orgs/:org/credits                      balance, holds, split
GET    /v1/orgs/:org/credits/ledger
POST   /v1/orgs/:org/credits/purchase
PUT    /v1/orgs/:org/credits/auto-topup

POST   /v1/webhooks/github
POST   /v1/webhooks/stripe
POST   /v1/webhooks/cloudflare
POST   /v1/relay/:project/:capability             stable webhook relay for user apps
```

### 7.2 Agent event stream

`agentd` emits these; `api` relays them over SSE with monotonic `id` for `Last-Event-ID` resume. Define in `packages/schema/agent-events.schema.json`.

```jsonc
{ "type": "turn.started",  "turn": 12, "hold": { "credits": 40 } }
{ "type": "message.delta", "turn": 12, "text": "I'll build the page…" }
{ "type": "tool.started",  "turn": 12, "id": "t1",
  "tool": "edit", "target": "app/pricing/page.tsx" }
{ "type": "tool.finished", "turn": 12, "id": "t1",
  "ok": true, "summary": "created", "diff_stat": { "add": 84, "del": 0 } }
{ "type": "lease.changed", "holder": "agent" }
{ "type": "preview.status", "state": "rebuilding" }
{ "type": "usage.tick",    "turn": 12,
  "tokens": { "in": 84000, "out": 3100, "cache_read": 22000, "cache_write": 0 },
  "sandbox_seconds": 46 }
{ "type": "turn.finished", "turn": 12, "status": "done",
  "checkpoint_sha": "7d9e014", "credits": 31 }
{ "type": "error", "code": "budget_exceeded", "message": "…", "retriable": false }
```

Rules: every event carries `turn`; the UI must tolerate unknown `type` values; `usage.tick` is advisory for the UI and authoritative only when reconciled server-side.

### 7.3 MCP tool contracts

Hosted by `mcp` in the control plane, **not** in the sandbox. The sandbox connects over HTTP with its session JWT. Every tool authorises against `(org_id, project_id)` derived from the token, never from arguments.

| Tool | Approval | Notes |
|---|---|---|
| `db.migrate` | auto if additive; **proposal** if destructive | Runs against the preview branch only |
| `deploy.preview` | auto | Build + deploy current branch |
| `deploy.publish` | **proposal** | Production |
| `capability.install` | auto | Provisioning may return `needs_action` |
| `secrets.list_keys` | auto | Returns key names only, never values |
| `domain.records` | auto | Read-only |
| `ads.read` | auto | Metrics and current config |
| `ads.propose` | **always a proposal** | Never mutates directly |
| `seo.audit_read` | auto | |
| `seo.keyword_lookup` | auto, metered | Costs real money per call |
| `web.fetch` | auto, allowlisted | Returns text, stripped of scripts |

**Hard rule:** no MCP tool ever returns a secret value, and no tool that spends money executes without an approved `proposals` row. The agent cannot approve its own proposal — the endpoint requires a user session.

### 7.4 Capability manifest schema

`packages/schema/capability-manifest.schema.json`:

```jsonc
{
  "id": "payments",
  "version": "2.4.0",
  "display_name": "Payments",
  "framework": { "next": ">=15 <16" },
  "credential_owner": "builder",
  "provision": {
    "kind": "stripe_connect",
    "completion": "hosted_onboarding",
    "produces": ["stripe_account_id"]
  },
  "dependencies": { "stripe": "^17.0.0" },
  "files": [
    { "path": "app/api/checkout/route.ts", "template": "checkout.route.ts.hbs", "on_conflict": "skip" },
    { "path": "app/api/stripe/webhook/route.ts", "template": "webhook.route.ts.hbs", "on_conflict": "skip" },
    { "path": "lib/stripe.ts", "template": "stripe.ts.hbs", "on_conflict": "overwrite" }
  ],
  "migrations": [
    { "id": "0001_subscriptions", "sql": "migrations/0001.sql", "additive": true }
  ],
  "env": [
    { "key": "STRIPE_PUBLISHABLE_KEY", "scope": "public", "from": "provision.publishable_key" },
    { "key": "STRIPE_SECRET_KEY", "scope": "server", "from": "provision.secret_key", "dev_variant": "test_mode" },
    { "key": "STRIPE_WEBHOOK_SECRET", "scope": "server", "from": "provision.webhook_secret" }
  ],
  "webhooks": [
    { "provider": "stripe", "relay_path": "/relay/{project}/payments", "events": ["checkout.session.completed", "customer.subscription.*"] }
  ],
  "smoke_check": { "kind": "http", "path": "/api/checkout", "method": "OPTIONS", "expect_status": 204 },
  "uninstall": { "remove_files": true, "remove_env": true, "drop_tables": false }
}
```

Installation must be **idempotent** (re-running is a no-op) and **removable** (files and env go, data tables stay with a warning).

---

## 8. Auth, tenancy and roles (console side)

- Sign-in: email magic link + Google OAuth. Sessions as httpOnly, `SameSite=Lax`, rotating cookies. Verify library choice (§22).
- Every request resolves to `(user_id, org_id, role)`. Role checks in a single middleware, never inline.
- Roles: `owner` (billing, delete, transfer), `admin` (everything except billing/delete), `editor` (build, deploy preview, propose), `viewer` (read).
- Approving anything that spends money requires `owner` or `admin`.
- Org switching in the project picker. Users may belong to many orgs.
- Full audit log for: sign-in, role change, secret read/write, proposal decision, publish, rollback, domain change, credit purchase, member removal.

---

## 9. Sandbox orchestration (`sandboxd`)

**Responsibilities:** create, resume, snapshot, stop sandboxes; maintain a warm pool; expose tunnels; enforce quotas; heartbeat usage.

**Lifecycle**

1. `POST /sessions` → look for a warm sandbox for this project's template image.
2. Miss → create from the template's pre-baked image with the project's Modal Volume mounted.
3. Reconcile: `git fetch && git reset --hard <branch head>`; if lockfile hash changed, reinstall dependencies.
4. Start `agentd`, which starts `opencode serve` on a private port and the dev server on another.
5. Register the tunnel URL against `<branch>.<project>.preview.<domain>` in Workers KV.
6. Heartbeat every 10s: sandbox seconds, CPU, memory, liveness → `usage_events`.
7. Idle 15 minutes → snapshot filesystem, stop sandbox, keep the volume.

**Quotas per session (hard, enforced in `agentd` and again in `aigw`):**

| Limit | Default |
|---|---|
| Wall clock per turn | 10 min |
| Tokens per turn | 400k |
| Tokens per session | 4M |
| CPU | 2 vCPU |
| Memory | 4 GB |
| Concurrent sandboxes per org | plan-dependent |

**Egress allowlist** — deny by default, allow only: package registries, `aigw`, `gitd`, `mcp`, and the project's own app database host. No general internet. `web.fetch` goes through `mcp`, which does the fetching and strips scripts.

**Warm pool:** 2–5 per popular template image per region, scaled on a rolling p50 of session starts. This is the single biggest lever on perceived product quality — treat time-to-first-preview as a tracked SLO (§17).

---

## 10. Git service (`gitd`)

**Storage.** One bare repo per project on durable storage, packfiles backed up to R2 nightly with `git gc` first. Path from `repos.internal_path`.

**Endpoints.**

```
GET/POST /git/:project/info/refs, /git-upload-pack, /git-receive-pack
GET      /api/tree?ref=&path=
GET      /api/blob?ref=&path=       (size-capped; >1MB returns a signed R2 URL)
GET      /api/diff?base=&head=      (structured hunks)
POST     /api/commit-tree           (restore: new commit from an old tree)
POST     /api/checkpoint            (commit under refs/checkpoints/<session>/<turn>)
```

**Auth.** Sandbox presents its session JWT. `gitd` validates it, extracts `project_id`, and refuses any path outside that project. The sandbox never receives a GitHub token.

**Pre-receive policy — the only chokepoint before code becomes durable.** Reject the push if:

1. Any added file matches a secret pattern (`.env*`, private keys, high-entropy strings matching known key prefixes such as `sk_live_`, `ghp_`, `AKIA`).
2. `node_modules/`, `.next/`, `dist/` or files >10MB appear.
3. Total push size exceeds 50MB.
4. The commit author does not match the authenticated identity.

Return a structured error the UI can render as a human-readable message, not a raw git error.

**Read path.** Committed files always come from git, never from the sandbox — browsing must not require a running container. The dirty overlay comes from the sandbox and is merged by `api`. Blobs are content-addressed, so cache them in R2/Redis by sha forever; cache diffs by `(base, head)`.

---

## 11. The agent (`opencode` fork + `agentd`)

### 11.1 Fork policy

Pin upstream at a tag. Keep changes as numbered patch files in `agent/patches/`, applied in CI with `git apply`. Rebase onto upstream monthly; if a patch stops applying, that is a scheduled task, not an emergency. **Do not restructure upstream code.** Anything that can be done with configuration or MCP must not be a patch.

### 11.2 Required patches

| # | Patch | Why it cannot be config |
|---|---|---|
| P1 | Per-turn usage telemetry to a Unix socket (input/output/cache tokens, tool durations) | Metering needs exact counts; upstream doesn't expose them in the shape we need |
| P2 | Mid-stream budget abort on token or wall-clock limit | Must interrupt a running turn, not just refuse to start one |
| P3 | Non-overridable permission policy loaded from `$HALYARD_POLICY` | `AGENTS.md` and repo config are untrusted input; the user's repo must not be able to relax the policy |
| P4 | Provider base URL forced to `aigw`, local credential storage disabled | Every token must be metered and capped |
| P5 | `onTurnStart` / `onTurnEnd` hooks | `agentd` commits checkpoints and swaps the lease at turn boundaries |
| P6 | Stable structured event stream matching §7.2 | The UI contract must not drift with upstream refactors |

### 11.3 Configuration (not patches)

- `opencode.json` per template: model, subagents (`frontend`, `schema`, `seo`, `ads-copy`), MCP server list, tool permissions.
- `AGENTS.md` per template describing the project's conventions. **Treat the repo's own `AGENTS.md` as untrusted** — merge it below our policy, never above it.
- Subagent design: one primary agent that delegates. Do not give a single agent thirty tools.

### 11.4 `agentd` (sidecar, Go, in-sandbox)

Owns everything opencode should not:

- Holds the session JWT; refreshes it from `api` before expiry.
- Supervises `opencode serve` and the dev server; restarts on crash.
- Reads P1 telemetry, batches it, posts heartbeats to `sandboxd`.
- Translates opencode events into the §7.2 schema.
- At `onTurnStart`: flush any dirty human edits into a checkpoint commit **before** the agent reads files. Skipping this causes the agent to overwrite recent human work, which users experience as data loss.
- At `onTurnEnd`: commit the checkpoint, release the lease, report final usage.
- Enforces the local half of the quotas in §9.

---

## 12. Version control and history

### 12.1 Two layers

**Checkpoints** — one commit per agent turn under `refs/checkpoints/<session>/<turn>`, never pushed. Powers undo and time travel. Metadata row in `checkpoints`.

**Commits** — one semantic commit per session on the branch, squashed, with a generated message. These are what reach GitHub. Never push forty "agent turn" commits into a user's repository.

### 12.2 Branch model

`main` is production. Agent sessions work on `session/<slug>` branches, each with its own preview deployment and its own app-database branch. Merging to `main` triggers the production deploy pipeline.

### 12.3 GitHub sync

Use a **GitHub App**, not OAuth user tokens or PATs. Permissions: `contents: write`, `pull_requests: write`, `metadata: read`. Add `workflows: write` only if we author CI files. Installation tokens expire in an hour; `workers` mints one per push.

- **Outbound:** agent commits land internally, then queue. On GitHub outage, commits queue and the UI shows the pending count. Never block the builder on GitHub.
- **Inbound:** webhook (verify the signature) → fetch into the internal mirror → rebase any in-flight session branch → on conflict, hand resolution to the agent, escalating to the user if it can't finish → rebuild the preview.
- Prefer the git wire protocol for moving files; use REST only for pull requests, checks and metadata, to stay clear of per-installation rate limits.
- Handle the uninstall webhook or dead installations accumulate.

### 12.4 Restore — forward-only

```
tree=$(git rev-parse <target_sha>^{tree})
new=$(git commit-tree $tree -p $(git rev-parse HEAD) -m "Restore version from <date>")
git update-ref refs/heads/<branch> $new
```

No `reset --hard`, no force push, ever. Partial restore is the same operation with a tree assembled from selected paths. After a restore: reset the sandbox working tree, reinstall if the lockfile hash changed, rebuild the preview.

**Distinguish these two in the UI and never merge them:**

| Action | Mechanism | Speed | Code changes |
|---|---|---|---|
| Roll back the live site | Repoint the edge to a stored artifact | Instant | No |
| Restore this version | Forward revert commit + rebuild | Minutes | Yes |

### 12.5 Migrations are the exception to forward-only

A migration that dropped a column cannot be undone by adding a commit. Therefore: capability and agent migrations must be additive (new nullable columns, new tables, no drops), and any destructive migration requires an approved `proposals` row.

---

## 13. Capability system

### 13.1 Install pipeline

1. Resolve manifest at the pinned version; check `framework` compatibility against the project.
2. Provision the backing resource (`sandboxd`/`workers` depending on kind). May return `needs_action` — e.g. Stripe hosted onboarding not yet completed.
3. Agent applies the module: write files (respecting `on_conflict`), install dependencies, generate and apply migrations against the preview branch.
4. Inject env keys into the secret store, **development scope with `dev_variant` applied** (test-mode keys only).
5. Run `smoke_check` against the preview. Fail → roll the whole install back as one unit.
6. Write a `timeline_events` row and a `project_capabilities` row.

### 13.2 v1 catalogue

| id | Credential owner | Provisioning |
|---|---|---|
| `auth` | none | Tables in the app's own database |
| `payments` | builder | Stripe Connect account + hosted onboarding |
| `email` | builder | Sending domain, SPF/DKIM records to add |
| `uploads` | none | R2 bucket per project + signed URLs |
| `slack` | builder | OAuth to the user's own workspace |
| `ai` | none | Gateway key; **phase 7** |

**Builder-time vs runtime credentials.** Builder-time means the project owner connects their own account — one credential in our secret store. Runtime means each end-user of the generated app connects their own account, which needs a token table in the app's database, callback routes, per-app encryption and refresh handling. **Ship builder-time only in v1.**

### 13.3 Webhook relay

Third-party webhooks cannot target a sandbox tunnel whose URL changes each session. `api` exposes a stable `/relay/:project/:capability` endpoint that forwards to whichever preview is currently live and queues (with replay) when none is. Users hit this on their first test checkout; without it the product looks broken.

---

## 14. Deployment and domains

### 14.1 Build and publish

1. Build in the sandbox: install, typecheck, test, `next build` with the OpenNext adapter.
2. Upload the artifact to R2 under an immutable key. Write a `deployments` row.
3. **The control plane promotes, the sandbox never deploys.** `workers` deploys the artifact to a Cloudflare Worker (one per project, versioned).
4. Bind production secrets at promote time. Dev secrets never reach production.
5. Health check the new version before it takes traffic; failure leaves the previous version live.
6. `is_live` moves atomically; rollback flips it back with no rebuild.

### 14.2 Domains

- Registration through a registrar API; connection for domains the user already owns.
- Use **Cloudflare for SaaS custom hostnames** for verification and automatic TLS rather than building ACME plumbing.
- Poll DNS with backoff; surface the exact records to add, copyable, with the current observed value alongside the expected one.
- Apex + `www` handled as a pair with a redirect, because every user wants this and nobody wants to configure it.

### 14.3 Generated apps on Workers

- Postgres access from a Worker needs Hyperdrive or an HTTP/WebSocket driver. The template must be built for whichever we choose — this is a template-level decision, not per-project.
- Scheduled work in generated apps uses Cloudflare cron triggers, declared in the template.
- **Preview URLs must not be publicly readable.** Gate them behind a signed cookie issued by `api`. An unlisted URL is not access control, and users will build things they don't want indexed or seen.

---

## 15. Growth surfaces

### 15.1 Ads

- Google Ads: OAuth per org, developer token, accounts linked under our manager account. Query metrics with GAQL on a schedule into the analytics store.
- Meta: Marketing API with `ads_management` and `business_management`, Business Verification completed.
- **Every mutation goes through `proposals`.** The agent writes the proposal; a user with `owner`/`admin` approves; `workers` executes with idempotent batched writes; the local mirror updates from the API response, not from the proposal.
- Conversion tracking: the deployed app posts events to an edge collector, which forwards to Google Enhanced Conversions and Meta CAPI. Declare this in the template so it works from day one.
- Spend caps in the policy vault are checked at execution time, not just at proposal time.

### 15.2 SEO

Loop: crawl (Playwright + Lighthouse in a Modal function) → score issues into `seo_issues` with an impact number → agent fixes the fixable ones as one reviewable version → publish and request indexing (sitemap + Indexing API) → track positions from Search Console and rank checks → next scheduled crawl.

Issues that need a human judgement call (e.g. two pages competing for one query) get `status = 'needs_user'` and are never auto-fixed.

---

## 16. Credits, metering and the AI gateway

### 16.1 Non-negotiables

1. **A ledger, not a counter.** Never `update balance = balance - x`. Append to `ledger_entries` with an idempotency key; `org_balances` is a rebuildable materialisation.
2. **Units and prices are separate.** Emitters write units into `usage_events`. Only the rating worker converts units to credits, stamping `price_book_version` on every charge so a support question about a past invoice is answerable exactly.
3. **Reserve then settle.** Pre-flight check → `holds` row at the p75 estimated cost → work runs with heartbeat metering → settle at actual, release the remainder. Holds have a TTL so a crashed sandbox never freezes credits.
4. **Overruns don't kill running work.** If actual exceeds the hold, allow a bounded negative float and block the *next* turn. Terminating a turn halfway leaves a half-edited working tree, which costs the user more than the overrun costs us.
5. **Never charge for our own failures.** Infrastructure faults auto-reverse.

### 16.2 Meters

`tokens_in`, `tokens_out`, `tokens_cache_read`, `tokens_cache_write` (four separate meters — they differ by roughly an order of magnitude in cost, and one combined "tokens" meter will misprice heavy sessions), `sandbox_seconds` by tier, `build`, `deploy`, `egress_gb`, `requests`, `db_storage_gb`, `db_compute_seconds`, `ai_gateway_tokens`, `seo_crawl_pages`, `keyword_lookup`, `rank_check`, `domain_year` (pass-through at cost, shown as its own line so it doesn't look like margin).

### 16.3 Two meters, two exhaustion behaviours

| Meter | Driven by | On exhaustion |
|---|---|---|
| Build | The user, present at the keyboard | Pause the builder. Harmless. |
| Runtime | Their app's end users | Policy ladder |

The runtime ladder, in order: warn at 80% → throttle the AI gateway at 100% with an error the app can handle → disable AI features but keep the site serving → suspend only after a grace period and repeated notification. **Never take a paying customer's production site down over a small shortfall.** Auto top-up, defaulted on with a user-set ceiling, is the real fix.

### 16.4 AI gateway (`aigw`)

Both the builder's agent traffic and (phase 7) the generated apps' AI features go through it. Responsibilities: project-scoped keys, provider proxying and failover, exact token accounting, per-project and per-session spend caps, prompt/response caching, and content-policy enforcement — the end users of our users' apps are untrusted, and their prompts land on our provider accounts under our terms.

Keep the provider and model behind config. Do not hardcode model names in application code. For current model identifiers, pricing and API behaviour, consult the provider's own documentation at build time — for Anthropic, `https://docs.claude.com/en/docs/about-claude/models` and `https://docs.claude.com/en/api/overview`.

### 16.5 Reconciliation

Nightly: compare charged credits against actual provider spend (LLM providers, Modal, Neon, Cloudflare, SEO data vendors). Track realised margin per org. Alert on negative-margin accounts — with agentic products a handful of power users can quietly run at a loss for months.

### 16.6 Denomination

Pick a scale where a normal agent turn costs somewhere in the tens of credits. If a typical turn costs 0.0031 credits, nobody can reason about their balance. Never display fractions.

---

## 17. Security model

### 17.1 Two credential classes

| Class | Examples | Where it may exist |
|---|---|---|
| Platform | Google Ads refresh token, GitHub installation token, registrar key, Stripe platform key | Control plane only. Reached from the sandbox exclusively via `mcp`. |
| App runtime | The app's own database URL, its Stripe key | May be in a sandbox, **but only ever the scoped development variant**: dev DB branch, test-mode Stripe, sandboxed integration accounts |

Live credentials never exist inside a development sandbox. A prompt injection that exfiltrates a sandbox environment then burns a test key — an incident report, not a disaster.

### 17.2 Threat list with required mitigation

| Threat | Mitigation |
|---|---|
| Prompt injection via scraped content or the repo's own `AGENTS.md` | Policy enforced outside the model (P3, egress allowlist, server-side MCP authorisation). Never trust the model to refuse. |
| Agent commits a live secret | `gitd` pre-receive scanner |
| Sandbox reaches another tenant | Per-session JWT scoped to one project; `gitd` and `mcp` derive scope from the token, never from arguments; no shared network |
| Credential theft from the secret store | Envelope encryption, per-tenant DEK under a KMS key; secret values never returned by any read API |
| Agent spends money | Every spending action requires an approved `proposals` row; the agent cannot approve |
| Crypto mining / token burn in sandboxes | Egress allowlist, CPU quota, hard session caps, repetition detection, per-org daily ceiling independent of balance |
| **Phishing and malware sites built on the platform** | See §19 — this is the single most underrated risk in this product category |
| Compromised generated app leaks user data | Templates ship secure defaults; secrets scoped `server` never reach the client bundle |
| Replay / duplicate charges | `Idempotency-Key` on every mutation; unique idempotency key on every ledger entry |

### 17.3 Observability and SLOs

OpenTelemetry traces spanning console → `api` → `sandboxd` → sandbox → tool call, with `org_id`, `project_id`, `session_id` and `turn` as span attributes throughout. Sentry for both the console and services. Structured JSON logs with no secret values and no user content above debug level.

Tracked SLOs:

| SLO | Target |
|---|---|
| Time to first preview, new project from template | p50 < 10s, p95 < 25s |
| Time to first token of an agent turn | p95 < 3s |
| Preview rebuild after an edit | p95 < 8s |
| Publish to live | p95 < 90s |
| Generated app availability | 99.9% monthly |
| Console API availability | 99.9% monthly |

---

## 18. Frontend specification

Route structure (App Router), matching the approved mockup:

```
/                                   → redirect to last project or /new
/new                                → template gallery + prompt box
/p/[project]                        → builder (chat + preview)
/p/[project]/files                  → tree + editor
/p/[project]/history                → timeline, restore
/p/[project]/features               → capability catalogue
/p/[project]/ship                   → versions, domains, repo, database
/p/[project]/ads                    → approvals, KPIs, campaigns
/p/[project]/search                 → SEO issues, positions, health
/settings/credits                   → balance, ledger, split, auto top-up
/settings/team                      → members, invites, roles
/settings/connections               → GitHub, Google, Meta, Slack
```

**Persistent chrome.** The credit gauge is in the top bar on every screen, showing build and runtime as separate bars with the active hold drawn as hatching. This is deliberate: users need to see burn while causing it.

**Semantic colour, three states only.** Violet = agent-owned action. Amber = waiting on you. Teal = live. Used identically for the ads approval, incomplete Stripe onboarding, and a DNS-pending domain — one learned pattern, not three.

**Copy rules.** No git vocabulary in primary surfaces: "You edited 2 files", not "commit c02b7ad". SHAs present but demoted to small monospace. An action keeps its name through the whole flow — the button that says "Publish" produces a toast that says "Published". Empty states are invitations to act. Errors say what happened and how to fix it, and never apologise.

**Streaming.** The builder subscribes to the SSE endpoint on mount, resumes with `Last-Event-ID`, and renders optimistically. Tool events append to the current turn card. Unknown event types are ignored, not fatal.

**Write lease.** When the agent holds it, the editor is read-only with a visible banner and a "take over" button that requests the lease for the end of the current turn. Never a silently rejecting editor.

**Accessibility.** Keyboard-navigable throughout, visible focus rings, `aria-live` for streaming regions and toasts, reduced-motion respected, 4.5:1 contrast minimum. Verify with axe in CI.

---

## 19. Anything missed — gaps not yet covered anywhere

These were not in the brief. Several are launch blockers, not nice-to-haves.

### 19.1 Legal and compliance (blocking)

- **Abuse: phishing and malware.** A free app builder with free hosting on your domains is a phishing factory. You will be used for this within weeks of launch. You need: content scanning on publish (brand-impersonation heuristics, credential-form detection), domain reputation monitoring, an `abuse@` contact and a documented takedown process with SLA, and a way to suspend a project's hosting in one action. Without this your apex domain gets blocklisted by browser safe-browsing services and every customer's preview URL breaks at once. **Treat this as phase 4, not phase 8.**
- Terms of service, acceptable use policy, privacy policy, DPA, subprocessor list.
- **GDPR/CCPA data subject requests** — export and delete, for both your users and their end users. The second is harder: their app's user data lives in a database you provisioned.
- **State clearly that the user owns the code and the AI output.** This is both a legal question and the single biggest trust objection you will face.
- **Code export / eject.** A user must be able to download a working repository and leave. Without it, enterprise buyers won't start. It also costs almost nothing given the git store already exists.
- Cookie consent in generated app templates, since your users' sites will be subject to it.
- PCI: Stripe Connect with hosted onboarding keeps you at SAQ-A. Confirm with Stripe; don't assume.
- Google Ads API and Meta Platform terms compliance as a tech provider; both have specific obligations for tools acting on advertiser accounts.
- Template licensing — license your templates permissively and explicitly, or users can't legally ship what you generated.
- Age/consent gating at signup.

### 19.2 Operational

- Backup and disaster recovery with stated RPO/RTO, for the control plane database, the git store and users' app databases. Test a restore before launch, not after an incident.
- Status page and incident communication. Your users' production sites depend on you; silence during an outage is what loses accounts.
- Noisy-neighbour and DDoS protection on generated apps, plus per-app rate limits.
- Log retention policy and PII scrubbing.
- Secret rotation runbook for platform credentials.
- Provider failover when an LLM provider degrades — the gateway makes this possible, but the policy has to be written.
- Your own cloud cost alerting, separate from customer credit accounting.
- Data residency, if you intend to sell in the EU.
- Feature flags, so a half-built capability can ship dark.
- Control plane migration safety — a review gate on any migration touching `ledger_entries`.

### 19.3 Product gaps

- **Onboarding.** Nothing in the design covers the first ninety seconds. This is where most signups are lost.
- Team collaboration beyond roles: comments on a version, notifications, who's-viewing.
- Support tooling: an internal admin console with impersonation, fully audited and consent-gated.
- Email deliverability for generated apps — per-domain SPF/DKIM setup, and a warning that shared sending reputations get poisoned.
- Analytics for the generated apps. Users will ask on day one, and it needs no new infrastructure given the edge collector.
- In-app usage and cost dashboards for the generated apps' own AI features (phase 7).
- Internationalisation of the console, and of generated app templates.
- Your own marketing site's SEO — you're selling an SEO tool.
- A public changelog and roadmap.
- Sandbox session sharing for pair debugging or support.
- Template update flow UI — the merge mechanism is specified but the review experience isn't.
- What happens on downgrade or non-payment: how long do you keep a suspended project's data, and how do you tell them before deleting it?

### 19.4 Technical gaps

- Search inside the codebase (the agent needs it; ripgrep in the sandbox is enough for v1).
- Language server support. Roughly doubles sandbox memory and needs a second protocol; **defer to phase 8.** Model-driven completion plus syntax highlighting gets most of the perceived quality.
- Binary and large file handling in the editor (size cap, signed download).
- Concurrent sessions on one project by two users — v1 should refuse the second with a clear message rather than corrupt the tree.
- Cold-start behaviour for generated apps on Workers with a database connection.
- Timezone and locale handling in ads reporting, where platform data is timezone-bound per account.

---

## 20. Delivery phases

Each phase ends deployed and demonstrable. Do not start a phase before the previous one's acceptance criteria pass.

### Phase 0 — Foundations
Monorepo, CI, `packages/schema` with generation into all three languages, control plane Postgres with migrations and RLS, auth and orgs, console shell with the design tokens from the mockup, one deployed environment end to end.
**Accept:** a user can sign up, create an org, sign in, and see an empty project list on a real deployed URL.

### Phase 1 — Sandbox and agent loop
`sandboxd` with Modal, sandbox image, `agentd`, opencode patches P1/P4/P5/P6, SSE event stream, chat UI, live preview tunnel, `gitd` with checkpoint commits.
**Accept:** a user prompts, the agent edits files, the preview updates live, and every turn produces a checkpoint. Time to first token p95 < 3s.

### Phase 2 — Templates, files, history
Template registry with pre-baked images, project creation from template, file tree and blob API served from git, CodeMirror editor, write lease, forward-only restore, timeline.
**Accept:** create from template in <10s p50; browse files with no sandbox running; edit as a human without racing the agent; restore any version and see it become a new version.

### Phase 3 — Ship
Build, immutable artifacts in R2, Worker deploy, health-gated promotion, rollback, custom domains via Cloudflare for SaaS, signed preview URLs.
**Accept:** publish to a real custom domain with working TLS; roll back in under 5 seconds; a failed build never takes production down.

### Phase 4 — Credits and abuse
Ledger with holds, reserve/settle on every turn, rating worker with a versioned price book, meters for tokens/sandbox/build/deploy, credit UI, Stripe checkout for purchases, auto top-up, **plus the abuse and takedown tooling from §19.1**.
**Accept:** a turn reserves, settles and shows an accurate cost; concurrent turns cannot overdraw; a project can be suspended within one minute of an abuse report.

### Phase 5 — Capabilities and GitHub
Manifest schema and installer, `auth` and `payments` capabilities, app database provisioning with preview branching, secret store with dev/prod variants, webhook relay, GitHub App with two-way sync and conflict rebase.
**Accept:** install auth and Stripe into a fresh project and complete a test checkout end to end; push from GitHub mid-session and have the session rebase cleanly.

### Phase 6 — Growth
Google Ads and Meta connectors, metrics sync, proposal and approval flow with spend caps, SEO crawl and audit, agent-applied SEO fixes, Search Console integration, rank tracking.
**Accept:** the agent drafts a campaign, a user approves it, and it appears live in Google Ads with the mirror in sync; six SEO issues get fixed in one reviewable version.

### Phase 7 — AI features for generated apps
`aigw` opened to generated apps, project-scoped keys, `ai` capability with a typed client and streaming route handler, pgvector in the app database, per-project and per-end-user caps, content policy at the gateway.
**Accept:** a generated app makes a metered AI call; exceeding the project cap throttles that app without taking it offline.

### Phase 8 — Polish
Language server, comments and notifications, admin console, analytics for generated apps, i18n, template update review UI.

---

## 21. Decisions the human must make before phase 1

1. Container host for the backend: Cloudflare Containers or an external host behind Cloudflare (§3.5).
2. All-Python backend or the Go/Python split (§5.1) — depends on team size.
3. Neon for app databases, or schema-per-project on shared Postgres (§3.3).
4. Auth library for generated apps, pinned version (§5.2).
5. Registrar for domain sales.
6. Credit denomination and the retail price per credit; target gross margin.
7. Whether the free tier permits publishing to a custom domain — this is the main abuse lever.
8. Revenue share on Stripe Connect (`application_fee_amount`): yes or no.
9. Whether the console targets non-technical users primarily. The mockup assumes semi-technical; a purely non-technical audience needs warmer visual design and less code exposure.

---

## 22. Facts to verify before depending on them

My information has a cutoff and these all move. Confirm each against current vendor documentation and record findings in `docs/verified.md` before building on them.

1. `@opennextjs/cloudflare` — current Next.js version support, unsupported features, ISR/caching behaviour.
2. Cloudflare Containers — memory and CPU limits, max request duration, support for long-lived SSE connections, pricing.
3. Cloudflare for SaaS — custom hostname limits per zone, TLS issuance latency, apex support.
4. Cloudflare Hyperdrive — supported Postgres providers, connection limits, latency.
5. Modal — Sandbox API surface, filesystem snapshot semantics, tunnel URL stability, volume performance, per-account concurrency limits, whether any non-Python client exists now.
6. `opencode` — current server API, config schema, MCP transport support, plugin/hook surface, and whether P1/P2/P3/P5 can now be done without patching. Pin a tag.
7. Neon — API for project and branch creation, branch limits per project, autoscaling behaviour, pricing at thousands of projects.
8. GitHub Apps — current fine-grained permission names, installation token TTL, rate limits per installation.
9. Google Ads API — developer token application process and current wait time, basic vs standard access limits, manager account linking requirements.
10. Meta Marketing API — current permission names, App Review requirements and timeline, Business Verification steps.
11. Stripe Connect — account type recommended for this use case, onboarding requirements, current SAQ-A applicability.
12. Google Indexing API — current eligibility rules (it has historically been restricted to certain content types).
13. LLM provider model identifiers, pricing and cache semantics — do not hardcode.
14. Auth.js / Better Auth — current status, adapter support for Drizzle, and behaviour on Cloudflare Workers.
15. Whether Cloudflare Workers can host the generated Next.js apps with the database driver chosen in decision 3.

---

## 23. Definition of done for v1

A user who has never seen the product can, in one session and without help: sign up, describe an app, watch it get built, edit a file themselves, add sign-in and payments, complete a test purchase, buy a domain, publish to it, see what it cost in credits, and connect their GitHub so they can leave with their code if they want to.

Everything else is optimisation.
