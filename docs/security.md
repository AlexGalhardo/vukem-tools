# Segurança: auditoria OWASP Top 10:2025

Auditoria feita na refatoração 2.0.0 (2026-09-26) nos três workspaces. Controles cobertos por testes em `backend/tests/test_security.py`, `telegram/tests/test_middleware_and_delivery.py` e `frontend/src/lib/api-proxy.test.ts`.

## Achados corrigidos

| # | Categoria | Achado | Correção |
|---|---|---|---|
| 1 | A04/A07 Segredos | Cookies de sessão do YouTube (3 arquivos, inclusive `backend/.env.example`), token do bot (`telegram/.env.prod`) e usuário/senha do proxy Webshare (`docs/todo.md`) versionados; `.gitignore` não ignorava `backend/cookies/*` | Arquivos e fallback hardcoded removidos, `.gitignore` corrigido, histórico reescrito. **Rotacionar as credenciais** (já estiveram no GitHub). |
| 2 | A09 Logging | Entrypoint do Docker imprimia todas as env vars com os 12 primeiros caracteres de segredos | Entrypoint não imprime env; logs de cookies só registram o tipo do erro |
| 3 | A01 Acesso | API pública na VPS sem autenticação (qualquer um consumia banda/CPU) | `API_KEY` + header `X-API-Key` (comparação em tempo constante); o setup gera a chave |
| 4 | A05 Injeção / SSRF | ffmpeg aceitava playlists HLS/concat disfarçadas de `.mp4` (leitura de arquivo local, requisições arbitrárias) | `-protocol_whitelist file` + allowlist de demuxers em `probe` |
| 5 | A05 SSRF | URL validada pela plataforma podia redirecionar para o extractor genérico do yt-dlp | `allowed_extractors` por plataforma; validação pelo *hostname* (ignora userinfo/porta, só http/https) |
| 6 | A06 Design | Jobs ilimitados em memória (DoS) e `segment_seconds=1` gerando milhares de clipes | `MAX_CONCURRENT_JOBS` (503 `SERVER_BUSY`); `segment_seconds` entre 5 e 600 |
| 7 | A01 | `job_id`/índice de clipe sem validação de formato | `^[0-9a-f]{32}$` no backend e no proxy do frontend |
| 8 | A10 Exceções | Nome de arquivo Unicode no `Content-Disposition` derrubava a resposta (500) | `filename` ASCII + `filename*` UTF-8 (RFC 6266) |
| 9 | A02 Configuração | Frontend sem headers de segurança; backend com CORS `allow_credentials=True` e `*` | CSP, `frame-ancestors 'none'`, `nosniff`, `Referrer-Policy`, `Permissions-Policy`, sem `X-Powered-By`; CORS só GET/POST sem credenciais; `nosniff`/`DENY` no backend |
| 10 | A02 | Erros do proxy revelavam a URL interna do backend; erro de validação ecoava a URL enviada | Mensagens genéricas, sem refletir entrada |
| 11 | A01 | Allowlist do bot deixava passar updates sem remetente | Fail-closed |
| 12 | A05 XSS no chat | Título do vídeo em Markdown legado (quebrava ou formatava) | `parse_mode=HTML` + `html.escape` |
| 13 | A03 Supply chain | Dependências com range aberto (`^`), sem auditoria | Versões exatas, `pip-audit` e `bun audit` no CI, Dependabot (pip, bun, docker, actions) |
| 14 | A02 | Script de VPS removia nginx e desativava sites de outros projetos automaticamente | Removido; o Caddy só ganha o arquivo do próprio site |

## Riscos aceitos

- **Frontend público**: qualquer visitante do site pode baixar vídeos (é o produto). A `API_KEY` protege o backend de uso direto; limitação por IP na borda (Vercel Firewall) fica como melhoria.
- **`'unsafe-inline'` no CSP**: Next.js e `next-themes` injetam scripts inline sem nonce; as páginas são estáticas e não renderizam HTML de terceiros.
- **Jobs em memória**: um reinício perde downloads em andamento (sem dado sensível).
- **Túnel residencial**: a chave da VPS só abre túneis para 80/443 da máquina de casa; comprometer a VPS permite usar a sua conexão para tráfego web.

## Checklist para mudanças futuras

Skill [`galhardo-video-secure-endpoint`](../.claude/skills/galhardo-video-secure-endpoint/SKILL.md).
