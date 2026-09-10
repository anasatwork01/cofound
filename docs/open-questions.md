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

- **`opencode` hook surface.** SPEC §11.2 assumes six patches are necessary.
  Before writing any of them, check the pinned tag for a plugin/hook API that
  makes P1/P2/P3/P5 unnecessary. Anything achievable by configuration must not
  be a patch (§11.1).
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

- **Sandbox egress allowlist mechanics.** SPEC §9 requires deny-by-default
  egress. Whether Modal exposes the necessary network policy primitives is
  part of §22 item 5, and the whole security model in §17.1 depends on it.
