# Backend FastAPI + ffmpeg + PO Token provider. Contexto de build: raiz do repositório.
# linux/amd64 fixo: produção é x86_64 mesmo quando o build roda em Apple Silicon.

FROM --platform=linux/amd64 python:3.12-slim-bookworm AS deps
RUN python -m venv /opt/venv
ENV PATH="/opt/venv/bin:$PATH"
COPY backend/requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Servidor do PO Token (bgutil-ytdlp-pot-provider), mesma tag do plugin Python.
# Mesmo codename Debian do runtime: o binário `node` copiado abaixo depende da mesma glibc,
# e também serve de runtime JS para o yt-dlp-ejs (YTDLP_JS_RUNTIME=node).
FROM --platform=linux/amd64 node:24-bookworm-slim AS pot
RUN apt-get update && apt-get install -y --no-install-recommends git ca-certificates \
	&& rm -rf /var/lib/apt/lists/*
RUN git clone --branch 2.0.0 --depth 1 https://github.com/Brainicism/bgutil-ytdlp-pot-provider.git /pot
WORKDIR /pot/server
RUN npm ci && npx tsc && npm prune --omit=dev

FROM --platform=linux/amd64 python:3.12-slim-bookworm
WORKDIR /app
RUN apt-get update && apt-get install -y --no-install-recommends ffmpeg ghostscript curl \
	&& rm -rf /var/lib/apt/lists/* \
	&& useradd --create-home appuser
ENV PATH="/opt/venv/bin:$PATH" \
	PYTHONDONTWRITEBYTECODE=1 \
	PYTHONUNBUFFERED=1 \
	PORT=8000
COPY --from=deps /opt/venv /opt/venv
COPY --from=pot /usr/local/bin/node /usr/local/bin/node
COPY --from=pot /pot/server/build ./pot-provider/build
COPY --from=pot /pot/server/node_modules ./pot-provider/node_modules
COPY --from=pot /pot/server/package.json ./pot-provider/package.json
COPY backend/app ./app
COPY --chmod=755 infra/docker/backend-entrypoint.sh /usr/local/bin/backend-entrypoint
RUN mkdir -p downloads cookies && chown -R appuser:appuser /app
USER appuser
EXPOSE 8000
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
	CMD curl -fsS "http://localhost:${PORT}/health" || exit 1
# 1 worker: o registro de jobs é em memória e o polling precisa cair no mesmo processo.
CMD ["backend-entrypoint"]
