# packages/chassis

The shared Go service runtime: configuration, logging, tracing, health, HTTP
and shutdown. `services/api`, `gitd`, `aigw` and `mcp` all import it. A service
main calls `chassis.Main` with a `Service` description and gets the rest.

It is a library, so it is **not** in the Makefile's `GO_SERVICES` — that
variable only builds `./services/<s>/cmd/<s>`.

## Package layout

Acyclic by construction. `scope`, `logkey` and `clock` are dependency-free
leaves precisely so `logging`, `telemetry`, `errs` and `httpx` can all reach
them without importing one another.

```
scope logkey clock  →  config  →  logging  →  errs  →  telemetry
                                                    →  lifecycle  →  health  →  httpx  →  chassis
```

| Package          | What                                                                       |
| ---------------- | -------------------------------------------------------------------------- |
| `scope`          | The SPEC §17.3 tenancy tuple: org, project, session, turn                  |
| `logkey`         | The closed log-field vocabulary                                            |
| `clock`          | The single injected time dependency, with a shareable `Fake`               |
| `config`         | An accumulating loader that reports every problem in one boot              |
| `logging`        | The redacting `slog` handler and the ctx-first call surface                |
| `errs`           | The error vocabulary, and the only constructor of the generated wire type  |
| `telemetry`      | Tracing setup, span enrichment, and two deliberately separate HTTP clients |
| `telemetry/otlp` | The OTLP exporter, isolated — importing it costs ~65 modules and ~10MB     |
| `lifecycle`      | The drain and the four-phase shutdown                                      |
| `health`         | A background-polled readiness registry                                     |
| `httpx`          | Middleware, routing, error rendering, SSE                                  |

## Five things that will bite if changed casually

Each is measured, and each has a named test.

**1. Panic recovery sits _inside_ tracing and logging.** Counterintuitive, and
the only position where the access log records 500 and the span records
`status=Error`. With recovery outermost the client still gets a 500 so nothing
_looks_ broken, while the log records status **0** and the span stays **Unset**
— panic blindness in exactly the incident that needs the trace.
`TestRecoverPositionIsObservable`.

**2. Every `ResponseWriter` wrapper must implement `Unwrap() http.ResponseWriter`.**
`ResponseController` walks the chain through an unexported interface, so one
middleware without it silently disables `SetWriteDeadline` and `Flush` for every
SSE stream behind it — with no build error.
`TestResponseControllerSurvivesTheProductionStack`.

**3. Only `WriteTimeout` cuts a live SSE stream.** `ReadTimeout`,
`ReadHeaderTimeout` and `IdleTimeout` do not. It stays at 30s to protect JSON
endpoints from slow clients, and `httpx.Open` clears it per request. Streams
must go through `httpx.Open`; a hand-rolled `text/event-stream` handler gets
truncated at 30s, and it surfaces as a client reconnect rather than a server
error, so it can hide behind `Last-Event-ID` resume indefinitely.

**4. `http.Server.Shutdown` does not cancel request contexts.** Hence the
four-phase sequence and our own drain broadcast. A handler's entire shutdown
participation is using `Stream.Context()` instead of `r.Context()`.

**5. `httpx.Timeout` calls the handler inline, never in a goroutine.** `Recover`
sits outside it, and a deferred `recover()` only catches panics on its own
goroutine — so spawning let a panicking handler escape recovery and crash the
process.

Also banned repo-wide: **`http.TimeoutHandler`**. It does not implement
`http.Flusher`, so `Flush()` returns "feature not supported" for every stream
behind it.

## The two SPEC §17.3 holes that are _not_ closed

Stated rather than claimed shut:

- **A credential interpolated into a log message** by code we do not own cannot
  be caught by key matching. `logging.ScanMessage` catches known shapes
  (`sk_live_`, `ghp_`, `postgres://`, …); an unknown provider's format still
  leaks. Task 2.13's `gitd` pre-receive scanner needs the same list — share it,
  do not copy it.
- **Debug level deliberately reveals user content.** Raising it is a privacy
  action, not a verbosity tweak, which is why production refuses
  `LOG_LEVEL=debug` and why the console-facing equivalent in a later task must
  be owner/admin-only and write an `audit_log` row.
