# CLAUDE.md

Project instructions for coding agents working in this repository.

## Read first

- [docs/SPEC.md](docs/SPEC.md) — the contract. Section numbers are cited
  throughout the codebase; don't renumber them.
- [docs/TASKS.md](docs/TASKS.md) — the work breakdown and current status.
- [CONTRIBUTING.md](CONTRIBUTING.md) — git conventions, in full.

## Git workflow — required

Never commit to `main`. Every change goes on a branch and reaches `main`
through a pull request.

1. **Branch** off up-to-date `main`, named by the kind of change:
   - `feature/<name>` — new capability
   - `fix/<name>` — fixing broken behaviour
   - `update/<name>` — docs, deps, config, refactors
     Lowercase kebab-case. Lead with the task ID when the work maps to a task in
     `docs/TASKS.md`: `feature/0.2-ci-pipeline`. One branch per task.
2. **Verify** — `make verify` must pass before pushing. Not "should"; must.
3. **Push** the branch with `-u origin <branch>`.
4. **Open a PR against `main`**, with a body stating what changed, why, how it
   was verified, and the task ID.

Commit subjects are imperative, ≤72 characters, and lead with the task ID
(`Task 0.6: add control plane schema with row-level security`). The body
explains why. Add the trailer:

```
Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
```

Flag these explicitly in the PR body — they are specification requirements,
not preferences: destructive migrations (§0 rule 6), anything touching
`ledger_entries` (§19.2), anything that weakens the sandbox trust boundary
(§17), a new vendor dependency (record the §22 verification in
`docs/verified.md` in the same PR), a new `agent/patches/` patch (§11.1).

## Commands

```bash
make doctor       # toolchain vs .tool-versions
make bootstrap    # install JS, Python and Go deps; install git hooks
make verify       # structure, lint, typecheck, test, build
make fmt          # auto-format all three languages
make check-branch # validate the current branch name
make help         # everything else
```

Node must be 22.18.0 — `nvm use` before running anything JS. The shell default
on this machine is older, and `engine-strict=true` will reject it.

## Working agreements from the spec

1. **Contracts before implementations.** Schemas in `packages/schema` first,
   then generate TypeScript, Go and Python from them. Never hand-write the same
   type twice.
2. **Verify platform facts before depending on them.** SPEC §22 lists 15 facts
   that move. Confirm against current vendor docs and record the finding in
   `docs/verified.md`. An unverified row is not permission to proceed.
3. **Every task ships tests.** Unit tests for pure logic, integration tests
   against a real Postgres in Docker, one end-to-end test per user-visible flow.
4. **Never invent an API surface.** If a vendor endpoint isn't documented, stop
   and add it to `docs/open-questions.md` rather than writing hopeful code.
5. **Migrations are forward-only and additive.**
6. **Don't re-litigate SPEC §5.** Those choices are made, with reasons.
7. **Ask, don't assume, on SPEC §21.** Those are business decisions.

## The rule that shapes everything

The sandbox is untrusted. It runs code an LLM wrote, influenced by content the
LLM read from the internet and from the user's own repository. It never holds a
credential that can spend money or reach another tenant's data. Everything
privileged happens in the control plane, reached over authenticated MCP or HTTP
with a short-lived, project-scoped token.

Do not weaken this for convenience. See SPEC §17.
