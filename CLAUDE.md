# Galhardo Video: guia para agentes

Monorepo de download e edição de vídeos.

## Estrutura

- `backend/`: FastAPI + yt-dlp + ffmpeg
- `frontend/`: Next.js 16
- `telegram/`: bot aiogram
- `infra/`: Docker, compose e scripts
- `setups/`: scripts de setup (`setup-[windows|unix]-*.sh`)

## Documentação

- [`docs/agents.md`](docs/agents.md): mapa, invariantes e pegadinhas (comece por aqui)
- [`PLAN.md`](PLAN.md): estado e próximos marcos
- [`docs/architecture.md`](docs/architecture.md), [`docs/development.md`](docs/development.md), [`docs/deployment.md`](docs/deployment.md)
- [`docs/youtube.md`](docs/youtube.md), [`docs/security.md`](docs/security.md), [`docs/api.md`](docs/api.md)
- [`docs/hostinger-agent-setup.md`](docs/hostinger-agent-setup.md): VPS da Hostinger por API e MCP

## Skills

- Índice em [`.claude/skills/SOURCES.md`](.claude/skills/SOURCES.md)
- `galhardo-video-secure-endpoint`: ao mexer em rotas, proxy ou bot
- `galhardo-video-local-verification`: antes de concluir qualquer tarefa

## Definição de pronto

- `bun run check && bun run build` verdes
- Conventional Commits e SemVer único nos 3 workspaces
- Indentação com tab (largura 4)
- `CHANGELOG.md` atualizado
