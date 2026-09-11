# Halyard

A SaaS where users build full-stack Next.js apps by prompting an agent, then
deploy, domain, monetise and market them from the same console.

**The specification is the contract.** Read [`docs/SPEC.md`](docs/SPEC.md)
before writing code, and [`docs/TASKS.md`](docs/TASKS.md) for the work
breakdown and current status.

## Quick start

```bash
mise install          # or: nvm use && asdf install   (see .tool-versions)
make doctor           # verify your toolchain
make bootstrap        # install JS, Python and Go dependencies, plus git hooks
make verify           # what CI runs: structure, lint, typecheck, test, build
make services-up      # Postgres + Redis in Docker
make test-integration # integration suite against them
make help             # all targets
```

## Contributing

`main` is protected. Branch, verify, push, open a pull request:

```bash
git switch -c feature/0.2-ci-pipeline    # or fix/<name>, update/<name>
make verify                              # must pass before you push
git push -u origin feature/0.2-ci-pipeline
```

Full conventions — branch naming, commit format, what needs an explicit note in
the PR body — are in [CONTRIBUTING.md](CONTRIBUTING.md).

## Layout

| Path                 | What                                                                             |
| -------------------- | -------------------------------------------------------------------------------- |
| `apps/console/`      | Next.js SaaS frontend (Cloudflare Workers via OpenNext)                          |
| `services/api/`      | Go - REST API, auth, SSE gateway, approval gates                                 |
| `services/gitd/`     | Go - git HTTP proxy, pre-receive policy, tree/blob/diff API                      |
| `services/aigw/`     | Go - AI gateway: provider proxy, token metering, spend caps                      |
| `services/mcp/`      | Go - MCP tool servers exposed to the sandbox                                     |
| `services/sandboxd/` | Python - Modal sandbox lifecycle, warm pool, tunnels                             |
| `services/workers/`  | Python - ads sync, SEO crawl, rating, reconciliation, GitHub sync                |
| `agent/opencode/`    | git submodule, pinned tag                                                        |
| `agent/patches/`     | empty by design - task 1.1 found an upstream mechanism for all six §11.2 patches |
| `agent/agentd/`      | Go - in-sandbox supervisor sidecar                                               |
| `packages/chassis/`  | Go - shared service chassis: config, logging, tracing, health, HTTP, errors      |
| `capabilities/`      | capability modules (auth, payments, email, uploads)                              |
| `templates/`         | app templates, each a git repo                                                   |
| `packages/schema/`   | **JSON Schema + OpenAPI: single source of truth for all types**                  |
| `db/migrations/`     | control plane migrations (goose, forward-only)                                   |
| `infra/`             | Cloudflare and Modal configuration                                               |

## The one rule that shapes everything

The sandbox is untrusted. It runs code an LLM wrote, influenced by content the
LLM read from the internet and from the user's own repository. **It never holds
a credential that can spend money or reach another tenant's data.** Everything
privileged happens in the control plane, reached over authenticated MCP or HTTP
with a short-lived, project-scoped token.

See SPEC §17 for the full security model. Do not weaken this boundary for
convenience.

## Working agreements

1. **Contracts before implementations.** Schemas in `packages/schema` first;
   generate TypeScript, Go and Python types from them. Never hand-write the
   same type twice.
2. **Verify platform facts before depending on them.** Everything in SPEC §22
   needs confirming against current vendor docs. Record findings in
   [`docs/verified.md`](docs/verified.md).
3. **Every task ships tests.** Unit tests for pure logic, integration tests
   against a real Postgres in Docker, one end-to-end test per user-visible flow.
4. **Never invent an API surface.** If a vendor endpoint isn't documented, stop
   and add it to [`docs/open-questions.md`](docs/open-questions.md).
5. **Migrations are forward-only and additive.**
6. **Don't re-litigate SPEC §5.** Those choices are made, with reasons.
