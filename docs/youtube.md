# YouTube: bloqueio por IP ("Sign in to confirm you're not a bot")

Instagram, X e TikTok funcionam da VPS; o YouTube não. O YouTube pontua cada requisição por **reputação do IP** + PO Token + cookies + taxa de requisições. IPs de datacenter (Hostinger, Koyeb, AWS...) vêm pré-marcados: código nenhum resolve isso sozinho. Do seu IP residencial o mesmo código funciona (verificado em 2026-09-26).

## O que o backend já faz

| Mitigação | Onde | Observação |
|---|---|---|
| PO Token (bgutil provider) | `YTDLP_POT_PROVIDER_*` | **Corrigido na 2.0.0**: antes o `base_url` ia como string e o yt-dlp lia `"h"`; o provider nunca era usado. |
| Runtime JS (yt-dlp-ejs + node) | `YTDLP_JS_RUNTIME=node` | Sem ele o yt-dlp perde formatos e avisa "No supported JavaScript runtime". |
| Clients padrão do yt-dlp | `YTDLP_PLAYER_CLIENTS=` (vazio) | Forçar `android,web` ficou obsoleto; os defaults acompanham o YouTube. |
| IPv4 forçado | `YTDLP_FORCE_IPV4=true` | O YouTube pontua um /64 IPv6 inteiro. |
| Cookies só como fallback | `YOUTUBE_COOKIES(_FILE)` | Tenta sem cookies; com cookies só se cair no bot-check. Avisa se expiraram. |
| Rate limit | `YTDLP_SLEEP_*` | Evita rajadas. |
| **Proxy só para o YouTube** | `YOUTUBE_PROXY` | A solução de fato para IP bloqueado (abaixo). |

## Solução: sair pela internet de casa (grátis, IP residencial)

```mermaid
flowchart LR
  API[backend na VPS] -->|socks5h://127.0.0.1:1080| SSH[ssh -D na VPS]
  SSH -->|Tailscale| HOME[PC de casa<br/>sshd]
  HOME --> YT[(YouTube)]
```

1. **Em casa** (máquina ligada 24h), com a chave pública que a VPS gera:
   ```bash
   VPS_PUBKEY="ssh-ed25519 AAAA..." bash setups/setup-unix-home-proxy.sh      # Linux/macOS
   VPS_PUBKEY="ssh-ed25519 AAAA..." bash setups/setup-windows-home-proxy.sh   # Windows (Git Bash admin)
   ```
   Instala a Tailscale e o servidor SSH e autoriza a chave **só para túnel às portas 80/443** (`restrict,port-forwarding,permitopen`): a VPS não ganha shell nem acesso à sua rede.
2. **Na VPS**:
   ```bash
   sudo HOME_PROXY_SSH=usuario@100.x.y.z TAILSCALE_AUTHKEY=tskey-... bash setups/setup-unix-vps-pm2.sh
   ```
   Na primeira execução o script cria `/root/.ssh/galhardo_home_proxy` e imprime a chave pública (use no passo 1). Depois sobe `galhardo-youtube-proxy` no PM2, grava `YOUTUBE_PROXY=socks5h://127.0.0.1:1080` no `backend/.env` e mostra o IP de saída.

Custos: o vídeo inteiro trafega pela sua conexão (upload de casa). Se a máquina de casa cair, os downloads do YouTube falham com `YOUTUBE_BOT_CHECK`/`NETWORK_ERROR` e as outras plataformas seguem normais.

Verificar: `curl --socks5-hostname 127.0.0.1:1080 https://api.ipify.org` na VPS deve mostrar o IP de casa.

## Alternativas

| Opção | Custo | Observação |
|---|---|---|
| Proxy residencial pago (Webshare Residential, IPRoyal...) | ~US$ 3–7/GB | `YOUTUBE_PROXY=http://usuario:senha@host:porta`. O plano **gratuito** do Webshare é de datacenter e costuma ser bloqueado também. |
| Cloudflare WARP (wireproxy) | grátis | IPs da Cloudflare também são marcados com frequência. |
| Cookies de uma conta | grátis | Ajudam pouco contra IP marcado e a conta pode ser sinalizada. Use uma conta descartável, nunca versione os cookies. |

## Diagnóstico

```bash
pm2 logs galhardo-backend-pm2 | grep -i "bot-check\|youtube"
curl -s localhost:4416/ping                    # PO Token provider vivo?
backend/.venv/bin/yt-dlp --proxy socks5h://127.0.0.1:1080 --js-runtimes node --simulate "https://youtu.be/jNQXAC9IVRw"
```

Extração quebrou para todas as plataformas de uma vez? Normalmente é versão do yt-dlp: atualize `backend/requirements.txt` e faça deploy.
