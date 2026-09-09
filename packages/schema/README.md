# packages/schema

**The single source of truth for every cross-language type.** JSON Schema and
OpenAPI live here; TypeScript, Go and Python types are _generated_ from them
into `packages/schema/gen/` (git-ignored, rebuilt with `make gen`).

SPEC §0 rule 2: never hand-write the same type twice.

Planned contents:

- `agent-events.schema.json` - SPEC §7.2 agent event stream
- `capability-manifest.schema.json` - SPEC §7.4
- `sandboxd.openapi.yaml` - the Go <-> Python interface (SPEC §3.2)
- `api.openapi.yaml` - the `/v1` REST surface (SPEC §7.1)
- `meters.schema.json` + price book items (SPEC §16.2)

CI must fail if generated output drifts from the schemas. Task 0.3.
