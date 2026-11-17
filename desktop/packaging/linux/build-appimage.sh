#!/usr/bin/env bash
# Builds vukem-tools_<version>_linux_amd64.AppImage from the desktop binary.
# appimagetool 1.9.1 is downloaded and checked before it runs.
set -euo pipefail

if [ $# -ne 3 ]; then
	echo "usage: build-appimage.sh <desktop binary> <version> <output directory>" >&2
	exit 2
fi

desktop="$1"
version="${2#v}"
out="$3"
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

[ -f "$desktop" ] || {
	echo "build-appimage: no desktop binary at $desktop" >&2
	exit 1
}

url="https://github.com/AppImage/appimagetool/releases/download/1.9.1/appimagetool-x86_64.AppImage"
sum="ed4ce84f0d9caff66f50bcca6ff6f35aae54ce8135408b3fa33abfc3cb384eb0"
tool="${APPIMAGETOOL:-}"
if [ -z "$tool" ]; then
	tool="$(mktemp)"
	curl -fsSL -o "$tool" "$url"
	echo "$sum  $tool" | sha256sum -c -
	chmod +x "$tool"
fi

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
appdir="$work/vukem-tools.AppDir"
mkdir -p "$appdir/usr/bin" "$out"
install -m 0755 "$desktop" "$appdir/usr/bin/vukem-tools"
install -m 0755 "$here/AppRun" "$appdir/AppRun"
install -m 0644 "$here/vukem-tools.desktop" "$appdir/vukem-tools.desktop"
# A 1x1 PNG is enough for the desktop entry. The window loads the web UI.
printf '%s' 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==' |
	base64 -d >"$appdir/vukem-tools.png"

file="$out/vukem-tools_${version}_linux_amd64.AppImage"
ARCH=x86_64 APPIMAGE_EXTRACT_AND_RUN=1 "$tool" --no-appstream "$appdir" "$file" >&2
echo "$file"
