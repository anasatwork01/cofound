"""The half of the Modal benchmark that must be right before it is ever run.

The valuable assertions here are the ones that catch a *plausible wrong
answer*, because that is this harness's failure mode -- it produces a number
that gets pasted into `docs/verified.md` and becomes SPEC §17's SLO, and a
percentile off by one rank looks exactly like a correct one. So:

- the percentile rule is pinned against hand-computed ranks, including the
  floating-point spelling that silently reports `max` as p95;
- the cost estimator is pinned against the $0.238/hour figure in
  `docs/verified.md`, so a mistyped rate constant fails here;
- the end-to-end figure is proved to be a percentile of per-iteration sums
  rather than a sum of per-phase percentiles;
- the `Sandbox.create` keyword arguments are asserted to carry no credential
  and no reapable timeout, because Modal enforces neither;
- and importing the harness is proved not to import `modal`, because the whole
  gating design rests on that.

Everything below runs with no Modal account, no credentials and no `modal`
installed. `make verify` must stay green on such a machine, so nothing here may
depend on the optional `bench` dependency group.
"""

from __future__ import annotations

import importlib
import io
import json
import math
import os
import re
import subprocess
import sys
import threading
import time
from collections.abc import Callable

import pytest
from halyard_sandboxd.bench import resume_latency as rl
from halyard_sandboxd.bench import stats

# ----------------------------------------------------------------- percentiles


def test_the_float_spelling_of_the_rank_really_does_diverge():
    """The bug the integer arithmetic removes -- stated accurately.

    It is tempting to claim `math.ceil(95 / 100 * 20)` is wrong. It is not:
    0.95 * 20 happens to round to exactly 19.0, and checked exhaustively the
    float spelling agrees with the integer one for p50/p95/p99 at every
    n <= 20000. The divergence is real but lives elsewhere -- p7 at n=100
    gives 7.000000000000001 and so rank 8 where rank 7 was meant.

    Asserted rather than commented for two reasons: so the docstring's claim
    cannot quietly become false on a different platform, and so that anyone
    who widens REPORTED_PERCENTILES is not relying on luck.
    """
    assert math.ceil(7 / 100 * 100) == 8  # the float spelling, off by a rank
    assert stats.nearest_rank_index(7, 100) == 6  # rank 7 of 100, zero-based

    for percentile in (50, 95, 99):
        for n in (5, 19, 20, 21, 99, 100, 101, 1000):
            assert stats.nearest_rank_index(percentile, n) == (
                min(max(math.ceil(percentile * n / 100) - 1, 0), n - 1)
            )


@pytest.mark.parametrize(
    ("percentile", "n", "expected"),
    [
        (0, 10, 0),  # clamped up from rank 0
        (50, 1, 0),
        (100, 1, 0),
        (50, 2, 0),  # ceil(1.0) -> rank 1 -> index 0: the lower of two
        (100, 2, 1),
        (50, 4, 1),
        (50, 5, 2),
        (95, 5, 4),  # only five samples: p95 IS the maximum
        (99, 100, 98),  # a hundred samples: p99 is finally distinct
        (100, 100, 99),
    ],
)
def test_nearest_rank_indices_are_hand_checked(percentile, n, expected):
    assert stats.nearest_rank_index(percentile, n) == expected


def test_a_percentile_is_always_an_observation_that_happened():
    samples = [0.5, 9.0, 1.25, 3.0, 2.0]
    for p in (0, 25, 50, 75, 95, 100):
        assert stats.quantile(samples, p) in samples


def test_zero_samples_is_an_error_not_a_zero():
    """A phase that never completed must not summarise as 0.00s."""
    with pytest.raises(ValueError, match="zero samples"):
        stats.nearest_rank_index(50, 0)
    with pytest.raises(ValueError, match="no samples"):
        stats.summarize("snapshot_filesystem", [])


def test_a_percentile_outside_0_to_100_is_an_error():
    with pytest.raises(ValueError, match=r"within 0\.\.100"):
        stats.nearest_rank_index(101, 10)


def test_small_runs_are_told_their_tail_percentiles_are_just_the_maximum():
    summary = stats.summarize("cold_create_to_ready", [1.0, 2.0, 3.0, 4.0, 5.0])
    assert summary.percentiles_s[50] == 3.0
    assert summary.percentiles_s[95] == 5.0 == summary.max_s
    assert summary.unresolved == (95, 99)


def test_a_long_enough_run_resolves_p95_and_says_how_long_that_is():
    summary = stats.summarize("cold_create_to_ready", [float(i) for i in range(1, 21)])
    assert summary.unresolved == (99,)
    assert stats.samples_to_resolve(95) == 20
    assert stats.samples_to_resolve(99) == 100


def test_summarize_does_not_require_sorted_input():
    unsorted = stats.summarize("x", [5.0, 1.0, 3.0, 2.0, 4.0])
    ordered = stats.summarize("x", [1.0, 2.0, 3.0, 4.0, 5.0])
    assert unsorted == ordered
    assert unsorted.min_s == 1.0
    assert unsorted.max_s == 5.0
    assert unsorted.mean_s == 3.0


# ---------------------------------------------------------- end-to-end totals


def test_end_to_end_percentile_comes_from_per_iteration_totals():
    """Not from adding the phases' own percentiles, which is biased high.

    Here the slow mount and the slow read happen on *different* iterations, so
    no iteration was ever as slow as p95(mount) + p95(read). Adding the
    percentiles would invent a 20s resume that never occurred.
    """
    mount = {1: 1.0, 2: 1.0, 3: 1.0, 4: 10.0}
    read = {1: 10.0, 2: 1.0, 3: 1.0, 4: 1.0}
    totals = stats.keyed_total(mount, read)
    assert totals == [11.0, 2.0, 2.0, 11.0]

    combined = stats.summarize("restore_by_mount_total", totals)
    naive = stats.quantile(list(mount.values()), 95) + stats.quantile(list(read.values()), 95)
    assert naive == 20.0
    assert combined.percentiles_s[95] == 11.0
    assert combined.percentiles_s[95] < naive


def test_a_partial_iteration_cannot_be_paired_with_a_later_one():
    """A run that died after mounting must not add iteration 4's read to it."""
    assert stats.keyed_total({1: 1.0, 2: 2.0, 3: 3.0}, {1: 10.0, 2: 20.0}) == [11.0, 22.0]
    assert stats.keyed_total() == []


def test_a_read_that_failed_mid_run_does_not_shift_every_later_pairing():
    """The bug positional zipping hid, and the reason the join key exists.

    Iteration 3's read failed, so `restore_by_mount` has five samples and
    `restore_by_mount_read` has four. Zipping by position pairs mount(i3) with
    read(i4) and mount(i4) with read(i5), and drops i5's mount -- four totals,
    none of which any iteration produced. Joining on the iteration number
    gives four totals that all happened, and loses only iteration 3.

    `restore_by_mount_total` is the figure destined for SPEC §17's SLO row, so
    a plausible wrong number here is the expensive kind.
    """
    mount = {1: 1.0, 2: 2.0, 3: 30.0, 4: 4.0, 5: 5.0}
    read = {1: 0.1, 2: 0.2, 4: 0.4, 5: 0.5}  # iteration 3's read failed

    positional = [m + r for m, r in zip(list(mount.values()), list(read.values()), strict=False)]
    assert positional == [1.1, 2.2, 30.4, 4.5]  # i3's mount + i4's read: 30.4s, invented

    joined = stats.keyed_total(mount, read)
    assert joined == [1.1, 2.2, 4.4, 5.5]
    assert 30.4 not in joined
    assert len(joined) == 4  # iteration 3 dropped whole, not half-counted


# ------------------------------------------------------------------------ cost


def test_the_sandbox_rate_reproduces_the_verified_238_dollars_per_hour():
    """docs/verified.md §22 item 5: a 2 vCPU / 4 GB sandbox is $0.238/hr.

    That is the arithmetic a mistyped rate constant would break, and the whole
    pre-run cost warning depends on it.
    """
    rate = stats.SandboxRate()
    assert rate.per_hour_usd(cpu_cores=1.0, memory_mib=4096) == pytest.approx(0.238, abs=0.001)


def test_asking_for_cpu_2_costs_about_1_6x_which_is_why_cpu_is_1_0():
    """Modal's unit is a physical core -- "2 vCPU equivalent" on its own pricing page.

    So SPEC's "2 vCPU / 4 GB" is cpu=1.0. Writing cpu=2.0 buys four vCPU
    equivalent and inflates the bill by this ratio, which is the specific
    mistake `BenchConfig.cpu_cores` documents.
    """
    rate = stats.SandboxRate()
    right = rate.per_hour_usd(cpu_cores=1.0, memory_mib=4096)
    wrong = rate.per_hour_usd(cpu_cores=2.0, memory_mib=4096)
    assert wrong / right == pytest.approx(1.6, abs=0.05)


def test_the_default_bench_config_is_the_spec_sandbox_size():
    cfg = rl.BenchConfig()
    assert (cfg.cpu_cores, cfg.memory_mib) == (1.0, 4096)


def test_the_cost_estimate_scales_with_iterations_and_is_never_free():
    five = stats.estimate_cost(iterations=5, cpu_cores=1.0, memory_mib=4096)
    twenty = stats.estimate_cost(iterations=20, cpu_cores=1.0, memory_mib=4096)
    assert five.total_usd > 0
    assert twenty.total_usd > 3 * five.total_usd
    # The warm-pool member is alive for the whole run, so it is the largest
    # single line. That is the fact SPEC §19's margin model needs.
    assert twenty.pool_sandbox_seconds > twenty.subject_sandbox_seconds


def test_the_estimate_is_derived_from_the_named_budgets_not_a_magic_total():
    model = stats.CostModel(
        create_to_ready_s=1.0,
        fill_project_s=1.0,
        snapshot_s=1.0,
        mount_s=1.0,
        verify_s=1.0,
        exec_calibration_s=1.0,
        teardown_slack_s=1.0,
    )
    estimate = stats.estimate_cost(iterations=1, cpu_cores=1.0, memory_mib=4096, model=model)
    # subject: ready + fill + two snapshots + slack = 5; restore: ready + slack = 2;
    # mount + verify + calibration = 3; wall clock = pool's own ready + 10 = 11.
    assert estimate.subject_sandbox_seconds == 5.0
    assert estimate.restore_sandbox_seconds == 2.0
    assert estimate.wall_clock_seconds == 11.0
    assert estimate.pool_sandbox_seconds == 12.0
    assert estimate.total_sandbox_seconds == 19.0


def test_the_cost_block_names_the_money_and_admits_it_is_a_floor():
    text = stats.render_cost_estimate(
        stats.estimate_cost(iterations=5, cpu_cores=1.0, memory_mib=4096)
    )
    assert "costs real money" in text
    assert "ESTIMATED COST" in text
    assert "floor" in text


# -------------------------------------------------------------- the safety rails


def test_create_never_carries_a_credential():
    """CLAUDE.md's rule, enforced here because Modal will not enforce it.

    Modal Secrets are injected as ordinary environment variables -- readable
    from os.environ, printenv or /proc/self/environ, with no masking or
    sealing. So the only control is not passing them, and the only way that
    stays true is a test.
    """
    kwargs = rl.create_kwargs(rl.BenchConfig())
    assert "secrets" not in kwargs
    assert "env" not in kwargs
    assert not any("token" in k or "secret" in k for k in kwargs)


def test_create_always_passes_an_explicit_timeout():
    """The default is 300s, which would kill the pool member mid-run."""
    kwargs = rl.create_kwargs(rl.BenchConfig())
    assert kwargs["timeout"] > 300
    assert kwargs["timeout"] == rl.resolved_sandbox_timeout_s(rl.BenchConfig())

    pinned = rl.create_kwargs(rl.BenchConfig(sandbox_timeout_s=1234))
    assert pinned["timeout"] == 1234  # an explicit flag still wins


def test_no_idle_timeout_can_reap_a_sandbox_mid_measurement():
    """Modal's definition of activity does not include "a process is running".

    Only an exec, a stdin write or an open tunnel connection counts, and
    idle_timeout has no pre-termination hook -- so a reap mid-run would both
    lose the measurement and lose the snapshot.
    """
    assert rl.create_kwargs(rl.BenchConfig())["idle_timeout"] is None


def test_both_outbound_allowlists_are_always_concrete_lists():
    """Passing one and omitting the other silently kills all non-443 egress.

    modal/sandbox.py:265 treats *both* None as OPEN but an empty list as an
    allowlist with nothing on it, so the two parameters are not independent.
    Every create in this harness passes both.
    """
    kwargs = rl.create_kwargs(rl.BenchConfig())
    assert kwargs["outbound_cidr_allowlist"] == []
    assert kwargs["outbound_domain_allowlist"] == []

    opened = rl.create_kwargs(rl.BenchConfig(outbound_domain_allowlist=("registry.npmjs.org",)))
    assert opened["outbound_domain_allowlist"] == ["registry.npmjs.org"]
    assert opened["outbound_cidr_allowlist"] == []  # still present, not dropped


def test_resources_are_hard_capped_by_default_and_burstable_only_on_request():
    """A bare scalar is a *request*: it permits bursting to request + 16 cores, billed."""
    capped = rl.create_kwargs(rl.BenchConfig())
    assert capped["cpu"] == (1.0, 1.0)
    assert capped["memory"] == (4096, 4096)

    burstable = rl.create_kwargs(rl.BenchConfig(hard_cap_resources=False))
    assert burstable["cpu"] == 1.0
    assert burstable["memory"] == 4096


def test_the_snapshot_ttl_is_hours_not_the_thirty_day_default():
    """Modal cannot list the images you created, so a forgotten one is unfindable."""
    cfg = rl.BenchConfig()
    assert 0 < cfg.snapshot_ttl_s <= 24 * 3600
    assert cfg.snapshot_ttl_s < 30 * 24 * 3600


def test_the_snapshot_timeout_is_above_the_55_second_default():
    """1.4.3 added support for longer than 55s, which is evidence snapshots exceed it.

    A harness that kept the default would truncate the very tail it exists to
    measure and report a TimeoutError as though it were the answer.
    """
    assert rl.BenchConfig().snapshot_timeout_s > 55


# ------------------------------------------------------------------- the gating


def test_importing_the_harness_does_not_import_modal():
    """The whole gate design rests on this.

    If the import were eager, `make verify` would fail at collection on every
    machine without the optional group -- which is all of them -- and the
    failure would look like a broken test rather than a missing dependency.

    Asserted in a subprocess rather than against this process's `sys.modules`.
    On a machine where `uv sync --group bench` HAS run, modal is importable,
    and any earlier test that exercised the gates will have imported it -- so
    an in-process assertion would pass or fail on test ordering rather than on
    the property being claimed.
    """
    assert "halyard_sandboxd.bench.resume_latency" in sys.modules
    probe = (
        "import sys;"
        "import halyard_sandboxd.bench.resume_latency as rl;"
        "assert rl.BenchConfig().iterations == 5;"
        "print('modal' in sys.modules)"
    )
    result = subprocess.run(
        [sys.executable, "-c", probe], capture_output=True, text=True, check=True
    )
    assert result.stdout.strip() == "False"


def test_credentials_are_decided_by_credentials_and_never_by_ci(tmp_path):
    """Task 0.6's lesson: key the gate on the capability, not the environment.

    A check keyed on `CI` skipped where it mattered and broke the job where it
    did not. So `CI=true` must make no difference either way here.
    """
    have, why = rl.modal_credentials_present(
        {"MODAL_TOKEN_ID": "ak-x", "MODAL_TOKEN_SECRET": "as-y"}, home=tmp_path
    )
    assert have and "MODAL_TOKEN_ID" in why

    have, why = rl.modal_credentials_present({"CI": "true"}, home=tmp_path)
    assert not have and "no Modal credentials" in why

    have, _ = rl.modal_credentials_present({"CI": ""}, home=tmp_path)
    assert not have


def test_half_a_credential_pair_is_not_a_credential(tmp_path):
    for environ in ({"MODAL_TOKEN_ID": "ak-x"}, {"MODAL_TOKEN_SECRET": "as-y"}):
        have, _ = rl.modal_credentials_present(environ, home=tmp_path)
        assert not have


def test_a_profile_file_counts_as_a_credential(tmp_path):
    (tmp_path / ".modal.toml").write_text("[default]\n")
    have, why = rl.modal_credentials_present({}, home=tmp_path)
    assert have and ".modal.toml" in why

    override = tmp_path / "elsewhere.toml"
    override.write_text("[default]\n")
    have, why = rl.modal_credentials_present({"MODAL_CONFIG_PATH": str(override)}, home=tmp_path)
    assert have and "elsewhere.toml" in why


def test_credentials_alone_are_not_permission_to_spend():
    """The spend opt-in is deliberately separate from having an account."""
    reasons = rl.blockers(
        rl.BenchConfig(),
        environ={},
        credentials=(True, "tokens"),
        import_error=None,
    )
    assert len(reasons) == 1
    assert rl.SPEND_ENV in reasons[0]


def test_every_gate_is_reported_at_once_not_one_at_a_time():
    reasons = rl.blockers(
        rl.BenchConfig(),
        environ={},
        credentials=(False, "no Modal credentials: ..."),
        import_error="modal is not importable",
    )
    assert len(reasons) == 3


def test_all_gates_satisfied_means_no_blockers():
    reasons = rl.blockers(
        rl.BenchConfig(),
        environ={rl.SPEND_ENV: "1"},
        credentials=(True, "tokens"),
        import_error=None,
    )
    assert reasons == ()


def test_a_setup_command_with_no_egress_is_refused_rather_than_left_to_hang():
    reasons = rl.blockers(
        rl.BenchConfig(project_setup_cmd="npm ci"),
        environ={rl.SPEND_ENV: "1"},
        credentials=(True, "tokens"),
        import_error=None,
    )
    assert any("needs egress" in r for r in reasons)

    allowed = rl.blockers(
        rl.BenchConfig(
            project_setup_cmd="npm ci", outbound_domain_allowlist=("registry.npmjs.org",)
        ),
        environ={rl.SPEND_ENV: "1"},
        credentials=(True, "tokens"),
        import_error=None,
    )
    assert allowed == ()


def test_a_nonsense_iteration_count_is_refused():
    reasons = rl.blockers(
        rl.BenchConfig(iterations=0),
        environ={rl.SPEND_ENV: "1"},
        credentials=(True, "tokens"),
        import_error=None,
    )
    assert any("at least 1" in r for r in reasons)


def test_the_default_invocation_prints_the_cost_and_creates_nothing(capsys):
    """No --run: this must be safe to type on a machine with live credentials.

    No `"modal" not in sys.modules` here. That assertion was order-dependent in
    exactly the way its sibling test's docstring explains: the gate tests
    deliberately import modal to decide whether a run *could* happen, so on any
    machine where `uv sync --all-packages --group bench` has run, this passed or
    failed on which node id ran first. The property it was reaching for is
    asserted properly in a subprocess by
    `test_importing_the_harness_does_not_import_modal`, and the property that
    actually matters -- nothing is *created* -- by
    `test_a_refused_run_never_reaches_sandbox_create` against a client that
    records being called.
    """
    assert rl.main([]) == 0
    out = capsys.readouterr().out
    assert "ESTIMATED COST" in out
    assert "Dry run: nothing was created" in out


def test_run_without_the_opt_in_refuses_and_exits_nonzero(capsys, monkeypatch):
    monkeypatch.delenv(rl.SPEND_ENV, raising=False)
    monkeypatch.setenv("MODAL_TOKEN_ID", "ak-fake")
    monkeypatch.setenv("MODAL_TOKEN_SECRET", "as-fake")
    assert rl.main(["--run", "-n", "1"]) == 2
    out = capsys.readouterr().out
    assert "Refusing to run:" in out
    assert rl.SPEND_ENV in out
    # `blockers` imports modal to decide whether it *could* run, so on a
    # machine with the bench group installed modal is in sys.modules by now.
    # Importing the client creates nothing; the invariant that matters is that
    # `Sandbox.create` was never reached, and that is asserted against a stub
    # client in `test_a_refused_run_never_reaches_sandbox_create`.


def test_the_spend_opt_in_must_be_exactly_one(monkeypatch):
    monkeypatch.setenv("MODAL_TOKEN_ID", "ak-fake")
    monkeypatch.setenv("MODAL_TOKEN_SECRET", "as-fake")
    for value in ("", "0", "yes", "true", "TRUE"):
        monkeypatch.setenv(rl.SPEND_ENV, value)
        assert rl.main(["--run", "-n", "1"]) == 2


# ----------------------------------------------------------- backend attribution


@pytest.mark.parametrize(
    ("sandbox_id", "expected"),
    [
        ("sb-" + "A" * 22, "v1"),
        ("sb-" + "a1B2c3D4e5F6g7H8i9J0k1", "v1"),
        ("sb-" + "A" * 21, "v2"),  # a sandbox id, but not the v1 shape
        ("sb-" + "A" * 23, "v2"),
        ("sb-" + "A" * 21 + "-", "unknown"),  # non-alphanumeric: neither shape
        ("sb-", "unknown"),  # no suffix at all
        ("sandbox-abc", "unknown"),  # not a sandbox id: the type prefix is "sb"
        ("im-abc123", "unknown"),  # an image id, which is a real way to get here
        ("", "unknown"),
    ],
)
def test_the_backend_is_recorded_from_the_id_not_assumed_from_the_request(sandbox_id, expected):
    """The client silently falls back to V1, and the two snapshot paths differ.

    `Sandbox.create` routes to V2 only when MODAL_SANDBOX_V2 is set AND no GPU,
    network file system or pty_info was requested. Asking for V2 is therefore
    not evidence of having got it, and numbers are not transferable between
    the backends -- so the report records what was observed.
    """
    assert rl.observed_backend(sandbox_id) == expected


def test_an_id_of_no_known_shape_is_unknown_rather_than_confidently_v2():
    """The client's rule is one-sided by design; copying it would mislabel.

    `modal/sandbox.py:218-222` routes anything that is not positively v1 to
    v2, so that ids minted in a newer format keep working. Good for routing,
    wrong for attribution: a v1 id whose format shifted would be reported as
    v2 with no hedge, and v1 and v2 take different snapshot code paths. There
    is no published positive v2 sandbox-id shape, so the third answer is
    `unknown`.
    """
    assert rl.observed_backend("sb-" + "A" * 22) == "v1"
    assert rl.observed_backend("task-abc") == "unknown"
    assert rl.observed_backend("sb-not_valid_base62!") == "unknown"


class _IdOnly:
    def __init__(self, object_id):
        self.object_id = object_id


class _ClientAnswered(_IdOnly):
    def __init__(self, object_id, is_v2):
        super().__init__(object_id)
        self._is_v2 = is_v2


def test_the_backend_prefers_the_clients_own_answer_and_reports_a_disagreement():
    """`_is_v2` is the client's resolved decision; the id shape is the fallback."""
    backend, how = rl.attribute_backend(_IdOnly("sb-" + "A" * 22))
    assert backend == "v1"
    assert "id shape" in how

    backend, how = rl.attribute_backend(_ClientAnswered("sb-" + "A" * 30, is_v2=True))
    assert backend == "v2"
    assert "_is_v2" in how
    assert "says" not in how  # the two agree, so nothing to warn about

    # The case worth recording: the client says v1, the id shape says v2.
    backend, how = rl.attribute_backend(_ClientAnswered("sb-" + "A" * 30, is_v2=False))
    assert backend == "v1"
    assert "_is_v2" in how and "the id shape says v2" in how


# ------------------------------------------------------------------ the artifact


def _finished_run():
    run = rl.Run()
    for i in range(1, 6):
        run.record("cold_create_to_ready", 1.0 + i, iteration=i)
        run.record("snapshot_filesystem", 10.0 + i, iteration=i)
        run.record("snapshot_directory", 5.0 + i, iteration=i)
        run.record("restore_by_boot_to_ready", 2.0 + i, iteration=i)
        run.record("restore_by_mount", 0.1 * i, iteration=i)
        run.record("restore_by_mount_read", 1.0 * i, iteration=i)
        run.record("exec_spawn_overhead", 0.4, iteration=i)
        run.workloads.append(
            {
                "iteration": i,
                "path": "/project",
                "files": 20000,
                "bytes": 81920000,
                "fill": "random",
            }
        )
    run.iterations_completed = 5
    run.meta["backend_observed"] = "v2"
    run.meta["backend_observed_from"] = "modal.Sandbox._is_v2 (private)"
    return run


def test_the_report_keeps_the_raw_samples_so_it_can_be_diffed_across_sdk_bumps():
    cfg = rl.BenchConfig(iterations=5)
    document = rl.report(
        cfg,
        _finished_run(),
        rl.Ledger(),
        stats.estimate_cost(iterations=5, cpu_cores=1.0, memory_mib=4096),
    )
    assert document["schema"] == rl.REPORT_SCHEMA
    assert document["modal_client_pin"] == "1.5.5"
    assert len(document["samples_s"]["snapshot_filesystem"]) == 5
    phases = [p["phase"] for p in document["phases"]]
    assert "restore_by_mount_total" in phases
    assert phases.index("cold_create_to_ready") < phases.index("snapshot_filesystem")


def test_a_run_that_measured_nothing_reports_nothing_rather_than_zeros():
    empty = rl.Run()
    document = rl.report(
        rl.BenchConfig(),
        empty,
        rl.Ledger(),
        stats.estimate_cost(iterations=5, cpu_cores=1.0, memory_mib=4096),
    )
    assert document["phases"] == []
    assert "nothing to report" in rl.render_human(rl.BenchConfig(), empty, document)


def test_the_human_summary_is_a_markdown_table_that_flags_its_own_weak_tail():
    cfg = rl.BenchConfig(iterations=5)
    run = _finished_run()
    document = rl.report(
        cfg, run, rl.Ledger(), stats.estimate_cost(iterations=5, cpu_cores=1.0, memory_mib=4096)
    )
    text = rl.render_human(cfg, run, document)
    assert "| phase" in text and "| p50" in text
    assert "restore_by_mount_total" in text
    # Five iterations cannot resolve p95, and the artifact has to say so rather
    # than let a reader quote it as a tail.
    assert "†" in text
    assert "p95 does not become a distinct observation until n>=20" in text
    assert "v2 observed" in text


# `modal.experimental.image_delete` is the only deletion API Modal exposes, and
# the harness has to *import the submodule* to reach it. These tests therefore
# build a real importable package on disk whose `__init__` does NOT expose
# `experimental` -- because that is what the real `modal` does, and a stub that
# handed `cleanup` an object already carrying the attribute is exactly how a
# deletion path that had never executed once came to look tested.


@pytest.fixture
def modal_shaped_package(tmp_path, monkeypatch):
    """A package shaped like `modal`: `experimental` present but not imported.

    Verified against a real modal 1.5.5 install: `modal/__init__.py` imports
    `billing` and `types` from the package and nothing else, so after
    `import modal`, `hasattr(modal, "experimental")` is False.
    """
    created = []

    def build(name, *, deleted):
        package = tmp_path / name
        package.mkdir()
        (package / "__init__.py").write_text(
            "# Mirrors modal/__init__.py: does NOT import .experimental.\n"
        )
        (package / "experimental.py").write_text(
            "deleted = []\n"
            "def image_delete(image_id):\n"
            "    if image_id == 'im-bad':\n"
            "        raise RuntimeError('nope')\n"
            "    deleted.append(image_id)\n"
        )
        monkeypatch.syspath_prepend(str(tmp_path))
        module = importlib.import_module(name)
        created.append(name)
        # The precondition this whole fix is about. If this ever passes
        # trivially, the stub has stopped resembling the real module.
        assert not hasattr(module, "experimental")
        return module

    try:
        yield build
    finally:
        for name in created:
            sys.modules.pop(f"{name}.experimental", None)
            sys.modules.pop(name, None)


def test_cleanup_performs_the_submodule_import_that_import_modal_does_not(modal_shaped_package):
    """The defect: `getattr(modal, "experimental", None)` was always None.

    `import modal` does not bind `modal.experimental`, so the old chain
    resolved to None on every run, every image took the "it is gone" branch,
    and `image_delete` was never called once -- leaking 2N whole-root-filesystem
    snapshots per run that Modal offers no API to list.

    Proved the way it was disproved: the module handed in has no
    `experimental` attribute (asserted by the fixture), so the only way a
    deletion can happen is if `cleanup` imported the submodule itself.
    """
    modal = modal_shaped_package("stub_modal_a", deleted=[])
    ledger = rl.Ledger(images=["im-a", "im-b"])

    rl.cleanup(modal, ledger, out=sys.stdout)

    assert modal.experimental.deleted == ["im-a", "im-b"]  # the import happened
    assert ledger.deleted == ["im-a", "im-b"]
    assert ledger.leaked == []


def test_cleanup_deletes_every_image_even_when_one_delete_fails(capsys, modal_shaped_package):
    modal = modal_shaped_package("stub_modal_b", deleted=[])
    ledger = rl.Ledger(images=["im-a", "im-bad", "im-b"])

    rl.cleanup(modal, ledger, out=sys.stdout)

    assert modal.experimental.deleted == ["im-a", "im-b"]
    assert ledger.deleted == ["im-a", "im-b"]
    assert any("im-bad" in item for item in ledger.leaked)
    assert "im-bad" in capsys.readouterr().out


def test_a_leaked_snapshot_id_is_reported_in_full_because_modal_cannot_list_them(capsys):
    """The unlistable-cost path. Cleanup failing quietly is the expensive bug.

    Here the deletion API really is unreachable, and the leak line has to say
    *why* -- a human holding the id can still delete it, a human without it
    cannot.
    """

    class _Stubborn:
        def terminate(self):
            raise RuntimeError("gone")

    class _NoSuchModule:
        # A module object whose name does not resolve, so the submodule import
        # genuinely fails. Named rather than left to default to "modal", which
        # on a machine with the bench group installed would import the real one.
        __name__ = "stub_modal_that_is_not_installed"

    ledger = rl.Ledger(sandboxes=[("subject-1", _Stubborn())], images=["im-abc123"])
    rl.cleanup(_NoSuchModule(), ledger, out=sys.stdout)

    out = capsys.readouterr().out
    assert "LEAKED" in out
    assert "im-abc123" in out
    assert "subject-1" in out
    assert "stub_modal_that_is_not_installed.experimental failed" in out
    assert ledger.deleted == []


# ------------------------------------------------------- the programs sent remotely


def _run_remote(script: str, *args: str) -> subprocess.CompletedProcess[str]:
    """Run one of the harness's remote programs locally, the way the sandbox will."""
    return subprocess.run(
        [sys.executable, "-c", script, *args], capture_output=True, text=True, check=False
    )


@pytest.mark.parametrize("name", ["_FILL", "_MEASURE", "_READ_ALL"])
def test_the_remote_programs_compile(name):
    """They are strings until a sandbox runs them, so nothing else checks them.

    A typo in one would surface for the first time thirty seconds into a run
    that is already being billed, after a sandbox had been created for it.
    """
    compile(getattr(rl, name), f"<{name}>", "exec")


def test_the_workload_programs_round_trip_before_they_ever_cost_money(tmp_path):
    """Fill, measure, read -- the three steps the measurement is built on.

    Exercised end to end locally because the alternative is discovering that
    the read step miscounts, or that the fill step writes nothing, from a
    paid run whose numbers then mean something other than what they say.
    """
    project = tmp_path / "project"
    assert _run_remote(rl._FILL, str(project), "600", "4", "random").returncode == 0

    measured = json.loads(_run_remote(rl._MEASURE, str(project)).stdout)
    assert measured == {"files": 600, "bytes": 600 * 4 * 1024}

    read = _run_remote(rl._READ_ALL, str(project), str(measured["files"]))
    assert read.returncode == 0
    assert json.loads(read.stdout)["bytes_read"] == measured["bytes"]


def test_a_short_mounted_tree_fails_loudly_rather_than_reporting_a_fast_mount(tmp_path):
    """The dangerous silent failure: a mount that delivered fewer files than it should.

    That would read as a *fast* restore, which is exactly the wrong direction
    for a number that becomes an SLO.
    """
    project = tmp_path / "project"
    _run_remote(rl._FILL, str(project), "8", "1", "random")
    short = _run_remote(rl._READ_ALL, str(project), "999")
    assert short.returncode != 0
    assert "expected 999" in short.stderr


def test_the_default_fill_is_incompressible_because_zeros_would_flatter_the_snapshot(tmp_path):
    """A tree of identical zero blocks lets a content-addressed or compressing
    snapshot store dedupe the payload away, producing a latency no real
    `node_modules` would reproduce. So `random` is the default, and the
    difference is asserted rather than trusted.
    """
    assert rl.BenchConfig().project_fill == "random"

    zeros = tmp_path / "zeros"
    _run_remote(rl._FILL, str(zeros), "4", "1", "zero")
    assert len({f.read_bytes() for f in zeros.rglob("f*")}) == 1

    noise = tmp_path / "noise"
    _run_remote(rl._FILL, str(noise), "4", "1", "random")
    assert len({f.read_bytes() for f in noise.rglob("f*")}) == 4


def test_pinning_a_region_is_priced_because_forgetting_it_would_understate_the_bill():
    """docs/verified.md §22 item 5: broad regions cost 1.15x, narrow ones 1.75x."""
    assert stats.region_multiplier(None) == 1.0
    assert stats.region_multiplier("us") == 1.15
    assert stats.region_multiplier("EU") == 1.15
    assert stats.region_multiplier("us-east") == 1.75  # narrow: also worsens cold start

    unpinned = stats.estimate_cost(iterations=5, cpu_cores=1.0, memory_mib=4096)
    broad = stats.estimate_cost(iterations=5, cpu_cores=1.0, memory_mib=4096, region="us")
    assert broad.total_usd == pytest.approx(unpinned.total_usd * 1.15)
    assert broad.total_sandbox_seconds == unpinned.total_sandbox_seconds  # price, not time


def test_the_region_is_unpinned_by_default_so_the_run_measures_modal_not_a_surcharge():
    """docs/verified.md §22 item 5, as corrected.

    Pinning costs 1.15x broad / 1.75x narrow and Modal advises broad regions
    for availability and cold start, so the default pins nothing. Snapshots do
    not constrain this: the "cannot pin a region" limit belongs to *memory*
    snapshots, and the only thing that applies to filesystem and directory
    snapshots either way is that they are stored in the United States.
    """
    cfg = rl.BenchConfig()
    assert cfg.region is None
    assert rl.create_kwargs(cfg)["region"] is None
    assert stats.region_multiplier(cfg.region) == 1.0

    # ...and `--region` still measures the pinned variant, at its real price.
    pinned = rl.config_from_args(rl._parser().parse_args(["--region", "us"]))
    assert pinned.region == "us"
    assert rl.estimate_for(pinned).region_multiplier == 1.15


def test_the_default_config_says_it_is_unpinned_and_where_snapshots_live(capsys):
    assert rl.main([]) == 0
    out = capsys.readouterr().out
    assert "region                  unpinned" in out
    assert "no region surcharge; snapshots reside in the US either way" in out


def test_every_row_of_the_cost_block_lines_up_including_the_longest_label():
    """`MODAL_MAX_THROTTLE_WAIT` is longer than every other label and used to
    sit one column out from all of them, in the block whose whole job is to be
    read carefully before money is spent."""
    text = stats.render_cost_estimate(
        stats.estimate_cost(iterations=5, cpu_cores=1.0, memory_mib=4096),
        sandbox_lifetime_s=1800,
        lifetime_basis="the floor",
        max_throttle_wait_s=120,
    )
    rows = [line for line in text.splitlines() if line.startswith("  ")]
    assert len(rows) == 9

    # Every row is two spaces, a label padded to one width, one space, then the
    # value -- so every value begins in the same column, the long label
    # included. Asserted as a column rather than as a literal string so the
    # next row added to the block has to line up too.
    column = 2 + stats._LABEL_WIDTH + 1
    for row in rows:
        assert row[column - 1] == " ", row
        assert row[column] != " ", row
    assert any(row.endswith("MODAL_MAX_THROTTLE_WAIT 120s") for row in rows), rows


# ------------------------------------------------------- the measurement sequence
#
# `measure`, `_iteration`, `_create_ready`, `_snapshot`, `_Deadline` and `_exec`
# had no tests at all: the suite covered only the pure halves, and two of the
# seven defects an adversarial review found lived in exactly that gap. A stub
# module is enough to close it, with one rule -- **the stub carries nothing the
# real module lacks**. Must-fix 1 existed because a stub that handed `cleanup`
# an object already holding `experimental` manufactured confidence in a code
# path that had never executed.


# How long the stub blocks when a test wants a call to overrun a deadline that
# `_shorten_deadline` has cut to 0.05s. Long enough that the deadline fires
# first on a loaded machine, short enough that the suite stays fast.
_STUB_SLOW_S = 0.3


class _Stream:
    def __init__(self, text: str) -> None:
        self._text = text

    def read(self) -> str:
        return self._text


class _StubProcess:
    def __init__(self, stdout: str = "", stderr: str = "", code: int = 0) -> None:
        self.stdout = _Stream(stdout)
        self.stderr = _Stream(stderr)
        self._code = code

    def wait(self) -> int:
        return self._code


class _StubImage:
    def __init__(self, object_id: str) -> None:
        self.object_id = object_id


class _StubSandbox:
    """Only the surface `resume_latency` actually calls, with the real names."""

    def __init__(self, modal: _StubModal, object_id: str, *, name: str, label: str) -> None:
        self._modal = modal
        self.object_id = object_id
        self._is_v2 = True  # what the real client sets on the V2 create path
        self.alive = True
        # What `Sandbox.create(name=..., tags=...)` was given. The real client
        # stores neither on the handle; they are kept here so a test can assert
        # the recovery handle exists and can address one sandbox by its label.
        self.name = name
        self.label = label
        self.terminate_calls = 0

    def wait_until_ready(self, timeout: int | None = None) -> None:
        self._modal.calls.append(("wait_until_ready", self.object_id))

    def poll(self) -> int | None:
        return None if self.alive else 0

    def exec(self, *argv: str, timeout: int | None = None) -> _StubProcess:
        return self._modal.exec_for(argv)

    def snapshot_filesystem(self, timeout: int, ttl: int | None = None) -> _StubImage:
        return self._modal.mint_snapshot("fs")

    def snapshot_directory(
        self, path: str, timeout: int | None = None, ttl: int | None = None
    ) -> _StubImage:
        return self._modal.mint_snapshot("dir")

    def mount_image(self, path: str, image: _StubImage) -> None:
        self._modal.mount_attempts += 1
        attempt = self._modal.mount_attempts
        if attempt in self._modal.fail_mount_on:
            raise RuntimeError(f"mount_image failed on #{attempt}")
        if attempt in self._modal.slow_mount_on:
            # Slow, then successful: a mount that blows its deadline and lands
            # anyway is the case the harness cannot distinguish from one that
            # blew its deadline and did not.
            time.sleep(_STUB_SLOW_S)
        self._modal.mounts.append((self.object_id, path))

    def unmount_image(self, path: str) -> None:
        self._modal.unmount_attempts += 1
        if self._modal.unmount_attempts in self._modal.fail_unmount_on:
            raise RuntimeError(f"unmount_image failed on #{self._modal.unmount_attempts}")
        self._modal.unmounts.append((self.object_id, path))

    def terminate(self) -> None:
        self.terminate_calls += 1
        hook = self._modal.on_terminate
        if hook is not None:
            hook(self)
        if self.label in self._modal.slow_terminate and self.terminate_calls == 1:
            # Overruns a (shortened) TERMINATE_TIMEOUT_S and then completes,
            # which is what leaves a sandbox both terminated and "LEAKED".
            time.sleep(_STUB_SLOW_S)
        self.alive = False
        self._modal.terminated.append(self.object_id)


class _StubModal:
    """A stand-in for the `modal` module, shaped like the real one.

    Carries `Sandbox`, `Probe`, `Image` and `App` -- and **no `experimental`**,
    because `import modal` does not bind it (verified against a real 1.5.5
    install: `modal/__init__.py` imports `billing` and `types` from the package
    and nothing else). `__name__` is a module name that does not resolve, so
    nothing here can accidentally reach the real client.

    `create` also double-checks the safety-relevant kwargs, so a change that
    started passing a credential or a reapable timeout fails here too.
    """

    __name__ = "stub_modal_not_installed"

    def __init__(
        self,
        *,
        fail_read_on: frozenset[int] = frozenset(),
        fail_snapshot_on: frozenset[int] = frozenset(),
        fail_mount_on: frozenset[int] = frozenset(),
        fail_unmount_on: frozenset[int] = frozenset(),
        slow_mount_on: frozenset[int] = frozenset(),
        slow_create: frozenset[str] = frozenset(),
        slow_terminate: frozenset[str] = frozenset(),
        files: int = 12,
    ) -> None:
        self.fail_read_on = fail_read_on
        self.fail_snapshot_on = fail_snapshot_on
        self.fail_mount_on = fail_mount_on
        self.fail_unmount_on = fail_unmount_on
        self.slow_mount_on = slow_mount_on
        self.slow_create = slow_create
        self.slow_terminate = slow_terminate
        self.files = files
        self.calls: list[tuple[str, str]] = []
        self.sandboxes: list[_StubSandbox] = []
        self.creates: list[tuple[str, dict[str, str]]] = []
        self.mounts: list[tuple[str, str]] = []
        self.unmounts: list[tuple[str, str]] = []
        self.terminated: list[str] = []
        self.snapshots: list[str] = []
        self.reads = 0
        self.fs_snapshots = 0
        self.mount_attempts = 0
        self.unmount_attempts = 0
        # Called at the top of every `terminate()`. The hook is how a test gets
        # inside the one call that hangs a paid run's teardown.
        self.on_terminate: Callable[[_StubSandbox], None] | None = None
        stub = self

        class Sandbox:
            @staticmethod
            def create(*argv: str, **kwargs: object) -> _StubSandbox:
                assert "secrets" not in kwargs and "env" not in kwargs
                assert isinstance(kwargs["timeout"], int) and kwargs["timeout"] > 300
                assert kwargs["idle_timeout"] is None
                # The recovery handle, asserted here as well as in its own
                # test: a create whose deadline fires never returns an id, so
                # a create with no `name=` and no `tags=` can strand a running
                # sandbox that nothing can name.
                name, tags = kwargs["name"], kwargs["tags"]
                assert isinstance(name, str) and name
                assert isinstance(tags, dict) and tags[rl.TAG_RUN] and tags[rl.TAG_LABEL]
                stub.creates.append((name, dict(tags)))
                label = str(tags[rl.TAG_LABEL])
                if label in stub.slow_create:
                    time.sleep(_STUB_SLOW_S)
                # 24 base62 characters: a sandbox id that is not the 22-char
                # v1 shape, so the id shape agrees with the `_is_v2` this stub
                # sets. A stub whose id said v1 while its flag said v2 would
                # be modelling a sandbox that cannot exist.
                sandbox = _StubSandbox(
                    stub, f"sb-{len(stub.sandboxes):024d}", name=name, label=label
                )
                stub.sandboxes.append(sandbox)
                return sandbox

        class Probe:
            @staticmethod
            def with_tcp(port: int) -> tuple[str, int]:
                return ("tcp", port)

        class Image:
            @staticmethod
            def from_registry(tag: str) -> _StubImage:
                return _StubImage("im-base")

            @staticmethod
            def from_name(name: str) -> _StubImage:
                return _StubImage(f"im-{name}")

        class App:
            @staticmethod
            def lookup(name: str, create_if_missing: bool = False) -> tuple[str, str]:
                return ("app", name)

        self.Sandbox = Sandbox
        self.Probe = Probe
        self.Image = Image
        self.App = App

    def mint_snapshot(self, kind: str) -> _StubImage:
        if kind == "fs":
            self.fs_snapshots += 1
            if self.fs_snapshots in self.fail_snapshot_on:
                raise RuntimeError(f"snapshot_filesystem failed on #{self.fs_snapshots}")
        image_id = f"im-{kind}-{len(self.snapshots)}"
        self.snapshots.append(image_id)
        return _StubImage(image_id)

    def exec_for(self, argv: tuple[str, ...]) -> _StubProcess:
        program = argv[2] if len(argv) > 2 else ""
        if program == rl._FILL or program == rl._NOOP:
            return _StubProcess("ok\n")
        if program == rl._MEASURE:
            return _StubProcess(json.dumps({"files": self.files, "bytes": self.files * 1024}))
        if program == rl._READ_ALL:
            # Exactly one read per iteration, so read N is iteration N.
            self.reads += 1
            if self.reads in self.fail_read_on:
                return _StubProcess(stderr="mounted tree has 3 files, expected 12", code=1)
            return _StubProcess(json.dumps({"files": self.files, "bytes_read": self.files * 1024}))
        raise AssertionError(f"the harness ran a program the stub does not know: {argv!r}")


def _measure(
    modal: _StubModal, *, iterations: int = 5, **overrides: object
) -> tuple[rl.Run, rl.Ledger, str]:
    cfg = rl.BenchConfig(iterations=iterations, project_files=12, project_file_kib=1, **overrides)  # type: ignore[arg-type]
    run, ledger, out = rl.Run(), rl.Ledger(), io.StringIO()
    rl.measure(modal, cfg, out=out, ledger=ledger, run=run)
    return run, ledger, out.getvalue()


def _shorten_deadline(monkeypatch, what: str, seconds: float = 0.05) -> None:
    """Make one named call's deadline fire, through the real deadline machinery.

    The alternative is waiting out the real bound, which is 240s for a create
    and 960s for a mount. Only the number of seconds is replaced: the call
    still runs on its own fresh thread, is still recorded in `stuck`, and still
    raises the real `DeadlineExceededError` from the real code path.
    """
    real = rl._Deadline.call

    def call(self, name: str, bound: float, fn, *, hint: str = ""):
        return real(self, name, seconds if name == what else bound, fn, hint=hint)

    monkeypatch.setattr(rl._Deadline, "call", call)


def test_the_stub_modal_resembles_the_real_one():
    """The rule that must-fix 1 was caused by breaking."""
    stub = _StubModal()
    assert not hasattr(stub, "experimental")
    for name in ("Sandbox", "Probe", "Image", "App"):
        assert hasattr(stub, name)


def test_a_clean_run_measures_every_phase_once_per_iteration():
    modal = _StubModal()
    run, ledger, text = _measure(modal)

    assert run.iterations_completed == 5
    assert run.failures == []
    for phase in (
        "cold_create_to_ready",
        "snapshot_filesystem",
        "snapshot_directory",
        "restore_by_boot_to_ready",
        "restore_by_mount",
        "restore_by_mount_read",
        "exec_spawn_overhead",
    ):
        assert run.iterations_of(phase) == [1, 2, 3, 4, 5], phase

    # The pool member is created once and lives for the whole run; every other
    # sandbox is gone by the end.
    pool, *rest = modal.sandboxes
    assert len(rest) == 10  # one subject and one restore per iteration
    assert pool.alive and all(not sandbox.alive for sandbox in rest)
    assert [label for label, _ in ledger.sandboxes] == ["pool"]
    assert len(modal.mounts) == len(modal.unmounts) == 5
    assert "pool member" in text


def test_a_failed_iteration_leaves_the_pool_clean_and_strands_no_sandbox():
    """Must-fix 5, reproduced: 5 mounts and 4 unmounts, a sandbox billing.

    `measure` swallows a failed iteration and starts the next one, so a
    success-path-only teardown left the failed iteration's subject running for
    the rest of the run -- at `-n 20`, ~70 minutes and about half the run's
    estimated cost, entirely outside the printed floor -- and left the pool
    member's project path populated, so the *next* iteration mounted onto a
    dirty path and recorded the result as valid data.
    """
    modal = _StubModal(fail_read_on=frozenset({3}))
    run, ledger, _ = _measure(modal)

    assert [f["iteration"] for f in run.failures] == [3]
    assert len(modal.mounts) == 5
    assert len(modal.unmounts) == 5  # was 4: the failed iteration never unmounted
    assert len(modal.mounts) == len(modal.unmounts)

    pool, *rest = modal.sandboxes
    assert pool.alive
    assert all(not sandbox.alive for sandbox in rest), "a failed iteration stranded a sandbox"
    assert [label for label, _ in ledger.sandboxes] == ["pool"]


def test_a_failure_before_the_mount_unmounts_nothing_and_still_terminates():
    """The other half of the `finally`: no mount attempted, no unmount issued."""
    modal = _StubModal(fail_snapshot_on=frozenset({2}))
    run, _ledger, _ = _measure(modal)

    assert [f["iteration"] for f in run.failures] == [2]
    assert len(modal.mounts) == 4
    assert len(modal.unmounts) == 4  # not 5: iteration 2 never mounted anything
    pool, *rest = modal.sandboxes
    assert pool.alive and all(not sandbox.alive for sandbox in rest)


def test_a_mount_that_failed_outright_is_not_unmounted_and_warns_about_nothing():
    """A clean `mount_image` failure left the old code attempting an unmount --
    4 mounts against 5 unmounts -- and an unmount failure then printed "a later
    iteration's tree may be a superset of what it snapshotted", which is false
    every time: the mount returned an error, so nothing was mounted. The
    unmount is attempted when the mount *may* have landed, not when it
    certainly did not."""
    modal = _StubModal(fail_mount_on=frozenset({1}), fail_unmount_on=frozenset({1}))
    run, ledger, _ = _measure(modal, iterations=1)

    assert [f["error_type"] for f in run.failures] == ["RuntimeError"]
    assert modal.mounts == []
    assert modal.unmount_attempts == 0, "an unmount was attempted for a mount that never landed"
    assert ledger.leaked == []
    assert run.samples.get("restore_by_mount") is None


def test_an_unmount_failure_says_whether_the_tree_is_certainly_there(monkeypatch):
    """The two cases that do need an unmount, and the difference in what they
    are allowed to claim afterwards."""
    landed = _StubModal(fail_unmount_on=frozenset({1}))
    _, ledger, _ = _measure(landed, iterations=1)
    assert landed.unmount_attempts == 1
    assert len(ledger.leaked) == 1
    assert "iteration 1" in ledger.leaked[0]
    assert "may be a superset of what it snapshotted" in ledger.leaked[0]
    assert "may or may not" not in ledger.leaked[0]  # this mount certainly landed

    timed_out = _StubModal(slow_mount_on=frozenset({1}), fail_unmount_on=frozenset({1}))
    _shorten_deadline(monkeypatch, "mount_image")
    run, doubtful, _ = _measure(timed_out, iterations=1)
    assert run.meta["deadline_timeouts"] == ["mount_image"]
    assert timed_out.unmount_attempts == 1, "a timed-out mount may have landed: still unmount"
    assert len(doubtful.leaked) == 1
    assert "may or may not hold that tree" in doubtful.leaked[0]
    assert "may be a superset of what it snapshotted" in doubtful.leaked[0]


def test_a_mid_run_read_failure_does_not_shift_the_end_to_end_pairings():
    """Must-fix 4, end to end through `measure` rather than through the joiner."""
    modal = _StubModal(fail_read_on=frozenset({3}))
    run, _, _ = _measure(modal)

    assert run.iterations_of("restore_by_mount") == [1, 2, 3, 4, 5]
    assert run.iterations_of("restore_by_mount_read") == [1, 2, 4, 5]

    total = next(s for s in rl.summaries(run) if s.phase == "restore_by_mount_total")
    assert total.n == 4  # iteration 3 dropped whole; the rest keep their own halves

    document = rl.report(
        rl.BenchConfig(iterations=5), run, rl.Ledger(), rl.estimate_for(rl.BenchConfig())
    )
    assert document["sample_iterations"]["restore_by_mount_read"] == [1, 2, 4, 5]


def test_summaries_drop_the_iteration_that_lost_a_half_rather_than_shifting_the_rest():
    """The same defect at the arithmetic level, with numbers that name it."""
    run = rl.Run()
    for iteration, mount, read in [
        (1, 1.0, 0.1),
        (2, 2.0, 0.2),
        (3, 30.0, None),  # the read failed here
        (4, 4.0, 0.4),
        (5, 5.0, 0.5),
    ]:
        run.record("restore_by_mount", mount, iteration=iteration)
        if read is not None:
            run.record("restore_by_mount_read", read, iteration=iteration)

    total = next(s for s in rl.summaries(run) if s.phase == "restore_by_mount_total")
    assert total.n == 4
    assert total.max_s == pytest.approx(5.5)  # positional zipping reported 30.4s


def test_the_pool_members_backend_is_recorded_from_the_clients_own_answer():
    modal = _StubModal()
    run, _, text = _measure(modal)
    assert run.meta["backend_observed"] == "v2"
    assert "_is_v2" in str(run.meta["backend_observed_from"])
    assert "from modal.Sandbox._is_v2" in text
    # The stub's flag and its id shape agree, so nothing is flagged as a
    # disagreement -- which is what makes the disagreement case meaningful.
    assert "the id shape says" not in str(run.meta["backend_observed_from"])
    assert rl.observed_backend(str(run.meta["pool_sandbox_id"])) == "v2"


def test_every_create_carries_the_name_and_tags_that_make_recovery_possible():
    """A create whose deadline fires never returns an id, so the id cannot be
    the only handle. Modal's own limits, from `modal/_utils/name_utils.py` in
    1.5.5: 64 characters of `[a-zA-Z0-9-_.]` for a name, 63 of
    `[a-zA-Z0-9._-]` for a tag key or value."""
    modal = _StubModal()
    _, ledger, _ = _measure(modal, iterations=2)

    assert [tags[rl.TAG_LABEL] for _, tags in modal.creates] == [
        "pool",
        "subject-1",
        "restore-1",
        "subject-2",
        "restore-2",
    ]
    names = [name for name, _ in modal.creates]
    assert len(set(names)) == len(names), "a name is unique among running sandboxes in an App"
    for name, tags in modal.creates:
        assert re.fullmatch(r"[A-Za-z0-9._-]{1,64}", name), name
        assert tags[rl.TAG_RUN] == ledger.run_id
        assert tags[rl.TAG_HARNESS] == rl.TAG_HARNESS_VALUE
        for key, value in tags.items():
            assert re.fullmatch(r"[A-Za-z0-9._-]{1,63}", key), key
            assert re.fullmatch(r"[A-Za-z0-9._-]{1,63}", value), value

    # Two runs in one App must not collide on a name, which is what a fixed
    # name would do against this morning's leftovers.
    assert rl.new_run_id() != ""
    assert rl.sandbox_identity(rl.Ledger(), "pool") != rl.sandbox_identity(rl.Ledger(), "pool")


def test_a_terminate_that_overran_its_deadline_and_then_worked_is_not_leaked(monkeypatch):
    """The stale line. `_iteration` terminates on the success path for
    measurement reasons and again in its `finally`; when the first call overran
    `TERMINATE_TIMEOUT_S` and then completed, the report listed `restore-1`
    under both `sandboxes_terminated` and `leaked` -- and the leak entry
    appeared twice -- in the document destined for `docs/verified.md`."""
    monkeypatch.setattr(rl, "TERMINATE_TIMEOUT_S", 0.05)
    modal = _StubModal(slow_terminate=frozenset({"restore-1"}))
    run, ledger, _ = _measure(modal, iterations=1)

    assert run.meta["deadline_timeouts"] == ["restore-1 terminate"]
    assert ledger.terminated.count("restore-1") == 1
    assert ledger.leaked == [], "a success retracts the earlier attempt's line"
    assert [label for label, _ in ledger.sandboxes] == ["pool"]

    cfg = rl.BenchConfig(iterations=1)
    document = rl.report(cfg, run, ledger, rl.estimate_for(cfg))
    cleanup = document["cleanup"]
    assert cleanup["sandboxes_terminated"].count("restore-1") == 1
    assert cleanup["leaked"] == []
    assert "restore-1" not in cleanup["sandboxes_outstanding"]


def test_the_leak_line_for_a_timed_out_terminate_is_about_the_sandbox_not_a_snapshot(monkeypatch):
    """`_Deadline`'s message was one size fits all: a terminate that hung said
    "if it was a snapshot, an image may exist whose id this process will never
    see", which is nonsense about a terminate. The id belongs in the line too:
    a leak line naming only the harness's own label is not actionable."""
    monkeypatch.setattr(rl, "TERMINATE_TIMEOUT_S", 0.02)

    class _Hangs:
        object_id = "sb-hangsforever0000000000"

        def terminate(self) -> None:
            time.sleep(_STUB_SLOW_S)

    sandbox = _Hangs()
    ledger = rl.Ledger(sandboxes=[("restore-1", sandbox)])
    rl._terminate("restore-1", sandbox, ledger=ledger, deadline=rl._Deadline())

    assert len(ledger.leaked) == 1
    line = ledger.leaked[0]
    assert "snapshot" not in line
    assert "may still be running and billing" in line
    assert sandbox.object_id in line
    assert [label for label, _ in ledger.sandboxes] == ["restore-1"]  # cleanup retries it


def test_the_run_records_the_lifetime_and_throttle_bound_it_actually_used():
    modal = _StubModal()
    run, _, _ = _measure(modal)
    assert run.meta["sandbox_lifetime_s"] == rl.resolved_sandbox_timeout_s(
        rl.BenchConfig(iterations=5, project_files=12, project_file_kib=1)
    )
    assert run.meta["max_throttle_wait_s"] == rl.DEFAULT_MAX_THROTTLE_WAIT_S


# ------------------------------------------------------------------ the deadline


def test_a_timed_out_call_does_not_poison_the_one_after_it():
    """Must-fix 3(b), reproduced.

    With one shared `ThreadPoolExecutor(max_workers=1)`, a call that timed out
    left the single worker busy, so the next `submit` sat in the queue and its
    own `result(timeout=...)` expired without the call ever starting. After one
    real timeout every later phase burned its whole timeout measuring nothing,
    and the run then hung forever at interpreter exit.
    """
    release = threading.Event()
    try:
        with rl._Deadline() as deadline:
            with pytest.raises(rl.BenchError, match="did not return within"):
                deadline.call("stuck", 0.05, release.wait)

            value, seconds = deadline.call("after", 10.0, lambda: (time.sleep(0.05), "ok")[1])
            assert value == "ok"
            assert 0.01 <= seconds < 5.0, "the second call measured queue time, not itself"
            assert deadline.stuck == ("stuck",)
    finally:
        release.set()


def test_a_stuck_call_runs_on_a_daemon_thread_so_it_cannot_block_exit():
    """Must-fix 3(b) again: `ThreadPoolExecutor` registers an atexit join.

    `shutdown(wait=False, cancel_futures=True)` did not help -- the process
    still blocked until the stuck worker returned, *after* printing the report.
    """
    release = threading.Event()
    try:
        with rl._Deadline() as deadline:
            with pytest.raises(rl.BenchError, match="daemon"):
                deadline.call("stuck", 0.05, release.wait)
        alive = [
            t for t in threading.enumerate() if t.name.startswith("modal-bench-") and t.is_alive()
        ]
        assert alive, "the stuck call's thread should still be running"
        assert all(t.daemon for t in alive)
    finally:
        release.set()


def test_the_deadline_times_the_call_and_not_the_wait_to_start():
    """Must-fix 3(a): started at `submit`, a slow predecessor's tail became the
    next phase's latency -- a `restore_by_mount` figure inflated by the previous
    iteration's snapshot overrun, reported as mount latency."""
    with rl._Deadline() as deadline:
        _, slow = deadline.call("slow", 10.0, lambda: time.sleep(0.20))
        _, fast = deadline.call("fast", 10.0, lambda: time.sleep(0.01))
    assert slow >= 0.19
    assert fast < 0.15


def test_an_exception_inside_the_call_reaches_the_caller_unchanged():
    with rl._Deadline() as deadline:
        with pytest.raises(ZeroDivisionError):
            deadline.call("boom", 10.0, lambda: 1 / 0)


def test_the_deadline_returns_what_the_call_returned():
    with rl._Deadline() as deadline:
        value, seconds = deadline.call("fine", 10.0, lambda: {"files": 3})
        assert value == {"files": 3}
        assert seconds >= 0.0
        assert deadline.stuck == ()


# -------------------------------------------------------- lifetime and resources


def test_the_default_config_survives_the_run_the_report_tells_you_to_do():
    """Must-fix 2. `-n 20` is not arbitrary: `samples_to_resolve(95)` is 20 and
    `render_human` tells the operator to quote a tail only from a run that
    long. That single recommended paid run projected ~4500s against a fixed
    3600s lifetime, so the pool member -- created first, needed last -- would
    have been reaped around iteration 16."""
    n = stats.samples_to_resolve(95)
    assert n == 20
    cfg = rl.BenchConfig(iterations=n)
    projected = rl.estimate_for(cfg).wall_clock_seconds
    lifetime = rl.resolved_sandbox_timeout_s(cfg)

    assert projected > 3600, "the old fixed default was shorter than this very run"
    assert lifetime > projected
    assert lifetime <= rl.MODAL_MAX_SANDBOX_TIMEOUT_S
    assert (
        rl.blockers(cfg, environ={rl.SPEND_ENV: "1"}, credentials=(True, "t"), import_error=None)
        == ()
    )


def test_the_derived_lifetime_is_capped_at_modals_ceiling_and_has_a_floor():
    assert rl.resolved_sandbox_timeout_s(rl.BenchConfig(iterations=1)) == rl.SANDBOX_TIMEOUT_FLOOR_S
    huge = rl.resolved_sandbox_timeout_s(rl.BenchConfig(iterations=rl.MAX_ITERATIONS))
    assert huge <= rl.MODAL_MAX_SANDBOX_TIMEOUT_S


def test_a_lifetime_shorter_than_the_projected_run_is_refused_and_names_the_flag():
    reasons = rl.blockers(
        rl.BenchConfig(iterations=20, sandbox_timeout_s=3600),
        environ={rl.SPEND_ENV: "1"},
        credentials=(True, "t"),
        import_error=None,
    )
    assert len(reasons) == 1
    assert "--sandbox-timeout" in reasons[0]
    assert "reaped around iteration 16 of 20" in reasons[0]


def test_a_lifetime_above_modals_ceiling_is_clamped_where_the_refusal_says_it_is():
    """The refusal text has always said "Modal caps it at 86400s" while the
    flag itself was accepted unclamped, so the harness was willing to ask for a
    lifetime Modal has no documented way to grant."""
    over = rl.BenchConfig(sandbox_timeout_s=rl.MODAL_MAX_SANDBOX_TIMEOUT_S + 10_000)
    assert rl.resolved_sandbox_timeout_s(over) == rl.MODAL_MAX_SANDBOX_TIMEOUT_S
    assert rl.create_kwargs(over)["timeout"] == rl.MODAL_MAX_SANDBOX_TIMEOUT_S

    reasons = rl.blockers(
        over, environ={rl.SPEND_ENV: "1"}, credentials=(True, "t"), import_error=None
    )
    assert any("--sandbox-timeout" in reason and "86400" in reason for reason in reasons)

    # ...and the ceiling exactly is not a mistake.
    assert (
        rl.blockers(
            rl.BenchConfig(sandbox_timeout_s=rl.MODAL_MAX_SANDBOX_TIMEOUT_S),
            environ={rl.SPEND_ENV: "1"},
            credentials=(True, "t"),
            import_error=None,
        )
        == ()
    )


def test_the_lifetime_caption_names_the_rule_that_actually_decided_it():
    """The caption read "derived from the wall clock above" on every run.

    Including every run of the documented `-n 5`, where the floor decides the
    number and the wall clock does not.
    """
    five = rl.BenchConfig(iterations=5)
    assert rl.resolved_sandbox_timeout_s(five) == rl.SANDBOX_TIMEOUT_FLOOR_S
    assert "floor" in rl.lifetime_basis(five)

    twenty = rl.BenchConfig(iterations=stats.samples_to_resolve(95))
    assert rl.resolved_sandbox_timeout_s(twenty) > rl.SANDBOX_TIMEOUT_FLOOR_S
    assert "wall clock" in rl.lifetime_basis(twenty)
    assert "floor" not in rl.lifetime_basis(twenty)

    assert rl.lifetime_basis(rl.BenchConfig(sandbox_timeout_s=4242)) == (
        "--sandbox-timeout, as given"
    )
    clamped = rl.lifetime_basis(rl.BenchConfig(sandbox_timeout_s=99_999))
    assert "clamped" in clamped and "86400" in clamped

    # And the caption the operator actually reads says the same thing.
    text = stats.render_cost_estimate(
        rl.estimate_for(five),
        sandbox_lifetime_s=rl.resolved_sandbox_timeout_s(five),
        lifetime_basis=rl.lifetime_basis(five),
    )
    assert "1800s  (the 1800s floor" in text
    assert "derived from the wall clock above" not in text


def test_a_fat_fingered_resource_request_is_refused_rather_than_priced():
    for cfg, flag in [
        (rl.BenchConfig(cpu_cores=100.0), "--cpu"),
        (rl.BenchConfig(cpu_cores=0.0), "--cpu"),
        (rl.BenchConfig(memory_mib=1_000_000), "--memory-mib"),
        (rl.BenchConfig(iterations=10_000), "--iterations"),
    ]:
        reasons = rl.blockers(
            cfg, environ={rl.SPEND_ENV: "1"}, credentials=(True, "t"), import_error=None
        )
        assert any(flag in reason for reason in reasons), (cfg, reasons)

    # ...and the sane request still passes.
    assert (
        rl.blockers(
            rl.BenchConfig(cpu_cores=rl.MAX_CPU_CORES, iterations=rl.MAX_ITERATIONS),
            environ={rl.SPEND_ENV: "1"},
            credentials=(True, "t"),
            import_error=None,
        )
        == ()
    )


# ------------------------------------------------- the report, end to end in main


@pytest.fixture
def a_run_that_can_spend(monkeypatch):
    """Satisfy every gate with a stub client, so `main` runs start to finish.

    Installed into `sys.modules` so `main`'s lazy `import modal` resolves to
    it. `monkeypatch.setitem` removes the key again afterwards, which keeps
    `test_importing_the_harness_does_not_import_modal` honest.
    """

    def install(modal: _StubModal) -> _StubModal:
        monkeypatch.setitem(sys.modules, "modal", modal)
        monkeypatch.setenv("MODAL_TOKEN_ID", "ak-fake")
        monkeypatch.setenv("MODAL_TOKEN_SECRET", "as-fake")
        monkeypatch.setenv(rl.SPEND_ENV, "1")
        monkeypatch.delenv("MODAL_SANDBOX_V2", raising=False)
        monkeypatch.delenv("MODAL_MAX_THROTTLE_WAIT", raising=False)
        return modal

    return install


def test_an_unwritable_json_out_does_not_discard_the_whole_run(
    tmp_path, capsys, a_run_that_can_spend
):
    """The write used to happen BEFORE the human report, and unguarded.

    A `--json-out` into a directory that does not exist therefore threw away
    the entire output of a paid 75-minute run before a single number reached
    the terminal.
    """
    a_run_that_can_spend(_StubModal())
    nowhere = tmp_path / "missing-directory" / "report.json"

    assert rl.main(["--run", "-n", "2", "--json-out", str(nowhere)]) == 0

    out = capsys.readouterr().out
    assert "| phase" in out and "restore_by_mount_total" in out
    assert "could not write" in out
    assert '"schema": "halyard.bench.modal-resume-latency/1"' in out  # fell back to stdout
    assert not nowhere.exists()


def test_the_human_report_is_printed_before_the_json_path(tmp_path, capsys, a_run_that_can_spend):
    a_run_that_can_spend(_StubModal())
    destination = tmp_path / "report.json"

    assert rl.main(["--run", "-n", "2", "--json-out", str(destination)]) == 0

    out = capsys.readouterr().out
    assert out.index("| phase") < out.index("machine-readable report")
    document = json.loads(destination.read_text())
    assert document["sample_iterations"]["restore_by_mount"] == [1, 2]
    assert document["sandbox_lifetime_s"] == rl.SANDBOX_TIMEOUT_FLOOR_S
    assert document["config"]["max_throttle_wait_s"] == rl.DEFAULT_MAX_THROTTLE_WAIT_S


def test_the_whole_report_is_out_before_cleanup_can_hang(tmp_path, a_run_that_can_spend):
    """Cleanup used to run in a `finally` that fired BEFORE the report.

    So a pool member whose `terminate()` blocked forever hung the main thread
    with only the per-iteration progress lines printed: no phase table, no JSON
    document, and no LEAKED ids -- the one part of a paid run's output that a
    human can act on, and the part `Ledger`'s own docstring says must reach
    them ("a human holding the id can still delete it, a human without it
    cannot").

    Proved from inside the call that hangs. By the time cleanup touches
    `terminate()`, the report is already on stdout and the JSON -- ids included
    -- is already on disk.
    """
    modal = a_run_that_can_spend(_StubModal())
    destination = tmp_path / "report.json"
    out = io.StringIO()
    seen: dict[str, str] = {}

    def snapshot(sandbox: _StubSandbox) -> None:
        # Only the pool member, because it is the only sandbox that nothing but
        # cleanup terminates.
        if sandbox.label == "pool":
            seen["stdout"] = out.getvalue()
            seen["json"] = destination.read_text() if destination.exists() else ""

    modal.on_terminate = snapshot

    assert rl.main(["--run", "-n", "1", "--json-out", str(destination)], out=out) == 0

    assert "| phase" in seen["stdout"], "the phase table had not been printed yet"
    assert "machine-readable report" in seen["stdout"]
    document = json.loads(seen["json"])
    assert document["phases"], "the JSON was written after cleanup"
    # The ids, before anything had a chance to hang: the pool member still to
    # terminate and both of the iteration's snapshots still to delete.
    assert document["cleanup"]["sandboxes_outstanding"] == ["pool"]
    assert len(document["cleanup"]["images_outstanding"]) == 2
    requested = document["cleanup"]["sandboxes_requested"]
    assert requested[0]["sandbox_id"] == modal.sandboxes[0].object_id

    # ...and cleanup's own outcome comes after the report, not instead of it.
    text = out.getvalue()
    assert text.index("| phase") < text.index("cleanup: terminated")
    assert "cleanup: terminated 1 sandbox(es), deleted 0 image(s)" in text
    # `_StubModal.__name__` does not resolve, so `image_delete` is genuinely
    # unreachable and both images are reported in full, with the reason.
    final = json.loads(destination.read_text())
    assert final["cleanup"]["sandboxes_terminated"] == ["subject-1", "restore-1", "pool"]
    assert final["cleanup"]["sandboxes_outstanding"] == []
    assert len(final["cleanup"]["leaked"]) == 2
    for image_id in modal.snapshots:
        assert any(image_id in item for item in final["cleanup"]["leaked"])
        assert image_id in text


def test_a_ctrl_c_during_cleanup_still_prints_what_it_did_not_reclaim(
    tmp_path, a_run_that_can_spend
):
    """The SIGTERM sent to kill a hung cleanup arrives inside the blocked call.

    A per-item `except Exception` did not catch it, so it became an uncaught
    `KeyboardInterrupt` traceback out of `main` and the LEAKED block never
    printed at all. Simulated the way it really arrives: raised from inside
    `terminate()`.
    """
    modal = a_run_that_can_spend(_StubModal())
    destination = tmp_path / "report.json"
    out = io.StringIO()

    def interrupt(sandbox: _StubSandbox) -> None:
        if sandbox.label == "pool":
            raise KeyboardInterrupt("signal 15")

    modal.on_terminate = interrupt

    assert rl.main(["--run", "-n", "1", "--json-out", str(destination)], out=out) == 0

    text = out.getvalue()
    assert "cleanup did not finish (KeyboardInterrupt: signal 15)" in text
    assert "LEAKED" in text
    assert "not terminated -- cleanup was interrupted" in text
    assert modal.sandboxes[0].object_id in text  # the pool member's id, printed
    for image_id in modal.snapshots:
        assert f"image {image_id}: not deleted -- cleanup was interrupted" in text
    document = json.loads(destination.read_text())
    assert document["cleanup"]["sandboxes_outstanding"] == ["pool"]
    assert len(document["cleanup"]["leaked"]) == 3  # the pool member and both images


def test_a_kill_landing_during_the_report_still_stops_the_meters(
    tmp_path, monkeypatch, capsys, a_run_that_can_spend
):
    """The window the reordering opens, closed in the same place.

    Printing the report before cleanup means the `kill` aimed at a hung
    teardown can land during the report instead. Stopping the meters must not
    be hostage to printing a table, and the failure must not become a
    traceback out of `main` -- that is how the previous version lost its
    LEAKED block.
    """
    modal = a_run_that_can_spend(_StubModal())

    def interrupted(*_args, **_kwargs):
        raise KeyboardInterrupt("signal 15")

    monkeypatch.setattr(rl, "render_human", interrupted)

    assert rl.main(["--run", "-n", "1", "--json-out", str(tmp_path / "r.json")]) == 130

    out = capsys.readouterr().out
    assert "reporting failed (KeyboardInterrupt: signal 15); cleaning up anyway" in out
    assert "cleanup: terminated 1 sandbox(es)" in out
    assert not modal.sandboxes[0].alive, "the pool member was left running"
    # The file still lands, written from the same document after cleanup.
    document = json.loads((tmp_path / "r.json").read_text())
    assert document["cleanup"]["sandboxes_terminated"] == ["subject-1", "restore-1", "pool"]
    assert document["phases"]


def test_a_create_that_blows_its_deadline_leaves_a_handle_a_human_can_use(
    tmp_path, monkeypatch, a_run_that_can_spend
):
    """`ledger.sandboxes` can only be appended to once the create returns, so a
    create that never returns used to strand a sandbox with no id anywhere:
    cleanup printed "terminated 0 sandbox(es)" while a sandbox may have been
    running for the whole derived lifetime -- 6832s at `-n 20`, about $0.45,
    more than the run's own printed estimate.

    The exposure is new: without a deadline the call would eventually return
    the handle. So the name and tags are recorded *before* the call, and the
    recovery route is printed the moment the deadline fires.
    """
    modal = a_run_that_can_spend(_StubModal(slow_create=frozenset({"pool"})))
    _shorten_deadline(monkeypatch, "pool Sandbox.create")
    destination = tmp_path / "report.json"
    out = io.StringIO()

    # No iteration completed, so the run is a failure -- but a reported one.
    assert rl.main(["--run", "-n", "20", "--json-out", str(destination)], out=out) == 1

    text = out.getvalue()
    document = json.loads(destination.read_text())
    run_id = document["metadata"]["run_id"]
    name = f"halyard-bench-{run_id}-pool"

    assert "WARNING: pool: Sandbox.create blew its deadline" in text
    assert f"name={name!r}" in text
    assert f"modal.Sandbox.from_name({rl.BenchConfig().app_name!r}, {name!r}).terminate()" in text
    assert f'modal.Sandbox.list(tags={{"halyard-bench-run": "{run_id}"}})' in text
    assert "MODAL_SANDBOX_V2=1" in text
    assert "dashboard is the only remaining route" in text
    assert "billing for its whole timeout=6832s lifetime" in text

    # The report says which call hung, which it could not when
    # `deadline_timeouts` was assigned after the iteration loop.
    assert document["metadata"]["deadline_timeouts"] == ["pool Sandbox.create"]
    assert document["iterations_completed"] == 0

    # No id was ever learned -- but the name and tags were recorded before the
    # call, so the artifact still names what may be running.
    requested = document["cleanup"]["sandboxes_requested"]
    assert requested == [
        {
            "label": "pool",
            "name": name,
            "tags": {
                rl.TAG_HARNESS: rl.TAG_HARNESS_VALUE,
                rl.TAG_RUN: run_id,
                rl.TAG_LABEL: "pool",
            },
            "sandbox_id": None,
        }
    ]
    assert document["cleanup"]["sandboxes_terminated"] == []
    assert any(name in item for item in document["cleanup"]["leaked"])

    # The exposure is real rather than hypothetical: the stub's create returns
    # after the deadline has already fired, so a sandbox exists server-side
    # that the harness never saw. Waited for, because that is what "the call is
    # still running on a worker thread" means.
    limit = time.monotonic() + 5
    while not modal.sandboxes and time.monotonic() < limit:
        time.sleep(0.01)
    assert [sandbox.name for sandbox in modal.sandboxes] == [name]
    assert "cleanup: terminated 0 sandbox(es), deleted 0 image(s)" in text


def test_the_modal_env_is_set_before_blockers_imports_modal(
    tmp_path, monkeypatch, capsys, a_run_that_can_spend
):
    """The comment claimed this; the code did not do it.

    `blockers(...)` calls `modal_import_error()`, which imports modal -- so the
    assignment that sat below `blockers` ran after the import it claimed to
    precede. Harmless while `config.get` re-reads `MODAL_<KEY>` on every read,
    a bug the moment a release snapshots config at import time.
    """
    seen: dict[str, str | None] = {}
    real = rl.modal_import_error

    def recording() -> str | None:
        seen["MODAL_SANDBOX_V2"] = os.environ.get("MODAL_SANDBOX_V2")
        seen["MODAL_MAX_THROTTLE_WAIT"] = os.environ.get("MODAL_MAX_THROTTLE_WAIT")
        return real()

    monkeypatch.setattr(rl, "modal_import_error", recording)
    a_run_that_can_spend(_StubModal())

    assert rl.main(["--run", "-n", "1", "--json-out", str(tmp_path / "r.json")]) == 0
    assert seen == {"MODAL_SANDBOX_V2": "1", "MODAL_MAX_THROTTLE_WAIT": "120"}
    assert "MODAL_MAX_THROTTLE_WAIT 120s" in capsys.readouterr().out


def test_the_report_names_the_exec_overhead_included_in_the_read():
    """`restore_by_mount_read` pays an exec RPC, a stdout drain and a wait that
    a real resume does not. Reported rather than subtracted, because at these
    sample counts the distributions overlap and a subtraction can go negative.
    """
    cfg = rl.BenchConfig(iterations=5)
    run = _finished_run()
    text = rl.render_human(cfg, run, rl.report(cfg, run, rl.Ledger(), rl.estimate_for(cfg)))
    assert "exec_spawn_overhead" in text
    assert "is the instrument, not Modal" in text


def test_the_workload_line_describes_the_whole_run_not_the_last_iteration():
    """`meta["workload"]` was overwritten every iteration while `workloads` kept
    all of them, so the caption described the last tree and the table above it
    described all of them."""
    run = _finished_run()
    assert "all 5" in rl.workload_note(run)
    assert run.meta.get("workload") is None  # no single-iteration value to quote

    run.workloads[-1] = {"iteration": 5, "path": "/p", "files": 9, "bytes": 900, "fill": "random"}
    varied = rl.workload_note(run)
    assert "varied across 5" in varied
    assert "9-20000 files" in varied

    assert "no iteration got as far" in rl.workload_note(rl.Run())


def test_a_refused_run_never_reaches_sandbox_create(monkeypatch, capsys, a_run_that_can_spend):
    """The gate, asserted against a client that would record being called.

    No spend opt-in means nothing is created. This is the assertion the older
    `"modal" not in sys.modules` check was reaching for: importing the client
    is harmless, calling it is not.
    """
    modal = a_run_that_can_spend(_StubModal())
    monkeypatch.delenv(rl.SPEND_ENV, raising=False)

    assert rl.main(["--run", "-n", "2"]) == 2

    assert modal.sandboxes == []
    assert modal.snapshots == []
    assert modal.mounts == []
    assert rl.SPEND_ENV in capsys.readouterr().out


def test_credentials_without_the_opt_in_create_nothing_even_with_a_live_client(
    monkeypatch, a_run_that_can_spend
):
    """Every value of the opt-in other than exactly "1" is a refusal."""
    modal = a_run_that_can_spend(_StubModal())
    for value in ("", "0", "yes", "true", "TRUE", " 1"):
        monkeypatch.setenv(rl.SPEND_ENV, value)
        assert rl.main(["--run", "-n", "1"]) == 2
    assert modal.sandboxes == []
