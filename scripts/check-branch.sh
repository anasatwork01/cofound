#!/usr/bin/env bash
# Validate a branch name against the conventions in CONTRIBUTING.md.
# Usage: check-branch.sh [branch]   (defaults to the current branch)
set -euo pipefail

branch="${1:-$(git rev-parse --abbrev-ref HEAD)}"

# Long-lived branches that are not feature work, plus automation that names
# its own branches. Dependabot uses dependabot/<ecosystem>/<dep>-<version>, so
# without this exemption its PRs would fail the check they are meant to pass.
case "$branch" in
  main|HEAD)      echo "  $branch: protected branch, not a work branch"; exit 0 ;;
  dependabot/*)   echo "  ok  $branch (automation)"; exit 0 ;;
esac

if [[ "$branch" =~ ^(feature|fix|update)/[a-z0-9][a-z0-9._-]*$ ]]; then
  echo "  ok  $branch"
  exit 0
fi

cat >&2 <<MSG
  bad branch name: $branch

  Expected one of:
    feature/<name>   new capability          e.g. feature/0.2-ci-pipeline
    fix/<name>       fixing broken behaviour e.g. fix/hold-ttl-never-expires
    update/<name>    docs, deps, config      e.g. update/git-conventions

  <name> is lowercase kebab-case (a-z 0-9 . _ -) and describes the change,
  not the file. Lead with the task ID from docs/TASKS.md when there is one.

  Rename with:  git branch -m <new-name>
  See CONTRIBUTING.md.
MSG
exit 1
