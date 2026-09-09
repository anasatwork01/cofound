#!/usr/bin/env bash
# Verify the local toolchain matches .tool-versions. Exits non-zero on drift so
# that "works on my machine" is caught before it reaches a reviewer.
set -euo pipefail
cd "$(dirname "$0")/.."

fail=0
red() { printf '\033[31m%s\033[0m\n' "$1"; }
green() { printf '\033[32m%s\033[0m\n' "$1"; }

want() { awk -v k="$1" '$1 == k { print $2 }' .tool-versions; }

# semver >= comparison, on major.minor.patch
gte() {
  [ "$1" = "$2" ] && return 0
  [ "$(printf '%s\n%s\n' "$1" "$2" | sort -V | head -1)" = "$2" ]
}

check() {
  local name="$1" got="$2" wanted="$3"
  if [ -z "$got" ]; then
    red "  MISSING  $name (want >= $wanted)"
    fail=1
  elif gte "$got" "$wanted"; then
    green "  ok       $name $got (want >= $wanted)"
  else
    red "  TOO OLD  $name $got (want >= $wanted)"
    fail=1
  fi
}

echo "toolchain:"

# A hardcoded GOROOT export survives a Go upgrade and then breaks every single
# go command with "cannot find GOROOT directory" — an error that looks nothing
# like its cause, and which makes the version probe below report "MISSING go".
# Name it explicitly so nobody loses an afternoon to it.
if [ -n "${GOROOT:-}" ] && [ ! -d "$GOROOT" ]; then
  red "  BROKEN   GOROOT is set to $GOROOT, which does not exist"
  echo "           A Go upgrade moved it. Unset GOROOT: the go binary locates its"
  echo "           own runtime, so the variable is unnecessary and version-fragile."
  echo "           Check your shell profile for a hardcoded 'export GOROOT=...'."
  fail=1
elif ! go version >/dev/null 2>&1; then
  red "  BROKEN   go is on PATH but does not run: $(go version 2>&1 | head -1)"
  fail=1
fi

check go     "$(go version 2>/dev/null | awk '{print $3}' | tr -d go)" "$(want golang)"
check node   "$(node --version 2>/dev/null | tr -d v)"                 "$(want nodejs)"
check pnpm   "$(pnpm --version 2>/dev/null)"                           "$(want pnpm)"
check python "$(python3 --version 2>/dev/null | awk '{print $2}')"     "$(want python)"
check uv     "$(uv --version 2>/dev/null | awk '{print $2}')"          "0.4.0"
check git    "$(git --version 2>/dev/null | awk '{print $3}')"         "2.40.0"
check docker "$(docker --version 2>/dev/null | awk '{print $3}' | tr -d ,)" "24.0.0"

if [ "$fail" -ne 0 ]; then
  echo
  red "toolchain drift. Install the pinned versions (mise install / asdf install, or nvm use)."
  exit 1
fi
