# opencode patches

Numbered patch files applied in CI with `git apply` against the pinned upstream
tag in `agent/opencode`. See SPEC §11.1-§11.2.

Rules:

- Anything achievable through `opencode.json`, `AGENTS.md` or MCP **must not** be
  a patch. See `agent/config/`.
- Do not restructure upstream code. Patches stay small and local.
- Rebase onto upstream monthly. A patch that stops applying is a scheduled task,
  not an emergency.

| #   | File      | Purpose                                                  | Task |
| --- | --------- | -------------------------------------------------------- | ---- |
| P1  | _pending_ | Per-turn usage telemetry to a Unix socket                | 1.7  |
| P2  | _pending_ | Mid-stream budget abort                                  | 4.6  |
| P3  | _pending_ | Non-overridable permission policy from `$HALYARD_POLICY` | 1.8  |
| P4  | _pending_ | Provider base URL forced to `aigw`, local creds disabled | 1.7  |
| P5  | _pending_ | `onTurnStart` / `onTurnEnd` hooks                        | 1.8  |
| P6  | _pending_ | Stable structured event stream matching SPEC §7.2        | 1.8  |

Before writing any of these, confirm against the pinned tag whether it can be
done without patching (SPEC §22 item 6) and record the finding in
`docs/verified.md`.
