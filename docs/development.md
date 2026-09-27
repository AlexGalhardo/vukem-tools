# Desenvolvimento

## Pré-requisitos

Python 3.12+, [Bun](https://bun.sh) 1.4.2, Git. Recomendados: ffmpeg/ffprobe (Cut & Crop e testes de mídia) e Node.js LTS (runtime JS do yt-dlp para o YouTube). Docker só para a stack em containers.

## Setup

| | Nativo (hooks, testes, dev) | Docker (stack completa) |
|---|---|---|
| Linux/macOS | `bash setups/setup-unix-local.sh` | `bash setups/setup-unix-docker.sh` |
| Windows (Git Bash) | `bash setups/setup-windows-local.sh` | `bash setups/setup-windows-docker.sh` |

O setup nativo cria `backend/.venv` e `telegram/.venv`, instala tudo (inclusive husky) e copia os `.env.example`. Os scripts Docker escolhem portas livres e aceitam `WITH_TELEGRAM=1`.

Rodando nativo (um terminal cada):

```bash
cd backend && .venv/bin/uvicorn app.main:app --reload --port 8000   # Windows: .venv/Scripts/
cd frontend && bun run dev                                          # http://localhost:3000
cd telegram && .venv/bin/python -m app.main                         # precisa de TELEGRAM_BOT_TOKEN
```

## Comandos (raiz)

| Comando | O que faz nos 3 workspaces |
|---|---|
| `bun run format` | Ruff format + Biome (escreve) |
| `bun run format:check` | Só verifica |
| `bun run fix` | Corrige lint e format (Ruff `--fix` + `format`, Biome `check --write`) |
| `bun run lint` | Ruff check + Biome lint (warnings viram erro) |
| `bun run typecheck` | `tsc --noEmit` (frontend) |
| `bun run test` | pytest (backend, telegram) + `bun test` (frontend) |
| `bun run build` | Importa os apps Python + `next build` |
| `bun run check` | format:check + lint + typecheck + test |
| `bun run build:docker` | Build das 3 imagens de produção |

Cada workspace também tem os seus (`cd frontend && bun run dev`, `cd backend && .venv/bin/pytest`...). O Python é resolvido por `infra/scripts/python.sh` (venv do workspace, ou o do sistema no CI).

## Git hooks (husky)

| Hook | Roda |
|---|---|
| `commit-msg` | commitlint ([Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/), escopos em `commitlint.config.js`) |
| `pre-commit` | `bun run fix` (Ruff `check --fix` + `format` no Python, Biome `check --write` no TypeScript) e re-stage dos arquivos já staged; depois `bun run check` |
| `pre-push` | `bun run test` e depois `bun run build` |

Instalados por `bun install` na raiz (`prepare: husky`). Não use `--no-verify`.

## Convenções

- Indentação com **tab (largura 4)** em todo código (`.editorconfig`, Ruff e Biome). YAML usa espaços. Finais de linha LF (`.gitattributes`).
- Python: Ruff (linha de 120), tipos em funções públicas. TypeScript: strict, sem `any`, sem `@ts-ignore`.
- Comentários só para o *porquê* e edge cases.
- Versão única [SemVer](https://semver.org/) nos 3 workspaces: `package.json` (raiz e frontend), `backend/pyproject.toml`, `backend/app/config.py::APP_VERSION` e `telegram/pyproject.toml`. O CI falha se divergirem ou se a tag `vX.Y.Z` não bater.
- Dependências pinadas em versão exata, sempre a última estável.
- Mudança relevante entra no [`CHANGELOG.md`](../CHANGELOG.md) ([Keep a Changelog](https://keepachangelog.com/en/1.1.0/)).

## Testes

- `backend/tests`: nunca acessam a rede (yt-dlp mockado). Os de ffmpeg geram vídeos sintéticos e são pulados sem ffmpeg. `test_security.py` cobre os controles de segurança.
- `telegram/tests`: `httpx.MockTransport` para o backend e mocks da Bot API.
- `frontend/src/**/*.test.ts`: `bun test` para `src/lib` (proxy, schemas, formatação, erros).
- `infra/smoke/stack.sh`: E2E contra a stack Docker no ar (roda no CI). `infra/smoke/real-download.sh`: downloads reais (usa rede; manual).
