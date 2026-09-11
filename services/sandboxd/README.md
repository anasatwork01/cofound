# `sandboxd` — Modal sandbox lifecycle

Python, per SPEC §3.2 and §21 decision 2 (reconfirmed 2026-09-11: a Python
sidecar for Modal). Owns create, wake, snapshot, stop, the warm pool, tunnels
and quotas.

**Read `docs/verified.md` §22 item 5 before changing anything here.** Every
constraint below is a verified property of the Modal 1.5.5 API, not a
preference, and three of them contradict SPEC §9's lifecycle as written.

## Where SPEC §9's lifecycle cannot work as written

§9 lists a seven-step lifecycle. Steps 2, 5 and 7 each rest on something Modal
does not do.

| §9 says                                                                              | Why it cannot work                                                                                                                                                                               | What replaces it                                                                                                                                                      |
| ------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 2. create "with the project's Modal Volume mounted", after a warm-pool hit in step 1 | `volumes=` is a **create-time-only** parameter. A sandbox that already exists — which is what a warm pool is — can never have a Volume attached. Steps 1 and 2 are mutually exclusive as written | Project state is a **directory snapshot**, mounted into the running sandbox with `mount_image('/project', …)`. A Volume, if used at all, holds only rebuildable cache |
| 5. register the tunnel URL in Workers KV                                             | Tunnel hostnames are cryptographically random and server-assigned, with no way to request or pin one. Every incarnation gets a new URL                                                           | KV is **rewritten on every incarnation and the old entry invalidated**. The stable name is ours, never Modal's                                                        |
| 7. "idle 15 minutes → snapshot, stop"                                                | Modal's `idle_timeout` has **no pre-termination hook**, and a merely-running `agentd` does not count as activity — so Modal would kill the sandbox _without_ snapshotting                        | `sandboxd` tracks idleness itself and drives snapshot-then-terminate. `idle_timeout` is only a wide backstop against `sandboxd` dying                                 |

And the word **resume** does not appear anywhere in this service for a reason:
there is no resume primitive in Modal. Waking a project means **creating a new
sandbox** and mounting its snapshot. A new sandbox id, a new tunnel URL, and a
cold `agentd`.

## The flow

### Session start

```
POST /sessions
  ├─ warm pool has a sandbox for this template image?
  │    ├─ yes → claim it (atomic, Redis)                          ~0ms
  │    └─ no  → Sandbox.create(image=Image.from_name(tpl:ver))    ~0.5–1s (v2, median; tail unmeasured)
  │              cpu=1.0, memory=4096          ← 1 physical core IS 2 vCPU
  │              timeout=<explicit>            ← the 300s default would kill the session
  │              idle_timeout=<wide backstop>  ← never the 15-minute trigger
  │              encrypted_ports=[dev server]
  │              inbound_cidr_allowlist=[our edge]
  │              outbound_cidr_allowlist + outbound_domain_allowlist  ← always BOTH
  │              readiness_probe=Probe.with_tcp(port)
  │              include_oidc_identity_token=True
  │              name=, tags={project_id, session_id}
  ├─ mount_image('/project', <project's directory snapshot>)      unmeasured
  │    └─ NotFoundError (snapshot expired/deleted) → clean clone from gitd
  ├─ reconcile: git fetch && git reset --hard <branch head>       DOMINANT COST
  │             lockfile hash changed? → reinstall
  ├─ agentd (already the entrypoint) is told to start opencode serve + dev server
  ├─ wait_until_ready()  ← readiness is the probe passing, not create() returning
  ├─ tunnels()[port] → rewrite KV for <branch>.<project>.preview.<domain>,
  │                    invalidate the previous entry
  └─ persist sandbox.object_id, the tunnel URL and the snapshot image id in Postgres
```

`agentd` is the sandbox **entrypoint** (`Sandbox.create('agentd', …)`), never an
`exec`. Three independent reasons, all verified: `ContainerProcess` has no
`from_id`, so an exec'd process cannot be reattached after `sandboxd` restarts;
only the entrypoint's logs are stored by Modal; and it makes the sandbox exit
code meaningful.

### While running

- Heartbeat every 10s → `usage_events` (§9 step 6, unchanged).
- `agentd` POSTs its event stream to `sandboxd` over authenticated HTTP.
  **`Sandbox.logs` cannot back the SSE stream** — it is fetch/tail only, and
  covers the entrypoint alone. It is a recovery and audit path.
- Every Modal call carries a **context deadline**, and `MaxThrottleWait` is set
  explicitly. The Go SDK's default on throttle is an unbounded silent wait; the
  Python client is better behaved but a hang is still the failure mode to design
  against. Map "throttled" to §18's amber _waiting on capacity_, never to a
  spinner.

### Idle, and the 24-hour ceiling

```
idle 15 min (sandboxd's own bookkeeping, NOT Modal's idle_timeout)
  ├─ snapshot_directory('/project', ttl=None)   ← ttl=None or it silently expires in 30 days
  ├─ record the new image id in Postgres        ← there is NO API to list snapshots;
  │                                               a lost row is an unlistable billed leak
  ├─ terminate()
  └─ invalidate the KV entry

approaching timeout (24h hard ceiling, no way to extend a running sandbox)
  └─ snapshot → create replacement → cut over → terminate old
```

The cutover is user-visible unless previews are addressed through a
`sandboxd`-owned stable URL that proxies to the current tunnel. **Design that
indirection now**, not at the 24-hour mark — it is also what makes step 5's
per-incarnation URL churn invisible.

## The state machine

`project_id` is stable and lives in Postgres. **`sandbox_id` is a disposable
per-incarnation value** and nothing may key off it across a stop. Same for the
tunnel URL.

What survives a stop: files in `/project`, via the snapshot. What does not:
every process, every bound port, the in-memory agent context, and the tunnel
URL. The console's "resuming" state may promise a **warm filesystem, not a warm
process** — and `agentd` cold-starts every single time.

Orphan reconciliation uses the stored ids plus `Sandbox.list(tags={project_id})`
— except that **v2 sandboxes are not returned by `Sandbox.list()`**, so stored
ids are the source of truth and enumeration is only a backstop.

## One design choice this service still has to lock in (task 1.3)

Project working-tree state is a **directory snapshot**, not a Volume. That is
the recommendation and the reasoning is in `docs/verified.md`, but it is a
decision task 1.3 makes explicit:

- A Volume cannot be attached to a warm sandbox at all (create-time only), so
  Volumes and a warm pool are mutually exclusive.
- Volumes v1 "work best under 50,000 files" with attach latency scaling
  **linearly**, and one mid-size `node_modules` is 30k–80k files.
- Volumes v2 lifts the count limit but Modal's own docs say "we don't recommend
  using Volumes v2 for mission-critical data at this time", and tree traversal —
  what `git status` and every file watcher do constantly — is **slower** than v1.
- `reload_volumes()` is unusable while a dev server holds files open ("volume
  busy"), and during a reload the volume **appears empty** to the container.

So: **a Volume is a rebuildable cache** (pnpm store, build artifacts) or it is
not used. Git remains the source of truth for user work, which is what §10's
`gitd` design already assumes.

## The §17 budget, honestly

SPEC §17 sets p50 < 10s for time to first preview. What is known:

- Container boot is **~0.5–1s at the median** on sandbox v2. No published tail.
- Snapshot create and restore are **completely unmeasured** — Modal publishes no
  timing at all. The one hard signal is that the SDK's default snapshot
  `timeout` is 55s and 1.4.3 added support for longer, i.e. it _can_ exceed 55s.
- `git fetch` + conditional reinstall is ours, and Modal's own guidance says
  this is normally the dominant cost.

**Task 1.19 measures the unmeasured part.** Until it has run, the SLO is a
target and not a commitment, and the p50 in §9's warm-pool sizing rule has
nothing behind it. Emit the `Created→Scheduled→Started→Ready` probe timings into
the §7.2 event stream so the console shows honest progress and we accumulate the
latency data Modal does not publish.

## Cost, because it changes the design

A 2 vCPU / 4 GB sandbox is **$0.238/hr — about $174/month if left running**.
Billing is per-second on `max(request, actual)` wall-clock, and Sandboxes are
billed at roughly **3× standard Function rates**. Ten idle warm sandboxes cost
~$57/day doing nothing.

Two consequences: request exactly `cpu=1.0` and no headroom, because
over-requesting is billed even when idle; and size the warm pool against that
number, not against latency alone. Modal has **no pool primitive** — the guide
only "suggests keeping pools of Sandbox IDs" — so the pool is ours to build and
ours to pay for either way. An open preview connection counts as activity, which
means **an abandoned browser tab keeps a sandbox warm and billing**.

## The §17 trust boundary, in this service specifically

Modal Secrets are injected as **plain readable environment variables**. There is
no masking and no read-back prevention — `os.environ`, `printenv` and
`/proc/self/environ` all work from inside the sandbox. Modal gives this boundary
no safety net, so it is structural here or it does not exist:

- **Nothing spendable in `secrets=` or `env=`, ever.** Prefer `secrets=` at
  `exec` time over create time so a token's lifetime is one command, not the
  sandbox's whole life.
- The sandbox holds a **non-spendable identity assertion** —
  `include_oidc_identity_token=True` gives it a `MODAL_IDENTITY_TOKEN` JWT
  signed by `https://oidc.modal.com` — presents it to the control plane, and the
  control plane maps `container_id` → `project_id` **from its own records**,
  never from a project id the sandbox claims.
- Treat that token as possibly long-lived: the docs state no TTL and no refresh
  behaviour, so the control plane sets a short expiry on what it mints and
  checks `exp`/`jti` replay.
- The egress allowlist is defence in depth, and its ceiling is in the threat
  model: **domain matching is TLS/443 SNI only** (so the app database on 5432
  needs a CIDR entry), and Modal documents **domain fronting** as a bypass, so a
  shared-CDN entry is an exfiltration channel.

A CI check belongs on this service: **no `Sandbox.create` or `exec` call site
may pass a spendable credential**, because Modal will not enforce that and a
reviewer will eventually miss it.

## Pinning

Pin `modal==1.5.5` exactly, set `MODAL_SANDBOX_V2` explicitly rather than
inheriting a default that flips in 1.6.0, and keep **every `modal.*` call behind
one adapter module** with its own integration tests — the Sandbox API changed in
every single release from 1.4.0 to 1.5.5. Run the tests with
`-W error::DeprecationWarning`: 1.5.5's release note says undocumented APIs are
removed in 1.6.0, and the client **silently falls back to v1** if a GPU, a
network file system or `pty_info` is requested.
