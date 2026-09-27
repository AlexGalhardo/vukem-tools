"""Configuração do bot via variáveis de ambiente (``.env``)."""

import os

from pydantic_settings import BaseSettings, SettingsConfigDict

TELEGRAM_UPLOAD_LIMIT_MB = 50.0


def _csv_ints(value: str) -> list[int]:
	return [int(item) for item in value.split(",") if item.strip()]


class Settings(BaseSettings):
	model_config = SettingsConfigDict(env_file=".env", env_file_encoding="utf-8", extra="ignore")

	TELEGRAM_BOT_TOKEN: str = ""

	# Backend: loopback no PM2 (http://127.0.0.1:8000) ou o nome do serviço no Docker Compose.
	API_BASE_URL: str = "http://127.0.0.1:8000"
	# Mesmo valor de API_KEY do backend (vazio se o backend estiver sem chave).
	API_KEY: str = ""

	# URL pública do frontend, usada no link de arquivos grandes demais para o Telegram.
	PUBLIC_WEB_BASE_URL: str = "http://localhost:3000"

	# Fail-closed: vazio = ninguém usa o bot (a API do backend não tem usuários).
	ALLOWED_TELEGRAM_USER_IDS: str = ""

	# Limite real de upload da Bot API é 50 MB.
	MAX_DIRECT_UPLOAD_MB: float = TELEGRAM_UPLOAD_LIMIT_MB
	# A Bot API tolera ~1 edição/s por chat.
	PROGRESS_EDIT_INTERVAL_SECONDS: float = 3.0
	POLL_INTERVAL_SECONDS: float = 2.0
	REQUEST_TIMEOUT_SECONDS: float = 120.0
	# Abaixo disso um "clipe de 60s" seria o próprio vídeo: pula a pergunta de modo.
	MIN_CLIPS_DURATION_SECONDS: float = 60.0

	@property
	def allowed_user_ids(self) -> list[int]:
		return _csv_ints(self.ALLOWED_TELEGRAM_USER_IDS)

	@property
	def max_direct_upload_bytes(self) -> int:
		return int(self.MAX_DIRECT_UPLOAD_MB * 1024 * 1024)


# ENV_FILE="" desliga a leitura do .env (usado pelos testes).
settings = Settings(_env_file=os.environ.get("ENV_FILE", ".env") or None)
