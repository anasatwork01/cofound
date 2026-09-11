"""Percentiles, cost arithmetic and report rendering for the Modal benchmarks.

Kept separate from `resume_latency` so that this module imports with **no
`modal` installed and no credentials present**. That is what lets `make verify`
cover the half of the harness most likely to be silently wrong.

A percentile off by one rank does not crash. It produces a plausible number,
and a plausible wrong number is worse than no number at all when the output is
going to be pasted into `docs/verified.md` and become SPEC §17's resume SLO.
So the ranking rule is stated exactly, computed in integer arithmetic, and
pinned by tests.
"""

from __future__ import annotations

import json
from collections.abc import Iterable, Mapping, Sequence
from dataclasses import dataclass

# What every phase reports. SPEC §17's SLO table quotes p50 and p95; p99 is
# here because a resume that hangs is the failure users actually notice.
REPORTED_PERCENTILES: tuple[int, ...] = (50, 95, 99)


# --------------------------------------------------------------- percentiles


def nearest_rank_index(percentile: int, n: int) -> int:
    """Index into `n` ascending samples for `percentile`, by nearest rank.

    Nearest rank -- NIST's "C = 1" definition, the inverse of the empirical
    CDF -- rather than any interpolating variant. Every number this harness
    reports is then a latency that was actually observed, not a weighted
    average of two that were. With N in the tens that distinction is the whole
    ballgame: interpolation manufactures a measurement between two real ones
    and hands it to the reader with the same apparent confidence.

    The arithmetic is integer on purpose. `math.ceil(percentile / 100 * n)`
    is *not* equivalent, because `percentile / 100` is generally inexact and
    the product can land just above an integer:

        >>> import math; 7 / 100 * 100, math.ceil(7 / 100 * 100)
        (7.000000000000001, 8)

    -- rank 8 where rank 7 was meant, one whole sample off. It happens for
    2821 of the 1_980_000 (percentile, n) pairs with n <= 20000, and, checked
    exhaustively, for **none** of them at p50/p95/p99 with n <= 20000. So the
    integer form is not fixing a live bug in today's report; it removes the
    class of bug, at zero cost, from arithmetic that a later change to
    `REPORTED_PERCENTILES` would walk straight into. `(p * n + 99) // 100` is
    exact ceiling division over integers and has no such case.
    """
    if n <= 0:
        raise ValueError("cannot take a percentile of zero samples")
    if not 0 <= percentile <= 100:
        raise ValueError(f"percentile must be within 0..100, got {percentile}")
    rank = (percentile * n + 99) // 100  # ceil(percentile * n / 100), exactly
    return min(max(rank - 1, 0), n - 1)


def quantile(samples: Sequence[float], percentile: int) -> float:
    """`percentile` of `samples`, by nearest rank. Does not require sorted input."""
    return sorted(samples)[nearest_rank_index(percentile, len(samples))]


def samples_to_resolve(percentile: int, *, ceiling: int = 100_000) -> int:
    """Smallest N at which `percentile` is a sample strictly below the maximum.

    Below that N the "p95" being reported *is* the largest observation, so it
    carries no more information than `max` and must not be read as a tail
    estimate. The answers are the familiar ones -- 20 for p95, 100 for p99 --
    but they are derived from the same ranking function the report uses rather
    than asserted, so the two cannot drift apart.
    """
    if percentile >= 100:
        return 1
    for n in range(1, ceiling + 1):
        if nearest_rank_index(percentile, n) < n - 1:
            return n
    raise ValueError(f"no N below {ceiling} resolves p{percentile}")


def pinned_to_max(n: int, percentiles: Iterable[int] = REPORTED_PERCENTILES) -> tuple[int, ...]:
    """Which `percentiles` are, at this N, just another name for the maximum."""
    return tuple(p for p in percentiles if p < 100 and nearest_rank_index(p, n) == n - 1)


@dataclass(frozen=True, slots=True)
class PhaseSummary:
    """One measured phase, summarised. Seconds throughout; never milliseconds."""

    phase: str
    n: int
    min_s: float
    mean_s: float
    max_s: float
    percentiles_s: Mapping[int, float]
    # Percentiles that equal `max_s` because N is too small to resolve them.
    # Reported rather than suppressed: a caller who prints p99 from a 5-sample
    # run should be told, in the artifact, that it is the maximum.
    unresolved: tuple[int, ...]
    note: str = ""

    def to_json(self) -> dict[str, object]:
        out: dict[str, object] = {"phase": self.phase, "n": self.n, "min_s": self.min_s}
        out.update({f"p{p}_s": v for p, v in sorted(self.percentiles_s.items())})
        out["max_s"] = self.max_s
        out["mean_s"] = self.mean_s
        out["unresolved_percentiles"] = list(self.unresolved)
        if self.note:
            out["note"] = self.note
        return out


def summarize(
    phase: str,
    samples: Sequence[float],
    *,
    percentiles: Sequence[int] = REPORTED_PERCENTILES,
    note: str = "",
) -> PhaseSummary:
    """Summarise one phase's samples. Raises on an empty phase rather than emitting zeros."""
    if not samples:
        raise ValueError(f"phase {phase!r} has no samples")
    ordered = sorted(samples)
    n = len(ordered)
    return PhaseSummary(
        phase=phase,
        n=n,
        min_s=ordered[0],
        mean_s=sum(ordered) / n,
        max_s=ordered[-1],
        percentiles_s={p: ordered[nearest_rank_index(p, n)] for p in percentiles},
        unresolved=pinned_to_max(n, percentiles),
        note=note,
    )


def keyed_total(*phases: Mapping[int, float]) -> list[float]:
    """Per-iteration sums of several phases, joined on the ITERATION NUMBER.

    An end-to-end percentile has to be computed from per-iteration totals, not
    by adding each phase's own percentile. `p95(mount) + p95(verify)` is the
    latency of an iteration that placed 95th percentile in *both* phases,
    which is the 95th percentile of nothing and is biased high.

    Joined on the iteration number because list position is not a join key.
    The earlier version zipped positionally and claimed truncation made a
    mispairing impossible; truncation only protects the case where the LAST
    iteration is short. Fail iteration 3's read of a 5-iteration run and
    `restore_by_mount` has 5 samples against `restore_by_mount_read`'s 4, so
    position pairs mount(i3) with read(i4) and mount(i4) with read(i5) and
    drops i5's mount entirely -- and `restore_by_mount_total` is the figure
    destined for SPEC §17's SLO row. A mid-run read failure is not exotic:
    `_READ_ALL` exits non-zero on a file-count mismatch, which is also what a
    dirty mount produces.

    Iterations missing any phase are dropped whole. Half an iteration is not
    an end-to-end measurement of anything.
    """
    if not phases:
        return []
    shared = set(phases[0])
    for phase in phases[1:]:
        shared &= set(phase)
    return [sum(phase[iteration] for phase in phases) for iteration in sorted(shared)]


# ---------------------------------------------------------------------- cost


@dataclass(frozen=True, slots=True)
class SandboxRate:
    """Modal Sandbox per-second pricing, from `docs/verified.md` §22 item 5.

    Sandboxes bill at roughly 3x standard Function rates, and the meter runs
    per-second on `max(request, actual)` for as long as the container is
    allocated. These are the Sandbox numbers, not the Function ones.
    """

    core_second_usd: float = 0.00003942
    gib_second_usd: float = 0.00000667

    def per_second_usd(self, *, cpu_cores: float, memory_mib: int) -> float:
        """Cost of one allocated second. `cpu_cores` is *physical* cores -- see BenchConfig."""
        return cpu_cores * self.core_second_usd + (memory_mib / 1024) * self.gib_second_usd

    def per_hour_usd(self, *, cpu_cores: float, memory_mib: int) -> float:
        return 3600.0 * self.per_second_usd(cpu_cores=cpu_cores, memory_mib=memory_mib)


@dataclass(frozen=True, slots=True)
class CostModel:
    """How many seconds each sandbox is expected to stay *allocated*, per iteration.

    Every field is an assumption, and measuring them is the entire point of the
    harness -- `create_to_ready_s` in particular is deliberately far above
    Modal's published ~0.5s median, because the published figure is a median
    with no p99 behind it. They are spelled out as named fields so that the
    dollar figure printed before a run is something a reader can argue with,
    rather than one magic total.

    `snapshot_s` defaults just under 55s because that is the only hard signal
    that exists: the SDK's own default `timeout` for a snapshot is 55s, and
    changelog 1.4.3 added support for longer, which is direct evidence
    snapshots can exceed it.
    """

    create_to_ready_s: float = 15.0
    fill_project_s: float = 60.0
    snapshot_s: float = 50.0
    mount_s: float = 10.0
    verify_s: float = 5.0
    # The calibration exec that measures what `verify_s` costs before any tree
    # is mounted. Small, but it is billed like everything else, so it is priced
    # rather than absorbed silently into the floor.
    exec_calibration_s: float = 2.0
    # Between the last measured call and the container actually going away.
    # verified.md records that whether the meter stops at the `terminate()`
    # call or at confirmed teardown is undocumented, so this is padding for a
    # thing Modal has not published.
    teardown_slack_s: float = 10.0


# Modal's broad regions. Pinning to one of these costs 1.15x; pinning to a
# narrow region ("us-east") costs 1.75x and *worsens* cold start. Not pinning
# at all costs 1.0x and is what the harness does by default -- Modal's own
# advice is broad regions for availability and cold start, and a benchmark
# should measure the platform rather than a surcharge. The multiplier is here
# for the runs that do pass `--region`, where omitting it would understate the
# bill by 15% or 75%.
BROAD_REGIONS = frozenset({"us", "eu", "ap"})


def region_multiplier(region: str | None) -> float:
    """Modal's region-pinning surcharge, from `docs/verified.md` §22 item 5.

    Omitting it on a pinned run is a 15% or 75% underestimate, which is exactly
    the sort of quiet error a cost warning must not contain.
    """
    if not region:
        return 1.0
    return 1.15 if region.strip().lower() in BROAD_REGIONS else 1.75


@dataclass(frozen=True, slots=True)
class CostEstimate:
    iterations: int
    cpu_cores: float
    memory_mib: int
    usd_per_sandbox_hour: float
    region: str | None
    region_multiplier: float
    subject_sandbox_seconds: float
    restore_sandbox_seconds: float
    pool_sandbox_seconds: float
    total_sandbox_seconds: float
    total_usd: float
    wall_clock_seconds: float

    def to_json(self) -> dict[str, object]:
        return {
            "iterations": self.iterations,
            "cpu_cores": self.cpu_cores,
            "memory_mib": self.memory_mib,
            "usd_per_sandbox_hour": round(self.usd_per_sandbox_hour, 6),
            "region": self.region,
            "region_multiplier": self.region_multiplier,
            "subject_sandbox_seconds": self.subject_sandbox_seconds,
            "restore_sandbox_seconds": self.restore_sandbox_seconds,
            "pool_sandbox_seconds": self.pool_sandbox_seconds,
            "total_sandbox_seconds": self.total_sandbox_seconds,
            "estimated_total_usd": round(self.total_usd, 4),
            "estimated_wall_clock_seconds": self.wall_clock_seconds,
            "basis": (
                "Floor, not a bound. Modal does not document whether the meter starts at "
                "Sandbox.create or at container start, nor whether it stops at terminate() "
                "or at confirmed teardown; a retried create or a throttled call is extra."
            ),
        }


def estimate_cost(
    *,
    iterations: int,
    cpu_cores: float,
    memory_mib: int,
    region: str | None = None,
    model: CostModel | None = None,
    rate: SandboxRate | None = None,
) -> CostEstimate:
    """Dollars for one run of the resume-latency harness, before it is started.

    Three sandboxes are in play. Per iteration: a *subject* that is created,
    filled and snapshotted twice; a *restore* target booted from the filesystem
    snapshot; and one *pool* member, created once and alive for the whole run,
    which is what `mount_image` mounts into. The pool member is the expensive
    one precisely because a warm pool is, by construction, paying for idle time
    -- which is the fact SPEC §19's margin model needs.
    """
    model = model or CostModel()
    rate = rate or SandboxRate()

    subject_per_iteration = (
        model.create_to_ready_s
        + model.fill_project_s
        + 2 * model.snapshot_s  # snapshot_filesystem and snapshot_directory
        + model.teardown_slack_s
    )
    restore_per_iteration = model.create_to_ready_s + model.teardown_slack_s
    mount_per_iteration = model.mount_s + model.verify_s + model.exec_calibration_s

    # The harness is sequential by design: overlapping iterations would have
    # them contend for the same workspace rate limit and the same host, which
    # is a different measurement than the one being asked for.
    wall_clock = model.create_to_ready_s + iterations * (
        subject_per_iteration + restore_per_iteration + mount_per_iteration
    )

    subject_seconds = iterations * subject_per_iteration
    restore_seconds = iterations * restore_per_iteration
    pool_seconds = wall_clock + model.teardown_slack_s
    total_seconds = subject_seconds + restore_seconds + pool_seconds

    multiplier = region_multiplier(region)
    per_second = rate.per_second_usd(cpu_cores=cpu_cores, memory_mib=memory_mib) * multiplier
    return CostEstimate(
        iterations=iterations,
        cpu_cores=cpu_cores,
        memory_mib=memory_mib,
        usd_per_sandbox_hour=3600.0 * per_second,
        region=region,
        region_multiplier=multiplier,
        subject_sandbox_seconds=subject_seconds,
        restore_sandbox_seconds=restore_seconds,
        pool_sandbox_seconds=pool_seconds,
        total_sandbox_seconds=total_seconds,
        total_usd=total_seconds * per_second,
        wall_clock_seconds=wall_clock,
    )


# ------------------------------------------------------------------ renderers


# The width of the cost block's label column, which is `MODAL_MAX_THROTTLE_WAIT`
# plus nothing to spare. It is a constant with a function over it rather than
# hand-counted spaces on each line because hand-counted, that one row -- the
# only label longer than the rest -- sat a column out from every other value.
_LABEL_WIDTH = 23


def _row(label: str, value: object) -> str:
    """One aligned `label   value` line of the cost block."""
    return f"  {label:<{_LABEL_WIDTH}} {value}"


def render_cost_estimate(
    estimate: CostEstimate,
    *,
    sandbox_lifetime_s: int | None = None,
    lifetime_basis: str = "",
    max_throttle_wait_s: int | None = None,
) -> str:
    """The block printed before anything is spawned. Money first, then consent.

    The lifetime belongs in this block rather than after it: it is bounded by
    the wall clock two lines above, and a reader has to be able to see that the
    run fits inside the sandboxes it is about to pay for.

    `lifetime_basis` is a caption from the caller rather than a flag, because
    four different rules can decide that number -- the flag, the projection,
    the floor and Modal's ceiling -- and a caption that names the wrong one is
    worse than no caption. It read "derived from the wall clock above" on every
    run, including every run of `-n 5`, where the floor decides it.
    """
    minutes = estimate.wall_clock_seconds / 60
    settings = []
    if sandbox_lifetime_s is not None:
        basis = f"  ({lifetime_basis})" if lifetime_basis else ""
        settings.append(_row("sandbox lifetime", f"{sandbox_lifetime_s}s{basis}"))
    if max_throttle_wait_s is not None:
        settings.append(_row("MODAL_MAX_THROTTLE_WAIT", f"{max_throttle_wait_s}s"))
    return "\n".join(
        [
            "This benchmark spawns real Modal Sandboxes and costs real money.",
            "",
            _row("iterations", estimate.iterations),
            _row(
                "sandbox size",
                f"cpu={estimate.cpu_cores} physical core(s) "
                f"(~{estimate.cpu_cores * 2:g} vCPU equivalent), {estimate.memory_mib} MiB",
            ),
            _row(
                "region",
                f"{estimate.region or 'unpinned'}"
                + (
                    f"  (x{estimate.region_multiplier:g} region surcharge)"
                    if estimate.region
                    else "  (no region surcharge; snapshots reside in the US either way)"
                ),
            ),
            _row("rate", f"${estimate.usd_per_sandbox_hour:.4f} / sandbox-hour"),
            _row(
                "billed sandbox-seconds",
                f"~{estimate.total_sandbox_seconds:.0f}"
                f"  (subject {estimate.subject_sandbox_seconds:.0f}"
                f" + restore {estimate.restore_sandbox_seconds:.0f}"
                f" + warm pool {estimate.pool_sandbox_seconds:.0f})",
            ),
            _row("ESTIMATED COST", f"${estimate.total_usd:.2f}"),
            _row("estimated wall clock", f"~{minutes:.0f} min"),
            *settings,
            "",
            "That is a floor. Modal does not publish whether the meter starts at",
            "Sandbox.create or at container start, nor whether it stops at the",
            "terminate() call or at confirmed teardown.",
        ]
    )


def render_markdown(
    summaries: Sequence[PhaseSummary],
    *,
    meta: Mapping[str, object],
    percentiles: Sequence[int] = REPORTED_PERCENTILES,
) -> str:
    """A table meant to be pasted into `docs/verified.md` without reformatting."""
    head = ["phase", "n", *(f"p{p}" for p in percentiles), "min", "max"]
    rows = [
        [
            s.phase,
            str(s.n),
            *(
                f"{s.percentiles_s[p]:.2f}s" + ("†" if p in s.unresolved else "")
                for p in percentiles
            ),
            f"{s.min_s:.2f}s",
            f"{s.max_s:.2f}s",
        ]
        for s in summaries
    ]
    width = [max(len(r[i]) for r in [head, *rows]) for i in range(len(head))]
    lines = [
        "| " + " | ".join(c.ljust(w) for c, w in zip(head, width, strict=True)) + " |",
        "| " + " | ".join("-" * w for w in width) + " |",
    ]
    lines += [
        "| " + " | ".join(c.ljust(w) for c, w in zip(r, width, strict=True)) + " |" for r in rows
    ]

    footnotes = sorted({p for s in summaries for p in s.unresolved})
    if footnotes:
        needed = ", ".join(f"p{p} needs n>={samples_to_resolve(p)}" for p in footnotes)
        lines += [
            "",
            f"† At this N the marked percentile *is* the maximum sample ({needed}). "
            "Do not read it as a tail estimate.",
        ]

    lines += ["", "Run metadata:", ""]
    lines += [f"- `{k}`: {json.dumps(v) if not isinstance(v, str) else v}" for k, v in meta.items()]
    return "\n".join(lines)
