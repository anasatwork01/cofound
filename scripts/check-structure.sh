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
  packages/chassis packages/schema packages/ui
  db/migrations
  infra/cloudflare infra/modal
  docs
)
required_files=(
  go.work pyproject.toml package.json pnpm-workspace.yaml Makefile
  .tool-versions .gitignore .editorconfig
  README.md CONTRIBUTING.md CLAUDE.md
  compose.yaml .github/workflows/ci.yml
  scripts/gen.sh
  packages/schema/common.schema.json
  packages/schema/agent-events.schema.json
  packages/schema/capability-manifest.schema.json
  packages/schema/meters.schema.json
  packages/schema/sandboxd.openapi.yaml
  packages/schema/api.openapi.yaml
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
