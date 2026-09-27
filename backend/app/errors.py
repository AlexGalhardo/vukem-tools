"""Taxonomia de erros estruturados da API.

Todo erro tratado vira ``{"detail": <mensagem pt-BR>, "error_type": <código estável>}`` com o
status HTTP da classe. Os códigos são espelhados em ``frontend/src/lib/errors.ts`` e
``telegram/app/errors.py`` — adicione nos três lugares ao criar um novo.

As funções ``classify_*`` traduzem falhas de baixo nível (ffmpeg, yt-dlp, ``OSError``) no
``AppError`` mais específico possível.
"""

from __future__ import annotations

import errno
import re

ERR_TIMEOUT = "TIMEOUT"
ERR_VIDEO_PROCESSING = "VIDEO_PROCESSING_FAILED"
ERR_FILE_TOO_LARGE = "FILE_TOO_LARGE"
ERR_OUT_OF_MEMORY = "OUT_OF_MEMORY"
ERR_NO_DISK_SPACE = "NO_DISK_SPACE"
ERR_NETWORK = "NETWORK_ERROR"
ERR_DOWNLOAD_FAILED = "DOWNLOAD_FAILED"
ERR_YOUTUBE_BOT_CHECK = "YOUTUBE_BOT_CHECK"
ERR_INVALID_MEDIA = "INVALID_MEDIA"
ERR_INVALID_REQUEST = "INVALID_REQUEST"
ERR_UNAUTHORIZED = "UNAUTHORIZED"
ERR_NOT_FOUND = "NOT_FOUND"
ERR_CONFLICT = "CONFLICT"
ERR_SERVER_BUSY = "SERVER_BUSY"
ERR_INTERNAL = "INTERNAL_ERROR"


class AppError(Exception):
	error_type: str = ERR_INTERNAL
	status_code: int = 500
	default_message: str = "Erro interno no servidor."

	def __init__(self, message: str | None = None):
		self.message = (message or self.default_message).strip()
		super().__init__(self.message)

	def as_body(self) -> dict[str, str]:
		return {"detail": self.message, "error_type": self.error_type}

	@classmethod
	def from_parts(cls, *, message: str, status_code: int, error_type: str) -> AppError:
		err = cls(message)
		err.status_code = status_code
		err.error_type = error_type
		return err


class ProcessingTimeoutError(AppError):
	error_type = ERR_TIMEOUT
	status_code = 504
	default_message = "O processamento passou do tempo limite. Tente um vídeo mais curto ou um trecho menor."


class VideoProcessingError(AppError):
	error_type = ERR_VIDEO_PROCESSING
	status_code = 502
	default_message = (
		"Falha ao processar o vídeo (ffmpeg). O arquivo pode estar corrompido ou num formato não suportado."
	)


class MediaTooLargeError(AppError):
	error_type = ERR_FILE_TOO_LARGE
	status_code = 413
	default_message = "O vídeo excede o limite de tamanho permitido."


class OutOfMemoryError(AppError):
	error_type = ERR_OUT_OF_MEMORY
	status_code = 503
	default_message = (
		"O servidor ficou sem memória durante o processamento. Tente um vídeo mais curto ou em resolução menor."
	)


class NoDiskSpaceError(AppError):
	error_type = ERR_NO_DISK_SPACE
	status_code = 507
	default_message = "Sem espaço em disco para o arquivo temporário. Tente mais tarde ou use um vídeo menor."


class NetworkError(AppError):
	error_type = ERR_NETWORK
	status_code = 502
	default_message = "Falha de rede ao baixar o vídeo da origem. Verifique a URL e tente novamente."


class DownloadFailedError(AppError):
	error_type = ERR_DOWNLOAD_FAILED
	status_code = 502
	default_message = (
		"Não foi possível baixar esse vídeo da origem. Ele pode ser privado, ter sido removido ou exigir login."
	)


class YoutubeBotCheckError(AppError):
	error_type = ERR_YOUTUBE_BOT_CHECK
	status_code = 502
	default_message = (
		"O YouTube bloqueou o IP do servidor ('Sign in to confirm you're not a bot'). "
		"Configure YOUTUBE_PROXY (ver docs/youtube.md)."
	)


class InvalidMediaError(AppError):
	error_type = ERR_INVALID_MEDIA
	status_code = 400
	default_message = "Não foi possível ler esse arquivo de vídeo. Ele pode estar corrompido."


class InvalidRequestError(AppError):
	error_type = ERR_INVALID_REQUEST
	status_code = 400
	default_message = "Requisição inválida."


class UnauthorizedError(AppError):
	error_type = ERR_UNAUTHORIZED
	status_code = 401
	default_message = "Chave de API ausente ou inválida."


class NotFoundError(AppError):
	error_type = ERR_NOT_FOUND
	status_code = 404
	default_message = "Recurso não encontrado ou expirado."


class ConflictError(AppError):
	error_type = ERR_CONFLICT
	status_code = 409
	default_message = "O download ainda não terminou."


class ServerBusyError(AppError):
	error_type = ERR_SERVER_BUSY
	status_code = 503
	default_message = "O servidor está processando muitos vídeos agora. Tente de novo em alguns minutos."


_OOM_PAT = re.compile(
	r"cannot allocate memory|out of memory|oom|memoryerror|av_buffer|error while opening encoder|signal 9|killed",
	re.IGNORECASE,
)
_DISK_PAT = re.compile(r"no space left on device|enospc|disk (?:is )?full|write error", re.IGNORECASE)
_NETWORK_PAT = re.compile(
	r"getaddrinfo|name or service not known|temporary failure in name resolution|"
	r"nodename nor servname|connection (?:reset|refused|timed out|aborted)|"
	r"network is unreachable|timed out|read timeout|"
	r"unable to (?:download|connect)|urlopen error|ssl|handshake|"
	r"remote end closed|max retries|failed to resolve|http error 5",
	re.IGNORECASE,
)
_TOO_LARGE_PAT = re.compile(r"max[-_ ]?filesize|larger than|file is larger", re.IGNORECASE)
_YOUTUBE_BOT_CHECK_PAT = re.compile(r"sign in to confirm (?:you'?re|you are) not a bot", re.IGNORECASE)


def _last_line(text: str, limit: int = 300) -> str:
	stripped = (text or "").strip()
	return stripped.splitlines()[-1][:limit] if stripped else ""


def classify_oserror(exc: OSError) -> AppError | None:
	if exc.errno == errno.ENOSPC or _DISK_PAT.search(str(exc)):
		return NoDiskSpaceError()
	if exc.errno == errno.ENOMEM or _OOM_PAT.search(str(exc)):
		return OutOfMemoryError()
	return None


def classify_ffmpeg_failure(returncode: int, stderr_text: str) -> AppError:
	text = stderr_text or ""
	# 137/-9 = SIGKILL, quase sempre o OOM killer do kernel.
	if returncode in (137, -9) or _OOM_PAT.search(text):
		return OutOfMemoryError()
	if _DISK_PAT.search(text):
		return NoDiskSpaceError()
	snippet = _last_line(text)
	return VideoProcessingError(f"Falha ao processar o vídeo (ffmpeg): {snippet}" if snippet else None)


def classify_ytdlp_error(message: str) -> AppError:
	text = message or ""
	if _YOUTUBE_BOT_CHECK_PAT.search(text):
		return YoutubeBotCheckError()
	if _TOO_LARGE_PAT.search(text):
		return MediaTooLargeError()
	if _NETWORK_PAT.search(text):
		return NetworkError()
	return DownloadFailedError(_last_line(text) or None)
