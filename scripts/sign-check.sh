#!/usr/bin/env bash
# Builds a packaged Visor signed one way, into /tmp/visor-signcheck, so you
# can try what macOS allows that signature (notifications, launch at login,
# Keychain prompts) without touching bin/visor.app.
#
#   scripts/sign-check.sh adhoc
#   scripts/sign-check.sh identity:"Visor Self-Signed"
#   scripts/sign-check.sh open      # launch the last build against the sandbox
#
# The app runs against the sandbox supervisord (scripts/sandbox.sh up) with
# its own state dir, never ~/Library/Application Support/Visor.
set -euo pipefail

DIR=/tmp/visor-signcheck
BIN="$DIR/bin"
APP="$BIN/visor.app"
SANDBOX_CONF=${VISOR_SANDBOX_DIR:-/tmp/visor-sandbox}/supervisord.conf

usage() { sed -n '2,10p' "$0" | sed 's/^# \{0,1\}//'; exit 2; }
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
  *) usage ;;
esac

cd "$(dirname "$0")/.."
mkdir -p "$BIN"
wails3 task darwin:package BIN_DIR="$BIN" SIGN_IDENTITY="$identity"
report
