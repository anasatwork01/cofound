"""Measure Modal snapshot and restore latency, because nobody has published it.

**This has never been run against a real Modal account.** No number below the
`cost_model` defaults came from a measurement; that is the whole point of the
file. Until someone runs it, `docs/verified.md` §22 item 5 carries a doc quote
("optimized for performance", "mounted instantly") where SPEC §17's SLO table
needs a distribution, and SPEC §9's `p50 < 10s` resume target cannot honestly
be committed to. This harness is what replaces the quote with a row.

What it costs
-------------
A 2 vCPU / 4 GB Sandbox is **$0.238/hour** and Modal bills per second on
`max(request, actual)` wall clock. On the defaults the tool prints **$0.15 for
`-n 5`** (~19 min) and **$0.58 for `-n 20`** (~76 min), which is the run that
resolves p95. Do not quote those figures from here -- quote whatever the tool
printed for the run you are about to pay for, because every number in them
comes from `cost_model`'s assumptions and a flag can change any of them. The
estimate is printed before anything is created, and nothing is created without
`--run` plus `HALYARD_MODAL_BENCH_SPEND=1`.

What it measures, and why each phase is separate
------------------------------------------------
SPEC §17 budgets 10 seconds at p50 for "new project from template". Modal
publishes a ~0.5s *median* container boot and **nothing at all** for taking or
restoring a snapshot. Reporting one end-to-end number would hide which part of
the budget is Modal's and which is ours, so each phase stands alone:

1. `cold_create_to_ready`      -- `Sandbox.create` from a base image, to a
                                  readiness probe passing.
2. `snapshot_filesystem`       -- `sb.snapshot_filesystem()`.
3. `snapshot_directory`        -- `sb.snapshot_directory('/project')`.
4. `restore_by_boot_to_ready`  -- `Sandbox.create(image=<fs snapshot>)`, to
                                  readiness. This is resume-by-boot, end to end.
5. `restore_by_mount`          -- `mount_image('/project', <dir snapshot>)`
                                  into an *already running* pool member.
6. `restore_by_mount_read`     -- a full recursive read of the mounted tree.
7. `restore_by_mount_total`    -- (5) + (6) per iteration, then percentiled.
8. `exec_spawn_overhead`       -- a no-op exec into the same pool member.

(6) exists to test Modal's "mounted instantly" claim rather than repeat it. If
mounted content pages in lazily, (5) is fast and misleading and (7) is the
honest resume number. Percentiles for (7) are computed from per-iteration
totals joined on the iteration number, never by adding (5)'s and (6)'s
percentiles and never by zipping two lists on position -- see
`stats.keyed_total`.

(8) is the calibration for (6), and it exists because (6) is not free of the
instrument. Reading the tree costs an exec RPC, a stdout drain and a wait,
none of which a real resume pays, so (6) and therefore (7) carry a fixed
overhead that is not Modal's. (8) is the same interpreter start and the same
round trip with nothing to read, measured per iteration on the same sandbox,
and the report prints it next to (6) rather than quietly subtracting it --
because at 20 iterations the two distributions overlap and a subtraction can
go negative.

Readiness is defined by `readiness_probe` + `wait_until_ready()`, never by
`Sandbox.create` returning. Measuring the call's return would measure the
scheduler's acknowledgement, which is not what a user waits through.

What it does NOT measure
------------------------
- **Tunnel acquisition.** `sb.tunnels()` is on the real path to a first
  preview, but declaring `encrypted_ports` changes the create call and would
  perturb phase 1. It is a known, separate gap in §17's budget.
- **A real `node_modules`.** The default workload is synthesised (file count
  and size are the two knobs Modal's own Volume docs say matter). A truly
  representative run needs `--project-setup-cmd 'npm ci'`, which needs egress
  opened with `--allow-domain` -- and then the measurement includes npm.
- **Contention.** Iterations are strictly sequential. Concurrent creates would
  also be measuring the 200 req/s workspace bucket, a different question.

Which reading of the platform this is built on
----------------------------------------------
**Region is unpinned by default** (`--region` measures a pinned variant).
`docs/verified.md` §22 item 5 records that pinning a broad region costs 1.15x
and a narrow one 1.75x, and that Modal advises broad regions for availability
and cold start. A benchmark should measure the platform, not the surcharge, so
the default pays neither -- and the 1.15x/1.75x multiplier stays in the
estimator for the runs that do pass `--region`.

Pinning a region and taking a filesystem or directory snapshot are compatible:
the "cannot pin a region" restriction belongs to **memory** snapshots
(`_experimental_enable_snapshot=True`), and `_experimental_create`'s own
docstring lists region placement and filesystem snapshots as both supported.
What applies either way is residency: **snapshots are stored in the United
States regardless of where the workload ran**, unpinned or not.

Running it
----------
    uv sync --all-packages --group bench   # modal==1.5.5 is NOT a default dep
    export MODAL_TOKEN_ID=... MODAL_TOKEN_SECRET=...
    uv run python -m halyard_sandboxd.bench                       # cost only
    HALYARD_MODAL_BENCH_SPEND=1 uv run python -m halyard_sandboxd.bench --run

`--all-packages` is not decoration: a bare `uv sync --group bench` syncs only
the workspace root, which uninstalls `halyard-sandboxd` itself and the next
line then fails with `No module named 'halyard_sandboxd'`.

Why it lives in `services/sandboxd`
-----------------------------------
`scripts/` is shell plus one codegen script: nothing there is imported,
typechecked or tested. This is neither a one-off nor throwaway. It is the first
code in the repo that calls `modal.*`, and `docs/verified.md`'s rule 1 for this
vendor is that every such call goes behind one adapter module owned by the
service that will use it -- which is `sandboxd`. Putting it here means
`services/sandboxd/tests` collects its unit tests under `make verify` with no
change to `testpaths`, and `mypy --strict` already covers
`services/sandboxd/src`. The call sites here are the draft of the ones task 1.3
will ship.
"""

from __future__ import annotations

import argparse
import importlib
import json
import os
import signal
import sys
import tempfile
import threading
import time
from collections.abc import Callable, Mapping, Sequence
from dataclasses import asdict, dataclass, field
from datetime import UTC, datetime
from pathlib import Path
from types import FrameType
from typing import Any, TextIO

from .stats import (
    CostEstimate,
    CostModel,
    PhaseSummary,
    estimate_cost,
    keyed_total,
    render_cost_estimate,
    render_markdown,
    samples_to_resolve,
    summarize,
)

# Pinned exactly. docs/verified.md records that every release from 1.4.0 to
# 1.5.5 changed something Sandbox-affecting, and that 1.5.5's own release note
# promises to remove undocumented APIs in 1.6.0. A benchmark whose client
# floats is not comparable with the run before it.
MODAL_PIN = "1.5.5"

# The opt-in. Deliberately not `CI`, not `--force`, and not a bare `-y`: it
# names the consequence, so nobody grants it by reflex.
SPEND_ENV = "HALYARD_MODAL_BENCH_SPEND"

REPORT_SCHEMA = "halyard.bench.modal-resume-latency/1"

_V1_SANDBOX_ID_ALPHABET = frozenset(
    "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
)
_V1_SANDBOX_ID_SUFFIX_LEN = 22

# `Sandbox.create`'s `timeout` is the sandbox's entire lifetime, and Modal caps
# it at 24 hours with no documented way to extend a running sandbox
# (docs/verified.md §22 item 5).
MODAL_MAX_SANDBOX_TIMEOUT_S = 24 * 3600

# The lifetime asked for is the projected wall clock times this, floored. The
# projection is a model of the very thing nobody has measured, so a run that
# overruns it by half must still outlive its own pool member. A fixed 3600s did
# not: the recommended `-n 20` projects ~4515s, so the pool member -- created
# first and needed until the last iteration -- would have been reaped around
# iteration 16, after which every mount, unmount and read fails, and
# `mount_image`/`unmount_image` resolve the task id WITHOUT
# `raise_if_task_complete` (modal/sandbox.py:1661, :1688) and so enter the
# unbounded 0.5s poll rather than erroring.
SANDBOX_TIMEOUT_HEADROOM = 1.5
SANDBOX_TIMEOUT_FLOOR_S = 1800

# Refuse a fat-fingered flag rather than price it and run it. Modal's own burst
# allowance above a request is 16 cores, so asking for more than 16 physical
# cores is not a measurement of anything a project sandbox will do; 64 GiB and
# 200 iterations are likewise far outside what this harness is for.
MAX_CPU_CORES = 16.0
MAX_MEMORY_MIB = 64 * 1024
MAX_ITERATIONS = 200

# docs/verified.md's rule for this vendor is that every Modal call gets a
# context deadline AND an explicit MaxThrottleWait, because
# `config.get("max_throttle_wait")` is None by default and the Python client
# then sleeps for the server's `retry_after_secs` and retries *without*
# incrementing the attempt counter (modal/_utils/grpc_utils.py:554-572) -- the
# same unbounded wait verified.md flags in the Go client. Left at the default,
# a throttled create's sleep would be recorded as Modal cold-start latency.
DEFAULT_MAX_THROTTLE_WAIT_S = 120

# Bounds for the calls that are cleanup rather than measurement. They exist so
# that a hung teardown cannot strand the run; they are not measured.
TERMINATE_TIMEOUT_S = 120
UNMOUNT_TIMEOUT_S = 300

# Every create carries a `name=` and `tags=` derived from the run id, because
# the sandbox id is not the only handle that can be lost: a create whose
# deadline fires never returns one at all, and the sandbox may still be
# starting server-side and billing for its whole lifetime. The name and the
# tags are chosen -- and recorded in the ledger -- *before* the call, so the
# report can name what may be running even when no id ever arrives.
NAME_PREFIX = "halyard-bench"
TAG_HARNESS = "halyard-bench"
TAG_HARNESS_VALUE = "resume-latency"
TAG_RUN = "halyard-bench-run"
TAG_LABEL = "halyard-bench-label"

# `_iteration`'s three-valued record of what happened to `mount_image`, which
# decides whether the `finally` attempts an unmount and what an unmount failure
# is then allowed to claim. Two values were not enough: a mount that returned
# an error and a mount that timed out need opposite treatment.
_MOUNT_NOT_ATTEMPTED = "not attempted"
_MOUNT_FAILED = "failed"
_MOUNT_MAYBE = "may have landed"
_MOUNT_LANDED = "landed"


class BenchError(RuntimeError):
    """A failure in the harness itself, as distinct from a slow Modal."""


class DeadlineExceededError(BenchError):
    """A Modal call that blew its deadline and whose thread may still be running.

    Its own type, rather than a message a caller has to pattern-match, because
    it carries the opposite information to every other failure: a call that
    *returned* an error did not do the thing, while a call that never returned
    may have done it and may be billing for it. Two callers branch on exactly
    that -- `_iteration` on whether to attempt an unmount, `_create_ready` on
    whether to print a recovery route for a sandbox with no id.
    """


# ------------------------------------------------------------------ configuration


@dataclass(frozen=True, slots=True)
class BenchConfig:
    """Everything a run needs. Every field that costs money is on the CLI."""

    iterations: int = 5

    # Modal's CPU unit is a PHYSICAL core, which Modal's own pricing page
    # labels "2 vCPU equivalent". So SPEC's "2 vCPU and 4 GB" is
    # cpu=1.0 / memory=4096 and NOT cpu=2.0 -- writing 2.0 provisions 4
    # vCPU-equivalent and raises CPU spend ~1.6x on the blended bill
    # (docs/verified.md §22 item 5).
    cpu_cores: float = 1.0
    memory_mib: int = 4096
    # A bare scalar is only a *request*, i.e. a floor, and permits bursting to
    # request + 16 cores -- which is billed, at per-second max(request,actual).
    # A benchmark whose machine can silently grow mid-run is measuring an
    # unknown machine and mispredicting its own invoice, so the tuple (hard
    # cap) form is the default.
    hard_cap_resources: bool = True

    # ALWAYS explicit: `Sandbox.create`'s default `timeout` is 300s, which
    # would kill the pool member partway through any run worth doing. `None`
    # means "derive it from this run's own projected wall clock" -- see
    # `resolved_sandbox_timeout_s`. A fixed number here is how the recommended
    # `-n 20` came to outlive the lifetime it asked for.
    sandbox_timeout_s: int | None = None
    # The `timeout=` passed to every `exec`, and the bound on the deadline
    # around it. Separate from the sandbox lifetime because it is a per-command
    # bound: `npm ci` under `--project-setup-cmd` is the long one.
    exec_timeout_s: int = 1800
    # MODAL_MAX_THROTTLE_WAIT, set explicitly rather than left at None.
    max_throttle_wait_s: int = DEFAULT_MAX_THROTTLE_WAIT_S
    # `snapshot_filesystem`/`snapshot_directory` default to 55s. Changelog
    # 1.4.3 added support for longer "when necessary", which is the only
    # published evidence of how long a snapshot can take -- so the ceiling
    # here is well above it, and the harness measures rather than truncates.
    snapshot_timeout_s: int = 900
    readiness_timeout_s: int = 300

    # Short on purpose. The default is 30 days, and Modal has **no API to list
    # the images you created**, so a snapshot this process creates and forgets
    # is billed storage nobody can enumerate to find. One hour means even a
    # SIGKILL mid-run leaks for an hour, not a month.
    snapshot_ttl_s: int = 3600

    probe_port: int = 8080
    project_path: str = "/project"
    project_files: int = 20_000
    project_file_kib: int = 4
    # Zero-filled files let a compressing or content-addressed snapshot store
    # dedupe the payload away and report a latency no real tree would produce.
    project_fill: str = "random"
    project_setup_cmd: str | None = None

    app_name: str = "halyard-bench-resume-latency"
    # A published named Image (`Image.from_name`) is the representative case,
    # since that is what sandboxd will boot; a registry tag is the default so
    # the harness runs before that image exists.
    image_name: str | None = None
    image_registry_tag: str = "python:3.12-slim"

    # Unpinned. verified.md: pinning a broad region costs 1.15x and a narrow
    # one 1.75x, and Modal advises broad regions for availability and cold
    # start -- so an unpinned default measures the platform rather than a
    # surcharge. `--region` measures the pinned variant, and the multiplier
    # stays in the estimator for when it is set. Filesystem and directory
    # snapshots place no restriction on `region` either way; the "cannot pin a
    # region" limit belongs to memory snapshots. What does apply either way is
    # that snapshots are stored in the United States regardless.
    region: str | None = None

    # Passed on every create as concrete lists, never left None. Python's
    # implementation treats "both None" as OPEN but an empty list as an
    # allowlist with nothing on it (modal/sandbox.py:265-269), so passing one
    # and omitting the other silently kills all non-443 egress. Empty/empty is
    # deny-by-default, which verified.md prefers over `block_network=True`
    # because that also disables i6pn and cannot be narrowed later.
    outbound_cidr_allowlist: tuple[str, ...] = ()
    outbound_domain_allowlist: tuple[str, ...] = ()

    # Explicit, never inherited. v1 and v2 take different snapshot code paths,
    # so the numbers are not transferable, and the default flips in 1.6.0.
    sandbox_v2: bool = True

    cost_model: CostModel = field(default_factory=CostModel)


def create_kwargs(cfg: BenchConfig) -> dict[str, Any]:
    """The safety-relevant half of every `Sandbox.create` call in this harness.

    Split out as pure data so a test can assert the invariants that cost money
    or weaken the sandbox, without a Modal account:

    - an explicit `timeout`, long enough for the whole run (the default 300s
      would kill a long one, and so would any fixed number shorter than the
      run the operator asked for);
    - `idle_timeout=None`, because Modal's definition of activity does not
      include "a process is running" -- only an exec, a stdin write or an open
      tunnel connection -- so any idle_timeout would reap a sandbox mid-
      measurement, and `idle_timeout` has no pre-termination hook;
    - both outbound allowlists present as concrete lists;
    - **no `secrets=` and no `env=` at all.** Modal Secrets are injected as
      ordinary readable environment variables; there is no masking or sealing.
      CLAUDE.md's rule is that the sandbox never holds a credential that can
      spend money or reach another tenant, and Modal will not enforce that,
      so it is enforced here and asserted in the tests.
    """
    resources: dict[str, Any]
    if cfg.hard_cap_resources:
        resources = {
            "cpu": (cfg.cpu_cores, cfg.cpu_cores),
            "memory": (cfg.memory_mib, cfg.memory_mib),
        }
    else:
        resources = {"cpu": cfg.cpu_cores, "memory": cfg.memory_mib}
    return {
        "timeout": resolved_sandbox_timeout_s(cfg),
        "idle_timeout": None,
        "region": cfg.region,
        "outbound_cidr_allowlist": list(cfg.outbound_cidr_allowlist),
        "outbound_domain_allowlist": list(cfg.outbound_domain_allowlist),
        **resources,
    }


def estimate_for(cfg: BenchConfig) -> CostEstimate:
    """This config's own cost and duration projection. Derived in one place only.

    `blockers`, `resolved_sandbox_timeout_s` and `main` all need it, and a run
    whose gate priced a different config than its warning printed would be the
    kind of quiet inconsistency this file exists to avoid.
    """
    return estimate_cost(
        iterations=cfg.iterations,
        cpu_cores=cfg.cpu_cores,
        memory_mib=cfg.memory_mib,
        region=cfg.region,
        model=cfg.cost_model,
    )


def resolved_sandbox_timeout_s(cfg: BenchConfig) -> int:
    """The lifetime every `Sandbox.create` in this run asks for.

    Derived from the run's own projected wall clock rather than fixed, because
    the pool member is created first and is needed by the last iteration: any
    lifetime shorter than the run reaps it mid-run, and the failure then looks
    like a slow Modal rather than a misconfigured harness. Capped at Modal's
    24h ceiling; `blockers` refuses the run outright if even the cap is short.

    An explicit `--sandbox-timeout` wins, so a pinned lifetime is still
    measurable -- it is just no longer the silent default. **The ceiling
    applies to it too.** `_lifetime_blockers` already tells the operator that
    "Modal caps it at 86400s", and a flag that was accepted unclamped made the
    harness the one component willing to ask Modal for a lifetime it has no
    way to grant. `blockers` names the clamp rather than leaving it silent.
    """
    if cfg.sandbox_timeout_s is not None:
        return min(cfg.sandbox_timeout_s, MODAL_MAX_SANDBOX_TIMEOUT_S)
    projected = estimate_for(cfg).wall_clock_seconds
    want = max(int(projected * SANDBOX_TIMEOUT_HEADROOM), SANDBOX_TIMEOUT_FLOOR_S)
    return min(want, MODAL_MAX_SANDBOX_TIMEOUT_S)


def lifetime_basis(cfg: BenchConfig) -> str:
    """Which rule decided `resolved_sandbox_timeout_s`, for the cost block to caption.

    Four rules can decide it and the caption used to name one of them
    unconditionally -- "derived from the wall clock above" -- which is wrong
    for every run the floor decides, including the documented `-n 5`. A
    caption that explains a number the reader can see is worth having; one
    that explains it wrongly is worse than none.
    """
    if cfg.sandbox_timeout_s is not None:
        if cfg.sandbox_timeout_s > MODAL_MAX_SANDBOX_TIMEOUT_S:
            return (
                f"--sandbox-timeout {cfg.sandbox_timeout_s}s, clamped to Modal's "
                f"{MODAL_MAX_SANDBOX_TIMEOUT_S}s ceiling"
            )
        return "--sandbox-timeout, as given"
    projected = estimate_for(cfg).wall_clock_seconds
    want = int(projected * SANDBOX_TIMEOUT_HEADROOM)
    if want >= MODAL_MAX_SANDBOX_TIMEOUT_S:
        return f"Modal's {MODAL_MAX_SANDBOX_TIMEOUT_S}s ceiling, below this run's projection"
    if want <= SANDBOX_TIMEOUT_FLOOR_S:
        return (
            f"the {SANDBOX_TIMEOUT_FLOOR_S}s floor, above {SANDBOX_TIMEOUT_HEADROOM:g}x the "
            f"{projected:.0f}s projected above"
        )
    return f"{SANDBOX_TIMEOUT_HEADROOM:g}x the wall clock projected above"


# ------------------------------------------------------------------------- gates


def modal_credentials_present(
    environ: Mapping[str, str], *, home: Path | None = None
) -> tuple[bool, str]:
    """Can a Modal call possibly succeed? Returns (yes, how it was decided).

    Keyed on the capability, never on `CI`. Task 0.6 taught this repo what the
    other choice costs: a skip-or-fail decision keyed on `CI` instead of on the
    capability it actually needed (`DATABASE_URL`) meant the check never ran
    where it mattered and broke the job where it did not. The fix was to ask
    for the thing, not the environment. Same discriminator here.

    Mirrors `modal.config`'s own resolution order: MODAL_TOKEN_ID plus
    MODAL_TOKEN_SECRET in the environment, else the profile file at
    MODAL_CONFIG_PATH or `~/.modal.toml` (`modal/config.py:120`).
    """
    if environ.get("MODAL_TOKEN_ID") and environ.get("MODAL_TOKEN_SECRET"):
        return True, "MODAL_TOKEN_ID and MODAL_TOKEN_SECRET are set"
    override = environ.get("MODAL_CONFIG_PATH")
    path = Path(override) if override else (home or Path.home()) / ".modal.toml"
    if path.is_file():
        return True, f"profile file {path}"
    return False, (
        "no Modal credentials: set MODAL_TOKEN_ID and MODAL_TOKEN_SECRET, "
        f"or run `modal token new` to write {path}"
    )


def modal_import_error() -> str | None:
    """Import `modal` and report why not, if not.

    Lazy so that a machine with no `modal` installed -- which is every machine
    running `make verify`, because the dependency is in a non-default group --
    gets a clean, explained refusal instead of an ImportError at collection.
    """
    try:
        import modal  # noqa: F401
    except ImportError as exc:
        return (
            f"modal is not importable ({exc}). It is deliberately not in the default "
            f"install; run `uv sync --all-packages --group bench` to get modal=={MODAL_PIN}."
        )
    return None


def blockers(
    cfg: BenchConfig,
    *,
    environ: Mapping[str, str],
    credentials: tuple[bool, str],
    import_error: str | None,
) -> tuple[str, ...]:
    """Every reason this run must not spawn a sandbox. Pure, so it is testable."""
    reasons: list[str] = []
    if import_error:
        reasons.append(import_error)
    ok, why = credentials
    if not ok:
        reasons.append(why)
    if environ.get(SPEND_ENV) != "1":
        reasons.append(
            f"{SPEND_ENV} is not 1. This run creates real Modal Sandboxes and is billed "
            "per second; the opt-in is separate from --run on purpose."
        )
    if cfg.project_setup_cmd and not (cfg.outbound_cidr_allowlist or cfg.outbound_domain_allowlist):
        reasons.append(
            "--project-setup-cmd needs egress, but both outbound allowlists are empty "
            "(deny-by-default). Add --allow-domain/--allow-cidr, and note that whatever "
            "you allow is then part of what the measurement includes."
        )
    if cfg.project_fill not in {"random", "zero"}:
        reasons.append(f"--project-fill must be random or zero, got {cfg.project_fill!r}")
    if cfg.iterations < 1:
        reasons.append("--iterations must be at least 1")
    elif cfg.iterations > MAX_ITERATIONS:
        reasons.append(
            f"--iterations {cfg.iterations} is above the {MAX_ITERATIONS} this harness will "
            "run. p99 needs n>=100 and that is already hours and dollars; beyond that a "
            "typo is the likelier explanation than an intention."
        )
    if not 0 < cfg.cpu_cores <= MAX_CPU_CORES:
        reasons.append(
            f"--cpu must be within 0..{MAX_CPU_CORES:g} PHYSICAL cores, got {cfg.cpu_cores:g}. "
            "The unit is a physical core (2 vCPU equivalent), so this is billed at roughly "
            "twice what the number looks like."
        )
    if not 0 < cfg.memory_mib <= MAX_MEMORY_MIB:
        reasons.append(f"--memory-mib must be within 1..{MAX_MEMORY_MIB} MiB, got {cfg.memory_mib}")
    if cfg.sandbox_timeout_s is not None and cfg.sandbox_timeout_s > MODAL_MAX_SANDBOX_TIMEOUT_S:
        reasons.append(
            f"--sandbox-timeout {cfg.sandbox_timeout_s} is above the "
            f"{MODAL_MAX_SANDBOX_TIMEOUT_S}s Modal caps a sandbox lifetime at, and there is no "
            "documented way to extend a running sandbox. The harness clamps to the ceiling "
            "rather than ask for a lifetime Modal cannot grant, so asking for more is a "
            "misunderstanding worth stopping: ask for at most the ceiling, or split the run."
        )
    reasons.extend(_lifetime_blockers(cfg))
    return tuple(reasons)


def _lifetime_blockers(cfg: BenchConfig) -> list[str]:
    """Refuse a run that is projected to outlive the sandboxes it creates.

    The pool member is created before iteration 1 and is still needed by the
    last one, so `timeout` has to cover the whole projected run. Nothing
    compared the two until a review pointed out that the single recommended
    paid run -- `-n 20`, because `samples_to_resolve(95)` is 20 and
    `render_human` tells the operator to quote a tail only from a run that
    long -- projected ~4515s against a fixed 3600s lifetime.
    """
    if cfg.iterations < 1:
        return []  # already refused, and the projection is meaningless
    lifetime = resolved_sandbox_timeout_s(cfg)
    projected = estimate_for(cfg).wall_clock_seconds
    if projected < lifetime:
        return []
    per_iteration = (projected - cfg.cost_model.create_to_ready_s) / cfg.iterations
    reaped = ""
    if per_iteration > 0:
        at = int((lifetime - cfg.cost_model.create_to_ready_s) // per_iteration) + 1
        reaped = f" -- the pool member would be reaped around iteration {at} of {cfg.iterations}"
    return [
        f"this run is projected to take {projected:.0f}s but every sandbox is created with "
        f"timeout={lifetime}s, which is the sandbox's whole lifetime{reaped}. Raise "
        f"--sandbox-timeout (Modal caps it at {MODAL_MAX_SANDBOX_TIMEOUT_S}s), or lower "
        "--iterations."
    ]


def observed_backend(sandbox_id: str) -> str:
    """`v1`, `v2` or `unknown`, inferred from the sandbox id's shape.

    The client picks the backend for you and does not tell you. `Sandbox.create`
    routes to the V2 path only when `MODAL_SANDBOX_V2` is set **and** no GPU,
    no network file system and no `pty_info` was requested, and **silently
    falls back to V1** otherwise (`modal/sandbox.py:703`). v1 and v2 take
    different snapshot code paths, so a latency taken on the backend you did
    not think you were on is not a number. It has to be recorded, not assumed.

    There is no public accessor. `modal/sandbox.py:208-222` discriminates on
    the id itself, and that rule is reimplemented here rather than imported,
    because a private function is exactly what 1.5.5's release note says will
    be removed in 1.6.0.

    Three-valued, where the client's rule is two-valued. The client
    *deliberately* routes anything that is not positively v1 to v2, so that an
    id minted in a newer format keeps working -- a sensible default for
    routing and a bad one for attribution, because it labels an id of a shape
    neither backend mints today as "v2" with full confidence. There is no
    published positive v2 *sandbox* id shape in 1.5.5 (the only positive v2
    test in the client is on task ids: `ta-` ... `V`,
    `modal/_utils/task_command_router_client.py:169-171`), so an id that is
    not a recognisable sandbox id at all is `unknown` and the report says so.
    """
    prefix, sep, suffix = sandbox_id.partition("-")
    if prefix != "sb" or sep != "-" or not suffix:
        return "unknown"  # not a sandbox id: `Sandbox` has type_prefix "sb"
    if not all(c in _V1_SANDBOX_ID_ALPHABET for c in suffix):
        return "unknown"  # a shape neither backend is known to mint
    return "v1" if len(suffix) == _V1_SANDBOX_ID_SUFFIX_LEN else "v2"


def attribute_backend(sandbox: Any) -> tuple[str, str]:
    """The backend actually in use, and how that was decided.

    Prefers the client's own resolved answer over re-deriving it. `_is_v2` is
    set on the V2 create path (`modal/sandbox.py:1093`) and from the id on
    `from_id` (`:1162`), which makes it the closest thing to an accessor that
    exists -- but it is private, and 1.5.5 promises to remove undocumented
    APIs in 1.6.0, so it is read defensively with the id shape as the
    fallback. Which of the two answered is recorded, and so is a disagreement
    between them, because the reader needs to know which number they hold.
    """
    from_shape = observed_backend(str(getattr(sandbox, "object_id", "")))
    is_v2 = getattr(sandbox, "_is_v2", None)
    if isinstance(is_v2, bool):
        backend = "v2" if is_v2 else "v1"
        how = "modal.Sandbox._is_v2 (private; the client's own resolved answer)"
        if backend != from_shape:
            how += f"; the id shape says {from_shape}"
        return backend, how
    return from_shape, "sandbox id shape (inferred, not a public API)"


# --------------------------------------------------------------- remote programs

# Passed as argv rather than interpolated, so nothing here can be broken by a
# path or a count. `python3` is present because the base image is a python one.

_FILL = """
import os, sys
root, count, kib, mode = sys.argv[1], int(sys.argv[2]), int(sys.argv[3]), sys.argv[4]
zeros = b"\\0" * (kib * 1024)
os.makedirs(root, exist_ok=True)
for i in range(count):
    directory = os.path.join(root, "d%05d" % (i // 256))
    if i % 256 == 0:
        os.makedirs(directory, exist_ok=True)
    body = os.urandom(kib * 1024) if mode == "random" else zeros
    with open(os.path.join(directory, "f%07d" % i), "wb") as handle:
        handle.write(body)
print("ok")
"""

_MEASURE = """
import json, os, sys
root = sys.argv[1]
files = 0
total = 0
for dirpath, _dirs, names in os.walk(root):
    for name in names:
        try:
            total += os.lstat(os.path.join(dirpath, name)).st_size
        except OSError:
            continue
        files += 1
print(json.dumps({"files": files, "bytes": total}))
"""

# Reads every byte, not just stats: a mount that returns instantly and then
# pages content in on first touch would otherwise look free.
_READ_ALL = """
import json, os, sys
root, want = sys.argv[1], int(sys.argv[2])
files = 0
read = 0
for dirpath, _dirs, names in os.walk(root):
    for name in names:
        with open(os.path.join(dirpath, name), "rb") as handle:
            read += len(handle.read())
        files += 1
if want and files != want:
    raise SystemExit("mounted tree has %d files, expected %d" % (files, want))
print(json.dumps({"files": files, "bytes_read": read}))
"""

# The calibration for `restore_by_mount_read`. Deliberately the same shape as
# `_READ_ALL` -- `python3 -c <program>` through `_exec`, so it pays the same
# exec RPC, the same interpreter start, the same stdout drain and the same
# wait -- with no tree to read. What is left is the part of that phase which is
# the instrument rather than Modal.
_NOOP = """
print("ok")
"""


# --------------------------------------------------------------------- the ledger


def new_run_id() -> str:
    """The discriminator in every sandbox `name=` and `tags=` this run creates.

    A name is unique among *running* sandboxes within an App and the App is
    reused across runs, so a fixed name would make this afternoon's run collide
    with this morning's leftovers. The timestamp doubles as the recovery key:
    one tag value finds every sandbox one run asked for.

    Timestamp *and* a random suffix, because the timestamp alone is only
    second-resolution: two `Ledger`s built inside the same second of the same
    process would share a run id, and then share every sandbox name in it. Kept
    readable rather than made a bare uuid so that a human reading a leak line
    can see when the run that leaked it happened.
    """
    stamp = datetime.now(UTC).strftime("%Y%m%dT%H%M%SZ")
    return f"{stamp}-{os.getpid()}-{os.urandom(2).hex()}"


@dataclass(slots=True)
class Ledger:
    """Everything this run created, or asked for, that Modal keeps charging for.

    Snapshots are the dangerous half. Modal has **no API to list the images you
    have created**, so an image id this process learns and then loses is billed
    storage that nobody can enumerate to find again. Hence: ids are recorded
    the instant they exist, cleanup runs on every path including
    KeyboardInterrupt, and anything not reclaimed is printed in full -- a human
    holding the id can still delete it, a human without it cannot.

    That rule is why the report is now printed and written *before* cleanup is
    attempted rather than after it. A `terminate()` that blocked forever used
    to hang the main thread inside cleanup, and the phase table, the JSON and
    the leaked ids -- the one part of the output a human can act on -- never
    reached the terminal at all.

    Sandboxes have a second failure mode that no id can cover: a create whose
    deadline fires never returns a handle. So `intents` records the `name=`
    and `tags=` a create was made with **before** it is called, which leaves a
    recovery route (`Sandbox.from_name`, `Sandbox.list(tags=...)`) even when
    the id never arrives.

    Leaks are keyed by resource rather than appended as free text, so a later
    success can retract an earlier failure's line. A terminate that overran
    its deadline and then completed used to leave the report calling the same
    sandbox both terminated and leaked, with the leak line printed twice.
    """

    sandboxes: list[tuple[str, Any]] = field(default_factory=list)
    images: list[str] = field(default_factory=list)
    terminated: list[str] = field(default_factory=list)
    deleted: list[str] = field(default_factory=list)
    # (resource key, message). The key is what makes a leak retractable.
    leaks: list[tuple[str, str]] = field(default_factory=list)
    intents: list[dict[str, object]] = field(default_factory=list)
    run_id: str = field(default_factory=new_run_id)

    @property
    def leaked(self) -> list[str]:
        """The leak messages, in the order first recorded, identical ones collapsed."""
        return [message for _, message in self.leaks]

    @property
    def leaked_keys(self) -> frozenset[str]:
        """Which resources currently have an unretracted leak against them."""
        return frozenset(key for key, _ in self.leaks)

    def leak(self, key: str, message: str) -> None:
        """Record that `key` may still be billed. Identical reports collapse into one."""
        if (key, message) not in self.leaks:
            self.leaks.append((key, message))

    def resolve(self, key: str) -> None:
        """Retract every leak against `key`: it has since been reclaimed."""
        self.leaks = [entry for entry in self.leaks if entry[0] != key]

    def intend(self, label: str, *, name: str, tags: Mapping[str, str]) -> dict[str, object]:
        """Record a create before it is made; the caller fills in the id afterwards."""
        intent: dict[str, object] = {
            "label": label,
            "name": name,
            "tags": dict(tags),
            "sandbox_id": None,
        }
        self.intents.append(intent)
        return intent

    def reclaimed_sandbox(self, label: str) -> None:
        """This sandbox's meter has stopped: stop counting it as outstanding or leaked."""
        self.terminated.append(label)
        self.sandboxes = [entry for entry in self.sandboxes if entry[0] != label]
        self.resolve(f"sandbox {label}")

    def reclaimed_image(self, image_id: str) -> None:
        self.deleted.append(image_id)
        self.images = [existing for existing in self.images if existing != image_id]
        self.resolve(f"image {image_id}")

    def to_json(self) -> dict[str, object]:
        return {
            "run_id": self.run_id,
            "sandboxes_terminated": list(self.terminated),
            "images_deleted": list(self.deleted),
            "leaked": self.leaked,
            # What was still outstanding when this document was built. The copy
            # written before cleanup lists everything cleanup is about to try;
            # the copy written after lists only what it could not reclaim.
            "sandboxes_outstanding": [label for label, _ in self.sandboxes],
            "images_outstanding": list(self.images),
            # Every create this run asked for, by the name and tags it asked
            # with. The only handle on a create that never returned.
            "sandboxes_requested": [dict(intent) for intent in self.intents],
        }


def sandbox_identity(ledger: Ledger, label: str) -> tuple[str, dict[str, str]]:
    """The `name=` and `tags=` for one create: the handle that survives a hang.

    Modal's rules, read from `modal/_utils/name_utils.py` in 1.5.5: a name is
    at most 64 characters of `[a-zA-Z0-9-_.]` and unique among running
    sandboxes in the App; a tag key or value is at most 63 of
    `[a-zA-Z0-9._-]`. The run id is digits, `T`, `Z` and dashes, and the
    labels are `pool`, `subject-N` and `restore-N`, so the longest name this
    can produce is around 50 characters -- inside both limits without needing
    to be truncated, which matters because a truncated name is not a handle.
    """
    return (
        f"{NAME_PREFIX}-{ledger.run_id}-{label}",
        {TAG_HARNESS: TAG_HARNESS_VALUE, TAG_RUN: ledger.run_id, TAG_LABEL: label},
    )


def _describe(label: str, sandbox: Any) -> str:
    """`sandbox <label> (<id>)`, because a leak line a human cannot act on is noise."""
    return f"sandbox {label} ({getattr(sandbox, 'object_id', None) or 'no id'})"


def image_delete(modal: Any) -> tuple[Callable[[str], Any] | None, str]:
    """Resolve `modal.experimental.image_delete`, importing the submodule first.

    The submodule import is the whole point. `import modal` does **not** bind
    `modal.experimental`: `modal/__init__.py` imports `billing` and `types`
    from the package and nothing else, so after a plain `import modal`,
    `hasattr(modal, "experimental")` is False -- verified against a real 1.5.5
    install. A `getattr(getattr(modal, "experimental", None), ...)` chain
    therefore resolved to None on every run, every image took the "it is gone"
    branch, and the deletion path had never executed once. Every run leaked 2N
    images that Modal offers no API to list, billed for the full ttl, under a
    printed diagnosis blaming an SDK change that had not happened.

    Imported by `modal.__name__` rather than the literal "modal" so that a test
    can hand this a module shaped like the real one -- a package whose
    `__init__` does not expose `experimental` -- and prove the import happens,
    rather than prove that `getattr` works on an object built to satisfy it.

    `image_delete` is `@synchronizer.create_blocking`, so it is callable from
    synchronous code as it stands.
    """
    name = getattr(modal, "__name__", "modal")
    try:
        importlib.import_module(f"{name}.experimental")
    except Exception as exc:  # - any import failure is the same answer
        # Not fatal on its own: a caller may have supplied the attribute
        # directly. Recorded so the leak line says why, not just "gone".
        why = f"importing {name}.experimental failed ({type(exc).__name__}: {exc})"
    else:
        why = f"{name}.experimental imported but exposes no image_delete"
    delete = getattr(getattr(modal, "experimental", None), "image_delete", None)
    if callable(delete):
        return delete, ""
    return None, why


def cleanup(modal: Any, ledger: Ledger, *, out: TextIO) -> None:
    """Terminate then delete, best effort, never raising. Order is not arbitrary.

    Sandboxes first: terminating stops a per-second meter, and a sandbox left
    running costs ~$0.238/hour where a stray snapshot costs storage. Each item
    is guarded on its own so one failure cannot strand the rest.

    Guarded against `BaseException`, not just `Exception`, and for the same
    reason `main` now reports before it calls this: a `terminate()` that blocks
    forever gets a `kill` or a Ctrl-C from whoever is watching, and both arrive
    here as `KeyboardInterrupt`. Caught, the attempt ends and the ids of
    everything not reclaimed still reach the terminal. Uncaught -- which is
    what a per-item `except Exception` meant -- it became a traceback out of
    `main` with the LEAKED block never printed, which is the one part of the
    output a human without the ids cannot reconstruct.
    """
    interrupted = ""
    # Counted here rather than off `ledger.terminated`, which is the whole
    # run's total: `_iteration` terminates each sandbox as it finishes with it,
    # so a line labelled "cleanup:" that printed the run total would claim
    # cleanup had mopped up work the run had already done.
    reclaimed_sandboxes = 0
    reclaimed_images = 0
    try:
        for label, sandbox in list(ledger.sandboxes):
            try:
                sandbox.terminate()
            except Exception as exc:  # cleanup must never raise; record and continue
                ledger.leak(f"sandbox {label}", f"{_describe(label, sandbox)}: {exc}")
            else:
                ledger.reclaimed_sandbox(label)
                reclaimed_sandboxes += 1

        delete, unavailable = image_delete(modal)
        for image_id in list(ledger.images):
            if delete is None:
                # The only deletion API Modal exposes is
                # `modal.experimental.image_delete`; `Image` itself has no delete.
                # If it has moved, the short ttl is the only remaining backstop and
                # the id has to be printed so a human can act on it.
                ledger.leak(f"image {image_id}", f"image {image_id}: {unavailable}")
                continue
            try:
                delete(image_id)
            except Exception as exc:  # cleanup must never raise; record and continue
                ledger.leak(f"image {image_id}", f"image {image_id}: {exc}")
            else:
                ledger.reclaimed_image(image_id)
                reclaimed_images += 1
    except BaseException as exc:  # a Ctrl-C or a SIGTERM *during* cleanup
        interrupted = f"{type(exc).__name__}: {exc}"
        for label, sandbox in ledger.sandboxes:
            if f"sandbox {label}" not in ledger.leaked_keys:
                ledger.leak(
                    f"sandbox {label}",
                    f"{_describe(label, sandbox)}: not terminated -- cleanup was interrupted",
                )
        for image_id in ledger.images:
            if f"image {image_id}" not in ledger.leaked_keys:
                ledger.leak(
                    f"image {image_id}",
                    f"image {image_id}: not deleted -- cleanup was interrupted",
                )

    print(
        f"cleanup: terminated {reclaimed_sandboxes} sandbox(es), "
        f"deleted {reclaimed_images} image(s)",
        file=out,
    )
    if interrupted:
        print(
            f"cleanup did not finish ({interrupted}); what it did not reclaim is below.",
            file=out,
        )
    if ledger.leaked:
        print("", file=out)
        print("LEAKED -- these are billed and Modal cannot list them for you:", file=out)
        for item in ledger.leaked:
            print(f"  {item}", file=out)
        print(
            "Stop a sandbox with modal.Sandbox.from_id(<id>).terminate() and delete an image "
            "with modal.experimental.image_delete(<id>). Left alone, a sandbox runs out its "
            "lifetime and an image expires at the ttl this run requested.",
            file=out,
        )


# ------------------------------------------------------------------- the measuring


@dataclass(slots=True)
class Run:
    """Samples and metadata accumulated as the run proceeds, so a crash keeps them."""

    # Keyed by phase, then by ITERATION NUMBER, never by list position. The
    # end-to-end figure is a per-iteration sum of two phases, and phases do not
    # stay the same length: a single failed read in the middle of a run leaves
    # `restore_by_mount` one sample longer than `restore_by_mount_read`, and a
    # positional join then pairs iteration 3's mount with iteration 4's read,
    # invents a total no iteration produced, and silently drops the last mount.
    # The iteration number is the only thing that makes the join honest, so it
    # is carried all the way from `record` to `stats.keyed_total`.
    samples: dict[str, dict[int, float]] = field(default_factory=dict)
    failures: list[dict[str, object]] = field(default_factory=list)
    meta: dict[str, object] = field(default_factory=dict)
    # One entry per iteration rather than one for the run. With
    # `--project-setup-cmd` the tree is rebuilt each time and need not come out
    # identical, and a snapshot latency is only interpretable next to the size
    # of the thing snapshotted -- so the sizes are kept per iteration instead
    # of the last one overwriting the rest.
    workloads: list[dict[str, object]] = field(default_factory=list)
    iterations_completed: int = 0

    def record(self, phase: str, seconds: float, *, iteration: int) -> None:
        self.samples.setdefault(phase, {})[iteration] = seconds

    def ordered(self, phase: str) -> list[float]:
        """This phase's samples in iteration order, for summarising and for JSON."""
        return [seconds for _, seconds in sorted(self.samples.get(phase, {}).items())]

    def iterations_of(self, phase: str) -> list[int]:
        """Which iterations this phase's samples came from. Reported, not assumed."""
        return sorted(self.samples.get(phase, {}))


class _Deadline:
    """Wall-clock deadlines around blocking Modal calls, one thread per call.

    Needed because a snapshot can only be taken from a *running* sandbox, and
    the Python client's path to one does not fail fast if it is not: both
    snapshot methods resolve a task id first, and `_get_task_id()` polls in a
    `while not self._task_id: ... asyncio.sleep(0.5)` loop
    (`modal/sandbox.py:1976-1992`) with no bound. `mount_image` and
    `unmount_image` do the same, and without `raise_if_task_complete`. Against
    a sandbox that has already finished, that is an infinite benchmark.

    Three properties, each of which a shared `ThreadPoolExecutor(max_workers=1)`
    got wrong, and each of which was reproduced before it was fixed:

    1. **A fresh thread per call.** With one shared worker, a call that timed
       out left the worker busy, so the *next* `submit` sat in the queue and
       its own deadline expired without the call ever starting. After one real
       timeout every later phase burned its whole timeout measuring nothing.
    2. **The timer starts inside the callable.** Started at `submit`, a merely
       slow predecessor's tail is charged to the next phase as queue time --
       a `restore_by_mount` figure inflated by the previous iteration's
       snapshot overrun and reported as mount latency. That is exactly the
       plausible-wrong-number failure this module exists to prevent.
    3. **Daemon threads.** `ThreadPoolExecutor` registers an `atexit` join, so
       `shutdown(wait=False, cancel_futures=True)` followed by `sys.exit(0)`
       still blocked until the stuck worker returned: the process hung forever
       at interpreter exit, *after* printing the report. A daemon thread does
       not hold the interpreter open.

    Honest about the residual: a thread is not cancellable, so a call that
    blows its deadline may still complete server-side. Two shapes of that cost
    money and each has a recovery line at its call site:

    - a timed-out **snapshot** may exist as an image whose id this process
      never learns, which is the second reason the requested ttl is short;
    - a timed-out **create** may leave a whole sandbox running, billing for its
      full lifetime, with no id anywhere -- which is why every create carries a
      `name=` and `tags=` chosen before the call, and why `_create_ready`
      prints how to find it.

    Timed-out calls are recorded in `stuck` so the report can name them, and
    raise `DeadlineExceededError` rather than a bare `BenchError` so a caller can
    tell "did not happen" from "may have happened". The generic message says
    only what is true of every call; anything call-specific comes in as
    `hint`, because a terminate that hung is not a snapshot that hung.
    """

    def __init__(self) -> None:
        self._stuck: list[str] = []
        self._threads = 0

    def __enter__(self) -> _Deadline:
        return self

    def __exit__(self, *_: object) -> None:
        return None

    @property
    def stuck(self) -> tuple[str, ...]:
        """Calls that blew their deadline and whose threads may still be running."""
        return tuple(self._stuck)

    def call(
        self, what: str, seconds: float, fn: Callable[[], Any], *, hint: str = ""
    ) -> tuple[Any, float]:
        outcome: dict[str, Any] = {}

        def run() -> None:
            started = time.perf_counter()  # inside: queue time is never latency
            try:
                outcome["value"] = fn()
            except BaseException as exc:  # - re-raised on the caller's thread
                outcome["error"] = exc
            finally:
                outcome["seconds"] = time.perf_counter() - started

        self._threads += 1
        thread = threading.Thread(target=run, name=f"modal-bench-{self._threads}", daemon=True)
        thread.start()
        thread.join(timeout=seconds)
        if thread.is_alive():
            self._stuck.append(what)
            raise DeadlineExceededError(
                f"{what} did not return within {seconds:.0f}s. The call is still running on a "
                "worker thread and cannot be cancelled, so whatever it was doing may yet "
                "happen" + (f"; {hint}" if hint else "") + ". The thread is a daemon, so it "
                "cannot hold up this process's exit."
            )
        error = outcome.get("error")
        if error is not None:
            raise error
        return outcome.get("value"), float(outcome["seconds"])


def _exec(sandbox: Any, *argv: str, timeout: int) -> str:
    """Run a command in a sandbox and return stdout, raising on a non-zero exit.

    An exec is also one of the three things Modal counts as activity, which is
    why the workload steps double as keep-alives.
    """
    process = sandbox.exec(*argv, timeout=timeout)
    stdout = process.stdout.read()
    stderr = process.stderr.read()
    code = process.wait()
    if code != 0:
        raise BenchError(f"{argv[0]} exited {code}: {stderr.strip() or stdout.strip()}")
    return str(stdout)


def _create_deadline_note(
    cfg: BenchConfig, *, label: str, name: str, tags: Mapping[str, str]
) -> list[str]:
    """What a human needs in order to find a sandbox whose create never returned.

    The one exposure the ledger's ids cannot cover, and it is not small: the
    lifetime this harness derives is 6832s at `-n 20`, so a stranded create is
    around $0.45 -- more than the whole run's printed estimate -- and
    `cleanup` would report "terminated 0 sandbox(es)" while it ran.

    Both routes out are named because each has a caveat the other does not,
    and both were read out of the 1.5.5 wheel rather than taken from prose:

    - `Sandbox.from_name(app, name)` is exact and needs no enumeration. It
      resolves a V2 sandbox by trying `_experimental_from_name` first, but
      **only when `MODAL_SANDBOX_V2` is set** (`modal/sandbox.py:1269`);
      otherwise it takes the V1-only path.
    - `Sandbox.list(tags=...)` finds everything one run asked for at once, and
      routes to the both-backends listing under the same condition
      (`modal/sandbox.py:2592`). Without that variable it omits V2 sandboxes
      entirely -- `_experimental_create`'s own docstring says so
      (`modal/sandbox.py:938`), and `docs/verified.md` §22 item 5 records it as
      the reason orphan reconciliation must use stored ids rather than
      enumeration.

    So the environment variable is part of the instruction, not a footnote,
    and the dashboard is named as the last route because for a V2 sandbox
    looked up without it, it is the only one.
    """
    selector = json.dumps({TAG_RUN: tags[TAG_RUN]})
    lines = [
        f"WARNING: {label}: Sandbox.create blew its deadline, so this process never learned an id.",
        f"  A sandbox may be starting server-side and billing for its whole "
        f"timeout={resolved_sandbox_timeout_s(cfg)}s lifetime.",
        f"  It was asked for as name={name!r} in app {cfg.app_name!r},",
        f"  tagged {json.dumps(dict(tags), sort_keys=True)}.",
        "  Find and stop it:",
        f"    modal.Sandbox.from_name({cfg.app_name!r}, {name!r}).terminate()",
        f"    for sb in modal.Sandbox.list(tags={selector}): sb.terminate()",
    ]
    if cfg.sandbox_v2:
        lines += [
            "  Set MODAL_SANDBOX_V2=1 for both: this run asked for the V2 backend, and without "
            "that variable",
            "  Sandbox.from_name and Sandbox.list each take the V1-only path "
            "(modal/sandbox.py:1269, :2592)",
            "  and a V2 sandbox is not returned at all (modal/sandbox.py:938). If neither finds "
            "it, the Modal",
            "  dashboard is the only remaining route.",
        ]
    return lines


def _create_ready(
    modal: Any,
    cfg: BenchConfig,
    *,
    app: Any,
    image: Any,
    label: str,
    ledger: Ledger,
    deadline: _Deadline,
    out: TextIO,
) -> tuple[Any, float]:
    """Create a sandbox and return it with its create-to-*ready* seconds.

    Readiness is `wait_until_ready()` against a TCP probe on the CMD's port,
    not `create` returning. `create` returning means the scheduler accepted the
    request; a user waits for a port to answer. Measuring the former is the
    specific way this benchmark could produce a confident wrong answer, so the
    two are never conflated.

    `create` itself is inside a deadline, which matters for a reason beyond
    hangs: a throttled create sleeps for the server's `retry_after_secs` and
    retries without incrementing the attempt counter, so with no bound its
    sleep would be recorded here as **Modal cold-start latency**. The reported
    figure is the sum of the two calls' own durations, so the harness's
    bookkeeping between them is not counted either.

    The create is also the one call whose deadline can strand something with
    no id at all -- `ledger.sandboxes` can only be appended to once the handle
    exists, and the deadline fires before that. Hence the `name=` and `tags=`,
    recorded in the ledger *before* the call so the report carries them
    whatever happens, and the recovery block printed the moment the deadline
    fires. The default trigger is `max_throttle_wait_s + 120` = 240s, which a
    first-ever `Image.from_registry` pull can plausibly exceed, so this is not
    a theoretical path.
    """
    name, tags = sandbox_identity(ledger, label)
    intent = ledger.intend(label, name=name, tags=tags)
    try:
        sandbox, create_s = deadline.call(
            f"{label} Sandbox.create",
            cfg.max_throttle_wait_s + 120,
            lambda: modal.Sandbox.create(
                "python3",
                "-m",
                "http.server",
                str(cfg.probe_port),
                app=app,
                name=name,
                tags=tags,
                image=image,
                readiness_probe=modal.Probe.with_tcp(cfg.probe_port),
                **create_kwargs(cfg),
            ),
            hint=f"a sandbox may be starting as name={name!r}; see the recovery lines below",
        )
    except DeadlineExceededError:
        note = _create_deadline_note(cfg, label=label, name=name, tags=tags)
        print("", file=out)
        for line in note:
            print(line, file=out)
        # Into the ledger as well as onto the terminal: the terminal line is
        # lost if the run is scrolled away, and the ledger's copy reaches both
        # the LEAKED block and the JSON artifact.
        ledger.leak(f"sandbox {label}", " ".join(line.strip() for line in note))
        raise
    # None means "no id was ever learned", which is the whole point of the row;
    # an id that exists is recorded as itself.
    object_id = getattr(sandbox, "object_id", None)
    intent["sandbox_id"] = str(object_id) if object_id else None
    ledger.sandboxes.append((label, sandbox))
    _, ready_s = deadline.call(
        f"{label} wait_until_ready",
        cfg.readiness_timeout_s + 30,
        lambda: sandbox.wait_until_ready(timeout=cfg.readiness_timeout_s),
        hint="the sandbox exists and is in the ledger, so cleanup will terminate it",
    )
    return sandbox, create_s + ready_s


def _terminate(label: str, sandbox: Any, *, ledger: Ledger, deadline: _Deadline) -> None:
    """Stop one sandbox's per-second meter now, idempotently, never raising.

    Idempotent via the ledger: a label no longer listed has already been
    terminated, which is what lets `_iteration` terminate on the success path
    for measurement reasons *and* unconditionally in its `finally` without
    double-terminating. A failure leaves the entry in place so the run's final
    `cleanup` retries it, and is not raised, because this runs on teardown
    paths that already have an exception in flight.

    A retry that succeeds **retracts** the earlier attempt's leak line. The
    common case is a `terminate()` that overran `TERMINATE_TIMEOUT_S` and then
    completed: the `finally`'s second call returns at once (Modal's
    `terminate()` is a no-op on a finished sandbox), and without the
    retraction the report listed one sandbox as terminated *and* as "LEAKED
    ... cleanup will retry", with the leak line printed twice -- in the
    document destined for `docs/verified.md`.
    """
    if not any(existing == label for existing, _ in ledger.sandboxes):
        return
    try:
        deadline.call(
            f"{label} terminate",
            TERMINATE_TIMEOUT_S,
            sandbox.terminate,
            hint="the sandbox may still be running and billing; cleanup retries it",
        )
    except Exception as exc:  # - retried by cleanup; must not mask the real error
        ledger.leak(
            f"sandbox {label}",
            f"{_describe(label, sandbox)}: terminate failed ({exc}); cleanup will retry",
        )
        return
    ledger.reclaimed_sandbox(label)


def _unmount(
    cfg: BenchConfig,
    *,
    index: int,
    pool: Any,
    ledger: Ledger,
    deadline: _Deadline,
    certain: bool,
) -> None:
    """Leave the pool member's project path clean, never raising.

    Behind a deadline because `unmount_image` resolves the task id **without**
    `raise_if_task_complete` (`modal/sandbox.py:1688`), so against a pool
    member that has gone away it enters the unbounded 0.5s poll -- and this is
    called from a `finally`, where a hang would strand the whole run.

    Called when the mount landed, and when it **timed out** -- a timed-out
    `mount_image` may well have mounted and the harness cannot ask. It is not
    called when the mount *returned an error*, because then nothing was
    mounted: an unmount would be a wasted RPC, and an unmount failure printed
    a data-quality warning about a tree that never existed, which was false
    every time it appeared. `certain` keeps the two apart in the text as well
    as in the decision.

    Keyed and captioned per iteration, and never retracted by a later success:
    that iteration 4's unmount worked says nothing about the measurement
    iteration 3's failure polluted.
    """
    try:
        deadline.call(
            "unmount_image",
            UNMOUNT_TIMEOUT_S,
            lambda: pool.unmount_image(cfg.project_path),
            hint="the path may or may not still be mounted",
        )
    except Exception as exc:  # - recorded; the next iteration is warned instead
        superset = (
            "Modal replaces a populated path on the next mount, so a later iteration's tree "
            "may be a superset of what it snapshotted and its read check would not catch that."
        )
        ledger.leak(
            f"mount {cfg.project_path} (iteration {index})",
            f"iteration {index}: pool mount at {cfg.project_path}: unmount failed ({exc}). "
            + (
                superset
                if certain
                else f"The mount itself timed out, so the path may or may not hold that tree. "
                f"If it does: {superset}"
            ),
        )


def _snapshot(
    cfg: BenchConfig,
    *,
    sandbox: Any,
    take: Callable[[], Any],
    what: str,
    ledger: Ledger,
    deadline: _Deadline,
) -> tuple[Any, float]:
    """Take a snapshot, recording its image id before anything else can fail."""
    if sandbox.poll() is not None:
        raise BenchError(
            f"{what}: the sandbox has already finished, so there is nothing to snapshot. "
            "Snapshots are only possible from a running sandbox."
        )
    image, seconds = deadline.call(
        what,
        cfg.snapshot_timeout_s + 60,
        take,
        hint=(
            "an image may exist whose id this process will never see, billed until its "
            f"ttl={cfg.snapshot_ttl_s}s runs out"
        ),
    )
    image_id = getattr(image, "object_id", None)
    if image_id:
        ledger.images.append(str(image_id))
    else:
        ledger.leak(
            f"image from {what}",
            f"image from {what}: created but exposed no object_id, so it cannot be deleted; "
            f"it expires at ttl={cfg.snapshot_ttl_s}s",
        )
    return image, seconds


def _resolve_image(modal: Any, cfg: BenchConfig) -> tuple[Any, str]:
    if cfg.image_name:
        return modal.Image.from_name(cfg.image_name), f"Image.from_name({cfg.image_name!r})"
    tag = cfg.image_registry_tag
    return modal.Image.from_registry(tag), f"Image.from_registry({tag!r})"


def measure(modal: Any, cfg: BenchConfig, *, out: TextIO, ledger: Ledger, run: Run) -> Run:
    """The measurement itself. Assumes the gates have already passed.

    `run` is owned by the caller rather than created here, so that samples
    already taken survive a Ctrl-C or a failure in the pool member. A run that
    dies during iteration 18 of 20 has been paid for either way; discarding
    its eighteen measurements would be the expensive mistake.
    """
    app = modal.App.lookup(cfg.app_name, create_if_missing=True)
    image, image_provenance = _resolve_image(modal, cfg)
    run.meta["image"] = image_provenance
    # The recovery key for everything this run asks Modal for, in the report as
    # well as in the ledger: one tag value finds every sandbox one run created.
    run.meta["run_id"] = ledger.run_id

    with _Deadline() as deadline:
        try:
            # One pool member for the whole run, which is what a warm pool
            # actually is: created once, then paid for while idle. Its own
            # create-to-ready is metadata rather than a phase sample -- with
            # n=1 a percentile of it would be theatre.
            pool, pool_ready_s = _create_ready(
                modal,
                cfg,
                app=app,
                image=image,
                label="pool",
                ledger=ledger,
                deadline=deadline,
                out=out,
            )
            backend, backend_from = attribute_backend(pool)
            run.meta["pool_create_to_ready_s"] = round(pool_ready_s, 3)
            run.meta["pool_sandbox_id"] = str(pool.object_id)
            run.meta["backend_observed"] = backend
            run.meta["backend_observed_from"] = backend_from
            print(
                f"pool member {pool.object_id} ready in {pool_ready_s:.2f}s "
                f"(backend {backend}, from {backend_from})",
                file=out,
            )

            for i in range(1, cfg.iterations + 1):
                print(f"\n-- iteration {i}/{cfg.iterations}", file=out)
                try:
                    _iteration(
                        modal,
                        cfg,
                        index=i,
                        app=app,
                        image=image,
                        pool=pool,
                        run=run,
                        ledger=ledger,
                        deadline=deadline,
                        out=out,
                    )
                    run.iterations_completed += 1
                except Exception as exc:  # one bad iteration must not end the run
                    # A failed iteration is data. It is recorded and the run
                    # continues: four measured iterations plus a described
                    # failure beat zero iterations and a traceback, and by this
                    # point the pool member has already been created and billed
                    # for.
                    #
                    # Broad on purpose, but the exception *type* is recorded, so
                    # a programming error repeated N times is visible in the
                    # report rather than disguised as N slow iterations.
                    print(f"   iteration {i} FAILED: {type(exc).__name__}: {exc}", file=out)
                    run.failures.append(
                        {"iteration": i, "error_type": type(exc).__name__, "error": str(exc)}
                    )
        finally:
            # In a `finally` because the deadline most worth recording is the
            # one that ends the run before the loop ever starts. Assigned after
            # the loop, a timeout on the pool's create or readiness -- the
            # failure that ends the whole run -- never reached the JSON, so the
            # artifact of a run that hung did not say which call hung.
            run.meta["sandbox_lifetime_s"] = resolved_sandbox_timeout_s(cfg)
            run.meta["max_throttle_wait_s"] = cfg.max_throttle_wait_s
            if deadline.stuck:
                run.meta["deadline_timeouts"] = list(deadline.stuck)
    return run


def _iteration(
    modal: Any,
    cfg: BenchConfig,
    *,
    index: int,
    app: Any,
    image: Any,
    pool: Any,
    run: Run,
    ledger: Ledger,
    deadline: _Deadline,
    out: TextIO,
) -> None:
    """One iteration of every phase. Everything it creates, it destroys.

    The `finally` is load-bearing twice over, and both halves were reproduced
    as live defects before it existed:

    - **Money.** `measure`'s `except Exception` swallows a failed iteration and
      starts the next one, so a success-path-only `terminate()` left the
      iteration's subject or restore sandbox running for the rest of the run.
      At `-n 20` a failure in iteration 2 stranded a sandbox for ~70 minutes,
      about half the run's whole estimated cost, entirely outside the printed
      floor.
    - **Correctness.** A success-path-only `unmount_image` left the pool
      member's project path populated, and Modal replaces a populated path on
      the next mount. The next iteration then measured a mount onto a dirty
      path and recorded it as valid data -- and `_READ_ALL`'s file count
      catches a *short* tree but not a superset, so nothing downstream would
      have noticed.

    The unmount is attempted when the mount landed and when it **timed out**,
    because a timed-out `mount_image` may well have mounted -- but not when
    the mount returned an error, because then nothing was mounted and the
    warning an unmount failure prints would be false. Three states, not two:
    see `_MOUNT_LANDED` and friends.
    """
    created: list[tuple[str, Any]] = []
    mount_state = _MOUNT_NOT_ATTEMPTED
    try:
        subject, ready_s = _create_ready(
            modal,
            cfg,
            app=app,
            image=image,
            label=f"subject-{index}",
            ledger=ledger,
            deadline=deadline,
            out=out,
        )
        created.append((f"subject-{index}", subject))
        run.record("cold_create_to_ready", ready_s, iteration=index)
        print(f"   cold_create_to_ready      {ready_s:8.2f}s  {subject.object_id}", file=out)

        # Workload. Outside every measured window, and reported as metadata so
        # the latencies below are interpretable rather than just small or large.
        # Behind a deadline like everything else, even though it is not
        # measured: `_exec` drains stdout and waits, and an exec whose stream
        # never closes would hang the run past the `finally` that stops the
        # meters -- the one unbounded path a timeout cannot help with.
        setup_cmd = cfg.project_setup_cmd
        if setup_cmd:
            deadline.call(
                "project_setup_cmd",
                cfg.exec_timeout_s + 60,
                lambda: _exec(subject, "sh", "-lc", setup_cmd, timeout=cfg.exec_timeout_s),
            )
        else:
            deadline.call(
                "fill_project",
                cfg.exec_timeout_s + 60,
                lambda: _exec(
                    subject,
                    "python3",
                    "-c",
                    _FILL,
                    cfg.project_path,
                    str(cfg.project_files),
                    str(cfg.project_file_kib),
                    cfg.project_fill,
                    timeout=cfg.exec_timeout_s,
                ),
            )
        stdout, _ = deadline.call(
            "measure_project",
            360,
            lambda: _exec(subject, "python3", "-c", _MEASURE, cfg.project_path, timeout=300),
        )
        measured = json.loads(stdout)
        workload: dict[str, object] = {
            "iteration": index,
            "path": cfg.project_path,
            "files": measured["files"],
            "bytes": measured["bytes"],
            "fill": "setup-cmd" if cfg.project_setup_cmd else cfg.project_fill,
            "setup_cmd": cfg.project_setup_cmd,
        }
        run.workloads.append(workload)
        print(f"   workload: {measured['files']} files, {measured['bytes'] / 1e6:.1f} MB", file=out)

        fs_image, fs_s = _snapshot(
            cfg,
            sandbox=subject,
            take=lambda: subject.snapshot_filesystem(
                cfg.snapshot_timeout_s, ttl=cfg.snapshot_ttl_s
            ),
            what="snapshot_filesystem",
            ledger=ledger,
            deadline=deadline,
        )
        run.record("snapshot_filesystem", fs_s, iteration=index)
        print(f"   snapshot_filesystem       {fs_s:8.2f}s", file=out)

        dir_image, dir_s = _snapshot(
            cfg,
            sandbox=subject,
            take=lambda: subject.snapshot_directory(
                cfg.project_path, timeout=cfg.snapshot_timeout_s, ttl=cfg.snapshot_ttl_s
            ),
            what="snapshot_directory",
            ledger=ledger,
            deadline=deadline,
        )
        run.record("snapshot_directory", dir_s, iteration=index)
        print(f"   snapshot_directory        {dir_s:8.2f}s", file=out)

        # Snapshot, THEN terminate -- the only possible order, since both
        # snapshot calls are RPCs to the live container. Terminated here rather
        # than left to the `finally` because the restore below should not be
        # measured while a second sandbox of the same size is still allocated.
        _terminate(f"subject-{index}", subject, ledger=ledger, deadline=deadline)

        # Path A: resume by booting the filesystem snapshot. `Sandbox.create`
        # from a snapshot image is the whole of this path, so the phase is
        # already end-to-end.
        restored, restore_s = _create_ready(
            modal,
            cfg,
            app=app,
            image=fs_image,
            label=f"restore-{index}",
            ledger=ledger,
            deadline=deadline,
            out=out,
        )
        created.append((f"restore-{index}", restored))
        run.record("restore_by_boot_to_ready", restore_s, iteration=index)
        print(f"   restore_by_boot_to_ready  {restore_s:8.2f}s  {restored.object_id}", file=out)
        _terminate(f"restore-{index}", restored, ledger=ledger, deadline=deadline)

        # The instrument, measured before the mount so a mounted tree cannot
        # perturb it: what `restore_by_mount_read` costs with nothing to read.
        _, calibration_s = deadline.call(
            "exec_spawn_overhead",
            cfg.exec_timeout_s + 60,
            lambda: _exec(pool, "python3", "-c", _NOOP, timeout=cfg.exec_timeout_s),
        )
        run.record("exec_spawn_overhead", calibration_s, iteration=index)

        # Path B: resume by mounting the directory snapshot into the
        # already-warm pool member. verified.md identifies this as the
        # mechanism that makes a warm pool viable at all, since `volumes=` is
        # create-only.
        #
        # `mount_state` is pessimistic before the call and corrected after it,
        # so that every exit from `deadline.call` leaves the `finally` below
        # with the right answer: a raise that is not a `DeadlineExceededError`
        # means `mount_image` returned an error and nothing was mounted.
        mount_state = _MOUNT_FAILED
        try:
            _, mount_s = deadline.call(
                "mount_image",
                cfg.snapshot_timeout_s + 60,
                lambda: pool.mount_image(cfg.project_path, dir_image),
                hint="the mount may have landed, so the unmount is attempted anyway",
            )
        except DeadlineExceededError:
            mount_state = _MOUNT_MAYBE
            raise
        mount_state = _MOUNT_LANDED
        run.record("restore_by_mount", mount_s, iteration=index)

        _, read_s = deadline.call(
            "restore_by_mount_read",
            cfg.exec_timeout_s + 60,
            lambda: _exec(
                pool,
                "python3",
                "-c",
                _READ_ALL,
                cfg.project_path,
                str(measured["files"]),
                timeout=cfg.exec_timeout_s,
            ),
        )
        run.record("restore_by_mount_read", read_s, iteration=index)
        print(
            f"   restore_by_mount          {mount_s:8.2f}s  (+ full read {read_s:.2f}s, "
            f"of which exec spawn {calibration_s:.2f}s)",
            file=out,
        )
    finally:
        # Sandboxes first: each one left running is a per-second meter, where a
        # dirty mount is only a wrong number. Then the mount, because the next
        # iteration mounts onto the same path.
        for label, sandbox in reversed(created):
            _terminate(label, sandbox, ledger=ledger, deadline=deadline)
        if mount_state in (_MOUNT_LANDED, _MOUNT_MAYBE):
            _unmount(
                cfg,
                index=index,
                pool=pool,
                ledger=ledger,
                deadline=deadline,
                certain=mount_state == _MOUNT_LANDED,
            )


# ------------------------------------------------------------------- the report


def summaries(run: Run) -> list[PhaseSummary]:
    """Every measured phase plus the one derived end-to-end figure."""
    order = [
        "cold_create_to_ready",
        "snapshot_filesystem",
        "snapshot_directory",
        "restore_by_boot_to_ready",
        "restore_by_mount",
        "restore_by_mount_read",
        "exec_spawn_overhead",
    ]
    notes = {
        "cold_create_to_ready": "Sandbox.create from the base image to readiness_probe passing",
        "restore_by_boot_to_ready": "resume by booting the filesystem snapshot; end to end",
        "restore_by_mount": "mount_image() return only, into an already-ready pool member",
        "restore_by_mount_read": (
            "full recursive read of the mounted tree afterwards; includes the exec spawn cost "
            "measured separately as exec_spawn_overhead"
        ),
        "exec_spawn_overhead": (
            "a no-op exec into the same pool member: the fixed instrument cost included in "
            "restore_by_mount_read, which a real resume would not pay"
        ),
    }
    out = [
        summarize(name, run.ordered(name), note=notes.get(name, ""))
        for name in order
        if run.samples.get(name)
    ]
    mount = run.samples.get("restore_by_mount", {})
    read = run.samples.get("restore_by_mount_read", {})
    totals = keyed_total(mount, read)
    if totals:
        out.append(
            summarize(
                "restore_by_mount_total",
                totals,
                note=(
                    "mount + full read, joined on the iteration number and summed per "
                    "iteration, then percentiled; iterations missing either half are dropped"
                ),
            )
        )
    return out


def report(cfg: BenchConfig, run: Run, ledger: Ledger, estimate: CostEstimate) -> dict[str, object]:
    """The machine-readable artifact. Raw samples included on purpose.

    Percentiles from a 5-sample run are not comparable with percentiles from a
    20-sample run, and the next SDK bump will want to re-derive both from the
    same numbers. Keeping the samples makes the JSON diffable; keeping only the
    summary would not be.
    """
    return {
        "schema": REPORT_SCHEMA,
        "generated_at": datetime.now(UTC).isoformat(timespec="seconds"),
        "modal_client_pin": MODAL_PIN,
        "backend_requested": "v2" if cfg.sandbox_v2 else "v1",
        "config": {k: v for k, v in asdict(cfg).items() if k != "cost_model"},
        "cost_model": asdict(cfg.cost_model),
        "cost_estimate": estimate.to_json(),
        "iterations_requested": cfg.iterations,
        "iterations_completed": run.iterations_completed,
        "metadata": run.meta,
        "sandbox_lifetime_s": resolved_sandbox_timeout_s(cfg),
        "phases": [s.to_json() for s in summaries(run)],
        "samples_s": {phase: run.ordered(phase) for phase in run.samples},
        # Which iteration each sample in `samples_s` came from. Without this a
        # reader cannot tell a 19-sample phase that skipped iteration 7 from
        # one that stopped at 19, and those are different runs.
        "sample_iterations": {phase: run.iterations_of(phase) for phase in run.samples},
        "workloads": run.workloads,
        "failures": run.failures,
        "cleanup": ledger.to_json(),
    }


def workload_note(run: Run) -> str:
    """One line describing the trees this run actually snapshotted.

    Derived from every iteration's workload rather than from a single
    `meta["workload"]` that each iteration overwrote. The overwritten value
    described the LAST iteration's tree while the table above it described all
    of them, which under `--project-setup-cmd` (where the tree is rebuilt each
    time and need not come out identical) is a caption that does not match its
    figure.
    """
    if not run.workloads:
        return "unknown: no iteration got as far as measuring its tree"
    files = sorted(int(w["files"]) for w in run.workloads)  # type: ignore[call-overload]
    total = sorted(int(w["bytes"]) for w in run.workloads)  # type: ignore[call-overload]
    fills = sorted({str(w["fill"]) for w in run.workloads})
    n = len(run.workloads)
    if files[0] == files[-1] and total[0] == total[-1]:
        return f"{files[0]} files, {total[0] / 1e6:.1f} MB, fill={'/'.join(fills)} (all {n})"
    return (
        f"{files[0]}-{files[-1]} files, {total[0] / 1e6:.1f}-{total[-1] / 1e6:.1f} MB, "
        f"fill={'/'.join(fills)} (varied across {n} iteration(s))"
    )


def render_human(cfg: BenchConfig, run: Run, document: Mapping[str, object]) -> str:
    phases = summaries(run)
    if not phases:
        return "No phase completed, so there is nothing to report."
    observed = run.meta.get("backend_observed", "unknown")
    how = run.meta.get("backend_observed_from", "not observed")
    meta = {
        "modal": f"=={MODAL_PIN}",
        "backend": f"{document['backend_requested']} requested, {observed} observed ({how})",
        "sandbox": f"cpu={cfg.cpu_cores} physical core(s), {cfg.memory_mib} MiB"
        f"{', hard-capped' if cfg.hard_cap_resources else ', burstable'}",
        "region": cfg.region or "unpinned (no region surcharge; snapshots reside in the US)",
        "sandbox_lifetime_s": document.get("sandbox_lifetime_s", "unknown"),
        "max_throttle_wait_s": cfg.max_throttle_wait_s,
        "workload": workload_note(run),
        "iterations": f"{run.iterations_completed} of {cfg.iterations} completed",
        "pool_create_to_ready_s": run.meta.get("pool_create_to_ready_s", "unknown"),
        "run_id": run.meta.get("run_id", "unknown"),
    }
    # Only when there were any: a row reading `[]` invites the reader to
    # wonder what it means, and a run with no stuck call has nothing to say
    # here. A run with one has the most important line in the table.
    if run.meta.get("deadline_timeouts"):
        meta["deadline_timeouts"] = run.meta["deadline_timeouts"]
    body = render_markdown(phases, meta=meta)
    overhead = run.ordered("exec_spawn_overhead")
    if overhead and run.samples.get("restore_by_mount_read"):
        instrument = summarize("exec_spawn_overhead", overhead)
        body += (
            f"\n\nrestore_by_mount_read and restore_by_mount_total include the cost of the "
            f"exec used to read the tree, measured here as exec_spawn_overhead: p50 "
            f"{instrument.percentiles_s[50]:.2f}s of every read sample is the instrument, not "
            "Modal. It is reported rather than subtracted because the two distributions "
            "overlap at these sample counts and a subtraction can go negative."
        )
    if cfg.iterations < samples_to_resolve(95):
        body += (
            f"\n\nThis run used {cfg.iterations} iterations. p95 does not become a distinct "
            f"observation until n>={samples_to_resolve(95)}, and p99 until "
            f"n>={samples_to_resolve(99)}. Quote p50 from this run; quote a tail only from a "
            "run long enough to have one."
        )
    return body


# ----------------------------------------------------------------------- the CLI


def _parser() -> argparse.ArgumentParser:
    d = BenchConfig()
    p = argparse.ArgumentParser(
        prog="python -m halyard_sandboxd.bench",
        description=(
            "Measure Modal snapshot/restore latency. Prints a cost estimate and exits "
            "unless --run is given AND " + SPEND_ENV + "=1."
        ),
    )
    p.add_argument("--run", action="store_true", help="actually create sandboxes (costs money)")
    p.add_argument("-n", "--iterations", type=int, default=d.iterations)
    p.add_argument("--cpu", type=float, default=d.cpu_cores, help="PHYSICAL cores; 1.0 == 2 vCPU")
    p.add_argument("--memory-mib", type=int, default=d.memory_mib)
    p.add_argument(
        "--burstable",
        action="store_true",
        help="request rather than hard-cap cpu/memory (permits billed bursting)",
    )
    p.add_argument(
        "--sandbox-timeout",
        type=int,
        default=d.sandbox_timeout_s,
        help="sandbox lifetime in seconds; default is derived from the run's own projected "
        f"wall clock (Modal caps it at {MODAL_MAX_SANDBOX_TIMEOUT_S})",
    )
    p.add_argument("--exec-timeout", type=int, default=d.exec_timeout_s)
    p.add_argument(
        "--max-throttle-wait",
        type=int,
        default=d.max_throttle_wait_s,
        help="MODAL_MAX_THROTTLE_WAIT: seconds a throttled call may sleep before it errors",
    )
    p.add_argument("--snapshot-timeout", type=int, default=d.snapshot_timeout_s)
    p.add_argument("--snapshot-ttl", type=int, default=d.snapshot_ttl_s)
    p.add_argument("--readiness-timeout", type=int, default=d.readiness_timeout_s)
    p.add_argument("--project-path", default=d.project_path)
    p.add_argument("--project-files", type=int, default=d.project_files)
    p.add_argument("--project-file-kib", type=int, default=d.project_file_kib)
    p.add_argument("--project-fill", choices=("random", "zero"), default=d.project_fill)
    p.add_argument(
        "--project-setup-cmd",
        default=d.project_setup_cmd,
        help="shell command to build the tree instead of synthesising it; needs egress",
    )
    p.add_argument("--app-name", default=d.app_name)
    p.add_argument("--image-name", default=d.image_name, help="a published named Modal Image")
    p.add_argument("--image-registry-tag", default=d.image_registry_tag)
    p.add_argument(
        "--region",
        default=d.region,
        help="pin a region (broad: us/eu/ap). Unpinned by default: pinning costs 1.15x broad "
        "or 1.75x narrow and the benchmark should measure the platform, not the surcharge",
    )
    p.add_argument("--allow-cidr", action="append", default=[], metavar="CIDR")
    p.add_argument("--allow-domain", action="append", default=[], metavar="DOMAIN")
    p.add_argument("--sandbox-v1", action="store_true", help="measure the V1 backend instead")
    p.add_argument("--probe-port", type=int, default=d.probe_port)
    p.add_argument(
        "--json-out",
        type=Path,
        default=Path(tempfile.gettempdir()) / "halyard-modal-resume-latency.json",
    )
    return p


def config_from_args(args: argparse.Namespace) -> BenchConfig:
    return BenchConfig(
        iterations=args.iterations,
        cpu_cores=args.cpu,
        memory_mib=args.memory_mib,
        hard_cap_resources=not args.burstable,
        sandbox_timeout_s=args.sandbox_timeout,
        exec_timeout_s=args.exec_timeout,
        max_throttle_wait_s=args.max_throttle_wait,
        snapshot_timeout_s=args.snapshot_timeout,
        snapshot_ttl_s=args.snapshot_ttl,
        readiness_timeout_s=args.readiness_timeout,
        probe_port=args.probe_port,
        project_path=args.project_path,
        project_files=args.project_files,
        project_file_kib=args.project_file_kib,
        project_fill=args.project_fill,
        project_setup_cmd=args.project_setup_cmd,
        app_name=args.app_name,
        image_name=args.image_name,
        image_registry_tag=args.image_registry_tag,
        region=args.region,
        outbound_cidr_allowlist=tuple(args.allow_cidr),
        outbound_domain_allowlist=tuple(args.allow_domain),
        sandbox_v2=not args.sandbox_v1,
    )


def _emit(
    cfg: BenchConfig,
    run: Run,
    ledger: Ledger,
    estimate: CostEstimate,
    *,
    json_out: Path,
    out: TextIO,
) -> dict[str, object]:
    """Print the human report and write the JSON, and return the document emitted.

    The human half first and the file write guarded, because a `--json-out`
    pointing into an unwritable directory used to discard the entire output of
    a paid 75-minute run before a single number reached the terminal.
    """
    document = report(cfg, run, ledger, estimate)
    print("", file=out)
    print(render_human(cfg, run, document), file=out)
    print("", file=out)
    serialised = json.dumps(document, indent=2, sort_keys=False)
    try:
        json_out.write_text(serialised + "\n")
    except OSError as exc:
        print(f"could not write {json_out} ({exc}); the report follows on stdout", file=out)
        print(serialised, file=out)
    else:
        print(f"machine-readable report: {json_out}", file=out)
    return document


def _rewrite(
    cfg: BenchConfig,
    run: Run,
    ledger: Ledger,
    estimate: CostEstimate,
    *,
    json_out: Path,
    out: TextIO,
) -> None:
    """Fold cleanup's outcome into the file `_emit` already wrote.

    Quiet on success: the path was printed with the report. A failure here is
    not a lost report -- the copy above stands, and it is the one that matters,
    since it lists everything cleanup was about to attempt -- so this warns in
    one line rather than dumping the whole document a second time.
    """
    try:
        json_out.write_text(json.dumps(report(cfg, run, ledger, estimate), indent=2) + "\n")
    except OSError as exc:
        print(
            f"could not update {json_out} with the cleanup outcome ({exc}); the cleanup lines "
            "above are the only record of it",
            file=out,
        )


def _on_sigterm(signum: int, _frame: FrameType | None) -> None:
    # Routed into KeyboardInterrupt so a `kill` and a Ctrl-C take the same
    # cleanup path. SIGKILL cannot be caught, which is why the ttl is short.
    raise KeyboardInterrupt(f"signal {signum}")


def main(argv: Sequence[str] | None = None, *, out: TextIO | None = None) -> int:
    out = out or sys.stdout
    args = _parser().parse_args(argv)
    cfg = config_from_args(args)

    estimate = estimate_for(cfg)
    # Money first, unconditionally, before any gate is even evaluated.
    print(
        render_cost_estimate(
            estimate,
            sandbox_lifetime_s=resolved_sandbox_timeout_s(cfg),
            lifetime_basis=lifetime_basis(cfg),
            max_throttle_wait_s=cfg.max_throttle_wait_s,
        ),
        file=out,
    )
    print("", file=out)

    if not args.run:
        print("Dry run: nothing was created. Re-run with --run to measure.", file=out)
        return 0

    # Set BEFORE `blockers`, because `blockers` imports modal (via
    # modal_import_error) and the client reads its config at import time.
    # `config.get` does resolve MODAL_<KEY> from the environment on every read
    # (`modal/config.py:362-382`), so setting it later happened to work -- but
    # the comment that claimed this ran before the import was simply false,
    # and a release that snapshots config at import would have made it a bug.
    #
    # MODAL_MAX_THROTTLE_WAIT is set for the reason docs/verified.md gives:
    # unset, `config.get("max_throttle_wait")` is None and a throttled call
    # sleeps for the server's `retry_after_secs` and retries forever without
    # incrementing the attempt counter.
    os.environ["MODAL_SANDBOX_V2"] = "1" if cfg.sandbox_v2 else "0"
    os.environ["MODAL_MAX_THROTTLE_WAIT"] = str(cfg.max_throttle_wait_s)

    reasons = blockers(
        cfg,
        environ=os.environ,
        credentials=modal_credentials_present(os.environ),
        import_error=modal_import_error(),
    )
    if reasons:
        print("Refusing to run:", file=out)
        for reason in reasons:
            print(f"  - {reason}", file=out)
        return 2

    import modal  # deliberately lazy; see modal_import_error

    signal.signal(signal.SIGTERM, _on_sigterm)
    ledger = Ledger()
    run = Run()
    status = 0
    try:
        measure(modal, cfg, out=out, ledger=ledger, run=run)
    except KeyboardInterrupt:
        print("\ninterrupted; reporting what was measured, then cleaning up", file=out)
        status = 130
    except Exception as exc:  # reported here, then cleaned up below
        print(f"\nrun failed: {exc}", file=out)
        status = 1

    # The report comes out BEFORE cleanup, and cleanup's outcome after it. That
    # is the same lesson as the guarded file write inside `_emit`, learned a
    # second time and more expensively: with cleanup first, a `terminate()`
    # that blocked forever hung the main thread before the phase table, the
    # JSON document and -- worst -- the LEAKED ids were printed. The SIGTERM
    # used to kill it then became a traceback, so a paid run produced its
    # per-iteration lines and nothing else, while the ids a human needs in
    # order to stop the billing existed only in the hung process's memory.
    #
    # The cost of the swap is bounded and small: rendering and writing are
    # pure local work over at most a few hundred samples -- milliseconds
    # against a per-second meter -- where the thing it now precedes is
    # unbounded by construction.
    #
    # Guarded against `BaseException` for the same reason `cleanup` is, and it
    # is this reordering that makes it matter: the `kill` aimed at a hung
    # teardown can now land during the report instead, and stopping the meters
    # must not be hostage to printing a table either. Caught rather than
    # re-raised, because a traceback out of `main` is how the last version of
    # this lost its LEAKED block.
    before_cleanup: dict[str, object] | None = None
    try:
        before_cleanup = _emit(cfg, run, ledger, estimate, json_out=args.json_out, out=out)
    except BaseException as exc:
        print(f"\nreporting failed ({type(exc).__name__}: {exc}); cleaning up anyway", file=out)
        status = status or (130 if isinstance(exc, KeyboardInterrupt) else 1)
    print("", file=out)
    cleanup(modal, ledger, out=out)
    if before_cleanup is None or ledger.to_json() != before_cleanup["cleanup"]:
        # Cleanup reclaimed something the document above lists as outstanding,
        # or failed to, so the file is rewritten with the final answer. The
        # copy already on stdout stands as the record of a run whose cleanup
        # never came back.
        _rewrite(cfg, run, ledger, estimate, json_out=args.json_out, out=out)
    if run.iterations_completed == 0:
        return status or 1
    return status
