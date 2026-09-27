---
name: galhardo-video-local-verification
description: Validate a change in the galhardo-video monorepo exactly like the git hooks and CI do (backend, frontend, telegram, Docker smoke) before calling it done. Use after any code, config, infra or dependency change in this repository, and before committing or opening a PR.
---

# Verificação local (igual ao CI)

Nada está pronto até estes comandos passarem **na raiz do repositório**:

```bash
bun run check    # format:check + lint + typecheck + test nos 3 workspaces (= hook pre-commit)
bun run build    # import dos apps Python + next build (= hook pre-push)
```

Mudou `infra/`, Dockerfile, compose, dependência de sistema ou fluxo ponta a ponta? Rode também:

```bash
bun run build:docker
API_PORT=18000 WEB_PORT=13000 docker compose -f infra/docker-compose.prod.yml up -d --wait
API=http://127.0.0.1:18000 WEB=http://127.0.0.1:13000 bash infra/smoke/stack.sh
docker compose -f infra/docker-compose.prod.yml down -v
```

Mudou script em `setups/` ou `infra/**/*.sh`: `shellcheck -x setups/*.sh infra/scripts/*.sh infra/docker/*.sh infra/smoke/*.sh`.

## Armadilhas conhecidas

- **Venvs**: `infra/scripts/python.sh` usa `backend/.venv` / `telegram/.venv` (criados por `setups/setup-*-local.sh`); sem eles cai no Python do sistema, que pode não ter as deps.
- **`.env` local vazando nos testes**: os `conftest.py` definem `ENV_FILE=""` antes de importar `app.*`. Não remova, senão o `backend/.env` do desenvolvedor muda o resultado dos testes.
- **CRLF no Windows**: `.gitattributes` força LF. Ao gerar arquivos via script Python, escreva bytes (`write_bytes`) ou `newline="\n"`; `write_text` em modo texto grava CRLF e quebra `.sh` dentro do Docker.
- **Indentação**: tab com largura 4 em todo código (`.editorconfig`); YAML e JSON gerados usam espaço.
- **ffmpeg**: testes de mídia são pulados sem `ffmpeg`/`ffprobe` no PATH. O CI instala; localmente instale para cobrir tudo.
- **YouTube em teste**: nenhum teste acessa a rede. Para validar extração real use `infra/smoke/real-download.sh` (depende do IP; ver `docs/youtube.md`).
- **Commit**: mensagens passam pelo commitlint (Conventional Commits, escopos em `commitlint.config.js`). Nunca use `--no-verify`.
