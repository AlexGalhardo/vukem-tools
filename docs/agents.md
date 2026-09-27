# Guia para agentes

Contexto mínimo para trabalhar neste repositório sem reler tudo. Documentação em português; código e identificadores em inglês.

## Mapa rápido

| Quero mexer em... | Arquivo |
|---|---|
| Nova plataforma / domínio aceito | `backend/app/platforms.py` (rotas saem de `routers/platforms.py`) |
| Opções do yt-dlp, proxy, cookies, bot-check | `backend/app/services/downloader.py` |
| Jobs, progresso, clipes de 60s | `backend/app/services/jobs.py` |
| Cut & crop, fatiamento, ffmpeg | `backend/app/services/editor.py` + `routers/edit.py` |
| Novo erro | `backend/app/errors.py` + `frontend/src/lib/errors.ts` + `telegram/app/errors.py` |
| Configuração | `backend/app/config.py`, `telegram/app/config.py`, `frontend/src/lib/config.ts` (+ `.env.example`) |
| Proxy do frontend para o backend | `frontend/src/lib/api-proxy.ts`, `download-request.ts` |
| Telas | `frontend/src/components/download-form.tsx`, `cut-crop-studio.tsx` |
| Conversa do bot | `telegram/app/handlers.py`, textos em `presenters.py` |
| Docker / compose | `infra/docker/`, `infra/docker-compose*.yml` |
| Deploy | `setups/setup-unix-vps-pm2.sh` (produção), [deployment.md](deployment.md) |

## Invariantes (não quebre)

1. Só o backend toca yt-dlp/ffmpeg; frontend e bot falam com ele por HTTP com `X-API-Key`.
2. Backend com **1 worker**: jobs vivem em memória e o polling precisa cair no mesmo processo.
3. yt-dlp é síncrono: sempre `asyncio.to_thread`. ffmpeg via `subprocess.run` em thread (Windows).
4. `extractor_args` do yt-dlp são **listas** de strings (string vira lista de caracteres).
5. Nenhum teste acessa a rede; nenhum segredo é versionado ou logado.
6. Versão SemVer idêntica nos 3 workspaces; commits em Conventional Commits.
7. Frontend: scroll normal de documento; não reintroduza layout de altura fixa (`h-full`/`overflow-hidden` nas páginas).
8. `frontend/AGENTS.md` é regenerado pelo `next dev` (Next.js 16: leia `frontend/node_modules/next/dist/docs/` antes de usar APIs novas).

## Fluxo de trabalho

1. Leia o [`PLAN.md`](../PLAN.md) (estado e próximos marcos) e a doc da área.
2. Implemente em fatias pequenas, com teste (skills `incremental-implementation`, `test-driven-development`).
3. Endpoint/rota/proxy/bot mudou → skill `galhardo-video-secure-endpoint`.
4. Antes de concluir → skill `galhardo-video-local-verification` (`bun run check && bun run build`).
5. Registre no `CHANGELOG.md` (`[Unreleased]`) e marque o item no `PLAN.md`.

## Pegadinhas

- `backend/.env` é lido com `cwd=backend/` (no PM2 também). Os testes desligam o `.env` via `ENV_FILE=""`.
- O PO Token provider é best-effort: fora do ar, o yt-dlp segue sem token.
- `YOUTUBE_PROXY` vale só para o YouTube; as outras plataformas saem direto.
- Instagram quase sempre exige cookies; X e TikTok normalmente não.
- `DEPLOY_TARGET` (backend, frontend, bot) identifica quem respondeu e vai em header HTTP: é sanitizado para ASCII nos três workspaces; mantenha isso ao mexer ([deployment.md](deployment.md#qual-deploy-respondeu)).
- Validação de domínio é por host exato: subdomínio novo de compartilhamento precisa entrar em `PLATFORMS`.
