# Cloudflare infrastructure

`wrangler` configs and Terraform for: the console Worker, per-project app
Workers, Cloudflare for SaaS custom hostnames, R2 buckets, Workers KV
(hostname -> deployment routing), Queues, Hyperdrive, WAF and DNS.

See SPEC §3.5 for what can and cannot run on Workers. **Do not attempt to port
`gitd` or `aigw` to Workers** - long-lived streaming and git packfile handling
are the wrong shape for that runtime.

## Task 0.12

`RUNBOOK.md` is the operational half: what is blocked and on whom, the exact API
token scopes, the configuration contract for every service, and what to check
after a first deploy. Read it before touching anything here.

The console's wrangler config lives with the app, at
`apps/console/wrangler.jsonc`, with `staging` and `production` named
environments. Two rules that bite:

- **`--env` is never optional.** Deploying without it publishes to a worker that
  is neither environment.
- **Bindings are not inherited by named environments.** `vars`, secrets, KV, R2
  and Hyperdrive must be repeated per environment; the failure mode of
  forgetting is staging writing to production.

**No Hyperdrive here.** It is a Workers binding, the console never touches
Postgres (SPEC §7.1), and a Worker holding the control-plane database credential
would invert §17. It belongs to the generated user apps — tasks 3.9 and 3.12.
See `docs/verified.md` §22 item 4.

The Go services' image is `infra/docker/Dockerfile`, parameterised by `SERVICE`
and deliberately host-agnostic because SPEC §21 decision 1 is still open.
