"""Configuration reads every problem at once, and never echoes a value."""

from __future__ import annotations

from datetime import timedelta

import pytest
from halyard_chassis.obs import vocab
from halyard_chassis.settings import (
    ChassisSettings,
    ConfigError,
    format_go_duration,
    load,
)

# Built at runtime, never written down. A literal shaped this much like a real
# Stripe key trips GitHub's push protection -- it validates shape, not liveness
# -- and asking it to allowlist a fake would blunt a tool that is doing its job.
# Deriving it from the generated prefix list is better anyway: the fixture
# follows the vocabulary instead of duplicating one entry from it.
LIVE_LOOKING_KEY = vocab.SECRET_PREFIXES[0] + "4eC39HqLyjWDarjtT1zdp7dc"


def _env(**overrides: str) -> dict[str, str]:
    return dict(overrides)


def test_defaults_load_with_an_empty_environment() -> None:
    s = load(ChassisSettings, environ={}, service="test")
    assert s.addr == ":8080"
    assert s.http_read_timeout == timedelta(seconds=30)
    assert s.shutdown_budget == timedelta(seconds=25)
    assert s.env == "development"


def test_every_problem_is_reported_at_once() -> None:
    """The property the whole module exists for.

    An operator with three variables wrong should learn all three from one
    restart, not one per deploy.
    """
    env = _env(HTTP_MAX_HEADER_VALUES="0", LOG_LEVEL="chatty", HALYARD_ENV="prod")
    with pytest.raises(ConfigError) as caught:
        load(ChassisSettings, environ=env, service="test", **_as_kwargs(env))
    reported = {p.env for p in caught.value.problems}
    assert {"HTTP_MAX_HEADER_VALUES", "LOG_LEVEL", "HALYARD_ENV"} <= reported


def _as_kwargs(env: dict[str, str]) -> dict[str, str]:
    """Feed the fake environment in through the constructor.

    `load` reads os.environ for real; passing the same values as keyword
    overrides is how a test supplies configuration without touching the
    process environment. Verified: init kwargs override env without
    monkeypatching.
    """
    return env


def test_a_problem_never_contains_the_value() -> None:
    """SPEC 17.2. A ValidationError's `input` field carries the raw
    pre-validation dict, so the naive renderer leaks every env value."""
    env = {"OTEL_TRACES_SAMPLER_ARG": LIVE_LOOKING_KEY}
    with pytest.raises(ConfigError) as caught:
        load(ChassisSettings, environ=env, service="test", **env)
    rendered = str(caught.value)
    assert LIVE_LOOKING_KEY not in rendered
    assert "OTEL_TRACES_SAMPLER_ARG" in rendered


def test_a_typo_is_reported_with_a_suggestion() -> None:
    """extra="forbid" is inert for os.environ, so this is built by hand."""
    with pytest.raises(ConfigError) as caught:
        load(ChassisSettings, environ={"HTTP_ADDDR": ":9090"}, service="test")
    problem = next(p for p in caught.value.problems if p.env == "HTTP_ADDDR")
    assert "HTTP_ADDR" in problem.provenance


def test_an_unrelated_variable_is_not_flagged() -> None:
    """PATH and HOME are not ours to complain about."""
    s = load(ChassisSettings, environ={"PATH": "/usr/bin", "HOME": "/root"}, service="test")
    assert s.service == "test"


@pytest.mark.parametrize("blank", ["", "   ", "\t"])
def test_a_blank_variable_falls_back_to_the_default(blank: str) -> None:
    """Compose, Kubernetes and shell templating all emit FOO= for unset.

    Whitespace-only matters separately: env_ignore_empty only sees the untrimmed
    string, so "   " would bind the empty string while the Go chassis -- whose
    raw() trims first -- falls back.
    """
    s = load(ChassisSettings, environ={}, service="test", HTTP_ADDR=blank)
    assert s.addr == ":8080"


def test_whitespace_is_stripped_before_parsing() -> None:
    """int tolerates " 42 " and bool rejects " true ", so without this a stray
    space breaks only the boolean settings."""
    s = load(ChassisSettings, environ={}, service="test", READINESS_DETAIL=" true ")
    assert s.readiness_detail is True


def test_debug_logging_is_refused_in_production() -> None:
    """SPEC 17.3: debug reveals user content, so this is a privacy setting."""
    with pytest.raises(ConfigError) as caught:
        load(
            ChassisSettings, environ={}, service="test", HALYARD_ENV="production", LOG_LEVEL="debug"
        )
    assert "reveals user content" in str(caught.value)


def test_the_shutdown_phases_must_fit_the_budget() -> None:
    """Overrunning it means SIGKILL mid-write."""
    with pytest.raises(ConfigError) as caught:
        load(
            ChassisSettings,
            environ={},
            service="test",
            SHUTDOWN_BUDGET="5s",
            SHUTDOWN_DRAIN_TIMEOUT="10s",
        )
    assert "SHUTDOWN_BUDGET" in str(caught.value)


def test_readiness_detail_is_refused_in_production() -> None:
    """/readyz is unauthenticated and detail includes probe error strings."""
    with pytest.raises(ConfigError):
        load(
            ChassisSettings,
            environ={},
            service="test",
            HALYARD_ENV="production",
            READINESS_DETAIL="true",
        )


def test_a_public_admin_address_is_refused_in_production() -> None:
    with pytest.raises(ConfigError):
        load(
            ChassisSettings,
            environ={},
            service="test",
            HALYARD_ENV="production",
            ADMIN_ADDR="0.0.0.0:9090",
            SHUTDOWN_DEREGISTER_DELAY="1s",
        )


def test_a_private_admin_address_is_allowed_in_production() -> None:
    s = load(
        ChassisSettings,
        environ={},
        service="test",
        HALYARD_ENV="production",
        ADMIN_ADDR="127.0.0.1:9090",
    )
    assert s.admin_addr == "127.0.0.1:9090"


def test_deregister_delay_defaults_by_environment() -> None:
    """Zero in development: waiting three seconds for a load balancer that is
    not there makes every local restart feel broken."""
    dev = load(ChassisSettings, environ={}, service="test")
    prod = load(ChassisSettings, environ={}, service="test", HALYARD_ENV="staging")
    assert dev.shutdown_deregister_delay == timedelta(0)
    assert prod.shutdown_deregister_delay == timedelta(seconds=3)


def test_comma_separated_fields_do_not_need_json() -> None:
    """A complex field that fails to JSON-decode raises SettingsError, which
    aborts before validation and hides every other problem."""
    s = load(
        ChassisSettings,
        environ={},
        service="test",
        TRUSTED_PROXY_CIDRS="10.0.0.0/8, 192.168.0.0/16",
        OTEL_EXPORTER_OTLP_HEADERS="authorization=Bearer x, x-tenant=acme",
    )
    assert len(s.trusted_proxies) == 2
    assert s.otlp_headers["x-tenant"] == "acme"


def test_a_bad_complex_field_still_accumulates_with_a_scalar_one() -> None:
    """The regression test for the SettingsError path."""
    with pytest.raises(ConfigError) as caught:
        load(
            ChassisSettings,
            environ={},
            service="test",
            TRUSTED_PROXY_CIDRS="not-a-cidr",
            HTTP_MAX_HEADER_VALUES="0",
        )
    reported = {p.env for p in caught.value.problems}
    assert len(reported) >= 2, f"only reported {reported}"


def test_the_boot_line_fingerprints_secrets_rather_than_printing_them() -> None:
    resolved = load(
        ChassisSettings,
        environ={},
        service="test",
        OTEL_EXPORTER_OTLP_HEADERS=f"authorization=Bearer {LIVE_LOOKING_KEY}",
    ).resolved()
    assert LIVE_LOOKING_KEY not in str(resolved)
    assert resolved["OTEL_EXPORTER_OTLP_HEADERS"].startswith("set (sha256:")


TEST_DSN = "https://0123456789abcdef0123456789abcdef@o0.ingest.example.invalid/42"


def test_the_sentry_dsn_is_fingerprinted_not_printed() -> None:
    """Bound through the loader rather than left for an SDK to read, so it
    lands in the one boot line that records effective configuration -- which
    must stay comparable between two replicas and never enough to authenticate
    with."""
    s = load(ChassisSettings, environ={}, service="test", SENTRY_DSN=TEST_DSN)
    assert s.sentry_dsn == TEST_DSN, "the reporter needs the real value"
    resolved = s.resolved()
    assert "0123456789abcdef" not in str(resolved)
    assert resolved["SENTRY_DSN"].startswith("set (sha256:")


def test_no_sentry_dsn_resolves_to_unset() -> None:
    """Empty is the only switch the reporter has, in both languages."""
    s = load(ChassisSettings, environ={}, service="test")
    assert s.sentry_dsn == ""
    assert s.resolved()["SENTRY_DSN"] == "unset"


@pytest.mark.parametrize("bad", ["not-a-url", "ftp://key@example.invalid/1"])
def test_a_malformed_sentry_dsn_is_a_problem(bad: str) -> None:
    """A service that starts on a typo'd DSN reports nothing, forever,
    silently. Matches the Go loader's SecretURL check on the same key."""
    with pytest.raises(ConfigError) as exc:
        load(ChassisSettings, environ={}, service="test", SENTRY_DSN=bad)
    message = str(exc.value)
    assert "SENTRY_DSN" in message
    assert bad not in message, "a problem must describe the shape wanted, never the value"


def test_the_boot_line_renders_durations_the_way_go_does() -> None:
    resolved = load(ChassisSettings, environ={}, service="test").resolved()
    assert resolved["HTTP_READ_TIMEOUT"] == "30s"
    assert resolved["HTTP_IDLE_TIMEOUT"] == "2m0s"


@pytest.mark.parametrize(
    ("delta", "text"),
    [
        (timedelta(0), "0s"),
        (timedelta(seconds=30), "30s"),
        (timedelta(minutes=2), "2m0s"),
        (timedelta(milliseconds=500), "500ms"),
        (timedelta(hours=1, minutes=30), "1h30m0s"),
        (timedelta(microseconds=100), "100µs"),
    ],
)
def test_duration_formatting_matches_go(delta: timedelta, text: str) -> None:
    assert format_go_duration(delta) == text


def test_settings_are_not_a_singleton() -> None:
    """Two instances read the environment independently, so two tests in one
    session cannot see each other's configuration."""
    a = load(ChassisSettings, environ={}, service="a", HTTP_ADDR=":1111")
    b = load(ChassisSettings, environ={}, service="b", HTTP_ADDR=":2222")
    assert (a.addr, b.addr) == (":1111", ":2222")


def test_a_service_extends_the_settings() -> None:
    class ServiceSettings(ChassisSettings):
        database_url: str = ""

    s = load(ServiceSettings, environ={}, service="test", DATABASE_URL="postgres://x")
    assert s.database_url == "postgres://x"
    assert s.addr == ":8080"
