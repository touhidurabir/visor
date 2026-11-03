#!/usr/bin/env bash
# Packs a built Visor.app into Visor-<version>-universal.dmg with an
# Applications shortcut, plus a .sha256 file next to it.
#
#   scripts/release-dmg.sh bin/visor.app 0.1.0 dist
#
# Plain hdiutil, no Finder styling: that needs a logged-in desktop session,
# which CI runners don't have.
set -euo pipefail

[ $# -eq 3 ] || { echo "usage: $0 <app> <version> <outdir>" >&2; exit 2; }
app=$1 version=$2 out=$3
[ -d "$app/Contents/MacOS" ] || { echo "$app is not an app bundle" >&2; exit 1; }

mkdir -p "$out"
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT
ditto "$app" "$stage/Visor.app" # ditto keeps the signature and attributes
ln -s /Applications "$stage/Applications"

name="Visor-$version-universal.dmg"
hdiutil create -volname "Visor $version" -srcfolder "$stage" -fs HFS+ -format UDZO -ov "$out/$name"
(cd "$out" && shasum -a 256 "$name" > "$name.sha256")
echo "$out/$name"
