#!/usr/bin/env bash
# Builds a macOS disk image that contains Vukem Tools.app. Runs on macOS (hdiutil).
# The app is not signed: no owner certificate was supplied.
set -euo pipefail

if [ $# -ne 4 ]; then
	echo "usage: build-dmg.sh <desktop binary> <version> <amd64|arm64> <output directory>" >&2
	exit 2
fi

desktop="$1"
version="${2#v}"
slug="${version//./-}"
arch="$3"
out="$4"
case "$arch" in
amd64 | arm64) ;;
*)
	echo "build-dmg: unknown architecture $arch" >&2
	exit 2
	;;
esac

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
[ -f "$desktop" ] || {
	echo "build-dmg: no desktop binary at $desktop" >&2
	exit 1
}

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
stage="$work/stage"
app="$stage/Vukem Tools.app"
mkdir -p "$app/Contents/MacOS" "$out"
install -m 0755 "$desktop" "$app/Contents/MacOS/vukem-tools"
sed "s/@VERSION@/${version}/g" "$here/Info.plist" >"$app/Contents/Info.plist"
ln -s /Applications "$stage/Applications"

dmg="$out/vukem-tools-${slug}-darwin-${arch}.dmg"
rm -f "$dmg"
hdiutil create -quiet -volname "Vukem Tools" -srcfolder "$stage" -ov -format UDZO "$dmg"
echo "$dmg"
