"""Contract tests for packages/schema.

The valuable assertion here is that the **worked examples written into the
specification** validate against the schemas derived from it. A schema that
merely self-validates proves only that it is well-formed JSON Schema; checking
it against SPEC's own examples is what catches a paraphrase drifting from the
contract it claims to encode.

Two of those examples do not validate as written. That is deliberate and
asserted below rather than papered over — see
test_spec_7_2_examples_as_written_expose_two_deviations.
"""

import json
from pathlib import Path
from typing import Any

import pytest
from jsonschema import Draft202012Validator
from referencing import Registry, Resource

SCHEMA_DIR = Path(__file__).resolve().parents[2] / "packages" / "schema"

JSON_SCHEMAS = [
    "common.schema.json",
    "agent-events.schema.json",
    "capability-manifest.schema.json",
    "meters.schema.json",
]


def _load(name: str) -> dict[str, Any]:
    return json.loads((SCHEMA_DIR / name).read_text())


def _registry() -> Registry:
    """Resolve cross-file $refs the way the generators do: by relative filename."""
    registry = Registry()
    for name in JSON_SCHEMAS:
        doc = _load(name)
        resource = Resource.from_contents(doc)
        # Register under both the relative filename used in $ref and the $id.
        registry = resource @ registry
        registry = registry.with_resource(uri=name, resource=resource)
    return registry


def _validator(name: str) -> Draft202012Validator:
    return Draft202012Validator(_load(name), registry=_registry())


# --------------------------------------------------------------- well-formedness


@pytest.mark.parametrize("name", JSON_SCHEMAS)
def test_schema_is_valid_json_schema(name: str) -> None:
    Draft202012Validator.check_schema(_load(name))


@pytest.mark.parametrize("name", JSON_SCHEMAS)
def test_every_definition_is_titled(name: str) -> None:
    """Generators name classes from `title`.

    Without one, datamodel-codegen emits Type, Type1, Type2 and the contract
    becomes unreadable in Python. Enforced here so the failure lands on whoever
    adds an untitled definition rather than on whoever reads the output.
    """
    untitled = [k for k, v in _load(name).get("$defs", {}).items() if "title" not in v]
    assert untitled == [], f"{name}: $defs without a title: {untitled}"


def test_common_aliases_cover_every_definition() -> None:
    """common.schema.json exposes each type at two pointer depths.

    JSON Schema generators address `#/$defs/X`; oapi-codegen rejects that depth
    outright and needs `#/components/schemas/X`. The aliases are one-line $refs
    so there is still one definition of each type — but a new $def with no alias
    would be invisible to every OpenAPI consumer, silently. Hence this test.
    """
    common = _load("common.schema.json")
    defs = set(common["$defs"])
    aliases = set(common["components"]["schemas"])
    assert defs == aliases, (
        f"missing aliases: {sorted(defs - aliases)}; stale aliases: {sorted(aliases - defs)}"
    )
    for name, alias in common["components"]["schemas"].items():
        assert alias == {"$ref": f"#/$defs/{name}"}, (
            f"alias for {name} must be a bare $ref, not a second definition"
        )


# --------------------------------------------------------------- SPEC 7.2 events

# Transcribed verbatim from SPEC section 7.2.
SPEC_7_2_EXAMPLES: list[dict[str, Any]] = [
    {"type": "turn.started", "turn": 12, "hold": {"credits": 40}},
    {"type": "message.delta", "turn": 12, "text": "I'll build the page..."},
    {
        "type": "tool.started",
        "turn": 12,
        "id": "t1",
        "tool": "edit",
        "target": "app/pricing/page.tsx",
    },
    {
        "type": "tool.finished",
        "turn": 12,
        "id": "t1",
        "ok": True,
        "summary": "created",
        "diff_stat": {"add": 84, "del": 0},
    },
    {"type": "lease.changed", "holder": "agent"},
    {"type": "preview.status", "state": "rebuilding"},
    {
        "type": "usage.tick",
        "turn": 12,
        "tokens": {"in": 84000, "out": 3100, "cache_read": 22000, "cache_write": 0},
        "sandbox_seconds": 46,
    },
    {
        "type": "turn.finished",
        "turn": 12,
        "status": "done",
        "checkpoint_sha": "7d9e014",
        "credits": 31,
    },
    {"type": "error", "code": "budget_exceeded", "message": "...", "retriable": False},
]


def test_spec_7_2_examples_validate_once_corrected() -> None:
    """Every SPEC 7.2 example validates after the two known corrections.

    This is the real check that the schema encodes the contract rather than a
    paraphrase of it.
    """
    validator = _validator("agent-events.schema.json")
    for example in SPEC_7_2_EXAMPLES:
        corrected = dict(example)
        corrected.setdefault("turn", 12)
        for money in ("credits",):
            if money in corrected:
                corrected[money] = str(corrected[money])
        if "hold" in corrected:
            corrected["hold"] = {"credits": str(corrected["hold"]["credits"])}
        errors = list(validator.iter_errors(corrected))
        assert errors == [], f"{example['type']}: {[e.message for e in errors]}"


def _variant_for(tag: str) -> str:
    """Map an event's `type` tag to its $defs name, read from the schema itself."""
    for name, defn in _load("agent-events.schema.json")["$defs"].items():
        enum = defn.get("properties", {}).get("type", {}).get("enum")
        if enum == [tag]:
            return name
    raise AssertionError(f"no variant declares type {tag!r}")


def test_spec_7_2_examples_as_written_expose_two_deviations() -> None:
    """Pins the exact places SPEC 7.2's examples disagree with its own prose.

    1. `turn` is required. SPEC 7.2 states "every event carries `turn`" as a
       rule, but three of its examples omit it: `lease.changed`,
       `preview.status` and `error`. The normative sentence wins over the
       illustration, so the schema requires it — which means those three
       examples need correcting in SPEC.

    2. Credits are decimal strings, not JSON numbers. SPEC 6 stores them as
       numeric(14,4) and SPEC 16.1 demands a ledger rather than a counter;
       routing money through an IEEE-754 double loses that exactness silently.
       The `turn.started` and `turn.finished` examples show 40 and 31 as bare
       numbers.

    Asserted rather than fixed quietly so that whoever reconciles SPEC has a
    precise list, and so that a future schema change cannot make these pass by
    accident without someone reading this docstring.

    Each example is checked against its own variant rather than the top-level
    oneOf, because a oneOf failure reports only "not valid under any of the
    given schemas" and would let an unrelated regression masquerade as this
    known deviation.
    """
    events = _load("agent-events.schema.json")
    registry = _registry()

    def errors_for(example: dict[str, Any]) -> list[str]:
        schema = {**events["$defs"][_variant_for(example["type"])], "$defs": events["$defs"]}
        return [
            e.message for e in Draft202012Validator(schema, registry=registry).iter_errors(example)
        ]

    failing = {ex["type"]: errors_for(ex) for ex in SPEC_7_2_EXAMPLES if errors_for(ex)}

    missing_turn = {"lease.changed", "preview.status", "error"}
    numeric_credits = {"turn.started", "turn.finished"}
    assert set(failing) == missing_turn | numeric_credits, (
        f"the set of deviating examples changed: {sorted(failing)}"
    )
    for tag in missing_turn:
        assert any("'turn' is a required property" in m for m in failing[tag]), failing[tag]
    for tag in numeric_credits:
        assert any("is not of type 'string'" in m for m in failing[tag]), failing[tag]


def test_envelope_accepts_an_event_type_it_has_never_heard_of() -> None:
    """SPEC 7.2 and 18: unknown `type` values are ignored, not fatal.

    A relay decodes the envelope to route an event without knowing its variant,
    so the envelope must tolerate a type from a newer producer. This is the
    forward-compatibility guarantee the SSE gateway depends on.
    """
    events = _load("agent-events.schema.json")
    envelope = Draft202012Validator(
        {**events["$defs"]["AgentEventEnvelope"], "$defs": events["$defs"]},
        registry=_registry(),
    )
    future = {"type": "sandbox.migrated", "turn": 3, "region": "iad", "reason": "rebalance"}
    assert list(envelope.iter_errors(future)) == []


# ------------------------------------------------------- SPEC 7.4 capability


def test_spec_7_4_manifest_example_validates_verbatim() -> None:
    """The worked payments manifest from SPEC 7.4, unaltered."""
    example = {
        "id": "payments",
        "version": "2.4.0",
        "display_name": "Payments",
        "framework": {"next": ">=15 <16"},
        "credential_owner": "builder",
        "provision": {
            "kind": "stripe_connect",
            "completion": "hosted_onboarding",
            "produces": ["stripe_account_id"],
        },
        "dependencies": {"stripe": "^17.0.0"},
        "files": [
            {
                "path": "app/api/checkout/route.ts",
                "template": "checkout.route.ts.hbs",
                "on_conflict": "skip",
            },
            {
                "path": "app/api/stripe/webhook/route.ts",
                "template": "webhook.route.ts.hbs",
                "on_conflict": "skip",
            },
            {"path": "lib/stripe.ts", "template": "stripe.ts.hbs", "on_conflict": "overwrite"},
        ],
        "migrations": [
            {"id": "0001_subscriptions", "sql": "migrations/0001.sql", "additive": True}
        ],
        "env": [
            {
                "key": "STRIPE_PUBLISHABLE_KEY",
                "scope": "public",
                "from": "provision.publishable_key",
            },
            {
                "key": "STRIPE_SECRET_KEY",
                "scope": "server",
                "from": "provision.secret_key",
                "dev_variant": "test_mode",
            },
            {"key": "STRIPE_WEBHOOK_SECRET", "scope": "server", "from": "provision.webhook_secret"},
        ],
        "webhooks": [
            {
                "provider": "stripe",
                "relay_path": "/relay/{project}/payments",
                "events": ["checkout.session.completed", "customer.subscription.*"],
            }
        ],
        "smoke_check": {
            "kind": "http",
            "path": "/api/checkout",
            "method": "OPTIONS",
            "expect_status": 204,
        },
        "uninstall": {"remove_files": True, "remove_env": True, "drop_tables": False},
    }
    errors = list(_validator("capability-manifest.schema.json").iter_errors(example))
    assert errors == [], [e.message for e in errors]


def test_manifest_rejects_an_unknown_key() -> None:
    """A typo in a manifest must fail the install, not be ignored.

    Silently dropping an unrecognised key is how a capability ships with its
    smoke check disabled because someone wrote `smoke_checks`.
    """
    minimal = {
        "id": "auth",
        "version": "1.0.0",
        "display_name": "Auth",
        "framework": {"next": ">=15 <16"},
        "credential_owner": "none",
        "uninstall": {"remove_files": True, "remove_env": True, "drop_tables": False},
    }
    assert list(_validator("capability-manifest.schema.json").iter_errors(minimal)) == []
    assert (
        list(
            _validator("capability-manifest.schema.json").iter_errors(
                {**minimal, "smoke_checks": {}}
            )
        )
        != []
    )


# ------------------------------------------------------------------ SPEC 16 meters


def test_spec_16_2_meter_list_is_complete() -> None:
    """All sixteen meters from SPEC 16.2, including the four separate token meters.

    SPEC 16.2 is explicit that one combined "tokens" meter will misprice heavy
    sessions, because the four differ by roughly an order of magnitude in cost.
    """
    expected = {
        "tokens_in",
        "tokens_out",
        "tokens_cache_read",
        "tokens_cache_write",
        "sandbox_seconds",
        "build",
        "deploy",
        "egress_gb",
        "requests",
        "db_storage_gb",
        "db_compute_seconds",
        "ai_gateway_tokens",
        "seo_crawl_pages",
        "keyword_lookup",
        "rank_check",
        "domain_year",
    }
    assert set(_load("meters.schema.json")["$defs"]["Meter"]["enum"]) == expected


def test_usage_event_carries_units_and_cannot_carry_credits() -> None:
    """SPEC 16.1: units and prices are separate.

    Emitters write units; only the rating worker converts them to credits and
    stamps the price book version. An emitter that could submit a credit amount
    would bypass that, making a past invoice unanswerable.
    """
    event = {
        "source": "agentd",
        "event_key": "session-9f3a:turn-12:tokens_in",
        "org_id": "a3f1c2d4-0000-4000-8000-000000000000",
        "meter": "tokens_in",
        "quantity": "84000",
        "occurred_at": "2026-09-09T12:00:00Z",
    }
    schema = {"$ref": "meters.schema.json#/$defs/UsageEvent"}
    v = Draft202012Validator(schema, registry=_registry())
    assert list(v.iter_errors(event)) == []
    assert list(v.iter_errors({**event, "credits": "31"})) != [], (
        "UsageEvent must reject a credits field"
    )


# ------------------------------------------------------- generated bindings


def _generated_module(name: str) -> Any:
    """Import a generated pydantic module.

    An ordinary import: packages/schema/pyproject.toml declares gen/python as
    the `halyard_schema` distribution, so these are installed like any other
    workspace package. They used to be loaded by file path, which needed a
    sys.modules dance to stop pydantic raising "is not fully defined" -- the
    generated modules use `from __future__ import annotations`, so every
    annotation is a string and pydantic resolves them through
    sys.modules[cls.__module__].__dict__ while building each class. Making the
    package importable removed that whole workaround, and it had to happen
    anyway: the Python chassis renders wire errors with
    halyard_schema.common.Error, and a module tree loaded by file path cannot be
    a runtime dependency of anything.
    """
    import importlib

    return importlib.import_module(f"halyard_schema.{name}")


def test_generated_python_validates_a_spec_event() -> None:
    """The generated pydantic models enforce the schema, not just mirror its names.

    This is the end-to-end check on the pipeline: a schema that produces
    importable-but-permissive models would pass every test above while giving
    the Python services no protection at all.
    """
    import pydantic

    events = _generated_module("agent_events")

    ok = events.TurnFinished(
        type="turn.finished", turn=12, status="done", checkpoint_sha="7d9e014", credits="31"
    )
    assert ok.turn == 12

    # Credits are decimal strings; a float must be rejected rather than coerced.
    with pytest.raises(pydantic.ValidationError):
        events.TurnFinished(type="turn.finished", turn=12, status="done", credits=31.5)

    # A status outside SPEC 6's turns.status set must not slip through.
    with pytest.raises(pydantic.ValidationError):
        events.TurnFinished(type="turn.finished", turn=12, status="finished-ish")

    # `turn` is required on every event (SPEC 7.2).
    with pytest.raises(pydantic.ValidationError):
        events.TurnFinished(type="turn.finished", status="done")


def test_generated_python_enforces_the_manifest_contract() -> None:
    """A manifest missing a required field must fail before an install starts."""
    import pydantic

    capability = _generated_module("capability_manifest")

    manifest = capability.CapabilityManifest(
        id="auth",
        version="1.0.0",
        display_name="Auth",
        framework={"next": ">=15 <16"},
        credential_owner="none",
        uninstall={"remove_files": True, "remove_env": True, "drop_tables": False},
    )
    assert manifest.credential_owner.value == "none"

    # SPEC 13.2 ships builder-time credentials only in v1, but `runtime` is a
    # legal value in the type — the restriction is a catalogue policy, not a
    # schema one, and conflating them would block phase 7.
    assert "runtime" in {m.value for m in type(manifest.credential_owner)}

    with pytest.raises(pydantic.ValidationError):
        capability.CapabilityManifest(id="auth", version="1.0.0", display_name="Auth")


@pytest.mark.parametrize("name", JSON_SCHEMAS)
def test_every_required_property_is_also_defined(name: str) -> None:
    """`required` without a matching entry in `properties` is a silent hole.

    JSON Schema treats it as "this key must be present, with any value", so
    validation still passes — while every code generator omits the field
    entirely, because it only emits what `properties` declares. The result is a
    contract that validates correctly and generates types missing a required
    field.

    This is not hypothetical: `turn` was required on all nine agent events and
    defined on none of them, and only the test that exercises the *generated*
    models caught it.
    """
    holes: list[str] = []

    def walk(node: Any, path: str) -> None:
        if isinstance(node, dict):
            required = node.get("required")
            props = node.get("properties")
            if isinstance(required, list) and isinstance(props, dict):
                for key in required:
                    if key not in props:
                        holes.append(f"{path}.required[{key!r}]")
            for k, v in node.items():
                walk(v, f"{path}.{k}")
        elif isinstance(node, list):
            for i, v in enumerate(node):
                walk(v, f"{path}[{i}]")

    walk(_load(name), name)
    assert holes == [], f"required but undefined: {holes}"
