#!/usr/bin/env bash
# Funções dos setups de Docker local (importado via `source` por setups/setup-*-docker.sh).

wait_for() {
	local label="$1" seconds="$2"
	shift 2
	echo "==> Aguardando $label (até ${seconds}s)..."
	for _ in $(seq "$seconds"); do
		"$@" >/dev/null 2>&1 && return 0
		sleep 1
	done
	echo "ERRO: $label não ficou pronto a tempo." >&2
	return 1
}

port_in_use() {
	(echo >"/dev/tcp/127.0.0.1/$1") >/dev/null 2>&1
}

free_port() {
	local port="$1"
	while port_in_use "$port"; do
		port=$((port + 1))
	done
	echo "$port"
}

compose_up_dev() {
	local compose=(docker compose -f infra/docker-compose.yml)
	[[ "${WITH_TELEGRAM:-0}" == "1" ]] && compose+=(--profile telegram)

	[[ -f backend/.env ]] || cp backend/.env.example backend/.env
	[[ -f frontend/.env.local ]] || cp frontend/.env.example frontend/.env.local
	[[ -f telegram/.env ]] || cp telegram/.env.example telegram/.env

	"${compose[@]}" down --remove-orphans >/dev/null 2>&1 || true
	API_PORT="$(free_port "${API_PORT:-8000}")"
	WEB_PORT="$(free_port "${WEB_PORT:-3000}")"
	export API_PORT WEB_PORT

	"${compose[@]}" up -d --build
	wait_for "o backend em http://localhost:$API_PORT/health" 180 curl -fsS "http://localhost:$API_PORT/health"

	echo
	echo "Frontend: http://localhost:$WEB_PORT"
	echo "Backend:  http://localhost:$API_PORT/docs"
	echo "Logs:     ${compose[*]} logs -f"
	echo "Parar:    ${compose[*]} down"
}
