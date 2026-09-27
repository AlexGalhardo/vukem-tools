#!/usr/bin/env bash
# Sobe a stack de desenvolvimento em Docker (infra/docker-compose.yml) no Windows
# (Git Bash ou WSL2, com Docker Desktop instalado).
#   bash setups/setup-windows-docker.sh            # backend + frontend
#   WITH_TELEGRAM=1 bash setups/setup-windows-docker.sh
set -euo pipefail
cd "$(dirname "$0")/.."
# shellcheck source=infra/scripts/docker-dev.sh
source infra/scripts/docker-dev.sh

if ! docker info >/dev/null 2>&1; then
	echo "==> Iniciando o Docker Desktop..."
	powershell.exe -NoProfile -Command "Start-Process 'C:\\Program Files\\Docker\\Docker\\Docker Desktop.exe'" >/dev/null 2>&1 || true
	wait_for "o Docker Desktop" 120 docker info
fi

compose_up_dev
