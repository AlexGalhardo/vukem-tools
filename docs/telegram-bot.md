# Bot do Telegram

Mesmo fluxo do site, via chat: manda o link → escolhe **MP4** ou **MP3** → (vídeos acima de 60s) **vídeo completo** ou **padrão Stories** → acompanha o progresso → recebe o resultado.

- **Vídeo completo / MP3**: enviado no chat se couber em 50 MB (limite da Bot API); acima disso, um link para `PUBLIC_WEB_BASE_URL/api/download/file/<job>`.
- **Padrão Stories**: cada clipe de 60s (inclusive o último, mais curto) chega como **um vídeo separado** (`Parte 1/N`, `Parte 2/N`...), fácil de salvar um a um no celular. Clipe acima de 50 MB vira link individual.

## Código (`telegram/app/`)

| Arquivo | Papel |
|---|---|
| `main.py` | Cria `Bot`/`Dispatcher`, liga a allowlist, long polling |
| `handlers.py` | Conversa (FSM) e entrega dos arquivos |
| `api_client.py` | Único ponto de contato com o backend (`X-API-Key`) |
| `presenters.py` | Textos do chat (HTML escapado) |
| `middleware.py` | Allowlist de IDs, fail-closed |
| `errors.py` | `error_type` → mensagem (espelha o backend) |

## Configuração (`telegram/.env`)

| Variável | Descrição |
|---|---|
| `TELEGRAM_BOT_TOKEN` | Do @BotFather. Obrigatório. |
| `ALLOWED_TELEGRAM_USER_IDS` | IDs autorizados (CSV). Vazio = ninguém usa. |
| `API_BASE_URL` | Backend (`http://127.0.0.1:<porta>` no PM2, `http://backend:8000` no Docker) |
| `API_KEY` | Igual à do backend |
| `PUBLIC_WEB_BASE_URL` | Frontend público, para links de arquivos grandes |

## Rodar

```bash
cd telegram && .venv/bin/python -m app.main
```

Em produção roda no PM2 (`galhardo-telegram-bot`). Só pode existir **uma** instância por token: duas dão `TelegramConflictError`.
