"""The tests that fail when the Go and Python chassis drift apart.

Three contracts cross the language boundary, and each has a different mechanism
here because each has a different single source of truth:

  * The observability vocabulary is GENERATED into both languages from
    packages/schema/observability.json, so drift is impossible and what is
    tested is that the generated files are actually the ones being imported.
  * The error catalogue lives in Go and is asserted against
    packages/chassis/errs/testdata/catalog.golden, which the Go tests also
    assert against. One golden file, two languages, so an edited message fails
    on whichever side was not edited.
  * The configuration key names live in packages/chassis/config/chassis.go and
    are parsed out of it here. A variable added on the Go side and not here
    fails this test, because one deployment manifest configures both and a
    Python service silently ignoring HTTP_READ_TIMEOUT is a manifest that lies.
"""

from __future__ import annotations

import re
from pathlib import Path

import pytest
from halyard_chassis import errors, settings
from halyard_chassis.obs import attrs, logkey, vocab

REPO = Path(__file__).resolve().parents[3]
GO_CONFIG = REPO / "packages/chassis/config/chassis.go"
GO_CATALOG_GOLDEN = REPO / "packages/chassis/errs/testdata/catalog.golden"
GO_REDACT = REPO / "packages/chassis/logging/vocab.gen.go"
GO_LOGKEY = REPO / "packages/chassis/logkey/keys.gen.go"
GO_ATTRS = REPO / "packages/chassis/telemetry/keys.gen.go"


def _go_string_list(source: str, name: str) -> list[str]:
    match = re.search(rf"var {name} = \[\]string\{{(.*?)\n\}}", source, re.S)
    assert match, f"{name} not found; the generator's Go output shape changed"
    return re.findall(r'"((?:[^"\\]|\\.)*)"', match.group(1))


# ------------------------------------------------------- generated vocabulary


def test_the_redaction_lists_are_the_same_in_both_languages() -> None:
    """The security-critical one.

    A denylist updated in Go and not in Python is a leak in whichever service
    was forgotten. These are generated from one document, so this test is
    really asserting that both generated files came from the same run.
    """
    go = GO_REDACT.read_text()

    # Order matters for these two: it is the order the generator emitted, and a
    # difference means the two files came from different runs of it.
    assert _go_string_list(go, "denySubstrings") == list(vocab.DENY_SUBSTRINGS)
    assert _go_string_list(go, "secretPrefixes") == list(vocab.SECRET_PREFIXES)

    # ALLOW_EXACT is a frozenset on the Python side -- it is only ever asked
    # "is this key in it" -- so order is not comparable and membership is what
    # the guarantee actually rests on.
    assert set(_go_string_list(go, "allowExact")) == set(vocab.ALLOW_EXACT)


def test_the_generated_files_came_from_the_source_document() -> None:
    """Closes the gap the frozenset comparison above leaves open.

    Both languages are checked against the JSON directly, so a hand-edit to
    either generated file is caught even where set comparison would not notice.
    """
    import json

    doc = json.loads((REPO / "packages/schema/observability.json").read_text())
    red = doc["redaction"]
    assert list(vocab.DENY_SUBSTRINGS) == red["deny_substrings"]["values"]
    assert sorted(vocab.ALLOW_EXACT) == sorted(red["allow_exact"]["values"])
    assert list(vocab.SECRET_PREFIXES) == [
        v for g in red["secret_prefixes"]["groups"] for v in g["values"]
    ]
    assert list(logkey.LOG_KEYS) == [k["key"] for g in doc["log_keys"]["groups"] for k in g["keys"]]


def test_the_replacement_strings_are_the_same() -> None:
    go = GO_REDACT.read_text()
    assert f'const Redacted = "{vocab.REDACTED}"' in go
    assert f'const SuppressedMessage = "{vocab.SUPPRESSED_MESSAGE}"' in go


def test_the_log_field_names_are_the_same() -> None:
    """If Go writes org_id and Python writes orgId, the SPEC 17.3 log search
    across five hops silently returns half the story."""
    go = GO_LOGKEY.read_text()
    # \s+= rather than " = ": gofmt aligns a const block, so all but the
    # longest name is followed by padding. A regex without it matches only the
    # last line and the test passes for the wrong reason.
    go_values = re.findall(r'^\t\w+\s+= "([^"]*)"$', go, re.M)
    assert len(go_values) == len(logkey.LOG_KEYS), (
        f"parsed {len(go_values)} Go constants but Python has {len(logkey.LOG_KEYS)}; "
        "if the Go count is small the regex stopped matching, not the vocabulary"
    )
    assert go_values == list(logkey.LOG_KEYS)


def test_the_span_and_baggage_keys_are_the_same() -> None:
    """If the keys differ, the two halves of a trace are two traces."""
    go = GO_ATTRS.read_text()
    go_span = re.findall(r'^\tKey\w+\s+= attribute\.Key\("([^"]*)"\)$', go, re.M)
    go_baggage = re.findall(r'^\tBaggage\w+\s+= "([^"]*)"$', go, re.M)
    py_span = [
        attrs.SPAN_ORG_ID,
        attrs.SPAN_PROJECT_ID,
        attrs.SPAN_SESSION_ID,
        attrs.SPAN_TURN,
        attrs.SPAN_REQUEST_ID,
        attrs.SPAN_ERROR_CODE,
        attrs.SPAN_STREAM_KIND,
    ]
    assert len(go_span) == len(py_span), f"parsed only {len(go_span)} span keys from Go"
    assert go_span == py_span
    assert go_baggage == [member for member, _ in attrs.BAGGAGE_MEMBERS]


def test_every_baggage_member_has_a_log_field() -> None:
    """Otherwise an inbound tenancy tuple reaches spans but not the log search."""
    members = {member for member, _ in attrs.BAGGAGE_MEMBERS}
    paired = {member for member, _ in attrs.BAGGAGE_LOG_FIELDS}
    assert members == paired


# ---------------------------------------------------------- error catalogue


def test_the_error_catalogue_matches_the_go_golden_file() -> None:
    """One golden file, two languages.

    The console branches on `code` and shows `message` and `fix` to the user. It
    must not matter which language served the request, so an edit on one side
    that is not made on the other fails here.
    """
    assert GO_CATALOG_GOLDEN.exists(), f"{GO_CATALOG_GOLDEN} missing"
    assert errors.CHASSIS.dump() == GO_CATALOG_GOLDEN.read_text()


def test_client_closed_is_absent_from_the_catalogue() -> None:
    """It is constructed directly and never rendered, so it is not a row."""
    assert errors.CHASSIS.lookup(errors.CODE_CLIENT_CLOSED) is None
    assert errors.client_closed().status == 499


def test_a_service_extends_the_catalogue_without_mutating_it() -> None:
    """Two services in one test session must not see each other's codes."""
    extended = errors.CHASSIS.extend(
        errors.Entry("turn_running", 409, False, "The agent is mid-turn.", "Wait.")
    )
    assert extended.lookup("turn_running") is not None
    assert errors.CHASSIS.lookup("turn_running") is None


# ------------------------------------------------------- configuration keys


def _go_config_keys() -> set[str]:
    source = GO_CONFIG.read_text()
    keys = set(re.findall(r'^\tKey\w+\s+= "([A-Z0-9_]+)"$', source, re.M))
    assert keys, (
        f"no key constants parsed from {GO_CONFIG}. The const block's shape "
        "changed; fix this parser rather than deleting the test, or the two "
        "chassis can start reading different variables."
    )
    return keys


def test_python_reads_every_variable_the_go_chassis_reads() -> None:
    """One manifest configures both languages.

    A Python service that silently ignores SHUTDOWN_BUDGET while the Go service
    honours it is worse than one that fails to start: the manifest looks
    correct, and only one of the two behaves that way.
    """
    go_keys = _go_config_keys()
    py_keys = set(settings.env_names(settings.ChassisSettings).values())
    missing = sorted(go_keys - py_keys)
    assert missing == [], (
        f"the Go chassis reads {missing} and this one does not. Add the fields "
        "to ChassisSettings with matching aliases and defaults."
    )


def test_python_invents_no_variable_of_its_own() -> None:
    """The reverse direction. A variable only Python reads is a variable an
    operator will set on one service and wonder about on the other."""
    go_keys = _go_config_keys()
    py_keys = set(settings.env_names(settings.ChassisSettings).values())
    # service/version/commit come from the build, not the environment.
    build_time = {"SERVICE", "VERSION", "COMMIT"}
    extra = sorted(py_keys - go_keys - build_time)
    assert extra == [], f"only the Python chassis reads {extra}"


@pytest.mark.parametrize(
    ("text", "seconds"),
    [
        ("30s", 30.0),
        ("500ms", 0.5),
        ("2m", 120.0),
        ("1h30m", 5400.0),
        ("2h45m", 9900.0),
        ("-1.5h", -5400.0),
        ("0", 0.0),
        ("100us", 0.0001),
    ],
)
def test_go_duration_strings_parse(text: str, seconds: float) -> None:
    """Every duration in the Go chassis's documented defaults must parse here.

    timedelta rejects all of these on its own, and both chassis read the same
    variables from the same manifests.
    """
    parsed = settings.parse_go_duration(text)
    assert parsed.total_seconds() == pytest.approx(seconds)


@pytest.mark.parametrize("text", ["30 seconds", "30x", "abc", "PT30S"])
def test_a_non_go_duration_is_passed_through(text: str) -> None:
    """So pydantic renders the error for a genuinely malformed value, and so ISO
    8601 still works."""
    assert settings.parse_go_duration(text) == text


@pytest.mark.parametrize(
    ("text", "value"),
    [("1048576", 1048576), ("1MB", 1 << 20), ("512KB", 512 << 10), ("2GB", 2 << 30), ("64b", 64)],
)
def test_byte_counts_use_binary_multipliers(text: str, value: int) -> None:
    """KB is 1024 in both languages, or HTTP_MAX_BODY_BYTES=1MB admits a
    different body size depending on which service received the request."""
    assert settings.parse_bytes(text) == value
