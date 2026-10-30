#!/usr/bin/env bash
# Fails if any core package under internal/ (everything except internal/app)
# depends on Wails. Only internal/app may import github.com/wailsapp.
set -euo pipefail
cd "$(dirname "$0")/.."

pkgs=$(go list ./internal/... | grep -v '/internal/app$' | grep -v '/internal/app/' || true)
if [ -z "$pkgs" ]; then
  echo "check-core-imports: no core packages found" >&2
  exit 1
fi

# shellcheck disable=SC2086
if bad=$(go list -deps $pkgs | grep 'github.com/wailsapp'); then
  echo "check-core-imports: core packages must not depend on Wails:" >&2
  echo "$bad" >&2
  exit 1
fi
