#!/usr/bin/env bash
# Transforma um Linux/macOS de casa (ligado 24h) na saída de internet do YouTube da VPS:
# a VPS abre um túnel SSH (SOCKS) por dentro da Tailscale e o YouTube vê o seu IP residencial.
#
#   VPS_PUBKEY="ssh-ed25519 AAAA... galhardo-vps-youtube-proxy" bash setups/setup-unix-home-proxy.sh
#
# VPS_PUBKEY é impressa por setups/setup-unix-vps-pm2.sh (arquivo /root/.ssh/galhardo_home_proxy.pub).
# A chave só pode abrir túneis para as portas 80/443: nada de shell nem outras portas.
set -euo pipefail

VPS_PUBKEY="${VPS_PUBKEY:?defina VPS_PUBKEY com a chave pública da VPS}"
[[ "$VPS_PUBKEY" == ssh-* ]] || { echo "VPS_PUBKEY não parece uma chave pública SSH." >&2; exit 1; }

echo "==> Tailscale"
if ! command -v tailscale >/dev/null 2>&1; then
	if [[ "$(uname -s)" == "Darwin" ]]; then
		brew install --cask tailscale
	else
		curl -fsSL https://tailscale.com/install.sh | sh
	fi
fi
tailscale status >/dev/null 2>&1 || sudo tailscale up --hostname="${TAILSCALE_HOSTNAME:-galhardo-home}"

echo "==> Servidor SSH"
if [[ "$(uname -s)" == "Darwin" ]]; then
	sudo systemsetup -setremotelogin on
else
	command -v sshd >/dev/null 2>&1 || sudo apt-get install -y openssh-server
	sudo systemctl enable --now ssh 2>/dev/null || sudo systemctl enable --now sshd
fi

echo "==> Autorizando a chave da VPS (somente túnel para :80/:443)"
mkdir -p ~/.ssh && chmod 700 ~/.ssh
touch ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys
KEY_BODY="$(awk '{print $2}' <<<"$VPS_PUBKEY")"
if ! grep -qF "$KEY_BODY" ~/.ssh/authorized_keys; then
	echo "restrict,port-forwarding,permitopen=\"*:443\",permitopen=\"*:80\" $VPS_PUBKEY" >>~/.ssh/authorized_keys
fi

cat <<EOF

Pronto. Na VPS, rode o setup de produção com:
  HOME_PROXY_SSH=$(whoami)@$(tailscale ip -4 | head -n1) sudo -E bash setups/setup-unix-vps-pm2.sh
Mantenha esta máquina ligada e sem suspender: todo download do YouTube passa pela sua internet.
EOF
