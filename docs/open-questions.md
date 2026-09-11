# Open questions

SPEC §0 rule 5 and rule 7: **ask, don't assume.** Undocumented vendor
behaviour and business decisions both land here rather than becoming hopeful
code.

---

## ~~Q0 - The canonical SPEC.md is not in the repository~~

**Resolved 2026-09-10.** `docs/SPEC.md` now holds the canonical specification,
1259 lines, and every `§`-citation in this repository resolves against it.

It was recovered from the original attachment rather than retyped, and the
encoding damage was _reversed_ rather than corrected. That distinction is the
whole reason this was safe to do:

- The corruption was a single clean UTF-8 → Latin-1 → UTF-8 round trip. Such a
  transformation is deterministic and lossless, so `t.encode('latin-1')
.decode('utf-8')` recovers the original bytes exactly. Both steps were checked
  for failure; either one erroring would have meant the damage was something
  else and the repair had to stop.
- **Proof it is a reversal and not a rewrite:** re-corrupting the repaired text
  reproduces the input byte for byte. Nothing was edited by judgement.
- Afterwards: 0 replacement characters, 24 clean `§`, 74 em dashes, and the
  architecture diagram's 534 box-drawing characters restored. Before: 24
  mojibake `§`, 655 broken sequences, 0 box-drawing characters.
- The section numbering is unchanged: 24 top-level sections and 48
  sub-sections, and every number cited anywhere in the repo is present.

`scripts/check-structure.sh` now fails if the placeholder returns, if mojibake
reappears, or if the section count changes — the three ways this could silently
regress.

**Still outstanding:** `docs/mockup.html`, which SPEC §3.1 and §18 name as the
source of the design tokens. It was never supplied and is not recoverable from
the attachment. **Owner:** human. **Blocks:** task 0.11 (console shell), which
`docs/TASKS.md` marks blocked for this reason.

---

## Q1 - SPEC §7.2's examples contradict its own prose

Found while deriving `agent-events.schema.json` and pinned by
`tests/contracts/test_schemas.py::test_spec_7_2_examples_as_written_expose_two_deviations`,
so neither can be forgotten or silently "fixed".

**1. Three examples omit `turn`.** §7.2 states as a rule that _every event
carries `turn`_, but the `lease.changed`, `preview.status` and `error` examples
have no `turn` field. The schema follows the normative sentence and requires it,
which means those three examples need correcting — or the rule needs relaxing to
"every turn-scoped event". Worth deciding deliberately: `lease.changed` and
`preview.status` are arguably session-scoped rather than turn-scoped, in which
case the rule is wrong rather than the examples.

**2. Credits appear as JSON numbers.** `{"hold": {"credits": 40}}` and
`"credits": 31`. The schema types credits as a decimal string, because SPEC §6
stores them as `numeric(14,4)` and §16.1 requires a ledger rather than a
counter — routing money through an IEEE-754 double loses that exactness
silently, and §16.5's nightly reconciliation is exactly where such losses would
surface as unexplained drift. If credits are instead meant to be integral at the
API boundary (§16.6 says never display fractions), say so and the type becomes
an integer; either is defensible, but the two spellings in §7.2 and §6 cannot
both be right.

**Owner:** human. **Blocks:** nothing today; phase 1 consumes both.

---

## Q2 - SPEC §6 references `template_versions` but never defines it

`projects.template_version_id` is declared `references template_versions(id)`,
and §7.1's `POST /v1/projects` takes a `template_version_id`, but no
`create table template_versions` appears in §6. `packages/schema/api.openapi.yaml`
models both `Template` and `TemplateVersion`, so the shape is known from the
contract even though the DDL is absent.

§6 opens with "Not exhaustive — add columns as needed", which licenses filling
this in, so task 0.6 defines `templates` and `template_versions` from the
committed OpenAPI models rather than blocking. Recorded here because it is a
gap in the document rather than a decision, and because the next person to diff
a fresh copy of §6 against `db/migrations` should know why there is a table in
one and not the other.

**Owner:** human, to confirm the derived shape. **Blocks:** nothing.

---

## Q3 - SPEC §8 requires rotating sessions but §6 defines no table for them

§6's `sessions` table is _agent_ sessions — project, branch, sandbox, state. §8
separately requires console sign-in with "httpOnly, `SameSite=Lax`, rotating
cookies", which needs durable server-side session state and, for magic links, a
single-use token table. Neither exists in §6.

Task 0.7 adds them under §6's "not exhaustive" licence, named to avoid the
collision with the existing `sessions` table. Recorded because the naming is a
decision a reader of §6 alone would not expect.

**Owner:** human, to confirm. **Blocks:** nothing.

---

## Q4 - Where console sign-in runs is not pinned down

§7.1 says auth is "via session cookie (console) or bearer PAT (future CLI)" and
lists no `/v1/auth/*` endpoints, so `api` clearly _verifies_ sessions. It does
not say who _issues_ them. §8 says "Verify library choice (§22)", and §22 item
14 asks about Auth.js / Better Auth — both JavaScript, which points at the
Next.js console.

That reading has a cost: the Go `api` service would then have to verify a
session format issued by a JS library, so the two would share a session table
and a hashing scheme by convention rather than by contract, and every upgrade of
that library becomes a change to a Go service.

Task 0.7 therefore issues and verifies sessions in `api`, which is the service
§7.1 already requires to verify them, and leaves the console a thin caller. The
§22 item 14 verification is still recorded in `docs/verified.md` because the
decision for _generated apps_ (§21 decision 4) is separate and still open.

**Owner:** human, to confirm or overrule. **Blocks:** nothing; 0.7 proceeds on
the stated reading.

---

## Q5 - No transactional email provider is specified

SPEC §8 requires "email magic link" sign-in, but neither §3's technology stack
nor §22's verification list names a provider to send it with. §13's capability
table has an `email` capability, but that is a capability offered to GENERATED
apps, not the console's own transactional mail.

Task 0.7 therefore ships a `Mailer` interface with one method — address and
link, nothing provider-shaped — and a `LogMailer` default that writes the link
instead of sending it. That default is safe to leave in place by accident: the
link is marked as chassis user content, so it is redacted above debug level and
a production deploy does not spray live sign-in links into a log pipeline. It is
still the wrong thing to ship, which is why this is open.

Working agreement 4 is the reason there is no half-written provider client here:
choosing one now would mean writing against an API nobody has documented for
this project, and the shapes providers impose (template ids, merge
dictionaries, campaign identifiers) are exactly what a premature choice would
bake into the interface.

**Needed:** a provider, and a §22-style verification of its API in
`docs/verified.md`. **Owner:** human. **Blocks:** sign-in working for a real
user in a deployed environment. Nothing in phase 0.

---

## Q6 - `/v1/projects/{project}` is ambiguous across orgs

SPEC §7.1's project paths take a project **slug**, and §6 makes a project slug
unique only within an org (`unique (org_id, slug)`). So for a user who belongs
to two orgs that each have a project called `crm`, `/v1/projects/crm` names two
different projects — and §7.1 defines no way to say which, even though §8
requires "org switching in the project picker", which means the console has a
current org it could send.

Task 0.8 resolves it as follows, and the choice is recorded because it adds a
header the specification does not mention:

- An optional `X-Halyard-Org` header names the org by slug. Documented in
  `api.openapi.yaml` as a reusable parameter, so the routes task 0.9 adds
  reference it rather than each inventing it.
- Without the header, the project is resolved across the caller's memberships.
  Exactly one match is served; **more than one is refused** with
  `ambiguous_project` (409) naming the candidate org slugs, which are safe to
  disclose because the caller is a member of all of them.
- A guess is never served. Picking one would mean acting on the wrong tenant's
  project, which is the worst outcome available here.

Single-org callers — almost everyone — therefore stay on exactly the path §7.1
specifies and never send the header.

**The alternatives, and why not:** making project slugs globally unique would
mean one tenant's naming choices constrain another's, which is visible to users
and worse. Putting the org in the path (`/v1/orgs/{org}/projects/{project}`)
would contradict §7.1's spelling directly.

**Owner:** human, to confirm the header or name a different mechanism.
**Blocks:** nothing; 0.8 proceeds on the stated reading and 0.9 builds on it.

---

## Q7 - Rate limiting is in-process, and "edge" needs the host decision

Task 0.10 asks for "edge rate limiting". What it ships is an **in-process**
token bucket, which is a different thing and worth stating plainly: an
in-process limiter divides the real limit by the number of replicas, so a
"50 requests per second per org" limit is actually 50 × replicas. That is
correct for protecting one process from a hot caller and **wrong** for
enforcing a per-tenant quota.

Making it correct needs shared state, and what that should be depends on
§21 decision 1 — the container host. On Cloudflare the natural answer is a
Durable Object or the platform's own rate limiting, applied genuinely at the
edge before a request reaches an origin at all; behind an external host it is
Redis, which the stack already has (§3.3). Choosing now would mean writing
against whichever one the decision then goes against.

So `httpx.RateLimitStore` is an interface with one in-process implementation,
and the limiter is wired at both the public and authenticated subtrees so the
policy — which surfaces, what keys, what numbers — is already decided and
tested. Only the store changes.

**Owner:** human, via §21 decision 1. **Blocks:** enforcing a real quota. Does
not block protecting a process, which is in place.

---

## Q8 - SPEC §3.2 and §5.1 rest on a fact that is no longer true

**Status: needs a human. This is a fact correction, not a re-litigation of
§5.**

SPEC §3.2 line 220 says:

> "Modal's SDK is Python. There is no supported Go SDK, so an all-Go backend
> would need a Python sidecar for sandbox orchestration anyway."

`github.com/modal-labs/modal-client/go` **v0.10.1** was published 2026-09-10,
in the same first-party monorepo as the Python client, documented on Modal's own
site. Verified independently at
`https://proxy.golang.org/github.com/modal-labs/modal-client/go/@latest`.

This matters because **§21 decision 2's resolution cites the false premise**:
"`sandboxd` stays Python either way because Modal has no Go SDK." The decision
itself still looks right, but the stated reason cannot stand as written.

The conclusion survives on grounds the SPEC did not cite, and which task 1.1
verified:

- The Go SDK is **pre-1.0 Beta**, with breaking changes in 0.8.0, 0.9.0 **and**
  0.10.0 — roughly every six to eight weeks.
- **Functions are Python-only** by Modal's own statement, and SPEC §14's
  Playwright/Lighthouse crawler is a Function. Python does not leave the stack
  either way.
- Python has a typed `ResourceExhaustedError` and a complete `.aio` surface. Go
  has neither, and its throttling default is an unbounded silent wait.

**Asked, not assumed:** should §3.2/§5.1's wording be corrected to "the Go SDK
exists but is pre-1.0 and not at parity" — keeping the decision, fixing the
reason, and making it re-examinable when the SDK reaches 1.0? Nothing is
blocked on the answer; `sandboxd` stays Python meanwhile.

## Q9 - `OPENCODE_DISABLE_PROJECT_CONFIG` is undocumented and load-bearing

SPEC §17's sandbox boundary depends on it, and it is not in opencode's
documented environment-variable table — it exists only in source
(`config/config.ts:420`, `config/paths.ts:27`).

Without it, two escapes are open on the pinned tag, both verified in source:

1. A repo's `.opencode/agent/*.md` frontmatter permissions are concatenated
   **after** `OPENCODE_PERMISSION` and evaluated with `findLast`, so the repo
   wins.
2. A repo's `.opencode/plugin/` executes arbitrary JavaScript with a Bun shell
   handle and the authenticated server SDK, outside the permission system
   entirely.

Working agreement 4 says an undocumented surface is not something to build
hopeful code on. The proposal is to **use it anyway and pin it with a test** —
task 1.7 or 1.8 starts `opencode serve` against a hostile fixture repo and
asserts the policy holds and the plugin never runs — so an upstream rename
fails CI rather than silently opening the sandbox. **Confirm that is acceptable,
or name a different mechanism.**

Related: SPEC §11.3 treats `AGENTS.md` as the untrusted-repo-content risk. The
plugin directory is sharper and should be added to it.

## Blocking decisions (SPEC §21) - needed before phase 1

| #   | Decision                                                                                                    | Owner | Blocks    | Notes                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| --- | ----------------------------------------------------------------------------------------------------------- | ----- | --------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 1   | Container host: Cloudflare Containers, or an external host (Fly.io / Railway / Cloud Run) behind Cloudflare | human | 0.10      | Depends on §22 item 2. Long-lived SSE and git packfile handling are the deciding constraints                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| 2   | ~~All-Python backend, or the Go/Python split~~                                                              | human | 0.4, 0.5  | **Resolved 2026-09-10: the Go/Python split, as specified.** Chosen over all-Python because api and gitd hold thousands of concurrent long-lived SSE streams and do heavy git packfile plumbing, where Go's memory-per-connection and process model are materially better; `sandboxd` stays Python either way because Modal has no Go SDK. Implemented by task 0.4 (`packages/chassis`). The original rationale is kept as the record of why it was a real question: **Depends on team size.** SPEC §5.1: at one or two engineers, go all-Python. Do not go all-Go. |
| 3   | Neon for app databases, or schema-per-project on shared Postgres behind PgBouncer                           | human | 5.4, 3.9  | Branching is what makes preview migrations safe; losing it means building migration dry-runs yourself                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| 4   | Auth library for generated apps, pinned version                                                             | human | 5.6, 2.1  | Auth.js or Better Auth. Template-level, not per project                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| 5   | Registrar for domain sales                                                                                  | human | 3.5       |                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| 6   | Credit denomination, retail price per credit, target gross margin                                           | human | 4.1       | SPEC §16.6: a normal turn should cost tens of credits. Never display fractions                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| 7   | Whether the free tier may publish to a custom domain                                                        | human | 4.10, 3.6 | **The main abuse lever.** Answer before phase 3 ships, not after                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| 8   | Revenue share on Stripe Connect (`application_fee_amount`)                                                  | human | 5.7       |                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| 9   | Whether the console targets primarily non-technical users                                                   | human | 0.9       | The mockup assumes semi-technical. A purely non-technical audience needs warmer visuals and less code exposure - this changes the token layer, so decide before 0.9                                                                                                                                                                                                                                                                                                                                                                                                |

## Long-lead items to start immediately

These have external approval queues measured in weeks, and phase 6 stalls
without them. Start them during phase 0.

- Google Ads API developer token application (§22 item 9)
- Meta App Review + Business Verification (§22 item 10)
- Stripe Connect platform application (§22 item 11)

## Technical unknowns

- ~~**`opencode` hook surface.**~~ Resolved 2026-09-11 by task 1.1: **all six**
  §11.2 patches have an upstream mechanism on `v1.18.30`, so `agent/patches/`
  stays empty. Three things that changed other parts of the SPEC rather than
  just the patch list are recorded in `docs/verified.md` §22 item 6 — the
  `.opencode/plugin/` RCE gap in §11.3, the cache-adjusted token fields that
  affect §19's ledger, and P5's turn-end hook not existing. See also Q9.
- ~~**Go module path.**~~ Resolved 2026-09-09: the remote is
  `https://github.com/anasatwork01/cofound.git`, so Go modules are
  `github.com/anasatwork01/cofound/...`. The npm scope stays `@halyard/*` -
  that is the product name, not the repo URL.
- **Production Redis is a separate choice from the dev image.** `compose.yaml`
  runs `redis:8.10-alpine` for local work, which says nothing about production.
  Redis 8 is AGPLv3-licensed; running it unmodified as internal infrastructure
  is unremarkable, but the managed offering will be picked on other grounds
  anyway (ElastiCache, Upstash, Valkey, or self-hosted). Decide before phase 1
  ships the write lease, since the lease is the first durable dependency on it.

- ~~**Go module path.**~~ and ~~**`go work sync` behaviour.**~~ Resolved
  2026-09-10 by task 0.4: `go work sync` rewrites a module's `go` directive to
  the maximum across its dependency graph, not to `go.work`'s own line, and
  `make bootstrap` runs it — so `packages/chassis` and `services/api` are
  committed at `go 1.27.1` while the untouched services stay at `1.25.0`. See
  `docs/verified.md` finding 7.

- ~~**Sandbox egress allowlist mechanics.**~~ Resolved 2026-09-11 by task 1.1.
  Modal exposes `block_network`, `outbound_cidr_allowlist` and
  `outbound_domain_allowlist` on `Sandbox.create`, so §17.1's deny-by-default
  egress is implementable. Two limits go in the threat model rather than being
  discovered later: domain matching is **TLS/443 SNI only** (Postgres on 5432
  needs a CIDR entry), and Modal documents **domain fronting** as a bypass, so a
  shared-CDN allowlist entry is an exfiltration channel. See `docs/verified.md`
  §22 item 5.

Still unknown after task 1.1, and each one is an **inference, not a
permission** (working agreement 4). Ask Modal support before any of these
sizes a capacity model:

- **Do Sandboxes count against the plan container cap?** Modal's resources
  guide phrases the unit as "Each Modal Function or Sandbox container...", but
  never states it outright. The cap is 100 on Starter, 5000 on Team.
- **Is `Sandbox.create` metered on the 200 req/s workspace bucket?** The
  documented sentence says "Function calls or HTTP requests". This decides warm
  pool refill burst sizes.
- **Per-workspace Volume count limit and volume-creation rate limit.** Not
  documented at all. Design so the choice stays reversible —
  `with_mount_options(sub_path=...)` on shared v2 Volumes needs no cap.
- **Concurrent tunnel limit per workspace, and tunnel bandwidth.** Not
  documented.
- **Maximum CPU/memory per sandbox.** Enforced server-side at create; the number
  is unpublished. 1 core / 4 GiB is plainly inside it, but a larger tier is a
  guess.
- **When the billing meter starts and stops** — at `create` or container start,
  at the `terminate()` call or confirmed teardown. Check against a real invoice
  in phase 1 before trusting §19 margins.
- **`MODAL_IDENTITY_TOKEN` TTL and refresh semantics.** The OIDC guide's example
  token shows a 48h `exp - iat`; no prose states a TTL or whether the
  in-container value rotates. Do not hardcode 48h.

- **Modal snapshot and restore latency is unmeasured, and it is the product's
  central number.** No published timing exists for `snapshot_filesystem`,
  `snapshot_directory` or `Sandbox.create` from either — only "optimized for
  performance" and "mounted instantly". The one hard signal is that the SDK's
  default `timeout` is 55s and changelog 1.4.3 added support for longer, which
  means snapshots **can** exceed 55 seconds. **Task 1.x must ship a benchmark
  harness** against a real workspace, measuring p50/p95/p99 on a representative
  repo with `node_modules` installed. SPEC §9's p50 < 10s resume SLO cannot be
  committed to before that exists.
