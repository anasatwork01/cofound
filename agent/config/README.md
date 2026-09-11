# Agent configuration (not patches)

Per-template `opencode.json` and `AGENTS.md`. See SPEC §11.3.

`agent/patches/` is **empty on purpose** — task 1.1 found an upstream mechanism
for all six §11.2 patches. Everything here is configuration, which is what
§11.1 requires when configuration is possible.

## The policy mechanism

SPEC §11.2 describes the non-overridable policy as loaded from
`$HALYARD_POLICY` by patch P3. **That is not how it works**, because P3 does not
exist. `sandboxd` sets two environment variables when it launches the sandbox,
and neither is optional:

| Variable                          | Value                                            | Why                                                                       |
| --------------------------------- | ------------------------------------------------ | ------------------------------------------------------------------------- |
| `OPENCODE_PERMISSION`             | inline JSON: catch-all deny plus explicit allows | Merged **after** every config file, so nothing in the repo merges over it |
| `OPENCODE_DISABLE_PROJECT_CONFIG` | `1`                                              | Closes two escapes the first variable does not. **Undocumented upstream** |

The policy is **inline JSON in the variable**, not a path to a file. A path
inside the sandbox would be writable by the thing the policy constrains.

### Why the second variable is not optional

`OPENCODE_PERMISSION` is merged last, which sounds sufficient and is not.
Permission _rules_ are evaluated with `findLast` and `Permission.merge` is plain
concatenation, so **the last matching rule wins** — and a repo-defined agent's
own rules are concatenated _after_ ours. A hostile repo ships
`.opencode/agent/helper.md` with relaxed frontmatter permissions and the policy
is gone.

Worse, and independent of permissions entirely: opencode **executes** plugins
discovered in the repository (`.opencode/plugin/`, plus npm packages named in
the repo's `opencode.json`). Plugin code gets a Bun shell handle and the
authenticated server SDK — arbitrary code execution outside the tool-permission
system, because plugins are not tools.

`OPENCODE_DISABLE_PROJECT_CONFIG=1` closes both, though **not by guarding the
loader directly**. It guards the upward `opencode.json` walk, and it removes the
repository's `.opencode` paths from `ConfigPaths.directories` — which is what
starves the loop that would otherwise load the repo's agents, commands and
plugins. Two global directories are deliberately left alone — `~/.config/opencode`
(`Global.Path.config`, the first unconditional entry in the search list) and the
home-rooted `~/.opencode` — which is why **Halyard's supervisor plugin is
installed globally in the image**, never per project.

The canonical install path is **`~/.config/opencode/plugin/`**. Note that
`Global.Path.config` is `xdgConfig/opencode`, so it honours `$XDG_CONFIG_HOME`:
the image must either set that variable deterministically or install to a
literal `~/.config/opencode/plugin`. A plugin that never loads is
indistinguishable from one that chose not to act, so this is pinned by the
contract test too.

So the line to watch on an upstream bump is `config/paths.ts`, not
`config/config.ts`. And one route into that loop is unguarded:
`OPENCODE_CONFIG_DIR` is always appended, so **never point it at anything the
repository can write.**

Because that variable is undocumented upstream, it is pinned by
`tests/contracts/test_opencode_upstream_contract.py`. A rename fails CI instead
of silently opening the sandbox. See `docs/open-questions.md` Q9.

### Not a lever: `permission.ask`

The plugin types declare a `permission.ask` hook with a vetoing signature. It is
dispatched at **zero call sites**. Building enforcement on it yields a veto that
never fires. The working interception point is `tool.execute.before`, which
denies by throwing — documented, dispatched, and awaited before the tool runs.
That is the second enforcement layer, and unlike config precedence it does not
depend on merge order.

## Security

A project's own `AGENTS.md` is untrusted input. It is merged _below_ our policy,
never above it — and note what that does **not** buy: `AGENTS.md` carries
instructions, not permissions, so it cannot alter the permission config, and no
configuration stops it influencing the model's behaviour. That is prompt
injection, a different and unsolved problem. Treating the two as one would make
the policy look like it solves more than it does.

The sharper edge is the repo's `.opencode/` directory, above. See
`docs/verified.md` §22 item 6 for the source-level detail, and the §11.3 erratum
in `docs/open-questions.md`.
