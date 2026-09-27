#!/usr/bin/env bash
# Alternativa self-hosted: stack completa (backend + frontend + bot) em Docker Compose
# (infra/docker-compose.prod.yml) atrás do Caddy do host, numa VPS Ubuntu.
#
#   sudo bash setups/setup-unix-vps-docker.sh                           # código já clonado
#   REPO_URL=git@github.com:AlexGalhardo/galhardo-studio.vercel.app.git sudo -E bash setup-unix-vps-docker.sh
#
# Variáveis (opcionais): APP_DIR=/opt/galhardo-video  DOMAIN=video.galhardo.tech  LETSENCRYPT_EMAIL=
#   API_PORT/WEB_PORT (default: próximas livres a partir de 8000/3000)
# O bot só sobe se telegram/.env tiver TELEGRAM_BOT_TOKEN. Idempotente.
set -euo pipefail

[[ "${EUID:-$(id -u)}" -eq 0 ]] || exec sudo -E bash "$0" "$@"

STEP="início"
trap 'echo "ERRO na etapa \"$STEP\" (linha $LINENO): $BASH_COMMAND" >&2' ERR
step() { STEP="$1"; echo; echo "=== $1 ==="; }

REPO_URL="${REPO_URL:-}"
REPO_BRANCH="${REPO_BRANCH:-main}"
DOMAIN="${DOMAIN:-video.galhardo.tech}"
LETSENCRYPT_EMAIL="${LETSENCRYPT_EMAIL:-}"
CADDY_SITE=/etc/caddy/sites-enabled/galhardo-video.caddy

if [[ -z "${APP_DIR:-}" ]]; then
	if [[ -f "$(pwd)/infra/docker-compose.prod.yml" ]]; then APP_DIR="$(pwd)"; else APP_DIR=/opt/galhardo-video; fi
fi

set_env() {
	if grep -q "^$2=" "$1"; then sed -i "s#^$2=.*#$2=$3#" "$1"; else echo "$2=$3" >>"$1"; fi
}
get_env() { grep -E "^$2=" "$1" 2>/dev/null | tail -n1 | cut -d= -f2- || true; }
port_in_use() { ss -Htln | awk -v p=":$1" '$4 ~ (p "$") {found=1} END {exit !found}'; }
free_port() { local p="$1"; while port_in_use "$p"; do p=$((p + 1)); done; echo "$p"; }

step "1. Pacotes, Docker e Caddy"
export DEBIAN_FRONTEND=noninteractive
apt-get update -y
apt-get install -y --no-install-recommends ca-certificates curl git gnupg openssl ufw \
	debian-keyring debian-archive-keyring apt-transport-https
command -v docker >/dev/null || curl -fsSL https://get.docker.com | sh
systemctl enable --now docker
if ! command -v caddy >/dev/null; then
	curl -1sLf https://dl.cloudsmith.io/public/caddy/stable/gpg.key | gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
	curl -1sLf https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt >/etc/apt/sources.list.d/caddy-stable.list
	apt-get update -y && apt-get install -y caddy
fi
mkdir -p /etc/caddy/sites-enabled
[[ -f /etc/caddy/Caddyfile ]] || : >/etc/caddy/Caddyfile
grep -q 'sites-enabled/\*.caddy' /etc/caddy/Caddyfile || echo 'import sites-enabled/*.caddy' >>/etc/caddy/Caddyfile
if [[ -n "$LETSENCRYPT_EMAIL" ]] && ! grep -q '^[[:space:]]*email ' /etc/caddy/Caddyfile; then
	printf '{\n\temail %s\n}\n\n%s' "$LETSENCRYPT_EMAIL" "$(cat /etc/caddy/Caddyfile)" >/etc/caddy/Caddyfile
fi
systemctl enable --now caddy

step "2. Código em $APP_DIR"
if [[ -n "$REPO_URL" ]]; then
	if [[ -d "$APP_DIR/.git" ]]; then
		git -C "$APP_DIR" fetch origin "$REPO_BRANCH"
		git -C "$APP_DIR" checkout "$REPO_BRANCH"
		git -C "$APP_DIR" reset --hard "origin/$REPO_BRANCH"
	else
		git clone --branch "$REPO_BRANCH" "$REPO_URL" "$APP_DIR"
	fi
fi
cd "$APP_DIR"
[[ -f infra/docker-compose.prod.yml ]] || { echo "$APP_DIR não é a raiz do repositório." >&2; exit 1; }

step "3. Arquivos .env"
[[ -f backend/.env ]] || cp backend/.env.example backend/.env
[[ -f frontend/.env.local ]] || cp frontend/.env.example frontend/.env.local
[[ -f telegram/.env ]] || cp telegram/.env.example telegram/.env
touch .env.deploy
API_KEY="$(get_env backend/.env API_KEY)"
[[ -n "$API_KEY" ]] || { API_KEY="$(openssl rand -hex 32)"; set_env backend/.env API_KEY "$API_KEY"; }
set_env backend/.env ALLOWED_ORIGINS "https://$DOMAIN"
set_env frontend/.env.local API_KEY "$API_KEY"
set_env telegram/.env API_KEY "$API_KEY"
set_env telegram/.env PUBLIC_WEB_BASE_URL "https://$DOMAIN"
chmod 600 backend/.env frontend/.env.local telegram/.env

COMPOSE=(docker compose --env-file .env.deploy -f infra/docker-compose.prod.yml)
[[ -n "$(get_env telegram/.env TELEGRAM_BOT_TOKEN)" ]] && COMPOSE+=(--profile telegram)

step "4. Portas (só em 127.0.0.1)"
"${COMPOSE[@]}" down --remove-orphans >/dev/null 2>&1 || true
API_PORT="${API_PORT:-$(get_env .env.deploy API_PORT)}"
WEB_PORT="${WEB_PORT:-$(get_env .env.deploy WEB_PORT)}"
API_PORT="$(free_port "${API_PORT:-8000}")"
WEB_PORT="$(free_port "${WEB_PORT:-3000}")"
set_env .env.deploy API_PORT "$API_PORT"
set_env .env.deploy WEB_PORT "$WEB_PORT"
echo "backend=127.0.0.1:$API_PORT frontend=127.0.0.1:$WEB_PORT"

step "5. docker compose up"
"${COMPOSE[@]}" up -d --build
docker image prune -f >/dev/null

step "6. Caddy -> $DOMAIN"
cat >"$CADDY_SITE" <<EOF
$DOMAIN {
	encode zstd gzip
	header -Server
	reverse_proxy 127.0.0.1:$WEB_PORT
}
EOF
caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile
systemctl reload caddy
ufw allow OpenSSH >/dev/null && ufw allow 80/tcp >/dev/null && ufw allow 443/tcp >/dev/null
ufw --force enable >/dev/null

step "7. Healthcheck"
for _ in $(seq 60); do
	if curl -fsS "http://127.0.0.1:$API_PORT/health"; then break; fi
	sleep 2
done
echo
"${COMPOSE[@]}" ps
echo
echo "App: https://$DOMAIN   Logs: cd $APP_DIR && ${COMPOSE[*]} logs -f"
