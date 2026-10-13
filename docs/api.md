# API do backend

Swagger em `/docs` (desligue com `DOCS_ENABLED=false`). Exemplos prontos em [`backend/requests.http`](../backend/requests.http).

**Autenticação:** com `API_KEY` definido, toda rota sob `/api/` exige o header `X-API-Key` (401 `UNAUTHORIZED` sem ele). `/` e `/health` são públicas.

**Deploy:** toda resposta traz `X-Deploy-Target` com o rótulo do deploy (`DEPLOY_TARGET`, ou `Koyeb (<região>)` detectado pelas variáveis `KOYEB_*`, ou `local`). O proxy do frontend repassa como `X-Backend-Deploy` e acrescenta `X-Frontend-Deploy`.

**Erros:** sempre `{"detail": "mensagem pt-BR", "error_type": "CODIGO"}`.

| Método | Rota | Descrição |
|---|---|---|
| GET | `/health` | `{status, version, yt_dlp_version, deploy_target}` |
| GET | `/api/v1/detect?url=` | `{platform}`: `youtube`, `instagram`, `twitter`, `tiktok` ou `null` |
| POST | `/api/v1/{plataforma}/info` | Metadados (título, duração, thumbnail, tamanho estimado) |
| POST | `/api/v1/{plataforma}/download` | Download síncrono em streaming |
| POST | `/api/v1/{plataforma}/download/start` | Inicia job → `{job_id}` |
| POST | `/api/v1/{plataforma}/download/clips/start` | Inicia job que fatia em clipes de 60s |
| GET | `/api/v1/downloads/{job_id}` | Progresso (`status`, `percent`, `phase`, `clips`...) |
| GET | `/api/v1/downloads/{job_id}/file` | Arquivo final (modo clipes: o `.zip`) |
| GET | `/api/v1/downloads/{job_id}/clips/{n}/file` | Um clipe (n começa em 1) |
| GET | `/api/v1/downloads/{job_id}/zip` | Todos os clipes |
| GET | `/api/v1/edit/capabilities` | ffmpeg disponível, limite de upload |
| POST | `/api/v1/edit/cut-crop` | multipart: `file`, `start`, `end`, `crop_x/y/w/h`, `output_format` |
| POST | `/api/v1/edit/split` | multipart: `file`, `segment_seconds` (5–600) → `.zip` + `X-Clip-Count` |
| POST | `/api/v1/{plataforma}/download/start` com `compress: true` | o MP4 baixado é comprimido antes da entrega; o status traz `original_mb` e `compressed_mb` quando houve ganho |
| POST | `/api/v1/compress/media` | multipart: `file` (vídeo ou áudio) → `.mp4` (H.264 CRF 28, AAC 128k) ou `.mp3` (128k) + `X-Original-Size`, `X-Compressed-Size`; `204` quando não há ganho |
| POST | `/api/v1/compress/pdf` | multipart: `file` (PDF) → PDF com imagens a ~150 dpi (Ghostscript `/ebook`), mesmos headers; `204` quando não há ganho |

Corpo de download: `{"url": "...", "format": "mp4" | "mp3", "quality": "best" | "worst"}`. `job_id` é hex de 32 caracteres.

## Códigos de erro

| `error_type` | HTTP | Quando |
|---|---|---|
| `INVALID_REQUEST` | 400/422 | Corpo inválido, URL de outra plataforma, parâmetro fora do limite |
| `INVALID_MEDIA` | 400 | Upload ilegível ou formato não permitido |
| `UNAUTHORIZED` | 401 | `X-API-Key` ausente/errada |
| `NOT_FOUND` | 404 | Job inexistente ou expirado |
| `CONFLICT` | 409 | Job ainda não terminou |
| `FILE_TOO_LARGE` | 413 | Passou de `MAX_FILE_SIZE_MB` / `MAX_UPLOAD_SIZE_MB` |
| `DOWNLOAD_FAILED`, `NETWORK_ERROR`, `YOUTUBE_BOT_CHECK`, `VIDEO_PROCESSING_FAILED` | 502 | Falha na origem ou no ffmpeg |
| `OUT_OF_MEMORY`, `SERVER_BUSY` | 503 | Sem RAM / `MAX_CONCURRENT_JOBS` atingido |
| `TIMEOUT` | 504 | ffmpeg passou do tempo limite |
| `NO_DISK_SPACE` | 507 | Disco cheio |
| `INTERNAL_ERROR` | 500 | Qualquer outra falha (sem stack trace na resposta) |
