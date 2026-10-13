# Acessos para agentes de IA (envs, deploys, DNS)

O que um agente (Claude Code e similares) usa na máquina do Alex para operar este projeto sem ninguém clicar em
painel. Estado conferido em 2026-10-13. Nenhum segredo fica neste repositório: cada ferramenta guarda o próprio login.

## Onde cada parte roda

| Parte | Onde | Como publica |
|---|---|---|
| Backend (FastAPI) | Koyeb, app `galhardo-video`, serviço `backend`, região `was`, instância `free` | build do `infra/docker/backend.Dockerfile` a partir do `main` |
| Frontend (Next.js) | Vercel, projeto `video.lekzera.xyz` (time `galhardos-projects`) | integração Git da Vercel |
| Código | GitHub `AlexGalhardo/video.lekzera.xyz` (privado; o nome antigo `galhardo-video.vercel.app` redireciona) | `git push origin main` |
| VPS Hostinger | alternativa de hospedagem (`setups/setup-unix-vps-pm2.sh`); **não é o backend em produção hoje** | SSH ou `cd.yml` |

Endereço público do backend: `https://galhardo-video-alexgalhardo-b4e8425d.koyeb.app`.

## Ferramentas e estado

| Ferramenta | Para quê | Estado em 2026-10-13 |
|---|---|---|
| `gh` (GitHub CLI) | repositório, releases, execuções do Actions | logado como `AlexGalhardo` |
| `vercel` (CLI 62) | envs e deploys do frontend | logado; escolher o projeto com `vercel link --project video.lekzera.xyz` |
| `koyeb` (CLI) | envs, secrets, deploys e logs do backend | logado; o binário está em `~/OneDrive/Documentos/koyeb/koyeb.exe` e **não está no PATH** |
| MCP da Hostinger (plugin `hostinger` do Claude Code) | VPS, DNS, domínios | autorizado por OAuth; ver [`hostinger-agent-setup.md`](hostinger-agent-setup.md) |
| MCP do GitHub (plugin `github`) | o mesmo que o `gh` | **falhando**: o token configurado é recusado (HTTP 401); o `gh` cobre |

## Passos do Alex (uma vez por máquina)

1. `gh auth login`
2. `vercel login`
3. `koyeb login` (abre o navegador) e pôr a pasta do `koyeb.exe` no PATH do usuário.
4. Hostinger: autorizar o plugin no navegador (o agente gera o link) ou
   `claude mcp add --transport http hostinger https://mcp.hostinger.com`.
5. Opcional: corrigir o token do plugin `github` do Claude Code.

## Comandos que o agente usa

```sh
# Koyeb (backend)
koyeb service list --app galhardo-video
koyeb deployment list --service <id-do-serviço>
koyeb service logs galhardo-video/backend
koyeb service redeploy galhardo-video/backend          # publica o main atual
koyeb secret create NOME --value-from-stdin            # o valor nunca vai na linha de comando
koyeb service update galhardo-video/backend --env NOME=@NOME   # env apontando para o secret

# Vercel (frontend)
vercel env ls production
vercel env add NOME production                         # pede o valor de forma oculta
vercel redeploy <url-do-deploy>

# GitHub
gh run list -R AlexGalhardo/video.lekzera.xyz --limit 5
```

**Não verificado:** as flags exatas de `koyeb secret create` e de `--env NOME=@NOME` nesta versão da CLI. Confira
com `koyeb secret create --help` e `koyeb service update --help` antes de usar.

## O que foi observado e pede decisão

- **O backend no Koyeb não tem `API_KEY`**: sem ela, a API fica aberta para qualquer pessoa, incluindo as rotas de
  compressão (uso de CPU). O frontend na Vercel também não tem `API_KEY` em produção. Definir a chave exige gravar o
  mesmo valor em três lugares e publicar de novo: secret no Koyeb, `API_KEY` no projeto `video.lekzera.xyz` da Vercel e
  `VIDEO_API_KEY` no projeto `lekzera.xyz`.
- **O push em `main` de 2026-10-13 não gerou deploy no Koyeb**, embora o serviço esteja com deploy automático ligado
  (`no_deploy_on_push: false`). O serviço ainda aponta para o nome antigo do repositório. Publicar exige
  `koyeb service redeploy`.
- **GitHub Actions não inicia os jobs** (todos falham em 2 a 3 segundos, antes de rodar): o `ci.yml` e o `cd.yml` estão
  parados até a cobrança da conta ser resolvida. A verificação é a local (`bun run check`, hooks do husky).
- **Instância `free` com suspensão por inatividade**: a primeira chamada depois de um tempo parada demora, e comprimir
  vídeo com libx264 nela é lento. Vídeos maiores tendem a estourar o limite de 150 s do `lekzera.xyz`, que então guarda
  o original.
- **`YOUTUBE_COOKIES` está como env em texto puro** no serviço (não como secret do Koyeb).

## Regras para agentes

- Pedir confirmação do Alex antes de: publicar em produção (`koyeb service redeploy`, `vercel --prod`), apagar app,
  serviço, secret ou env, e trocar ou criar chaves.
- Nunca imprimir, logar ou commitar valores de env. Listar só os nomes.
- Valores marcados como sensíveis na Vercel não podem ser lidos de volta (`[SENSITIVE]`): guarde a origem do valor.
- Depois de mudar env: publicar de novo e conferir `GET /health` e uma rota sob `/api/`.
