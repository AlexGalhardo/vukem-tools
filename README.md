<h1 align="center">Galhardo Video</h1>

<p align="center">Baixe vídeos do YouTube, Instagram, X e TikTok em MP4, MP3 ou clipes de 60s, e corte/recorte vídeos no navegador ou pelo Telegram.</p>

<p align="center">
	<a href="https://github.com/AlexGalhardo/galhardo-studio.vercel.app/actions/workflows/ci.yml"><img alt="CI" src="https://github.com/AlexGalhardo/galhardo-studio.vercel.app/actions/workflows/ci.yml/badge.svg?branch=main"></a>
	<a href="LICENSE"><img alt="Licença MIT" src="https://img.shields.io/badge/licen%C3%A7a-MIT-blue.svg"></a>
	<a href="CHANGELOG.md"><img alt="Versão" src="https://img.shields.io/badge/vers%C3%A3o-2.0.0-green.svg"></a>
</p>

## Sobre

Cole o link de um vídeo e baixe em alguns cliques, com barra de progresso real. Para Stories, o vídeo sai fatiado em clipes de até 60 segundos; no Telegram cada parte chega como um vídeo separado, pronta para salvar no celular.

- Download de YouTube, Instagram, X/Twitter e TikTok em MP4 (melhor ou menor qualidade) ou MP3
- Modo Stories: clipes consecutivos de até 60s, individuais ou em `.zip`
- Cut & Crop: corte por tempo e recorte por área de qualquer vídeo enviado
- Bot do Telegram com o mesmo fluxo, restrito a uma lista de usuários
- Erros explicados em português (sem memória, disco cheio, bloqueio do YouTube...)

## Stack

| Camada | Tecnologia |
|---|---|
| Backend | [FastAPI](https://fastapi.tiangolo.com) + [yt-dlp](https://github.com/yt-dlp/yt-dlp) + [ffmpeg](https://ffmpeg.org), Python 3.12 |
| Frontend | [Next.js](https://nextjs.org) 16 (App Router) + React 19 + TypeScript 7, Tailwind CSS 4, shadcn/ui, Zod |
| Bot | [aiogram](https://aiogram.dev) 3 + httpx |
| YouTube | PO Token ([bgutil](https://github.com/Brainicism/bgutil-ytdlp-pot-provider)), yt-dlp-ejs + Node, proxy residencial via [Tailscale](https://tailscale.com) |
| Qualidade | Ruff, Biome, pytest, `bun test`, Husky + commitlint, pip-audit / bun audit |
| Deploy | [Vercel](https://vercel.com) (frontend), VPS com PM2 + [Caddy](https://caddyserver.com) (backend e bot), Docker Compose opcional |

## Rodando localmente

Pré-requisitos: Python 3.12+, [Bun](https://bun.sh) 1.4.2 e, recomendados, ffmpeg e Node.js LTS. Escolha o script do seu sistema:

| | Nativo (dev, testes, hooks) | Docker (stack completa) |
|---|---|---|
| **Windows** (Git Bash) | [`setup-windows-local.sh`](setups/setup-windows-local.sh) | [`setup-windows-docker.sh`](setups/setup-windows-docker.sh) |
| **Linux/macOS** | [`setup-unix-local.sh`](setups/setup-unix-local.sh) | [`setup-unix-docker.sh`](setups/setup-unix-docker.sh) |

```bash
git clone git@github.com:AlexGalhardo/galhardo-studio.vercel.app.git
cd galhardo-studio.vercel.app
bash setups/setup-unix-local.sh      # ou o script da tabela acima
cd backend && .venv/bin/uvicorn app.main:app --reload --port 8000   # em outro terminal: cd frontend && bun run dev
```

Variáveis de ambiente: [`backend/.env.example`](backend/.env.example), [`frontend/.env.example`](frontend/.env.example) e [`telegram/.env.example`](telegram/.env.example).

## Comandos

Na raiz, valendo para os três workspaces:

```bash
bun run check          # format:check + lint + typecheck + testes (hook de pre-commit)
bun run build          # build de produção (hook de pre-push)
bun run format         # Ruff + Biome
bun run test           # pytest (backend, telegram) + bun test (frontend)
bun run build:docker   # imagens de produção
```

## Documentação

A pasta [`docs/`](docs) explica o projeto por área (também é o contexto dos agentes de IA, via [`AGENTS.md`](AGENTS.md)):

- [Arquitetura](docs/architecture.md)
- [API do backend](docs/api.md)
- [Desenvolvimento, testes e convenções](docs/development.md)
- [Deploy e CI/CD](docs/deployment.md)
- [YouTube: bloqueio por IP e proxy residencial](docs/youtube.md)
- [Bot do Telegram](docs/telegram-bot.md)
- [Segurança (auditoria OWASP Top 10:2025)](docs/security.md)
- [Guia para agentes](docs/agents.md) e [plano de trabalho](PLAN.md)

## Contribuindo

Commits seguem [Conventional Commits](https://www.conventionalcommits.org/pt-br) (validado pelo commitlint) e o versionamento segue [SemVer](https://semver.org/lang/pt-BR/), com a mesma versão nos três workspaces. Antes de abrir um PR, `bun run check` e `bun run build` precisam passar: o CI bloqueia em todos. Mudanças relevantes entram no [`CHANGELOG.md`](CHANGELOG.md).

## Créditos e licença

Criado por [Alex Galhardo](https://github.com/AlexGalhardo). Distribuído sob a [licença MIT](LICENSE).
