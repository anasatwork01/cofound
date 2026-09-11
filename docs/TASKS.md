# Halyard - work breakdown

Every task is sized to land in one reviewable change with tests. Task IDs are
stable and referenced from code comments, PR titles and `docs/verified.md`.

**Phase gates are hard.** SPEC §20: do not start a phase before the previous
one's acceptance criteria pass. Cross-cutting tasks (`X.*`) run continuously
and are not gated.

Status: `todo` · `blocked` · `wip` · `done`

---

## Do these now, in parallel with phase 0

External approval queues run in weeks, and phase 6 stalls without them.

| ID  | Task                                                              | Status   |
| --- | ----------------------------------------------------------------- | -------- |
| L.1 | Apply for a Google Ads API developer token (§22.9)                | todo     |
| L.2 | Start Meta App Review + Business Verification (§22.10)            | todo     |
| L.3 | Apply for a Stripe Connect platform account (§22.11)              | todo     |
| L.4 | Answer the nine SPEC §21 decisions - see `docs/open-questions.md` | todo     |
| L.5 | Commit the canonical `docs/SPEC.md` and `docs/mockup.html` (Q0)   | **done** |

---

## Phase 0 - Foundations

_Accept: a user can sign up, create an org, sign in, and see an empty project
list on a real deployed URL._

| ID   | Task                                                                                                                                                                                                                                                                                                                                                                                                                                                                     | Depends on           | Status      |
| ---- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | -------------------- | ----------- |
| 0.1  | Monorepo skeleton, toolchain pins, `make doctor` / `make verify`, opencode pinned as a submodule                                                                                                                                                                                                                                                                                                                                                                         | -                    | **done**    |
| 0.2  | CI: GitHub Actions running `make verify` per language, plus Postgres + Redis in Docker for integration tests                                                                                                                                                                                                                                                                                                                                                             | 0.1                  | **done**    |
| 0.3  | `packages/schema`: JSON Schema + OpenAPI sources, codegen to TS/Go/Python, `make gen`, CI drift check                                                                                                                                                                                                                                                                                                                                                                    | 0.1                  | **done**    |
| 0.4  | Go service chassis: config, slog JSON logging, OTel tracing, health/readiness, graceful shutdown, chi router, error envelope                                                                                                                                                                                                                                                                                                                                             | 0.2, §21.2           | **done**    |
| 0.5  | Python service chassis: FastAPI app factory, settings, logging, OTel, health endpoints                                                                                                                                                                                                                                                                                                                                                                                   | 0.2, §21.2           | **done**    |
| 0.6  | Control plane schema: goose migrations for all of SPEC §6, pgx pool, RLS policies + per-transaction `app.org_id`, integration test proving cross-tenant denial. Write the policy with `nullif(current_setting('app.org_id', true), '')::uuid` — see `docs/verified.md`.                                                                                                                                                                                                  | 0.4                  | **done**    |
| 0.7  | Auth: email magic link + Google OAuth, rotating httpOnly `SameSite=Lax` sessions                                                                                                                                                                                                                                                                                                                                                                                         | 0.6                  | **done**    |
| 0.8  | Tenancy middleware: resolve `(user_id, org_id, role)` once, role matrix in one place, never inline                                                                                                                                                                                                                                                                                                                                                                       | 0.7                  | **done**    |
| 0.9  | Org + project CRUD, invites, org switching, `audit_log` writer covering SPEC §8's list                                                                                                                                                                                                                                                                                                                                                                                   | 0.8                  | **done**    |
| 0.10 | `Idempotency-Key` middleware, structured error contract, edge rate limiting                                                                                                                                                                                                                                                                                                                                                                                              | 0.4                  | **done**    |
| 0.11 | Console shell: Next.js App Router, Tailwind token layer extracted from `docs/mockup.html`, SPEC §18 route skeleton, TanStack Query, Zustand, top-bar chrome                                                                                                                                                                                                                                                                                                              | 0.3, L.5, §21.9      | **done**    |
| 0.12 | First deployed environment: console on Workers via OpenNext (**done**: staging + production wrangler envs, gated deploy workflow, verified OpenNext build); service images **built and published to GHCR** on every merge (**done**, host-agnostic); one Go service on the chosen container host (**blocked on §21.1** — the decision, plus an account). **Hyperdrive is not needed here** - it is a Workers binding and the console never touches Postgres (§22 item 4) | 0.11, §21.1, §22.1-2 | blocked\*\* |
| 0.13 | Sentry for console and services; axe accessibility checks in CI                                                                                                                                                                                                                                                                                                                                                                                                          | 0.2, 0.11            | todo        |

\*\* **0.12 is half-shipped and blocked on one decision.** The console side is
built: staging and production wrangler environments, a deploy workflow that is
inert until `DEPLOY_CONSOLE_ENABLED` is set, and the OpenNext build verified end
to end. The Go-service side needs SPEC §21 decision 1, which §22 item 2 has now
made decidable - see the brief in `docs/open-questions.md`. The container image
is built, host-agnostic and CI-tested, so that half is waiting on the decision
rather than on work. Nothing has been deployed: no Cloudflare account, token or
DNS was used. `infra/cloudflare/RUNBOOK.md` is the executable half.

\* **0.11's token values are now real.** They were provisional while
`docs/mockup.html` did not exist; task L.5 authored it and
`packages/ui/src/tokens/palette.css` is extracted from it. The swap was the
one-file change 0.11 promised, and the contrast test checked it rather than
trusting it. See `packages/ui/DESIGN.md`.

---

## Phase 1 - Sandbox and agent loop

_Accept: a user prompts, the agent edits files, the preview updates live, every
turn produces a checkpoint, time-to-first-token p95 < 3s._

| ID   | Task                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 | Depends on    | Status |
| ---- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------- | ------ |
| 1.1  | Verify §22 items 5 and 6 (Modal API + snapshots + tunnels; opencode hook surface). Decide which of P1-P6 are still needed                                                                                                                                                                                                                                                                                                                                                                            | 0.1           | done   |
| 1.2  | Sandbox base image: Dockerfile + Modal image definition, Node/pnpm, git, ripgrep, built opencode, `agentd`                                                                                                                                                                                                                                                                                                                                                                                           | 1.1           | todo   |
| 1.3  | `sandboxd`: define `sandboxd.openapi.yaml` first, then create/**wake**/stop. **No resume primitive exists** - wake is create-new + `mount_image` of the project's directory snapshot. **Not a Volume**: `volumes=` is create-time only, so Volumes and 1.5's warm pool are mutually exclusive                                                                                                                                                                                                        | 1.2, 0.3, 0.5 | todo   |
| 1.4  | Sandbox reconcile-on-wake: `git fetch` + reset to branch head, reinstall when the lockfile hash changed. Wider than a git sync - only files survive a snapshot, so `agentd` cold-starts, no port is bound, and the new tunnel URL must be re-registered                                                                                                                                                                                                                                              | 1.3, 1.11     | todo   |
| 1.5  | Warm pool (2-5 per popular image, scaled on rolling p50), Redis registry, quota enforcement. **Modal has no pool primitive**, and a pooled sandbox with `agentd` merely idling is not active by Modal's definition so it gets reaped - sandboxd reaps and snapshots itself. Size against ~$0.24/hr per idle sandbox (§19)                                                                                                                                                                            | 1.3, 1.19     | todo   |
| 1.6  | Preview tunnels + Workers KV routing for `<branch>.<project>.preview.<domain>`                                                                                                                                                                                                                                                                                                                                                                                                                       | 1.3           | todo   |
| 1.7  | Sandbox egress allowlist, deny-by-default; per-session CPU/memory/token/wall-clock quotas                                                                                                                                                                                                                                                                                                                                                                                                            | 1.3           | todo   |
| 1.8  | `agentd`: supervise `opencode serve` and the dev server, restart on crash, hold and refresh the session JWT                                                                                                                                                                                                                                                                                                                                                                                          | 1.2           | todo   |
| 1.9  | opencode provider routing (was P4: `provider.<id>.options.baseURL` config, local credentials disabled) and usage telemetry (was P1: read `message.updated` off the SSE stream, **not** a Unix socket). No patches - see 1.1                                                                                                                                                                                                                                                                          | 1.1           | todo   |
| 1.10 | opencode turn boundaries (was P5: blocking `chat.message` hook for turn start; `agentd` serialises on `session.idle` for turn end), event adapter to SPEC §7.2 (was P6), and the policy env vars (was P3: `OPENCODE_PERMISSION` + `OPENCODE_DISABLE_PROJECT_CONFIG`, **not** `$HALYARD_POLICY`). No patches - see 1.1                                                                                                                                                                                | 1.1, 0.3      | todo   |
| 1.11 | `gitd` v1: bare repo per project, session-JWT auth scoped to one project, `git-upload-pack` / `git-receive-pack` proxy                                                                                                                                                                                                                                                                                                                                                                               | 0.4           | todo   |
| 1.12 | `gitd` checkpoint commits under `refs/checkpoints/<session>/<turn>` + `checkpoints` rows                                                                                                                                                                                                                                                                                                                                                                                                             | 1.11, 0.6     | todo   |
| 1.13 | `aigw` v1: provider proxy behind config, exact four-way token accounting, per-session spend caps (no caching yet). **The upstream fields are pre-netted**: billable input is `input + cache.read + cache.write`, and `tokens_out` must carry `output + reasoning`. Assert it in tests, not comments                                                                                                                                                                                                  | 0.4, §22.13   | todo   |
| 1.14 | SSE gateway in `api`: Redis pub/sub fan-out, monotonic event ids, `Last-Event-ID` resume                                                                                                                                                                                                                                                                                                                                                                                                             | 0.4, 0.3      | todo   |
| 1.15 | Turn lifecycle: `POST /turns`, abort, `sessions`/`turns` rows, `timeline_events`                                                                                                                                                                                                                                                                                                                                                                                                                     | 1.14, 1.12    | todo   |
| 1.16 | Builder chat UI: streaming turn cards, tool events appended live, unknown event types ignored, preview pane                                                                                                                                                                                                                                                                                                                                                                                          | 0.11, 1.14    | todo   |
| 1.17 | OTel spans console → `api` → `sandboxd` → sandbox → tool call, carrying `org_id`/`project_id`/`session_id`/`turn`                                                                                                                                                                                                                                                                                                                                                                                    | 0.4, 0.5      | todo   |
| 1.18 | E2E test + the two phase-1 SLOs instrumented (time to first token, preview rebuild)                                                                                                                                                                                                                                                                                                                                                                                                                  | 1.16          | todo   |
| 1.19 | Modal resume-latency benchmark against a real workspace: p50/p95/p99 for snapshot create and restore. Harness has **landed** (`services/sandboxd/src/halyard_sandboxd/bench/`); only the paid run remains. **Run it before 1.3 and 1.5** - it needs nothing from them, and its numbers are what should settle 1.3's snapshot-vs-Volume choice and 1.5's pool sizing. Replaces §22 item 5's unmeasured latency row. Measures **Modal's half only** (~0.5-1s of §17's 10s); the reconcile step is ours | 1.1           | todo   |

---

## Phase 2 - Templates, files, history

_Accept: create from template in <10s p50 - the target task 1.19 confirms or
revises, since Modal's snapshot/restore latency is unpublished; browse files
with no sandbox running; edit as a human without racing the agent; restore any version and see
it become a new version._

| ID   | Task                                                                                                                                     | Depends on | Status |
| ---- | ---------------------------------------------------------------------------------------------------------------------------------------- | ---------- | ------ |
| 2.1  | Template registry: `templates/` layout, versioning, `template_versions` rows, licence file per template                                  | 1.2, §21.4 | todo   |
| 2.2  | Pre-baked image build pipeline, one image per template version, wired to the warm pool                                                   | 2.1, 1.5   | todo   |
| 2.3  | Project creation from template, hitting the p50 < 10s SLO                                                                                | 2.2, 0.9   | todo   |
| 2.4  | Project creation from a bare prompt (template selection by the agent)                                                                    | 2.3        | todo   |
| 2.5  | `gitd` read API: `tree`, `blob` (1MB cap, larger returns a signed R2 URL), structured diff hunks                                         | 1.11       | todo   |
| 2.6  | Content-addressed blob cache in R2/Redis forever by sha; diff cache by `(base, head)`                                                    | 2.5        | todo   |
| 2.7  | File tree + CodeMirror 6 editor; binary and large-file handling with a size cap and signed download                                      | 2.5, 0.11  | todo   |
| 2.8  | Write lease: Redis TTL behind an interface, `lease.changed` events, read-only banner and "take over" - never a silently rejecting editor | 1.14       | todo   |
| 2.9  | Human edit path: `PUT /file` → sandbox overlay, dirty-overlay merge in `api`, committed files always read from git                       | 2.5, 2.8   | todo   |
| 2.10 | Flush dirty human edits into a checkpoint at `onTurnStart`, **before** the agent reads files                                             | 1.10, 2.9  | todo   |
| 2.11 | Forward-only restore via `commit-tree`; partial restore from selected paths; sandbox reset + conditional reinstall                       | 1.12, 1.4  | todo   |
| 2.12 | Timeline UI + restore UX; "roll back the live site" and "restore this version" presented as distinct actions                             | 2.11, 0.11 | todo   |
| 2.13 | `gitd` pre-receive policy: secret patterns, banned paths, >10MB files, 50MB push cap, author match, structured renderable errors         | 1.11       | todo   |
| 2.14 | Refuse a second concurrent session on one project with a clear message                                                                   | 2.8        | todo   |
| 2.15 | Nightly `git gc` + packfile backup to R2                                                                                                 | 1.11       | todo   |
| 2.16 | Codebase search for the agent (ripgrep in the sandbox)                                                                                   | 1.2        | todo   |
| 2.17 | Template update merge mechanism (review UI is 8.6)                                                                                       | 2.1        | todo   |

---

## Phase 3 - Ship

_Accept: publish to a real custom domain with working TLS; roll back in under
5 seconds; a failed build never takes production down._

| ID   | Task                                                                                                                     | Depends on           | Status  |
| ---- | ------------------------------------------------------------------------------------------------------------------------ | -------------------- | ------- |
| 3.1  | Build in the sandbox: install, typecheck, test, `next build` with the OpenNext adapter                                   | 1.2, §22.1           | todo    |
| 3.2  | Immutable artifacts in R2 under content-addressed keys; `deployments` rows and version numbering                         | 3.1                  | todo    |
| 3.3  | Worker deploy from the control plane, one versioned Worker per project. **The sandbox never deploys.**                   | 3.2                  | todo    |
| 3.4  | Bind production secrets at promote time; dev secrets must never reach production                                         | 3.3, 5.3             | todo    |
| 3.5  | Health-gated promotion, atomic `is_live`, rollback by repointing the edge with no rebuild (<5s)                          | 3.3                  | todo    |
| 3.6  | Signed-cookie gate on preview URLs - an unlisted URL is not access control                                               | 1.6                  | todo    |
| 3.7  | Domain connection flow for domains the user already owns; DNS records surfaced copyably with observed vs expected values | 0.9                  | todo    |
| 3.8  | Domain registration through a registrar API                                                                              | 3.7, §21.5           | todo    |
| 3.9  | Cloudflare for SaaS custom hostnames + automatic TLS; apex and `www` handled as a pair with a redirect                   | 3.7, §22.3           | todo    |
| 3.10 | DNS polling with backoff, `dns_status`/`tls_status` surfaced                                                             | 3.9                  | todo    |
| 3.11 | Ship screen: versions, domains, repo, database                                                                           | 3.5, 0.11            | todo    |
| 3.12 | Generated-app database driver in the template (Hyperdrive or HTTP/WebSocket) + cold-start behaviour                      | §21.3, §22.4, §22.15 | blocked |
| 3.13 | Cloudflare cron triggers declared in the template                                                                        | 2.1                  | todo    |
| 3.14 | Edge analytics collector (Queues + Worker) declared in the template                                                      | 2.1                  | todo    |
| 3.15 | Per-app rate limits and noisy-neighbour / DDoS protection on generated apps                                              | 3.3                  | todo    |

---

## Phase 4 - Credits and abuse

_Accept: a turn reserves, settles and shows an accurate cost; concurrent turns
cannot overdraw; a project can be suspended within one minute of an abuse
report._

| ID   | Task                                                                                                                                                                                                                                                                                                                                                      | Depends on      | Status  |
| ---- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------- | ------- |
| 4.1  | Meter definitions in `packages/schema` (four separate token meters, never one combined) and a versioned price book                                                                                                                                                                                                                                        | 0.3, §21.6      | blocked |
| 4.2  | `usage_events` ingest behind an interface (partitioned Postgres now, ClickHouse later); idempotent emitters keyed by `(source, event_key)`                                                                                                                                                                                                                | 0.6             | todo    |
| 4.3  | Rating worker: units → credits, stamping `price_book_version` on every charge                                                                                                                                                                                                                                                                             | 4.1, 4.2        | todo    |
| 4.4  | Ledger + grants + rebuildable `org_balances`; **never** `update balance = balance - x`; review gate on any migration touching `ledger_entries`                                                                                                                                                                                                            | 4.3             | todo    |
| 4.5  | Holds: reserve at p75 estimate, TTL sweeper so a crashed sandbox never freezes credits, settle at actual, release the remainder                                                                                                                                                                                                                           | 4.4             | todo    |
| 4.6  | Wire reserve/settle into the turn lifecycle; mid-stream budget abort via `POST /api/session/{id}/interrupt` (**no patch P2** - struck by 1.1; that it stops a turn mid-stream rather than after the current step is still a source reading, so assert it); bounded negative float so an overrun blocks the _next_ turn instead of killing the running one | 4.5, 1.10, 1.15 | todo    |
| 4.7  | Runtime exhaustion ladder: warn at 80% → throttle the gateway at 100% → disable AI features but keep serving → suspend only after a grace period and repeated notice                                                                                                                                                                                      | 4.4             | todo    |
| 4.8  | Auto-reversal of charges caused by our own infrastructure faults                                                                                                                                                                                                                                                                                          | 4.4             | todo    |
| 4.9  | Credit UI: build and runtime as separate bars with the active hold hatched, in the top bar on every screen                                                                                                                                                                                                                                                | 4.5, 0.11       | todo    |
| 4.10 | Ledger view, spend split, Stripe checkout for credit purchases, auto top-up defaulted on with a user-set ceiling                                                                                                                                                                                                                                          | 4.9             | todo    |
| 4.11 | Per-org daily spend ceiling independent of balance; repetition and mining detection in sandboxes                                                                                                                                                                                                                                                          | 4.4, 1.7        | todo    |
| 4.12 | Publish-time content scanning: brand-impersonation heuristics, credential-form detection                                                                                                                                                                                                                                                                  | 3.5             | todo    |
| 4.13 | One-action project suspension reaching the edge in under a minute                                                                                                                                                                                                                                                                                         | 3.5             | todo    |
| 4.14 | `abuse@` intake, documented takedown process with an SLA, domain reputation and safe-browsing monitoring                                                                                                                                                                                                                                                  | 4.13            | todo    |
| 4.15 | Nightly reconciliation against actual provider spend; realised margin per org; alerts on negative-margin accounts                                                                                                                                                                                                                                         | 4.3             | todo    |

---

## Phase 5 - Capabilities and GitHub

_Accept: install auth and Stripe into a fresh project and complete a test
checkout end to end; push from GitHub mid-session and have the session rebase
cleanly._

| ID   | Task                                                                                                                                                            | Depends on      | Status  |
| ---- | --------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------- | ------- |
| 5.1  | `capability-manifest.schema.json` + `capabilities` / `capability_versions` tables + framework range checks                                                      | 0.3, 0.6        | todo    |
| 5.2  | Installer engine: idempotent apply, `on_conflict` handling, dependency install, `smoke_check`, all-or-nothing rollback                                          | 5.1             | todo    |
| 5.3  | Uninstall: files and env removed, data tables kept with a warning                                                                                               | 5.2             | todo    |
| 5.4  | Secret store: envelope encryption, per-tenant DEK under a KMS key, `development`/`production` scopes, **no read API returns a value**                           | 0.6             | todo    |
| 5.5  | App database provisioning + a branch per preview                                                                                                                | §21.3, §22.7    | blocked |
| 5.6  | Additive-migration enforcement; destructive migrations require an approved proposal                                                                             | 5.5, 5.11       | todo    |
| 5.7  | `mcp` service: the SPEC §7.3 tool set, scope derived from the token and never from arguments, no tool ever returns a secret value                               | 0.4, 5.4        | todo    |
| 5.8  | `proposals` engine + approve/decline endpoints; `owner`/`admin` only; the agent cannot approve its own proposal                                                 | 0.8, 5.7        | todo    |
| 5.9  | `auth` capability                                                                                                                                               | 5.2, 5.5, §21.4 | blocked |
| 5.10 | `payments` capability: Stripe Connect + hosted onboarding + `needs_action` state                                                                                | 5.2, §22.11     | blocked |
| 5.11 | Webhook relay `/relay/:project/:capability`, forwarding to the live preview and queueing with replay when none is up                                            | 5.2             | todo    |
| 5.12 | `email` capability: sending domain, SPF/DKIM records to add, deliverability warning about shared reputation                                                     | 5.2             | todo    |
| 5.13 | `uploads` capability: R2 bucket per project + signed URLs                                                                                                       | 5.2             | todo    |
| 5.14 | `slack` capability: OAuth into the user's own workspace                                                                                                         | 5.2             | todo    |
| 5.15 | GitHub App: install flow, `contents:write` / `pull_requests:write` / `metadata:read`, per-push installation tokens                                              | 0.9, §22.8      | blocked |
| 5.16 | Outbound sync: internal commit → queue → push; pending count in the UI; **never block the builder on GitHub**                                                   | 5.15, 1.11      | todo    |
| 5.17 | Inbound sync: verified webhook → fetch into the mirror → rebase in-flight session branches → agent conflict resolution escalating to the user → rebuild preview | 5.16            | todo    |
| 5.18 | Authority handover: `git_authority` flips to `github` on link; single owner per project at all times                                                            | 5.16            | todo    |
| 5.19 | Handle the uninstall webhook and dead installations                                                                                                             | 5.15            | todo    |
| 5.20 | Semantic squashed commits per session with generated messages - never forty "agent turn" commits                                                                | 1.12            | todo    |
| 5.21 | Code export / eject: download a working repository and leave                                                                                                    | 1.11            | todo    |

---

## Phase 6 - Growth

_Accept: the agent drafts a campaign, a user approves it, and it appears live in
Google Ads with the mirror in sync; six SEO issues get fixed in one reviewable
version._

| ID   | Task                                                                                                                          | Depends on   | Status  |
| ---- | ----------------------------------------------------------------------------------------------------------------------------- | ------------ | ------- |
| 6.1  | `connectors` table + per-org OAuth for Google, Meta, GSC, Slack, with encrypted refresh tokens                                | 5.4          | todo    |
| 6.2  | Google Ads: account linking under our manager account, GAQL metrics sync on a schedule                                        | 6.1, L.1     | blocked |
| 6.3  | Meta Marketing API metrics sync                                                                                               | 6.1, L.2     | blocked |
| 6.4  | Timezone and locale handling in ads reporting (platform data is timezone-bound per account)                                   | 6.2          | todo    |
| 6.5  | `ads.propose` → approved proposal → idempotent batched execution; mirror updated from the API response, not from the proposal | 5.8, 6.2     | todo    |
| 6.6  | Spend caps in the policy vault, checked at execution time as well as proposal time                                            | 6.5          | todo    |
| 6.7  | Ads screen: approvals, KPIs, campaigns                                                                                        | 6.5, 0.11    | todo    |
| 6.8  | Conversion tracking: edge collector → Google Enhanced Conversions + Meta CAPI, declared in the template                       | 3.14         | todo    |
| 6.9  | SEO crawl: Playwright + Lighthouse in a Modal function                                                                        | 1.3          | todo    |
| 6.10 | Issue scoring into `seo_issues` with an impact number and an `agent_fixable` flag                                             | 6.9          | todo    |
| 6.11 | Agent applies fixable issues as one reviewable version; `needs_user` issues are never auto-fixed                              | 6.10, 2.11   | todo    |
| 6.12 | Sitemap generation + indexing requests                                                                                        | 6.11, §22.12 | blocked |
| 6.13 | Search Console integration + rank tracking into `rank_snapshots`                                                              | 6.1          | todo    |
| 6.14 | Search screen: issues, positions, health                                                                                      | 6.13, 0.11   | todo    |
| 6.15 | Move usage, ad metrics and rank history to ClickHouse behind the existing interface                                           | 4.2          | todo    |

---

## Phase 7 - AI features for generated apps

_Accept: a generated app makes a metered AI call; exceeding the project cap
throttles that app without taking it offline._

| ID  | Task                                                                                                                        | Depends on | Status |
| --- | --------------------------------------------------------------------------------------------------------------------------- | ---------- | ------ |
| 7.1 | `aigw` opened to generated apps with project-scoped keys                                                                    | 1.13       | todo   |
| 7.2 | Per-project and per-end-user caps; throttling that returns a handleable error rather than an outage                         | 7.1, 4.7   | todo   |
| 7.3 | Content policy at the gateway - end users of our users' apps are untrusted, and their prompts land on our provider accounts | 7.1        | todo   |
| 7.4 | Provider failover and caching in `aigw`                                                                                     | 7.1        | todo   |
| 7.5 | `ai` capability: typed client + streaming route handler                                                                     | 5.2, 7.1   | todo   |
| 7.6 | pgvector in the app database                                                                                                | 5.5        | todo   |
| 7.7 | In-app usage and cost dashboards for the generated apps' AI features                                                        | 7.2        | todo   |

---

## Phase 8 - Polish

| ID  | Task                                                                            | Status |
| --- | ------------------------------------------------------------------------------- | ------ |
| 8.1 | Language server in the sandbox (roughly doubles sandbox memory - measure first) | todo   |
| 8.2 | Comments on a version, notifications, who's-viewing                             | todo   |
| 8.3 | Admin console with consent-gated, fully audited impersonation                   | todo   |
| 8.4 | Analytics for the generated apps, on the existing edge collector                | todo   |
| 8.5 | i18n for the console and for generated app templates                            | todo   |
| 8.6 | Template update review UI                                                       | todo   |
| 8.7 | Sandbox session sharing for pair debugging and support                          | todo   |

---

## Cross-cutting - continuous, not phase-gated

Several of these are launch blockers (SPEC §19). They are listed separately
because they do not fit a single phase, not because they can slip.

| ID   | Task                                                                                                                      | Must land by         | Status |
| ---- | ------------------------------------------------------------------------------------------------------------------------- | -------------------- | ------ |
| X.1  | Legal pack: ToS, acceptable use policy, privacy policy, DPA, subprocessor list                                            | before public launch | todo   |
| X.2  | **State clearly that the user owns the code and the AI output** - the biggest trust objection                             | before public launch | todo   |
| X.3  | Template licensing: permissive and explicit, or users cannot legally ship what we generated                               | phase 2              | todo   |
| X.4  | GDPR/CCPA data subject requests - export and delete, for our users _and_ their end users                                  | before public launch | todo   |
| X.5  | Cookie consent in generated app templates                                                                                 | phase 2              | todo   |
| X.6  | Age and consent gating at signup                                                                                          | phase 0              | todo   |
| X.7  | Confirm PCI SAQ-A applicability with Stripe rather than assuming it                                                       | phase 5              | todo   |
| X.8  | Google Ads and Meta platform terms compliance as a tech provider                                                          | phase 6              | todo   |
| X.9  | Backup and DR with stated RPO/RTO for the control plane DB, git store and app databases; **test a restore before launch** | before public launch | todo   |
| X.10 | Status page and incident communication                                                                                    | phase 3              | todo   |
| X.11 | Log retention policy and PII scrubbing; no secrets or user content above debug level                                      | phase 0              | todo   |
| X.12 | Secret rotation runbook for platform credentials                                                                          | phase 5              | todo   |
| X.13 | LLM provider failover policy (the gateway makes it possible; the policy has to be written)                                | phase 7              | todo   |
| X.14 | Our own cloud cost alerting, separate from customer credit accounting                                                     | phase 4              | todo   |
| X.15 | Feature flags, so a half-built capability can ship dark                                                                   | phase 0              | todo   |
| X.16 | SLO dashboards and alerting for all seven SPEC §17.3 targets                                                              | phase 1 onward       | todo   |
| X.17 | Onboarding: the first ninety seconds, where most signups are lost                                                         | phase 2              | todo   |
| X.18 | Downgrade and non-payment policy: how long suspended project data is kept, and how users are warned before deletion       | phase 4              | todo   |
| X.19 | Data residency, if we intend to sell in the EU                                                                            | before EU launch     | todo   |
| X.20 | Our own marketing site's SEO - we are selling an SEO tool                                                                 | before public launch | todo   |
| X.21 | Public changelog and roadmap                                                                                              | before public launch | todo   |
| X.22 | Support tooling and runbooks                                                                                              | phase 4              | todo   |

---

## Definition of done for v1 (SPEC §23)

A user who has never seen the product can, in one session and without help:
sign up, describe an app, watch it get built, edit a file themselves, add
sign-in and payments, complete a test purchase, buy a domain, publish to it,
see what it cost in credits, and connect their GitHub so they can leave with
their code if they want to.

Everything else is optimisation.
