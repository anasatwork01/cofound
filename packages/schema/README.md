# packages/schema

**The single source of truth for every cross-language type.** SPEC 0 rule 2:
contracts before implementations, and never hand-write the same type twice.

TypeScript, Go and Python are _generated_ from the documents here by
`make gen`. Change a schema, run `make gen`, commit both together. `make verify`
runs `gen-check`, which regenerates and fails if the tree disagrees.

## Documents

| File                              | Contract                                                                                                                        | SPEC         |
| --------------------------------- | ------------------------------------------------------------------------------------------------------------------------------- | ------------ |
| `common.schema.json`              | Primitives shared by everything: Uuid, Timestamp, Slug, GitSha, Credits, Role, ActorKind, the error envelope, cursor pagination | 6, 8, 10, 18 |
| `agent-events.schema.json`        | The agent event stream `agentd` emits and `api` relays over SSE                                                                 | 7.2          |
| `capability-manifest.schema.json` | Capability module manifests                                                                                                     | 7.4, 13      |
| `meters.schema.json`              | Meters, price books, usage events                                                                                               | 6, 16        |
| `sandboxd.openapi.yaml`           | The Go/Python boundary — the one place the two-language split would otherwise drift                                             | 3.2, 9       |
| `api.openapi.yaml`                | The public `/v1` REST surface, phase 0 and 1 only                                                                               | 7.1          |

## Why definitions appear at two pointer depths

`common.schema.json` keeps its definitions in `$defs` and aliases each one under
`components/schemas` as a one-line `$ref`.

This is not redundancy. JSON Schema generators address `#/$defs/X`, while
`oapi-codegen` rejects that outright — it accepts only depth-4 pointers such as
`#/components/schemas/X`. The aliases satisfy both, so there is still exactly
one definition of each type, and the OpenAPI documents reuse the very same
generated Go types through `-import-mapping` rather than redeclaring them.

`test_common_aliases_cover_every_definition` fails if a new `$def` has no alias,
because an unaliased type is invisible to every OpenAPI consumer and would fail
silently.

## Scope of `api.openapi.yaml`

SPEC 7.1 lists every path in the product but specifies request and response
bodies for none of them. This file therefore covers only the endpoints whose
payloads the specification actually determines. Later phases add their own in
the task that implements them; writing speculative bodies for the phase 6 ads
endpoints now would be inventing an API surface, which SPEC 0 rule 5 forbids.

## Generators

Pinned so output is reproducible. Go tools are `tool` directives in
`tools/go.mod`; the rest come from `pnpm install` and `uv sync`.

| Target                       | Tool                                    |
| ---------------------------- | --------------------------------------- |
| Go, from JSON Schema         | `go-jsonschema`                         |
| Go, from OpenAPI             | `oapi-codegen` (types only)             |
| Python                       | `datamodel-code-generator`, pydantic v2 |
| TypeScript, from JSON Schema | `json-schema-to-typescript`             |
| TypeScript, from OpenAPI     | `openapi-typescript`                    |

`scripts/gen.sh` must stay deterministic: anything that varies between runs
turns the drift check into a coin flip that reviewers learn to re-run until it
passes. `--disable-timestamp` is load-bearing for exactly that reason.
