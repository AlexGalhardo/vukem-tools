#!/usr/bin/env bash
# Writes checksums.txt (SHA-256, `sha256sum -c` format) for every release file in a directory.
# usage: checksums.sh <directory>
set -euo pipefail

if [ $# -ne 1 ] || [ ! -d "$1" ]; then
	echo "usage: checksums.sh <directory>" >&2
	exit 2
fi
cd "$1"
rm -f checksums.txt
find . -maxdepth 1 -type f ! -name checksums.txt -printf '%f\n' | LC_ALL=C sort | xargs -d '\n' sha256sum >checksums.txt
test -s checksums.txt
sha256sum -c checksums.txt
