# Verified platform facts

SPEC §0 rule 3 and §22: confirm every item against **current vendor
documentation** before building on it, and record what you found here with a
date and a link. An unverified row is not permission to proceed.

Status values: `unverified` · `verified` · `contradicted` · `blocked`

## SPEC §22 checklist

| #   | Fact to verify                                                                                                     | Status     | Checked    | Finding                                                                                                                                                                                                                                                                                                                            |
| --- | ------------------------------------------------------------------------------------------------------------------ | ---------- | ---------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | `@opennextjs/cloudflare` - Next.js version support, unsupported features, ISR/caching                              | verified   | 2026-09-11 | Pin `next` **16.3.4** exactly and `@opennextjs/cloudflare` **1.20.6**. **Next 15 fails the build gate on 2026-10-21**; a missing major (17) fails too, so never `^16`. Console needs no cache bindings. Long-lived SSE is killed by weekly runtime updates - `Last-Event-ID` is load-bearing.                                      |
| 2   | Cloudflare Containers - memory/CPU limits, max request duration, long-lived SSE, pricing                           | verified   | 2026-09-11 | **Points away from Containers for the streaming services.** A container is fronted by a Durable Object, and Cloudflare documents that a streamed `fetch()` body does **not** keep a DO alive. Per-instance connection limits are unpublished. Decides §21.1 - see the brief in `docs/open-questions.md`.                           |
| 3   | Cloudflare for SaaS - custom hostname limits per zone, TLS issuance latency, apex support                          | unverified | -          | Blocks task 3.6                                                                                                                                                                                                                                                                                                                    |
| 4   | Cloudflare Hyperdrive - supported Postgres providers, connection limits, latency                                   | verified   | 2026-09-11 | **Not needed by 0.12** - it is a Workers binding and the console never touches Postgres (§7.1). Blocks tasks 3.9, 3.12. 0.6's `set_config(..., true)` RLS pattern **survives**. Caching is default-on with an undocumented key: use `--caching-disabled` for any multi-tenant database.                                            |
| 5   | Modal - Sandbox API, snapshot semantics, tunnel URL stability, volume perf, concurrency limits, non-Python client  | verified   | 2026-09-11 | Python `1.5.5` (wheel read). **No resume primitive**; resume is snapshot-and-recreate with a new id. Tunnel URLs are **not** stable. Deny-by-default egress **exists** (TLS/443 SNI only). A Go SDK now exists - §5.1's premise is false. Latency unmeasured: 1.x needs a benchmark.                                               |
| 6   | `opencode` - server API, config schema, MCP transport, plugin/hook surface; whether P1/P2/P3/P5 still need patches | verified   | 2026-09-11 | Pinned at `v1.18.30` (`3104c1428e`). **All six §11.2 patches have an upstream mechanism**; `agent/patches/` stays empty. P3 needs **two** env vars, one undocumented; repo `.opencode/plugin/` is RCE (§11.3 gap); `tokens.input` is cache-adjusted (§19 gap). Read, not run - tasks 1.7/1.8 must assert against a running server. |
| 7   | Neon - project/branch creation API, branch limits, autoscaling, pricing at thousands of projects                   | unverified | -          | Blocks SPEC §21 decision 3, task 5.4                                                                                                                                                                                                                                                                                               |
| 8   | GitHub Apps - fine-grained permission names, installation token TTL, per-installation rate limits                  | unverified | -          | Blocks task 5.12                                                                                                                                                                                                                                                                                                                   |
| 9   | Google Ads API - developer token process and wait time, basic vs standard access, manager linking                  | unverified | -          | **Long lead time. Start the application during phase 0.**                                                                                                                                                                                                                                                                          |
| 10  | Meta Marketing API - permission names, App Review requirements and timeline, Business Verification                 | unverified | -          | **Long lead time. Start during phase 0.**                                                                                                                                                                                                                                                                                          |
| 11  | Stripe Connect - recommended account type, onboarding requirements, SAQ-A applicability                            | unverified | -          | Blocks task 5.7; confirm SAQ-A with Stripe, don't assume                                                                                                                                                                                                                                                                           |
| 12  | Google Indexing API - current eligibility rules                                                                    | unverified | -          | Blocks task 6.11                                                                                                                                                                                                                                                                                                                   |
| 13  | LLM provider model identifiers, pricing, cache semantics                                                           | unverified | -          | Never hardcode. Config only. See SPEC §16.4                                                                                                                                                                                                                                                                                        |
| 14  | Auth.js / Better Auth - status, Drizzle adapter support, behaviour on Cloudflare Workers                           | unverified | -          | Blocks SPEC §21 decision 4, task 5.6                                                                                                                                                                                                                                                                                               |
| 15  | Whether Workers can host the generated Next.js apps with the driver chosen in decision 3                           | unverified | -          | Blocks task 3.9                                                                                                                                                                                                                                                                                                                    |

## Design system typefaces

Verified 2026-09-12 for task L.5, when `docs/mockup.html` was authored and the
console stopped using a system font stack. CLAUDE.md working agreement 2
applies to a new dependency the console downloads on every cold start, so the
families were confirmed to exist and the bytes were measured rather than
estimated.

| Family                         | On Google Fonts | Shape              | Latin subset   |
| ------------------------------ | --------------- | ------------------ | -------------- |
| **Atkinson Hyperlegible Next** | v7              | variable `400 700` | **33.2 KB**    |
| **Atkinson Hyperlegible Mono** | v8              | static 400, 600    | 17.3 + 10.5 KB |

**61.0 KB for the whole type system**, latin subset, woff2 (magic `wOF2`
confirmed on each file). One variable file covers all four weights the design
uses, which is why the sans costs one request rather than four.

Note the plain `Atkinson Hyperlegible` family — without `Next` — now **404s** on
the Google Fonts CSS API. It has been superseded, so a copy-pasted older
snippet will silently fail to load and fall through to the stack.

### `adjustFontFallback` does not apply to these families

`next/font/google` generates a metric-matched fallback `@font-face` only for
families in Next's own metrics database, and neither Atkinson family is in it.
**Verified by building and grepping the output**: no `Fallback` face appears,
and `adjustFontFallback: true` is silently a no-op.

That matters rather than being trivia. `display: swap` with no metric-matched
fallback reflows the page when the webfont lands, and in this console the reflow
lands on the credit gauge — top bar, every screen, a reserved slot for every
readout — which is exactly the jitter SPEC §18's persistent chrome must not
have.

So the fallback faces are authored by hand in `apps/console/src/app/globals.css`
from metrics measured off the real fonts with fontTools:

| Font                  | unitsPerEm | ascent | descent | avg lowercase advance |
| --------------------- | ---------- | ------ | ------- | --------------------- |
| Atkinson Next         | 1000       | 98.40% | 31.60%  | 48.673% of em         |
| Atkinson Mono         | 1000       | 98.40% | 31.60%  | 63.200% of em         |
| Arial (sans fallback) | 2048       | -      | -       | 48.954% of em         |
| Courier New (mono)    | 2048       | -      | -       | 60.010% of em         |

`size-adjust` is the ratio of those advances: **99.43%** for the sans,
**105.32%** for the mono. **Re-measure if either family is replaced** — a stale
`size-adjust` is worse than none, because it reflows in a direction nobody
expects.

`adjustFontFallback: true` is left set in `fonts.ts`. If a future Next version
adds these metrics, the hand-authored faces become redundant rather than wrong.

## SPEC §22 item 1 — `@opennextjs/cloudflare`

Verified 2026-09-11 for task 0.11. Where a fact decides a version pin it was read from the
**published package** — registry metadata and the extracted tarball — rather than from the
documentation, because on this item the two disagree and the documentation is the stale side.

| Package                  | Pin          | Why exactly this                                                                   |
| ------------------------ | ------------ | ---------------------------------------------------------------------------------- |
| `@opennextjs/cloudflare` | **1.20.6**   | Exact, not caret. The Next-version gate moves with every release                   |
| `@opennextjs/aws`        | 4.1.4        | Transitive, pinned exactly by the adapter. **Owns the support gate** below         |
| `next`                   | **16.3.4**   | Not Next 15 — see the expiry below. Not `^16` — see Next 17                        |
| `wrangler`               | 4.131.0      | Peer is `^4.125.0`; `engines.node >= 22.0.0` is the repo's binding Node floor      |
| `react` / `react-dom`    | 19.2.8       | Identical versions. 19.3.0 shipped 2026-09-09 — two days old, not a foundation pin |
| `tailwindcss`            | 4.3.3        | v4, build-time only. Nothing Tailwind reaches the Worker                           |
| `workerd`                | 1.20260911.1 | Sets the maximum usable `compatibility_date`                                       |

### Next.js 15 stops building in 40 days

`@opennextjs/aws` 4.1.4 hardcodes a release-date table and refuses any major more than two
years past its release:

```js
NEXT_RELEASE_DATES = { 16: "2025-10-21", 15: "2024-10-21", 14: "2023-10-26", ... }
// isNextVersionSupported() → false once now > releaseDate + 2 years
// checkNextVersionSupport() → process.exit(1)
```

So **Next 15 fails OpenNext's build gate on 2026-10-21**, forty days from this verification.
Vercel's own support policy agrees to the day (15.x Maintenance LTS "until Oct. 21, 2026"), and
npm's publish timestamp for `next@15.0.0` is 2024-10-21. A console pinned to Next 15 would
build cleanly today and then start failing CI — and task 3.1's sandbox build — with a bare
`process.exit(1)`.

**Pin Next 16.3.4.** Two further traps in the same mechanism:

- The peer range is `>=15.5.24 <16 || >=16.3.3`, so **16.0.0 through 16.3.2 are excluded**. The
  range has ratcheted forward every few releases, and the exact-patch floors are the signature
  of Next security backports. Treat the adapter and Next as a **matched pair** and bump them in
  one commit; an adapter bump can _force_ a Next bump rather than merely allow one.
- `NEXT_RELEASE_DATES` has **no `17` key**, and a missing major returns false rather than
  "unknown, allow". When Next 17 ships — late October 2026 on the observed cadence — the build
  breaks until a new adapter release. **Never write `^16`**: a routine `pnpm update` would pull
  17 and produce an error that reads like a policy complaint rather than a version problem.

There is an escape hatch, `--dangerouslyUseUnsupportedNextVersion`. Do not add it to a build
script. If it ever looks necessary that is a signal to bump the adapter, and it should surface
as a reviewed change rather than a flag someone added to make CI green.

### Where the docs are wrong

Worth stating plainly, because pinning from the documentation produces a broken install:

| Docs say                                       | The published package says                                  |
| ---------------------------------------------- | ----------------------------------------------------------- |
| "latest minors of Next.js 14 and 15" supported | No Next 14 at all; `>=15.5.24 <16 \|\| >=16.3.3`            |
| "Wrangler 3.99.0 or later"                     | `wrangler: ^4.125.0`                                        |
| Worker size limit 3 MiB free / 10 MiB paid     | **64 MiB uncompressed**, identical on both plans            |
| Node middleware "not yet supported"            | Implemented, but logs "experimental … use at your own risk" |

### Size is not the constraint; startup time is

The Worker ceiling is **64 MiB uncompressed** on both plans, and "there is no compressed size
limit". CodeMirror 6 does not threaten that, so SPEC §3.1's "smaller bundle" rationale for
CodeMirror over Monaco stands on client load time, not on the Worker limit. **Do not add a
bundle-size gate to CI on the strength of the stale 10 MiB number.**

The real ceiling is **1 second of CPU to parse and execute global scope**, rejected at deploy
as `Script startup exceeded CPU time limit` (error 10021). The adapter's
`routePreloadingBehavior` defaults to `"none"` and its own type doc warns that anything else
"can result in higher CPU usage on cold starts" — so leave it unset, and have task 0.12 capture
`startup_time_ms` from the wrangler deploy output.

### Streaming, and the limit that actually bites

Serving a stream from the console's own route handlers works and is not buffered: the
`cloudflare-node` wrapper declares `supportStreaming: true`, resolves the `Response` as soon as
headers are written, sets `retainChunks: false`, and plumbs the original `abortSignal` through
so `request.signal.onabort` fires on client disconnect.

Neither duration nor CPU is the constraint. Cloudflare imposes **no wall-clock limit** on an
HTTP-triggered Worker, and "waiting on network requests does not count toward CPU time" — an
idle SSE connection burns essentially nothing.

**The limit that bites is the runtime update cycle.** Cloudflare updates the Workers runtime a
few times per week and gives in-flight requests a **30-second grace period**, after which they
are terminated. A stream held open for hours _will_ be killed several times a week, by design.

That makes SPEC §3.1's `Last-Event-ID` resume load-bearing rather than a nicety. **Task 1.14's
SSE gateway must treat mid-stream termination as routine**, and the console must reconnect and
replay without surfacing an error.

Two consequences for the console specifically:

- **Do not proxy SSE through the console Worker.** Point the browser straight at the Go
  gateway. A Worker invocation may have at most **6 simultaneous outgoing connections** awaiting
  response headers, and each isolate is capped at **128 MB shared across all concurrent
  requests** — a proxy would spend one connection slot per viewer against both budgets.
- **A decision is needed before task 1.14, and it belongs to the gateway's auth design.**
  Native `EventSource` cannot set request headers, so it cannot carry
  `Authorization: Bearer`; it does send `Last-Event-ID` automatically on reconnect. `fetch`
  streaming can set headers but then the console owns `Last-Event-ID` bookkeeping by hand.
  Choosing `EventSource` means cookie auth plus CORS with `Access-Control-Allow-Credentials`
  and an explicit non-wildcard origin on the Go side. Recorded in `docs/open-questions.md`.

### The console needs no cache infrastructure

`defineCloudflareConfig()` defaults `incrementalCache`, `tagCache`, `queue` and `cachePurge` to
`"dummy"` and `enableCacheInterception` to `false`. None of it is required unless ISR or
on-demand revalidation is used, and the console is an authenticated dashboard — dynamic SSR plus
TanStack Query, no ISR surface.

So `apps/console/open-next.config.ts` is a bare `defineCloudflareConfig({})`, and **task 0.12
provisions no R2, KV, D1 or Durable Object queue for the console.**

The generated user apps (tasks 3.1 and 3.3) are a different matter and will need most of it:
R2 via `NEXT_INC_CACHE_R2_BUCKET` plus a `WORKER_SELF_REFERENCE` service binding whose service
name must equal the worker name; a Durable Object queue for time-based revalidation; D1 or
`DOShardedTagCache` for on-demand revalidation. Choose R2 over KV — Cloudflare and OpenNext both
say KV is eventually consistent and not recommended here. Note the self-reference binding makes
the wrangler config depend on the worker's own name, which matters for task 3.3's
one-versioned-Worker-per-project scheme.

### Runtime surface, and the one feature to avoid

For `compatibility_date >= 2026-08-04` Workers enables `nodejs_compat` and `nodejs_compat_v2`
by default. `node:fs`, `net`, `http`/`https`, `stream`, `crypto`, `zlib`, `process` and
`AsyncLocalStorage` are native. **The failure mode to design against** is the stub set —
`child_process`, `worker_threads`, `vm`, `cluster`, `http2`, `sqlite`, `dgram`, `tty`, `v8`,
`readline`, `repl`, `inspector` import _successfully_ and throw at call time, as does anything
unenv-polyfilled (`[unenv] <method> is not implemented yet!`). So a dependency touching those
must be refused at review, not discovered in staging.

Everything SPEC §3.1 and §18 need is supported: App Router, Route Handlers, dynamic routes,
SSG, SSR, edge middleware, PPR, ISR, `after()`, `'use cache'`, Turbopack. **Node.js middleware
is the exception** — implemented but explicitly experimental and unmaintained. Use edge
middleware if auth redirects ever need one.

React Server Components are the supported path, not a caveat: the adapter deliberately targets
Next's **Node.js** runtime because "the edge runtime does not support all Next.js features".
**Do not put `export const runtime = 'edge'` on any route** — that opts out of the runtime the
adapter is built for.

### `global_fetch_strictly_public` is a security flag

`wrangler.jsonc` needs `main: ".open-next/worker.js"`, a `compatibility_date`, an `assets` block
binding `.open-next/assets` to `ASSETS`, and
`compatibility_flags: ["nodejs_compat", "global_fetch_strictly_public"]`.

That second flag is **not cosmetic**. Without it, "requests to a Worker's own zone are routed to
the zone's origin server, ignoring any Workers mapped to the URL and also bypassing Cloudflare
security settings." A server-side fetch from the console to Halyard's own API domain would skip
the WAF and any Access policy in front of it — a quiet hole in §17's boundary, on a flag that
looks removable. It is commented in `wrangler.jsonc` for that reason.

### Build versus deploy maps onto §17 exactly

`build` needs no Cloudflare credentials; `preview`, `deploy` and `upload` shell out to wrangler
and do. Its only network call is a `workerd` version lookup, and that runs only when _creating_
a wrangler config — so a committed `wrangler.jsonc` removes it entirely and the build stays
clean under §17.1's deny-by-default egress.

That is precisely TASKS 3.3's "the sandbox never deploys": **the sandbox runs
`opennextjs-cloudflare build` only and never holds a Cloudflare API token**; the control plane
runs `deploy`. One trap for task 3.1 — a missing config file throws in CI but _prompts
interactively_ when a TTY is attached, so set `SKIP_WRANGLER_CONFIG_CHECK=yes` in the sandbox
build environment or a pty-allocating Modal sandbox will sit at a prompt forever instead of
failing.

`next dev` is unaffected; local development is ordinary Next.js. Keep
`opennextjs-cloudflare preview` as a pre-merge check rather than an inner loop.

### Library choices all survive

Tailwind v4 cannot break under OpenNext because none of it runs there — it compiles to plain CSS
at build time, and `@tailwindcss/oxide` ships prebuilt binaries for darwin arm64/x64, linux
gnu/musl on both arches, plus a `wasm32-wasi` fallback. The same pin works on the dev Mac, in CI
and in a Modal image whatever its libc.

CodeMirror 6, TanStack Query, Zustand, react-hook-form and zod are all pure JS with React 18/19
peers. CodeMirror is **import-safe under SSR** — `@codemirror/view` guards its module-scope DOM
access with `typeof` checks — so task 2.7 should put it behind `"use client"` and construct
`EditorView` in an effect, but must not reach for `dynamic(…, {ssr:false})` believing SSR would
crash. That belief would push 2.7 into a worse design.

### Two gaps this verification opened

- **SPEC §3.1 requires "zod schemas generated from `packages/schema`" and no such generation
  exists.** Task 0.3 emitted TypeScript types, Go and Python only. `json-schema-to-zod` 2.8.1
  emits zod v4 by default. 0.11 ships no forms so it does not need this yet, but the task that
  writes the first form must extend `make gen` rather than hand-write a schema — working
  agreement 1 forbids writing the same type twice.
- **Image optimization is an unmade decision.** Next's optimizer needs Cloudflare Images, which
  is paid, and the adapter's image path supports neither `minimumCacheTTL` nor
  `dangerouslyAllowLocalIP`. The console ships `images: { unoptimized: true }` because a
  configured-but-unbound optimizer 500s at runtime. Task 0.12 should decide explicitly rather
  than inherit the `images` binding from the adapter's template config, which enables it.

### Toolchain facts this pinned

- **TypeScript stays at 5.9.3, and 7.x would break `make gen-check`.** `typescript@7.0.2` is
  the native compiler: its package `exports["."]` is `./lib/version.cjs`, exporting only
  `{version, versionMajorMinor}`. `openapi-typescript` imports the classic compiler API at
  runtime and dies. The unblocker is named — openapi-typescript shipping TS 7 support — so this
  is a dependency-compatibility pin, not a judgement that TS 7 is unready. (tsc 7 handles this
  repo's strict options, `-b` project references and `jsx: preserve` fine; it also drops the
  tsconfig `plugins` array and ships no tsserver, so the Next IDE plugin cannot load under it.)
- **jsdom cannot be installed here.** jsdom 30's `engines.node` is
  `^22.22.2 || ^24.15.0 || >=26.0.0`; `.tool-versions` pins 22.18.0 and `.npmrc` sets
  `engine-strict=true`, so pnpm aborts. The Next.js docs prescribe jsdom; **use happy-dom**,
  which is also faster and runs axe correctly despite a stale warning in vitest-axe's README.
  Bumping Node to satisfy jsdom is a cross-cutting toolchain change and belongs in its own task.
- **`tsc --noEmit` fails on a clean checkout unless `next typegen` runs first**, because
  Next 16 generates `next-env.d.ts` and `.next/types/**` where the global `PageProps`/
  `LayoutProps` helpers live. `typecheck-js` runs both, in that order.
- **Do not unit-test an async Server Component.** Next.js documents them as unsupported, and
  the failure is silent: the component renders nothing rather than erroring, so a test asserting
  something is _absent_ passes vacuously. They belong in task 1.18's E2E test.

### What this does not establish

No Worker was deployed and no Cloudflare account was used. Everything above is read from
published package artifacts, Cloudflare's and OpenNext's current documentation, and the
registry. Specifically unmeasured: whether a stream held open past 60 seconds survives on a
deployed Worker (the adapter wraps the handler in `ctx.waitUntil`, whose 30-second cap
Cloudflare says does **not** apply while a client is still receiving a streamed body — but that
is prose, not a measurement). **Task 0.12 should hold one stream open past 60s against a real
deployment.** Until then the browser-to-Go-gateway path, which does not depend on it at all, is
the one to prefer.

## SPEC §22 item 2 — Cloudflare Containers

Verified 2026-09-11 for task 0.12, because SPEC §21 decision 1 depends on it.
Read from Cloudflare's current documentation, from the published
`@cloudflare/containers@0.3.7` tarball, and from the full Containers docs corpus
(`containers/llms-full.txt`, 287 KB, fetched whole) — that last one matters,
because two of the findings below are **absences**.

**Cloudflare Containers is GA**, since 2026-04-13, Workers Paid only. Maturity
is not the objection, and any argument from "it's beta" is out of date.

### The finding that decides §21 decision 1

A container is fronted by a **Durable Object**: `@cloudflare/containers`'
`Container` class extends `DurableObject`, one DO instance owns one container
instance, and every request goes Worker → DO → container. So every Durable
Object limit is a Containers limit.

Cloudflare's Durable Objects documentation says, verbatim:

> "This applies to outbound TCP sockets and outbound WebSockets... **It does not
> apply to plain `fetch()` subrequests. Those never keep the Durable Object
> alive, even while the response body is still streaming.**"

And `@cloudflare/containers@0.3.7` proxies a response as exactly that
(`dist/lib/container.js:953-960`):

```js
const res = await tcpPort.fetch(containerUrl, request)
if (res.body !== null) {
  const { readable, writable } = new IdentityTransformStream()
  res.body?.pipeTo(writable).finally(() => {
    this.decrementInflight()
  })
  return new Response(readable, res)
}
```

The `fetch` has already resolved and the handler has already returned while
bytes are still flowing. **A long-lived SSE stream proxied through a Cloudflare
Container is not documented to survive**, and SPEC §5.1 has `api` and `gitd`
holding thousands of them.

Marked as an inference of exactly one step: both halves are confirmed from
primary artifacts, but Cloudflare never writes the sentence "SSE through a
container will be cut."

It compounds. The SDK's keep-alive accounting is in-memory DO state
(`private inflightRequests`), and Cloudflare says in-memory state is discarded
on hibernation — so the stream breaks **and** the container may then be reaped
as idle.

**The obvious workaround is already closed.** Switching §3.1's transport to
WebSockets does not rescue this: the SDK calls `server.accept()`, not the
hibernation API, an outbound connection keeps a DO alive "for a maximum of 15
minutes", and "code updates disconnect all WebSockets". That trades an
undocumented ceiling for a documented 15-minute one, and bills for the whole
connection.

### The second finding is an absence, and it is decisive on its own

SPEC §3.5 offers Containers "**if** its current limits on memory, request
duration, persistent connections and long-lived SSE suit us."

- The Containers docs mention SSE **zero times**.
- **No per-instance concurrent-connection number is published anywhere.**
- The only bound on record: "A single instance of a Durable Object cannot do
  more work than is possible on a single thread", with four overload errors and
  **no threshold given for any of them**.

That condition cannot be evaluated from documentation, and working agreement 2
says an unverified row is not permission to proceed. Cloud Run, by contrast,
publishes 1,000 concurrent requests per instance and a 60-minute ceiling. **A
known bad number beats an unknown**, which is precisely the "more moving parts,
fewer unknowns" trade §3.5 already wrote down.

### Third: `gitd` wants things Containers does not have

- **All disk is ephemeral**, max 20 GB, lost on every sleep or restart.
  Snapshots are "coming soon". The documented persistence path is FUSE-over-R2
  with an explicit "should not expect native SSD-like performance".
- **Request bodies are capped by the Cloudflare account plan** — 100 MB on
  Free/Pro — because every container request passes through a Worker. A first
  `git push` of a large repo returns **413 before `gitd` sees a byte**.
- **No raw TCP ingress**: "end-users cannot make non-HTTP TCP or UDP requests to
  a Container instance."
- **No min-instances and no autoscaling.** `max_instances` is a ceiling only;
  the load balancer is a random pick over N fixed DO names.

### What does not decide it

- **Cost.** Six always-on services: Containers ≈ $68/mo (including ~$25 of
  Durable Object duration), Fly ≈ $36/mo, Railway ≈ $54/mo + $20 plan, Cloud Run
  ≈ $171/mo. Every one is rounding error beside the ~$174/mo per continuously
  running Modal sandbox in §22 item 5. Anyone reaching for cost here is reaching
  for the wrong lever.
- **Recycling.** Containers recycles on host restarts and the DO leg inherits
  the Workers runtime cycle — but Cloudflare is actually **kinder than Fly on
  deploys**: SIGTERM, up to 15 minutes to drain, then SIGKILL, against Fly's
  `kill_timeout` default of 5 seconds. `Last-Event-ID` resume is required on all
  four hosts, so it discriminates between none of them.
- **Data residency.** Here Cloudflare is the **best** of the four:
  `constraints.jurisdiction = "eu"` is a real shipped field. Worth recording
  because §22 item 5 found Modal stores snapshots in the US regardless.

### The alternatives, on the same constraints

|                           | SSE / duration ceiling                                        | Concurrency        | Disk                     | Egress       | ~6 services always-on |
| ------------------------- | ------------------------------------------------------------- | ------------------ | ------------------------ | ------------ | --------------------- |
| **Cloudflare Containers** | undocumented; the DO does not stay alive for a streamed body  | **unpublished**    | ephemeral, 20 GB         | included     | ~$68/mo               |
| **Fly.io**                | none documented — and **no timeout figures published at all** | not published      | real volumes $0.15/GB/mo | $0.02/GB     | **~$36/mo**           |
| **Railway**               | none published                                                | not published      | volumes $0.15/GB/mo      | **$0.05/GB** | ~$54/mo + $20         |
| **Cloud Run**             | **60 min hard** (5 min default)                               | **1,000/instance** | ephemeral + GCS FUSE     | GCP rates    | ~$171/mo              |

### What would have to be true for Containers to win

1. **The measurement contradicts the first finding.** Deploy one `lite`
   instance, hold SSE open for an hour, see whether it survives. This is cheap —
   one Workers Paid account, no new vendor — and it is the _only_ thing that
   would change the answer. If the small vendor surface is wanted, make §21.1
   **conditional on that experiment** rather than deciding on prose either way.
2. **The architecture changes so containers never hold the streams** —
   terminate SSE in a Durable Object using the hibernation API, with the Go
   services doing request/response behind it. That is Cloudflare's intended
   shape and it works, but it contradicts §5.1's premise, reopens §5.4, and is a
   redesign rather than a host choice. Named here so it is a visible option
   rather than something smuggled in later.
3. **`gitd` moves off the container host entirely.** That neutralises the third
   finding but neither of the first two.

### What this does not establish

No Cloudflare account was used and nothing was deployed. The central claim is a
one-step inference from two confirmed artifacts, and **the experiment in (1) is
the only thing that settles it**. `@cloudflare/containers` is also at 0.3.7,
published 2026-06-04 — pre-1.0, and three months without a release for a GA
product.

## SPEC §22 item 4 — Cloudflare Hyperdrive

Verified 2026-09-11 for task 0.12, from Cloudflare's current documentation
(including the raw `index.md` sources where the rendered page was ambiguous),
the published limits and pricing tables, the `wrangler hyperdrive` command
reference, and Cloudflare's own benchmark post.

### The headline: task 0.12 does not need Hyperdrive, and the checklist row was wrong

Hyperdrive is a **Workers binding**. The only Worker in 0.12 is the console, and
the console never touches Postgres — SPEC §7.1 puts every read and write behind
the Go `api` service, and the repository bears that out: there is no Postgres
client anywhere in `apps/console`. The Go services reach Postgres over pgx from
a container, where a Workers-only binding is unreachable by construction.

So the row's "Blocks tasks 0.12, 3.9" was wrong about 0.12. **Corrected to
"Blocks tasks 3.9, 3.12"** — the generated user apps on Workers, which is
exactly where `docs/TASKS.md` already files it.

Task 0.12 therefore provisions **no** Hyperdrive configuration and adds **no**
`hyperdrive` block to `apps/console/wrangler.jsonc`. A Worker holding the
control-plane database credential would invert §17.

### The good news, because it removes a real worry

**Task 0.6's row-level security pattern survives Hyperdrive.** Hyperdrive is a
transaction-mode pooler that pins one origin connection for a transaction's full
duration and RESETs it on return, so

```sql
set_config('app.org_id', $1, true)   -- is_local = true
```

followed by the query **inside the same pgx transaction** is exactly the
pooler-safe form. Tenant scoping does not need redesigning for it.

Marked `likely` rather than `confirmed` on purpose: Cloudflare documents the
pooling contract but never names RLS, `set_config` or session GUCs, and nothing
was measured. **Task 3.9 must pin it with an integration test** — set
`app.org_id` for org A in one transaction through a real binding, then assert a
following transaction scoped to org B sees none of A's rows. An assertion, not a
comment.

Hyperdrive is also **free on both Workers plans**, with no per-query, per-GB or
egress charge, so it adds no line to §19's margin model.

### The one genuine danger, for whoever does 3.9 and 3.12

**Query caching is default-on** — 60s `max_age`, 15s `stale_while_revalidate` —
Cloudflare states plainly that it does not invalidate on write, and **what the
cache key is composed of is nowhere documented**, including whether a session
GUC set by `set_config` participates in it.

If the key ignored `app.org_id`, a parameterised tenant-scoped `SELECT` cached
for org A would be served to org B: a **silent cross-tenant read**, the exact
failure §6 and §17 exist to prevent, and one no application test would catch
because the rows come back looking valid.

**Fail closed.** Any Hyperdrive config fronting a database that carries more
than one tenant's rows must be created `--caching-disabled`. Do not resolve this
by reasoning about what the key probably contains.

### The rest of what 3.9 and 3.12 will need, established now

- **Point at the DIRECT endpoint, never a `-pooler` host.** Cloudflare's own Neon
  guide says to uncheck connection pooling — Hyperdrive is itself the pooler.
- **Declare the binding under every named environment separately.** Bindings are
  not inherited, and the failure mode is staging writing to production. (The same
  rule the console's `wrangler.jsonc` comments about `vars`.)
- **Share one config across projects.** The per-account cap is **25** on Paid, so
  one config per generated project is exhausted almost immediately; per-project
  isolation must come from credentials or schema instead.
- **Test a write-then-immediately-read against a deployed Worker.** `wrangler
dev` bypasses Hyperdrive entirely, so the stale-read window cannot appear
  locally or in a unit test — it first appears in staging as an intermittent "my
  change didn't save".

### A throughput trade worth recording

Cloudflare explicitly recommends **against** wrapping queries in transactions on
Hyperdrive, and Halyard's `Scope` does so unconditionally, because §6's isolation
requires it. So every tenant read pins one of ~100 connections for its full
duration and most of Hyperdrive's multiplexing advantage is forfeited.

That is a throughput fact, not a correctness one, and the trade is not
negotiable in that direction. It is also a second, independent reason the
**control-plane** database stays behind Go `api` with a long-lived pgx pool
rather than behind a Worker. Note for 3.12: the generated-app template should
not adopt per-query transactions unless its own design needs them.

### What this does not establish

No Cloudflare account was used; no query, deployment or API call was made.
Nothing about cache-key composition, Postgres 18 compatibility, or same-region
latency is measured.

## SPEC §22 item 5 — Modal

Verified 2026-09-11 for task 1.1 against the **modal 1.5.5 wheel source**
(released 2026-08-28, latest on PyPI) plus current vendor documentation. Where
a fact is load-bearing it was read out of the wheel rather than taken from
prose; where only prose exists that is said explicitly.

Versions that matter:

| Thing                                   | Version                          | Note                                                                                      |
| --------------------------------------- | -------------------------------- | ----------------------------------------------------------------------------------------- |
| `modal` (Python)                        | **1.5.5**                        | `>=3.10,<3.15`. Pin exactly — see "the surface moves" below                               |
| `github.com/modal-labs/modal-client/go` | **v0.10.1** (2026-09-10)         | Beta. Breaking changes in 0.8.0, 0.9.0 **and** 0.10.0                                     |
| `modal-labs/libmodal` (old Go/JS home)  | archived 2026-07-18              | `github.com/modal-labs/libmodal/modal-go` is stale at v0.7.3; do not let it into `go.mod` |
| Sandbox backend                         | v1 default, **v2 opt-in** (Beta) | `MODAL_SANDBOX_V2=1`. Becomes the default in 1.6.0                                        |

### There is no resume, and SPEC §9's lifecycle depends on one

`resume` does not exist. Not deprecated, not experimental — grepping the whole
1.5.5 client for any `resume` symbol returns nothing, and the Sandbox method
set is `create` / `from_id` / `from_name` / `wait` / `poll` / `terminate` /
`detach`. `terminate()` is terminal: its docstring is "This is a no-op if the
Sandbox has already finished running."

**Resume must be modelled as snapshot-and-recreate, and the new sandbox has a
new id.** The mechanism is `snapshot_directory(path, ttl=...)` →
`Sandbox.create(image=...)` or `sb.mount_image(path, image)`. Modal's own
guidance points the same way: "If you need a Sandbox to run for more than 24
hours, we recommend using Filesystem Snapshots to preserve its state."

Three consequences, each of which changes a design rather than an
implementation detail:

- **`sandbox_id` is a per-incarnation value, not a project identity.** Key
  everything on `project_id`; store `sandbox.object_id` as disposable.
- **Only files survive.** Processes, bound ports, in-memory agent context and
  the tunnel URL are gone. The console's "resuming" must promise a warm
  filesystem, not a warm process, and `agentd` cold-starts every time.
- **Snapshots can only be taken from a _running_ sandbox** — both snapshot
  calls issue an RPC to the live container — so the order is snapshot, _then_
  terminate. That is what SPEC §9 already says; the point is that it is the
  only possible order, and it makes idle detection sandboxd's job.

Do **not** reach for memory snapshots to close the gap. They are
`_experimental_`-prefixed, they **terminate the sandbox in the act of taking
one**, restore "creates a new one, a clone, with its own identity", restore is
pinned to the same exact instance type, and they expire after a
**non-extendable 7 days** — with a re-snapshot inheriting the original expiry,
so a chain can never outlive the first. "The user comes back next month" alone
rules them out.

### Snapshots expire in 30 days by default, and cannot be listed

`ttl` defaults to `30 * 24 * 3600` on both `snapshot_filesystem` and
`snapshot_directory` (read at `modal/sandbox.py:1555` and `:1699`). Filesystem
snapshots used to persist indefinitely; **1.5.0 changed the default**. A
project untouched for 31 days silently loses its filesystem unless `ttl=None`
is passed explicitly. Modal's own Directory Snapshots blog still says "persist
indefinitely" and "30 days after last use" — it predates the change and is
wrong on both counts. It is 30 days from **creation**.

There is also **no API to list the snapshots you have created**. The control
plane is the only possible system of record: store the image id and its expiry
in Postgres beside the project. A lost row is an unrecoverable storage leak,
billed until the TTL runs out. And `NotFoundError` on resume is a first-class
path — fall back to a clean clone from `gitd` — not an unexpected error.

### Volumes and a warm pool are mutually exclusive

`volumes=` is a **`Sandbox.create` parameter only**. There is no documented way
to attach a Volume to a running sandbox; `mount_image`/`unmount_image` operate
on Images, not Volumes. So a pool of pre-booted sandboxes cannot have a
project's Volume attached at claim time. **Pick one: per-project Volumes with a
cold create, or a warm pool with directory snapshots.** Directory snapshots are
the better half of that trade — Modal says they are "mounted instantly, and
their contents are prioritized for pre-loading" — and they also decouple base
image rebuilds from user state.

Volume performance is independently disqualifying for a working tree. v1 "works
best when they contain less than 50,000 files", scales attach latency
**linearly** with file count, and has a hard **500,000 inode** ceiling that
fails with `ENOSPC`. One mid-size Next.js `node_modules` is routinely
30k–80k files. v2 removes the file-count limit but Modal states plainly: "we
don't recommend using Volumes v2 for mission-critical data at this time", and
tree traversal — exactly what `git status`, a bundler's watcher and a
dependency resolver do constantly — is **slower** than v1.

Two more Volume traps if one is used as a cache anyway: a reload makes the
volume **appear empty** to the container that initiated it, and fails with
"volume busy" if any file is open — so `reload_volumes()` is unusable while a
dev server or `agentd` is running. And `statfs` is fake: `df`,
`shutil.disk_usage()` and `os.statvfs()` return placeholder values, which
several package managers pre-flight against.

**Conclusion: Volumes are for rebuildable caches (pnpm store, build artifacts),
never for the only copy of user work.** SPEC's git-as-source-of-truth is the
right call and this is an independent reason for it.

### A running process does not keep a sandbox alive

Modal's definition of activity is narrow and does not include "a process is
running". A sandbox is active only if an `exec` is running, its stdin is being
written to, or **a tunnel has an open TCP connection**.

A warm-pool sandbox with `agentd` idling matches none of these and any
`idle_timeout` will reap it. Worse, `idle_timeout` has **no pre-termination
hook** — Modal kills without snapshotting, and that session's unsnapshotted
work is gone. So: leave `idle_timeout` as a wide backstop (30–60 min) against
sandboxd itself dying, and let sandboxd drive idle detection, snapshot and
terminate from its own bookkeeping.

Lifetime is capped regardless. `timeout` **defaults to 300 seconds** and is
capped at **24 hours**, with no documented way to extend a running sandbox.
Every `Sandbox.create` must pass an explicit `timeout` or a session dies
mid-turn, and sandboxd needs a scheduled snapshot-and-rotate before the 24h
ceiling — which is user-visible unless previews are addressed through a
sandboxd-owned stable URL that proxies to the current tunnel.

### `agentd` must be the entrypoint, not an `exec`

`Sandbox.from_id()` reattaches to a **sandbox** from any process — a pure
server lookup needing no App object — so sandboxd is free to restart or move
replicas. **`ContainerProcess` has no equivalent.** There is no `from_id`, its
constructor needs a private live `TaskCommandRouterClient`, and there is no RPC
to enumerate running execs. A process handle dies with the sandboxd process
that created it, and its buffered output dies with it.

Running `agentd` as the sandbox CMD fixes this three ways at once: it is
Modal's documented pattern for long-lived services, it makes the sandbox exit
code meaningful, and **only the entrypoint's logs are stored** (`Sandbox.logs`,
new in 1.5.5 — "only logs from the entrypoint process of a Sandbox are
currently stored, and streaming logs via this interface is not currently
supported").

That last limitation also settles a design question: **`Sandbox.logs` cannot
back SPEC §9's SSE stream.** It is fetch/tail only. The live stream must be
`agentd` POSTing to sandboxd over authenticated HTTP — which SPEC line 882
already specifies for telemetry, and should be widened to carry agent output.
`Sandbox.logs.tail()` is a recovery and audit path.

### Deny-by-default egress exists — this answers the §17.1 open question

`block_network`, `outbound_cidr_allowlist` and `outbound_domain_allowlist` are
all real parameters on `Sandbox.create` (`modal/sandbox.pyi:192-194`). The two
allowlists combine **additively** — "traffic that meets either criteria will be
let through" — and `*.` wildcards match parent and subdomains.

So SPEC §17.1's deny-by-default egress is implementable as a genuine
defence-in-depth layer: even if the sandbox somehow obtains a credential, the
allowlist stops it reaching `api.stripe.com`. **But the ceiling has to go into
the threat model, because it is lower than it first looks:**

- **Domain matching is TLS/443 only, by ClientHello SNI.** Modal does not
  decrypt: "the Host header, URL path, and body are never inspected." Anything
  that is not TLS on 443 — **Postgres on 5432 included** — needs a CIDR
  allowlist, which means resolving the DB host to CIDRs at launch or fronting
  it with HTTPS.
- **Domain fronting is documented as a bypass, by Modal.** "A Sandbox can reach
  a non-allowlisted domain there by sending an allowlisted SNI with the other
  name in the Host header... the allowlist itself does not prevent the
  mismatch." Any shared-CDN entry is a live exfiltration channel, and
  `*.github.com` grants every repo on GitHub, not the project's.
- **It is Beta and three months old** (`outbound_domain_allowlist` shipped
  1.5.0, 2026-06-09). Pin the client, add an integration test asserting a
  blocked destination actually fails, and treat a Modal-side behaviour change
  as a security regression.

Two implementation traps, both verified in source rather than docs:

1. **Always pass both allowlists on every call.** Python's
   `_experimental_set_outbound_network_policy` docstring says
   "outbound_cidr_allowlist: ... If None, all CIDRs are allowed", but the
   implementation is `allowed_cidrs=list(outbound_cidr_allowlist or [])` inside
   an `if either is not None` branch (`modal/sandbox.py:1449-1452`) — so
   updating **only** the domain allowlist silently sets the CIDR allowlist to
   empty and kills all non-443 egress. The Go SDK refuses the same call
   outright. The two SDKs are not behaviourally equivalent.

   The `Sandbox.create` path is a **different function** —
   `_build_network_access` (`modal/sandbox.py:250-269`) — and it is better
   behaved, which is worth knowing precisely because it makes the behaviours
   inconsistent: both-`None` is an explicit, deliberate `OPEN`, and
   `block_network=True` combined with either allowlist **raises**
   `InvalidError` rather than silently winning. But the same `list(x or [])`
   applies, so passing one allowlist and not the other still empties the other.
   The practical rule is identical on both paths; an **empty list on create is
   therefore a reliable, deliberate deny-all**, which is the right default for
   §17.

2. **Prefer an empty allowlist to `block_network=True`.** `block_network` also
   disables i6pn, is mutually exclusive with all three allowlist parameters,
   and forecloses sandbox-to-sidecar addressing.

Runtime narrowing ("broad during install, narrow for the agent run") exists but
is Alpha and underscore-prefixed, and **a dimension not set at creation can
never be narrowed later** — create with `["*"]` if narrowing is wanted. If it
is used, isolate it behind one function.

### Tunnel URLs are not stable, and are public by default

Tunnel hostnames are "cryptographically random" and server-assigned, with no
client-side way to request or pin one — `SandboxCreateParams` has no hostname
field. Combined with "there is no resume", the answer to §22's tunnel-stability
question is: **the URL changes on every create and every restore, and the KV
registration for a preview hostname must be rewritten each time and the old
entry invalidated.** (Marked as inference: Modal never states the negative, it
follows from the absence of a stop/start API and of any hostname parameter.
Confirm with one live experiment before 1.x locks the design.)

Modal's `custom_domain` does **not** solve this. The leading label stays
Modal-assigned, setup is **NS delegation** of a whole subtree to Modal's
nameservers plus a manual Slack request, and it is Team/Enterprise-gated.
Halyard's own edge keeps ownership of the hostname.

A tunnel URL is also public: "they are also public on the Internet, so anyone
can access your application if they are given the URL." Connect tokens are the
wrong shape for a preview a user opens in a browser (bearer-in-URL, metadata
capped at 512 chars and explicitly "should not contain secrets"). **Use
`inbound_cidr_allowlist` to make the tunnel reachable only from Halyard's own
proxy** and authenticate previews there. Note also that a tunnel is a raw TLS
TCP stream, not an HTTP reverse proxy — Modal does no L7 processing and adds no
`X-Forwarded-For`.

One useful interaction: an open tunnel connection counts as activity, so an
abandoned browser tab keeps a sandbox warm — and billing.

### Secrets are readable environment variables; OIDC is the §17 primitive

Modal Secrets are injected as ordinary env vars, and Modal's own guide reads
them with `os.environ['MY_PASSWORD']`. There is **no masking, sealing, or
write-only env** — nothing stops sandboxed code reading an injected value from
`os.environ`, `printenv` or `/proc/self/environ`. `env=` is the same thing
without encryption at rest.

**Modal provides no safety net for SPEC §17. The boundary is structural or it
does not exist.** The platform-shaped primitive that _does_ help is
`include_oidc_identity_token=True`, which injects a `MODAL_IDENTITY_TOKEN` JWT
signed by `https://oidc.modal.com` carrying `workspace_id`, `environment_id`,
`app_id`, `function_id` and `container_id`, verifiable against Modal's JWKS.
The sandbox presents that; the control plane maps `container_id` → `project_id`
**from its own records**, never from a project id the sandbox claims, and
returns a short-lived project-scoped token.

Two caveats: the identity token is still an env var the sandbox can read, and
its claims identify the **container**, not the project. The docs state no TTL
or refresh behaviour (the example token's `exp - iat` is 48h, which is not a
documented guarantee) — so the control plane must set a short expiry on what it
mints and check `exp`/`jti` replay rather than assume Modal rotates anything.

This also wants a repo-level check: **no `Sandbox.create`/`exec` call site in
sandboxd may pass a spendable credential in `secrets=` or `env=`.** That rule
has to be enforced here, because Modal will not enforce it.

### `cpu=1.0` is 2 vCPU, and a warm pool is the biggest line in §19

Modal's CPU unit is a **physical core**, which its own pricing page labels
"2 vCPU equivalent". SPEC's "2 vCPU and 4 GB" is therefore
**`cpu=1.0, memory=4096`**, not `cpu=2.0` — writing `2.0` provisions 4
vCPU-equivalent and raises CPU spend ~1.6× on the blended bill.

A bare scalar is only a **request** (a floor) and permits bursting to
request + 16 cores, which is billed; `cpu=(1.0, 1.0), memory=(4096, 4096)` is
the hard cap. Billing is per-second on `max(request, actual)` wall-clock while
the container is allocated, and **Sandboxes are billed at roughly 3× standard
Function rates** ($0.00003942/core/s + $0.00000667/GiB/s, against $0.0000131
and $0.00000222).

That arithmetic: **a 2 vCPU / 4 GB sandbox is $0.238/hr — $5.71/day, ~$174/mo
if left running.** Ten idle warm sandboxes cost ~$57/day doing nothing. This
belongs in SPEC §19's margin model before a warm pool is sized, and it argues
for measuring cold-create latency first — Modal has **no pool primitive at
all** (its guide only "suggests keeping pools of Sandbox IDs"), so the pool is
ours to build and ours to pay for either way.

What is _not_ documented: whether the meter starts at `create` or at container
start, and whether it stops at the `terminate()` call or at confirmed teardown.
Model it conservatively as request-rate × wall-clock-alive and check it against
a real invoice in phase 1.

### Concurrency is plan-gated at 100, and throttling is a silent hang

Starter ($0) is **100 containers**; Team ($250/mo fixed) is **5000**;
Enterprise is custom. The "millions of concurrent sandboxes" in Modal's v2 blog
is a platform capability claim, not an account entitlement — do not quote it in
a capacity plan. 100 is a real phase-1 ceiling and it covers _every_ Modal
container, so SPEC §14's Playwright/Lighthouse crawler shares it. **The $250/mo
Team floor should be a fixed cost in §19 from day one, not a later upgrade.**

Workspace operations are separately rate-limited to **200 calls/s** for a new
account (burst ×5s), returning 429, with no self-service raise.

The throttling behaviour is the sharp edge: **the Go client silently waits, and
the wait is unbounded.** The server attaches an `RPCRetryPolicy` with
`RetryAfterSecs`; the client's interceptor sleeps and retries with
`// don't increment attempt; server-driven retries are unlimited`. The only
bounds are `MODAL_MAX_THROTTLE_WAIT` (a nil pointer unless set) and the
caller's context deadline. The default is the worst of both worlds — not an
error, not a success, a hang.

**Every Modal call needs a context deadline and an explicit `MaxThrottleWait`**,
mapped to a user-visible amber "waiting on capacity" state per SPEC §18.
Related: `codes.ResourceExhausted` is **not** in the Go SDK's retryable set and
the Go SDK has **no typed error** for quota exhaustion, where Python has
`modal.exception.ResourceExhaustedError`.

### Cold start is ~0.5s at the median, and every other number is unmeasured

The only published figures are medians: "Containers boot in about one second"
and, on sandbox v2, "less than half a second at the median". There is **no
published p90/p99**, no number for creating from a pre-baked image under load,
and **no published timing at all for taking or restoring a snapshot** — the
docs corpus contains only "optimized for performance" and "mounted instantly".
The nearest hard evidence is the SDK's own `timeout=55` default and changelog
1.4.3 noting support for "setting a `timeout=` longer than 55s when necessary",
which is direct evidence snapshot creation **can exceed 55 seconds**.

Modal's own product post makes the right point: boot time "is usually the
smallest part of what your users actually wait through", and names `git clone`
and `npm install` as the dominant Started→Ready cost. **Budget the sandbox as
~0.5–1s of SPEC §9's p50 < 10s, not most of it.** The rest is the reconcile
step, and that is ours.

**Task 1.19 is that benchmark**, and its harness has landed in this branch:
`services/sandboxd/src/halyard_sandboxd/bench/` measures p50/p95/p99 for
`snapshot_filesystem`, `snapshot_directory`, `Sandbox.create` from each, and
`mount_image` into a running sandbox, reporting each phase separately so the
split between Modal's share and ours is visible. It has **not been run** — that
needs a real workspace and spends money.

Run it **before** tasks 1.3 and 1.5, not after: it depends on nothing from
them, and its numbers are what should settle 1.3's snapshot-versus-Volume choice
and 1.5's pool sizing. Do not commit to a resume-latency SLO before it exists.

`readiness_probe` + `wait_until_ready()` are GA and are the right instrument:
`modal.Probe.with_tcp(port)` on the dev server port makes "resumed" mean
"actually usable", and the Created→Scheduled→Started→Ready lifecycle events
give the console honest progress instead of a spinner.

Region pinning works (`us`/`eu`/`ap` broad, `us-east` etc. narrow) but **narrow
regions cost 1.75× and worsen cold start** — the two things a warm pool exists
to fix — against 1.15× for broad. Prefer broad regions for the pool.

### SPEC §5.1 rests on a fact that is no longer true

> "Modal's SDK is Python. There is no supported Go SDK, so an all-Go backend
> would need a Python sidecar for sandbox orchestration anyway."

**This is now false.** `github.com/modal-labs/modal-client/go` v0.10.1 was
published 2026-09-10, lives in the same first-party monorepo as the Python
client, is documented on Modal's own docs site, and its `SandboxCreateParams`
covers everything SPEC §9 needs — create, exec, snapshots, tunnels, volumes,
readiness probes, network policy.

The **conclusion** still holds, on grounds the spec did not cite:

- The Go SDK is **pre-1.0 Beta** with breaking changes in 0.8.0, 0.9.0 and
  0.10.0 — roughly every six to eight weeks.
- **Functions remain Python-only** by Modal's own statement ("Python remains
  the only supported Function runtime"), and SPEC §14's crawler is a Function.
  Python does not leave the stack either way.
- Python has a typed `ResourceExhaustedError` and a complete `.aio` surface;
  Go has neither.

So: keep `sandboxd` in Python, and **correct §5.1's stated reason** to "the
Go SDK exists but is pre-1.0 and not at parity".

**Resolved 2026-09-11 by the human: `sandboxd` stays Python — a Python sidecar
for Modal, as specified.** §21 decision 2's resolution clause is corrected to
cite the real grounds, and the decision now carries a re-examination trigger it
did not have before: when `modal-client/go` reaches 1.0 and parity, the question
is live again. SPEC §5.1's line is left as written and recorded as an erratum,
because the SPEC is the contract. See `docs/open-questions.md` Q8.

### The surface moves, so isolate it

Every release from 1.4.0 to 1.5.5 changed something Sandbox-affecting: new
filesystem API and OIDC tokens (1.4.0), readiness probes (1.4.1),
`unmount_image` (1.4.2), inbound/outbound CIDR allowlists and snapshot images
as root filesystems (1.4.3), named Images and domain allowlists and **two
breaking TTL changes** (1.5.0), port-scoped connect tokens (1.5.1), blocking
`reload_volumes` (1.5.2), the V2 backend (1.5.4), `Sandbox.logs` (1.5.5).
Unreleased in `CHANGELOG_DEV.md`: `Sandbox.open/ls/mkdir/rm/watch` and
`modal.file_io.FileIO` **removed**.

1.5.5's own release note: "We are deprecating a number of undocumented APIs on
object types... will be removed in version 1.6.0." That directly threatens
every `_experimental_` call.

Three rules follow:

1. **Every `modal.*` call goes behind one adapter module** with its own
   integration suite, so an SDK bump is one file to re-verify.
2. **Run sandboxd's tests with `-W error::DeprecationWarning`** so removals
   surface in CI, not production.
3. **Set `MODAL_SANDBOX_V2` explicitly** rather than inheriting a default that
   flips in 1.6.0 — and note the client **silently falls back to v1** if a GPU,
   a network file system or `pty_info` is requested
   (`modal/sandbox.py:703`). Never assume you got v2 because you asked. V2
   sandboxes are also **not returned by `Sandbox.list()`**, so orphan
   reconciliation must use stored ids, not enumeration.

Also: do not pass `environment_name=`, `pty_info=` or `cidr_allowlist=` — all
three are deprecated in 1.5.5 and slated for removal in 1.6.0.

### One thing to tell the business, not the code

**Snapshots are stored in the United States regardless of where the workload
runs.** A user's whole project filesystem — source, any `.env` written to disk,
build output — leaves its region the moment it is snapshotted. If Halyard ever
claims EU data residency, snapshotting breaks that claim. The only mitigation on
offer is an Alpha customer-supplied-encryption-key option
(`_experimental_encryption_key` on `snapshot_directory`/`mount_image`). Flagged
for SPEC §21.

**Correction, because an earlier draft of this section conflated two features:**
the "cannot pin a region" restriction belongs to **memory** snapshots
(`_experimental_enable_snapshot=True`), alongside their same-instance-type
restore and no-GPU limits. **Filesystem and directory snapshots place no
restriction on `region`** — `_experimental_create`'s own docstring lists region
placement and filesystem snapshots as both supported, and there is no
client-side guard tying them together. Only the US-residency point applies to
both. This matters practically: it means the warm pool can pin a broad region
_and_ snapshot, which the merged version of the sentence appeared to forbid.

### What this does not establish

No Modal account was used and **no API call was made**. Everything above is
read from the 1.5.5 wheel source, the Go SDK source and tags, Modal's
documentation, changelogs and pricing page. Specifically still unmeasured and
unconfirmed:

- Snapshot create/restore latency. **The harness now exists** —
  `services/sandboxd/src/halyard_sandboxd/bench/`, run via
  `python -m halyard_sandboxd.bench` — and it has never been run: it needs a
  real workspace, costs money, and is gated behind explicit opt-in. Task 1.19
  is the run. Until then every latency statement in this section is a doc
  quote, not a measurement.
- That a blocked destination actually fails — assert it in 1.3's tests.
- Tunnel URL instability across a snapshot cycle (inferred, not stated).
- Whether Sandboxes count against the plan container cap (inferred from "Each
  Modal Function or Sandbox container...", never stated outright).
- Whether `Sandboxes.Create` is metered on the 200/s Function-call bucket.
- Per-workspace Volume and concurrent-tunnel limits, and the undocumented
  max CPU/memory per sandbox.

Those six are in `docs/open-questions.md`. Under working agreement 4 an
inference is not permission to size a capacity model against it.

## SPEC §22 item 6 — `opencode`

Verified 2026-09-11 for task 1.1, by **reading the pinned source** rather than
the upstream project's documentation. The submodule is
`agent/opencode` at `anomalyco/opencode`, tag `v1.18.30`, commit
`3104c1428e`, released 2026-09-09. Everything below is a fact about that
commit; a later tag must be re-checked.

### The headline: none of the six patches looks necessary

SPEC §11.2 lists six patches to a fork, each with a stated reason it "cannot be
config". On the pinned tag, **all six have an upstream mechanism**. §11.1's own
rule — "Anything achievable by configuration must not be a patch" — therefore
points at not forking at all.

| #      | §11.2's stated reason                                    | What the pinned tag actually has                                                                                                                                                         | Verdict                                       |
| ------ | -------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------- |
| **P1** | "upstream doesn't expose them in the shape we need"      | `AssistantMessage.tokens` = `{ total, input, output, reasoning, cache: { write, read } }`, plus `cost`. That is the shape, exactly — **but the fields do not mean what they look like**. | **no patch — read the metering trap below**   |
| **P2** | "must interrupt a running turn"                          | `POST /api/session/{sessionID}/interrupt` (v2), or legacy `POST /session/{sessionID}/abort`                                                                                              | **no patch**                                  |
| **P3** | "the user's repo must not be able to relax the policy"   | `OPENCODE_PERMISSION` env var, merged **after every config file** — but it is **not sufficient alone**                                                                                   | **no patch — two env vars, one undocumented** |
| **P4** | "every token must be metered and capped"                 | `provider.<id>.options.baseURL` in config                                                                                                                                                | **no patch**                                  |
| **P5** | "`agentd` commits checkpoints at turn boundaries"        | Turn **start** is a blocking `chat.message` hook. Turn **end** has no blocking hook — only fire-and-forget events.                                                                       | **no patch — but P5 changes shape**           |
| **P6** | "the UI contract must not drift with upstream refactors" | A documented OpenAPI contract with 94 event types                                                                                                                                        | **no patch — but see below**                  |

### P3 needs two environment variables, and `OPENCODE_PERMISSION` alone is not enough

`OPENCODE_PERMISSION` carries a JSON permission config, and
`packages/opencode/src/config/config.ts:559` applies it **after** the entire
merge chain: global config → `$OPENCODE_CONFIG` → project `opencode.json` files
walking up to the worktree → `.opencode/` directories. Nothing the repository
contains can merge over it, because nothing merges after it.

**That is true and still insufficient.** Permission _rules_ are not evaluated by
merge order — they are evaluated with `findLast`
(`permission/index.ts:32` and `:210`), so the **last matching rule in a
concatenated list wins**. And `Permission.merge` is literally
`rulesets.flat()` (`permission/index.ts:200-202`). Follow what that means for a
repo-defined agent:

```ts
// agent/agent.ts:277 — a config-defined agent starts from the user ruleset
permission: Permission.merge(defaults, user) // `user` carries OPENCODE_PERMISSION
// agent/agent.ts:293 — then its own rules are appended AFTER
item.permission = Permission.merge(item.permission, Permission.fromConfig(value.permission ?? {}))
```

Those agents come from the repository. `ConfigAgent.load` globs
`{agent,agents}/**/*.md` under each discovered `.opencode` directory
(`config/agent.ts:13`) and merges the markdown frontmatter into `result.agent`
(`config/config.ts:474`). **So a hostile repo ships
`.opencode/agent/helper.md` with relaxed frontmatter permissions, its rules
land last, `findLast` picks them, and the sandbox policy is gone.**

`OPENCODE_DISABLE_PROJECT_CONFIG=1` is what actually closes this — but **not
where you would expect, and the distinction matters for what to watch on an
upstream bump.**

`config.ts:420` guards only the upward `opencode.json` / `.jsonc` walk. The loop
that loads the repository's agents, commands and plugins (`config.ts:438`,
calling `ConfigAgent.load` at `:474`, `loadMode` at `:475` and
`ConfigPlugin.load` at `:478`) sits **outside** that guard. Those are suppressed
only **indirectly**, because `ConfigPaths.directories` (`paths.ts:27`) stops
contributing the repository's `.opencode` paths when the flag is set, so the
loop has nothing repo-controlled to iterate.

The security conclusion is unchanged — with the flag set, all of it is
suppressed — but **`paths.ts:27` carries more of §17's weight than
`config.ts:420` does.** An upstream change that fed that loop from anywhere
other than `ConfigPaths.directories` would re-open the escape with the flag
still set, and it would not touch the line anyone would think to check. The pin
test asserts the chain, not just the flag.

The home-rooted `~/.opencode` scan is deliberately outside the guard
(`paths.ts:34-38`, `start` and `stop` both `Global.Path.home`), so Halyard's own
globally-installed supervisor plugin still loads. That is exactly the split we
want.

One route into that same loop is **not** guarded at all:
`Flag.OPENCODE_CONFIG_DIR` is appended to the directory list unconditionally
and `config.ts:439` treats it like a `.opencode` directory. That is a
Halyard-side invariant rather than an upstream defect — **never point
`OPENCODE_CONFIG_DIR` at anything the repository can write** — and it is
recorded in the pin test's failure text.

**Both variables are required, and one of them is undocumented.**
`OPENCODE_DISABLE_PROJECT_CONFIG` does not appear in the CLI documentation's
environment-variable table; it exists only in source. Under working agreement 4
that goes to `docs/open-questions.md` and gets pinned by a test, so an upstream
rename fails CI instead of silently opening the sandbox.

Two further caveats, stated rather than assumed away:

- `OPENCODE_PERMISSION` is applied with `mergeDeep`, not a replace. A repo can
  still add permission keys our policy does not mention, so the policy needs a
  catch-all deny rather than an enumeration of what is forbidden.
- §11.2 names `AGENTS.md` alongside repo config as untrusted input. `AGENTS.md`
  carries **instructions**, not permissions — it cannot alter the permission
  config, and no patch would stop it influencing the model's behaviour. That is
  prompt injection, which is a different and unsolved problem; conflating the
  two would make P3 look like it solves more than it does.

### A correction: `permission.ask` is declared but never fires

An earlier reading of this section claimed `permission.ask` gave a second,
independent lever for P3. **That was wrong, and it is worth recording as a trap
rather than quietly deleting.** The hook is declared in the public plugin types
with a vetoing signature:

```ts
// packages/plugin/src/index.ts:261
"permission.ask"?: (input: Permission, output: { status: "ask" | "deny" | "allow" }) => Promise<void>
```

Hooks are dispatched by exact string literal through `Plugin.trigger(name, …)`,
and enumerating every dispatch site in `packages/opencode/src` yields:
`chat.headers`, `chat.message`, `chat.params`, `command.execute.before`,
`shell.env`, `tool.definition`, `tool.execute.after`, `tool.execute.before`,
and four `experimental.*`. **`permission.ask` is not among them — zero call
sites.** Every other match in the tree is `permission.asked`, the past-tense
observe-only _event_.

Reading the type definitions alone would produce a permission veto that never
fires and a test suite that never notices. The working interception point is
**`tool.execute.before`, which denies by throwing** — documented, dispatched,
and awaited in sequence before the tool runs. That is the right second layer
under §17, and unlike config precedence it does not depend on merge order at
all.

### The repo can execute arbitrary code, which §11.3 does not contemplate

§11.3 warns that the repository's `AGENTS.md` is untrusted input. The sharper
edge is that **opencode loads and runs plugins from the repository itself**.
`ConfigPlugin.load(dir)` (`config.ts:478`) auto-discovers `.opencode/plugin/`,
and the repo's `opencode.json` can name npm packages in its `"plugin"` array.
Plugin code receives `$` — a Bun shell handle — and `client`, the fully
authenticated server SDK. That is arbitrary code execution **entirely outside
the tool-permission system**: no permission rule is consulted, because plugins
are not tools.

So `OPENCODE_DISABLE_PROJECT_CONFIG=1` is load-bearing for **§17**, not merely
for P3. Without it, opening a user's repository executes that repository's
JavaScript at startup.

`OPENCODE_PURE` is not the answer here: it disables **all** external plugins,
including Halyard's own supervisor plugin. It remains useful for a sandbox
image that must not reach npm at runtime, but it cannot be the policy
mechanism.

### The metering trap in P1's numbers

`tokens.input` is **not** the provider's input count. `session/session.ts:364`:

```ts
const adjustedInputTokens = safe(inputTokens - cacheReadInputTokens - cacheWriteInputTokens)
// …
input: adjustedInputTokens,
output: safe(outputTokens - reasoningTokens),
reasoning: reasoningTokens,
cache: { read: cacheReadInputTokens, write: cacheWriteInputTokens },
```

Both headline fields are already net of something. For §19's ledger:

- **billable input = `input + cache.read + cache.write`**
- **billable output = `output + reasoning`**

`total` is present too, and is **not** the sum of those fields — it is the
provider's own `usage.totalTokens`, passed through unadjusted. Do not reconcile
one against the other; pick the components and sum them yourself.

Treating `tokens.input` as the provider's input count under-bills by the entire
cache volume — which on a cached agent loop is the _majority_ of input tokens.
opencode also normalises cache-write across providers that report it only in
metadata (Anthropic, Vertex, Bedrock), so the field is trustworthy once summed
correctly. This belongs in task 1.13's `aigw` accounting tests as an explicit
assertion, not as a comment.

### P5 splits: turn start can block, turn end cannot

`chat.message` is genuinely awaited — `Plugin.trigger` runs it as
`yield* Effect.promise(async () => fn(input, output))` — so a plugin **can**
hold a turn open at the start while a checkpoint commits. There is no
equivalent at the end: `session.idle` and `session.status` are events, and the
plugin `event` hook is dispatched fire-and-forget. **A plugin cannot hold a
turn open to swap a write lease.**

So P5 becomes zero patches but a different design: `onTurnStart` is a blocking
`chat.message` hook; `onTurnEnd` is `agentd` serialising on the event stream —
it waits for `session.idle`, commits, swaps the lease, and only then submits
the next prompt. **That makes an invariant explicit for task 2.8: if anything
other than `agentd` can POST a prompt, the lease swap races.**

One caveat for §11.4, which requires flushing dirty human edits "before the
agent reads files": `chat.message` fires _after_ the message's file and
`@`-mention parts have been resolved and read from disk. Tool-driven reads
(`read`/`grep`/`glob`) all happen after the hook, so the main data-loss
scenario is covered — but explicitly attached files are snapshotted
pre-checkpoint. If that matters, `agentd` should checkpoint when it accepts the
prompt, before forwarding it.

### opencode already checkpoints, which overlaps §12.1

Worth knowing before building `gitd` checkpoints from scratch: opencode
snapshots each turn into a **separate git directory**
(`<data>/snapshot/<projectID>/<hash(worktree)>`), driven as
`git --git-dir <that> --work-tree <worktree>`, so it never writes to the user's
own `.git`. Each assistant message carries `snapshot: { start, end, files }`,
and `POST /session/{id}/revert` and `/unrevert` exist.

This does **not** replace §12's `refs/checkpoints/<session>/<turn>` naming or
the semantic-commit layer, and §12's checkpoints must live in `gitd` where the
control plane can reach them. But it removes the need to re-derive per-turn file
diffs, and task 1.12 should evaluate reusing the `snapshot.files` list rather
than recomputing it.

### P6 is "no patch" for a different reason

The others are "upstream already does this". P6 is "patching upstream is the
wrong fix". The stated goal — the UI contract must not drift — is real, but a
patch that makes opencode emit our event shape has to be rebased forever,
while an **adapter in `agentd`** mapping 94 documented event types onto §7.2's
contract does not. The adapter is also where the mapping is testable against
`agent-events.schema.json`, which a patch inside a vendored fork is not.

The event vocabulary maps cleanly: `EventSessionNextPromptAdmitted` → turn
started, `EventSessionNextTextDelta` → message delta,
`EventSessionNextToolCalled`/`ToolSuccess`/`ToolFailed` → tool started/finished,
`EventSessionError` → error, `EventSessionIdle` → turn finished.

### What this does not establish

**The source was read, not run.** Each verdict is a reading of the pinned
commit, and reading tells you what the code intends rather than what it does.
Before a patch is formally struck off, task 1.7 or 1.8 should assert the
behaviour against a running `opencode serve`:

- that `OPENCODE_PERMISSION` genuinely survives a hostile `opencode.json` and a
  hostile `.opencode/opencode.json` in the repository;
- that `interrupt` stops a turn mid-stream rather than after the current step;
- that `tokens` is populated on the events `agentd` actually consumes, not only
  on a message fetched afterwards;
- that `baseURL` routes **every** provider call, including any model-listing or
  auth probe made at startup.

- that a hostile fixture repo containing `.opencode/opencode.json`,
  `.opencode/agent/evil.md` **and** `.opencode/plugin/evil.ts` cannot relax the
  policy and cannot get its plugin executed, with both env vars set.

Until those exist, the honest status is "no patch appears necessary", not "no
patch is necessary".

**Resolved 2026-09-11 by the human for P3 specifically: use both environment
variables, and pin the undocumented one.**
`tests/contracts/test_opencode_upstream_contract.py` asserts every mechanism
above still exists in the pinned source, so an upstream rename fails CI rather
than silently opening the sandbox. That pins the **mechanism**; tasks 1.7/1.8
still own the behavioural assertion against a running server. See
`docs/open-questions.md` Q9. The P3 precedence chain in particular — `OPENCODE_PERMISSION`
merged last → remeda source-wins → `findLast` evaluation → agent rules appended
after → project config suppressed by `OPENCODE_DISABLE_PROJECT_CONFIG` — is a
five-step inference. Each step is individually confirmed in source; the chain has
not been executed end to end.

### Why not fork, quantified

§11.1 already says configuration beats patching. The pinned tag puts numbers on
it. opencode ships roughly **one release every 1.4 days** — 45 releases between
v1.17.16 (2026-07-09) and v1.18.30 (2026-09-09). Over the last 90 days the exact
files the six patches would touch churned hard: `session/prompt.ts` 54 commits,
`session/session.ts` 36, `session/tools.ts` 33, `permission/index.ts` 27,
`config/config.ts` 21. The codebase is also mid-migration to Effect-TS, so
patches would sit on top of ongoing structural refactors.

By contrast the plugin contract — `packages/plugin/src/index.ts` — has not
changed since 2026-06-01, and its recent history is purely additive. **The stable
surface is the one we were told to patch around.** Rebasing six patches across
~30 releases a month, any one of which is security-relevant, is a recurring
multi-hour task with a real chance of silently mis-applying the policy patch.

### The upstream moved

`github.com/sst/opencode` now 301-redirects to `github.com/anomalyco/opencode`
(same team, rebranded from SST to Anomaly Innovations), and the default branch is
**`dev`**, not `main`. Checked rather than assumed: `.gitmodules` already points
at the `anomalyco` path, and no §-citation in this repository names a GitHub URL
— so **nothing needs renaming**. This note exists only so the rebrand is not
rediscovered as a surprise. CI tooling that tracks upstream should target `dev`
semantics.

### Other facts about the pinned tag

- **Server API**: 162 paths under an OpenAPI 3.1 document at
  `packages/sdk/openapi.json`, which is the contract `agentd` should generate a
  client from rather than hand-writing one.
- **Server auth**: `OPENCODE_SERVER_USERNAME` / `OPENCODE_SERVER_PASSWORD`,
  so the loopback port `agentd` talks to need not be unauthenticated.
- **MCP**: `mcp` is a first-class config key with two transports.
  `McpLocalConfig` spawns a command over stdio; `McpRemoteConfig` takes a `url`
  and an arbitrary `headers` map — which is precisely what §13 needs, since the
  sandbox connects to the control plane's MCP server over HTTP carrying its
  session JWT as a bearer token. No patch, and no stdio bridge.
- **Plugin API**: `packages/plugin` exports a `Hooks` interface. The hooks
  that are actually **dispatched** are `chat.headers`, `chat.message`,
  `chat.params`, `command.execute.before`, `shell.env`, `tool.definition`,
  `tool.execute.before`/`after` and four `experimental.*` — see the
  `permission.ask` correction above before trusting the type definitions.
  Halyard ships **one** supervisor plugin, installed globally in the sandbox
  image at `~/.config/opencode/plugin/` so it survives
  `OPENCODE_DISABLE_PROJECT_CONFIG`.
- **`chat.headers` is relevant to P4** and was not examined here. Re-check it
  before assuming P4 needs the `baseURL` route at all — task 1.9 owns that.
- **`OPENCODE_PURE`** skips external plugin discovery and installation — useful
  for a sandbox image that must not reach npm at runtime, given §9's
  deny-by-default egress.

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

## Tenancy resolution

Verified 2026-09-11 for task 0.8. Three findings, each of which came from a
failing test rather than from reading the specification.

### Middleware at a subtree root cannot see route parameters

`chi` populates `URLParam` while matching, so middleware registered with `Use`
at the root of a subtree runs **before** the pattern below it has been matched.
A tenancy middleware mounted there sees an empty `{org}` and reports "that
request needs an organisation" for every request. The chassis's own `RouteTag`
middleware carries the same note.

The split that fixes it is also the better design: **authentication** needs no
route parameters and happens once at the subtree root; **org resolution** needs
them and happens in `Require`, which is inline on a route and therefore
post-routing by construction. SPEC §8's "role checks in a single middleware,
never inline" is still satisfied — `Require` is that middleware and the only
thing that consults the matrix.

### An org-keyed policy on `orgs` is circular

Resolving which org a request is about means reading `orgs` by slug and
`org_members` by user, and both happen **before** any org is current. With
migration `00013`'s org-keyed policy those reads return zero rows, so the
resolver reports "that org does not exist" for every org that does.

Migration `00016` adds a second per-transaction setting, `app.user_id`, and
makes only those two tables' policies user-aware:

```sql
-- org_members: keyed on columns and settings only, never a subquery, so orgs'
-- policy can read it without recursion.
using (user_id = halyard_current_user_id() or org_id = halyard_current_org_id())
```

Verified afterwards: a user scope shows exactly the caller's orgs and
memberships, does **not** expose another user's membership rows, and opens no
other table — `projects`, `secrets`, `ledger_entries` and `audit_log` all still
return nothing without an org.

`projects` was deliberately **left** org-keyed. Widening it the same way would
put a membership subquery on every project read forever, and the measured cost
is 0.36 ms for the current policy against 2.71 ms for a subquery form. Project
resolution instead lists the caller's orgs — one cheap user-scoped query — and
looks the project up inside each. One extra round trip for a single-org user,
and none once the console sends the org header.

### Creating an org is only possible scoped to its own new id

The policy on `orgs` compares the row's `id` to `app.org_id`, so an unscoped
`INSERT` is refused: you cannot create an org without saying which org you are
creating. `POST /v1/orgs` therefore generates the id first and scopes the
transaction to it.

That is not a workaround, it is a property worth having: an insert naming any
**other** id is refused, so a request cannot create an org it did not declare.
Verified in all three directions — unscoped refused, mismatched id refused,
scoped-to-itself allowed.

### Disclosure: 404 for a non-member, 403 for a member who lacks the role

A 403 for a resource in someone else's org **confirms it exists**. §7.1 makes
cross-tenant reads indistinguishable from absence on purpose, so "you may not
see this" and "this does not exist" are the same answer, with the same code and
the same message — asserted by comparing the two responses byte for byte.

A member who lacks the role gets 403, because they have already been shown the
org exists. Hiding the reason there would only leave the console unable to
explain why a button did nothing, and the refusal names the role required.

## Org and project surface

Verified 2026-09-11 for task 0.9. Three findings, all from failing tests.

### An unscoped read returns nothing once a table has a policy

`/v1/auth/session` listed the caller's orgs with an unscoped query. That worked
until migration `00016` gave `orgs` and `org_members` policies — after which it
returned **zero rows silently**, so the console would have rendered an org
switcher with nothing in it and no error anywhere. The query now runs in a user
scope.

The general shape is worth stating: **adding a policy to a table breaks every
unscoped read of it, and breaks them quietly.** `Pool.Unscoped` is named to be
conspicuous in review for exactly this reason, and every remaining use of it is
either a table with no policy (`users`, the catalogue) or a lookup whose
authorisation is a token rather than a scope (accepting an invite).

### Excluding archived rows in the resolver made them unreachable

The tenancy resolver filtered `archived_at is null`, which meant an archived
project could not be fetched **at all** — not even to see that it was archived,
though the `Project` schema has an `archived_at` field — and a repeated
`DELETE` returned 404 rather than being idempotent.

Which rows are hidden is a decision for each handler, not for the thing that
resolves identity. `listProjects` filters; `getProject` does not.

### `enum` without `type` generates an untyped value

`GitAuthority` was declared with `title` and `enum` but no `type`, which is
valid JSON Schema. oapi-codegen emitted `type GitAuthority = interface{}` — an
untyped value for a two-member enum, giving a Go client nothing to switch on and
a TypeScript client no union. Adding `type: string` produces the constants and a
`Valid()` method. Worth checking any other enum declared the same way.

### Rules the database enforces rather than the handler

- **An org's creation and its first membership are one transaction.** An org
  with no members is unreachable by anyone, including the person who just made
  it.
- **The last owner cannot be removed or demoted.** An org with no owner has
  nobody who can bill it, delete it or transfer it (§8) — it is
  unadministerable, and the person clicking the button is not usually intending
  that.
- **An admin cannot invite, remove or promote an owner.** Otherwise "everything
  except billing and delete" includes manufacturing someone who can do both,
  which makes the carve-out decorative.
- **One live invitation per address per org**, by partial unique index. Clicking
  "invite" twice otherwise sends two links, and accepting the older one after
  the newer was revoked is a confusing way to end up with the wrong role.

## Idempotency and rate limiting

Verified 2026-09-11 for task 0.10.

### A claim needs three states, not two

A key is claimed before the handler runs and completed after, so a row is
`absent`, `claimed but not completed`, or `completed`. The middle one is the
whole point: without it, a client that retries after a timeout **while the
original is still running** gets a second execution — the exact failure the
header exists to prevent. That case answers 409 with `Retry-After`.

The claim is one statement — an `insert ... on conflict do nothing` whose
result is `union`-ed with a select of the existing row. A select-then-insert
would let two concurrent first attempts both pass the select, which is the same
double-execution by another route.

A claim also carries a deadline, because a process that dies mid-request would
otherwise leave the key claimed forever and the client permanently unable to
retry.

### The response is captured, not re-derived

A replay returns the bytes the first attempt wrote. Re-rendering from current
state would differ — a project created and then renamed replays with the new
name — and a client reconciling the two would conclude something it did not do
had happened.

Only some headers are replayed. `Set-Cookie` must never be, because it would
hand a second caller the first caller's session, and the request id must not be,
because a replay is a different request and the log correlation would be wrong.

A 5xx releases the claim rather than storing it: the condition that caused it
may have cleared, so a retry should be a real attempt. A 4xx is stored, because
the same request will be refused the same way.

### The key is scoped to the org

The key is chosen by the client. Without the org in both the primary key and the
row-level security policy, one tenant could guess another's key and be handed
their response. Asserted by having two orgs use the same key and checking
neither sees the other's answer.

### An unkeyed rate limiter puts everyone in one bucket

`KeyByIP` reads the peer the chassis's `ClientIP` middleware resolved. The first
version fell back to a single constant when that middleware had not run — which
looks harmless and means **one abusive client rate-limits everybody**. A test
with two callers caught it. The fallback is now the TCP peer, which cannot be
spoofed the way a header can.

### A token bucket rather than a fixed window

A fixed window lets a caller spend the whole allowance in the last millisecond
of one window and the whole of the next in the first — twice the intended rate,
at exactly the moment a thundering herd forms. A bucket cannot be made to do
that, and the test asserts an hour of idleness still only buys the burst.

**The limiter is in-process, which is not the same as "edge".** See
docs/open-questions.md Q7: an in-process limiter divides the real limit by the
number of replicas, and making it a genuine quota needs shared state whose shape
depends on §21 decision 1.

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
