"""Única camada que fala com o yt-dlp (usado como biblioteca, não via subprocess).

O yt-dlp é síncrono: toda chamada roda em ``asyncio.to_thread`` para não bloquear o loop.
"""

import asyncio
import logging
import uuid
from collections.abc import Callable
from datetime import UTC, datetime
from pathlib import Path
from typing import Any, TypeVar

import yt_dlp

from app.config import settings
from app.errors import (
	DownloadFailedError,
	MediaTooLargeError,
	OutOfMemoryError,
	YoutubeBotCheckError,
	classify_oserror,
	classify_ytdlp_error,
)
from app.models.schemas import FormatInfo, OutputFormat, VideoInfoResponse, VideoQuality
from app.platforms import PLATFORMS
from app.utils.cookies import cookies_min_expiry, resolve_cookies_file
from app.utils.filenames import uppercase_ascii_slug

ProgressHook = Callable[[dict[str, Any]], None]
T = TypeVar("T")

logger = logging.getLogger("video-api")

_AUTO = "__auto__"


def bytes_to_mb(value: float | None) -> float | None:
	return round(value / (1024 * 1024), 2) if value else None


def media_type_for(fmt: OutputFormat) -> str:
	return "audio/mpeg" if fmt == OutputFormat.MP3 else "video/mp4"


class VideoDownloadService:
	def __init__(self, platform: str, cookies_file: str | None = _AUTO):
		self.platform = platform
		self.cookies_file = resolve_cookies_file(platform) if cookies_file == _AUTO else cookies_file

	def _base_opts(self, cookies_file: str | None) -> dict[str, Any]:
		youtube_args: dict[str, Any] = {}
		if settings.ytdlp_player_clients_list:
			youtube_args["player_client"] = settings.ytdlp_player_clients_list

		opts: dict[str, Any] = {
			"quiet": True,
			"no_warnings": True,
			"noplaylist": True,
			"socket_timeout": settings.YTDLP_TIMEOUT,
			"restrictfilenames": True,
			"allowed_extractors": list(PLATFORMS[self.platform].extractors),
			"extractor_args": {"youtube": youtube_args},
		}
		if cookies_file:
			opts["cookiefile"] = cookies_file
		if settings.YTDLP_FORCE_IPV4:
			opts["force_ipv4"] = True
		if settings.YTDLP_JS_RUNTIME:
			opts["js_runtimes"] = {settings.YTDLP_JS_RUNTIME: {}}

		if self.platform == "youtube":
			if settings.YOUTUBE_PROXY:
				opts["proxy"] = settings.YOUTUBE_PROXY
			# Best-effort: sem o servidor local no ar, o yt-dlp só segue sem PO Token.
			if settings.YTDLP_POT_PROVIDER_ENABLED:
				opts["extractor_args"]["youtubepot-bgutilhttp"] = {"base_url": [settings.YTDLP_POT_PROVIDER_URL]}

		if settings.YTDLP_SLEEP_REQUESTS > 0:
			opts["sleep_interval_requests"] = settings.YTDLP_SLEEP_REQUESTS
		if settings.YTDLP_SLEEP_INTERVAL > 0:
			opts["sleep_interval"] = settings.YTDLP_SLEEP_INTERVAL
			if settings.YTDLP_MAX_SLEEP_INTERVAL > settings.YTDLP_SLEEP_INTERVAL:
				opts["max_sleep_interval"] = settings.YTDLP_MAX_SLEEP_INTERVAL
		return opts

	def _cookie_attempts(self) -> list[str | None]:
		# YouTube tenta primeiro sem cookies (não expõe a sessão à toa) e só usa os cookies
		# configurados se cair no bot-check. As demais plataformas usam os cookies direto.
		if self.platform != "youtube":
			return [self.cookies_file]
		return [None, self.cookies_file] if self.cookies_file else [None]

	def _run(self, action: Callable[[dict[str, Any]], T]) -> T:
		attempts = self._cookie_attempts()
		for index, cookies_file in enumerate(attempts):
			try:
				return action(self._base_opts(cookies_file))
			except YoutubeBotCheckError as error:
				if index + 1 < len(attempts):
					logger.warning("YouTube bot-check sem cookies; tentando com os cookies configurados.")
					continue
				raise self._with_cookie_expiry_hint(error, cookies_file) from error
		raise AssertionError("unreachable: _cookie_attempts() nunca é vazio")

	@staticmethod
	def _with_cookie_expiry_hint(error: YoutubeBotCheckError, cookies_file: str | None) -> YoutubeBotCheckError:
		expiry = cookies_min_expiry(cookies_file) if cookies_file else None
		if expiry and datetime.now(UTC) > expiry:
			return YoutubeBotCheckError(
				f"{error.message} Os cookies do YouTube configurados expiraram em "
				f"{expiry:%d/%m/%Y} — gere novos e atualize YOUTUBE_COOKIES."
			)
		return error

	@staticmethod
	def _translate(exc: BaseException) -> BaseException:
		if isinstance(exc, yt_dlp.utils.DownloadError):
			return classify_ytdlp_error(str(exc))
		if isinstance(exc, MemoryError):
			return OutOfMemoryError()
		if isinstance(exc, OSError):
			return classify_oserror(exc) or DownloadFailedError()
		return exc

	def _extract_info_sync(self, url: str) -> dict[str, Any]:
		def action(opts: dict[str, Any]) -> dict[str, Any]:
			try:
				with yt_dlp.YoutubeDL({**opts, "skip_download": True}) as ydl:
					return ydl.extract_info(url, download=False)
			except (yt_dlp.utils.DownloadError, OSError, MemoryError) as e:
				raise self._translate(e) from e

		return self._run(action)

	async def extract_info(self, url: str) -> VideoInfoResponse:
		info = await asyncio.to_thread(self._extract_info_sync, url)
		return self._to_response(info)

	def _to_response(self, info: dict[str, Any]) -> VideoInfoResponse:
		requested = info.get("requested_formats") or []
		if requested:
			estimated = sum(f.get("filesize") or f.get("filesize_approx") or 0 for f in requested) or None
		else:
			estimated = info.get("filesize") or info.get("filesize_approx")

		formats = [
			FormatInfo(
				format_id=f.get("format_id", "?"),
				ext=f.get("ext", "?"),
				resolution=f.get("resolution") or f.get("format_note"),
				fps=f.get("fps"),
				filesize_mb=bytes_to_mb(f.get("filesize") or f.get("filesize_approx")),
				acodec=f.get("acodec"),
				vcodec=f.get("vcodec"),
				note=f.get("format_note"),
			)
			for f in info.get("formats") or []
		]
		return VideoInfoResponse(
			platform=self.platform,
			id=str(info.get("id")),
			title=info.get("title", "Sem título"),
			uploader=info.get("uploader") or info.get("channel"),
			duration_seconds=info.get("duration"),
			thumbnail=info.get("thumbnail"),
			view_count=info.get("view_count"),
			upload_date=info.get("upload_date"),
			webpage_url=info.get("webpage_url") or "",
			estimated_filesize_mb=bytes_to_mb(estimated),
			exceeds_size_limit=bool(estimated and estimated > settings.max_file_size_bytes),
			max_file_size_mb=settings.MAX_FILE_SIZE_MB,
			formats=formats,
		)

	@staticmethod
	def _format_selector(fmt: OutputFormat, quality: VideoQuality) -> str:
		if fmt == OutputFormat.MP3:
			return "bestaudio/best"
		if quality == VideoQuality.WORST:
			return "worstvideo*+worstaudio/worst"
		return "bestvideo[ext=mp4]+bestaudio[ext=m4a]/bestvideo+bestaudio/best[ext=mp4]/best"

	def _download_sync(
		self,
		url: str,
		fmt: OutputFormat,
		quality: VideoQuality,
		task_id: str,
		progress_hook: ProgressHook | None = None,
		postprocessor_hook: ProgressHook | None = None,
	) -> tuple[Path, str]:
		def action(base_opts: dict[str, Any]) -> tuple[Path, str]:
			opts = {
				**base_opts,
				"format": self._format_selector(fmt, quality),
				"outtmpl": str(settings.DOWNLOAD_DIR / f"{task_id}.%(ext)s"),
				"max_filesize": settings.max_file_size_bytes,
				"progress_hooks": [progress_hook] if progress_hook else [],
				"postprocessor_hooks": [postprocessor_hook] if postprocessor_hook else [],
			}
			if fmt == OutputFormat.MP3:
				opts["postprocessors"] = [
					{"key": "FFmpegExtractAudio", "preferredcodec": "mp3", "preferredquality": "192"}
				]
			else:
				opts["merge_output_format"] = "mp4"

			try:
				with yt_dlp.YoutubeDL(opts) as ydl:
					info = ydl.extract_info(url, download=True)
					final_path = self._resolve_output_path(Path(ydl.prepare_filename(info)), fmt, task_id)
			except (yt_dlp.utils.DownloadError, OSError, MemoryError) as e:
				self._remove_partials(task_id)
				raise self._translate(e) from e
			return self._finalize(final_path, info, fmt)

		return self._run(action)

	@staticmethod
	def _remove_partials(task_id: str) -> None:
		for leftover in settings.DOWNLOAD_DIR.glob(f"{task_id}.*"):
			leftover.unlink(missing_ok=True)

	@staticmethod
	def _resolve_output_path(base: Path, fmt: OutputFormat, task_id: str) -> Path:
		# Merge/pós-processamento pode trocar a extensão prevista pelo yt-dlp.
		expected = base.with_suffix(".mp3" if fmt == OutputFormat.MP3 else ".mp4")
		for candidate in (expected, base):
			if candidate.exists():
				return candidate
		matches = sorted(settings.DOWNLOAD_DIR.glob(f"{task_id}.*"))
		if not matches:
			raise DownloadFailedError("O arquivo baixado não foi encontrado após o processamento.")
		return matches[0]

	@staticmethod
	def _finalize(path: Path, info: dict[str, Any], fmt: OutputFormat) -> tuple[Path, str]:
		size = path.stat().st_size
		if size > settings.max_file_size_bytes:
			path.unlink(missing_ok=True)
			raise MediaTooLargeError(
				f"O arquivo baixado ({bytes_to_mb(size)} MB) excede o limite de {settings.MAX_FILE_SIZE_MB} MB."
			)
		prefix = "AUDIO" if fmt == OutputFormat.MP3 else "VIDEO"
		return path, f"{prefix}_{uppercase_ascii_slug(info.get('title') or path.stem)}{path.suffix}"

	async def download_file(
		self,
		url: str,
		fmt: OutputFormat,
		quality: VideoQuality,
		progress_hook: ProgressHook | None = None,
		postprocessor_hook: ProgressHook | None = None,
	) -> tuple[Path, str, str]:
		"""Retorna ``(caminho, nome_amigavel, media_type)``. Hooks rodam na thread do download."""
		path, pretty = await asyncio.to_thread(
			self._download_sync, url, fmt, quality, uuid.uuid4().hex, progress_hook, postprocessor_hook
		)
		return path, pretty, media_type_for(fmt)


def get_service(platform: str, cookies_file: str | None = _AUTO) -> VideoDownloadService:
	"""Factory — existe para os testes poderem fazer monkeypatch."""
	return VideoDownloadService(platform=platform, cookies_file=cookies_file)
