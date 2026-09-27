"""Configuração central, lida de variáveis de ambiente (``.env``) via pydantic-settings.

Importar este módulo cria ``DOWNLOAD_DIR`` e ``COOKIES_DIR`` (efeito colateral proposital).
"""

import os
from pathlib import Path

from pydantic_settings import BaseSettings, SettingsConfigDict

APP_VERSION = "2.0.0"


def _csv(value: str) -> list[str]:
	return [item.strip() for item in value.split(",") if item.strip()]


class Settings(BaseSettings):
	model_config = SettingsConfigDict(env_file=".env", env_file_encoding="utf-8", extra="ignore")

	APP_NAME: str = "Galhardo Video API"
	DEBUG: bool = False
	DOCS_ENABLED: bool = True

	# Segredo compartilhado com o frontend (route handlers) e o bot. Vazio = API aberta,
	# aceitável só quando o backend não é alcançável publicamente (rede interna/loopback).
	API_KEY: str | None = None

	ALLOWED_ORIGINS: str = "http://localhost:3000,https://galhardo-studio.vercel.app,https://video.galhardo.tech"

	DOWNLOAD_DIR: Path = Path("downloads")
	COOKIES_DIR: Path = Path("cookies")

	MAX_FILE_SIZE_MB: int = 1024
	MAX_UPLOAD_SIZE_MB: int = 1024
	# Jobs de download/clipes rodando ao mesmo tempo; o excedente recebe 503 SERVER_BUSY.
	MAX_CONCURRENT_JOBS: int = 3
	STREAM_CHUNK_SIZE: int = 1024 * 1024

	YTDLP_TIMEOUT: int = 120
	# Vazio = clients padrão do yt-dlp, que os mantenedores ajustam a cada mudança do YouTube.
	YTDLP_PLAYER_CLIENTS: str = ""
	YTDLP_SLEEP_REQUESTS: float = 1.0
	YTDLP_SLEEP_INTERVAL: float = 1.0
	YTDLP_MAX_SLEEP_INTERVAL: float = 5.0
	# O YouTube pontua um /64 IPv6 inteiro como uma unidade de reputação.
	YTDLP_FORCE_IPV4: bool = True
	# Runtime JS usado pelo yt-dlp-ejs para resolver os desafios de assinatura do YouTube.
	YTDLP_JS_RUNTIME: str = "node"
	YTDLP_POT_PROVIDER_ENABLED: bool = True
	YTDLP_POT_PROVIDER_URL: str = "http://127.0.0.1:4416"
	# Aplicado SÓ ao YouTube: tira o tráfego do IP de datacenter bloqueado (ver docs/youtube.md).
	YOUTUBE_PROXY: str | None = None

	FFMPEG_BIN: str = "ffmpeg"
	FFPROBE_BIN: str = "ffprobe"
	# 0 = ffmpeg decide; 1 reduz o pico de RAM em servidores pequenos.
	FFMPEG_THREADS: int = 0

	YOUTUBE_COOKIES_FILE: str | None = None
	YOUTUBE_COOKIES: str | None = None
	INSTAGRAM_COOKIES_FILE: str | None = None
	INSTAGRAM_COOKIES: str | None = None
	TWITTER_COOKIES_FILE: str | None = None
	TWITTER_COOKIES: str | None = None
	TIKTOK_COOKIES_FILE: str | None = None
	TIKTOK_COOKIES: str | None = None
	COOKIES_FROM_BASE64: bool = False

	@property
	def allowed_origins_list(self) -> list[str]:
		return _csv(self.ALLOWED_ORIGINS)

	@property
	def ytdlp_player_clients_list(self) -> list[str]:
		return _csv(self.YTDLP_PLAYER_CLIENTS)

	@property
	def max_file_size_bytes(self) -> int:
		return self.MAX_FILE_SIZE_MB * 1024 * 1024

	@property
	def max_upload_size_bytes(self) -> int:
		return self.MAX_UPLOAD_SIZE_MB * 1024 * 1024


# ENV_FILE="" desliga a leitura do .env (usado pelos testes para não herdar o .env local).
settings = Settings(_env_file=os.environ.get("ENV_FILE", ".env") or None)

settings.DOWNLOAD_DIR.mkdir(parents=True, exist_ok=True)
settings.COOKIES_DIR.mkdir(parents=True, exist_ok=True)
