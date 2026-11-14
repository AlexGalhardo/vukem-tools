#!/usr/bin/env bash
# Runs the engine-dependent Go tests inside the production image, with
# REQUIRE_ENGINES=1 so a missing engine fails the run instead of skipping.
#
#   scripts/engine-tests.sh [image]     # default: vukem-tools:local
#
# The image has no Go toolchain, so each package's test binary is compiled
# here for Linux and run in the image from that package's directory.
set -euo pipefail

image="${1:-vukem-tools:local}"
packages=(audio compressor downloader editor engine image pdf video)
# Inside the repository rather than mktemp: Docker Desktop on Windows cannot
# mount a Git Bash /tmp path.
out="$PWD/.engine-tests"
mkdir -p "$out"
trap 'rm -rf "$out"' EXIT

for pkg in "${packages[@]}"; do
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go test -c -o "$out/$pkg.test" "./internal/$pkg"
done

MSYS_NO_PATHCONV=1 docker run --rm --user root \
	--cap-add SYS_ADMIN \
	-e REQUIRE_ENGINES=1 \
	-v "$PWD":/repo:ro -v "$out":/t:ro \
	--entrypoint sh "$image" -c '
		status=0
		for pkg in '"${packages[*]}"'; do
			echo "== $pkg"
			(cd "/repo/internal/$pkg" && "/t/$pkg.test" -test.count=1) || status=1
		done
		exit $status'
