# First deployed environment — runbook

Task 0.12. **Nothing in this repository has been deployed.** No Cloudflare
account, API token, DNS record or container host was used in building it, so
every step below is unexecuted and the first run is the real test.

Read `docs/verified.md` §22 items 1, 2 and 4 first. They are where the version
pins and the platform limits come from, and two of them contradict the vendor's
own documentation.

## What is blocked, and on whom

| Blocker                                     | Owner     | What it stops                                                        |
| ------------------------------------------- | --------- | -------------------------------------------------------------------- |
| **SPEC §21 decision 1** — container host    | **human** | Every Go service. See the decision brief in `docs/open-questions.md` |
| Cloudflare account + API token              | **human** | Both console environments                                            |
| A Postgres instance reachable from the host | **human** | `api` readiness                                                      |
| DNS for the console hostname                | **human** | Anything other than `*.workers.dev`                                  |

The console half needs only the account. **The service half cannot start until
§21 decision 1 is made**, because "deploy to the chosen host" has no subject
until then.

## Part 1 — the console (unblocked except for the account)

### 1. Cloudflare account and API token

Create a token with the **narrowest** scopes that work:

- `Account → Workers Scripts → Edit`
- `Account → Account Settings → Read` (wrangler reads the account list)
- `Zone → Workers Routes → Edit`, **only** once a custom hostname is wanted

Do **not** use a Global API Key. It is account-wide and cannot be scoped or
rotated independently, which makes it exactly the kind of credential SPEC §17
exists to keep out of a pipeline.

### 2. GitHub Environments

Create two, `staging` and `production`, and put the same two secrets in each:

| Secret                  | Where it comes from                        |
| ----------------------- | ------------------------------------------ |
| `CLOUDFLARE_API_TOKEN`  | step 1, one token per environment ideally  |
| `CLOUDFLARE_ACCOUNT_ID` | Cloudflare dashboard, or `wrangler whoami` |

Add a **required reviewer** to `production` only. The deploy workflow names the
environment; the protection rule lives in repository settings and is not
something this repo can assert.

### 3. Turn the workflow on

Set the repository variable `DEPLOY_CONSOLE_ENABLED` to `true`.

Until then `.github/workflows/deploy-console.yml` skips every job by design — a
deploy workflow that fails for want of a secret teaches people to ignore red.

### 4. First deploy

```bash
nvm use                                        # 22.18.0; wrangler needs >= 22
pnpm install --frozen-lockfile
pnpm --filter @halyard/console cf:build        # opennextjs-cloudflare build
cd apps/console
pnpm exec wrangler deploy --env staging        # --env is NOT optional
```

Omitting `--env` publishes to a worker named `halyard-console` that is neither
environment. The workflow always passes it explicitly for that reason.

### 5. What to check, in this order

1. `/` redirects to `/new`, and `/new` renders the prompt and the gallery.
2. The credit gauge shows its **unknown** state on every screen. That is
   correct — there is no credits endpoint until phase 4, and zero would read as
   "you are out of credits".
3. `startup_time_ms` in the deploy output. §22 item 1 established that the real
   ceiling is **1 second of startup CPU** (rejected as error 10021), not bundle
   size — the 64 MiB uncompressed limit is far away, and OpenNext's own docs
   still publish a stale 3/10 MiB figure. Watch the CPU number, not the bytes.
4. `compatibility_date` is `2026-09-10`. The adapter starts warning when it is
   roughly six months old, so it needs a refresh around **March 2027**.

### 6. Hold a stream open

Not strictly console work, but do it while an environment is fresh: **open one
SSE stream and leave it for an hour.** §22 item 1 found that Cloudflare updates
the Workers runtime a few times a week and gives in-flight requests a 30-second
grace, so a long-lived stream is cut several times a week _by design_. That
makes `Last-Event-ID` resume load-bearing rather than a nicety, and task 1.14
must treat mid-stream termination as routine. Better to see it now than to
debug it as an intermittent bug later.

## Part 2 — the Go services (blocked on §21 decision 1)

### The image is ready and host-agnostic

```bash
docker build -f infra/docker/Dockerfile \
  --build-arg SERVICE=api \
  --build-arg VERSION="$(git describe --tags --always --dirty)" \
  --build-arg COMMIT="$(git rev-parse --short HEAD)" \
  -t halyard-api .
```

`SERVICE` selects the binary: `api`, `gitd`, `aigw`, `mcp`. The build context
must be the **repository root** — `go.work` spans `packages/` and `services/`,
so a narrower context cannot resolve the workspace. Every candidate host in
SPEC §3.5 consumes an OCI image, so this file does not need the decision and
must not grow a host-specific assumption.

Verified locally: 33.9 MB, distroless, non-root, and it serves `/healthz` and
`/readyz` against a real Postgres. CI builds it on every PR and asserts it
starts.

### Configuration contract

The chassis binds these; `docs/verified.md` and `packages/chassis/config` are
the source of truth.

| Variable                                                       | Notes                                                                                                                                                                                        |
| -------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `HTTP_ADDR`                                                    | **Not `PORT`.** Defaults to `:8080`. A host that injects `PORT` (Cloud Run, Railway) must set `HTTP_ADDR` explicitly, or the service listens on the default and the host routes to nothing   |
| `ADMIN_ADDR`                                                   | Leave **unset** in production. It exposes pprof and the log-level control, and the chassis refuses a non-loopback value in production                                                        |
| `HALYARD_ENV`                                                  | `staging` or `production`. Changes log format and turns on production-only config validation                                                                                                 |
| `DATABASE_URL`                                                 | **Must not be a superuser.** `db.Open` refuses a role with `SUPERUSER` or `BYPASSRLS`, because row-level security would be silently inert — migration `00013` creates `halyard_app` for this |
| `REDIS_URL`                                                    |                                                                                                                                                                                              |
| `CONSOLE_ORIGIN`                                               | Every sign-in link is built from this, never from the request's `Host` — an attacker who can set `Host` would otherwise have us email a link to their server                                 |
| `GOOGLE_CLIENT_ID` / `_SECRET` / `_REDIRECT_URL`               | Google OAuth, task 0.7                                                                                                                                                                       |
| `SESSION_IDLE_TIMEOUT` / `_ABSOLUTE_TIMEOUT` / `_ROTATE_AFTER` | Defaults are sensible; override only deliberately                                                                                                                                            |
| `OTEL_EXPORTER_OTLP_ENDPOINT`                                  | Unset disables export. Task 0.13 owns Sentry                                                                                                                                                 |

The RLS guard firing is a **success**, not a misconfiguration: it means tenant
isolation is real. If a service refuses to start saying the role bypasses
row-level security, the fix is the credential, never the guard.

### When §21 decision 1 is made

1. Publish images to a registry the host can pull from.
2. Add a deploy workflow per host, modelled on `deploy-console.yml`: gated on a
   repository variable, environment-scoped secrets, explicit target.
3. Point Cloudflare DNS at the host and put the WAF in front (SPEC §3.5).
4. Set `TRUSTED_PROXY_CIDRS` to the proxy's ranges — otherwise every request
   appears to come from the proxy and `KeyByIP` rate limiting (task 0.10)
   buckets every client together.
5. Measure what the chosen host does **not** write down. For Cloudflare
   Containers that is per-instance concurrent connections and whether a proxied
   SSE stream survives at all; for Fly.io it is Fly Proxy's long-lived-HTTP
   behaviour. §22 item 2 records which unknown belongs to which host.

## What this runbook does not cover

Custom hostnames and TLS for **generated user apps** (Cloudflare for SaaS,
§22 item 3, task 3.6), Hyperdrive (§22 item 4 — **not needed by 0.12**; it is a
Workers binding and the control-plane database lives behind Go `api`), R2, KV,
Queues and Durable Objects. Each arrives with the task that needs it.
