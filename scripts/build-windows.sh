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
out="$(cd "$out" && pwd)"
# The staging directory sits under the output directory, not in /tmp: the native
# makensis and go.exe must see the same files Git Bash created.
work="$out/.stage"
rm -rf "$work"
mkdir -p "$work"
trap 'rm -rf "$work"' EXIT

# native <path>: the path in the form the running OS expects (C:\... on Windows).
native() {
	if command -v cygpath >/dev/null 2>&1; then cygpath -w "$1"; else printf '%s' "$1"; fi
}

exe="$work/vukem-tools.exe"
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath 	-ldflags "-s -w -H windowsgui -X main.version=${version}" 	-o "$(native "$exe")" ./cmd/desktop

setup="$out/vukem-tools-${slug}-windows-amd64-setup.exe"
echo "build-windows: exe   = $exe -> $(native "$exe")"
echo "build-windows: setup = $setup -> $(native "$setup")"
echo "build-windows: nsi   = $(native "$here/desktop/packaging/windows/installer.nsi")"
ls -l "$exe"
# A PE file starts with "MZ"; a broken build fails here, not on the owner's machine.
[ "$(head -c 2 "$exe")" = "MZ" ] || {
	echo "build-windows: $exe is not a PE file" >&2
	exit 1
}

# MSYS2_ARG_CONV_EXCL stops Git Bash from rewriting the -D values and the script path.
MSYS2_ARG_CONV_EXCL='*' makensis -V3 	"-DVERSION=$numeric" "-DEXE=$(native "$exe")" "-DOUTFILE=$(native "$setup")" 	"$(native "$here/desktop/packaging/windows/installer.nsi")"
ls -l "$setup"

zipfile="$out/vukem-tools-${slug}-windows-amd64-portable.zip"
rm -f "$zipfile"
if command -v zip >/dev/null 2>&1; then
	(cd "$work" && zip -q "$zipfile" vukem-tools.exe)
else
	(cd "$work" && 7z a -tzip -bso0 "$(native "$zipfile")" vukem-tools.exe)
fi
ls -l "$out"
