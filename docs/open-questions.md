# Open questions

SPEC §0 rule 5 and rule 7: **ask, don't assume.** Undocumented vendor
behaviour and business decisions both land here rather than becoming hopeful
code.

---

## Q0 - The canonical SPEC.md is not in the repository

`docs/SPEC.md` is a placeholder. The spec arrived as a conversation document
whose text had been through a UTF-8 → Latin-1 round trip, so transcribing it
would have committed corrupted section markers and a destroyed architecture
diagram into the file every other file cites.

**Needed:** `cp /path/to/SPEC.md docs/SPEC.md`. Same for `docs/mockup.html`,
which SPEC §3.1 and §18 name as the source of the design tokens and which
task 0.9 cannot start without.

**Owner:** human. **Blocks:** nothing mechanically; blocks task 0.9 in practice.

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

## Blocking decisions (SPEC §21) - needed before phase 1

| #   | Decision                                                                                                    | Owner | Blocks    | Notes                                                                                                                                                                                                   |
| --- | ----------------------------------------------------------------------------------------------------------- | ----- | --------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | Container host: Cloudflare Containers, or an external host (Fly.io / Railway / Cloud Run) behind Cloudflare | human | 0.10      | Depends on §22 item 2. Long-lived SSE and git packfile handling are the deciding constraints                                                                                                            |
| 2   | All-Python backend, or the Go/Python split                                                                  | human | 0.4, 0.5  | **Depends on team size.** SPEC §5.1: at one or two engineers, go all-Python. Do not go all-Go. This scaffold assumes the split; collapsing it later means deleting four Go modules, not rewriting logic |
| 3   | Neon for app databases, or schema-per-project on shared Postgres behind PgBouncer                           | human | 5.4, 3.9  | Branching is what makes preview migrations safe; losing it means building migration dry-runs yourself                                                                                                   |
| 4   | Auth library for generated apps, pinned version                                                             | human | 5.6, 2.1  | Auth.js or Better Auth. Template-level, not per project                                                                                                                                                 |
| 5   | Registrar for domain sales                                                                                  | human | 3.5       |                                                                                                                                                                                                         |
| 6   | Credit denomination, retail price per credit, target gross margin                                           | human | 4.1       | SPEC §16.6: a normal turn should cost tens of credits. Never display fractions                                                                                                                          |
| 7   | Whether the free tier may publish to a custom domain                                                        | human | 4.10, 3.6 | **The main abuse lever.** Answer before phase 3 ships, not after                                                                                                                                        |
| 8   | Revenue share on Stripe Connect (`application_fee_amount`)                                                  | human | 5.7       |                                                                                                                                                                                                         |
| 9   | Whether the console targets primarily non-technical users                                                   | human | 0.9       | The mockup assumes semi-technical. A purely non-technical audience needs warmer visuals and less code exposure - this changes the token layer, so decide before 0.9                                     |

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

- **Sandbox egress allowlist mechanics.** SPEC §9 requires deny-by-default
  egress. Whether Modal exposes the necessary network policy primitives is
  part of §22 item 5, and the whole security model in §17.1 depends on it.
