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

**Now resolved too.** `docs/mockup.html` was never supplied and was not
recoverable from the attachment, so on 2026-09-12 it was **authored** rather
than waited for, with the human's explicit instruction to "implement
docs/mockup.html yourself or implement without it".

Authoring it was the better of the two, because SPEC §3.1 and §18 both
_reference_ that path: without the file those references dangle permanently and
the token layer stays provisional forever. The console's design system is now
extracted from it, and the quarantine plus contrast machinery task 0.11 built
for the swap is what made the swap a genuine one-file change.

**It answers SPEC §21 decision 9 by implication, and that is recorded rather
than smuggled in** — see the note under that decision below.

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

## ~~Q8 - SPEC §5.1 rests on a fact that is no longer true~~

**Resolved 2026-09-11 by the human: `sandboxd` stays Python — a Python sidecar
for Modal, as specified.** The decision stands; only its stated reason was
wrong. This was a fact correction, not a re-litigation of §5.

SPEC §5.1 line 220 says:

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

### What the resolution changes

Nothing in the code: `sandboxd` was already Python and stays Python. What
changes is the **record of why**, so the decision is re-examinable on its real
grounds rather than on a premise that has already expired:

> `sandboxd` is Python because the Modal Go SDK is pre-1.0 and not at parity,
> and because Modal Functions are Python-only — so SPEC §14's crawler keeps
> Python in the stack regardless.

That reason carries a **re-examination trigger** the old one did not: when
`modal-client/go` reaches 1.0 and parity, the question is live again. Until
then it is closed. §21 decision 2's resolution clause is corrected to say this.

### Erratum: SPEC §5.1 line 220 is left as written

`docs/SPEC.md` is the contract, recovered verbatim, and `CLAUDE.md` treats it as
such — so a factual error in it is recorded here rather than silently edited.
The line still reads:

> "Modal's SDK is Python. There is no supported Go SDK, so an all-Go backend
> would need a Python sidecar for sandbox orchestration anyway."

The first sentence and the conclusion are both fine. **The middle clause is
false.** The proposed one-line replacement, for a human to approve:

> "Modal's SDK is Python. The Go SDK is pre-1.0 and not at parity, and Modal
> Functions are Python-only, so an all-Go backend would need a Python sidecar
> for sandbox orchestration anyway."

Same section count, same conclusion, no renumbering. Say the word and it goes
in; until then this erratum is the record.

## ~~Q9 - `OPENCODE_DISABLE_PROJECT_CONFIG` is undocumented and load-bearing~~

**Resolved 2026-09-11 by the human: use it, and pin it.** Both environment
variables are adopted, and the undocumented one is pinned by a test so an
upstream rename fails CI instead of silently opening the sandbox. The
reasoning, and what the pin does and does not cover, is below.

SPEC §17's sandbox boundary depends on it, and it is not in opencode's
documented environment-variable table — it exists only in source
(`config/config.ts:420` and, load-bearingly, `config/paths.ts:27`).

Without it, two escapes are open on the pinned tag, both verified in source:

1. A repo's `.opencode/agent/*.md` frontmatter permissions are concatenated
   **after** `OPENCODE_PERMISSION` and evaluated with `findLast`, so the repo
   wins.
2. A repo's `.opencode/plugin/` executes arbitrary JavaScript with a Bun shell
   handle and the authenticated server SDK, outside the permission system
   entirely.

Working agreement 4 says an undocumented surface is not something to build
hopeful code on. There was no alternative that did not weaken §17: the
documented `OPENCODE_PERMISSION` alone is escapable through repo-defined
agents, and `OPENCODE_PURE` — which is documented — disables **all** external
plugins including Halyard's own supervisor, so it cannot be the policy
mechanism. The resolution is therefore to depend on the undocumented flag and
make that dependency **loud**.

### The adopted mechanism

Two environment variables, set by `sandboxd` when it launches the sandbox.
Neither is optional:

| Variable                          | Value                                            | Why                                                              |
| --------------------------------- | ------------------------------------------------ | ---------------------------------------------------------------- |
| `OPENCODE_PERMISSION`             | inline JSON, catch-all deny plus explicit allows | Merged after every config file, so the repo cannot merge over it |
| `OPENCODE_DISABLE_PROJECT_CONFIG` | `1`                                              | Closes the two escapes above. **Undocumented upstream**          |

`tool.execute.before` is the second, independent enforcement layer: it denies by
throwing, it is documented, and it does not depend on config precedence at all.
`permission.ask` is **not** a lever — it is declared in the plugin types but
dispatched at zero call sites (see `docs/verified.md` §22 item 6).

### How the pin works, and what it does not cover

`tests/contracts/test_opencode_upstream_contract.py` asserts the mechanisms
still exist in the pinned submodule source — the flag's guard sites, the
`findLast` evaluation, the concatenating `Permission.merge`, the agent-after-user
merge order, and the absence of a `permission.ask` dispatch. A rename or a
change of shape fails CI. **This pins the mechanism, not the behaviour:** it is
still a source reading. Tasks 1.7/1.8 own the behavioural half — start
`opencode serve` against a hostile fixture repo carrying
`.opencode/opencode.json`, `.opencode/agent/evil.md` and
`.opencode/plugin/evil.ts`, and assert the policy holds and the plugin never
executes.

Related, and **not** closed by this resolution: SPEC §11.3 treats `AGENTS.md` as
the untrusted-repo-content risk. The plugin directory is sharper and belongs in
§11.3's threat model — a SPEC text change, so it is recorded as an erratum
rather than silently edited. See the §11.3 erratum below.

## Q10 - `EventSource` or `fetch` streaming, and it decides the gateway's auth

SPEC §3.1 says "native `EventSource`/`fetch` streaming against the SSE gateway"
and leaves the choice open. It cannot stay open past task 1.14, because the two
options need different authentication on the Go side:

|                              | `EventSource`                                                                                            | `fetch` streaming                     |
| ---------------------------- | -------------------------------------------------------------------------------------------------------- | ------------------------------------- |
| Request headers              | **Cannot set any** — so no `Authorization: Bearer`                                                       | Can set them                          |
| `Last-Event-ID` on reconnect | Sent automatically                                                                                       | **The console implements it by hand** |
| What the gateway needs       | Cookie auth, plus CORS with `Access-Control-Allow-Credentials: true` and an explicit non-wildcard origin | Bearer token, ordinary CORS           |

This is not a preference. Task 1.1's verification of §22 item 1 established that
Cloudflare updates the Workers runtime a few times per week and terminates
in-flight requests after a 30-second grace period, so **a long-lived stream will
be cut several times a week by design**. `Last-Event-ID` resume is therefore
load-bearing rather than a nicety, and whichever transport is chosen has to
carry it.

`EventSource` looks the better fit — the browser owns reconnection and replay,
which is exactly the part that must not be got wrong — and the console already
uses a `__Host-` session cookie from task 0.7. The cost is that the Go gateway
needs credentialed CORS. **Confirm before 1.14 starts.**

Related and already settled: do **not** proxy the stream through the console
Worker. A Worker invocation may hold at most 6 simultaneous outgoing connections
and each isolate is capped at 128 MB across all concurrent requests, so the
browser talks to the Go gateway directly. See `docs/verified.md` §22 item 1.

## SPEC errata

`docs/SPEC.md` is the contract. `CLAUDE.md` says its section numbers are cited
throughout the codebase, and the file is committed verbatim (it is in
`.prettierignore` for that reason). So where task 1.1 found the SPEC stating
something now known to be false, the correction is **recorded here and not
silently applied**.

**Fourteen of these would cause someone to build the wrong thing**, so this is
not a tidying exercise. None of them renumbers a section. The prose changes in
this repository that were safe to make have already been made — this list is
only what lives inside the contract itself.

Grouped by section, worst first.

### §5.1 line 220 — "there is no supported Go SDK"

See the Q8 erratum above, which carries the proposed one-line replacement. The
decision (a Python sidecar for Modal) is unchanged and was reconfirmed by the
human; only the middle clause is false. Noted here too because this list is
where someone will look.

### §9 — the sandbox lifecycle, six items

This is the section task 1.3 implements, and it is the one task 1.1 damaged
most. The corrected flow is written up in full in `services/sandboxd/README.md`;
these are the specific lines.

**§9 responsibilities line — "create, resume, snapshot, stop"**

There is no resume primitive in Modal. Proposed:

> **Responsibilities:** create, **wake**, snapshot, stop sandboxes; maintain a
> warm pool; expose tunnels; enforce quotas; heartbeat usage. There is no resume
> — waking a project creates a **new** sandbox and restores its snapshot.

**§9 step 2 — "create ... with the project's Modal Volume mounted"**

`volumes=` is a `Sandbox.create` parameter only, so a warm sandbox (step 1) can
never have a Volume attached. Steps 1 and 2 are mutually exclusive as written.
Proposed:

> 2. Miss → create from the template's pre-baked image, then `mount_image` the
>    project's directory snapshot. **Not a Volume**: `volumes=` is create-time
>    only, so Volumes and the warm pool are mutually exclusive, and Volume v1's
>    50,000-file budget does not fit a `node_modules`.

**§9 step 5 — "Register the tunnel URL ... in Workers KV"**

Reads as a one-time registration. Modal assigns a random hostname with no way to
pin one, and it changes on every create and every restore. Proposed:

> 5. Register the tunnel URL against `<branch>.<project>.preview.<domain>` in
>    Workers KV, **rewriting it on every incarnation and invalidating the
>    previous entry** — Modal's hostname is random and unpinnable. Restrict it
>    with `inbound_cidr_allowlist` so only our own edge can reach it; a Modal
>    tunnel is public by default.

**§9 step 7 — "Idle 15 minutes → snapshot filesystem, stop sandbox, keep the volume"**

Three problems: Modal's `idle_timeout` has no pre-termination hook so it would
kill _without_ snapshotting; a merely-running `agentd` is not "active" by
Modal's definition; and the retained artifact is a snapshot image whose id only
we can record. Proposed:

> 7. Idle 15 minutes, **tracked by `sandboxd` rather than Modal's
>    `idle_timeout`** (which has no pre-termination hook and does not count a
>    running daemon as activity) → snapshot the project directory with
>    `ttl=None`, record the image id and expiry in Postgres because Modal cannot
>    list snapshots, then terminate. Add: before the **24-hour** maximum
>    lifetime, snapshot and rotate to a replacement sandbox.

**§9 quota table — "CPU | 2 vCPU"**

Correct as a quota, a units trap at the call site. Proposed footnote:

> Modal's `cpu=` takes **physical cores**, which its pricing page labels "2 vCPU
> equivalent" — so this is `cpu=1.0, memory=4096`. `cpu=2.0` provisions twice
> the CPU and raises spend ~1.6×, and a bare scalar is only a request that
> permits billed bursting; the tuple form is the hard cap.

**§9 egress allowlist — "the project's own app database host"**

Now implementable, but not as a domain entry. Modal's domain allowlist matches
TLS/443 SNI only. Proposed addition:

> The app database host cannot be authorised by name — Postgres on 5432 is not
> TLS-on-443, so it needs a CIDR entry. Domain fronting is a documented bypass,
> so a shared-CDN entry is an exfiltration channel and `*.github.com` grants
> every repository on GitHub.

**§9 warm pool — "2–5 per popular template image per region"**

Modal has no pool primitive, a pooled sandbox with `agentd` idling gets reaped,
and narrow-region pinning costs 1.75× while _worsening_ cold start. Proposed
addition:

> Modal has no pool primitive; this is ours to build over sandbox ids. Prefer
> broad regions (`us`/`eu`/`ap`, 1.15×) — narrow regions cost 1.75× and reduce
> the schedulable pool, which is the opposite of what a warm pool is for. Size
> it against ~$0.24/hr per idle sandbox (§19), and measure cold create first
> (task 1.19) before committing to a pool at all.

### §11.2 — "Required patches", and P3's row

The heading says these patches are **required** and the third column asserts of
each that it **cannot be config**. Task 1.1 found an upstream mechanism for all
six. Proposed: retitle to **"§11.2 Behaviours we need from `opencode`"**, and
replace the "Why it cannot be config" column with "How it is obtained", naming
the mechanism per row (see `agent/patches/README.md`).

P3's row additionally names a mechanism that never existed:

> | P3 | Non-overridable permission policy loaded from `$HALYARD_POLICY` | ... |

There is no `$HALYARD_POLICY`. Proposed:

> | P3 | Non-overridable permission policy | `OPENCODE_PERMISSION` carrying
> **inline JSON** — never a file path, which the sandbox could write — plus
> `OPENCODE_DISABLE_PROJECT_CONFIG=1`, because the env var alone is escapable
> through repo-defined agents. |

### §11.3 — the untrusted-repo threat model stops at `AGENTS.md`

§11.3 names the project's own `AGENTS.md` as the untrusted input. That is true
and incomplete: on the pinned tag, opencode **auto-discovers and executes**
plugins from the repository itself (`.opencode/plugin/`, plus npm packages named
in the repo's `opencode.json`), and plugin code receives a Bun shell handle and
the authenticated server SDK. That is arbitrary code execution **outside the
tool-permission system entirely** — no permission rule is consulted, because
plugins are not tools.

`AGENTS.md` influences the model. A repo plugin owns the process. Proposed
addition:

> A project's `.opencode/` directory is untrusted input in a stronger sense than
> `AGENTS.md`: opencode discovers and executes plugins from it, outside the
> tool-permission system. `sandboxd` therefore sets
> `OPENCODE_DISABLE_PROJECT_CONFIG=1`, and Halyard's own supervisor plugin is
> installed globally in the image (`~/.config/opencode/plugin/`) rather than per
> project.

### §11.4 — `agentd`'s telemetry source and both turn hooks

**"Reads P1 telemetry"** — there is no P1 and no Unix socket. Proposed: "Reads
per-turn usage off `opencode`'s event stream (`message.updated`), batches it,
posts heartbeats to `sandboxd`." Note the numbers are pre-netted — see §16.2
below.

**"At `onTurnStart`: flush any dirty human edits into a checkpoint commit before
the agent reads files"** — achievable via the blocking `chat.message` hook, with
**one residual data-loss window the SPEC should state**: `chat.message` fires
_after_ the message's explicitly attached files and `@`-mentions have already
been read from disk. Tool-driven reads (`read`/`grep`/`glob`) all happen after
the hook, so the main scenario is covered. Proposed addition: "…using the
blocking `chat.message` plugin hook. Attached-file content is resolved before
that hook fires, so `agentd` checkpoints when it **accepts** the prompt, before
forwarding it."

**"At `onTurnEnd`: commit the checkpoint, release the lease"** — there is no
blocking turn-end hook; `session.idle` is a fire-and-forget event. Proposed:
"After `session.idle`, `agentd` commits the checkpoint and swaps the lease
**before submitting the next prompt** — it cannot hold a turn open, so
serialisation is what makes this safe. If anything other than `agentd` can POST
a prompt, the lease swap races."

### §16.2 — the four meters need a mapping rule

This is the clause the generated schemas carry, and the obvious mapping
under-bills. The four-meter split is right and stays; what is missing is what
goes in each. Proposed addition:

> `tokens_in` receives the agent's **already cache-adjusted** input, so billable
> input is `tokens_in + tokens_cache_read + tokens_cache_write`; `tokens_out`
> receives **output plus reasoning**, because the agent's output field excludes
> reasoning and there is no fifth meter. Emitting the upstream fields verbatim
> under-bills by the whole cache volume and never bills reasoning at all.

(`packages/schema/meters.schema.json` and `agent-events.schema.json` already say
this, so the generated code carries it; §16.2 is the clause they derive from.)

### §17.2 — the threat table cites a patch that does not exist

> | Prompt injection via scraped content or the repo's own `AGENTS.md` | Policy
> enforced outside the model (P3, egress allowlist, ...) |

This is where an implementer looks for what actually stops prompt injection, and
P3 is not a thing. Proposed: replace "P3" with "the `OPENCODE_PERMISSION` +
`OPENCODE_DISABLE_PROJECT_CONFIG` policy and the `tool.execute.before` hook",
and add the repo `.opencode/plugin/` vector alongside `AGENTS.md`.

### §17.3 — the p50 < 10s SLO is provisional

> | Time to first preview, new project from template | p50 < 10s, p95 < 25s |

The Modal component is entirely unmeasured: no published timing exists for
snapshot create or restore, and the SDK's own default snapshot timeout is 55s
with support added for longer. Proposed: mark it "target, pending task 1.19"
until measured. Container boot is ~0.5–1s of the budget; the rest is the
reconcile step, which is ours.

### §13.3 — the tunnel URL changes per incarnation, not per session

> "a sandbox tunnel whose URL changes each session"

It changes on every create **and** every restore, so a single long session can
see several. The webhook-relay conclusion is unaffected and correct; the
reasoning generalises to §9 step 5 and §14.2.

### §19.2 — data residency has a concrete Modal-shaped blocker

> "Data residency, if you intend to sell in the EU."

Snapshots are stored in the **United States regardless of where the workload
runs**, because filesystem snapshots are Modal Images underneath. So the moment
a project is snapshotted, its whole filesystem leaves its region. The only
mitigation offered is an Alpha customer-supplied-encryption-key option.

_(Not to be confused with region pinning: filesystem and directory snapshots
place no restriction on `region`. That limit belongs to memory snapshots, which
this design does not use.)_

### Phase 1 and Phase 2 scope lines list patches as deliverables

Phase 1 names "opencode patches P1/P4/P5/P6" as scope; Phase 2's accept
criterion states the p50 unqualified. Both follow from the entries above.
`docs/TASKS.md` rows 1.9, 1.10, 1.19 and 4.6 have already been rewritten; the
SPEC's own phase summaries have not.

## Decision brief for §21 decision 1 — the container host

**Status: ready to decide. §22 item 2 is verified; the facts are in
`docs/verified.md`.** Task 0.12 is blocked on this and on nothing else.

Three facts decide it and all three point the same way — **an external container
host behind Cloudflare** — but one is an inference, and that is what the
decision should be gated on.

### 1. A container-proxied SSE stream is not documented to survive

Cloudflare Containers is fronted by a **Durable Object**, and Cloudflare's own
DO documentation says a streamed `fetch()` body "never keep[s] the Durable
Object alive, even while the response body is still streaming." The published
`@cloudflare/containers@0.3.7` proxies a response as exactly that kind of
subrequest. SPEC §5.1 has `api` and `gitd` holding thousands of long-lived
streams.

Both halves are confirmed from primary artifacts. The **conclusion** is one
inference step, because Cloudflare never writes the sentence outright.

The obvious workaround is closed: WebSockets through a container get
`server.accept()` rather than hibernation, cap at a documented 15 minutes, and
are disconnected by every deploy.

### 2. The limits §3.5 made the condition are not published

§3.5 offers Containers "**if** its current limits on memory, request duration,
persistent connections and long-lived SSE suit us." The Containers docs mention
SSE **zero times** and publish **no** per-instance connection number. Cloud Run
publishes 1,000 concurrent and a 60-minute ceiling. **A known bad number beats
an unknown** — which is the "more moving parts, fewer unknowns" trade §3.5
already anticipated.

### 3. `gitd` wants real disk and large bodies; Containers has neither

Ephemeral disk only (20 GB max, lost on restart, snapshots "coming soon"), and
request bodies capped by the **account plan** — 100 MB on Pro — because every
container request passes through a Worker. A first `git push` of a large repo
returns 413 before `gitd` sees a byte. Fly and Railway both sell real volumes.

### What does _not_ decide it

- **Cost.** ~$68/mo (Containers) vs ~$36 (Fly) vs ~$54+$20 (Railway) vs ~$171
  (Cloud Run) for six always-on services. All rounding error beside the
  ~$174/mo per continuously-running Modal sandbox in §22 item 5.
- **Maturity.** Containers has been GA since 2026-04-13.
- **Recycling.** Cloudflare is _kinder_ than Fly on deploys — 15 minutes to
  drain against Fly's 5-second default. `Last-Event-ID` resume is needed on all
  four hosts.
- **Data residency.** Cloudflare is the **best** here
  (`constraints.jurisdiction = "eu"`).

### The three ways to answer

1. **Pick an external host.** The recommendation is **Fly.io**: real volumes,
   $0.02/GB egress, full TCP, `min_machines_running`, lowest predictable
   always-on cost. Cloud Run is worst (60-minute ceiling, 1,000-concurrency cap,
   most expensive). Fly vs Railway turns on preferences the facts do not settle.
   **Fly publishes no timeout figures at all**, so 0.12 must establish Fly
   Proxy's long-lived-HTTP behaviour by experiment.
2. **Gate on one cheap experiment.** If the small vendor surface is worth it,
   deploy one `lite` container, hold SSE open for an hour, and see. One Workers
   Paid account, no new vendor, and it is the only thing that would overturn
   fact 1. Deciding _conditionally_ is a legitimate answer here.
3. **Change the architecture** so containers never hold the streams — terminate
   SSE in a Durable Object with the hibernation API, Go services behind it doing
   request/response only. Cloudflare's intended shape, and it works. But it
   contradicts §5.1, reopens §5.4, and is a redesign rather than a host choice.
   Named so it is a visible option rather than a later surprise.

**What is blocked meanwhile:** every Go service's deployment. The container
image itself is built, tested and host-agnostic (`infra/docker/Dockerfile`), and
the console half of 0.12 does not depend on this at all.

## Blocking decisions (SPEC §21) - needed before phase 1

| #   | Decision                                                                                                    | Owner | Blocks    | Notes                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| --- | ----------------------------------------------------------------------------------------------------------- | ----- | --------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | Container host: Cloudflare Containers, or an external host (Fly.io / Railway / Cloud Run) behind Cloudflare | human | 0.12      | **§22 item 2 is now verified and the facts point one way: an external host.** Long-lived SSE and git packfile handling are the deciding constraints, and Containers is fronted by a Durable Object that a streamed `fetch()` body does not keep alive. Full brief below.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| 2   | ~~All-Python backend, or the Go/Python split~~                                                              | human | 0.4, 0.5  | **Resolved 2026-09-10: the Go/Python split, as specified.** Chosen over all-Python because api and gitd hold thousands of concurrent long-lived SSE streams and do heavy git packfile plumbing, where Go's memory-per-connection and process model are materially better; `sandboxd` stays Python either way — **not** because Modal lacks a Go SDK (it has had one since 2026-09-10; see the Q8 erratum) but because that SDK is pre-1.0 and not at parity, and because Modal Functions are Python-only, so §14's crawler keeps Python in the stack regardless. Reason corrected 2026-09-11; the decision itself is unchanged and was reconfirmed by the human. Implemented by task 0.4 (`packages/chassis`). The original rationale is kept as the record of why it was a real question: **Depends on team size.** SPEC §5.1: at one or two engineers, go all-Python. Do not go all-Go. |
| 3   | Neon for app databases, or schema-per-project on shared Postgres behind PgBouncer                           | human | 5.4, 3.9  | Branching is what makes preview migrations safe; losing it means building migration dry-runs yourself                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| 4   | Auth library for generated apps, pinned version                                                             | human | 5.6, 2.1  | Auth.js or Better Auth. Template-level, not per project                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| 5   | Registrar for domain sales                                                                                  | human | 3.5       |                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| 6   | Credit denomination, retail price per credit, target gross margin                                           | human | 4.1       | SPEC §16.6: a normal turn should cost tens of credits. Never display fractions                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| 7   | Whether the free tier may publish to a custom domain                                                        | human | 4.10, 3.6 | **The main abuse lever.** Answer before phase 3 ships, not after                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| 8   | Revenue share on Stripe Connect (`application_fee_amount`)                                                  | human | 5.7       |                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| 9   | Whether the console targets primarily non-technical users                                                   | human | 0.9       | **Answered by implication, open to overrule.** SPEC §21.9 itself says "the mockup assumes semi-technical", and task L.5 authored the mockup on that assumption — so the shipped design targets a semi-technical founder. A purely non-technical audience would mean a warmer palette and less code exposure (the SHA in small mono on `/ship`, the file counts in the builder). That is a re-extraction of `palette.css` plus a copy pass, not a rebuild. Say the word and it changes.                                                                                                                                                                                                                                                                                                                                                                                                    |

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
