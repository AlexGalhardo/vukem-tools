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

`graphify` (exigia CLI Python externo; o código aqui é pequeno), `ui-ux-pro-max` e `minimalist-ui` (redundantes com `impeccable`/`frontend-design` e com estética diferente da do app), e, em 2026-10-18, `vercel-react-best-practices` (as regras são do Next.js App Router — server actions, `after`, barrel imports; este projeto usa Inertia v3 + Vite, então a skill dava conselho errado).

As 46 skills `golang-*`, mais `karpathy-guidelines` e `tdd`, foram copiadas do repo do vukem em 2026-10-18. `vukem-release` foi copiada junto e removida no mesmo dia: ela descreve o ciclo de release do **framework** (tags de `drivers/*` e `contrib/*`, bump dos 3 apps), não deste app — o processo daqui está em `vukem-tools-release`.

Atenção: `tdd` (do vukem) e `test-driven-development` (addyosmani) cobrem o mesmo assunto com fluxos diferentes. Em caso de conflito, `tdd` vence, porque é a que o framework usa.

## Skills do próprio projeto

- `vukem-tools-verification`: validar como os hooks e o CI (`go build`/`vet`/`golangci-lint`/`go test -race`, `bun run typecheck`/`build`/`lint:md`) e exigir benchmark em caminho de conversão.
- `vukem-tools-secure-endpoint`: checklist de segurança para rotas, API keys, uploads, jobs e invocação de engines (base: `docs/web-research.md` e `.claude/rules/engines.md`).
- `vukem-tools-release`: processo de release deste app (git-cliff, tag uma por vez, verificar a GitHub release).
