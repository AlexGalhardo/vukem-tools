# PLAN.md: Galhardo Video

Documento vivo do projeto: contexto, marcos e microtarefas. Marque `[x]` ao concluir e registre no `CHANGELOG.md`.

## Contexto

- **Produto**: baixar vídeos do YouTube, Instagram, X e TikTok (MP4, MP3, clipes de 60s) e cortar/recortar vídeos, pelo site ou pelo Telegram.
- **Produção**: frontend na Vercel; backend + bot + PO Token via PM2 numa VPS Hostinger (Ubuntu) atrás do Caddy.
- **Problema central**: o YouTube bloqueia o IP de datacenter da VPS. A solução adotada é sair pela internet de casa via Tailscale + túnel SSH (`YOUTUBE_PROXY`), ver [docs/youtube.md](docs/youtube.md).
- **Versão atual**: 2.0.0 (refatoração geral de 2026-09-26).

## Marco 1: Refatoração 2.0.0 (concluído)

### Repositório e convenções

- [x] Histórico reescrito em 10 commits no padrão Conventional Commits
- [x] Husky + commitlint (escopos: backend, frontend, telegram, infra, setups, ci, docs, deps, release)
- [x] `pre-commit` = `bun run check`; `pre-push` = `bun run build` (3 workspaces)
- [x] SemVer único (2.0.0) validado no CI
- [x] Tab (largura 4) via `.editorconfig`, Ruff e Biome; LF via `.gitattributes`
- [x] Dependências pinadas nas últimas estáveis

### Estrutura

- [x] `setups/` com `setup-[windows|unix]-*.sh` (local, docker, vps-pm2, vps-docker, home-proxy)
- [x] `infra/` com Dockerfiles, compose de dev e prod, scripts e smoke tests
- [x] Removidos experimentos de deploy, docs do Koyeb, `koyeb.exe`, `DOCKER.md`, skills fora de contexto

### Segurança (OWASP Top 10:2025, ver [docs/security.md](docs/security.md))

- [x] Segredos removidos do repositório e `.gitignore` corrigido
- [x] `API_KEY` no backend, frontend e bot
- [x] Hardening do ffmpeg (protocolo e demuxers), allowlist de extractors do yt-dlp, validação por host
- [x] Limite de jobs, validação de IDs, headers de segurança, CORS restrito
- [x] Allowlist do bot fail-closed, HTML escapado

### Código

- [x] Backend: registro único de plataformas, downloader e jobs simplificados, erros padronizados
- [x] Correção do PO Token (`base_url` como lista) e runtime JS do yt-dlp
- [x] Frontend: proxy DRY (`lib/api-proxy.ts`), `lib/http.ts` nos componentes, sem URL interna vazando
- [x] Bot: clipes de 60s enviados como vídeos separados; `presenters.py`; config simplificada

### Qualidade e entrega

- [x] Testes: backend (pytest), telegram (pytest, novo), frontend (`bun test`, novo)
- [x] CI: lint, format, tsc, testes, build, auditoria, Docker + smoke, shellcheck, commitlint, versão
- [x] CD: deploy na VPS por SSH e GitHub Release por tag
- [x] README, CLAUDE.md = AGENTS.md, docs por área, CHANGELOG no Keep a Changelog

## Marco 2: Colocar a 2.0.0 em produção (ação do Alex)

- [ ] Rotacionar credenciais que estiveram no GitHub
  - [ ] Encerrar todas as sessões da conta Google dos cookies do YouTube
  - [ ] Revogar o token do bot no @BotFather (`/revoke`) e atualizar `telegram/.env`
  - [ ] Trocar a senha do proxy Webshare
- [ ] Publicar o histórico novo: `git push --force-with-lease origin main` e `git push origin v2.0.0`
- [ ] Na VPS: `cd /opt/galhardo-studio && git fetch && git reset --hard origin/main && sudo bash setups/setup-unix-vps-pm2.sh`
- [ ] Copiar a `API_KEY` impressa pelo setup para a Vercel (`API_BASE_URL`, `API_KEY`) e fazer redeploy
- [ ] Remover da Vercel a variável antiga `NEXT_PUBLIC_API_BASE_URL`
- [ ] Testar Instagram, X e TikTok pelo site e pelo bot

## Marco 3: YouTube pela internet de casa (ação do Alex)

- [ ] Escolher a máquina de casa que fica ligada 24h (PC, notebook ou Raspberry Pi)
- [ ] Criar conta na Tailscale e gerar uma auth key para a VPS
- [ ] Rodar o setup da VPS com `HOME_PROXY_SSH` para gerar a chave e copiar a chave pública
- [ ] Rodar `setups/setup-{unix,windows}-home-proxy.sh` em casa com `VPS_PUBKEY`
- [ ] Reexecutar o setup da VPS e confirmar que o IP de saída é o de casa
- [ ] Baixar um vídeo do YouTube pelo site e pelo bot

## Marco 4: CD automático (opcional)

- [ ] Criar o environment `production` no GitHub com os secrets `VPS_HOST`, `VPS_USER`, `VPS_SSH_KEY`
- [ ] Variáveis opcionais: `VPS_APP_DIR`, `HOME_PROXY_SSH`
- [ ] Dar `sudo` sem senha ao usuário de deploy para `bash setups/setup-unix-vps-pm2.sh`
- [ ] Confirmar que a integração Git da Vercel usa Root Directory = `frontend`

## Marco 5: Melhorias futuras (backlog)

- [ ] Registro de jobs em Redis para rodar mais de 1 worker/réplica
- [ ] Rate limit por IP na borda (Vercel Firewall) para o site público
- [ ] Dividir `download-form.tsx` e `cut-crop-studio.tsx` em hooks (`useDownloadJob`, `useCropBox`) e componentes menores
- [ ] Polling do frontend com `setTimeout` encadeado (evita requisições sobrepostas)
- [ ] Testes E2E de UI com Playwright
- [ ] Healthcheck do túnel do YouTube exposto em `/health` (sem dados sensíveis)
- [ ] Nonce no CSP para remover `'unsafe-inline'`
