# Deploy e CI/CD

## Topologia de produção

| Peça | Onde | Como |
|---|---|---|
| Frontend | Vercel | Integração Git (Root Directory = `frontend`). Envs: `API_BASE_URL`, `API_KEY`. |
| Backend + bot + PO Token | VPS Ubuntu (Hostinger) | PM2 nativo atrás do Caddy: `setups/setup-unix-vps-pm2.sh` |
| Saída do YouTube (opcional) | PC/Raspberry em casa | Túnel SSH pela Tailscale: `setups/setup-*-home-proxy.sh` ([youtube.md](youtube.md)) |

Alternativa self-hosted completa (sem Vercel): `setups/setup-unix-vps-docker.sh` sobe `infra/docker-compose.prod.yml` (backend + frontend + bot) atrás do Caddy em `video.galhardo.tech`.

## VPS com PM2 (produção atual)

```bash
ssh root@<vps>
cd /opt/galhardo-studio && git pull
sudo bash setups/setup-unix-vps-pm2.sh
# com o túnel do YouTube:
sudo HOME_PROXY_SSH=usuario@100.x.y.z bash setups/setup-unix-vps-pm2.sh
```

O script é idempotente: instala Node 24 + PM2, Caddy, os venvs, compila o PO Token provider, cria/migra `backend/.env` e `telegram/.env`, **gera `API_KEY` se estiver vazia** (e imprime o valor para configurar na Vercel), sorteia uma porta livre (gravada em `.env.deploy`), sobe os processos e publica o backend em `https://$DOMAIN` (default `galhardo.tech`).

| Processo PM2 | O que é |
|---|---|
| `galhardo-backend-pm2` | uvicorn, 1 worker, `127.0.0.1:<porta>` |
| `galhardo-telegram-bot` | bot (só sobe com `TELEGRAM_BOT_TOKEN`) |
| `galhardo-pot-provider` | PO Token provider em `127.0.0.1:4416` |
| `galhardo-youtube-proxy` | `ssh -D 127.0.0.1:1080` até a sua casa (opcional) |

Depois de editar um `.env`: `pm2 restart all`. Logs: `pm2 logs <nome>`.

## Variáveis de ambiente

Documentadas em cada `.env.example`: [`backend`](../backend/.env.example), [`frontend`](../frontend/.env.example), [`telegram`](../telegram/.env.example). A mesma `API_KEY` precisa estar no backend, no frontend (Vercel) e no bot.

## CI (`.github/workflows/ci.yml`)

Em PR e push na `main`:

| Job | Verifica |
|---|---|
| `conventions` | commitlint nos commits do PR; versão SemVer idêntica nos 3 workspaces (e igual à tag, se houver) |
| `backend` / `telegram` | `ruff format --check`, `ruff check`, `pytest`, import do app, `pip-audit` |
| `frontend` | Biome (format + lint), `tsc`, `bun test`, `next build`, `bun audit` |
| `docker` | build das 3 imagens + stack no ar + `infra/smoke/stack.sh` |
| `shellcheck` | todos os scripts de `setups/` e `infra/` |

## CD (`.github/workflows/cd.yml`)

Disparado quando o CI termina verde:

- **Push na `main`** → `deploy-vps`: SSH na VPS, `git reset --hard <sha>` e reexecuta `setups/setup-unix-vps-pm2.sh`. Só roda se os secrets existirem (environment `production`): `VPS_HOST`, `VPS_USER`, `VPS_SSH_KEY`; variáveis opcionais `VPS_APP_DIR`, `HOME_PROXY_SSH`. O usuário precisa de `sudo` sem senha.
- **Tag `vX.Y.Z`** → `release`: cria o GitHub Release com a seção correspondente do `CHANGELOG.md`.

## Release (SemVer)

1. Atualize a versão nos 5 lugares listados em [development.md](development.md#convenções) e mova o `[Unreleased]` do CHANGELOG para `[X.Y.Z] - AAAA-MM-DD`.
2. `git commit -m "chore(release): vX.Y.Z"` → `git tag vX.Y.Z` → `git push --follow-tags`.
