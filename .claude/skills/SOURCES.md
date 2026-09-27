# Origem das skills de terceiros

Skills copiadas (vendored) dos repositórios upstream. Para atualizar, clone o repo no commit desejado e substitua a pasta correspondente — não edite os arquivos à mão, para não divergir do upstream.

| Skill(s) | Repositório | Commit | Licença |
| --- | --- | --- | --- |
| `impeccable` (+ `.claude/agents/impeccable-*.md`) | [pbakaus/impeccable](https://github.com/pbakaus/impeccable) | `9d715cc4f5564a990ca8345abfdd5df6dc9b41c8` | Apache-2.0 |
| `ponytail`, `ponytail-audit`, `ponytail-debt`, `ponytail-gain`, `ponytail-help`, `ponytail-review` | [DietrichGebert/ponytail](https://github.com/DietrichGebert/ponytail) | `e3ba2aa6f1e6f0bc4d69eb09c9f0d0a93af56156` | MIT |
| `api-and-interface-design` … `using-agent-skills` (25 skills) + `.claude/references/*.md` + `.claude/agents/{code-reviewer,security-auditor,test-engineer,web-performance-auditor}.md` | [addyosmani/agent-skills](https://github.com/addyosmani/agent-skills) | `bcab6a1b8503100e8618c3b4e32cc78de43de769` | MIT |
| `frontend-design` ([agenticskills.io](https://agenticskills.io/skills/frontend-design) aponta para o repo da Anthropic) | [anthropics/skills](https://github.com/anthropics/skills) | `33375500bcea98d610eb30ce10ac4e59b89c390d` | ver `frontend-design/LICENSE.txt` |

## Dependências externas

- **impeccable** traz scripts próprios em `impeccable/scripts/`. Os hooks de `settings.json` do upstream **não** foram instalados.

## Removidas deste projeto

`graphify` (exigia CLI Python externo; o código aqui é pequeno), `ui-ux-pro-max` e `minimalist-ui` (redundantes com `impeccable`/`frontend-design` e com estética diferente da do app) e as skills `respondeae-*` do projeto de origem.

## Skills do próprio projeto

- `galhardo-video-local-verification`: validar como os hooks e o CI (`bun run check`, `bun run build`, smoke em Docker).
- `galhardo-video-secure-endpoint`: checklist de segurança para rotas, proxies, uploads e bot (base: `docs/security.md`).
