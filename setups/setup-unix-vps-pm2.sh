#!/usr/bin/env bash
# Produção atual: backend + bot do Telegram + PO Token provider nativos via PM2, atrás do
# Caddy (TLS automático), numa VPS Ubuntu. O frontend continua na Vercel.
#
#   sudo bash setups/setup-unix-vps-pm2.sh                               # código já clonado
#   REPO_URL=git@github.com:AlexGalhardo/galhardo-studio.vercel.app.git sudo -E bash setup-unix-vps-pm2.sh
#
# Variáveis (todas opcionais):
#   APP_DIR=/opt/galhardo-studio  DOMAIN=galhardo.tech  LETSENCRYPT_EMAIL=
#   PUBLIC_WEB_BASE_URL=https://galhardo-video.vercel.app  API_PORT=<aleatória livre>
#   HOME_PROXY_SSH=usuario@100.x.y.z   túnel SOCKS para o YouTube sair pela internet de casa
#   TAILSCALE_AUTHKEY=tskey-...        entra na tailnet sem login interativo
# Idempotente: reexecutar atualiza o código, as dependências e reinicia os processos.
# Detalhes e troubleshooting: docs/deployment.md e docs/youtube.md.
set -euo pipefail

[[ "${EUID:-$(id -u)}" -eq 0 ]] || exec sudo -E bash "$0" "$@"

STEP="início"
trap 'echo "ERRO na etapa \"$STEP\" (linha $LINENO): $BASH_COMMAND" >&2' ERR
step() { STEP="$1"; echo; echo "=== $1 ==="; }

REPO_URL="${REPO_URL:-}"
REPO_BRANCH="${REPO_BRANCH:-main}"
DOMAIN="${DOMAIN:-galhardo.tech}"
LETSENCRYPT_EMAIL="${LETSENCRYPT_EMAIL:-}"
PUBLIC_WEB_BASE_URL="${PUBLIC_WEB_BASE_URL:-https://galhardo-video.vercel.app}"
HOME_PROXY_SSH="${HOME_PROXY_SSH:-}"
TAILSCALE_AUTHKEY="${TAILSCALE_AUTHKEY:-}"
# Rótulo que o backend e o bot mostram nas respostas (header X-Deploy-Target, /status do bot).
DEPLOY_TARGET="${DEPLOY_TARGET:-VPS Hostinger}"
PM2_VERSION="7.0.4"
POT_PROVIDER_TAG="2.0.0"
SOCKS_PORT=1080
# Nomes mantidos da versão anterior: reexecutar substitui os processos já existentes.
PM2_BACKEND=galhardo-backend-pm2
PM2_TELEGRAM=galhardo-telegram-bot
PM2_POT=galhardo-pot-provider
PM2_PROXY=galhardo-youtube-proxy
CADDY_SITE=/etc/caddy/sites-enabled/galhardo-studio-backend-pm2.caddy

# Ordem: APP_DIR explícito > repositório onde este script está (funciona rodando de qualquer
# pasta, inclusive de dentro de setups/) > diretório atual > padrão para clonar do zero.
if [[ -z "${APP_DIR:-}" ]]; then
	SCRIPT_REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
	if [[ -f "$SCRIPT_REPO/backend/requirements.txt" ]]; then
		APP_DIR="$SCRIPT_REPO"
	elif [[ -f "$(pwd)/backend/requirements.txt" ]]; then
		APP_DIR="$(pwd)"
	else
		APP_DIR=/opt/galhardo-studio
	fi
fi

set_env() { # set_env <arquivo> <CHAVE> <valor>: substitui ou acrescenta sem duplicar
	if grep -q "^$2=" "$1"; then sed -i "s#^$2=.*#$2=$3#" "$1"; else echo "$2=$3" >>"$1"; fi
}
get_env() { grep -E "^$2=" "$1" 2>/dev/null | tail -n1 | cut -d= -f2- || true; }
port_in_use() { ss -Htln | awk -v p=":$1" '$4 ~ (p "$") {found=1} END {exit !found}'; }

step "1. Pacotes do sistema"
export DEBIAN_FRONTEND=noninteractive
apt-get update -y
apt-get install -y --no-install-recommends ca-certificates curl git gnupg openssl ufw ffmpeg openssh-client \
	python3 python3-venv debian-keyring debian-archive-keyring apt-transport-https
python3 -c 'import sys; raise SystemExit(sys.version_info < (3, 12))' || { echo "Python 3.12+ é obrigatório." >&2; exit 1; }

step "2. Node.js LTS (runtime do PM2, do PO Token provider e do yt-dlp-ejs) + PM2"
if ! command -v node >/dev/null || [[ "$(node -p 'process.versions.node.split(".")[0]')" -lt 22 ]]; then
	curl -fsSL https://deb.nodesource.com/setup_24.x | bash -
	apt-get install -y nodejs
fi
[[ "$(pm2 --version 2>/dev/null || true)" == "$PM2_VERSION" ]] || npm install -g "pm2@$PM2_VERSION"

step "3. Caddy"
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

step "4. Código em $APP_DIR"
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
[[ -f backend/requirements.txt ]] || { echo "$APP_DIR não é a raiz do repositório." >&2; exit 1; }

step "5. Dependências Python (backend/.venv e telegram/.venv)"
for workspace in backend telegram; do
	# Venv não é relocável: se a pasta do app foi movida (ou o python3 do sistema mudou), os
	# shebangs apontam para o caminho antigo e o pip falha com "bad interpreter". Recria.
	if ! "$workspace/.venv/bin/pip" --version >/dev/null 2>&1; then
		echo "Recriando $workspace/.venv (ausente ou quebrado)."
		rm -rf "$workspace/.venv"
		python3 -m venv "$workspace/.venv"
	fi
	"$workspace/.venv/bin/python" -m pip install -q --upgrade pip
	"$workspace/.venv/bin/python" -m pip install -q -r "$workspace/requirements.txt"
done

step "6. PO Token provider ($POT_PROVIDER_TAG)"
POT_DIR="$APP_DIR/backend/pot-provider-src"
if [[ "$(git -C "$POT_DIR" describe --tags 2>/dev/null || true)" != "$POT_PROVIDER_TAG" ]]; then
	rm -rf "$POT_DIR"
	git clone --branch "$POT_PROVIDER_TAG" --depth 1 https://github.com/Brainicism/bgutil-ytdlp-pot-provider.git "$POT_DIR"
fi
(cd "$POT_DIR/server" && npm ci --no-audit --no-fund && npx tsc)

step "7. Arquivos .env"
# Versões anteriores gravavam no .env da raiz, que o backend (cwd=backend/) nunca lia.
if [[ ! -f backend/.env && -f .env ]]; then
	cp .env backend/.env
	echo "backend/.env migrado a partir do .env da raiz."
fi
[[ -f backend/.env ]] || cp backend/.env.example backend/.env
[[ -f telegram/.env ]] || cp telegram/.env.example telegram/.env
touch .env.deploy

API_PORT="${API_PORT:-$(get_env .env.deploy API_PORT)}"
API_PORT="${API_PORT:-$(get_env .env API_PORT)}"
if [[ -z "$API_PORT" ]] || { port_in_use "$API_PORT" && ! pm2 describe "$PM2_BACKEND" >/dev/null 2>&1; }; then
	until API_PORT=$((20000 + RANDOM % 40000)) && ! port_in_use "$API_PORT"; do :; done
fi
set_env .env.deploy API_PORT "$API_PORT"

API_KEY="$(get_env backend/.env API_KEY)"
if [[ -z "$API_KEY" ]]; then
	API_KEY="$(openssl rand -hex 32)"
	set_env backend/.env API_KEY "$API_KEY"
	API_KEY_CREATED=true
fi
set_env backend/.env ALLOWED_ORIGINS "$PUBLIC_WEB_BASE_URL"
set_env telegram/.env API_BASE_URL "http://127.0.0.1:$API_PORT"
set_env telegram/.env API_KEY "$API_KEY"
set_env telegram/.env PUBLIC_WEB_BASE_URL "$PUBLIC_WEB_BASE_URL"
# Não sobrescreve um rótulo já personalizado no .env.
for env_file in backend/.env telegram/.env; do
	[[ -n "$(get_env "$env_file" DEPLOY_TARGET)" ]] || set_env "$env_file" DEPLOY_TARGET "$DEPLOY_TARGET"
done
chmod 600 backend/.env telegram/.env

if [[ -n "$HOME_PROXY_SSH" ]]; then
	step "8. Túnel do YouTube pela internet de casa ($HOME_PROXY_SSH)"
	command -v tailscale >/dev/null || curl -fsSL https://tailscale.com/install.sh | sh
	if ! tailscale status >/dev/null 2>&1; then
		if [[ -n "$TAILSCALE_AUTHKEY" ]]; then
			tailscale up --authkey="$TAILSCALE_AUTHKEY" --hostname=galhardo-vps
		else
			echo "Rode 'tailscale up --hostname=galhardo-vps', autentique no link e reexecute este script." >&2
			exit 1
		fi
	fi
	KEY=/root/.ssh/galhardo_home_proxy
	[[ -f "$KEY" ]] || ssh-keygen -t ed25519 -N "" -C "galhardo-vps-youtube-proxy" -f "$KEY" >/dev/null
	pm2 delete "$PM2_PROXY" >/dev/null 2>&1 || true
	pm2 start ssh --name "$PM2_PROXY" --interpreter none --restart-delay 10000 -- \
		-N -D "127.0.0.1:$SOCKS_PORT" -i "$KEY" -o ExitOnForwardFailure=yes -o ServerAliveInterval=30 \
		-o ServerAliveCountMax=3 -o StrictHostKeyChecking=accept-new "$HOME_PROXY_SSH"
	set_env backend/.env YOUTUBE_PROXY "socks5h://127.0.0.1:$SOCKS_PORT"
	sleep 5
	if EXIT_IP="$(curl -fsS --max-time 15 --socks5-hostname "127.0.0.1:$SOCKS_PORT" https://api.ipify.org)"; then
		echo "Túnel OK: o YouTube vai ver o IP $EXIT_IP."
	else
		echo "AVISO: túnel ainda não conectou. Autorize esta chave em casa (setups/setup-*-home-proxy.sh):"
		cat "$KEY.pub"
	fi
fi

step "9. Processos no PM2"
for name in "$PM2_POT" "$PM2_BACKEND" "$PM2_TELEGRAM"; do pm2 delete "$name" >/dev/null 2>&1 || true; done
pm2 start node --name "$PM2_POT" --cwd "$POT_DIR/server" --interpreter none --restart-delay 5000 -- \
	build/main.js --port 4416 --host 127.0.0.1
pm2 start "$APP_DIR/backend/.venv/bin/python" --name "$PM2_BACKEND" --cwd "$APP_DIR/backend" --interpreter none \
	--restart-delay 5000 -- -m uvicorn app.main:app --host 127.0.0.1 --port "$API_PORT" --workers 1
if [[ -n "$(get_env telegram/.env TELEGRAM_BOT_TOKEN)" ]]; then
	pm2 start "$APP_DIR/telegram/.venv/bin/python" --name "$PM2_TELEGRAM" --cwd "$APP_DIR/telegram" \
		--interpreter none --restart-delay 5000 -- -m app.main
else
	echo "AVISO: TELEGRAM_BOT_TOKEN vazio em telegram/.env — bot não iniciado."
fi
pm2 save
pm2 startup systemd -u root --hp /root >/dev/null

step "10. Caddy -> 127.0.0.1:$API_PORT ($DOMAIN)"
cat >"$CADDY_SITE" <<EOF
$DOMAIN {
	encode zstd gzip
	header -Server
	reverse_proxy 127.0.0.1:$API_PORT
}
EOF
caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile
systemctl reload caddy

step "11. Firewall"
ufw allow OpenSSH >/dev/null
ufw allow 80/tcp >/dev/null
ufw allow 443/tcp >/dev/null
ufw --force enable >/dev/null

step "12. Healthcheck"
for _ in $(seq 20); do
	if curl -fsS "http://127.0.0.1:$API_PORT/health"; then break; fi
	sleep 1
done
echo
pm2 status

cat <<EOF

Backend:  https://$DOMAIN  (docs em /docs, health em /health)
Logs:     pm2 logs $PM2_BACKEND | pm2 logs $PM2_TELEGRAM | pm2 logs $PM2_POT
Config:   $APP_DIR/backend/.env e $APP_DIR/telegram/.env (depois: pm2 restart all)
EOF
if [[ "${API_KEY_CREATED:-false}" == "true" ]]; then
	cat <<EOF

AÇÃO NECESSÁRIA: o backend agora exige X-API-Key. Na Vercel (frontend), defina:
  API_BASE_URL=https://$DOMAIN
  API_KEY=$API_KEY
e faça um redeploy. O bot já foi configurado com a mesma chave.
EOF
fi
