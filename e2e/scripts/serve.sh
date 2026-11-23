#!/usr/bin/env bash
# Starts the e2e sandbox supervisord and the server-mode Visor against it.
# Playwright runs this as its webServer and, at the end of a run or on
# Ctrl-C, stops it with SIGTERM (webServer.gracefulShutdown). The trap then
# stops Visor, waits for it to exit, and tears the sandbox down, so nothing
# outlives a run. A sandbox left over from a run that was killed outright is
# torn down here before the new one starts.
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
BIN=$ROOT/bin/visor-e2e
LOG=$ROOT/e2e/visor-server.log
export VISOR_SANDBOX_DIR=/tmp/visor-e2e

if [[ ! -x $BIN ]]; then
  echo "bin/visor-e2e is missing. Build it first: go build -tags server -o bin/visor-e2e . (or run: task test:e2e)" >&2
  exit 1
fi

pid=
sshpid=
cleanup() {
  trap - EXIT TERM INT HUP
  if [[ -n $pid ]]; then
    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
  fi
  if [[ -n $sshpid ]]; then
    kill "$sshpid" 2>/dev/null || true
    wait "$sshpid" 2>/dev/null || true
  fi
  "$ROOT/scripts/sandbox.sh" down >/dev/null 2>&1 || true
}
trap cleanup EXIT
trap 'exit 143' TERM INT HUP

"$ROOT/scripts/sandbox.sh" down >/dev/null 2>&1 || true
"$ROOT/scripts/sandbox.sh" up

# The other specs start past first-run setup; doctor.spec.ts runs its own
# Visors on fresh state to see Doctor.
mkdir -p "$VISOR_SANDBOX_DIR/state"
printf '{"onboarded":true}\n' >"$VISOR_SANDBOX_DIR/state/settings.json"

# A test SSH server on 127.0.0.1:8767 (user e2e, password e2e) in front of
# the same sandbox, for the server-tab specs (servers.spec.ts).
SSHBIN=$ROOT/bin/visor-e2e-ssh
if [[ -x $SSHBIN ]]; then
  "$SSHBIN" -addr 127.0.0.1:8767 >>"$ROOT/e2e/visor-ssh.log" 2>&1 &
  sshpid=$!
fi

# Visor's own log goes to a file (uploaded by CI on failure); DEBUG_SERVER=1
# also shows it live.
: >"$LOG"
if [[ -n ${DEBUG_SERVER:-} ]]; then
  exec 3> >(tee -a "$LOG" >&2)
else
  exec 3>>"$LOG"
fi
VISOR_CONFIG=$VISOR_SANDBOX_DIR/supervisord.conf \
  VISOR_STATE_DIR=$VISOR_SANDBOX_DIR/state \
  WAILS_SERVER_HOST=127.0.0.1 \
  WAILS_SERVER_PORT=8766 \
  VISOR_MCP_PORT=8768 \
  "$BIN" >&3 2>&3 &
pid=$!
wait "$pid"
