#!/bin/sh
# Uso: sh infra/scripts/python.sh <backend|telegram> <argumentos do python>
# Roda o Python do venv do workspace (Linux/macOS ou Windows) e cai no do sistema (CI).
set -eu
workspace="$1"
shift
cd "$(dirname "$0")/../../$workspace"
for candidate in .venv/bin/python .venv/Scripts/python.exe; do
	if [ -x "$candidate" ]; then
		exec "$candidate" "$@"
	fi
done
if command -v python3 >/dev/null 2>&1; then
	exec python3 "$@"
fi
exec python "$@"
