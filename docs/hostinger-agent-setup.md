# Hostinger: operar a VPS por API e MCP (agentes de IA)

Objetivo: o Alex faz uma vez os passos que só um humano pode fazer (criar o token, autorizar o cliente) e, dali em
diante, agentes de IA (Claude Code e similares) operam a VPS sem ninguém clicar no hPanel.

Pesquisa feita em 2026-10-13 na documentação oficial. Cada item diz de onde veio; o que não foi confirmado está
marcado como **não verificado** e deve ser conferido antes de virar automação.

## O que existe

| Meio | O que é | Fonte |
|---|---|---|
| API pública | REST em `https://developers.hostinger.com`, caminhos `/api/<produto>/v1/...` | <https://docs.hostinger.com/api-reference/overview> |
| MCP remoto | `https://mcp.hostinger.com` (Streamable HTTP, login por OAuth) | <https://github.com/hostinger/api-mcp-server> |
| MCP local | pacote npm do repositório `hostinger/api-mcp-server` (stdio ou `--http` em `127.0.0.1:8100`) | idem |
| CLI | `hostinger` (`hostinger vps virtual-machines list`) | <https://docs.hostinger.com/api-reference/cli.md> |
| SDKs | TypeScript `@hostinger/sdk`, Python `hostinger-api`, PHP `hostinger/api-php-sdk` | <https://docs.hostinger.com/api-reference/tools.md> |
| Terraform | provider `hostinger/hostinger` | idem |
| Ansible | coleção `hostinger.vps` | idem |
| GitHub Action | `hostinger/deploy-action@v1` (deploy de Docker Compose na VPS) | <https://www.hostinger.com/support/?p=5831> |
| n8n | `n8n-nodes-hostinger-api` | tools.md acima |

**Não verificado:** o nome exato do pacote npm do MCP (a documentação cita `@hostinger/mcp`; buscas citam o nome
antigo `hostinger-api-mcp`) e a versão estável atual. Confira em <https://github.com/hostinger/api-mcp-server> antes
de fixar uma versão. Este projeto não usa a GitHub Action acima: o deploy é o `cd.yml` por SSH.

## API

- **Token:** hPanel, perfil, API (<https://hpanel.hostinger.com/profile/api>). Vai no header
  `Authorization: Bearer <token>`. O token tem as mesmas permissões do usuário que o criou; não há escopos. Pode ter
  data de expiração.
- **Limite:** 90 requisições por minuto por usuário; acima disso, `429` com `Retry-After`.
- **Grupos de VPS** (<https://docs.hostinger.com/api-reference/endpoints/vps.md>): máquina virtual (listar, detalhes,
  start, stop, restart, recreate, métricas, senha de root, hostname, nameservers, chaves públicas anexadas), snapshots
  (criar, ver, apagar, restaurar), backups, firewall, chaves SSH públicas, scripts de pós-instalação, Docker Manager,
  malware scanner, registros PTR e modo de recuperação.
- **Caminhos confirmados:** `POST /api/vps/v1/virtual-machines/{id}/start` e `.../restart`.
- **Não verificado:** os caminhos exatos de firewall, backups, Docker, scripts, DNS e domínios. A fonte é a
  especificação <https://developers.hostinger.com/openapi/openapi.json>.

## MCP

- **Remoto (recomendado):** nada para instalar e nenhum token em arquivo.

  ```sh
  claude mcp add --transport http hostinger https://mcp.hostinger.com
  ```

  O primeiro uso abre o login da Hostinger no navegador (OAuth, emissor `https://auth.hostinger.com`).
- **Local (stdio):** precisa de Node 24+ e da variável `HOSTINGER_API_TOKEN` (`API_TOKEN` é um alias obsoleto). Há
  binários separados por área: `hostinger-vps-mcp`, `hostinger-dns-mcp`, `hostinger-domains-mcp`.
- **Ferramentas:** `search`, `execute` e `multi-execute` (até 20 operações por chamada) sobre as operações da API.
- **Plugin do Claude Code:** na máquina do Alex já existe um plugin `hostinger` instalado, ainda sem autenticação (só
  expõe `authenticate` e `complete_authentication`), com skills de deploy de hospedagem (`hostinger:hosting-deploy-*`).
  A origem desse plugin não foi confirmada como oficial. Autenticar esse plugin ou adicionar o MCP remoto acima dá no
  mesmo resultado; basta um dos dois.

## Passos do Alex (uma vez)

1. Escolher o caminho: MCP remoto com OAuth (sem token em disco) ou token de API.
2. Com OAuth: rodar o `claude mcp add` acima e concluir o login no navegador.
3. Com token: criar em <https://hpanel.hostinger.com/profile/api>, com expiração, e guardar só em variável de ambiente
   do usuário (`HOSTINGER_API_TOKEN`). Nunca em `.mcp.json` versionado, `.env` commitado ou chat.
4. Para o CD deste repositório (`.github/workflows/cd.yml`): criar o environment `production` no GitHub com
   `VPS_HOST`, `VPS_USER` e `VPS_SSH_KEY` (ver `PLAN.md`, Marco 4).

## O que um agente passa a fazer sozinho

- Ver estado e métricas da VPS, reiniciar, parar e iniciar.
- Criar um snapshot antes de qualquer mudança arriscada e restaurar se der errado.
- Ajustar regras de firewall e anexar chaves SSH públicas.
- Gerenciar projetos do Docker Manager (se o deploy passar a usar Docker).
- Conferir e alterar registros DNS de `lekzera.xyz` e `video.lekzera.xyz`.

O deploy do código continua sendo `git push` em `main` (CD por SSH) ou, à mão, `sudo bash
setups/setup-unix-vps-pm2.sh` na VPS. A API não substitui a sessão SSH: instalar pacotes e editar `.env` dentro da
máquina segue por SSH ou pelo script de setup.

## Regras para agentes

- **Pedir confirmação humana** antes de: `recreate` da VM (reconstrói a máquina), apagar ou restaurar snapshot, trocar
  senha de root, reset de DNS, mudança de nameservers e qualquer compra.
- **Snapshot antes** de mexer em firewall, hostname ou Docker em produção.
- **Segredos:** nunca imprimir, logar ou commitar o token; nunca colocá-lo em URL.
- **Respeitar o limite** de 90 requisições por minuto: agrupar leituras com `multi-execute`.
- O que a API não cobre (conta, cobrança, criação de conta) continua no hPanel.
