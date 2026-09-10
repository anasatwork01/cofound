# halyard-chassis

Everything a Halyard Python service needs before it has any behaviour of its
own: configuration, structured logging with SPEC §17.3 redaction, tracing,
readiness, ordered shutdown, and the error envelope every endpoint returns.

The Go counterpart is [`packages/chassis`](../chassis). The two are **not**
independent implementations of the same idea — see [Shared
contracts](#shared-contracts).

## Using it

```python
from halyard_chassis import Service, Setup, run


def build(setup: Setup) -> None:
    setup.registry.register("postgres", postgres_check(setup.settings.database_url))
    setup.routers.append(router)
    setup.stream_routes.append("/v1/sessions/{id}/events")  # exempt from the request deadline


def main() -> int:
    return run(
        Service(
            name="sandboxd",
            version=__version__,
            settings_class=SandboxdSettings,
            build=build,
        )
    )
```

`services/sandboxd` is the worked example. A service owns its settings subclass,
its probes and its routers; everything else is inherited.

## What you get

| Module      | Responsibility                                                                        |
| ----------- | ------------------------------------------------------------------------------------- |
| `settings`  | Reads the environment once, reports **every** problem at once, never echoes a value   |
| `logs`      | One JSON line per record, redaction that cannot be bypassed, correlation from context |
| `errors`    | The error vocabulary, the catalogue, and the only code that builds a wire error       |
| `telemetry` | Tracing that boots with no collector, and the tenancy tuple on spans                  |
| `health`    | Background-polled readiness; liveness that reads none of it                           |
| `lifecycle` | Ordered shutdown, and the drain signal ASGI does not provide                          |
| `asgi`      | The app factory, middleware order, and error rendering                                |
| `runner`    | `run()` — the single entrypoint, equivalent to Go's `chassis.Main`                    |
| `obs`       | The generated vocabulary: log field names, redaction lists, span keys                 |

## Shared contracts

Three things cross the language boundary, and each has a different mechanism
because each has a different source of truth. `packages/pychassis/tests/test_contracts.py`
fails if any of them drifts.

1. **The observability vocabulary** — log field names, the redaction denylist and
   allowlist, credential prefixes, span and baggage keys — is **generated** into
   both languages from [`packages/schema/observability.json`](../schema/observability.json)
   by `scripts/gen.sh`. Drift is impossible rather than merely tested for. Edit
   the JSON, run `make gen`, commit both.
2. **The error catalogue** lives in Go and is asserted against
   `packages/chassis/errs/testdata/catalog.golden`, which the Go tests also
   assert against. One golden file, two languages, so a message edited on one
   side fails the build on the other. The console branches on `code` and shows
   `message` and `fix` to a user; it must not matter which language served the
   request.
3. **Configuration variable names** live in `packages/chassis/config/chassis.go`
   and are parsed out of it by the Python tests. One deployment manifest
   configures both languages, so a Python service silently ignoring
   `HTTP_READ_TIMEOUT` is a manifest that lies about one of its services.

## Things that surprised us

Each of these is recorded with its evidence in [`docs/verified.md`](../../docs/verified.md).
They are here because every one of them makes the _obvious_ implementation
quietly wrong.

**Redaction has to live at `logging.Handler.handle`.** A `Filter` on the root
logger looks global and is not: `callHandlers` walks ancestor loggers' handlers
but never their filters, so records from `httpx`, `uvicorn.access` and `asyncpg`
bypass it entirely while your own records are redacted. A test that only logs
from application code passes on a broken chassis.

**Redact the resolved message, not the arguments.** `log.info("token=%s", secret)`
leaves the secret in `record.args`. Call `getMessage()` first, then redact, then
clear `args`.

**A pydantic `ValidationError` carries the raw input.** `str(e)` on a settings
failure prints your environment values — verified leaking a live-shaped API key.
Render only through `errors(include_input=False, include_url=False)`.

**Go duration strings do not parse.** `30s` is a fine `time.ParseDuration` input
and a `timedelta` parse failure. Both chassis read the same variables, so
`settings.parse_go_duration` accepts Go syntax and passes ISO 8601 through.

**`OTEL_SDK_DISABLED` is a trap twice over.** It only recognises the literal
`"true"`, so `=1` leaves the SDK exporting while the operator believes telemetry
is off; and even when honoured it still builds the provider and starts the batch
processor's thread. Disabled mode is an empty `OTEL_EXPORTER_OTLP_ENDPOINT`,
which builds nothing.

**There is no `set_error_handler` in OTel Python.** `GlobalErrorHandler` and the
`opentelemetry_error_handler` entry point have zero call sites in the packages
this chassis uses. The real seam is a handler on the `"opentelemetry"` logger.

**uvicorn runs the lifespan shutdown _last_.** It closes the listener first, then
drains, then runs your shutdown block. So a readiness flip and a drain phase
cannot live there — by then there is nothing left to drain. And uvicorn never
tells an in-flight handler that the _server_ is stopping, so a stream could only
learn about shutdown after it ended. `lifecycle.Shutdown` chains in front of
uvicorn's own SIGTERM handler to break that circle, and exposes
`shutdown.draining` for streams to select on.

**`timeout_graceful_shutdown` defaults to `None`, which is unbounded**, and it
does not bound the lifespan block either. The runner always sets it, and every
phase carries its own `asyncio.timeout`.

**A `TaskGroup` across a lifespan `yield` is dangerous.** When a child crashes it
cancels the _parent_ task, the shutdown half of the lifespan never runs, and
uvicorn keeps serving 200s. Pollers are plain tasks with strong references and a
supervisor loop.

**A graceful shutdown exits 143, not 0.** uvicorn restores the original signal
handlers and re-raises the captured signal. Do not assert exit code 0 in a
shutdown test, and do not alert on 143.

**There is no uvicorn write timeout.** The Go trap where
`http.Server.WriteTimeout` severs a live SSE stream has no equivalent — but the
flip side is that nothing bounds a runaway handler either, which is why the
chassis implements its own per-request deadline and exempts streams from it.

## Two holes that are stated, not closed

Both are the same two the Go chassis states, for the same reasons.

- A credential interpolated into a message by code we do not own cannot be
  caught by key matching. `scan_message` catches known shapes; an unknown
  provider's format still leaks.
- Raising the level to `debug` deliberately reveals user content. That is a
  privacy action, not a verbosity tweak — which is why `ChassisSettings` refuses
  `LOG_LEVEL=debug` when `HALYARD_ENV=production`.
