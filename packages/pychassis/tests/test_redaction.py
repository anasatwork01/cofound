"""Proof that SPEC 17.3 redaction cannot be bypassed.

Every test here asserts against the raw bytes the real production handler chain
wrote, not against a parsed view that might normalise something away and not
against a stub that might not share the behaviour under test.

The most important test in the file is
`test_a_third_party_logger_is_also_redacted`. A redaction test that only logs
from application code passes on a chassis whose redaction is attached to the
root LOGGER rather than to a handler -- which is broken for every record from
httpx, uvicorn and asyncpg. That is the failure this file exists to catch.
"""

from __future__ import annotations

import logging
from collections.abc import Iterator

import pytest
from halyard_chassis import logs
from halyard_chassis.obs import logkey, vocab
from halyard_chassis.obs.redaction import is_secret_key

# Built at runtime, never written down. A literal shaped this much like a real
# Stripe key trips GitHub's push protection -- it validates shape, not liveness
# -- and asking it to allowlist a fake would blunt a tool that is doing its job.
# Deriving it from the generated prefix list is better anyway: the fixture
# follows the vocabulary instead of duplicating one entry from it.
LIVE_LOOKING_KEY = vocab.SECRET_PREFIXES[0] + "4eC39HqLyjWDarjtT1zdp7dc"
PASSWORD = "hunter2-correct-horse"


@pytest.fixture
def sink() -> Iterator[logs.Sink]:
    s = logs.Sink()
    logs.configure(logs.LogConfig(service="test", env="test", level="info", out=s))
    yield s
    logs.reset()


def test_a_secret_keyed_field_is_redacted(sink: logs.Sink) -> None:
    logging.getLogger("app").info("saved", extra={"api_token": LIVE_LOOKING_KEY})
    assert not sink.contains(LIVE_LOOKING_KEY)
    assert sink.last()["api_token"] == vocab.REDACTED


def test_a_third_party_logger_is_also_redacted(sink: logs.Sink) -> None:
    """The test that fails on a Filter-based implementation.

    Logger filters are not consulted for ancestor loggers, so a filter on root
    never sees this record. Handler filters and Handler.handle do.
    """
    logging.getLogger("httpx._client").warning("auth failed", extra={"password": PASSWORD})
    assert not sink.contains(PASSWORD)
    assert sink.last()["password"] == vocab.REDACTED


def test_a_lazily_interpolated_secret_is_caught(sink: logs.Sink) -> None:
    """The secret lives in record.args, not record.msg, until getMessage runs."""
    logging.getLogger("app").info("using token=%s", LIVE_LOOKING_KEY)
    assert not sink.contains(LIVE_LOOKING_KEY)
    assert sink.last()["msg"] == vocab.SUPPRESSED_MESSAGE
    assert sink.last()["suppressed_pattern"] == "sk_live_"


def test_a_credential_in_the_message_is_suppressed_with_its_pattern(sink: logs.Sink) -> None:
    logging.getLogger("app").error(f"connect failed: postgres://u:{PASSWORD}@db/x")
    assert not sink.contains(PASSWORD)
    line = sink.last()
    assert line["msg"] == vocab.SUPPRESSED_MESSAGE
    assert line["suppressed_pattern"] == "postgres://"


def test_a_secret_nested_under_a_clean_key_is_caught(sink: logs.Sink) -> None:
    """A mapping is slog's group: an inner secret key is still redacted."""
    logging.getLogger("app").info(
        "call", extra={"request": {"url": "/x", "authorization": "Bearer abc"}}
    )
    assert not sink.contains("Bearer abc")
    assert sink.last()["request"]["authorization"] == vocab.REDACTED
    assert sink.last()["request"]["url"] == "/x"


def test_a_secret_shaped_outer_key_redacts_the_whole_subtree(sink: logs.Sink) -> None:
    logging.getLogger("app").info("call", extra={"credentials": {"user": "amy", "pw": "s3cret"}})
    assert not sink.contains("amy")
    assert sink.last()["credentials"] == vocab.REDACTED


def test_an_objects_str_is_scanned(sink: logs.Sink) -> None:
    class Opaque:
        def __str__(self) -> str:
            return f"Token({LIVE_LOOKING_KEY})"

    logging.getLogger("app").info("built", extra={"client": Opaque()})
    assert not sink.contains(LIVE_LOOKING_KEY)


def test_a_traceback_is_redacted(sink: logs.Sink) -> None:
    """Locals are not rendered, but the exception's own str() is."""
    try:
        raise RuntimeError(f"bad key {LIVE_LOOKING_KEY}")
    except RuntimeError:
        logging.getLogger("app").exception("failed")
    assert not sink.contains(LIVE_LOOKING_KEY)


def test_user_content_is_hidden_above_debug(sink: logs.Sink) -> None:
    logging.getLogger("app").info("turn", extra={"prompt": logs.prompt("build me a CRM")})
    assert not sink.contains("build me a CRM")
    assert sink.last()["prompt"] == logs.OMITTED_USER_CONTENT


def test_user_content_is_revealed_at_debug() -> None:
    """Deliberately. That is a privacy action, not a verbosity tweak."""
    s = logs.Sink()
    logs.configure(logs.LogConfig(service="test", level="debug", out=s))
    try:
        logging.getLogger("app").debug("turn", extra={"prompt": logs.prompt("build me a CRM")})
        assert s.last()["prompt"] == "build me a CRM"
    finally:
        logs.reset()


def test_user_content_fails_closed_through_a_foreign_formatter() -> None:
    """The marker renders itself even where the chassis is not involved."""
    assert str(logs.user("secret plan")) == logs.OMITTED_USER_CONTENT
    assert f"{logs.user('secret plan')}" == logs.OMITTED_USER_CONTENT
    assert repr(logs.user("secret plan")) == logs.OMITTED_USER_CONTENT


def test_the_allowlist_covers_the_whole_log_vocabulary() -> None:
    """The audit that stops the denylist from eating a field a debugger needed.

    Every key in the closed vocabulary must survive classification. Without
    this, "key" alone eats idempotency_key and "auth" eats author -- and the
    field simply goes missing from the logs, which nobody notices until they
    need it.
    """
    swallowed = [k for k in logkey.LOG_KEYS if is_secret_key(k)]
    assert swallowed == [], (
        f"these log field names are classified as secrets and would be redacted: {swallowed}. "
        "Add them to redaction.allow_exact in packages/schema/observability.json."
    )


def test_a_handler_added_by_someone_else_also_redacts(sink: logs.Sink) -> None:
    """The property that survives logging.basicConfig(force=True).

    A second root handler is wrapped rather than refused, so a debugger or a
    log-shipping sidecar still works -- and still cannot emit a secret.
    """
    import io

    foreign_stream = io.StringIO()
    logging.getLogger().addHandler(logging.StreamHandler(foreign_stream))

    # A secret in the MESSAGE, because the default formatter renders the message
    # and not the extras -- so this is the path where a foreign handler could
    # actually print one.
    logging.getLogger("app").info("saving token=%s", LIVE_LOOKING_KEY)

    assert LIVE_LOOKING_KEY not in foreign_stream.getvalue()
    assert vocab.SUPPRESSED_MESSAGE in foreign_stream.getvalue()
    assert not sink.contains(LIVE_LOOKING_KEY)


def test_basic_config_force_cannot_uninstall_redaction(sink: logs.Sink) -> None:
    import io

    stream = io.StringIO()
    logging.basicConfig(force=True, stream=stream)
    logging.getLogger("third.party").warning("token=%s", LIVE_LOOKING_KEY)
    assert LIVE_LOOKING_KEY not in stream.getvalue()


def test_last_resort_is_removed(sink: logs.Sink) -> None:
    """A library setting propagate=False without a handler would otherwise write
    straight to stderr with the default formatter, around all of this."""
    assert logging.lastResort is None
    assert logging.raiseExceptions is False


def test_a_non_propagating_logger_is_adopted() -> None:
    """uvicorn sets propagate=False on uvicorn.access.

    With lastResort removed, such a logger's records would simply vanish; before
    it was removed they went to raw stderr around all redaction. Adopting is
    better than either, and better than the boot warning this replaced.
    """
    orphan = logging.getLogger("some.library.that.went.solo")
    orphan.propagate = False
    orphan.addHandler(logging.NullHandler())

    s = logs.Sink()
    logs.configure(logs.LogConfig(service="test", level="info", out=s))
    try:
        assert orphan.propagate is True
        orphan.warning("token=%s", LIVE_LOOKING_KEY)
        assert not s.contains(LIVE_LOOKING_KEY)
        assert s.last()["msg"] == vocab.SUPPRESSED_MESSAGE
    finally:
        logs.reset()
        orphan.handlers.clear()


def test_a_library_handler_is_kept_but_made_to_redact() -> None:
    """A library that genuinely needed its own sink keeps it -- redacting."""
    import io

    stream = io.StringIO()
    library = logging.getLogger("library.with.its.own.sink")
    library.addHandler(logging.StreamHandler(stream))

    s = logs.Sink()
    logs.configure(logs.LogConfig(service="test", level="info", out=s))
    try:
        library.warning("token=%s", LIVE_LOOKING_KEY)
        assert library.handlers, "the library's own handler was discarded"
        assert LIVE_LOOKING_KEY not in stream.getvalue()
        assert vocab.SUPPRESSED_MESSAGE in stream.getvalue()
    finally:
        logs.reset()
        library.handlers.clear()
