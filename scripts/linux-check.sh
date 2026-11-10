#!/usr/bin/env bash
# The Linux counterpart of sign-check.sh: builds the AppImage into
# /tmp/visor-linuxcheck, so you can try the packaged app (tray, banners,
# launch at login, one instance, the keyring) without touching bin/.
#
#   scripts/linux-check.sh build   # wails3 task linux:create:appimage, copied here
#   scripts/linux-check.sh open    # run that AppImage against the sandbox
#
# It runs against the sandbox supervisord (scripts/sandbox.sh up) with its
# own state dir, never ~/.config/Visor. On Ubuntu 23.10+ the AppImage needs
# an AppArmor profile first: Visor prints the steps when it is missing.
set -euo pipefail

DIR=/tmp/visor-linuxcheck
IMAGE="$DIR/Visor.AppImage"
SANDBOX_CONF=${VISOR_SANDBOX_DIR:-/tmp/visor-sandbox}/supervisord.conf

usage() { sed -n '2,11p' "$0" | sed 's/^# \{0,1\}//'; exit 2; }
[ $# -eq 1 ] || usage
[ "$(uname -s)" = Linux ] || { echo "linux-check.sh runs on Linux; on a Mac use scripts/sign-check.sh" >&2; exit 1; }

case "$1" in
  build)
    cd "$(dirname "$0")/.."
    wails3 task linux:create:appimage
    mkdir -p "$DIR"
    cp bin/visor-x86_64.AppImage "$IMAGE"
    chmod +x "$IMAGE"
    echo
    echo "Built: $IMAGE"
    echo "Next:  scripts/linux-check.sh open"
    ;;
  open)
    [ -x "$IMAGE" ] || { echo "No build yet: run scripts/linux-check.sh build first" >&2; exit 1; }
    [ -f "$SANDBOX_CONF" ] || { echo "No sandbox at $SANDBOX_CONF: run scripts/sandbox.sh up" >&2; exit 1; }
    mkdir -p "$DIR/state"
    VISOR_CONFIG="$SANDBOX_CONF" VISOR_STATE_DIR="$DIR/state" exec "$IMAGE"
    ;;
  *) usage ;;
esac
