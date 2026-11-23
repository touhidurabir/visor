#!/usr/bin/env bash
# Builds a packaged Visor signed one way, into /tmp/visor-signcheck, so you
# can try what macOS allows that signature (notifications, launch at login,
# Keychain prompts) without touching bin/visor.app.
#
#   scripts/sign-check.sh adhoc
#   scripts/sign-check.sh identity:"Visor Self-Signed"
#   scripts/sign-check.sh open      # launch the last build against the sandbox
#   scripts/sign-check.sh open-real # launch it against your real supervisord
#
# Both use their own state dir, never ~/Library/Application Support/Visor.
# open needs the sandbox supervisord (scripts/sandbox.sh up); open-real finds
# your real config the way an installed Visor does, so its actions are real.
set -euo pipefail

DIR=/tmp/visor-signcheck
BIN="$DIR/bin"
APP="$BIN/visor.app"
SANDBOX_CONF=${VISOR_SANDBOX_DIR:-/tmp/visor-sandbox}/supervisord.conf

usage() { sed -n '2,13p' "$0" | sed 's/^# \{0,1\}//'; exit 2; }
[ $# -eq 1 ] || usage

report() {
  echo
  echo "== Signature"
  codesign -dv "$APP" 2>&1 | grep -E 'Identifier|Signature|TeamIdentifier|Authority' || true
  echo
  echo "== Gatekeeper (spctl)"
  spctl -a -vv "$APP" 2>&1 || true
  echo
  echo "Built: $APP"
  echo "Next:  scripts/sign-check.sh open"
}

case "$1" in
  adhoc) identity="-" ;;
  identity:*) identity="${1#identity:}" ;;
  open)
    [ -d "$APP" ] || { echo "No build yet: run with adhoc or identity:<name> first" >&2; exit 1; }
    [ -f "$SANDBOX_CONF" ] || { echo "No sandbox at $SANDBOX_CONF: run scripts/sandbox.sh up" >&2; exit 1; }
    mkdir -p "$DIR/state"
    exec open -n --env "VISOR_CONFIG=$SANDBOX_CONF" --env "VISOR_STATE_DIR=$DIR/state" "$APP"
    ;;
  open-real)
    [ -d "$APP" ] || { echo "No build yet: run with adhoc or identity:<name> first" >&2; exit 1; }
    # Packaged Visors share one single-instance ID: a second launch would only
    # bring the running one forward, and two would poll and notify twice.
    if running=$(pgrep -lx visor); then
      echo "Quit the Visor that's running first:" >&2
      echo "$running" >&2
      exit 1
    fi
    mkdir -p "$DIR/state-real"
    echo "Visor runs against your real supervisord: start, stop and Save & apply are real."
    echo "Its own state is in $DIR/state-real. Shared with your installed Visor: Keychain secrets"
    echo "(the MCP token, supervisord's password) and Launch at login."
    exec open -n --env "VISOR_STATE_DIR=$DIR/state-real" "$APP"
    ;;
  *) usage ;;
esac

cd "$(dirname "$0")/.."
mkdir -p "$BIN"
wails3 task darwin:package BIN_DIR="$BIN" SIGN_IDENTITY="$identity"
report
