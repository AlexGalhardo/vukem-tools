"""Registro em memória dos jobs de download em background.

Fluxo: ``POST {prefix}/download/start`` cria o job -> o cliente faz polling em
``GET /api/v1/downloads/{job_id}`` -> quando ``finished``, coleta o arquivo. Por ser em
memória, o backend roda com 1 worker (polling precisa cair no mesmo processo).

Os hooks do yt-dlp rodam na thread do download e só fazem atribuições simples no
dataclass, o que dispensa lock.
"""

import asyncio
import logging
import shutil
import time
import uuid
from collections.abc import Coroutine
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, Literal

from app.config import settings
from app.errors import ERR_INTERNAL, AppError, OutOfMemoryError, ServerBusyError, classify_oserror
from app.models.schemas import OutputFormat, VideoQuality
from app.services.compressor import compress_media
from app.services.downloader import ProgressHook, bytes_to_mb, get_service
from app.services.editor import SEGMENT_SECONDS, build_clips_zip, split_into_segments
from app.utils.filenames import sanitize_filename
from app.utils.streaming import cleanup_files

logger = logging.getLogger("video-api")

JOB_TTL_SECONDS = 3600
_CLIPS_DOWNLOAD_CEILING = 85.0

JobStatusName = Literal["pending", "downloading", "processing", "finished", "error"]


@dataclass
class DownloadJob:
	id: str
	status: JobStatusName = "pending"
	percent: float = 0.0
	downloaded_bytes: int = 0
	total_bytes: int | None = None
	speed: float | None = None
	eta: int | None = None
	phase: str = "Na fila"
	filename: str | None = None
	media_type: str | None = None
	path: Path | None = None
	error: str | None = None
	error_status: int = 502
	error_type: str = ERR_INTERNAL
	# Compressão depois do download: tamanhos antes e depois, quando houve ganho.
	original_bytes: int | None = None
	compressed_bytes: int | None = None
	# Modo clipes: ``clips`` é a lista pública (sem caminhos de disco).
	clips: list[dict[str, Any]] | None = None
	clip_paths: list[Path] = field(default_factory=list)
	zip_path: Path | None = None
	updated_at: float = field(default_factory=time.monotonic)

	@property
	def is_active(self) -> bool:
		return self.status in ("pending", "downloading", "processing")

	@property
	def clips_dir(self) -> Path:
		return settings.DOWNLOAD_DIR / f"clips_{self.id}"

	def touch(self) -> None:
		self.updated_at = time.monotonic()

	def finish(self, path: Path, filename: str, media_type: str) -> None:
		size = path.stat().st_size
		self.path, self.filename, self.media_type = path, filename, media_type
		self.downloaded_bytes = self.total_bytes = size
		self.percent, self.status, self.phase = 100.0, "finished", "Concluído"

	def fail(self, exc: BaseException, *, generic: str) -> None:
		err: AppError | None
		if isinstance(exc, AppError):
			err = exc
		elif isinstance(exc, MemoryError):
			err = OutOfMemoryError()
		elif isinstance(exc, OSError):
			err = classify_oserror(exc)
		else:
			err = None
		self.status = "error"
		if err is None:
			logger.exception("Job %s falhou com erro inesperado", self.id, exc_info=exc)
			self.error, self.error_status, self.error_type = generic, 500, ERR_INTERNAL
		else:
			self.error, self.error_status, self.error_type = err.message, err.status_code, err.error_type

	def as_dict(self) -> dict[str, Any]:
		return {
			"job_id": self.id,
			"status": self.status,
			"percent": round(self.percent, 1),
			"downloaded_bytes": self.downloaded_bytes,
			"total_bytes": self.total_bytes,
			"downloaded_mb": bytes_to_mb(self.downloaded_bytes),
			"total_mb": bytes_to_mb(self.total_bytes),
			"speed_bps": self.speed,
			"eta_seconds": self.eta,
			"phase": self.phase,
			"filename": self.filename,
			"detail": self.error,
			"error_type": self.error_type if self.status == "error" else None,
			"original_mb": bytes_to_mb(self.original_bytes),
			"compressed_mb": bytes_to_mb(self.compressed_bytes),
			"clips": self.clips,
			"clip_count": len(self.clips) if self.clips is not None else None,
		}


_JOBS: dict[str, DownloadJob] = {}
# Referência forte às tasks em andamento (senão o GC pode coletá-las no meio).
_TASKS: set[asyncio.Task[None]] = set()


def _sweep() -> None:
	now = time.monotonic()
	for job_id in [jid for jid, job in _JOBS.items() if now - job.updated_at > JOB_TTL_SECONDS]:
		discard_job(job_id)


def get_job(job_id: str) -> DownloadJob | None:
	_sweep()
	return _JOBS.get(job_id)


def forget_job(job_id: str) -> None:
	"""Remove só a entrada (arquivos já apagados pelo streaming)."""
	_JOBS.pop(job_id, None)


def discard_job(job_id: str) -> None:
	job = _JOBS.pop(job_id, None)
	if job:
		cleanup_files(job.path, job.zip_path, *job.clip_paths)
		shutil.rmtree(job.clips_dir, ignore_errors=True)


def _spawn(job: DownloadJob, work: Coroutine[Any, Any, None]) -> DownloadJob:
	task = asyncio.create_task(work)
	_TASKS.add(task)
	task.add_done_callback(_TASKS.discard)
	return job


def _new_job() -> DownloadJob:
	_sweep()
	if sum(job.is_active for job in _JOBS.values()) >= settings.MAX_CONCURRENT_JOBS:
		raise ServerBusyError()
	job = DownloadJob(id=uuid.uuid4().hex)
	_JOBS[job.id] = job
	return job


def _progress_hook(job: DownloadJob, ceiling: float = 99.0) -> ProgressHook:
	# MP4 "best" baixa vídeo e áudio como duas faixas, cada uma recomeçando do zero;
	# ``base`` acumula as faixas concluídas para a barra ser única e monotônica.
	# ``ceiling`` reserva o fim da barra para merge/fatiamento.
	state = {"base": 0}

	def hook(d: dict[str, Any]) -> None:
		status = d.get("status")
		if status == "downloading":
			stream_total = d.get("total_bytes") or d.get("total_bytes_estimate")
			done = state["base"] + int(d.get("downloaded_bytes") or 0)
			total = state["base"] + int(stream_total) if stream_total else None
			job.status, job.phase = "downloading", "Baixando"
			job.downloaded_bytes, job.total_bytes = done, total
			job.speed = float(d["speed"]) if d.get("speed") is not None else None
			job.eta = round(d["eta"]) if d.get("eta") is not None else None
			if total:
				job.percent = max(job.percent, min(ceiling, done / total * 100.0))
		elif status == "finished":
			state["base"] += int(
				d.get("total_bytes") or d.get("total_bytes_estimate") or d.get("downloaded_bytes") or 0
			)
			job.downloaded_bytes = state["base"]
		job.touch()

	return hook


def _postprocessor_hook(job: DownloadJob, ceiling: float = 99.0) -> ProgressHook:
	phases = {"ExtractAudio": "Convertendo para MP3", "Merger": "Juntando vídeo e áudio"}

	def hook(d: dict[str, Any]) -> None:
		if d.get("status") in ("started", "processing"):
			name = d.get("postprocessor") or ""
			job.status = "processing"
			job.percent = max(job.percent, ceiling)
			job.phase = next((label for key, label in phases.items() if key in name), "Processando")
			job.touch()

	return hook


async def _compress(job: DownloadJob, path: Path) -> Path:
	"""Comprime o vídeo baixado e fica com o menor dos dois. Uma falha aqui nunca derruba o download."""
	job.status, job.phase = "processing", "Comprimindo o vídeo"
	job.percent = max(job.percent, 99.0)
	job.touch()
	try:
		out = await compress_media(path)
	except AppError as e:
		logger.warning("Job %s: compressão falhou (%s); o original é entregue", job.id, e.error_type)
		return path
	original, compressed = path.stat().st_size, out.stat().st_size
	if compressed >= original:
		cleanup_files(out)
		return path
	cleanup_files(path)
	job.original_bytes, job.compressed_bytes = original, compressed
	return out


async def _run_download(
	job: DownloadJob, platform: str, url: str, fmt: OutputFormat, quality: VideoQuality, compress: bool = False
) -> None:
	try:
		path, pretty, media_type = await get_service(platform).download_file(
			url, fmt, quality, _progress_hook(job), _postprocessor_hook(job)
		)
		if compress and fmt == OutputFormat.MP4:
			path = await _compress(job, path)
		job.finish(path, pretty, media_type)
	except Exception as e:  # noqa: BLE001 - o job em background precisa sempre terminar num estado
		job.fail(e, generic="Erro inesperado ao baixar o vídeo.")
	finally:
		job.touch()


async def _run_clips(
	job: DownloadJob, platform: str, url: str, fmt: OutputFormat, quality: VideoQuality, segment_seconds: int
) -> None:
	source: Path | None = None
	try:
		source, pretty, _ = await get_service(platform).download_file(
			url,
			fmt,
			quality,
			_progress_hook(job, _CLIPS_DOWNLOAD_CEILING),
			_postprocessor_hook(job, _CLIPS_DOWNLOAD_CEILING),
		)
		job.status, job.phase = "processing", f"Dividindo em clipes de {segment_seconds}s"
		job.percent = max(job.percent, _CLIPS_DOWNLOAD_CEILING)
		job.touch()

		base_name = sanitize_filename(Path(pretty).stem) or "video"
		segments = await split_into_segments(source, job.clips_dir, base_name, segment_seconds)

		job.percent, job.phase = 95.0, "Compactando (.zip)"
		job.touch()
		zip_path = settings.DOWNLOAD_DIR / f"clips_{job.id}.zip"
		await asyncio.to_thread(build_clips_zip, segments, zip_path)

		job.clip_paths = [seg.path for seg in segments]
		job.clips = [
			{
				"index": seg.index,
				"filename": seg.filename,
				"duration_seconds": seg.duration_seconds,
				"size_mb": bytes_to_mb(seg.size_bytes),
			}
			for seg in segments
		]
		job.zip_path = zip_path
		job.finish(zip_path, f"{base_name}_clipes.zip", "application/zip")
	except Exception as e:  # noqa: BLE001 - idem _run_download
		job.fail(e, generic="Erro inesperado ao gerar os clipes.")
	finally:
		cleanup_files(source)
		job.touch()


def start_download_job(
	platform: str, url: str, fmt: OutputFormat, quality: VideoQuality, compress: bool = False
) -> DownloadJob:
	job = _new_job()
	return _spawn(job, _run_download(job, platform, url, fmt, quality, compress))


def start_clips_job(
	platform: str,
	url: str,
	fmt: OutputFormat,
	quality: VideoQuality,
	segment_seconds: int = SEGMENT_SECONDS,
) -> DownloadJob:
	job = _new_job()
	return _spawn(job, _run_clips(job, platform, url, fmt, quality, segment_seconds))
