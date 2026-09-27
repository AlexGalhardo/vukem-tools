"""Acompanhamento e coleta de jobs iniciados por ``{prefix}/download/start`` ou ``.../clips/start``.

No modo normal ``/file`` descarta o job ao terminar de transmitir. No modo clipes nada é
descartado sob demanda (vários arquivos podem ser baixados); o TTL de 1h limpa.
"""

from typing import Annotated

from fastapi import APIRouter, Path

from app.errors import AppError, ConflictError, NotFoundError
from app.models.schemas import DownloadJobStatus, ErrorResponse
from app.services.jobs import DownloadJob, forget_job, get_job
from app.utils.streaming import stream_file_response

router = APIRouter(prefix="/api/v1/downloads", tags=["Downloads"])

JobId = Annotated[str, Path(pattern=r"^[0-9a-f]{32}$")]
_ERRORS = {404: {"model": ErrorResponse}, 409: {"model": ErrorResponse}}


def _job_or_404(job_id: str) -> DownloadJob:
	job = get_job(job_id)
	if job is None:
		raise NotFoundError("Job não encontrado ou expirado.")
	return job


def _finished_job(job_id: str) -> DownloadJob:
	job = _job_or_404(job_id)
	if job.status == "error":
		raise AppError.from_parts(
			message=job.error or "Falha no download.", status_code=job.error_status, error_type=job.error_type
		)
	if job.status != "finished" or job.path is None:
		raise ConflictError()
	return job


@router.get("/{job_id}", response_model=DownloadJobStatus, responses=_ERRORS, summary="Progresso de um job")
async def job_status(job_id: JobId) -> dict:
	return _job_or_404(job_id).as_dict()


@router.get("/{job_id}/file", responses=_ERRORS, summary="Arquivo final (no modo clipes, o .zip)")
async def job_file(job_id: JobId):
	job = _finished_job(job_id)
	is_clips = job.clips is not None
	return stream_file_response(
		job.path,
		download_name=job.filename or f"{job_id}.bin",
		media_type=job.media_type or "application/octet-stream",
		# No modo clipes o .zip é compartilhado com /zip e os clipes individuais.
		cleanup=not is_clips,
		after=None if is_clips else (lambda: forget_job(job_id)),
	)


@router.get("/{job_id}/clips/{index}/file", responses=_ERRORS, summary="Um clipe individual (index a partir de 1)")
async def job_clip_file(job_id: JobId, index: Annotated[int, Path(ge=1)]):
	job = _finished_job(job_id)
	if index > len(job.clip_paths):
		raise NotFoundError("Clipe inexistente.")
	path = job.clip_paths[index - 1]
	media_type = "audio/mpeg" if path.suffix.lower() == ".mp3" else "video/mp4"
	return stream_file_response(path, download_name=path.name, media_type=media_type, cleanup=False)


@router.get("/{job_id}/zip", responses=_ERRORS, summary="Todos os clipes num .zip")
async def job_clips_zip(job_id: JobId):
	job = _finished_job(job_id)
	if job.zip_path is None:
		raise NotFoundError("Este job não gerou clipes.")
	return stream_file_response(
		job.zip_path,
		download_name=job.filename or f"{job_id}_clipes.zip",
		media_type="application/zip",
		cleanup=False,
	)
