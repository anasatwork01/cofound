# Verified platform facts

SPEC §0 rule 3 and §22: confirm every item against **current vendor
documentation** before building on it, and record what you found here with a
date and a link. An unverified row is not permission to proceed.

Status values: `unverified` · `verified` · `contradicted` · `blocked`

## SPEC §22 checklist

| #   | Fact to verify                                                                                                     | Status     | Checked    | Finding                                                                                                                                                             |
| --- | ------------------------------------------------------------------------------------------------------------------ | ---------- | ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | `@opennextjs/cloudflare` - Next.js version support, unsupported features, ISR/caching                              | unverified | -          | Blocks tasks 0.9, 0.10, 3.1                                                                                                                                         |
| 2   | Cloudflare Containers - memory/CPU limits, max request duration, long-lived SSE, pricing                           | unverified | -          | Blocks SPEC §21 decision 1                                                                                                                                          |
| 3   | Cloudflare for SaaS - custom hostname limits per zone, TLS issuance latency, apex support                          | unverified | -          | Blocks task 3.6                                                                                                                                                     |
| 4   | Cloudflare Hyperdrive - supported Postgres providers, connection limits, latency                                   | unverified | -          | Blocks tasks 0.10, 3.9                                                                                                                                              |
| 5   | Modal - Sandbox API, snapshot semantics, tunnel URL stability, volume perf, concurrency limits, non-Python client  | unverified | -          | Blocks phase 1 entirely                                                                                                                                             |
| 6   | `opencode` - server API, config schema, MCP transport, plugin/hook surface; whether P1/P2/P3/P5 still need patches | partial    | 2026-09-09 | Pinned at tag `v1.18.30` (`3104c1428e`), remote `https://github.com/anomalyco/opencode.git`. Hook/patch feasibility **not** yet assessed - do that before task 1.7. |
| 7   | Neon - project/branch creation API, branch limits, autoscaling, pricing at thousands of projects                   | unverified | -          | Blocks SPEC §21 decision 3, task 5.4                                                                                                                                |
| 8   | GitHub Apps - fine-grained permission names, installation token TTL, per-installation rate limits                  | unverified | -          | Blocks task 5.12                                                                                                                                                    |
| 9   | Google Ads API - developer token process and wait time, basic vs standard access, manager linking                  | unverified | -          | **Long lead time. Start the application during phase 0.**                                                                                                           |
| 10  | Meta Marketing API - permission names, App Review requirements and timeline, Business Verification                 | unverified | -          | **Long lead time. Start during phase 0.**                                                                                                                           |
| 11  | Stripe Connect - recommended account type, onboarding requirements, SAQ-A applicability                            | unverified | -          | Blocks task 5.7; confirm SAQ-A with Stripe, don't assume                                                                                                            |
| 12  | Google Indexing API - current eligibility rules                                                                    | unverified | -          | Blocks task 6.11                                                                                                                                                    |
| 13  | LLM provider model identifiers, pricing, cache semantics                                                           | unverified | -          | Never hardcode. Config only. See SPEC §16.4                                                                                                                         |
| 14  | Auth.js / Better Auth - status, Drizzle adapter support, behaviour on Cloudflare Workers                           | unverified | -          | Blocks SPEC §21 decision 4, task 5.6                                                                                                                                |
| 15  | Whether Workers can host the generated Next.js apps with the driver chosen in decision 3                           | unverified | -          | Blocks task 3.9                                                                                                                                                     |

## Local toolchain

Verified 2026-09-09 on darwin/amd64 (Darwin 25.5.0). Pins in `.tool-versions`,
enforced by `make doctor`.

| Tool   | Pinned  | Installed       | Note                                                                                                                                                                                                                                                                                                      |
| ------ | ------- | --------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Go     | 1.27.1  | 1.27.1          | SPEC §3.2 asks for 1.23+. Bumped from 1.25.6 on 2026-09-09 when Homebrew upgraded the toolchain; the pin tracks what is installed so CI's `setup-go` matches. `go.work` deliberately still declares `go 1.25.0` — that is the minimum _language_ version the code needs, not the toolchain the team runs. |
| Node   | 22.18.0 | 22.18.0 via nvm | Default shell node was 18.20.8, which is too old for current Next.js. `.nvmrc` + `engine-strict=true` make the mismatch fail loudly rather than silently.                                                                                                                                                 |
| pnpm   | 9.12.0  | 9.12.0          |                                                                                                                                                                                                                                                                                                           |
| Python | 3.13.1  | 3.13.1          | SPEC §3.2 asks for 3.12+; `requires-python = ">=3.12"`                                                                                                                                                                                                                                                    |
| uv     | >= 0.4  | 0.11.18         |                                                                                                                                                                                                                                                                                                           |
| Docker | >= 24   | 29.5.2          | Needed for integration tests against real Postgres                                                                                                                                                                                                                                                        |
| git    | >= 2.40 | 2.46.2          |                                                                                                                                                                                                                                                                                                           |

## Service containers

Pinned in `compose.yaml` and `.github/workflows/ci.yml`. Verified 2026-09-09 by
running them and asserting behaviour in `tests/integration/test_infra.py`.

| Component | Pin                    | Confirmed running    |
| --------- | ---------------------- | -------------------- |
| Postgres  | `postgres:18.6-alpine` | PostgreSQL 18.6      |
| Redis     | `redis:8.10-alpine`    | redis_version 8.10.1 |

### Postgres 18 changed the volume mount point

The data volume must be mounted at `/var/lib/postgresql`, **not** at
`/var/lib/postgresql/data`. The image places the cluster in a
version-namespaced subdirectory so `pg_upgrade --link` works without crossing a
mount boundary. Mounting `.../data` makes the container exit 1 on start with a
long explanatory message. See docker-library/postgres#1259.

### `current_setting('app.org_id', true)` does not reset to NULL

This decides how the SPEC §6 RLS policy must be written, and the intuitive
guess is wrong. Measured directly against 18.6:

| State of the connection                   | `current_setting('app.org_id', true)` |
| ----------------------------------------- | ------------------------------------- |
| setting never set on this session         | `NULL`                                |
| after a `set local` transaction has ended | `''` (empty string)                   |

And `''::uuid` raises `invalid input syntax for type uuid`.

So the policy shape written in SPEC §6 —
`using (org_id = current_setting('app.org_id')::uuid)` — **fails closed**, which
is the important part: an error returns no rows, so there is no cross-tenant
leak. But it fails with a database error rather than an empty result, and it
does so on any pooled connection that has already served one scoped request.

**Task 0.6 should write the policy as
`using (org_id = nullif(current_setting('app.org_id', true), '')::uuid)`.** NULL
matches no row, so an unscoped connection sees nothing instead of raising.
`tests/integration/test_infra.py::test_postgres_rls_session_variable_resets_to_empty_string`
pins all of this down, so a future Postgres upgrade that changes it fails there.

### Redis persistence is off deliberately

`--save "" --appendonly no`, because SPEC §3.3 says Redis holds nothing durable.
Local behaviour therefore matches an eviction-capable production cache instead
of accidentally depending on data surviving a restart.

Not yet installed, needed by the tasks that introduce them: `goose` (task 0.6),
`wrangler` (task 0.10), `modal` (task 1.3).
