#!/usr/bin/env bash
# Starts the e2e sandbox supervisord and the server-mode Visor against it.
# Playwright runs this as its webServer and stops it with SIGTERM; the trap
# then stops Visor and tears the sandbox down, so nothing outlives a run.
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
BIN=$ROOT/bin/visor-e2e
export VISOR_SANDBOX_DIR=/tmp/visor-e2e

if [[ ! -x $BIN ]]; then
  echo "bin/visor-e2e is missing. Build it first: go build -tags server -o bin/visor-e2e . (or run: task test:e2e)" >&2
  exit 1
fi

pid=
cleanup() {
  trap - EXIT TERM INT HUP
  [[ -n $pid ]] && kill "$pid" 2>/dev/null || true
  "$ROOT/scripts/sandbox.sh" down >/dev/null 2>&1 || true
}
trap cleanup EXIT
trap 'exit 143' TERM INT HUP

"$ROOT/scripts/sandbox.sh" up
VISOR_CONFIG=$VISOR_SANDBOX_DIR/supervisord.conf \
  VISOR_STATE_DIR=$VISOR_SANDBOX_DIR/state \
  WAILS_SERVER_HOST=127.0.0.1 \
  WAILS_SERVER_PORT=8766 \
  "$BIN" 2> >([[ -n ${DEBUG_SERVER:-} ]] && cat >&2 || cat >/dev/null) &
pid=$!
wait "$pid"
