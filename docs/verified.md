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

## Python dependencies

Verified 2026-09-10 for task 0.5, against PyPI, the upstream changelogs and the
installed wheel source. Every behavioural claim below was executed on CPython
3.13.1; where a documented mechanism turned out not to work, that is stated
rather than the working alternative simply being used.

| Package                                        | Pinned         | Why exactly this                                                                   |
| ---------------------------------------------- | -------------- | ---------------------------------------------------------------------------------- |
| `fastapi`                                      | `0.141.1`      | Latest stable. Classifiers through 3.14.                                           |
| `starlette`                                    | `1.6.0`        | **Pinned explicitly, and this is load-bearing.** See below.                        |
| `uvicorn[standard]`                            | `0.52.4`       | Latest stable. `httptools` and `uvloop` publish cp313 wheels, so no source builds. |
| `pydantic`                                     | `2.13.5`       | Exact, not ranged. See the `_extract_field_info` note below.                       |
| `pydantic-settings`                            | `2.15.0`       | Exact. 2.15.0 altered `case_sensitive` semantics in a _minor_ release.             |
| `opentelemetry-{api,sdk}`                      | `1.44.0`       | The stable 1.x line.                                                               |
| `opentelemetry-exporter-otlp-proto-http`       | `1.44.0`       | Follows the 1.x line. No `grpcio` in its closure.                                  |
| `opentelemetry-instrumentation-{fastapi,asgi}` | `0.65b0`       | The paired 0.x release. See the pairing rule below.                                |
| `httpx2`                                       | `>=0.29` (dev) | Starlette 1.6's `TestClient` imports `httpx2`, not `httpx`.                        |

### FastAPI no longer caps Starlette, and the floor admits five CVEs

FastAPI 0.141.1 declares `starlette>=0.46.0` with **no upper bound** — the cap
was removed when Starlette reached 1.0. So FastAPI no longer protects a build
from a breaking Starlette release, and the chassis must pin it itself or a
future Starlette 2.0 resolves silently into a build.

The floor is also below five published advisories. Two matter directly to
SPEC §17: **CVE-2026-48710** (missing Host header validation poisons
`request.url.path`) and **CVE-2026-54282** (an unvalidated request path
concatenated into the authority poisons `request.url.hostname`). A poisoned
`request.url.path` would defeat path-based authorization in the control plane.
`1.6.0` clears all five.

### The OTel version pairing is arithmetic

Core release `1.N.x` pairs with instrumentation release `0.(N+21)bX`. `1.44.0`
pairs with `0.65b0`. Bumping one line without the other is the Python form of
the otel/otelhttp mismatch already recorded under Go dependencies. Compute the
0.x number rather than looking it up.

Confirmed absent: **`grpcio` is not in the resolved lockfile.** The HTTP
exporter needs none of it; the `opentelemetry-exporter-otlp` _meta-package_
would pull `opentelemetry-exporter-otlp-proto-grpc` and grpcio 1.83.1, which is
exactly the bloat the Go chassis restructured a package to avoid. The exporter
is however built on blocking `requests`, so `BatchSpanProcessor` is mandatory —
`SimpleSpanProcessor` would block the event loop on an HTTP POST per span.

### `OTEL_SDK_DISABLED` is a trap twice over, in Python too

Recorded under Go dependencies as _unimplemented_. In Python it is implemented
and still unusable:

1. The parse is `value.lower().strip() == "true"`, so `OTEL_SDK_DISABLED=1`,
   `=yes` and `=on` leave the SDK **fully enabled and exporting** while the
   operator believes telemetry is off.
2. Even when honoured, it still constructs the provider, runs resource
   detection, builds the exporter and starts the batch processor's daemon
   thread.

So disabled mode is implemented the same way as in Go: an empty
`OTEL_EXPORTER_OTLP_ENDPOINT` builds no provider at all.

### There is no OTel error handler in Python

`otel.SetErrorHandler` has no equivalent. Writing an `ErrorHandler` subclass and
registering the `opentelemetry_error_handler` entry point — the **documented**
mechanism — accomplishes nothing: `GlobalErrorHandler` has zero call sites in
the SDK, exporter or instrumentation packages this chassis uses. Recorded here
so nobody re-derives it from the docstring. The real seam is a
`logging.Handler` on `logging.getLogger("opentelemetry")`, and trace-export
failures are logged once per failed batch with no deduplication, so the chassis
adds its own.

### Neither OTel shutdown API accepts a deadline

`TracerProvider.shutdown()` takes no timeout and is registered with `atexit` by
default, so a hanging collector can add up to 30 seconds to termination — past
most SIGTERM grace periods, turning a clean deploy into a SIGKILL. Hence
`shutdown_on_exit=False`.

`BatchSpanProcessor.shutdown()` takes no timeout either, and
`force_flush(timeout_millis=N)` **accepts N and ignores it** — verified: asked
for 100 ms against a 2 s exporter, it returned `True` after 2.00 s. There is no
API here that can be asked to give up, so the chassis bounds it with
`wait_for` over a worker thread and abandons the thread if the budget expires.
That is safe because the processor's worker is a daemon thread.

Also: `set_baggage()` returns a **new** Context and does not mutate the ambient
one, so code that drops the return value type-checks, runs and propagates
nothing. And `OTEL_PROPAGATORS` is captured at import time of
`opentelemetry.propagate` into a module global, so setting it from a Python
settings module is a silent no-op — the chassis calls
`propagate.set_global_textmap()` instead.

### Python's logging: where redaction must live

The chassis needs one hook that every record passes through, including records
from libraries. Four candidates were measured and three of them are broken for
this purpose:

| Hook                            | Why it fails                                                                                                                                                                                                                                                       |
| ------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `Filter` on the root **logger** | `callHandlers` walks ancestor loggers' **handlers** but never their **filters**, so every record from `httpx`, `uvicorn.access` and `asyncpg` bypasses it. A test that only logs from app code passes on a broken chassis.                                         |
| `Formatter`                     | Four escapes: a sibling handler with its own formatter; the `exc_text` cache holding the raw traceback for the next handler; `Handler.handleError` dumping raw `msg` and `args` to stderr when a formatter raises; and handlers that never call `format()` at all. |
| `setLoggerClass`                | Misses every logger created before the call — i.e. essentially all library loggers, created at import — and never touches root.                                                                                                                                    |
| `setLogRecordFactory`           | A single global slot any library can clobber, and it runs before `exc_text` or the interpolated message exist.                                                                                                                                                     |

**`logging.Handler.handle` is the answer.** It also runs before
`QueueHandler.prepare` interpolates, and before any sibling formatter can
populate the `exc_text` cache with plaintext.

Supporting findings, all verified:

- `log.info("token=%s", secret)` leaves the secret in `record.args`. Resolve
  `getMessage()` first, redact, then clear `args`. Inspecting args element-wise
  misses a non-`str` `msg` whose `__str__` holds the secret.
- Guarding traceback redaction with `if not record.exc_text` **leaks from both
  handlers** when an unredacted one formatted the record first. Redact
  unconditionally and null `exc_info`.
- Python 3.12's return-a-`LogRecord` filter form is the **opposite** of what is
  wanted: it modifies a record for one handler "without side effects on other
  handlers", leaving the plaintext original for everyone else.
- `logging.lastResort` writes records to raw stderr at WARNING, around every
  handler, for any logger with `propagate=False` and no handler of its own. It
  is set to `None`.
- `logging.raiseExceptions` must be `False`, or a missing format-string
  attribute routes to `handleError`, which dumps raw `msg` and `args` to stderr
  — a cosmetic bug becoming a plaintext leak.
- `basicConfig(force=True)` removes **and closes** existing root handlers. The
  chassis wraps any handler added to root so it redacts too, rather than
  refusing it — refusing breaks `pytest`, `caplog` and debuggers, which pushes
  people to configure late or not at all.
- Stdlib tracebacks do not print locals, but the exception's own `str()`, PEP 678
  notes, the whole `__cause__`/`__context__` chain and the **source line of the
  raise** all render. Redacting the formatted string covers all four.
- A `ContextVar` does **not** propagate into `loop.run_in_executor`,
  `ThreadPoolExecutor.submit` or `threading.Thread` — all start with an empty
  context, so a sync-`def` endpoint silently logs the default.
  `asyncio.to_thread` is the exception that does propagate.

### pydantic-settings: four things that break accumulation or leak

- **A `ValidationError` carries the raw input.** On a `missing` error, `input`
  holds the entire pre-validation dict, so `str(e)` and `errors()` contain raw
  environment values — verified leaking a live-shaped API key and a password.
  `SecretStr` does not help: the leak happens before field validation runs.
  Render only through `errors(include_input=False, include_url=False)`.
- **`SettingsError` is not a `ValidationError` subclass** and aborts before
  validation, reporting exactly one problem. Every complex field in
  `ChassisSettings` is declared `NoDecode` with comma-splitting instead, which
  restores full accumulation.
- **`extra="forbid"` is inert for `os.environ`.** It reads like strict
  validation, but `HTTP_ADDDR=...` is dropped without comment and the service
  starts on the default. Unknown-variable detection is built by hand.
- **An empty variable crashes a field that has a good default.** `HTTP_ADDR=`
  against a defaulted field raises rather than falling back, and Compose,
  Kubernetes and shell templating all emit `FOO=` for unset. `env_ignore_empty`
  fixes the empty case; whitespace-only needed a stripping validator, because
  `env_ignore_empty` only sees the untrimmed string while Go's `raw()` trims
  first.

Two smaller ones: `bool` rejects surrounding whitespace while `int` accepts it,
so a stray trailing space breaks only the boolean settings; and
`SettingsConfigDict` merges across the MRO, so a subclass that _omits_
`env_prefix` keeps the base's rather than clearing it.

`env_names()` derives the field-to-variable mapping from the fields rather than
calling `EnvSettingsSource._extract_field_info`, a private API on a package that
changes resolution semantics in minor releases. The cost is that `AliasChoices`
is unsupported — deliberately: it cannot report which alias supplied a value,
and an error naming the wrong variable is worse than none.

### uvicorn's shutdown order, and why the Go four-phase design cannot be ported directly

Verified against the 0.52.4 source:

1. The SIGTERM handler only sets `should_exit`; the main loop notices on its
   next 0.1 s tick.
2. `Server.shutdown()` closes **every listener immediately** — there is no
   lame-duck window in which it still accepts traffic.
3. It then waits for in-flight work inside
   `asyncio.wait_for(..., timeout=timeout_graceful_shutdown)`.
4. **Only then** does it run the lifespan shutdown block.

So the Go chassis's phase 1 (fail readiness) and phase 2 (drain) cannot live in
the lifespan: by the time it runs, the listener is closed and there is nothing
left to drain.

Worse, **uvicorn never tells an in-flight handler that the server is stopping.**
`receive()` yields `http.disconnect` only when the _client_ went away, and
`Protocol.shutdown()` for a started response merely sets `keep_alive = False`.
That makes a circular dependency: a stream could only learn about shutdown from
the lifespan block, which does not run until the stream ends.

`lifecycle.Shutdown` resolves it by capturing the installed SIGTERM handler
(uvicorn's bound `Server.handle_exit`), installing its own, and calling the
captured one once its deregister and drain phases finish. It must be installed
from the lifespan **startup**, because uvicorn installs its handlers inside
`serve()` and anything earlier is simply replaced.

Further verified facts:

- **`timeout_graceful_shutdown` defaults to `None`**, which is
  `asyncio.wait_for(..., timeout=None)` — unbounded. One hung stream blocks
  shutdown forever and the lifespan cleanup never runs. The runner always sets
  it.
- It does **not** bound the lifespan shutdown block, which sits outside the
  `wait_for`. A hung `pool.close()` hangs the process regardless of every
  uvicorn setting, so each phase carries its own `asyncio.timeout`.
- uvicorn does not **await** the tasks it cancels before running the lifespan
  shutdown, so a cancelled handler's `finally` runs concurrently with the block
  tearing down the resources it touches.
- A **second SIGTERM escalates nothing** (`handle_exit` promotes to `force_exit`
  only for SIGINT), so a supervisor re-sending it does nothing. And when
  `force_exit` _is_ set, the lifespan shutdown is **skipped entirely** — so
  correctness must never depend solely on it.
- A graceful shutdown **exits 143, not 0**: `capture_signals` restores the
  original handlers and re-raises the captured signal.
- A request cancelled by the graceful timeout is logged by uvicorn as
  `ERROR: Exception in ASGI application` with a full `CancelledError` traceback,
  so every rolling deploy emits error-level tracebacks unless the chassis
  classifies them as expected.

**There is no uvicorn write, response, total-request or header-read timeout
anywhere in the codebase.** The Go trap where `http.Server.WriteTimeout` severs
a live SSE stream has no Python equivalent, and `timeout_keep_alive` (5 s) cannot
do it either — the keep-alive timer is armed only between requests and is
disarmed when one begins. The flip side is that nothing bounds a runaway handler,
which is why the chassis implements its own per-request deadline in middleware
and exempts streams.

### asyncio: a TaskGroup is the wrong tool for a background poller

- Holding a `TaskGroup` open across a lifespan `yield` is actively dangerous:
  `_on_task_done` calls `self._parent_task.cancel()`, so a poller that crashes
  mid-serving cancels the **lifespan task**, the `ExceptionGroup` surfaces at
  `contextlib`'s `athrow`, and **the shutdown half never runs** while uvicorn
  keeps serving 200s.
- A bare `create_task` poller that raises is **completely silent** at the time of
  failure; asyncio only mentions it when the task is garbage collected. And the
  loop keeps only **weak** references, so an unreferenced task can be collected
  mid-execution.
- `asyncio.CancelledError` derives from `BaseException` on 3.13, so
  `except Exception` in a supervisor loop is automatically cancellation-safe —
  and a bare `except:` is not.
- `task.cancel()` is only a request: cancel, then `await` while suppressing
  `CancelledError`, or the task's own cleanup is truncated.
- A timeout that fires while the body is inside a `finally` interrupts the
  cleanup mid-way, so shutdown steps get a fresh budget rather than inheriting
  the caller's. And a timeout around `asyncio.shield()`ed work raises to the
  waiter while the shielded task keeps running unsupervised — so a health poll
  is never shielded.
- The lifespan shutdown half runs on a fully operational loop: arbitrary awaits,
  DNS and new connections all work, so a final OTLP flush over the network there
  is viable.
- Neither Starlette nor uvicorn exposes any in-flight request count or drain
  signal. uvicorn tracks `server_state.connections` and `.tasks` internally, but
  they are undocumented and connection-level rather than request-level.

### Dependency health checks

| Dependency                  | Cheapest correct check              | Measured                                        | Note                                                                                                               |
| --------------------------- | ----------------------------------- | ----------------------------------------------- | ------------------------------------------------------------------------------------------------------------------ |
| Postgres (`asyncpg` 0.31.0) | `execute("SELECT 1")`, no arguments | **0.589 ms/op pooled, 26.5 ms/op reconnecting** | Takes the simple-query path, so it does not pollute the statement cache. `fetchval` is marginally faster but does. |
| Redis (`redis-py` 8.1.0)    | `ping()` on a dedicated client      | 0.183 ms/op                                     | Must use `retry=Retry(NoBackoff(), 0)`.                                                                            |

- **The probe must hold its connection.** Measured against the local
  containers on 2026-09-10: a fresh `asyncpg.connect` per poll costs 26.5 ms
  against 0.589 ms through a pool — 45x, and a full TCP and authentication
  handshake every `READINESS_INTERVAL` on every replica. A readiness probe that
  is itself a load generator is a bad readiness probe, so
  `halyard_sandboxd.probes` keeps a two-connection pool and registers its
  `close` with the chassis shutdown.
- **asyncpg has no built-in pool health check.** Its pool only tests a local
  `is_closed()` flag on acquire and recycles idle connections on a timer, so the
  background poller _is_ the health check.
- asyncpg **transparently recovers** from a server-side terminated backend:
  after `pg_terminate_backend`, `pool.execute("SELECT 1")` succeeded on the
  first retry. So a single failed poll is a failover blip, not an outage — which
  is why the registry's failure threshold is 2 rather than 1.
- **redis-py defaults to 10 retries with exponential jitter backoff** on
  `ConnectionError`, which makes a "cheap" ping take many seconds against an
  unreachable host regardless of `socket_connect_timeout`. Hence the dedicated
  no-retry client. On the _ordinary_ client, set `health_check_interval=30` so
  request-path traffic validates connections.
- A liveness probe that checks the database is the standard mistake: a database
  blip then restarts every replica, which does not fix the database.
  Kubernetes' own docs warn this causes cascading failures. `/livez` reads no
  dependency verdict; `/readyz` reads all of them.

## Control plane database

Verified 2026-09-11 for task 0.6, against the live Postgres 18.6 container and
the Go module proxy. Every behavioural claim below was **executed**, not read:
the scripts are reproduced by `tests/integration/test_tenancy.py` and
`packages/db/db_test.go`, so a future Postgres upgrade that changes any of it
fails the build.

| Package                       | Pinned    | Why                                            |
| ----------------------------- | --------- | ---------------------------------------------- |
| `github.com/jackc/pgx/v5`     | `v5.11.0` | Latest. `pgxpool` is still the pool.           |
| `github.com/pressly/goose/v3` | `v3.28.0` | Used as a **library**, not the CLI. See below. |
| `github.com/exaring/otelpgx`  | `v0.12.0` | Query spans, pairs with otel-go 1.46.0.        |
| `github.com/google/uuid`      | `v1.6.0`  | Test seeding only.                             |

### Row-level security is inert for the role that owns the tables

This is the finding the whole task turns on, and it fails **silently**.

Connected as the table owner, a policy-scoped query returned **every row of
every tenant** — no error, no warning, no clue in the result that a policy
existed at all. And `alter table ... force row level security` did **not** fix
it, because the role was also a superuser: `FORCE` subjects the _owner_ to
policies but a `SUPERUSER` or `BYPASSRLS` role ignores them regardless.

`compose.yaml`'s `halyard` role is `rolsuper=t, rolbypassrls=t`. So a service
configured with the migration credentials has **no tenant isolation whatsoever**
while looking perfectly healthy.

Two mitigations, both in place, neither optional:

1. Migration `00013` creates `halyard_app` — not the owner, not a superuser, no
   `BYPASSRLS` — and services connect as that role. It ships `NOLOGIN` with no
   password, because a credential in a migration is a credential in git.
2. `db.Open` reads `rolsuper` and `rolbypassrls` at startup and **refuses to
   return a pool** for a role that would bypass policies. There is no way to
   detect this from the application's own queries — they simply return more rows
   than they should — so the check has to happen before traffic does.
   `packages/db/db_test.go::TestOpenRefusesAPrivilegedRole` asserts it.

`tests/integration/test_tenancy.py::test_the_owner_would_see_everything` pins
the trap itself as an executable fact, so "simplify the two roles into one"
fails a test rather than shipping.

### The naive policy expression works cold and breaks warm

SPEC §6 writes `current_setting('app.org_id')::uuid`. Measured, in this order:

| Connection state                 | `current_setting('app.org_id', true)` | `::uuid`                                        |
| -------------------------------- | ------------------------------------- | ----------------------------------------------- |
| never scoped                     | `NULL`                                | `NULL` — harmless                               |
| after a scoped transaction ended | `''`                                  | **raises** `invalid input syntax for type uuid` |

So the naive spelling passes on a cold connection and starts raising once the
pool is warm — which is the worst possible failure shape, because it works in
testing and breaks under traffic. `nullif(current_setting('app.org_id', true),
'')::uuid` yields `NULL`, and a policy comparing against `NULL` matches **zero
rows** with no error: it fails closed quietly rather than as a 500 on an
unrelated endpoint.

### `SET LOCAL` cannot be parameterised, and a plain `SET` leaks

- `set local app.org_id = $1` is a **syntax error**. Building it by
  concatenation would put a SQL injection in the one place that must not have
  one, so `db.Scope` uses `set_config('app.org_id', $1, true)` — verified
  identical in effect, and parameterised.
- A `SET` _without_ `LOCAL` persists for the whole session. On a pooled
  connection that means the next borrower inherits the previous request's org —
  one tenant reading another's data with no code change and no error.
  `db.Scope` therefore always opens a transaction, and there is no exported way
  to set the org without one. `TestScopeDoesNotLeakOntoTheConnection` hammers 50
  acquisitions to prove it.

### Reaching the org through a parent table does not scale

Sixteen tables in §6 carry `project_id` but no `org_id`, so §6's literal rule
("enable RLS on every table with `org_id`") would leave `secrets` — the most
sensitive table in the schema — with no policy. Measured on 100k rows / 1000
projects / 200 orgs, reading one org's rows:

| Policy shape                                                 | Time        | Plan                                                 |
| ------------------------------------------------------------ | ----------- | ---------------------------------------------------- |
| `org_id = current_org()`                                     | **0.36 ms** | Bitmap Index Scan                                    |
| `project_id in (select id from projects where org_id = ...)` | 2.71 ms     | Index Only Scan, **99 500 rows discarded by filter** |
| `exists (select 1 from projects ...)`                        | 77.24 ms    | **Seq Scan** + JIT                                   |

The subquery forms do not use an index to _find_ the rows; they scan and
discard, so their cost tracks the whole table rather than the tenant. At 100k
rows that is 2.7 ms; at 10M it is not a policy, it is an outage.

So `org_id` is denormalised onto every tenant-scoped table, under §6's own
"Not exhaustive — add columns as needed". The integrity hole that would
normally create — a row whose `org_id` disagrees with its parent's is invisible
to its real owner and visible to someone else — is closed by a **composite
foreign key** onto the parent's `(id, org_id)`, which is why `projects` carries
an otherwise-redundant `unique (id, org_id)`. Verified: a mismatched insert is
refused, and so is moving a project between orgs while children reference it.
The result satisfies §6's RLS rule on **all 28** tenant tables rather than on
ten of them.

### `USING` alone governs writes too

For a `FOR ALL` policy, Postgres reuses the `USING` expression as the check on
rows being written when `WITH CHECK` is omitted. Verified: with `USING` only, an
`INSERT` naming another org is refused with "new row violates row-level security
policy", and so is an `UPDATE` that would move a row out of the current org. My
initial assumption was the opposite; the test corrected it. Spelling it twice
would add no protection and create two places to keep in step.

Also verified: RLS **enabled with no policy at all denies everything**, so a
table that gets `enable row level security` and no `create policy` fails closed
rather than open. And a `DELETE` of an invisible row reports `DELETE 0` rather
than erroring — so an unqualified `delete from secrets` as one tenant leaves
every other tenant's rows untouched.

### goose's CLI pulls a driver for every database it supports

`github.com/pressly/goose/v3/cmd/goose` imports ClickHouse, MySQL, MSSQL,
Vertica, YDB and SQLite drivers. Taking it as a `go tool` dependency put all of
them in this module's graph for a Postgres-only control plane; `go mod tidy` was
still resolving after several minutes and was abandoned.

The goose **library** pulls none of that. `packages/db/cmd/migrate` is a ~100
line binary using `goose.UpContext` over `database/sql` with pgx's `stdlib`
driver: 209 modules total and **zero** other-database drivers, asserted by
`make verify`'s structure step.

Other goose facts confirmed: `up` takes a Postgres advisory lock, so two
replicas deploying at once do not both apply a migration; and goose parses
**any** line beginning with its annotation prefix, comment or not — a comment
that merely _mentioned_ the rollback marker made the migration unparseable,
which `make db-validate` caught.

### Forward-only is enforced by absence

No migration has a rollback section, and `make db-validate` fails the build if
one appears. SPEC §0 rule 6 makes migrations forward-only, and the most reliable
enforcement is for the rollback not to exist: a section that drops a table is one
command away from deleting a tenant's history. Local iteration uses
`make db-reset`, which refuses to run against anything but the compose database.

## Console authentication

Verified 2026-09-11 for task 0.7. No new third-party dependency: the flows are
implemented against Go's standard library and the packages already pinned, so
what is recorded here is behaviour rather than versions.

### Google's ID token needs no signature check on this path

`parseIDToken` reads the claims and does **not** verify the JWT signature. That
is standards-sanctioned rather than a shortcut. OpenID Connect Core §3.1.3.7
item 6: _"If the ID Token is received via direct communication between the
Client and the Token Endpoint, the TLS server validation MAY be used to
validate the issuer in place of checking the token signature."_ This token
arrives on our own TLS connection to Google's token endpoint, authenticated with
our client secret — never through the browser — so the transport already
establishes who sent it.

The claims that are **not** optional are enforced, and skipping the signature is
only sound because they are:

- **`aud` must equal our client id.** Without it, an ID token minted for _any_
  other Google client would authenticate a user here. This is the
  confused-deputy problem the claim exists for.
- **`iss` must be Google.** Both historical spellings are accepted
  (`https://accounts.google.com` and `accounts.google.com`), nothing else.
- **`exp` must be present and future.** A missing `exp` is rejected rather than
  treated as "no deadline", which would make the token a permanent credential.
- **`email_verified` must be true**, and it is read as either a JSON boolean or
  the string `"true"` — Google has emitted both. A parser handling only the
  boolean reads the string as false and refuses every sign-in, which looks like
  a Google outage rather than a bug.

The alternative — fetching, caching and rotating Google's JWKS — is worth doing
for a token arriving through an untrusted channel. Here it would add a second
network dependency on the sign-in path without adding a property.

### `%v` reflects into unexported fields

The `Token` type originally did **not** implement `fmt.Stringer`, on the
reasoning that a `String` method would embed the token in any log line that
formatted a surrounding struct. That was exactly backwards, and
`TestTokenRedactsItselfInEveryFormatVerb` caught it: `fmt` reflects into
unexported fields, so `%v` on a struct holding a `Token` printed the raw token
regardless. **Not** implementing `Stringer` left the hole open; implementing it
to return `Token([redacted])`, plus `GoString` for `%#v`, closes it. The test
covers `%v`, `%s`, `%+v`, `%#v` and `%q`, on the value, the struct and a pointer
to it.

### Postgres timestamps are microseconds; Go's are nanoseconds

`timestamptz` stores microsecond precision. `time.Now()` on Linux returns
nanoseconds. So a deadline held in memory and the same deadline read back from a
column differ in the last three digits, and comparing the two is a test that
passes on macOS — whose clock is already microsecond-granular — and fails on
Linux. That is exactly what happened: `TestRotationDoesNotExtendTheAbsoluteDeadline`
passed locally and failed in CI with `...357563094` against `...357563`.

Any assertion about a timestamp that has been through the database must take
both sides from the database, or truncate to `time.Microsecond`. The test now
reads its baseline back with a query rather than using the value the constructor
returned.

### The rotation grace window is a bug fix, not slack

SPEC §8 requires rotating cookies. A console page issues several requests at
once, so if the first rotates the token the others are still carrying the old
one — and without a grace window each is rejected, signing the user out for the
crime of loading a page. A rotated token therefore stays valid for 60 seconds
and the caller is handed the successor session.

Two properties keep that from becoming a hole, both asserted:

- Past the grace window the old token is dead, so a cookie captured earlier does
  not keep working.
- Rotation **never** extends `absolute_expires_at`. Otherwise "rotating" means
  "immortal under a new name each time", and nothing ever ejects a session an
  attacker keeps warm. The test rotates five times and then checks the session
  dies at its original absolute deadline.

The grace path deliberately returns **no** new token: the raw successor cannot
be recovered (only its hash is stored), and it does not need to be — the request
that did the rotation already gave the browser the new cookie. Minting a fresh
token per concurrent request is how one page load becomes six sessions.

### `__Host-` requires Secure, and is dropped silently without it

The session cookie is `__Host-halyard_session` in production. A browser accepts
that prefix only with `Secure`, `Path=/` and **no** `Domain`, which is what
stops the cookie reaching a preview subdomain running code the agent wrote
(SPEC §9, §17). Without `Secure` the browser drops it with no error, presenting
as "sign-in does nothing" — so the name is chosen by the same flag that sets
`Secure` rather than configured separately, and development falls back to the
unprefixed name.

`SameSite=Lax` and not `Strict`: a magic link arrives from an email client as a
cross-site top-level navigation, and `Strict` withholds the cookie on exactly
that request, so the user lands signed out. The OAuth challenge cookie is `Lax`
for the same reason — the callback arrives from Google.

### Postgres, not application logic, enforces single use

A magic link is consumed with one statement:

```sql
update login_tokens set consumed_at = now()
 where token_hash = $1 and consumed_at is null and expires_at > now()
returning email
```

A select-then-update would let two concurrent clicks both pass the select — the
classic double-spend — and single use is what makes a credential sitting in an
inbox acceptable at all.

### Email normalisation stops at case and whitespace

Deliberately **not** the popular extras of stripping dots or `+suffixes`. Those
rules belong to particular providers, change without notice, and applying them
collapses two distinct addresses at any provider that does not share the
assumption. Under-normalising creates a duplicate account, which is
recoverable; over-normalising hands one person's account to another, which is
not. Asserted in both directions.

### Restrictive policies, and the widening that prompted them

Found while verifying row-level security for task 0.6, after `00013` was already
written. Postgres OR-s permissive policies and AND-s restrictive ones, so with
only permissive policies **any** policy added later widens access:

```sql
create policy support_read on secrets using (true);   -- for an internal admin view
```

That one line makes every tenant's secrets readable by every tenant. Migration
`00015` adds a restrictive guard carrying the same org comparison to all 28
tenant tables, which is AND-ed with whatever else exists.
`test_a_careless_permissive_policy_cannot_widen_access` performs exactly that
mistake and asserts the boundary holds — and was checked to **fail** when the
guard is dropped, so it is not passing for another reason.

Related, from the same verification: the ownership bypass follows role
**membership**, not identity, so granting the app role membership in the owner
role would silently disable RLS on any table that is not `FORCE`d. Every table
is forced, which makes that mistake non-fatal.

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
