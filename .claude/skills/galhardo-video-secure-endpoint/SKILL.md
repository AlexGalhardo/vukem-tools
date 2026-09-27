---
name: galhardo-video-secure-endpoint
description: Security checklist for adding or changing an HTTP endpoint, platform, proxy route, upload handler or bot command in the galhardo-video monorepo. Use whenever backend routers, frontend src/app/api route handlers, telegram handlers, yt-dlp options or ffmpeg invocations change.
---

# Endpoint seguro no galhardo-video

Auditoria de referência: `docs/security.md` (OWASP Top 10:2025). Siga os itens que se aplicam.

## Backend (FastAPI)

1. **Autenticação**: toda rota sob `/api/` passa pelo middleware de `X-API-Key` em `app/main.py` (comparação em tempo constante). Não crie rotas fora de `/api/` que exponham dados ou gastem CPU; `/health` e `/` são as únicas públicas.
2. **Plataformas**: URLs só entram via `app/platforms.py` (`validate_platform_url` compara o *hostname*, não o `netloc`). Nova plataforma = nova entrada em `PLATFORMS` com `extractors` restritos; nunca libere o extractor `generic` (SSRF).
3. **Validação de entrada**: use tipos/limites do Pydantic ou `Annotated[..., Form(ge=..., le=...)]`. IDs de job são `^[0-9a-f]{32}$`.
4. **Erros**: lance subclasses de `AppError` (`app/errors.py`). Mensagens nunca ecoam a entrada do usuário nem caminhos internos. Novo `error_type` = atualizar também `frontend/src/lib/errors.ts` e `telegram/app/errors.py` (há testes que comparam os conjuntos).
5. **Recursos**: trabalho pesado vira job (`app/services/jobs.py`), respeitando `MAX_CONCURRENT_JOBS`. Uploads são gravados em blocos com limite (`_save_upload`).
6. **ffmpeg/ffprobe**: sempre lista de argumentos (nunca shell) e `_INPUT_HARDENING` (`-protocol_whitelist file`) + checagem de demuxer em `probe`, para impedir playlists HLS/concat que leem arquivos locais.
7. **Arquivos devolvidos**: `stream_file_response` (nome sanitizado, `filename*` UTF-8, `Cache-Control: no-store`).
8. **Segredos**: nunca logue cookies, tokens ou variáveis de ambiente. Cookies ficam em `backend/cookies/` (ignorado pelo git) ou em variável de ambiente.

## Frontend (route handlers em `src/app/api`)

- Use `proxyJson` / `proxyStream` / `proxyUpload` / `forwardDownloadRequest` de `src/lib/`: eles injetam a `API_KEY`, repassam só headers permitidos e não vazam a URL interna do backend.
- Valide parâmetros de rota (`isJobId`, `isClipIndex`) antes de montar o path do backend.
- Segredo nunca em `NEXT_PUBLIC_*`. Headers de segurança/CSP ficam em `next.config.ts`.

## Bot do Telegram

- Todo update passa pela `AllowlistMiddleware` (fail-closed, inclusive updates sem remetente).
- Texto vindo das plataformas vai com `parse_mode="HTML"` e `html.escape` (`app/presenters.py`).
- O bot só fala com o backend via `BackendClient` (com `X-API-Key`).

## Antes de concluir

Adicione um teste para o caso malicioso (ex.: `backend/tests/test_security.py`) e rode a skill `galhardo-video-local-verification`.
