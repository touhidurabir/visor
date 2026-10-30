#!/usr/bin/env bash
# Fails if any core package under internal/ (everything except internal/app)
# depends on Wails. Only internal/app may import github.com/wailsapp.
# Also fails if `go list` itself fails, so a broken toolchain or package
# can never turn the check green.
set -euo pipefail
cd "$(dirname "$0")/.."

all=$(go list ./internal/...)

# Portable (bash 3.2) loop instead of mapfile.
pkgs=()
while IFS= read -r p; do
  case "$p" in
    */internal/app | */internal/app/*) ;;
    "") ;;
    *) pkgs+=("$p") ;;
  esac
done <<<"$all"

if [ "${#pkgs[@]}" -eq 0 ]; then
  echo "check-core-imports: no core packages found" >&2
  exit 1
fi

deps=$(go list -deps "${pkgs[@]}")

bad=$(grep '^github.com/wailsapp/' <<<"$deps" || true)
if [ -n "$bad" ]; then
  echo "check-core-imports: core packages must not depend on Wails:" >&2
  echo "$bad" >&2
  exit 1
fi
