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
  packages/chassis packages/pychassis packages/schema packages/ui
  db/migrations
  infra/cloudflare infra/modal
  docs
)
required_files=(
  go.work pyproject.toml package.json pnpm-workspace.yaml Makefile
  .tool-versions .gitignore .editorconfig
  README.md CONTRIBUTING.md CLAUDE.md
  compose.yaml .github/workflows/ci.yml
  scripts/gen.sh scripts/gen_obs.py
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
