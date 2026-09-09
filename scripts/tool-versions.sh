#!/usr/bin/env bash
# Emit .tool-versions as key=value lines.
#
#   ./scripts/tool-versions.sh >> "$GITHUB_OUTPUT"
#
# This is what keeps CI and `make doctor` reading the same pins. Duplicating a
# version into a workflow file is how CI and local development silently diverge.
set -euo pipefail
cd "$(dirname "$0")/.."
awk '!/^[[:space:]]*#/ && NF >= 2 { print $1 "=" $2 }' .tool-versions
