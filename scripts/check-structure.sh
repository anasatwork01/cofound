#!/usr/bin/env bash
# Assert the repo layout still matches SPEC §4. Cheap, and it catches the slow
# drift that makes a monorepo hard to navigate six months in.
set -euo pipefail
cd "$(dirname "$0")/.."

required_dirs=(
  apps/console
  services/api services/gitd services/aigw services/mcp
  services/sandboxd services/workers
  agent/opencode agent/patches agent/agentd agent/image agent/config
  capabilities/auth capabilities/payments capabilities/email capabilities/uploads
  templates
  packages/chassis packages/db packages/pychassis packages/schema packages/ui
  db/migrations
  infra/cloudflare infra/modal
  docs
)
required_files=(
  go.work pyproject.toml package.json pnpm-workspace.yaml Makefile
  .tool-versions .gitignore .editorconfig
  README.md CONTRIBUTING.md CLAUDE.md
  compose.yaml .github/workflows/ci.yml
  scripts/gen.sh scripts/gen_obs.py scripts/gen_zod.mjs
  packages/schema/common.schema.json
  packages/schema/agent-events.schema.json
  packages/schema/capability-manifest.schema.json
  packages/schema/meters.schema.json
  packages/schema/sandboxd.openapi.yaml
  packages/schema/api.openapi.yaml
  packages/schema/observability.json
  packages/schema/observability.schema.json
  scripts/doctor.sh scripts/check-structure.sh scripts/check-branch.sh
  scripts/tool-versions.sh
  .githooks/pre-push
  db/migrations/00001_tenancy.sql
  db/migrations/00013_row_level_security.sql
  docs/SPEC.md docs/TASKS.md docs/verified.md docs/open-questions.md
)

fail=0
for d in "${required_dirs[@]}"; do
  [ -d "$d" ] || { echo "  MISSING DIR   $d"; fail=1; }
done
for f in "${required_files[@]}"; do
  [ -f "$f" ] || { echo "  MISSING FILE  $f"; fail=1; }
done

# docs/SPEC.md is the contract every other file cites by section number, and it
# is committed verbatim from an external document. Two things can go wrong
# silently: the placeholder could come back, and a copy could be pasted through
# a UTF-8 -> Latin-1 round trip -- which is exactly what happened to the first
# copy, turning every "\u00a7" into two characters and destroying the
# architecture diagram. Both are cheap to detect and expensive to notice late.
if ! grep -q '^## 6\. Data model' docs/SPEC.md; then
  echo "  docs/SPEC.md has no section 6; is it still the placeholder?"
  echo "  Every §-citation in this repository resolves against that document."
  fail=1
fi
if grep -q 'Â' docs/SPEC.md; then
  echo "  docs/SPEC.md contains mojibake (Â). It went through a UTF-8 -> Latin-1"
  echo "  round trip. Recover with: python3 -c \"import io;"
  echo "  p='docs/SPEC.md';t=io.open(p,encoding='utf-8').read();"
  echo "  io.open(p,'w',encoding='utf-8').write(t.encode('latin-1').decode('utf-8'))\""
  fail=1
fi
if [ "$(grep -c '^## ' docs/SPEC.md)" -ne 24 ]; then
  echo "  docs/SPEC.md has $(grep -c '^## ' docs/SPEC.md) top-level sections, want 24."
  echo "  Section numbers are cited throughout the codebase; renumbering breaks them."
  fail=1
fi

# The zod schemas SPEC §3.1 puts console forms on. Listed literally rather than
# read out of scripts/gen_zod.mjs the way the gen_obs.py outputs below are: that
# script imports json-schema-to-zod, yaml and prettier at module scope, and this
# check runs in a CI job that installs no dependencies. The two lists are held
# together by packages/schema/tests/zod-generator.test.ts, which asserts this
# array against `gen_zod.mjs --outputs` in a job that does have them.
zod_outputs=(
  packages/schema/gen/zod/common.ts
  packages/schema/gen/zod/api-v1.ts
)
for out in "${zod_outputs[@]}"; do
  if [ ! -f "$out" ]; then
    echo "  MISSING FILE  $out (run 'make gen')"; fail=1
  elif ! head -5 "$out" | grep -qi 'DO NOT EDIT'; then
    echo "  $out is a scripts/gen_zod.mjs output but is not marked DO NOT EDIT"; fail=1
  fi
done

# Every file scripts/gen_obs.py writes must exist and must say so. A generated
# file that someone hand-edits and un-marks is a copy of the redaction denylist
# that has quietly stopped tracking the source document.
while read -r out; do
  if [ ! -f "$out" ]; then
    echo "  MISSING FILE  $out (run 'make gen')"; fail=1
  elif ! head -5 "$out" | grep -qi 'DO NOT EDIT'; then
    echo "  $out is a gen_obs.py output but is not marked DO NOT EDIT"; fail=1
  fi
done < <(./scripts/gen_obs.py --outputs)

# The migration runner uses goose as a LIBRARY, not its CLI: cmd/goose imports a
# driver for every database goose supports, which put ClickHouse, MySQL, MSSQL,
# Vertica and YDB in the dependency graph of a Postgres-only control plane. The
# distinction is invisible in a diff, so assert the outcome instead.
if [ -f packages/db/go.sum ]; then
  if grep -qiE "clickhouse|go-sql-driver/mysql|denisenkom|vertica|ydb-go-sdk" packages/db/go.sum; then
    echo "  packages/db has picked up a non-Postgres database driver."
    echo "  Almost certainly github.com/pressly/goose/v3/cmd/goose crept back in;"
    echo "  use the goose library from cmd/migrate instead. See docs/verified.md."
    fail=1
  fi
fi

# The agent submodule must stay pinned, never tracked on a moving branch (§11.1).
if [ -f .gitmodules ]; then
  if ! grep -q 'path = agent/opencode' .gitmodules; then
    echo "  agent/opencode is not registered as a submodule"; fail=1
  fi
  if grep -q 'branch =' .gitmodules; then
    echo "  .gitmodules tracks a branch; SPEC §11.1 requires a pinned tag"; fail=1
  fi
fi

# CI must read its toolchain versions from .tool-versions, never inline them.
if grep -nE '(go-version|node-version|python-version|version):[[:space:]]*[\x27"]?[0-9]+\.' \
     .github/workflows/ci.yml >/dev/null 2>&1; then
  echo "  .github/workflows/ci.yml hardcodes a toolchain version;"
  echo "  use scripts/tool-versions.sh so .tool-versions stays the only source"
  fail=1
fi

if [ "$fail" -ne 0 ]; then
  echo "structure check failed"
  exit 1
fi
echo "structure ok"
