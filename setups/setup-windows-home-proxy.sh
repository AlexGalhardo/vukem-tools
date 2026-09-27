#!/usr/bin/env bash
# Transforma um PC Windows de casa (ligado 24h) na saída de internet do YouTube da VPS:
# a VPS abre um túnel SSH (SOCKS) por dentro da Tailscale e o YouTube vê o seu IP residencial.
#
# Rode no Git Bash aberto como Administrador:
#   VPS_PUBKEY="ssh-ed25519 AAAA... galhardo-vps-youtube-proxy" bash setups/setup-windows-home-proxy.sh
#
# VPS_PUBKEY é impressa por setups/setup-unix-vps-pm2.sh (arquivo /root/.ssh/galhardo_home_proxy.pub).
# A chave só pode abrir túneis para as portas 80/443: nada de shell nem outras portas.
set -euo pipefail

VPS_PUBKEY="${VPS_PUBKEY:?defina VPS_PUBKEY com a chave pública da VPS}"
[[ "$VPS_PUBKEY" == ssh-* ]] || { echo "VPS_PUBKEY não parece uma chave pública SSH." >&2; exit 1; }
net session >/dev/null 2>&1 || { echo "Abra o Git Bash como Administrador." >&2; exit 1; }

run_ps() { powershell.exe -NoProfile -ExecutionPolicy Bypass -Command "$1"; }

echo "==> Tailscale"
command -v tailscale >/dev/null 2>&1 || winget install -e --id Tailscale.Tailscale --accept-source-agreements --accept-package-agreements
TAILSCALE="$(command -v tailscale || echo "/c/Program Files/Tailscale/tailscale.exe")"
"$TAILSCALE" status >/dev/null 2>&1 || "$TAILSCALE" up --hostname="${TAILSCALE_HOSTNAME:-galhardo-home}"

echo "==> OpenSSH Server"
run_ps "if ((Get-WindowsCapability -Online -Name 'OpenSSH.Server*').State -ne 'Installed') { Add-WindowsCapability -Online -Name 'OpenSSH.Server~~~~0.0.1.0' | Out-Null }"
run_ps "Set-Service sshd -StartupType Automatic; Start-Service sshd"

echo "==> Autorizando a chave da VPS (somente túnel para :80/:443)"
# Contas administradoras do Windows leem este arquivo em vez de ~/.ssh/authorized_keys.
KEYS_FILE="/c/ProgramData/ssh/administrators_authorized_keys"
touch "$KEYS_FILE"
KEY_BODY="$(awk '{print $2}' <<<"$VPS_PUBKEY")"
if ! grep -qF "$KEY_BODY" "$KEYS_FILE"; then
	echo "restrict,port-forwarding,permitopen=\"*:443\",permitopen=\"*:80\" $VPS_PUBKEY" >>"$KEYS_FILE"
fi
# O sshd ignora o arquivo se outros usuários puderem lê-lo.
icacls.exe 'C:\ProgramData\ssh\administrators_authorized_keys' /inheritance:r /grant 'Administrators:F' /grant 'SYSTEM:F' >/dev/null

cat <<EOF

Pronto. Na VPS, rode o setup de produção com:
  HOME_PROXY_SSH=$USERNAME@$("$TAILSCALE" ip -4 | head -n1 | tr -d '\r') sudo -E bash setups/setup-unix-vps-pm2.sh
Mantenha o PC ligado e sem suspender (ex.: powercfg /change standby-timeout-ac 0):
todo download do YouTube passa pela sua internet.
EOF
