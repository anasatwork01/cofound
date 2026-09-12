#!/usr/bin/env bash
# Regenerate every language binding from packages/schema.
#
# SPEC 0 rule 2: the schemas are the single source of truth, and TypeScript, Go
# and Python are derived from them. Never hand-write the same type twice.
#
# Output is committed, not ignored, for two reasons: it lets CI diff a fresh
# generation against the tree to prove the two agree (`make gen-check`), and it
# means building a service does not require five code generators to be
# installed.
#
# This script must be deterministic. Anything that varies between runs — a
# timestamp, a map iteration order — turns the drift check into a coin flip
# that reviewers learn to re-run until it passes.
set -euo pipefail
cd "$(dirname "$0")/.."

SCHEMA=packages/schema
GEN=$SCHEMA/gen
TOOLS=$SCHEMA/tools
MODULE=github.com/anasatwork01/cofound/packages/schema
COMMON_PKG=$MODULE/gen/go/common

# JSON Schema documents, and the identifiers each maps to per language.
#            file                             go pkg        python module          ts file
JSON_DOCS=(
  "common.schema.json                         common        common                 common"
  "agent-events.schema.json                   agentevents   agent_events           agent-events"
  "capability-manifest.schema.json            capability    capability_manifest    capability-manifest"
  "meters.schema.json                         meters        meters                 meters"
)
#            file                    go pkg        python module    ts file
OPENAPI_DOCS=(
  "sandboxd.openapi.yaml             sandboxdapi   sandboxd_api     sandboxd-api"
  "api.openapi.yaml                  apiv1         api_v1           api-v1"
)

say() { printf '  %s\n' "$1"; }

# Prove every generator's toolchain runs BEFORE anything is deleted.
#
# The wipe below is what makes a removed schema take its bindings with it, and
# it is also what turns a broken toolchain into a broken repository: a generator
# that cannot start leaves the tree without the bindings that were committed,
# and the first symptom is an unrelated build failing on a missing package.
#
# Not hypothetical. Homebrew upgrading Go leaves a stale GOROOT exported from
# the shell profile, every `go` command then exits 2 with "cannot find GOROOT
# directory", and `make gen` deletes six Go packages, six Python modules and six
# TypeScript files on its way to reporting the failure.
#
# `make doctor` answers "is the toolchain the pinned one", which is the better
# question and a different one. This asks only whether each generator can run at
# all, because that is the part this script destroys the tree over.
for tool in "go version" "node --version" "uv --version"; do
  if ! $tool >/dev/null 2>&1; then
    echo "error: \`$tool\` failed, and generating empties $GEN before it starts." >&2
    echo "       Refusing to delete the committed bindings. Run 'make doctor'." >&2
    exit 1
  fi
done

# Regenerate from empty so a deleted schema takes its bindings with it — but
# keep gen/go/go.mod and gen/go/go.sum, which are checked-in infrastructure
# rather than generated output.
#
# They used to be rewritten here and reconciled with `go mod tidy`. That was
# wrong twice over: tidy needs the network, and with no require lines to pin
# against it resolves the *latest* version of each dependency, so a new release
# of oapi-codegen's runtime would fail the drift check on an unrelated pull
# request. Exactly the coin flip this script's header warns about.
#
# A schema change that introduces a new import now fails at `go build` with a
# message naming the missing module, which is a better place to find out.
rm -rf "$GEN/python" "$GEN/typescript" "$GEN/zod" "$GEN/README.md"
find "$GEN/go" -mindepth 1 -maxdepth 1 -type d -exec rm -rf {} +
mkdir -p "$GEN/go" "$GEN/python/halyard_schema" "$GEN/typescript" "$GEN/zod"

if [ ! -f "$GEN/go/go.mod" ]; then
  echo "error: $GEN/go/go.mod is missing. It is checked in, not generated." >&2
  exit 1
fi

# ------------------------------------------------------------------ go
say "go"
for row in "${JSON_DOCS[@]}"; do
  read -r doc pkg _ _ <<<"$row"
  mkdir -p "$GEN/go/$pkg"
  (cd "$TOOLS" && go tool go-jsonschema \
    --package "$pkg" \
    --output "../gen/go/$pkg/$pkg.gen.go" \
    "../$doc")
  say "    $pkg"
done

for row in "${OPENAPI_DOCS[@]}"; do
  read -r doc pkg _ _ <<<"$row"
  mkdir -p "$GEN/go/$pkg"
  (cd "$TOOLS" && go tool oapi-codegen \
    -generate types \
    -package "$pkg" \
    -import-mapping="./common.schema.json:$COMMON_PKG" \
    -o "../gen/go/$pkg/$pkg.gen.go" \
    "../$doc")
  say "    $pkg"
done

# go-jsonschema renders `format: date-time` at the top level as a DEFINED TYPE,
# `type Timestamp time.Time`. A defined type does not inherit its underlying
# type's methods, so Timestamp has neither MarshalJSON nor UnmarshalJSON: it
# encodes as `{}` — time.Time's fields are unexported — and decoding a
# timestamp string into one fails outright. Silent in Go, and on the wire it
# presents as a console that sorts by date and gets NaN.
#
# The methods are emitted here rather than hand-written into the package
# because this script deletes and rebuilds every directory under gen/go on each
# run, so a hand-written file there would not survive.
#
# The grep is an assertion, not a check. If a go-jsonschema upgrade ever starts
# emitting its own marshaller, or renames the type, appending ours would
# produce a duplicate-method compile error far from here — so fail at the point
# where the assumption broke.
if ! grep -q '^type Timestamp time\.Time$' "$GEN/go/common/common.gen.go"; then
  echo "error: gen/go/common no longer declares 'type Timestamp time.Time'." >&2
  echo "       go-jsonschema's output changed; revisit the JSON methods in scripts/gen.sh." >&2
  exit 1
fi
cat >"$GEN/go/common/timestamp.gen.go" <<'GO'
// Code generated by scripts/gen.sh. DO NOT EDIT.

package common

import "time"

// MarshalJSON renders RFC 3339 in UTC.
//
// UTC() is applied here rather than trusted from the caller because
// common.schema.json says "always UTC" and a time.Time carries whatever
// location it was built with — pgx hands back the session's, which is not
// necessarily UTC.
func (t Timestamp) MarshalJSON() ([]byte, error) {
	return time.Time(t).UTC().MarshalJSON()
}

// UnmarshalJSON accepts RFC 3339 and normalises to UTC.
func (t *Timestamp) UnmarshalJSON(data []byte) error {
	var v time.Time
	if err := v.UnmarshalJSON(data); err != nil {
		return err
	}
	*t = Timestamp(v.UTC())
	return nil
}

// Time returns the underlying instant, for callers that need to compare or
// format rather than serialise.
func (t Timestamp) Time() time.Time { return time.Time(t).UTC() }

// String renders the same text MarshalJSON does, without the quotes, so a log
// line or an error message shows an instant rather than a struct dump.
func (t Timestamp) String() string { return time.Time(t).UTC().Format(time.RFC3339Nano) }
GO

gofmt -w "$GEN/go"

# ------------------------------------------------------------------ python
say "python"
DMC_ARGS=(
  --output-model-type pydantic_v2.BaseModel
  --target-python-version 3.12
  --use-standard-collections
  --use-union-operator
  --use-title-as-name
  # Emit Annotated[str, Field(pattern=...)] rather than constr(...). With
  # `from __future__ import annotations` in the header, every annotation is a
  # string, and a bare constr() call inside one cannot be resolved when pydantic
  # builds the class — the model raises "is not fully defined" on first use.
  # Without this flag the generated models are unusable, which no test that
  # merely imports them would notice.
  --use-annotated
  # Without this every run rewrites a timestamp header and the drift check
  # fails on an unchanged schema.
  --disable-timestamp
  --formatters ruff-format
)
for row in "${JSON_DOCS[@]}" ; do
  read -r doc _ mod _ <<<"$row"
  uv run --quiet datamodel-codegen \
    --input "$SCHEMA/$doc" --input-file-type jsonschema \
    --output "$GEN/python/halyard_schema/$mod.py" "${DMC_ARGS[@]}"
  say "    $mod"
done
for row in "${OPENAPI_DOCS[@]}"; do
  read -r doc _ mod _ <<<"$row"
  uv run --quiet datamodel-codegen \
    --input "$SCHEMA/$doc" --input-file-type openapi \
    --output "$GEN/python/halyard_schema/$mod.py" "${DMC_ARGS[@]}"
  say "    $mod"
done
cat > "$GEN/python/halyard_schema/__init__.py" <<'EOF'
"""Generated by scripts/gen.sh. Edit packages/schema instead.

Importable as halyard_schema because packages/schema/pyproject.toml declares it
as a uv workspace member: the Python chassis renders wire errors with
halyard_schema.common.Error, and a module tree loaded by file path cannot be a
runtime dependency of anything.
"""
EOF
# PEP 561: without this, mypy in another package treats every import from
# halyard_schema as Any and the generated models stop checking anything.
touch "$GEN/python/halyard_schema/py.typed"

# ------------------------------------------------------------------ typescript
say "typescript"
for row in "${JSON_DOCS[@]}"; do
  read -r doc _ _ ts <<<"$row"
  npx --no-install json2ts \
    --input "$SCHEMA/$doc" \
    --output "$GEN/typescript/$ts.ts" \
    --cwd "$SCHEMA" \
    --additionalProperties false >/dev/null
  say "    $ts.ts"
done
for row in "${OPENAPI_DOCS[@]}"; do
  read -r doc _ _ ts <<<"$row"
  npx --no-install openapi-typescript "$SCHEMA/$doc" \
    -o "$GEN/typescript/$ts.ts" >/dev/null 2>&1
  say "    $ts.ts"
done

# ------------------------------------------------------------------ zod
# SPEC 3.1: "Forms: react-hook-form + zod, with zod schemas generated from
# packages/schema". Task 0.3 generated three languages and no zod, so the first
# console form had nothing to import.
#
# Not a fourth row in the tables above, because zod needs something the other
# three generators do not: json-schema-to-zod has no $ref support whatsoever and
# compiles a reference to `z.any()` without complaining. scripts/gen_zod.mjs
# resolves references itself and errors on one it cannot place. Its own header
# explains the rest, including why it generates for two documents rather than
# six.
say "zod"
node scripts/gen_zod.mjs

# ------------------------------------------------- observability vocabulary
# observability.json is deliberately absent from JSON_DOCS above: nothing
# serialises a document of that shape, so generating model types from it would
# produce dead code. What it needs instead is constants, in both languages, and
# those are emitted straight into the packages that own the identifiers rather
# than into gen/ — logkey.Service has to stay logkey.Service.
#
# Nothing is removed first. These four paths are rewritten in full on every run,
# and deleting them before regenerating would leave the tree missing files the
# Go build needs if the generator then failed.
say "observability"
uv run --quiet python scripts/gen_obs.py
gofmt -w $(./scripts/gen_obs.py --outputs | grep '\.go$')
uv run --quiet ruff format --quiet $(./scripts/gen_obs.py --outputs | grep '\.py$')

cat > "$GEN/README.md" <<'EOF'
# Generated bindings — do not edit

Every file here is produced by `scripts/gen.sh` from the schemas in
`packages/schema`. Editing one is pointless: the next `make gen` overwrites it,
and `make gen-check` fails the build in the meantime.

Change the schema, run `make gen`, and commit both together.

These are committed rather than ignored so that CI can prove the tree matches
the schemas, and so that building a service does not require five code
generators to be installed.

**Two exceptions:** `go/go.mod` and `go/go.sum` are checked in and
hand-maintained. `scripts/gen.sh` preserves them, because re-resolving those
versions on every run would make the drift check fail whenever a dependency
published a release. If generated code gains a new import, `go build` names the
missing module.
EOF
say "done"
