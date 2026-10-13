"""Rotas por plataforma, geradas a partir de ``app.platforms.PLATFORMS``.

Cada plataforma expõe ``POST /api/v1/{nome}/info``, ``/download``, ``/download/start`` e
``/download/clips/start``.
"""

from fastapi import APIRouter

from app.errors import InvalidRequestError
from app.models.schemas import DownloadJobCreated, DownloadRequest, ErrorResponse, VideoInfoResponse
from app.platforms import PLATFORMS, InvalidPlatformURLError, Platform, validate_platform_url
from app.services.downloader import get_service
from app.services.jobs import start_clips_job, start_download_job
from app.utils.streaming import stream_file_response

_ERRORS = {400: {"model": ErrorResponse}, 502: {"model": ErrorResponse}, 503: {"model": ErrorResponse}}


def build_platform_router(spec: Platform) -> APIRouter:
	router = APIRouter(prefix=f"/api/v1/{spec.name}", tags=[spec.label])

	def checked_url(payload: DownloadRequest) -> str:
		url = str(payload.url)
		try:
			validate_platform_url(spec.name, url)
		except InvalidPlatformURLError as e:
			raise InvalidRequestError(str(e)) from e
		return url

	@router.post("/info", response_model=VideoInfoResponse, responses=_ERRORS, summary="Metadados do vídeo")
	async def get_info(payload: DownloadRequest) -> VideoInfoResponse:
		return await get_service(spec.name).extract_info(checked_url(payload))

	@router.post(
		"/download",
		responses={
			200: {"content": {"application/octet-stream": {}}},
			413: {"model": ErrorResponse},
			**_ERRORS,
		},
		summary="Baixa o vídeo (MP4/MP3) de forma síncrona, em streaming",
	)
	async def download(payload: DownloadRequest):
		path, name, media_type = await get_service(spec.name).download_file(
			checked_url(payload), payload.format, payload.quality
		)
		return stream_file_response(path, download_name=name, media_type=media_type)

	@router.post(
		"/download/start",
		response_model=DownloadJobCreated,
		responses=_ERRORS,
		summary="Inicia o download em background e devolve o job_id para polling",
	)
	async def download_start(payload: DownloadRequest) -> DownloadJobCreated:
		job = start_download_job(spec.name, checked_url(payload), payload.format, payload.quality, payload.compress)
		return DownloadJobCreated(job_id=job.id)

	@router.post(
		"/download/clips/start",
		response_model=DownloadJobCreated,
		responses=_ERRORS,
		summary="Baixa e fatia o vídeo em clipes de 60s (Stories) em background",
	)
	async def download_clips_start(payload: DownloadRequest) -> DownloadJobCreated:
		job = start_clips_job(spec.name, checked_url(payload), payload.format, payload.quality)
		return DownloadJobCreated(job_id=job.id)

	return router


routers = [build_platform_router(spec) for spec in PLATFORMS.values()]
