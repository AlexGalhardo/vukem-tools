#!/usr/bin/env bash
# Sobe a stack de desenvolvimento em Docker (infra/docker-compose.yml) em Linux/macOS.
#   bash setups/setup-unix-docker.sh            # backend + frontend
#   WITH_TELEGRAM=1 bash setups/setup-unix-docker.sh
# Escolhe portas livres a partir de 8000/3000 e espera o backend responder /health.
set -euo pipefail
cd "$(dirname "$0")/.."
# shellcheck source=infra/scripts/docker-dev.sh
source infra/scripts/docker-dev.sh

if ! docker info >/dev/null 2>&1; then
	if [[ "$(uname -s)" == "Darwin" ]]; then
		open -a Docker
	else
		sudo systemctl start docker
	fi
	wait_for "o daemon do Docker" 60 docker info
fi

compose_up_dev
