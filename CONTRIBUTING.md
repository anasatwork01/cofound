# Contributing

## Git conventions

### Branch naming

`main` is protected. Nothing lands on it except through a pull request.

| Prefix           | Use for                                                 | Example                          |
| ---------------- | ------------------------------------------------------- | -------------------------------- |
| `feature/<name>` | new capability                                          | `feature/0.2-ci-pipeline`        |
| `fix/<name>`     | fixing broken behaviour                                 | `fix/sse-reconnect-drops-events` |
| `update/<name>`  | changes to existing work: docs, deps, config, refactors | `update/git-conventions`         |

Rules for `<name>`:

- lowercase kebab-case; `a-z 0-9 . _ -` only
- describe the change, not the file (`fix/hold-ttl-never-expires`, not `fix/holds-go`)
- when the work maps to a task in [docs/TASKS.md](docs/TASKS.md), lead with the
  task ID: `feature/0.6-control-plane-schema`. One branch per task.

`scripts/check-branch.sh` enforces this, and the pre-push hook installed by
`make bootstrap` runs it. CI will enforce it on pull requests (task 0.2).

### The loop

```bash
git switch main && git pull
git switch -c feature/0.2-ci-pipeline
# ... work ...
make verify                       # must pass before you push
git push -u origin feature/0.2-ci-pipeline
gh pr create --base main --fill   # or open the compare URL git prints
```

### Commit messages

Imperative subject, 72 characters or fewer, no trailing period. Blank line.
Then a body explaining **why**, since the diff already says what.

Lead the subject with the task ID when there is one:

```
Task 0.6: add control plane schema with row-level security

RLS is defence in depth, not the primary control — application queries still
filter on org_id explicitly (SPEC §6). The integration test asserts a
cross-tenant read is denied, so a future policy regression fails CI rather
than leaking.
```

Agent-assisted commits carry a trailer:

```
Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
```

### Pull requests

- **Target `main`.** Never open a PR against another feature branch unless it
  genuinely stacks, and say so in the description if it does.
- Title: same form as the commit subject.
- Body: what changed, why, how you verified it, and the task ID it closes.
- `make verify` must pass. A red PR is a draft.
- **Squash merge**, then delete the branch. `main` keeps one commit per task,
  which is what makes `git log main` readable as a delivery record.

### Things that need an explicit note in the PR body

These come from the specification, not from taste:

- **Any destructive migration** — SPEC §0 rule 6 requires a human approval note
  in the PR. Migrations are forward-only and additive by default.
- **Anything touching `ledger_entries`** — review gate, SPEC §19.2.
- **Weakening the sandbox trust boundary** — SPEC §17. If a change puts a
  credential anywhere a sandbox can reach it, say so loudly or don't do it.
- **A new vendor dependency** — record the SPEC §22 verification in
  [docs/verified.md](docs/verified.md) in the same PR.
- **A new `agent/patches/` patch** — justify why configuration or MCP cannot do
  it (SPEC §11.1).

### What not to do

- Don't commit to `main` directly. The pre-push hook will stop you; `--no-verify`
  exists for genuine emergencies and should show up in the incident notes.
- Don't force-push a branch someone else has reviewed. Add commits instead.
- Don't force-push `main`, ever. Restores are forward-only (SPEC §12.4, §5.6).
- Don't commit generated output. `packages/schema/gen/` is rebuilt with
  `make gen`.

## Before you push

```bash
make verify     # structure, lint, typecheck, test, build — all three languages
```

`make fmt` fixes most lint failures. `make doctor` diagnoses a toolchain that
disagrees with [.tool-versions](.tool-versions).
