#!/usr/bin/env bash
# Builds the Windows desktop executable, the per-user NSIS installer and the portable zip.
# Needs makensis on PATH (the release workflow installs a pinned NSIS) and zip or 7z for the zip.
# usage: build-windows.sh <version X.Y.Z> <output directory>
set -euo pipefail

if [ $# -ne 2 ]; then
	echo "usage: build-windows.sh <version> <output directory>" >&2
	exit 2
fi
version="${1#v}"
slug="${version//./-}"
out="$2"
here="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
numeric="${version%%[-+]*}"
mkdir -p "$out"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath \
	-ldflags "-s -w -H windowsgui -X main.version=${version}" \
	-o "$work/vukem-tools.exe" ./cmd/desktop

makensis -V2 -DVERSION="$numeric" -DEXE="$(cd "$work" && pwd -W 2>/dev/null || pwd)/vukem-tools.exe" \
	-DOUTFILE="$(cd "$out" && pwd -W 2>/dev/null || pwd)/vukem-tools-${slug}-windows-amd64-setup.exe" \
	"$here/desktop/packaging/windows/installer.nsi"

zipfile="$(cd "$out" && pwd)/vukem-tools-${slug}-windows-amd64-portable.zip"
rm -f "$zipfile"
if command -v zip >/dev/null 2>&1; then
	(cd "$work" && zip -q "$zipfile" vukem-tools.exe)
else
	(cd "$work" && 7z a -tzip -bso0 "$zipfile" vukem-tools.exe)
fi
ls -l "$out"
