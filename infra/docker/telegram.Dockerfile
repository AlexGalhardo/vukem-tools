# Bot do Telegram: só fala HTTP com o backend (sem ffmpeg/yt-dlp). Contexto: raiz do repositório.

FROM --platform=linux/amd64 python:3.12-slim-bookworm AS deps
RUN python -m venv /opt/venv
ENV PATH="/opt/venv/bin:$PATH"
COPY telegram/requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

FROM --platform=linux/amd64 python:3.12-slim-bookworm
WORKDIR /app
ENV PATH="/opt/venv/bin:$PATH" \
	PYTHONDONTWRITEBYTECODE=1 \
	PYTHONUNBUFFERED=1
COPY --from=deps /opt/venv /opt/venv
COPY telegram/app ./app
RUN useradd --create-home appuser && chown -R appuser:appuser /app
USER appuser
# Worker de long polling: nenhuma porta exposta.
CMD ["python", "-m", "app.main"]
