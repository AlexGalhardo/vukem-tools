"""
errors.py
---------
Traduz o ``error_type`` estruturado do backend (ver ``backend/app/errors.py``)
numa mensagem específica e acionável em português — mesma curadoria usada no
frontend (``frontend/src/lib/errors.ts``), para o usuário ver a mesma
explicação seja pelo site ou pelo bot.
"""

from __future__ import annotations

ERROR_COPY: dict[str, str] = {
	"TIMEOUT": ("⏱️ O processamento demorou demais e foi interrompido. Tente um vídeo mais curto."),
	"VIDEO_PROCESSING_FAILED": (
		"⚠️ Falha ao processar o vídeo (ffmpeg). O arquivo pode estar corrompido ou num formato não suportado."
	),
	"FILE_TOO_LARGE": "📦 O vídeo passa do limite de tamanho. Tente MP3 ou um trecho menor.",
	"OUT_OF_MEMORY": (
		"🧠 O servidor ficou sem memória durante o processamento. Tente um vídeo mais curto ou em resolução menor."
	),
	"NO_DISK_SPACE": "💾 Sem espaço em disco no servidor. Tente de novo em alguns minutos.",
	"NETWORK_ERROR": (
		"🌐 Falha de rede ao baixar da origem. Confira a URL e tente de novo — "
		"a plataforma de origem pode estar instável."
	),
	"DOWNLOAD_FAILED": (
		"❌ Não foi possível baixar esse vídeo. Ele pode ser privado, ter sido removido, ou exigir login."
	),
	"YOUTUBE_BOT_CHECK": (
		"🤖 O YouTube bloqueou o IP do servidor. Avise o administrador para configurar o YOUTUBE_PROXY."
	),
	"INVALID_MEDIA": "🎞️ Não consegui ler esse arquivo de vídeo.",
	"INVALID_REQUEST": "⚠️ Requisição inválida.",
	"UNAUTHORIZED": "🔐 O bot não está autorizado no backend (API_KEY divergente). Avise o administrador.",
	"NOT_FOUND": "🔎 Esse download expirou. Mande a URL de novo.",
	"CONFLICT": "⏳ O download ainda não terminou.",
	"SERVER_BUSY": "🚦 O servidor está processando muitos vídeos agora. Tente de novo em alguns minutos.",
	"INTERNAL_ERROR": "🛠️ Erro interno no servidor. Tente novamente em instantes.",
}

_GENERIC = "❌ Algo deu errado. Tente novamente."
# Falha sem tratamento específico (timeout de rede, exceção inesperada).
FALLBACK_MESSAGE = "⏱️ Aconteceu algum problema. Tente novamente mais tarde ou tente com outra URL."


_YOUTUBE_COOKIES_EXPIRED_MARKER = "expiraram"


def resolve_error_message(error_type: str | None, detail: str | None = None) -> str:
	"""Mensagem final a mandar no chat: copy curada por ``error_type`` > ``detail`` > fallback.

	Exceção: ``YOUTUBE_BOT_CHECK`` normalmente usa só a copy fixa (ignora
	``detail``) — mas quando o backend detecta que os cookies do YouTube
	configurados expiraram (``YoutubeBotCheckError`` enriquecido em
	``backend/app/services/downloader.py::_enrich_bot_check_error``), esse
	aviso é específico o bastante pra valer a pena aparecer no chat, não só
	nos logs do backend — o operador não vê os logs do backend no dia a dia.
	"""
	if error_type and error_type in ERROR_COPY:
		message = ERROR_COPY[error_type]
		if error_type == "YOUTUBE_BOT_CHECK" and detail and _YOUTUBE_COOKIES_EXPIRED_MARKER in detail.lower():
			message = f"{message}\n\n⚠️ {detail.strip()}"
		return message
	if detail and detail.strip():
		return f"❌ {detail.strip()}"
	return _GENERIC
