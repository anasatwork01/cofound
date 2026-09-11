# opencode patches

**This directory is empty, and is expected to stay that way.** SPEC §11.1-§11.2
describes numbered patch files applied in CI with `git apply` against the pinned
upstream tag in `agent/opencode`. Task 1.1 found an upstream mechanism for all
six, so there are no patch files and CI applies nothing.

The rules below apply _if_ a patch ever becomes necessary:

- Anything achievable through configuration - `opencode.json`, `AGENTS.md`, an
  environment variable such as `OPENCODE_PERMISSION`, or MCP - **must not** be a
  patch. See `agent/config/`.
- Do not restructure upstream code. Patches stay small and local.
- Rebase onto upstream monthly. A patch that stops applying is a scheduled task,
  not an emergency. Note what that would cost: opencode ships roughly one
  release every 1.4 days, and the files these patches would touch churned
  21-54 commits in 90 days.

| #   | File         | Purpose                                                                                                                                              | Task | Status                         |
| --- | ------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------- | ---- | ------------------------------ |
| P1  | _not needed_ | Per-turn usage telemetry, read off the event stream - **not** a Unix socket                                                                          | 1.9  | upstream mechanism found       |
| P2  | _not needed_ | Mid-stream budget abort                                                                                                                              | 4.6  | upstream mechanism found       |
| P3  | _not needed_ | Non-overridable permission policy (§11.2 says from `$HALYARD_POLICY`; the real mechanism is inline JSON in `OPENCODE_PERMISSION`, never a file path) | 1.10 | two env vars, one undocumented |
| P4  | _not needed_ | Provider base URL forced to `aigw`, local creds disabled                                                                                             | 1.9  | upstream mechanism found       |
| P5  | _not needed_ | `onTurnStart` / `onTurnEnd` hooks                                                                                                                    | 1.10 | start hooks; end is `agentd`'s |
| P6  | _wrong fix_  | Stable structured event stream matching SPEC §7.2                                                                                                    | 1.10 | adapter in `agentd`            |

## Why this directory is empty

Task 1.1 read the pinned tag (`v1.18.30`, `3104c1428e`) and found an upstream
mechanism for every one of the six. §11.1's own rule settles it: "Anything
achievable through `opencode.json`, `AGENTS.md` or MCP **must not** be a patch."

The short version, with the full reasoning in `docs/verified.md`:

- **P1** — `AssistantMessage.tokens` is already `{ input, output, reasoning,
cache: { read, write } }` with `cost`. §11.2 says upstream "doesn't expose
  them in the shape we need"; it does, exactly. **But `input` and `output` are
  each already net of something** — billable input is
  `input + cache.read + cache.write`. See the metering trap in
  `docs/verified.md`.
- **P2** — `POST /api/session/{sessionID}/interrupt`.
- **P3** — **two** environment variables, not one. `OPENCODE_PERMISSION` is
  merged after every config file, but permission _rules_ are evaluated with
  `findLast` and a repo's `.opencode/agent/*.md` frontmatter is concatenated
  **after** it — so `OPENCODE_PERMISSION` alone is escapable.
  `OPENCODE_DISABLE_PROJECT_CONFIG=1` is what closes it, and that flag is
  **undocumented** (Q9). It is also load-bearing for §17 independently: without
  it, a repo's `.opencode/plugin/` executes arbitrary JavaScript at startup.
- **P4** — `provider.<id>.options.baseURL` is a config key.
- **P5** — turn **start** is the blocking `chat.message` plugin hook. Turn
  **end** has no blocking hook at all, so `agentd` owns it by serialising on the
  event stream: wait for `session.idle`, commit, swap the lease, then submit the
  next prompt. Zero patches, but a different shape than §11.2 assumes.
- **P6** — the goal is real but a patch is the wrong shape for it. A patch
  making upstream emit our events must be rebased forever; an adapter in
  `agentd` over the documented 94-event stream need not be, and is testable
  against `agent-events.schema.json`.

**This is a reading of the source, not a test of it.** Tasks 1.7 and 1.8 should
assert each behaviour against a running `opencode serve` before the patches are
considered permanently unnecessary — in particular against a hostile fixture
repo carrying `.opencode/opencode.json`, `.opencode/agent/evil.md` **and**
`.opencode/plugin/evil.ts`, proving the policy holds and the plugin never runs.

One trap found while reading, recorded so nobody rediscovers it: the
`permission.ask` plugin hook is **declared in the public types with a vetoing
signature but dispatched at zero call sites**. Do not build policy enforcement
on it. The working interception point is `tool.execute.before`, which denies by
throwing.

If a patch does turn out to be needed, the rules above still apply: small,
local, no restructuring, and rebased monthly.
