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

## Go dependencies

Added by task 0.4 (the Go service chassis) and verified on 2026-09-10 against
`proxy.golang.org` and the local module cache — every claim below was checked by
running a command, not by reading a changelog. CONTRIBUTING.md requires this
record in the same PR as a new vendor dependency.

| Module                                                            | Pinned             | Direct                            | Note                                                                                                                                              |
| ----------------------------------------------------------------- | ------------------ | --------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------- |
| `github.com/go-chi/chi/v5`                                        | v5.3.2             | yes                               | Named by TASKS.md 0.4. Its `go.mod` contains **no `require` directive at all** — one module, zero transitive. v5.3.2 specifically: see finding 5. |
| `go.opentelemetry.io/otel`                                        | v1.46.0            | yes                               | API, baggage, and the bundled `semconv/v1.43.0`. v1.47.0-rc.1 exists and is a prerelease — do not pin it.                                         |
| `go.opentelemetry.io/otel/trace`                                  | v1.46.0            | yes                               | `trace/noop` for disabled mode, `SpanContextFromContext` for log correlation.                                                                     |
| `go.opentelemetry.io/otel/sdk`                                    | v1.46.0            | yes                               | TracerProvider, resource detection, and `sdk/trace/tracetest`, which is what makes every span assertion in-memory.                                |
| `go.opentelemetry.io/otel/metric`                                 | v1.46.0            | yes                               | `RouterConfig.MeterProvider`; otelhttp records HTTP server metrics.                                                                               |
| `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp`   | v0.71.0            | yes                               | Server middleware and client transport. v0.x, explicitly outside otel's stability guarantee.                                                      |
| `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp` | v1.46.0            | yes, in `telemetry/otlp` only     | Isolated leaf: see finding 6.                                                                                                                     |
| `github.com/felixge/httpsnoop`                                    | v1.1.0             | indirect, **pinned deliberately** | See finding 2. Never allow a downgrade.                                                                                                           |
| `github.com/anasatwork01/cofound/packages/schema/gen/go`          | v0.0.0 + `replace` | yes                               | The generated `common.Error` and `agentevents.ErrorEvent`. The replace is mandatory: see finding 7's sibling note below.                          |

**Rejected, recorded so nobody re-derives it:** `sethvargo/go-envconfig` v1.4.3
(fail-first — it structurally cannot list every problem, which is the one thing
the loader exists to do) · `caarlos0/env/v11` v11.4.1 (aggregates, but has no
validation layer, so a validator comes back anyway) ·
`exporters/stdout/stdouttrace` (the no-op default already covers collector-free
development, and the `ExporterFactory` seam makes adding one later purely
additive) · `contrib/processors/baggagecopy` (requires pre-1.0 `otel/log`) ·
`contrib/bridges/otelslog` (logs signal still pre-1.0, and it ignores
`HandlerOptions.ReplaceAttr`) · `go-simpler.org/sloglint` + `golangci-lint`
(deferred: adopting a linter changes this repo's CI shape and needs a toolchain
pin through `.tool-versions`).

### Findings

1. **`otel v1.46.0` and `otelhttp v0.71.0` are a matched pair.** otelhttp's
   `go.mod` requires `otel v1.46.0` exactly. Bump them together and group them
   in any dependency-update tooling.
2. **`httpsnoop` must never drop below v1.1.0.** It is transitive via otelhttp
   and it is the component that decides whether `SetWriteDeadline` reaches the
   real `ResponseWriter`. Verified: `grep -c SetWriteDeadline` on v1.0.4's
   `wrap_generated_gteq_1.8.go` returns **0**; v1.1.0's `wrap_generated.go`
   returns **520**. A downgrade breaks every SSE stream with no build error and
   no runtime warning. Both versions are already in this machine's module cache,
   so it is a live possibility.
3. **`semconv v1.43.0` is the highest schema bundled inside `otel v1.46.0`.**
   semconv is not a separate module. Bump the import path as its own commit —
   the schema URL is exported telemetry contract. The generated helpers are
   inconsistent: `ServiceName(v)` exists, `DeploymentEnvironmentName(v)` does
   **not**; use `DeploymentEnvironmentNameKey.String(v)`.
4. **`OTEL_SDK_DISABLED` is not implemented in the Go SDK.** Verified by
   grepping `otel@v1.46.0` and `otel/sdk@v1.46.0` — no occurrences. The chassis
   therefore implements no-op mode itself, and an absent collector must never
   fail boot.
5. **otelhttp renames the server span _after_ the handler**, guarded by
   `r.Pattern != ""`. **chi v5.3.2 sets `r.Pattern`; chi v5.2.3 does not.** So
   the **default** span-name formatter already produces low-cardinality names
   and a custom `r.URL.Path` formatter destroys them. Separately, otelhttp does
   **not** emit `http.route` — it never calls `HTTPServer.Route` — so a
   `RouteTag` middleware is required with any router. `WithRouteTag` was removed
   in v0.71.0; only `WithPublicEndpointFn` remains.
6. **`otlptracehttp` pulls gRPC regardless.** Its `internal/otlpconfig` imports
   `google.golang.org/grpc`, so `otlptracegrpc` would save nothing. Measured in
   this repo: `go list -deps ./telemetry` yields **0** gRPC packages while
   `go list -deps ./telemetry/otlp` yields **66**. That is why the exporter
   lives in a leaf package the chassis core does not import.
7. **`go work sync` rewrites a module's `go` directive to the maximum across its
   dependency graph** — not to `go.work`'s own line. `make bootstrap` runs it,
   so a module depending on the chassis must be committed at `go 1.27.1` or the
   next bootstrap leaves a dirty tree. `go build`, `go vet` and `go test` do
   **not** rewrite. Related: **`go vet` rejects Go 1.27 stdlib from a
   `go 1.25.0` module even though `go build` accepts it**
   (`http.Server.MaxHeaderValueCount requires go1.27 or later`), and
   `make lint-go` runs vet — so that combination fails CI while building fine
   locally.
8. **An intra-workspace `require` needs a matching `replace`.** The repository
   is public, so without one the module path resolves against
   `proxy.golang.org` and fails on "no matching versions" — and it fails _late_,
   only once the required module has an external dependency of its own, naming a
   source file rather than the missing directive. It reads as a network problem.

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
