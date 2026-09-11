"""Source-level pins on the upstream mechanisms Halyard now depends on.

SPEC §11.1 says anything achievable by configuration must not be a patch, and
task 1.1 found that all six §11.2 patches have an upstream mechanism on the
pinned tag. So `agent/patches/` is empty and Halyard depends on `opencode`
source behaviour it does not own. Two of those dependencies are load-bearing in
a way a passing test suite would not otherwise notice:

* **§17's sandbox boundary** rests on `OPENCODE_PERMISSION` being applied after
  every config merge *and on nothing merging after it*, on
  `OPENCODE_DISABLE_PROJECT_CONFIG` suppressing the repository's own config
  *and* its `.opencode/plugin/` code execution, on `Global.Path.config`
  (`~/.config/opencode`) staying *outside* that suppression so the supervisor
  plugin Halyard installs at `~/.config/opencode/plugin/` keeps loading, and on
  `Plugin.trigger` still awaiting each hook so a `tool.execute.before` that
  throws actually denies the call. `OPENCODE_DISABLE_PROJECT_CONFIG` is
  **undocumented upstream** — it exists only in source — which is why
  docs/open-questions.md Q9 resolved to "use it, and pin it".
* **§19's ledger** rests on `tokens.input` already being net of cache reads and
  writes. Bill `tokens.input` alone and Halyard under-bills by the entire cache
  volume, which on a cached agent loop is the majority of input tokens. Before
  any of that arithmetic it rests on a configured
  `provider.<id>.options.baseURL` outranking the model's own API URL, because
  that is the only thing routing provider calls through `aigw`: a call that goes
  direct is not mis-billed, it is never billed at all.

The valuable assertion is therefore **not** that these files parse or that the
strings exist somewhere in the tree. It is that each mechanism is still in the
file that gives it its meaning, and — where the guarantee is an *ordering* —
that the ordering still holds. A rename, a move, or a reshuffle of the merge
chain fails here loudly at upgrade time instead of silently opening the sandbox
or silently mis-billing.

Three deliberate design rules, each learned the hard way in this repository:

1. **No assertion on a line number.** Upstream ships roughly one release every
   1.4 days and these files churn hard; line numbers drift on every bump and
   would produce nothing but false failures. Each pin matches a *code shape*
   scoped to one file, and reports the line it found (or last saw) for the human
   reading the failure.
2. **Pin what may come after with a whitelist, not a blacklist.** A list of
   known-earlier merge stages cannot see a stage that did not exist when the
   list was written, and that is exactly the change worth catching: appending
   `result = mergeDeep(result, yield* loadFile(...))` to Config's resolve
   defeats §17 outright while every named-stage assertion stays green. So the
   ordering pin scans the tail of resolve for seven code shapes that write
   config — five keyed on the identifier `result`, two on a config-merge callee
   — allows only the four write shapes present at the pinned tag, and fails on
   anything else those shapes catch. Inherit the limit rather than
   rediscovering it: seven *shapes* is not a proof of absence. A write that
   never names `result` inside the scanned region — through an alias taken
   earlier in resolve, through a closure that captured it, or from code outside
   resolve entirely — is invisible to it. The pin's own docstring records which
   of those are closed and which are out of scope by construction.
3. **Fail, don't skip, when the submodule is missing in CI.** Task 0.6 shipped a
   guard keyed on `CI` when the real capability was `DATABASE_URL`, and the test
   silently never ran. Here the capability is "is `agent/opencode` checked out",
   so that is what `opencode_src` keys on — and
   `test_ci_python_job_checks_out_the_submodule` asserts the workflow wiring
   itself, because a pin that skips everywhere pins nothing.
"""

import json
import os
import re
import subprocess
from pathlib import Path

import pytest

REPO_ROOT = Path(__file__).resolve().parents[2]
OPENCODE = REPO_ROOT / "agent" / "opencode"
CI_WORKFLOW = REPO_ROOT / ".github" / "workflows" / "ci.yml"

# The submodule state these pins were read against, recorded in docs/verified.md
# under "SPEC §22 item 6". A bump is not a formality: verified.md says outright
# that "a later tag must be re-checked", and this is the tripwire that enforces
# it. See test_submodule_matches_the_ref_these_pins_were_verified_against.
PINNED_TAG = "v1.18.30"
PINNED_COMMIT = "3104c1428ec91f809e5ab86631300de41eb6952e"

# Paths are relative to the submodule root, and are part of each pin: a
# mechanism that moved to another file is a change we want to review, not
# something a tree-wide grep should paper over.
CONFIG = "packages/opencode/src/config/config.ts"
PATHS = "packages/opencode/src/config/paths.ts"
FLAG = "packages/core/src/flag/flag.ts"
PERMISSION = "packages/opencode/src/permission/index.ts"
AGENT = "packages/opencode/src/agent/agent.ts"
CONFIG_AGENT = "packages/opencode/src/config/agent.ts"
CONFIG_PLUGIN = "packages/opencode/src/config/plugin.ts"
SESSION = "packages/opencode/src/session/session.ts"
PLUGIN_TYPES = "packages/plugin/src/index.ts"
PLUGIN_DISPATCH = "packages/opencode/src/plugin/index.ts"
PROVIDER = "packages/opencode/src/provider/provider.ts"
GLOBAL_PATHS = "packages/core/src/global.ts"
OPENAPI = "packages/sdk/openapi.json"

# Where hooks are dispatched from. Pin 7 asserts an *absence* over this subtree,
# so it has to be the subtree that does the dispatching.
DISPATCH_ROOT = "packages/opencode/src"


# --------------------------------------------------------------- capability gate


def _in_ci() -> bool:
    return os.environ.get("CI", "").strip().lower() not in ("", "0", "false")


def _bail(reason: str) -> None:
    """Fail in CI, skip outside it. Never silently pass.

    The distinction is not "is this CI" standing in for a capability — the
    capability is checked separately and first. It is "should this environment
    have had the capability": a developer who has not run `git submodule update`
    gets a skip and an instruction, while CI gets a failure, because the one CI
    job that runs these tests is wired to check the submodule out and a skip
    there would mean these pins assert nothing at all.
    """
    if _in_ci():
        pytest.fail(reason)
    pytest.skip(reason)


@pytest.fixture(scope="module")
def opencode_src() -> Path:
    probe = OPENCODE / CONFIG
    if probe.is_file():
        return OPENCODE
    _bail(
        "the `agent/opencode` submodule is not checked out, so every upstream pin in "
        "this file would assert nothing.\n\n"
        "  expected  " + str(probe) + "\n\n"
        "  Locally:   git submodule update --init agent/opencode\n"
        "  In CI:     the `python` job's actions/checkout step must keep "
        "`submodules: true`.\n"
        "             If it was removed, restore it — see "
        "test_ci_python_job_checks_out_the_submodule\n"
        "             in this file, and docs/open-questions.md Q9 for why these pins "
        "exist."
    )
    raise AssertionError("unreachable")  # pragma: no cover - _bail always raises


# ------------------------------------------------------------------- primitives


def _read(root: Path, rel: str) -> str:
    return (root / rel).read_text(encoding="utf-8")


def _line_of(text: str, index: int) -> int:
    return text.count("\n", 0, index) + 1


def _where(text: str, match: re.Match[str] | None, was: str) -> str:
    """Report the line for a human without ever asserting on it."""
    if match is None:
        return f"not found (at {PINNED_TAG} it was {was})"
    return f"line {_line_of(text, match.start())} (at {PINNED_TAG} it was {was})"


def _broken(
    mechanism: str,
    *,
    file: str,
    expected: str,
    was: str,
    guarantee: str,
    spec: str,
    action: str,
    found: str = "",
) -> str:
    """Format the failure a person reads mid-upgrade, with no other context."""
    lines = [
        "",
        f"UPSTREAM opencode PIN BROKEN - {mechanism}",
        "",
        f"  file       agent/opencode/{file}",
        f"  expected   {expected}",
        f"  at {PINNED_TAG}  {was}",
    ]
    if found:
        lines.append(f"  found      {found}")
    lines += [
        "",
        f"  Halyard depends on this ({spec}):",
        f"      {guarantee}",
        "",
        "  What to do now:",
        f"      {action}",
        "",
        "  Context: docs/verified.md section 'SPEC §22 item 6 — `opencode`' records the reading",
        "  these pins encode, and docs/open-questions.md Q9 records the decision to depend on",
        "  upstream rather than fork. Both must be revised in the same change as this pin.",
        "",
    ]
    return "\n".join(lines)


def _last(text: str, pattern: str) -> re.Match[str] | None:
    """Last match, because 'nothing merges after this' is about the last one."""
    matches = list(re.finditer(pattern, text))
    return matches[-1] if matches else None


_CLOSERS = {"(": ")", "[": "]", "{": "}"}


def _balanced(text: str, open_at: int) -> tuple[int, int] | None:
    """Span of the bracketed group opening at `open_at`; end is exclusive.

    Bracket counting, not a TypeScript parser: adequate because every caller
    points it at one small, bracket-literal-free region of one file, and it
    returns None rather than guessing when the count never closes. Only the
    opening bracket's own kind is counted, so a nested group of another kind
    cannot unbalance it.

    Every structural claim in this file is made with this rather than with a
    regex, because the regex version of "is X inside this block" is a
    line-oriented approximation that a reformat quietly turns into "no".
    """
    if not 0 <= open_at < len(text) or text[open_at] not in _CLOSERS:
        return None
    opener = text[open_at]
    closer = _CLOSERS[opener]
    depth = 0
    for i in range(open_at, len(text)):
        if text[i] == opener:
            depth += 1
        elif text[i] == closer:
            depth -= 1
            if depth == 0:
                return (open_at, i + 1)
    return None


def _balanced_block(text: str, open_brace: int) -> str | None:
    """The `{...}` block starting at `open_brace`, or None if unbalanced."""
    span = _balanced(text, open_brace)
    return None if span is None else text[span[0] : span[1]]


def _call_args(text: str, call: re.Match[str]) -> str | None:
    """Argument text of a call whose match ends one character past its `(`.

    Balanced, so an argument list prettier has wrapped across four lines reads
    the same as a single-line one. A single-line `[^\\n]*` here matches nothing
    the moment the call wraps, and a loop over no matches is a pin that passes
    without asserting anything.
    """
    span = _balanced(text, call.end() - 1)
    if span is None:
        return None
    return text[span[0] + 1 : span[1] - 1]


def _enclosing_braces(text: str, start: int, position: int) -> list[int]:
    """Offsets of every `{` opened at or after `start` and still open at `position`.

    Innermost last, so `len()` is the nesting depth and `[-1]` is the block a
    human should go and read. This is how "that statement is at the top level of
    that loop body" becomes a structural claim instead of an indentation guess.
    """
    stack: list[int] = []
    for i in range(start, position):
        if text[i] == "{":
            stack.append(i)
        elif text[i] == "}" and stack:
            stack.pop()
    return stack


def _statement_start(text: str, region_start: int, position: int) -> int:
    """Offset where the statement containing `position` begins.

    Upstream runs prettier with `semi: false`, so a statement boundary is a
    newline at the enclosing block's own depth. Depth is counted from
    `region_start` across all three bracket kinds, so a newline inside a wrapped
    argument list or array literal is not mistaken for a boundary.
    """
    depth = 0
    boundary = region_start
    for i in range(region_start, position):
        char = text[i]
        if char in "{([":
            depth += 1
        elif char in "})]":
            depth -= 1
        elif char == "\n" and depth == 0:
            boundary = i + 1
    return boundary


# ------------------------------------------------------- 1. OPENCODE_PERMISSION


def test_opencode_permission_is_applied_after_the_whole_merge_chain(
    opencode_src: Path,
) -> None:
    """§17: the injected tool policy is safe only because nothing merges after it.

    `OPENCODE_PERMISSION` is applied with `mergeDeep` over `result.permission`
    once every config file has been merged: global config, `$OPENCODE_CONFIG`,
    the project `opencode.json` walk, every discovered `.opencode` directory,
    `OPENCODE_CONFIG_CONTENT` and the managed config. If it moves earlier in
    that chain, a file in the user's repository can merge over Halyard's policy.

    Asserted twice, because the two halves catch different changes:

    * each named stage is located and required to sit *before* the application
      point — which names the culprit when a stage upstream already had moves;
    * the remainder of resolve is then scanned for seven code shapes that write
      config — five keyed on the identifier `result`, two on a config-merge
      callee — and only the four write shapes present at the pinned tag are
      allowed, which is the half that notices a stage nobody has named yet.

    Be exact about what that second half proves, because the wording decides
    whether someone lands a bump. It is not "nothing writes config after the
    policy is applied". It is "no code matching these seven shapes, other than
    the four allowed writes, appears between the application and resolve's
    return" — and the four allowed writes are matched as shapes too, so a
    refactor of one of them reports as an offender rather than passing on the
    strength of its field name. Three consequences, two closed and one open:

    * an alias taken *inside* the region is closed. A bare `result` that is not
      a property access is itself one of the shapes, and the alias assertion
      below reports `const cfg = result` with its own diagnosis rather than as
      a generic offender.
    * an alias taken *earlier* in resolve, or a closure that captured `result`
      before the policy was applied, is not visible here and will not be
      without a TypeScript parser. Nothing in this file claims otherwise.
    * a write *after* resolve returns is out of scope by construction. The
      region ends at `return { config: result, ... }`; `Config.state`
      (config.ts:614-618) hands that object to `InstanceState`, and
      `Config.get()` returns the same mutable object to every caller. Code that
      mutates it there is outside every pin in this file, and deliberately so:
      pinning it would mean pinning the whole file, which drifts on every
      release and would fail for reasons that have nothing to do with §17.
    """
    text = _read(opencode_src, CONFIG)

    apply_re = (
        r"result\.permission\s*=\s*mergeDeep\(\s*result\.permission\s*\?\?\s*\{\}\s*,\s*"
        r"JSON\.parse\(\s*Flag\.OPENCODE_PERMISSION\s*\)\s*\)"
    )
    applied = _last(text, apply_re)
    assert applied is not None, _broken(
        "OPENCODE_PERMISSION is no longer merged into the resolved permission config",
        file=CONFIG,
        expected="result.permission = mergeDeep(result.permission ?? {}, "
        "JSON.parse(Flag.OPENCODE_PERMISSION))",
        was="config.ts:561, inside `if (Flag.OPENCODE_PERMISSION)`",
        guarantee="The sandbox's entire tool policy is injected through this one env var. "
        "If opencode no longer reads it, the sandbox runs with opencode's defaults "
        "and nothing Halyard sets constrains the agent.",
        spec="SPEC §17.1",
        action="Find how the resolved permission config is now overridden from outside the "
        "repository and re-point this pin at it. If there is no such mechanism, STOP: "
        "§11.2's P3 needs a patch again and agent/patches/ is no longer empty.",
    )
    apply_at = applied.start()

    # Every earlier stage of the merge chain, with the last occurrence of each,
    # because the guarantee is about what comes last rather than what exists.
    stages = [
        (
            "global config",
            r'yield\*\s*merge\(Global\.Path\.config,\s*global,\s*"global"\)',
            "config.ts:413",
        ),
        (
            "$OPENCODE_CONFIG",
            r"if\s*\(\s*Flag\.OPENCODE_CONFIG\s*\)",
            "config.ts:415",
        ),
        (
            "project opencode.json walk",
            r"if\s*\(\s*!Flag\.OPENCODE_DISABLE_PROJECT_CONFIG\s*\)",
            "config.ts:420",
        ),
        (
            "repo-discovered agents",
            r"ConfigAgent\.load\(dir\)",
            "config.ts:474",
        ),
        (
            "repo-discovered plugins",
            r"ConfigPlugin\.load\(dir\)",
            "config.ts:478",
        ),
        (
            "OPENCODE_CONFIG_CONTENT",
            r"process\.env\.OPENCODE_CONFIG_CONTENT",
            "config.ts:481",
        ),
        (
            "managed (org) config",
            r"mergeConfigConcatArrays\(",
            "config.ts:541",
        ),
    ]
    for label, pattern, was in stages:
        stage = _last(text, pattern)

        # Two distinct failures, deliberately not collapsed into one. A stage
        # that vanished means this pin can no longer PROVE the ordering; a stage
        # that moved past the env var means the ordering is BROKEN. Reporting
        # the second when the truth is the first sends the reader hunting for a
        # reordering that never happened.
        assert stage is not None, _broken(
            f"the {label} stage of the config merge chain has moved, been renamed, or gone",
            file=CONFIG,
            expected=f"the {label} stage to still be recognisable in Config's resolve",
            was=was,
            guarantee="This pin proves OPENCODE_PERMISSION is applied after every config "
            f"stage by locating each stage and comparing positions. With {label} "
            "unrecognisable it proves nothing about that stage, and the sandbox policy's "
            "precedence is unverified rather than verified.",
            spec="SPEC §17.1",
            action="Read Config's resolve, find what that stage became, and re-point this "
            "pin's pattern at it. Do not delete the entry: the stage merging after "
            "OPENCODE_PERMISSION is exactly the escape being watched for.",
            found=f"pattern {pattern!r} matched nowhere in the file",
        )
        assert stage.start() < apply_at, _broken(
            f"OPENCODE_PERMISSION is no longer applied after the {label} stage",
            file=CONFIG,
            expected=f"the last merge of the {label} stage to appear BEFORE the "
            "OPENCODE_PERMISSION application",
            was=f"{was}, before config.ts:561",
            guarantee="OPENCODE_PERMISSION carries the sandbox policy and is only "
            "authoritative because it is applied last. A config stage that now runs "
            f"after it ({label}) can merge over the policy — and repo-discovered "
            "agents and plugins come from the user's untrusted repository.",
            spec="SPEC §17.1",
            action="Read the merge chain in Config's resolve and establish what is now "
            "last. If any repository-sourced stage merges after OPENCODE_PERMISSION, "
            "treat the sandbox policy as unenforceable until it is fixed and do not "
            "land the bump.",
            found=_where(text, stage, was)
            + f"; OPENCODE_PERMISSION applied at line {_line_of(text, apply_at)}",
        )

    # The list above is a blacklist of stage names that existed at the pinned
    # tag, so it is blind to a stage that did not. Four lines appended after the
    # OPENCODE_PERMISSION block are enough to defeat it entirely:
    #
    #     const extra = yield* loadFile(
    #       path.join(ctx.directory, "opencode.repo.json"), authEnv)
    #     result = mergeDeep(result, extra)
    #
    # — a file in the user's own repository, merged source-wins over the
    # injected policy, with all seven ordering assertions still green. So the
    # rest of resolve is scanned for the shapes that write config, and only the
    # four write shapes present at the pinned tag are allowed through.
    closing = _last(text, r"return \{\s*config:\s*result\s*,")
    assert closing is not None, _broken(
        "the end of Config's resolve can no longer be located",
        file=CONFIG,
        expected="`return { config: result, directories, ... }` closing Config's resolve",
        was="config.ts:600-601",
        guarantee="The check below bounds itself between the OPENCODE_PERMISSION "
        "application and the end of resolve. Without the closing anchor it would scan "
        "the rest of the file - every unrelated helper - and report merges that have "
        "nothing to do with the sandbox policy. Unbounded, it is worse than absent.",
        spec="SPEC §17.1",
        action="Read the end of Config's resolve, re-anchor this bound on whatever now "
        "hands `result` back, and re-run. Do not delete the bound.",
    )
    # From the END of the application, so the application itself is not read as
    # a stage that merges after it.
    tail = text[applied.end() : closing.start()]

    # Every write the tail performs at the pinned tag, each with the reason it
    # cannot carry repository-sourced permission config. Anything else one of
    # the `writes` scans below finds is an offender. `\s*` and `,?` throughout
    # because upstream runs prettier: one more argument or one longer identifier
    # rewraps a call and moves the commas.
    allowed_shapes = [
        (
            # The `result.tools` compatibility shim. The first argument is
            # matched as ANY identifier rather than the literal `perms`:
            # renaming a local is a pure refactor with no semantic change, and
            # pinning the name made it fail here under the headline "a config
            # stage now merges AFTER OPENCODE_PERMISSION" with an evidence line
            # byte-identical to the real shim - a sandbox-is-overridable alarm
            # with nothing in it hinting that a variable had been renamed. The
            # precedence claim is carried by the argument ORDER, which
            # test_nothing_outranks_result_permission_after_the_env_var_is_applied
            # asserts by reading the LAST argument, and never by the name of the
            # first one.
            r"result\.permission\s*=\s*mergeDeep\(\s*[A-Za-z_$][\w$]*\s*,\s*"
            r"result\.permission\s*\?\?\s*\{\}\s*,?\s*\)",
            "the `result.tools` shim, with `result.permission` passed LAST",
        ),
        (
            r'result\.username\s*=\s*(?:os\.userInfo\(\)\.username\s*\|\|\s*)?"user"',
            "the OS username fallback - a string, not config",
        ),
        (
            r'result\.share\s*=\s*"auto"',
            "the `autoshare` compatibility alias",
        ),
        (
            r"result\.compaction\s*=\s*\{\s*\.\.\.result\.compaction\s*,\s*"
            r"(?:auto|prune):\s*false\s*,?\s*\}",
            "the autocompact and prune flags - booleans, not permissions",
        ),
    ]
    allowed = {
        match.start() for pattern, _ in allowed_shapes for match in re.finditer(pattern, tail)
    }

    # (d) of the shapes that defeated the callee-keyed version of this scan:
    # `const cfg = result`, then `cfg.permission = mergeDeep(cfg.permission ??
    # {}, extra)`. The bare-`result` shape below does catch the alias itself,
    # but reports it as "uses `result` as a whole value", which sends the reader
    # hunting for a call. Asserted first, and separately, so the message says
    # what it is.
    aliases = [
        f"line {_line_of(text, applied.end() + hit.start())}: {' '.join(hit.group(0).split())!r}"
        for hit in re.finditer(
            r"\b(?:const|let|var)\s+[A-Za-z_$][\w$]*\s*(?::[^=\n]*)?=\s*result\b(?!\s*[.\[])",
            tail,
        )
    ]
    assert not aliases, _broken(
        "the resolved config is aliased after OPENCODE_PERMISSION is applied",
        file=CONFIG,
        expected="no `const x = result` between the OPENCODE_PERMISSION application and "
        "resolve's return",
        was="no alias at all - every write in that region names `result` directly",
        guarantee="Five of the seven shapes the scan below uses are keyed on the "
        "identifier `result`, and the other two on a config-merge callee. An alias defeats "
        "all five and needs neither of the two: `cfg.permission = mergeDeep(cfg.permission "
        "?? {}, extra)` merges over the injected sandbox policy with every pin in this file "
        "green. Forbidding the alias is cheap; the TypeScript parser that would be needed "
        "to follow one is not.",
        spec="SPEC §17.1",
        action="Read what the alias is for. If it is genuinely read-only, add the alias AND "
        "every write made through it to `allowed_shapes` above, together, with the reason. "
        "If anything writes permission config through it, §17's policy is overridable and "
        "the bump must not land - §11.2's P3 needs a patch again.",
        found=("\n             ").join(aliases),
    )

    # Shapes keyed on the write TARGET as well as on the callee. Keyed only on
    # the callee, this scan was defeated three ways with every assertion green:
    # an aliased remeda import (`import { mergeDeep, mergeDeep as deepMerge }`,
    # which also keeps the remeda pin below green), a plain object spread, and
    # handing `result` to a helper that mutates it in place.
    #
    # The third element of each entry answers one question: can this shape reach
    # `result.permission`? It decides the headline, because an alarm that does
    # not match its own evidence teaches the next reader to skim this failure.
    # `None` means "read it off the matched text", since a write to one named
    # field of `result` is a different claim from a write to the policy itself.
    writes = [
        (r"\bresult\s*=(?!=)", "reassigns the whole resolved config", True),
        (
            r"\bresult(?:\.[A-Za-z_$][\w$]*)+\s*=\s*(?:mergeDeep|mergeConfig[A-Za-z]*)\s*\(",
            "merges into a field of `result`",
            None,
        ),
        (
            # Any write through a property path on `result`, whatever the
            # right-hand side is: a spread, a call, a literal, a same-named
            # import under another alias. `(?<![=!<>])=(?![=>])` so `===`, `!=`,
            # `>=` and `=>` are not read as assignments.
            r"\bresult(?:\.[A-Za-z_$][\w$]*)+[^\n=;]*(?<![=!<>])=(?![=>])",
            "writes into a field of `result`",
            None,
        ),
        (r"Object\.assign\(\s*result\b", "assigns onto `result` wholesale", True),
        (r"yield\*\s*merge\(", "runs a `merge(source, ...)` config-file stage", True),
        (r"mergeConfigConcatArrays\s*\(", "runs a concat-arrays config merge", True),
        (
            # `result` used as a whole value rather than read through a
            # property: handed to a call that can mutate it, aliased, or
            # reassigned. Every mention in the tail at the pinned tag is a
            # property access, so this matches nothing there.
            r"\bresult\b(?!\s*\.)",
            "uses `result` as a whole value - handed to a call, aliased or captured",
            True,
        ),
    ]

    # One offender per source line, described by the most specific shape that
    # matched it, and severe if ANY shape on that line can reach the policy.
    source_lines = text.splitlines()
    found: dict[int, tuple[str, bool]] = {}
    for pattern, what, severity in writes:
        for hit in re.finditer(pattern, tail):
            if hit.start() in allowed:
                continue
            line = _line_of(text, applied.end() + hit.start())
            reaches = severity if severity is not None else ".permission" in hit.group(0)
            if line in found:
                found[line] = (found[line][0], found[line][1] or reaches)
            else:
                found[line] = (what, reaches)

    offenders = [
        f"line {line}: {what} - {source_lines[line - 1].strip()[:110]!r}"
        for line, (what, _) in sorted(found.items())
    ]
    reaches_policy = any(reaches for _, reaches in found.values())
    assert not offenders, _broken(
        # The strong headline is a claim about the sandbox, so it is reserved
        # for evidence that supports it: a write that targets
        # `result.permission`, replaces `result`, or hands `result` somewhere it
        # can be mutated. A write to some other field - `result.compaction`, say
        # - follows the policy application in program order without touching it.
        # That is still worth reviewing, and it is not a sandbox escape.
        "a config stage now merges AFTER OPENCODE_PERMISSION"
        if reaches_policy
        else "a config write now follows OPENCODE_PERMISSION",
        file=CONFIG,
        expected="the only config writes after the OPENCODE_PERMISSION application to be "
        "the four shapes verified at the pinned tag, exactly as verified: the "
        "`result.tools` shim (`result.permission = mergeDeep(<any ident>, "
        "result.permission ?? {})`), the OS username fallback, the `autoshare` alias, and "
        "the compaction flags (`result.compaction = { ...result.compaction, <flag>: "
        "false }`). See `allowed_shapes` for the exact patterns and the reason each is "
        "harmless",
        was="those four shapes, and nothing else, between config.ts:561 and the return at "
        "config.ts:600",
        guarantee=(
            "OPENCODE_PERMISSION carries the entire sandbox tool policy and is "
            "authoritative only because it is applied last. `mergeDeep` is source-wins, so "
            "a stage that merges after it - loading one more file, from anywhere - replaces "
            "the policy wholesale. The stage need not be repository-sourced to be a "
            "problem: it need only be reachable by something the repository controls."
            if reaches_policy
            else "This region is whitelisted rather than blacklisted so that a config stage "
            "nobody has named yet cannot slip in unseen - that is the change that would "
            "defeat §17 with every other assertion in this file green. On the evidence "
            "below nothing targets `result.permission` and nothing replaces `result`, so "
            "the injected policy still wins; the write is reported because an unreviewed "
            "addition to this region is how the dangerous case would arrive."
        ),
        spec="SPEC §17.1",
        action=(
            "Read the stage listed below and establish whether it can carry "
            "repository-sourced content. If it can, §17's policy is overridable and the "
            "bump must not land - §11.2's P3 needs a patch again. If it provably cannot, "
            "add its shape to `allowed_shapes` above with a comment recording why, so the "
            "next reader inherits the reasoning rather than repeating it. If the match is a "
            "bare `result` handed to a call, read what the callee does with it: this pin "
            "cannot tell a read from a mutation, and a proven read belongs in "
            "`allowed_shapes` with that reason."
            if reaches_policy
            else "Read the write listed below. If it provably cannot carry permission "
            "config - another field of `result`, a boolean, a string - add its shape to "
            "`allowed_shapes` above with the reason, so the next reader inherits it. Only "
            "if it can reach `result.permission`, or replace `result`, is this a §17 "
            "problem; in that case treat the sandbox policy as unenforceable and do not "
            "land the bump."
        ),
        found=("\n             ").join(offenders),
    )


def test_nothing_outranks_result_permission_after_the_env_var_is_applied(
    opencode_src: Path,
) -> None:
    """§17: remeda's mergeDeep is source-wins, so argument order is the policy.

    One assignment does follow the env var: the `tools` compatibility shim,
    written `mergeDeep(perms, result.permission ?? {})` — `result.permission`
    second, so the env var still wins. Any later assignment that puts
    `result.permission` first would silently demote it.

    This is the assertion that carries the precedence claim, and it reads the
    LAST argument to do it. The first argument's *name* is deliberately pinned
    nowhere: the whitelist in the pin above matches any identifier there for the
    same reason, because renaming a local changes nothing about which argument
    wins and must not raise a §17 alarm.

    The argument text is read with a balanced-paren scan rather than a
    line-oriented regex. Upstream runs prettier at a 120-column width, so one
    more argument or one longer identifier wraps this call across lines — and a
    single-line pattern then matches nothing, iterates over nothing, and passes.
    """
    text = _read(opencode_src, CONFIG)
    apply_at = _last(
        text,
        r"JSON\.parse\(\s*Flag\.OPENCODE_PERMISSION\s*\)",
    )
    assert apply_at is not None, "covered by the pin above; this test needs its anchor"

    for assignment in re.finditer(r"result\.permission\s*=\s*mergeDeep\s*\(", text):
        if assignment.start() <= apply_at.start():
            continue
        args = _call_args(text, assignment)
        assert args is not None, _broken(
            "a `result.permission = mergeDeep(...)` argument list could not be read",
            file=CONFIG,
            expected="a balanced argument list, so the last argument can be identified",
            was="config.ts:577, `mergeDeep(perms, result.permission ?? {})`",
            guarantee="This pin proves the injected policy still wins by reading which "
            "argument comes last. A call it cannot bracket-match leaves that unproven "
            "rather than proven, and unproven is what the pin exists to prevent.",
            spec="SPEC §17.1",
            action="Read the call at the line below and re-point this pin at its shape.",
            found=f"line {_line_of(text, assignment.start())}: unbalanced from `mergeDeep(`",
        )
        # `,?\s*$` rather than `\s*$`: prettier adds a trailing comma when it
        # wraps the call, and "last argument" must not depend on formatting.
        assert re.search(r",\s*result\.permission\s*\?\?\s*\{\}\s*,?\s*$", args, re.DOTALL), (
            _broken(
                "a later merge into result.permission no longer keeps it as the winning argument",
                file=CONFIG,
                expected="every `result.permission = mergeDeep(...)` after the "
                "OPENCODE_PERMISSION application to pass `result.permission ?? {}` LAST",
                was="config.ts:577, `mergeDeep(perms, result.permission ?? {})`",
                guarantee="mergeDeep is source-wins: the last argument overrides the first. "
                "An assignment that puts result.permission first lets whatever is being "
                "merged in override the policy Halyard injected, without moving a single "
                "line of the merge chain.",
                spec="SPEC §17.1",
                action="Read the new assignment and work out what now wins. If it can carry "
                "anything sourced from the user's repository, the sandbox policy is "
                "overridable and the bump must not land.",
                found=f"line {_line_of(text, assignment.start())}: "
                f"result.permission = mergeDeep({' '.join(args.split())})",
            )
        )


def test_mergedeep_is_still_remedas_source_wins_implementation(
    opencode_src: Path,
) -> None:
    """§17: the two pins above conclude "the last argument wins" from this import.

    Neither pin reads `mergeDeep`; both reason about its semantics. remeda's
    `mergeDeep(target, source)` is source-wins, which is what makes
    `mergeDeep(result.permission ?? {}, JSON.parse(OPENCODE_PERMISSION))` an
    override rather than a suggestion. Swap the import for a local helper of the
    same name — or for lodash's `merge`, which mutates the target instead — and
    both pins stay green while their conclusion inverts.
    """
    text = _read(opencode_src, CONFIG)

    imported = _last(text, r'import\s*\{[^}]*\bmergeDeep\b[^}]*\}\s*from\s*"remeda"')
    assert imported is not None, _broken(
        "`mergeDeep` no longer comes from remeda",
        file=CONFIG,
        expected='`import { mergeDeep } from "remeda"`',
        was="config.ts:7",
        guarantee="Every precedence claim in this file's §17 pins rests on remeda's "
        "source-wins merge: later argument overrides earlier. A same-named helper with "
        "target-wins semantics would make OPENCODE_PERMISSION the *loser* of every merge "
        "it appears in - the sandbox policy applied, logged, and then discarded - with "
        "both argument-order pins still passing.",
        spec="SPEC §17.1",
        action="Read the new implementation and establish which argument wins. If it is "
        "the first, invert both argument-order pins above and re-derive the P3 precedence "
        "chain in docs/verified.md 'SPEC §22 item 6' before landing the bump.",
    )


# ------------------------------------- 2. OPENCODE_DISABLE_PROJECT_CONFIG guards


def test_the_disable_project_config_env_var_names_have_not_changed(
    opencode_src: Path,
) -> None:
    """§17: these two strings are the whole interface Halyard sets.

    `Flag` reads them by literal name. A rename upstream would leave Halyard
    setting env vars nobody reads — the sandbox would boot, run, and enforce
    nothing. This is the single most valuable assertion in the file, because
    `OPENCODE_DISABLE_PROJECT_CONFIG` is not in opencode's documented
    environment-variable table at all (docs/open-questions.md Q9).
    """
    text = _read(opencode_src, FLAG)

    for name, pattern, was in [
        (
            "OPENCODE_DISABLE_PROJECT_CONFIG",
            r"get OPENCODE_DISABLE_PROJECT_CONFIG\(\)\s*\{\s*"
            r'return truthy\("OPENCODE_DISABLE_PROJECT_CONFIG"\)\s*\}',
            "flag.ts:54-56",
        ),
        (
            "OPENCODE_PERMISSION",
            r"get OPENCODE_PERMISSION\(\)\s*\{\s*"
            r'return process\.env\["OPENCODE_PERMISSION"\]\s*\}',
            "flag.ts:69-71",
        ),
    ]:
        found = _last(text, pattern)
        assert found is not None, _broken(
            f"the {name} environment variable is no longer read under that name",
            file=FLAG,
            expected=f'a `Flag.{name}` accessor reading process.env["{name}"]',
            was=was,
            guarantee="Halyard sets both variables when it launches opencode in the "
            "sandbox. They are the only mechanism that (a) injects the tool policy and "
            "(b) suppresses the untrusted repository's own config, agents and "
            "auto-executed plugins. A renamed variable is set-but-ignored: no error, no "
            "warning, no policy."
            + (
                " This one is undocumented upstream, so a rename would not even appear "
                "in a changelog."
                if name == "OPENCODE_DISABLE_PROJECT_CONFIG"
                else ""
            ),
            spec="SPEC §17.1, §17.3",
            action="Find the new name in agent/opencode/packages/core/src/flag/flag.ts, "
            "update every place Halyard sets it (sandbox image, sandboxd launch env, "
            "docs/verified.md §22 item 6 and Q9), and update this pin. Until then, "
            "assume a hostile repo can relax the policy and execute its own JavaScript.",
        )
        assert found.group(0).count(name) == 2, "accessor and env key must agree"


def test_disable_project_config_guards_the_project_opencode_json_walk(
    opencode_src: Path,
) -> None:
    """§17: the flag must still suppress the repo's own opencode.json files."""
    text = _read(opencode_src, CONFIG)

    guard = _last(text, r"if\s*\(\s*!Flag\.OPENCODE_DISABLE_PROJECT_CONFIG\s*\)\s*\{")
    assert guard is not None, _broken(
        "the project-config block is no longer guarded by OPENCODE_DISABLE_PROJECT_CONFIG",
        file=CONFIG,
        expected="`if (!Flag.OPENCODE_DISABLE_PROJECT_CONFIG) {` wrapping the "
        '`ConfigPaths.files("opencode", ...)` walk',
        was="config.ts:420-424",
        guarantee="Without this guard, opencode merges `opencode.json` files found by "
        "walking up from the working directory to the worktree root — files that live "
        "in the user's untrusted repository.",
        spec="SPEC §17.1",
        action="Establish how project config is suppressed now. If it cannot be, the "
        "sandbox must stop opening user repositories until §11.2 P3 is re-planned.",
    )

    block = _balanced_block(text, guard.end() - 1)
    assert block is not None and re.search(r'ConfigPaths\.files\(\s*"opencode"', block), _broken(
        "the project opencode.json walk has moved out of the OPENCODE_DISABLE_PROJECT_CONFIG guard",
        file=CONFIG,
        expected='`ConfigPaths.files("opencode", ctx.directory, ctx.worktree)` INSIDE the '
        "`if (!Flag.OPENCODE_DISABLE_PROJECT_CONFIG)` block",
        was="config.ts:421, inside the guard at config.ts:420",
        guarantee="The guard only protects what it encloses. A walk that moved outside it "
        "reads the untrusted repository's opencode.json unconditionally, whatever "
        "Halyard sets.",
        spec="SPEC §17.1",
        action="Read the block the guard now encloses and the code that was moved out of "
        "it. Anything repository-sourced that escaped the guard is a sandbox escape.",
        found=f"guard at line {_line_of(text, guard.start())} encloses "
        f"{len((block or '').splitlines())} lines, none matching the walk",
    )


def _directories_array(text: str) -> tuple[int, int] | None:
    """Span of the `unique([...])` array `ConfigPaths.directories` returns.

    Every structural claim about that function is made inside this span, because
    a directory is in opencode's config search path exactly by being an element
    of this array, and a ternary can only suppress what it encloses.
    """
    call = _last(text, r"return\s+unique\(\s*\[")
    if call is None:
        return None
    return _balanced(text, text.index("[", call.start()))


def _guard_ternaries(text: str, array: tuple[int, int]) -> list[tuple[int, int]]:
    """Spans of EVERY `...(!Flag.OPENCODE_DISABLE_PROJECT_CONFIG ? ... : [])` spread.

    Bracket-matched rather than pattern-matched, and all of them rather than the
    first. `ConfigPaths.directories` already contains a second `? x : []`
    ternary for `OPENCODE_CONFIG_DIR`, so "there is a `: []` somewhere in
    between" is satisfied by the wrong ternary the moment the array is
    reordered — which is how the earlier version of the home-scan proof below
    could have passed with the home scan inside the guard. Taking only the first
    guard has the same weakness one step along: an element suppressed by a
    second copy of the same flag test would go unseen.
    """
    start, end = array
    spans = []
    for match in re.finditer(r"\.\.\.\(\s*!Flag\.OPENCODE_DISABLE_PROJECT_CONFIG", text[start:end]):
        span = _balanced(text, text.index("(", start + match.start()))
        if span is not None:
            spans.append(span)
    return spans


def _inside(spans: list[tuple[int, int]], position: int) -> bool:
    return any(begin < position < finish for begin, finish in spans)


def _spans_for_humans(text: str, spans: list[tuple[int, int]]) -> str:
    if not spans:
        return "no OPENCODE_DISABLE_PROJECT_CONFIG ternary found in the array"
    return "guard ternaries at lines " + ", ".join(
        f"{_line_of(text, begin)}-{_line_of(text, finish)}" for begin, finish in spans
    )


def test_the_global_config_directory_is_scanned_unconditionally(
    opencode_src: Path,
) -> None:
    """§17: `~/.config/opencode/plugin/` is where the supervisor plugin loads from.

    §17's second enforcement layer is that plugin's `tool.execute.before` hook,
    and a plugin that never loads is indistinguishable from one that chose not
    to act: no error, no warning, a healthy-looking sandbox with one of its two
    enforcement layers simply absent. So the install path is pinned the same way
    the merge order is.

    `Global.Path.config` is the first element of the array
    `ConfigPaths.directories` returns and sits outside every
    `OPENCODE_DISABLE_PROJECT_CONFIG` ternary in it, which is what makes it
    survive `OPENCODE_DISABLE_PROJECT_CONFIG=1`. Both halves are asserted: the
    element is there and unconditional, and it is outside every one of those
    ternaries' bracket-matched spans.
    """
    text = _read(opencode_src, PATHS)

    array = _directories_array(text)
    assert array is not None, _broken(
        "ConfigPaths.directories no longer returns a `unique([...])` array",
        file=PATHS,
        expected="`return unique([ ... ])` listing the config search path",
        was="paths.ts:24-40",
        guarantee="Three pins in this file prove their claims by bracket-matching that "
        "array: what is in the search path, and what the OPENCODE_DISABLE_PROJECT_CONFIG "
        "ternary encloses. Without it they cannot make a structural claim at all.",
        spec="SPEC §17.1, §17.3",
        action="Read ConfigPaths.directories and re-anchor `_directories_array` on its new "
        "shape before trusting any other paths.ts pin in this file.",
    )
    start, end = array
    body = text[start:end]

    element = re.search(r"[\[,]\s*Global\.Path\.config\s*,", body)
    assert element is not None, _broken(
        "`Global.Path.config` is no longer an unconditional element of the config search path",
        file=PATHS,
        expected="`Global.Path.config` as a bare element of the `unique([...])` array - "
        "not spread out of a ternary, not behind a flag",
        was="paths.ts:25, the first element of the array",
        guarantee="Halyard installs its supervisor plugin into "
        "`~/.config/opencode/plugin/` in the sandbox image, precisely because this "
        "element is unconditional and therefore survives "
        "OPENCODE_DISABLE_PROJECT_CONFIG=1. That plugin's `tool.execute.before` hook is "
        "§17's second enforcement layer, the one that holds even if the permission config "
        "is somehow relaxed. If this directory stops being scanned - or starts being "
        "scanned only under some condition - the plugin silently stops loading and the "
        "layer is gone with nothing to observe.",
        spec="SPEC §17.3",
        action="Find where global plugins are discovered now, move the sandbox image's "
        "install location to match, and re-verify that the untrusted repository cannot "
        "write to the new location. Note the Halyard-side invariant this pin cannot "
        "assert: `Global.Path.config` is `path.join(xdgConfig, 'opencode')`, and "
        "`xdgConfig` honours $XDG_CONFIG_HOME - so the sandbox image must either set "
        "XDG_CONFIG_HOME deterministically or install to a literal "
        "`~/.config/opencode/plugin`, or the scanned directory and the installed "
        "directory are not the same directory.",
    )

    guards = _guard_ternaries(text, array)
    assert not _inside(guards, start + element.start()), _broken(
        "`Global.Path.config` has moved INSIDE the OPENCODE_DISABLE_PROJECT_CONFIG guard",
        file=PATHS,
        expected="the `Global.Path.config` element to sit OUTSIDE every "
        "`...(!Flag.OPENCODE_DISABLE_PROJECT_CONFIG ? ... : [])` spread in the array",
        was="element at paths.ts:25, guard ternary opening at paths.ts:26",
        guarantee="Halyard sets OPENCODE_DISABLE_PROJECT_CONFIG=1 in every sandbox. A "
        "global config directory suppressed by that same flag means the supervisor plugin "
        "loads on a developer's laptop and not in the sandbox - the one environment where "
        "§17 is the whole point. This is the failure mode a test suite cannot see: "
        "everything passes, the sandbox boots, and `tool.execute.before` is never called.",
        spec="SPEC §17.3",
        action="Do not land the bump. Either keep the global directory ungated upstream, "
        "or install the supervisor plugin through a mechanism the flag does not reach - "
        "OPENCODE_CONFIG_DIR is appended unconditionally today - and re-verify the "
        "repository cannot write to wherever that lands.",
        found=f"element at line {_line_of(text, start + element.start())}; "
        + _spans_for_humans(text, guards),
    )

    globals_text = _read(opencode_src, GLOBAL_PATHS)
    definition = _last(
        globals_text,
        r"const config\s*=\s*path\.join\(\s*xdgConfig!\s*,\s*app\s*\)",
    )
    assert definition is not None, _broken(
        "`Global.Path.config` is no longer `$XDG_CONFIG_HOME/opencode`",
        file=GLOBAL_PATHS,
        expected='`const config = path.join(xdgConfig!, app)` with `const app = "opencode"`',
        was="global.ts:13",
        guarantee="The element pinned above only names a directory; this is the line that "
        "says WHICH directory, and the sandbox image hard-codes the answer when it "
        "installs the supervisor plugin. If the global config root moves - to "
        "$XDG_DATA_HOME, to `~/.opencode`, anywhere - the image installs into a directory "
        "nothing scans, and §17's second enforcement layer is absent in exactly the "
        "quiet way this file exists to prevent.",
        spec="SPEC §17.3",
        action="Read the new definition, move the sandbox image's install path to match, "
        "and update docs/verified.md 'SPEC §22 item 6' where it records "
        "`~/.config/opencode/plugin/` as the install location.",
    )


def test_the_opencode_scan_targets_are_still_exactly_one_directory_name(
    opencode_src: Path,
) -> None:
    """§17: what `afs.up` looks for, asserted once, with its own diagnosis.

    The two scan pins below match their `targets` list loosely, on purpose. A
    benign widening to `targets: [".opencode", ".config/opencode"]` would
    otherwise make both of them fail with a confidently wrong message - "the
    scan is no longer gated on the flag", when it is gated and merely looks at
    one more name. The widening is still a change Halyard must review, so it is
    asserted here instead, where the message can say what it actually means.
    """
    text = _read(opencode_src, PATHS)

    array = _directories_array(text)
    assert array is not None, "covered by the pin above; this one needs its span"
    start, end = array

    lists = [
        [name.strip().strip('"').strip("'") for name in raw.split(",") if name.strip()]
        for raw in re.findall(r"targets:\s*\[([^\]]*)\]", text[start:end])
    ]
    assert lists and all(names == [".opencode"] for names in lists), _broken(
        "the directories scanned into the config search path have changed name",
        file=PATHS,
        expected="every `afs.up({ targets: [...] })` inside the array to look for exactly "
        '`[".opencode"]`',
        was='two scans, both `targets: [".opencode"]`, at paths.ts:29 and paths.ts:36',
        guarantee="Each name in that list is a directory whose `opencode.json`, whose "
        "`{agent,agents}/**/*.md` frontmatter permissions and whose "
        "`{plugin,plugins}/*.{ts,js}` - imported and executed at startup, outside the "
        "tool-permission system - enter opencode's config. Halyard's hostile-repo fixture "
        "(task 1.7/1.8) plants files under `.opencode/` because that is this list. A new "
        "name is a new place a repository can put executable code, and the fixture does "
        "not cover it.",
        spec="SPEC §17.1, §17.3, §11.3",
        action="For each new name: if it can appear inside the user's repository, extend "
        "the hostile fixture to plant code there and re-confirm "
        "OPENCODE_DISABLE_PROJECT_CONFIG suppresses it. If it is under the sandbox user's "
        "home, the image must keep it out of the agent's write reach. Then update this "
        "pin and docs/verified.md 'SPEC §22 item 6'.",
        found=f"target lists found: {lists}",
    )
    assert len(lists) == 2, _broken(
        "ConfigPaths.directories no longer runs exactly two `.opencode` scans",
        file=PATHS,
        expected="two `afs.up` scans in the array: the guarded upward walk from the "
        "working directory, and the ungated one rooted at the home directory",
        was="paths.ts:26-38",
        guarantee="The two scans are pinned separately below, one for being gated and one "
        "for being ungated. A third scan is a third root from which repository or "
        "user-writable JavaScript can be loaded, and no pin here covers it; a scan that "
        "vanished means one of those two pins is now asserting something about the other.",
        spec="SPEC §17.1, §17.3",
        action="Read ConfigPaths.directories, work out what each scan's root is and "
        "whether the flag reaches it, and add or retire a pin below to match.",
        found=f"{len(lists)} scans, targets {lists}",
    )


def test_disable_project_config_also_drops_the_upward_opencode_directory_scan(
    opencode_src: Path,
) -> None:
    """§17: this is the guard that actually suppresses repo agents and plugins.

    Note what this pin does *not* rely on. `ConfigAgent.load`,
    `ConfigAgent.loadMode` and `ConfigPlugin.load` are **not** inside the
    `config.ts` guard above — they run in the loop over
    `ConfigPaths.directories(...)`. They are suppressed only because
    `directories()` stops contributing the repository's `.opencode` directories
    when the flag is set. So `paths.ts` carries more of §17's weight than
    `config.ts` does, and that is what is pinned here.

    Two assertions, because they are two different findings and the failure has
    to say which. The scan is located first; only then is it required to sit
    inside a guard ternary, and that containment is a bracket match on the
    ternary rather than a literal `? yield* afs.up({` adjacency. The literal
    version could not survive the true branch being reshaped at all: wrapping
    that branch in an array literal, to bring one more element under the same
    flag, breaks the adjacency while leaving the scan fully gated - and the pin
    then failed with "the scan is no longer gated on
    OPENCODE_DISABLE_PROJECT_CONFIG", which was simply untrue. A pin that
    reports the wrong cause is worse than one that reports none.
    """
    text = _read(opencode_src, PATHS)

    scan = _last(
        text,
        r'afs\.up\(\{\s*targets:\s*\[[^\]]*"\.opencode"[^\]]*\]\s*,\s*'
        r"start:\s*directory\s*,\s*stop:\s*worktree\s*,?\s*\}\)",
    )
    assert scan is not None, _broken(
        "the upward `.opencode` directory scan can no longer be found in ConfigPaths.directories",
        file=PATHS,
        expected="an `afs.up({ targets: ['.opencode'], start: directory, stop: worktree })` "
        "scan - the upward walk from the working directory to the worktree root",
        was="paths.ts:28-32",
        guarantee="This pin proves the repository's `.opencode` directories are suppressed "
        "by locating that scan and then showing it sits inside the "
        "OPENCODE_DISABLE_PROJECT_CONFIG ternary. With the scan unrecognisable it proves "
        "nothing either way, and unproven is what this file exists to prevent. Read what "
        "this failure does and does not say: the pin cannot find the code, which is not "
        "the same as the scan having been ungated.",
        spec="SPEC §17.1, §17.3",
        action="Read ConfigPaths.directories, find what the upward walk became, and "
        "re-point this pin at it. If the walk is genuinely gone, establish what now puts "
        "the repository's `.opencode` directories into the search path - something must, or "
        "repo agents and plugins have stopped loading altogether, which is a behaviour "
        "change to record in docs/verified.md rather than a pin to delete.",
    )

    array = _directories_array(text)
    guards = _guard_ternaries(text, array) if array is not None else []
    assert array is not None and guards, _broken(
        "the OPENCODE_DISABLE_PROJECT_CONFIG ternary can no longer be bracket-matched",
        file=PATHS,
        expected="`unique([ ... ...(!Flag.OPENCODE_DISABLE_PROJECT_CONFIG ? ... : []) ... ])`",
        was="array at paths.ts:24-40, guard ternary at paths.ts:26-33",
        guarantee="Whether the upward scan is inside or outside the guard is the whole "
        "claim of this pin, and it is answered by matching that ternary's brackets. "
        "Without the ternary there is nothing to be inside of, and a reworked function has "
        "to be read rather than pattern-matched.",
        spec="SPEC §17.1, §17.3",
        action="Read ConfigPaths.directories and rewrite this pin, and the two helpers it "
        "uses, against its new shape.",
    )
    assert _inside(guards, scan.start()), _broken(
        "the upward `.opencode` directory scan is no longer gated on "
        "OPENCODE_DISABLE_PROJECT_CONFIG",
        file=PATHS,
        expected="the `afs.up({ targets: ['.opencode'], start: directory, stop: worktree })` "
        "scan to sit anywhere inside a "
        "`...(!Flag.OPENCODE_DISABLE_PROJECT_CONFIG ? ... : [])` spread, however that "
        "branch is shaped",
        was="paths.ts:28-32, inside the guard ternary at paths.ts:26-33",
        guarantee="This scan is what puts the repository's `.opencode` directory into the "
        "config search path. Everything downstream follows from it: the directory's own "
        "opencode.json, `{agent,agents}/**/*.md` whose frontmatter permissions land LAST "
        "in the ruleset, and `{plugin,plugins}/*.{ts,js}` which opencode imports and "
        "runs with a Bun shell handle and an authenticated server SDK, entirely outside "
        "the tool-permission system. Ungated, opening a user's repository executes that "
        "repository's JavaScript.",
        spec="SPEC §17.1, §17.3, §11.3",
        action="Read ConfigPaths.directories and establish what now excludes the "
        "repository's .opencode directories. If nothing does, the sandbox must not open "
        "an untrusted repository until it is fixed; OPENCODE_PURE is not a substitute "
        "because it also disables Halyard's own supervisor plugin.",
        found=f"scan at line {_line_of(text, scan.start())}; " + _spans_for_humans(text, guards),
    )


def test_the_home_rooted_opencode_scan_stays_outside_the_guard(
    opencode_src: Path,
) -> None:
    """§17: a second directory the flag does not reach — not the install path.

    Halyard's supervisor plugin installs under `Global.Path.config`
    (`~/.config/opencode/plugin/`), which is the pin above; this scan is a
    different directory pinned for a different reason. It is a second, separate
    `afs.up` with `start` and `stop` both `Global.Path.home`, placed after the
    guarded ternary has closed, so `$HOME/.opencode` is read whatever
    `OPENCODE_DISABLE_PROJECT_CONFIG` is set to.

    Being ungated makes it a hazard rather than a service. The sandbox agent
    runs as the user whose home this is, and `.opencode/plugin/*.{ts,js}` under
    it is imported and executed at the next opencode start with a Bun shell
    handle and an authenticated server SDK, entirely outside the
    tool-permission system. So §17 needs `$HOME/.opencode` kept out of the
    agent's write reach — and likewise anything that redirects the root:
    `Global.Path.home` is `process.env.OPENCODE_TEST_HOME ?? os.homedir()`, so
    an agent that can set that variable on a nested `opencode` invocation aims
    this ungated scan wherever it likes, the repository included.
    """
    text = _read(opencode_src, PATHS)

    home = _last(
        text,
        r'afs\.up\(\{\s*targets:\s*\[[^\]]*"\.opencode"[^\]]*\]\s*,\s*'
        r"start:\s*Global\.Path\.home\s*,\s*stop:\s*Global\.Path\.home\s*,?\s*\}\)",
    )
    assert home is not None, _broken(
        "the home-rooted `~/.opencode` scan is gone",
        file=PATHS,
        expected="an `afs.up({ targets: ['.opencode'], start: Global.Path.home, "
        "stop: Global.Path.home })` scan",
        was="paths.ts:34-38",
        guarantee="This scan is the one config source that `OPENCODE_DISABLE_PROJECT_CONFIG` "
        "does not reach and that lives somewhere the sandbox agent can write. The sandbox "
        "image hardens `$HOME/.opencode` because of it, and docs/verified.md's §17 reading "
        "counts it as an open edge. Its disappearance is good news - one fewer ungated "
        "source of auto-executed JavaScript - but it is recorded as a live hazard in two "
        "places, so it gets re-derived rather than left to rot.",
        spec="SPEC §17.3",
        action="Re-read ConfigPaths.directories. If the scan is genuinely gone, record "
        "that in docs/verified.md 'SPEC §22 item 6', drop the image hardening that exists "
        "only for it, and delete this pin. Do NOT conclude anything about the supervisor "
        "plugin's loading from this failure: that is `Global.Path.config`, pinned above.",
    )

    # The structural proof is a bracket match on the guard's own ternary, not a
    # search for `: []` between the flag and the scan. `ConfigPaths.directories`
    # already contains a second `? x : []` ternary (OPENCODE_CONFIG_DIR), so the
    # older text-between version was one reorder away from being satisfied by
    # the wrong ternary while the home scan sat inside the guard.
    array = _directories_array(text)
    guards = _guard_ternaries(text, array) if array is not None else []
    assert array is not None and guards, _broken(
        "the OPENCODE_DISABLE_PROJECT_CONFIG ternary can no longer be bracket-matched",
        file=PATHS,
        expected="`unique([ ... ...(!Flag.OPENCODE_DISABLE_PROJECT_CONFIG ? ... : []) ... ])`",
        was="array at paths.ts:24-40, guard ternary at paths.ts:26-33",
        guarantee="Whether the home scan is inside or outside the guard is the whole claim "
        "of this pin, and it is answered by matching the ternary's brackets. Without the "
        "ternary there is nothing to be outside of, and a reworked function must be read "
        "rather than pattern-matched.",
        spec="SPEC §17.3",
        action="Read ConfigPaths.directories and rewrite this pin, and the two helpers it "
        "uses, against its new shape.",
    )
    assert array[0] <= home.start() < array[1] and not _inside(guards, home.start()), _broken(
        "the home-rooted `~/.opencode` scan is no longer an ungated element of the "
        "config search path",
        file=PATHS,
        expected="the home scan to be an element of the `unique([...])` array and OUTSIDE "
        "every `...(!Flag.OPENCODE_DISABLE_PROJECT_CONFIG ? ... : [])` spread in it",
        was="home scan at paths.ts:34-38, after the ternary closes at paths.ts:33",
        guarantee="Two readings, both of which change docs/verified.md. Inside the "
        "ternary, `$HOME/.opencode` is suppressed in every Halyard sandbox and the image "
        "hardening that exists for it is dead weight. Outside the array altogether, this "
        "scan feeds nothing and the pin is matching dead code - which means it would go on "
        "passing after the real scan changed.",
        spec="SPEC §17.3",
        action="Read ConfigPaths.directories, decide which of the two it is, and update "
        "docs/verified.md 'SPEC §22 item 6' and the sandbox image's hardening together. "
        "Neither reading says anything about the supervisor plugin, which loads from "
        "`Global.Path.config`.",
        found=f"array spans lines {_line_of(text, array[0])}-{_line_of(text, array[1])}, "
        + _spans_for_humans(text, guards)
        + f", home scan at line {_line_of(text, home.start())}",
    )


# ------------------------------------------------------- 4. last rule wins


def test_permission_rules_are_evaluated_with_findlast_over_a_flat_concatenation(
    opencode_src: Path,
) -> None:
    """§17: LAST RULE WINS, which is why suppressing project config matters.

    Merge order does not decide a permission outcome — rule order does.
    `Permission.merge` is literally `rulesets.flat()`, and `evaluate` picks with
    `findLast`. So a ruleset appended after Halyard's wins regardless of which
    config file it came from. Every other §17 pin in this file is load-bearing
    *because* of this one.
    """
    text = _read(opencode_src, PERMISSION)

    evaluate = _last(text, r"rulesets\s*\.flat\(\)\s*\.findLast\(")
    assert evaluate is not None, _broken(
        "permission rules are no longer evaluated as findLast over a flat concatenation",
        file=PERMISSION,
        expected="`rulesets.flat().findLast(...)` inside `evaluate`",
        was="permission/index.ts:30-32",
        guarantee="Halyard's threat model is built on last-rule-wins: it is the reason a "
        "repo-defined agent's frontmatter permissions can beat OPENCODE_PERMISSION, and "
        "therefore the reason OPENCODE_DISABLE_PROJECT_CONFIG is mandatory rather than "
        "belt-and-braces. If evaluation became first-match, or precedence-aware, the "
        "whole §17 analysis in docs/verified.md needs redoing — it may be safer, but it "
        "is no longer the thing that was reasoned about.",
        spec="SPEC §17.1",
        action="Re-read permission/index.ts evaluate(), redo the precedence analysis in "
        "docs/verified.md 'SPEC §22 item 6', and only then update this pin.",
    )

    merge = _last(
        text,
        r"export function merge\([^)]*rulesets[^)]*\)[^{]*\{\s*return rulesets\.flat\(\)\s*\}",
    )
    assert merge is not None, _broken(
        "Permission.merge is no longer plain concatenation",
        file=PERMISSION,
        expected="`export function merge(...rulesets) { return rulesets.flat() }`",
        was="permission/index.ts:200-202",
        guarantee="Halyard reasons about permission precedence purely as list order, "
        "because merge appends rather than overrides. A merge that now dedupes, sorts or "
        "resolves conflicts changes which rule wins without changing any rule.",
        spec="SPEC §17.1",
        action="Read the new merge, redo the precedence analysis, and re-verify that a "
        "hostile `.opencode/agent/*.md` still cannot outrank OPENCODE_PERMISSION.",
    )


def test_permission_evaluate_still_defaults_to_ask_when_no_rule_matches(
    opencode_src: Path,
) -> None:
    """§17: the fallthrough must never become an allow.

    docs/verified.md notes that `OPENCODE_PERMISSION` is applied with
    `mergeDeep`, so a repo can introduce permission keys Halyard's policy does
    not mention — which is why the policy needs a catch-all deny. That advice is
    only survivable while the *upstream* fallthrough is `ask` rather than
    `allow`: `ask` with no human attached blocks, `allow` runs.
    """
    text = _read(opencode_src, PERMISSION)
    default = _last(
        text,
        r'\?\?\s*\{\s*action:\s*"ask"\s*,\s*permission\s*,\s*pattern:\s*"\*"\s*,?\s*\}',
    )
    assert default is not None, _broken(
        'the no-matching-rule fallthrough is no longer `action: "ask"`',
        file=PERMISSION,
        expected='`?? { action: "ask", permission, pattern: "*" }` at the end of evaluate',
        was="permission/index.ts:33-37",
        guarantee="OPENCODE_PERMISSION is merged, not replaced, so a permission key "
        "Halyard's policy never enumerated can reach evaluation with no matching rule. "
        "Today that fails closed. If the default became `allow`, every tool opencode "
        "adds in a future release is permitted in the sandbox from the day it ships, "
        "before Halyard has heard of it.",
        spec="SPEC §17.1",
        action="If the default changed, Halyard's policy must enumerate a catch-all deny "
        "for `*` and a test must assert it end to end before the bump lands.",
    )


# ------------------------------------- 5. repo agents outrank the user ruleset


def test_a_repo_defined_agents_permissions_are_appended_after_the_user_ruleset(
    opencode_src: Path,
) -> None:
    """§17: this is the escape OPENCODE_DISABLE_PROJECT_CONFIG exists to close.

    A config-defined agent starts from `merge(defaults, user)` — `user` is where
    OPENCODE_PERMISSION lands — and then its *own* rules are concatenated after.
    With findLast evaluation the agent's rules win. Pinned as an ordering,
    because that ordering is the vulnerability: if it ever reverses, the
    undocumented flag stops being load-bearing for this particular escape and
    Q9 can be revisited.
    """
    text = _read(opencode_src, AGENT)

    base = _last(text, r"permission:\s*Permission\.merge\(\s*defaults\s*,\s*user\s*\)")
    assert base is not None, _broken(
        "a config-defined agent no longer starts from Permission.merge(defaults, user)",
        file=AGENT,
        expected="`permission: Permission.merge(defaults, user)` when an agent entry is created",
        was="agent/agent.ts:277",
        guarantee="`user` is the ruleset OPENCODE_PERMISSION feeds. If an agent no longer "
        "inherits it, Halyard's policy does not apply to that agent at all — which is "
        "worse than the escape this pin was written for.",
        spec="SPEC §17.1",
        action="Read how agent permissions are now seeded and confirm the injected policy "
        "still reaches every agent, including repo-defined ones.",
    )

    own = _last(
        text,
        r"item\.permission\s*=\s*Permission\.merge\(\s*item\.permission\s*,\s*"
        r"Permission\.fromConfig\(\s*value\.permission\s*\?\?\s*\{\}\s*\)\s*\)",
    )
    assert own is not None and own.start() > base.start(), _broken(
        "a config-defined agent's own permissions are no longer concatenated after the "
        "user ruleset",
        file=AGENT,
        expected="`item.permission = Permission.merge(item.permission, "
        "Permission.fromConfig(value.permission ?? {}))` AFTER the merge(defaults, user) "
        "seed",
        was="agent/agent.ts:293, after the seed at :277",
        guarantee="Halyard's §17 analysis says a hostile repo's `.opencode/agent/evil.md` "
        "would win on the pinned tag, and that OPENCODE_DISABLE_PROJECT_CONFIG is the only "
        "thing stopping it. This pin is the evidence for that claim. If the order changed, "
        "the claim in docs/verified.md is now wrong in one direction or the other and must "
        "be rewritten rather than left to rot.",
        spec="SPEC §17.1",
        action="Re-read agent/agent.ts, update docs/verified.md 'SPEC §22 item 6' and Q9, "
        "and re-derive whether both env vars are still required.",
        found=_where(text, own, "agent/agent.ts:293")
        + f"; seed at line {_line_of(text, base.start())}",
    )


def test_repo_agents_are_globbed_from_each_discovered_opencode_directory(
    opencode_src: Path,
) -> None:
    """§17: names the exact repository files that become agent definitions."""
    agents = _read(opencode_src, CONFIG_AGENT)
    glob = _last(agents, r'Glob\.scan\(\s*"\{agent,agents\}/\*\*/\*\.md"')
    assert glob is not None, _broken(
        "repo agents are no longer globbed from `{agent,agents}/**/*.md`",
        file=CONFIG_AGENT,
        expected='`Glob.scan("{agent,agents}/**/*.md", { cwd: dir, ... })` in ConfigAgent.load',
        was="config/agent.ts:13",
        guarantee="Halyard's hostile-repo fixture (task 1.7/1.8) places its agent at "
        "`.opencode/agent/evil.md` because that is what this glob picks up. A wider or "
        "differently-shaped glob means the fixture tests a path upstream no longer reads, "
        "and passes while proving nothing.",
        spec="SPEC §17.1",
        action="Update the hostile fixture and this pin together, so the fixture keeps "
        "exercising a path that is actually loaded.",
    )

    config = _read(opencode_src, CONFIG)
    loop = _last(config, r"for \(const dir of directories\)")
    load = _last(
        config,
        r"result\.agent\s*=\s*mergeDeep\(\s*result\.agent\s*\?\?\s*\{\}\s*,\s*"
        r"yield\*\s*Effect\.promise\(\(\)\s*=>\s*ConfigAgent\.load\(dir\)\)\s*\)",
    )
    directories = _last(
        config,
        r"const directories\s*=\s*yield\*\s*"
        r"ConfigPaths\.directories\(\s*ctx\.directory\s*,\s*ctx\.worktree\s*\)",
    )
    assert (
        loop is not None
        and load is not None
        and directories is not None
        and directories.start() < loop.start() < load.start()
    ), _broken(
        "repo agents are no longer loaded from the ConfigPaths.directories loop",
        file=CONFIG,
        expected="`ConfigAgent.load(dir)` inside `for (const dir of directories)`, where "
        "`directories` comes from `ConfigPaths.directories(ctx.directory, ctx.worktree)`",
        was="directories at config.ts:430, loop at config.ts:438, load at config.ts:474",
        guarantee="This indirection is the whole suppression mechanism. Repo agents are "
        "NOT inside the `if (!Flag.OPENCODE_DISABLE_PROJECT_CONFIG)` block in config.ts — "
        "they are suppressed only because `directories()` stops returning the repository's "
        ".opencode paths when the flag is set. If the load no longer goes through that "
        "list, the flag stops suppressing repo agents even though paths.ts is unchanged.",
        spec="SPEC §17.1",
        action="Trace how agent definitions are discovered now, and find what excludes the "
        "repository's directories on the new path. If nothing does, this is a sandbox "
        "escape: a hostile `.opencode/agent/*.md` outranks the injected policy.",
    )


# ----------------------------------- 6. repo plugins are discovered and executed


def test_repo_plugins_are_auto_discovered_and_executed_from_the_same_loop(
    opencode_src: Path,
) -> None:
    """§11.3, §17: arbitrary code execution from the repository, outside permissions.

    Plugins are not tools, so no permission rule is consulted before one runs;
    plugin code receives `$` (a Bun shell handle) and `client` (the
    authenticated server SDK). Pinned as a *present* danger rather than an
    absent one: the assertion says "this still happens, and it is still gated
    only by the directory list", so that a change to either half is reviewed.
    """
    plugin = _read(opencode_src, CONFIG_PLUGIN)
    glob = _last(plugin, r'Glob\.scan\(\s*"\{plugin,plugins\}/\*\.\{ts,js\}"')
    assert glob is not None, _broken(
        "repo plugins are no longer discovered from `{plugin,plugins}/*.{ts,js}`",
        file=CONFIG_PLUGIN,
        expected='`Glob.scan("{plugin,plugins}/*.{ts,js}", { cwd: dir, ... })` in '
        "ConfigPlugin.load",
        was="config/plugin.ts:21",
        guarantee="SPEC §11.3 treats the repository as untrusted input and, after task "
        "1.1, names `.opencode/plugin/` as the sharpest edge — it is arbitrary code "
        "execution at startup, outside the tool-permission system entirely. Halyard's "
        "hostile fixture plants `.opencode/plugin/evil.ts` because of this glob. If the "
        "pattern widened, the fixture no longer covers what upstream loads.",
        spec="SPEC §17.1, §11.3",
        action="Widen the hostile fixture to match the new pattern, re-confirm "
        "OPENCODE_DISABLE_PROJECT_CONFIG still suppresses all of it, and update this pin. "
        "Then check the direction the fixture cannot show you: this same glob is what loads "
        "Halyard's OWN supervisor plugin from `~/.config/opencode/plugin/`, because "
        "ConfigPlugin.load runs for every directory in the config search path. A pattern "
        "that NARROWED - to `plugins/*.{ts,js}`, say - fires this pin while quietly "
        "stopping that plugin from loading, and takes §17's second enforcement layer with "
        "it. Move the sandbox image's install path in the same change.",
    )

    config = _read(opencode_src, CONFIG)
    loop = _last(config, r"for \(const dir of directories\)")
    load = _last(config, r"ConfigPlugin\.load\(dir\)")
    assert loop is not None and load is not None and loop.start() < load.start(), _broken(
        "repo plugin discovery no longer runs from the ConfigPaths.directories loop",
        file=CONFIG,
        expected="`ConfigPlugin.load(dir)` inside `for (const dir of directories)`",
        was="loop at config.ts:438, load at config.ts:478",
        guarantee="Like repo agents, plugin discovery is suppressed indirectly: it is "
        "outside config.ts's OPENCODE_DISABLE_PROJECT_CONFIG block and only stops "
        "happening because `directories()` omits the repository's .opencode paths. A load "
        "that bypasses that list executes the user's repository JavaScript with the flag "
        "still set.",
        spec="SPEC §17.1, §11.3",
        action="Trace the new discovery path and find what excludes repository "
        "directories. If nothing does, stop: opening a user repo now runs its code, and "
        "OPENCODE_PURE is not a usable substitute because it also disables Halyard's own "
        "supervisor plugin.",
    )


# Tokens that make a statement conditional. `(?<!\?)\?(?![?.])` is a bare `?`,
# so `??` and `?.` are not read as ternaries.
_CONDITIONAL_TOKENS = [
    (r"\bif\s*\(", "an `if (...)` guard"),
    (r"\belse\b", "an `else` branch"),
    (r"\bswitch\s*\(", "a `switch`"),
    (r"(?<!\?)\?(?![?.])", "a `?:` ternary"),
    (r"&&", "an `&&` short circuit"),
    (r"\|\|", "a `||` short circuit"),
]

# Loop control earlier in the body has the same effect as wrapping the load in a
# conditional, without nesting it inside anything.
_LOOP_SKIPS = [(r"\bcontinue\b", "a `continue`"), (r"\bbreak\b", "a `break`")]


def test_the_repo_plugin_load_is_unconditional_within_the_directories_loop(
    opencode_src: Path,
) -> None:
    """§17: being in the search path is necessary; being loaded is sufficient.

    The pin above proves `ConfigPlugin.load(dir)` runs *after* the header of the
    loop over `ConfigPaths.directories(...)`. That is not the same as running
    for every directory in it, and the gap between the two is one of §17's two
    enforcement layers.

    Upstream already tests `dir.endsWith(".opencode")` one statement earlier, to
    decide whether to load that directory's `opencode.json` (config.ts:439,
    which also admits `Flag.OPENCODE_CONFIG_DIR`). Hoisting the plugin load into
    the same branch is the obvious tidy-up, and it is silent:
    `Global.Path.config` is `~/.config/opencode`, whose last nine characters are
    `/opencode` and NOT `.opencode`. The directory stays in the search path, so
    test_the_global_config_directory_is_scanned_unconditionally keeps passing -
    correctly, it is still there - while `{plugin,plugins}/*.{ts,js}` beneath it
    is never scanned again and Halyard's supervisor plugin stops loading with
    the whole suite green.

    So the load is pinned as unconditional: at the top level of the loop body,
    in a statement carrying no branch of its own, with nothing earlier in the
    body that can skip the rest of an iteration.
    """
    text = _read(opencode_src, CONFIG)

    loop = _last(text, r"for \(const dir of directories\)")
    load = _last(text, r"ConfigPlugin\.load\(dir\)")
    assert loop is not None and load is not None, (
        "covered by the pin above; this one needs both of its anchors"
    )

    opener = text.find("{", loop.end())
    body = _balanced(text, opener) if opener != -1 else None
    assert body is not None and body[0] < load.start() < body[1], _broken(
        "the directories loop body can no longer be bracket-matched around the plugin load",
        file=CONFIG,
        expected="`for (const dir of directories) { ... ConfigPlugin.load(dir) ... }` - a "
        "brace-matchable body with the load inside it",
        was="loop body at config.ts:438-480, load at config.ts:478",
        guarantee="The claim below is that the load is unconditional INSIDE that body, and "
        "it is made by bracket-matching the body and then measuring nesting within it. "
        "Without the body there is nothing to be at the top level of, and the reworked loop "
        "has to be read rather than pattern-matched.",
        spec="SPEC §17.3, §11.3",
        action="Read the loop in Config's resolve and re-anchor this pin on its new shape "
        "before trusting it.",
    )

    reasons = [
        f"nested inside the block opening at line {_line_of(text, offset)}"
        for offset in _enclosing_braces(text, body[0] + 1, load.start())
    ]
    statement = text[_statement_start(text, body[0] + 1, load.start()) : load.end()]
    reasons += [
        f"its own statement carries {why}"
        for pattern, why in _CONDITIONAL_TOKENS
        if re.search(pattern, statement)
    ]
    reasons += [
        f"{why} at line {_line_of(text, body[0] + hit.start())} can skip the rest of an "
        "iteration before the load is reached"
        for pattern, why in _LOOP_SKIPS
        for hit in re.finditer(pattern, text[body[0] : load.start()])
    ]
    assert not reasons, _broken(
        "the repo plugin load is no longer unconditional inside the directories loop",
        file=CONFIG,
        expected="`ConfigPlugin.load(dir)` to run for EVERY directory in "
        "`ConfigPaths.directories(...)`: at the top level of the loop body, in a statement "
        "with no branch of its own, with nothing earlier in the body that can skip the "
        "iteration",
        was="config.ts:478, at the top level of the loop opened at config.ts:438",
        guarantee="Halyard installs its supervisor plugin at `~/.config/opencode/plugin/`, "
        "under `Global.Path.config`. That directory is in the config search path "
        "unconditionally - pinned separately, in "
        "test_the_global_config_directory_is_scanned_unconditionally - but being in the "
        "search path is only half of it: the plugin loads because THIS load runs for every "
        "entry in that path. Gate it on anything about `dir` and the supervisor plugin "
        "stops loading, in the quietest way this file exists to prevent. "
        "`Global.Path.config` is `~/.config/opencode`, whose last nine characters are "
        "`/opencode` and not `.opencode`, so the natural-looking "
        "`if (dir.endsWith('.opencode'))` - the very test upstream applies one statement "
        "earlier, at config.ts:439 - excludes exactly the one directory Halyard depends on, "
        "while leaving that directory in the search path so every other pin here stays "
        "green. §17's second enforcement layer is that plugin's `tool.execute.before` hook, "
        "and a plugin that never loads is indistinguishable from one that chose not to act: "
        "no error, no warning, a healthy-looking sandbox with one of its two layers absent.",
        spec="SPEC §17.3, §11.3",
        action="Read the condition reported below and work out whether "
        "`Global.Path.config` satisfies it. If it does not, the supervisor plugin has to be "
        "installed somewhere the new condition admits - and re-verified as unreachable by "
        "the untrusted repository - before the bump lands; update the sandbox image and "
        "docs/verified.md 'SPEC §22 item 6' in the same change. A `continue` or `break` "
        "reported below may belong to a nested loop and be harmless: this pin cannot tell, "
        "so read the line it names.",
        found=("\n             ").join(reasons),
    )


# ------------------------------- 7. permission.ask is declared but never fires


def _dispatched_hook_names(root: Path) -> dict[str, tuple[str, int]]:
    """Hook names passed as the first literal argument to `.trigger(...)`.

    Deliberately matches the *dispatch* form rather than any occurrence of the
    name. `permission.ask` also exists as a method on opencode's internal
    Permission service (`permission.ask({...})` in session/processor.ts), which
    is a different thing entirely — a grep for the bare string would find it and
    conclude, wrongly, that the plugin hook fires.
    """
    found: dict[str, tuple[str, int]] = {}
    for path in sorted((root / DISPATCH_ROOT).rglob("*.ts")):
        text = path.read_text(encoding="utf-8")
        for match in re.finditer(r"\.trigger\(\s*\"([A-Za-z][\w.]*)\"", text):
            found.setdefault(
                match.group(1),
                (str(path.relative_to(root)), _line_of(text, match.start())),
            )
    return found


def test_the_vetoing_permission_ask_plugin_hook_is_still_declared(
    opencode_src: Path,
) -> None:
    """§17: the trap. Declared with a veto signature, dispatched nowhere."""
    text = _read(opencode_src, PLUGIN_TYPES)
    declared = _last(
        text,
        r'"permission\.ask"\?:\s*\(\s*input:\s*Permission\s*,\s*output:\s*\{\s*'
        r'status:\s*"ask"\s*\|\s*"deny"\s*\|\s*"allow"\s*\}\s*\)\s*=>\s*Promise<void>',
    )
    assert declared is not None, _broken(
        "the `permission.ask` plugin hook declaration has changed or gone",
        file=PLUGIN_TYPES,
        expected='`"permission.ask"?: (input: Permission, output: { status: "ask" | "deny" '
        '| "allow" }) => Promise<void>`',
        was="packages/plugin/src/index.ts:261",
        guarantee="An earlier reading of this hook claimed it gave Halyard a second, "
        "independent permission veto. It does not — it has zero dispatch sites — and "
        "docs/verified.md keeps that as a recorded trap rather than deleting it. This pin "
        "exists so the trap is re-examined when the declaration moves: reading the type "
        "definitions alone produces a veto that never fires and a test suite that never "
        "notices.",
        spec="SPEC §17.3",
        action="Check whether the hook was removed (delete this pin and the trap note) or "
        "reshaped (re-read whether it now fires — see the companion pin below).",
    )


def test_permission_ask_still_has_zero_dispatch_sites(opencode_src: Path) -> None:
    """§17: pin the ABSENCE. A newly-dispatched veto is a lever we want told about."""
    dispatched = _dispatched_hook_names(opencode_src)
    site = dispatched.get("permission.ask")
    assert site is None, _broken(
        "`permission.ask` is now DISPATCHED - a real permission veto hook exists",
        file=DISPATCH_ROOT,
        expected=f'no `.trigger("permission.ask", ...)` call site anywhere under {DISPATCH_ROOT}',
        was="zero call sites; the dispatched set was chat.headers, chat.message, "
        "chat.params, command.execute.before, shell.env, tool.definition, "
        "tool.execute.after, tool.execute.before and four experimental.*",
        guarantee="This is a pin on an absence, and the failure is GOOD NEWS rather than a "
        "regression. Halyard enforces §17 through `tool.execute.before`, which denies by "
        "throwing and is coupled to tool execution. A dispatched `permission.ask` would "
        "be a second, independent enforcement point that intercepts the permission "
        "decision itself — worth adopting deliberately rather than discovering by "
        "accident two releases later.",
        spec="SPEC §17.3",
        action="Read the new call site, decide whether Halyard's supervisor plugin should "
        "implement it, update docs/verified.md 'SPEC §22 item 6' (the recorded trap is now "
        "historical), then delete this pin.",
        found=f"dispatched at {site[0]}:{site[1]}" if site else "",
    )


def test_tool_execute_before_is_still_dispatched(opencode_src: Path) -> None:
    """§17: the actual enforcement point. It must exist."""
    dispatched = _dispatched_hook_names(opencode_src)
    assert "tool.execute.before" in dispatched, _broken(
        "`tool.execute.before` is no longer dispatched",
        file=DISPATCH_ROOT,
        expected='at least one `.trigger("tool.execute.before", ...)` call site under '
        f"{DISPATCH_ROOT}",
        was="dispatched from session/tools.ts and tool/code-mode.ts",
        guarantee="This hook is Halyard's enforcement point: the supervisor plugin denies "
        "a tool call by throwing from it, and unlike config precedence it does not depend "
        "on merge order at all. It is the second layer under §17, the one that holds even "
        "if the permission config is somehow relaxed. Without it, §17 rests on "
        "OPENCODE_PERMISSION alone.",
        spec="SPEC §17.3",
        action="Find the replacement interception point, port the supervisor plugin to it, "
        "and do not run untrusted repositories in the sandbox until it is ported.",
        found=f"hooks currently dispatched: {', '.join(sorted(dispatched))}",
    )


# ------------------------------------------------- 8. the metering arithmetic


def test_token_accounting_is_still_net_of_cache_reads_and_writes(
    opencode_src: Path,
) -> None:
    """§19: `tokens.input` is not the provider's input count, and never was.

    Halyard's ledger computes billable input as `input + cache.read +
    cache.write` and billable output as `output + reasoning`, because upstream
    subtracts both cache classes out of `input` and reasoning out of `output`.
    Get this wrong and nothing breaks — the invoices are just quietly wrong, and
    §16.5's nightly reconciliation surfaces it as unexplained drift much later.
    """
    text = _read(opencode_src, SESSION)

    arithmetic = _last(
        text,
        r"const adjustedInputTokens\s*=\s*safe\(\s*inputTokens\s*-\s*"
        r"cacheReadInputTokens\s*-\s*cacheWriteInputTokens\s*\)",
    )
    assert arithmetic is not None, _broken(
        "the cache-adjusted input token arithmetic has changed",
        file=SESSION,
        expected="`const adjustedInputTokens = safe(inputTokens - cacheReadInputTokens - "
        "cacheWriteInputTokens)`",
        was="session/session.ts:364",
        guarantee="Halyard's ledger adds cache.read and cache.write back onto input to "
        "recover the provider's billable input count. That correction is only right while "
        "upstream subtracts exactly those two terms. If upstream stops subtracting them, "
        "Halyard DOUBLE-COUNTS the cache volume - which on a cached agent loop is the "
        "majority of input tokens - and over-bills every turn.",
        spec="SPEC §19.2, §16.5",
        action="Re-derive billable input from the new arithmetic, update the aigw "
        "accounting tests, and treat any already-written ledger_entries as needing a "
        "§19.2 review before the change ships.",
    )

    fields = [
        (
            "input",
            r"input:\s*adjustedInputTokens\s*,",
            "input: adjustedInputTokens",
            "`input` is the NON-cached input count. Halyard adds cache.read and "
            "cache.write back on. A field that is now the gross count means Halyard "
            "bills the cache twice.",
        ),
        (
            "output",
            r"output:\s*safe\(\s*outputTokens\s*-\s*reasoningTokens\s*\)\s*,",
            "output: safe(outputTokens - reasoningTokens)",
            "`output` already excludes reasoning tokens. Halyard adds `reasoning` back "
            "on for billable output; if reasoning stops being subtracted here, that "
            "addition double-counts it.",
        ),
        (
            "reasoning",
            r"reasoning:\s*reasoningTokens\s*,",
            "reasoning: reasoningTokens",
            "the term Halyard adds back onto `output`. If it disappears, reasoning "
            "tokens are billed as zero and every thinking-heavy turn under-bills.",
        ),
        (
            "cache",
            r"cache:\s*\{\s*write:\s*cacheWriteInputTokens\s*,\s*"
            r"read:\s*cacheReadInputTokens\s*,?\s*\}",
            "cache: { write: cacheWriteInputTokens, read: cacheReadInputTokens }",
            "the two terms Halyard adds back onto `input`, normalised by opencode across "
            "providers that only report cache writes in metadata (Anthropic, Vertex, "
            "Bedrock). A rename here silently drops that volume from the ledger.",
        ),
    ]
    for name, pattern, shape, why in fields:
        match = _last(text, pattern)
        assert match is not None, _broken(
            f"the emitted `tokens.{name}` field has changed shape",
            file=SESSION,
            expected=f"`{shape}` in the emitted tokens object",
            was="session/session.ts:367-375",
            guarantee=why,
            spec="SPEC §19.2",
            action="Read the new tokens object, re-derive billable input and output, and "
            "update both Halyard's ledger arithmetic and its accounting tests in the same "
            "change. Do not adjust this pin without adjusting the arithmetic.",
            found=f"`tokens.{name}` not matched; the object is around line "
            + str(_line_of(text, arithmetic.start())),
        )


# ------------------------------------------------- 9. the abort/interrupt surface


def test_the_abort_and_interrupt_endpoints_are_still_in_the_openapi_contract(
    opencode_src: Path,
) -> None:
    """§11.2 P2: Halyard must be able to stop a running turn.

    Asserted against the committed OpenAPI document rather than the router,
    because that document is the contract `agentd` generates its client from.
    """
    document = json.loads(_read(opencode_src, OPENAPI))
    paths = document.get("paths", {})

    for route, operation, purpose in [
        (
            "/session/{sessionID}/abort",
            "session.abort",
            "the legacy abort route",
        ),
        (
            "/api/session/{sessionID}/interrupt",
            "v2.session.interrupt",
            "the v2 interrupt route",
        ),
    ]:
        entry = paths.get(route)
        assert entry is not None, _broken(
            f"{purpose} `POST {route}` is gone from the OpenAPI contract",
            file=OPENAPI,
            expected=f"a `{route}` entry under `paths`",
            was=f"present at {PINNED_TAG} with operationId `{operation}`",
            guarantee="SPEC §11.2's P2 is 'must interrupt a running turn', and task 1.1 "
            "struck that patch precisely because these two routes exist. A turn that "
            "cannot be stopped is a turn that keeps spending credits after the user has "
            "asked it to stop, and §19's spend cap has no way to act.",
            spec="SPEC §19.3, §11.2",
            action="Find the replacement route, regenerate the agentd client from "
            "packages/sdk/openapi.json, and re-check whether P2 can still be 'no patch'. "
            f"The contract currently exposes {len(paths)} paths; grep it for 'interrupt' "
            "and 'abort'.",
        )
        assert entry.get("post", {}).get("operationId") == operation, _broken(
            f"{purpose} no longer has operationId `{operation}`",
            file=OPENAPI,
            expected=f"`paths['{route}'].post.operationId == '{operation}'`",
            was=f"`{operation}` at {PINNED_TAG}",
            guarantee="agentd generates its client from this document, so the operationId "
            "is the method name Halyard calls. A renamed operation is a compile or runtime "
            "break in agentd's turn-cancellation path — the path §19's spend cap depends "
            "on.",
            spec="SPEC §19.3, §11.2",
            action="Regenerate the client and update agentd's cancellation call site.",
            found=f"found `{entry.get('post', {}).get('operationId')}` and methods {sorted(entry)}",
        )


# ------------------------ 10. hooks are awaited, not fired and forgotten


def test_plugin_hooks_are_awaited_so_a_throwing_hook_denies(opencode_src: Path) -> None:
    """§17: `Plugin.trigger` awaits each hook, which is what makes it a veto.

    Added after review. `test_tool_execute_before_is_still_dispatched` proves
    that hook is *dispatched*, and dispatched is not awaited. Halyard's supervisor plugin
    denies a tool call by throwing from that hook, and the throw only reaches
    the caller because `trigger` runs every hook as
    `yield* Effect.promise(async () => fn(input, output))` - awaited in
    sequence, with a rejection surfacing as a defect rather than being
    swallowed. Fork that, or wrap it in `Effect.ignore` or `Effect.catch`, and
    every denial becomes a log line while the tool executes anyway. §17's second
    enforcement layer would be present, dispatched, and inert.

    The same line carries §11.4 and task 2.8. docs/verified.md's P5 reshaping -
    "turn start can block, turn end cannot" - is a fact about this dispatch:
    `chat.message` is the blocking hook `agentd` commits a checkpoint inside,
    and the lease-swap invariant for task 2.8 was derived from it.
    """
    text = _read(opencode_src, PLUGIN_DISPATCH)

    trigger = _last(text, r'Effect\.fn\("Plugin\.trigger"\)')
    assert trigger is not None, _broken(
        "`Plugin.trigger` is gone or renamed",
        file=PLUGIN_DISPATCH,
        expected='`const trigger = Effect.fn("Plugin.trigger")(function* ...)`',
        was="plugin/index.ts:284",
        guarantee="Every plugin hook Halyard implements is invoked through this one "
        "function - the §17 enforcement hook and the §11.4 checkpoint hook alike. How it "
        "invokes them decides whether either can block.",
        spec="SPEC §17.3, §11.4",
        action="Find the new dispatcher and re-point this pin at it before trusting any "
        "hook to be able to deny or delay anything.",
    )
    following = re.search(r'Effect\.fn\("', text[trigger.end() :])
    window = text[trigger.end() : trigger.end() + (following.start() if following else len(text))]

    swallowed = [
        f"line {_line_of(text, trigger.end() + match.start())}: {match.group(0)}"
        for match in re.finditer(
            r"Effect\.(fork\w*|ignore\w*|catch\w*|either|exit|orElse\w*|timeout\w*)",
            window,
        )
    ]
    assert not swallowed, _broken(
        "Plugin.trigger now forks or catches around the hook call",
        file=PLUGIN_DISPATCH,
        expected="no `Effect.fork*`, `Effect.ignore*`, `Effect.catch*`, `Effect.either`, "
        "`Effect.exit`, `Effect.orElse*` or `Effect.timeout*` between the trigger "
        "declaration and the next `Effect.fn`",
        was="plugin/index.ts:284-297 contained only `Effect.promise`",
        guarantee="A throw from `tool.execute.before` is how the supervisor plugin denies "
        "a tool call. Catching it upstream - entirely reasonable as isolation, so one "
        "broken third-party plugin cannot kill a session - converts every §17 denial into "
        "a warning while the tool proceeds. This is the most plausible silent regression "
        "in the whole file, because it would ship as a robustness fix.",
        spec="SPEC §17.3",
        action="Read what is caught or forked and whether a hook's rejection still reaches "
        "the tool call site. If it does not, the supervisor plugin cannot deny anything: "
        "treat §17 as resting on OPENCODE_PERMISSION alone and do not land the bump.",
        found=("\n             ").join(swallowed),
    )
    awaited = _last(
        window,
        r"yield\*\s*Effect\.promise\(\s*async\s*\(\s*\)\s*=>\s*"
        r"fn\(\s*input\s*,\s*output\s*\)\s*\)(?!\s*\.)",
    )
    assert awaited is not None, _broken(
        "plugin hooks are no longer awaited in Plugin.trigger",
        file=PLUGIN_DISPATCH,
        expected="`yield* Effect.promise(async () => fn(input, output))` in the loop over "
        "hooks, unpiped",
        was="plugin/index.ts:294",
        guarantee="Halyard's supervisor plugin enforces §17 by THROWING from "
        "`tool.execute.before`. That denies the call only because trigger awaits the hook "
        "and lets the rejection propagate as a defect. A forked or piped-away dispatch "
        "leaves the plugin loaded, the hook called, the denial logged - and the tool run. "
        "Nothing in Halyard's own test suite can see the difference unless it runs a real "
        "denial end to end.",
        spec="SPEC §17.3, §11.4",
        action="Read the new dispatch. If a hook can no longer block, §17 rests on "
        "OPENCODE_PERMISSION alone and the sandbox must not open untrusted repositories "
        "until the supervisor has another interception point; §11.4's dirty-edit flush and "
        "task 2.8's lease swap both need redesigning at the same time.",
        found=f"Plugin.trigger at line {_line_of(text, trigger.start())}; its body does not "
        "await each hook in the pinned shape",
    )


# ------------------------- 11. the route every provider call has to take


def test_a_configured_baseurl_still_outranks_the_models_own_api_url(
    opencode_src: Path,
) -> None:
    """§19: `aigw` only meters a call that `baseURL` sent to it.

    Added after review. §11.2's P4 - "every token must be metered and capped" -
    is "no patch" solely because `provider.<id>.options.baseURL` exists and
    Halyard points it at `aigw`. The load-bearing half is precedence: a
    configured `baseURL` must beat the model's own `model.api.url`. Inverted,
    calls go straight to the provider - not mis-billed but absent from §19's
    ledger entirely, while the spend cap watches a number that never moves. That
    is a larger hole than the cache arithmetic this file already pins, and it is
    the kind that shows up as an invoice rather than as a failure.

    This pins the precedence, not the coverage. docs/verified.md is explicit
    that "`baseURL` routes every provider call, including any model-listing or
    auth probe made at startup" was read rather than run, and that `chat.headers`
    may matter too; tasks 1.7 and 1.9 own the behavioural half.
    """
    text = _read(opencode_src, PROVIDER)

    precedence = _last(
        text,
        r'typeof options\["baseURL"\]\s*===\s*"string"\s*&&\s*'
        r'options\["baseURL"\]\s*!==\s*""\s*\?\s*options\["baseURL"\]\s*:\s*'
        r"model\.api\.url",
    )
    assert precedence is not None, _broken(
        "a configured `baseURL` may no longer outrank the model's own API URL",
        file=PROVIDER,
        expected='`typeof options["baseURL"] === "string" && options["baseURL"] !== "" '
        '? options["baseURL"] : model.api.url`',
        was="provider/provider.ts:1760-1761",
        guarantee="Halyard sets `provider.<id>.options.baseURL` to `aigw` for every "
        "provider, and that is the entire mechanism by which §19 sees a token at all. "
        "This expression is where the configured value wins over the model's built-in "
        "URL. If the precedence inverts, or the fallback becomes unconditional, provider "
        "calls leave the sandbox for the provider directly: no metering, no spend cap, no "
        "ledger_entries row, and §16.5's reconciliation finds the gap a month later.",
        spec="SPEC §19.2, §19.3, §11.2",
        action="Read how the SDK's baseURL is chosen now. If config can no longer force "
        "it, P4 is not 'no patch' any more (SPEC §11.1) and that is a specification-level "
        "change: egress must pin the gateway instead, or agent/patches/ stops being empty.",
    )

    applied = _last(text, r'options\["baseURL"\]\s*=\s*baseURL')
    assert applied is not None and applied.start() > precedence.start(), _broken(
        "the resolved baseURL is no longer written back into the provider SDK options",
        file=PROVIDER,
        expected='`if (baseURL !== undefined) options["baseURL"] = baseURL` after the '
        "precedence expression",
        was="provider/provider.ts:1781, after the resolution at :1759-1776",
        guarantee="Resolving the right URL and handing it to the SDK are two steps, and "
        "only the second one routes traffic. `options` is what is passed to the provider "
        "package; a resolution that is computed and dropped sends every call to the "
        "provider's default endpoint, past `aigw`, while the config still reads as though "
        "the gateway were in place.",
        spec="SPEC §19.2, §11.2",
        action="Trace where the model's options object is constructed now and confirm the "
        "configured baseURL reaches it. Task 1.9 should assert this against a running "
        "`opencode serve` with a gateway that logs every request.",
        found=_where(text, applied, "provider/provider.ts:1781")
        + f"; precedence expression at line {_line_of(text, precedence.start())}",
    )


# ------------------------------------------------------------ the wiring itself


def test_ci_python_job_checks_out_the_submodule() -> None:
    """The pin that stops every other pin in this file from quietly vanishing.

    Task 0.6 shipped a guard whose skip-or-fail decision keyed on the wrong
    discriminator, and the affected tests silently never ran. The equivalent
    failure here is subtler: nothing in this file would go red. CI would simply
    stop having the submodule, `opencode_src` would bail, and — because a
    developer's machine legitimately may not have it — the bail would look like
    an ordinary skip.

    So the workflow wiring is itself asserted, and deliberately does NOT take
    the `opencode_src` fixture: this is the one test in the file that must run
    even when the submodule is absent, since that is exactly when it matters.
    """
    workflow = CI_WORKFLOW.read_text(encoding="utf-8")

    # Job blocks are two-space keys under `jobs:`; a job's body is everything up
    # to the next such key. Scoped to the text after `jobs:` because `on:` has
    # two-space keys too: unscoped, the assertions below still worked, but the
    # failure message listed `push`, `pull_request` and `workflow_dispatch` as
    # CI jobs, and the message is the part a reader acts on.
    anchor = re.search(r"^jobs:[ \t]*$", workflow, re.MULTILINE)
    assert anchor is not None, (
        "\n.github/workflows/ci.yml has no top-level `jobs:` key, so this pin cannot find "
        "the job that\nruns tests/contracts/test_opencode_upstream_contract.py with the "
        "`agent/opencode` submodule\nchecked out. Read the workflow and re-anchor this "
        "pin on whatever defines the jobs now.\n"
    )
    jobs = {
        match.group(1): match.group(2)
        for match in re.finditer(
            r"^  ([A-Za-z][\w-]*):\n(.*?)(?=^  [A-Za-z][\w-]*:\n|\Z)",
            workflow[anchor.end() :],
            re.MULTILINE | re.DOTALL,
        )
    }
    assert "python" in jobs, (
        "\n.github/workflows/ci.yml no longer defines a `python` job.\n"
        "That job is the only place tests/contracts/test_opencode_upstream_contract.py "
        "runs with\nthe `agent/opencode` submodule checked out. Whichever job runs "
        "`make test-py` now must\ncarry `submodules: true` on its actions/checkout step, "
        "and this pin must name it.\n"
        f"Jobs found: {', '.join(sorted(jobs))}\n"
    )

    body = jobs["python"]
    # The ref is matched loosely. ci.yml pins third-party actions to commit
    # SHAs and says why in a comment, but nothing enforces it - so requiring
    # `@<40 hex>` here would fail this pin, whose whole job is to be trusted,
    # with a diagnosis about submodules when the real change was a switch to
    # `actions/checkout@v8`. Whether the action is SHA-pinned is a different
    # assertion belonging to a different test.
    checkout = re.search(
        r"- uses: actions/checkout@\S+[^\n]*\n(.*?)(?=\n      - |\Z)", body, re.DOTALL
    )
    step = checkout.group(0) if checkout else body
    assert checkout is not None and re.search(
        r"^\s+submodules: true$", checkout.group(1), re.MULTILINE
    ), (
        "\nCI's `python` job checks out the repository WITHOUT submodules, so every "
        "upstream pin in\n"
        "tests/contracts/test_opencode_upstream_contract.py asserts nothing in CI.\n\n"
        "  Halyard depends on unpatched `opencode` source behaviour for SPEC §17's sandbox\n"
        "  boundary (OPENCODE_PERMISSION and the undocumented "
        "OPENCODE_DISABLE_PROJECT_CONFIG)\n"
        "  and for SPEC §19's token accounting. Without the submodule those pins skip, and "
        "an\n"
        "  upstream rename opens the sandbox or mis-bills silently instead of failing CI -\n"
        "  which is the exact outcome docs/open-questions.md Q9 resolved to prevent.\n\n"
        "  Fix: restore `with: { submodules: true, fetch-depth: 1 }` on the `python` job's\n"
        "  actions/checkout step in .github/workflows/ci.yml. No other job needs it.\n\n"
        f"  The job's checkout step currently reads:\n{step[:400]}\n"
    )


def test_submodule_matches_the_ref_these_pins_were_verified_against(
    opencode_src: Path,
) -> None:
    """A deliberate tripwire on the submodule ref, not an accident.

    docs/verified.md says it outright: everything in 'SPEC §22 item 6' is a fact
    about commit 3104c1428e, and "a later tag must be re-checked". The shape
    pins above catch a rename or a move. They cannot catch a behaviour change
    that leaves the source text intact — and the §22 item 6 reading is explicit
    that the source was *read, not run*, so a five-step precedence inference is
    riding on it.

    This test therefore fails on any bump, by design. That is one line to update
    after a re-read, not a reason to weaken the pin.
    """
    probe = subprocess.run(
        ["git", "-C", str(opencode_src), "rev-parse", "HEAD"],
        capture_output=True,
        text=True,
        check=False,
    )
    if probe.returncode != 0:
        _bail(
            "cannot resolve the `agent/opencode` submodule HEAD, so the ref these pins "
            "were verified against is unknown.\n\n"
            f"  git -C {opencode_src} rev-parse HEAD exited "
            f"{probe.returncode}: {probe.stderr.strip()}\n\n"
            "  In CI: the `python` job checks the submodule out with actions/checkout, "
            "which leaves a\n"
            "  real git directory behind, so this failing means the checkout changed shape."
        )

    head = probe.stdout.strip()
    assert head == PINNED_COMMIT, (
        f"\nThe `agent/opencode` submodule has moved off {PINNED_TAG} "
        f"({PINNED_COMMIT[:10]}).\n\n"
        f"  expected  {PINNED_COMMIT}  ({PINNED_TAG})\n"
        f"  found     {head}\n\n"
        "  This failure is intentional and is not a false positive. Every fact in\n"
        "  docs/verified.md 'SPEC §22 item 6' is a fact about the old commit, and that\n"
        '  section says so: "a later tag must be re-checked". The shape pins in this file\n'
        "  catch a rename or a move; they cannot catch a behaviour change that leaves the\n"
        "  source text identical, and §22 item 6 is explicit that the source was read, not\n"
        "  run - SPEC §17's sandbox boundary rests on a five-step precedence inference "
        "whose\n"
        "  chain has never been executed end to end.\n\n"
        "  Before landing the bump:\n"
        "    1. Re-read the mechanisms in docs/verified.md 'SPEC §22 item 6' against the "
        "new\n"
        "       tree, and check opencode's changelog for anything touching config merge\n"
        "       order, the permission ruleset, plugin loading or token accounting.\n"
        "    2. Confirm the six §11.2 patches are still all 'no patch' (SPEC §11.1). If any\n"
        "       upstream mechanism is gone, agent/patches/ is no longer empty and that is a\n"
        "       specification-level change, flagged in the PR body.\n"
        "    3. Update PINNED_TAG and PINNED_COMMIT in this file, and the 'at <tag>' lines\n"
        "       in any pin whose location moved.\n"
    )
