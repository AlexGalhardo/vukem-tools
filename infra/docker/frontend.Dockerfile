# Next.js 16 (saída standalone) rodando em Bun. Contexto de build: raiz do repositório.

FROM --platform=linux/amd64 oven/bun:1.4.2-alpine AS deps
WORKDIR /app
COPY frontend/package.json frontend/bun.lock ./
RUN bun install --frozen-lockfile

# Usado só pelo infra/docker-compose.yml (dev, código montado do host).
FROM deps AS dev
ENV NEXT_TELEMETRY_DISABLED=1 HOSTNAME=0.0.0.0
COPY frontend/ .
EXPOSE 3000
CMD ["bun", "run", "dev"]

FROM deps AS builder
ENV NEXT_TELEMETRY_DISABLED=1
COPY frontend/ .
RUN bun run build

FROM --platform=linux/amd64 oven/bun:1.4.2-alpine AS runner
WORKDIR /app
ENV NODE_ENV=production NEXT_TELEMETRY_DISABLED=1 PORT=3000 HOSTNAME=0.0.0.0
RUN addgroup --system --gid 1001 nodejs && adduser --system --uid 1001 nextjs
COPY --from=builder --chown=nextjs:nodejs /app/.next/standalone ./
COPY --from=builder --chown=nextjs:nodejs /app/.next/static ./.next/static
COPY --from=builder --chown=nextjs:nodejs /app/public ./public
USER nextjs
EXPOSE 3000
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
	CMD bun -e "fetch('http://127.0.0.1:'+(process.env.PORT||3000)+'/').then(r=>process.exit(r.ok?0:1)).catch(()=>process.exit(1))"
CMD ["bun", "server.js"]
