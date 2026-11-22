#!/usr/bin/env bash
# Cross-builds the vukem-tools CLI for the release targets. No cgo, so one Linux runner builds all.
# usage: build-cli.sh <version X.Y.Z> <output directory>
# Output: vukem-tools-cli-X-Y-Z-<os>-<arch>[.exe]
set -euo pipefail

if [ $# -ne 2 ]; then
	echo "usage: build-cli.sh <version> <output directory>" >&2
	exit 2
fi
version="${1#v}"
slug="${version//./-}"
out="$2"
mkdir -p "$out"

for target in windows/amd64 linux/amd64 linux/arm64 darwin/arm64; do
	goos="${target%/*}"
	goarch="${target#*/}"
	ext=""
	[ "$goos" = windows ] && ext=".exe"
	CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -trimpath \
		-ldflags "-s -w -X main.version=${version}" \
		-o "$out/vukem-tools-cli-${slug}-${goos}-${goarch}${ext}" ./cmd/vukem-tools
done
ls -l "$out"
