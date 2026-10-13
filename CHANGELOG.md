# Changelog

Todas as mudanças relevantes deste projeto ficam aqui. O formato segue o [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) e o projeto usa [Versionamento Semântico](https://semver.org/lang/pt-BR/) (versão única para backend, frontend e bot).

## [Unreleased]

### Added

- Download com compressão: `compress: true` em `/download/start` comprime o MP4 depois de baixar (H.264 CRF 28, AAC 128k) e entrega o menor dos dois; o frontend envia sempre e mostra quanto diminuiu em MB e em %.
- Compressão por API, para o gerenciador de arquivos do `lekzera.xyz`: `POST /api/v1/compress/media` (vídeo → `.mp4` H.264 CRF 28 + AAC 128k; áudio → `.mp3` 128k) e `POST /api/v1/compress/pdf` (Ghostscript, imagens a ~150 dpi). Respondem `204` quando o resultado não fica menor.
- Backend também no Railway (projeto `video-lekzera-xyz`), usado pelo `lekzera.xyz` para a compressão: a instância gratuita do Koyeb não termina um PDF a tempo.
- `docs/agent-setup.md`: ferramentas e estado dos acessos de um agente (Koyeb, Vercel, GitHub, Hostinger), onde cada parte roda e o que está pendente.
- `docs/hostinger-agent-setup.md`: como agentes de IA operam a VPS pela API pública e pelo MCP da Hostinger, e o que o Alex configura uma vez.
- Ghostscript instalado pelo `setup-unix-vps-pm2.sh`, pela imagem Docker do backend e pelo CI; binário configurável em `GHOSTSCRIPT_BIN`.
- Identificação do deploy que respondeu (Koyeb + Vercel, VPS Hostinger, Docker): `DEPLOY_TARGET` no backend (autodetecta `Koyeb (<região>)` pelas variáveis `KOYEB_*`), header `X-Deploy-Target` em toda resposta e campo `deploy_target` em `/` e `/health`.
- Frontend: route handlers carimbam `X-Frontend-Deploy` (`Vercel (<região>)` ou `DEPLOY_TARGET`) e `X-Backend-Deploy`; nova rota `/api/deploy`; badge na navbar com `frontend → api` e aviso no console quando a resposta muda de deploy.
- Bot: rodapé `🛰️ Bot: … · API: …` no preview, na entrega e nos erros, e comando `/status`.
- `setup-unix-vps-pm2.sh` grava `DEPLOY_TARGET=VPS Hostinger` no backend e no bot (sem sobrescrever valor personalizado); `docker-compose.prod.yml` usa `VPS Docker` por padrão.

### Changed

- Hook `pre-commit` corrige lint e format automaticamente (Ruff no Python, Biome no TypeScript) antes de verificar; `pre-push` roda os testes antes do build.

### Fixed

- `setup-unix-vps-pm2.sh` / `setup-unix-vps-docker.sh`: `APP_DIR` vem da pasta do próprio script (antes, rodar de dentro de `setups/` caía no padrão `/opt/...` inexistente).
- `setup-unix-vps-pm2.sh`: recria o venv quando o `pip` não executa (pasta do app movida ou `python3` do sistema atualizado dá "bad interpreter").
- CI do frontend: `next typegen` antes do `tsc` (tipos globais `RouteContext`/`LayoutProps`).
- Aviso de múltiplos lockfiles no `next build` (`outputFileTracingRoot`).
- Deploy no Koyeb apontando para o Dockerfile antigo (`backend/Dockerfile`).

## [2.0.0] - 2026-09-26

Refatoração geral: segurança, arquitetura, infraestrutura, CI/CD e correção do fluxo do YouTube.

### Added

- `YOUTUBE_PROXY`: proxy aplicado só ao YouTube, com túnel SSH pela Tailscale até uma máquina de casa (`setups/setup-*-home-proxy.sh`) para sair com IP residencial.
- Runtime JS do yt-dlp (`yt-dlp-ejs` + Node, `YTDLP_JS_RUNTIME`) para não perder formatos do YouTube.
- Autenticação opcional do backend por `API_KEY` (header `X-API-Key`), gerada automaticamente pelos setups de VPS.
- Limite de jobs simultâneos (`MAX_CONCURRENT_JOBS`, erro `SERVER_BUSY`) e novos erros `UNAUTHORIZED`, `NOT_FOUND` e `CONFLICT`.
- Headers de segurança no frontend (CSP, `frame-ancestors`, `nosniff`, `Permissions-Policy`) e no backend.
- Testes do bot do Telegram e do frontend (`bun test`); testes de segurança no backend.
- CI no GitHub Actions (lint, format, tsc, testes, build, auditoria de dependências, imagens Docker, smoke E2E, shellcheck, commitlint e checagem de versão) e CD (deploy na VPS e GitHub Release por tag).
- Husky + commitlint (Conventional Commits) com `bun run check` no pre-commit e `bun run build` no pre-push, cobrindo os três workspaces.
- Documentação por área em `docs/`, `PLAN.md` e skills do projeto em `.claude/skills/`.

### Changed

- Bot do Telegram: no modo Stories cada clipe de 60s é enviado como um vídeo separado, em vez de um `.zip`.
- Plataformas definidas num único registro (`backend/app/platforms.py`); as quatro rotas por plataforma são geradas a partir dele.
- Proxy do frontend reescrito num único módulo (`src/lib/api-proxy.ts`), sem expor a URL do backend ao browser.
- YouTube usa os player clients padrão do yt-dlp; cookies só entram se a tentativa sem cookies cair no bot-check.
- Infra movida para `infra/` (Dockerfiles com contexto na raiz, compose de dev e de produção com portas só em loopback) e scripts para `setups/` (`setup-[windows|unix]-*.sh`).
- `setups/setup-unix-vps-pm2.sh` reescrito de forma enxuta e idempotente (mantém os nomes de processo e o site do Caddy da produção).
- Bot configurado por um único `API_BASE_URL` (sai o par `VPS_API_ENDPOINT`/`VPS_API_BASE_URL`).
- Indentação com tab (largura 4) em todos os workspaces; dependências pinadas nas últimas versões estáveis (Next.js 16.3, React 19.3, TypeScript 7).

### Removed

- Cookies do YouTube versionados (inclusive o fallback hardcoded), token do bot e credenciais de proxy que estavam no repositório.
- Variantes experimentais de deploy (container único, bot em PM2 com backend em Docker, compose só backend+bot), `DOCKER.md`, docs do Koyeb e `koyeb.exe`.
- Suporte a `tv` em `YTDLP_PLAYER_CLIENTS` com reconciliação automática de cookies.

### Fixed

- PO Token provider nunca era usado: o `base_url` ia como string e o yt-dlp o lia como `"h"`.
- Setup da VPS gravava o `.env` na raiz, que o backend (rodando em `backend/`) nunca lia; agora usa `backend/.env` e migra o antigo.
- Nome de arquivo Unicode no Cut & Crop causava erro 500 no `Content-Disposition`.
- Cookies `#HttpOnly_` eram ignorados no cálculo de expiração.

### Security

- Auditoria OWASP Top 10:2025 ([docs/security.md](docs/security.md)): bloqueio de playlists HLS/concat no ffmpeg (LFI/SSRF), allowlist de extractors do yt-dlp, validação de host, IDs de job validados, entrypoint sem vazar variáveis de ambiente, allowlist do bot fail-closed e HTML escapado nas mensagens.
- **Credenciais expostas no histórico antigo devem ser rotacionadas**: sessão do YouTube, token do bot e senha do proxy Webshare.

## [1.0.0] - 2026-09-14

Primeira versão, com histórico consolidado na refatoração 2.0.0.

### Added

- API FastAPI com download de YouTube, Instagram, X e TikTok em MP4/MP3, jobs em background com progresso e modo "Clipes p/ Stories" (60s).
- Cut & Crop e fatiamento de vídeos enviados, com ffmpeg.
- Frontend Next.js com telas de Download e Cut & Crop.
- Bot do Telegram com allowlist de usuários.
- Mitigações do bot-check do YouTube (PO Token provider, IPv4, rate limit, cookies).
- Deploy no Koyeb, na Vercel e em VPS com Caddy.

[Unreleased]: https://github.com/AlexGalhardo/galhardo-studio.vercel.app/compare/v2.0.0...HEAD
[2.0.0]: https://github.com/AlexGalhardo/galhardo-studio.vercel.app/releases/tag/v2.0.0
