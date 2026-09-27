"""Único ponto de contato com o backend FastAPI (o bot nunca usa o yt-dlp diretamente)."""

from __future__ import annotations

import re
from dataclasses import dataclass, field
from typing import Any, Literal
from urllib.parse import unquote

import httpx

Platform = Literal["youtube", "instagram", "twitter", "tiktok"]
OutputFormat = Literal["mp4", "mp3"]
JobMode = Literal["full", "clips"]
JobStatusName = Literal["pending", "downloading", "processing", "finished", "error"]


class BackendError(Exception):
	"""Erro estruturado do backend (``{detail, error_type}``)."""

	def __init__(self, detail: str, error_type: str | None) -> None:
		super().__init__(detail)
		self.detail = detail
		self.error_type = error_type


@dataclass(frozen=True)
class VideoInfo:
	title: str
	uploader: str | None
	duration_seconds: float | None
	thumbnail: str | None
	estimated_filesize_mb: float | None
	exceeds_size_limit: bool
	max_file_size_mb: int


@dataclass(frozen=True)
class Clip:
	index: int
	filename: str
	size_mb: float | None


@dataclass(frozen=True)
class JobStatus:
	job_id: str
	status: JobStatusName
	percent: float
	phase: str
	total_bytes: int | None
	detail: str | None
	error_type: str | None
	clips: list[Clip] = field(default_factory=list)


@dataclass(frozen=True)
class DownloadedFile:
	content: bytes
	filename: str
	media_type: str


def _raise_for_error(response: httpx.Response) -> None:
	if response.is_success:
		return
	try:
		body: dict[str, Any] = response.json()
	except ValueError:
		body = {}
	raise BackendError(
		detail=body.get("detail") or f"Erro HTTP {response.status_code}", error_type=body.get("error_type")
	)


_FILENAME_STAR = re.compile(r"filename\*=UTF-8''([^;]+)", re.IGNORECASE)
_FILENAME = re.compile(r'filename="?([^";]+)"?', re.IGNORECASE)


def filename_from_content_disposition(header: str | None) -> str | None:
	if not header:
		return None
	if match := _FILENAME_STAR.search(header):
		return unquote(match.group(1))
	if match := _FILENAME.search(header):
		return match.group(1)
	return None


class BackendClient:
	def __init__(self, base_url: str, api_key: str = "", timeout: float = 30.0, file_timeout: float = 120.0) -> None:
		headers = {"X-API-Key": api_key} if api_key else {}
		self._client = httpx.AsyncClient(base_url=base_url, timeout=timeout, headers=headers)
		self._file_timeout = file_timeout
		# Deploy do backend que respondeu por último (header X-Deploy-Target); None = nunca respondeu.
		self.last_deploy_target: str | None = None

	def _check(self, response: httpx.Response) -> None:
		if deploy := response.headers.get("x-deploy-target"):
			self.last_deploy_target = deploy
		_raise_for_error(response)

	async def health(self) -> str | None:
		"""Consulta ``/health`` e devolve o deploy do backend (None se estiver fora do ar)."""
		try:
			response = await self._client.get("/health")
			self._check(response)
		except (httpx.HTTPError, BackendError):
			return None
		return response.json().get("deploy_target") or self.last_deploy_target

	async def aclose(self) -> None:
		await self._client.aclose()

	async def detect_platform(self, url: str) -> Platform | None:
		response = await self._client.get("/api/v1/detect", params={"url": url})
		self._check(response)
		return response.json().get("platform")

	async def get_info(self, platform: Platform, url: str) -> VideoInfo:
		response = await self._client.post(f"/api/v1/{platform}/info", json={"url": url})
		self._check(response)
		data = response.json()
		return VideoInfo(
			title=data["title"],
			uploader=data.get("uploader"),
			duration_seconds=data.get("duration_seconds"),
			thumbnail=data.get("thumbnail"),
			estimated_filesize_mb=data.get("estimated_filesize_mb"),
			exceeds_size_limit=data.get("exceeds_size_limit", False),
			max_file_size_mb=data["max_file_size_mb"],
		)

	async def start_job(self, platform: Platform, url: str, fmt: OutputFormat, mode: JobMode) -> str:
		endpoint = "download/clips/start" if mode == "clips" else "download/start"
		response = await self._client.post(
			f"/api/v1/{platform}/{endpoint}", json={"url": url, "format": fmt, "quality": "best"}
		)
		self._check(response)
		return response.json()["job_id"]

	async def get_job_status(self, job_id: str) -> JobStatus:
		response = await self._client.get(f"/api/v1/downloads/{job_id}")
		self._check(response)
		data = response.json()
		return JobStatus(
			job_id=data["job_id"],
			status=data["status"],
			percent=data["percent"],
			phase=data["phase"],
			total_bytes=data.get("total_bytes"),
			detail=data.get("detail"),
			error_type=data.get("error_type"),
			clips=[Clip(c["index"], c["filename"], c.get("size_mb")) for c in data.get("clips") or []],
		)

	async def _get_file(self, path: str, fallback_name: str) -> DownloadedFile:
		response = await self._client.get(path, timeout=self._file_timeout)
		self._check(response)
		return DownloadedFile(
			content=response.content,
			filename=filename_from_content_disposition(response.headers.get("content-disposition")) or fallback_name,
			media_type=response.headers.get("content-type", "application/octet-stream"),
		)

	async def download_file(self, job_id: str) -> DownloadedFile:
		"""Arquivo final do job (uso único: o backend apaga depois de transmitir)."""
		return await self._get_file(f"/api/v1/downloads/{job_id}/file", f"{job_id}.bin")

	async def download_clip(self, job_id: str, index: int) -> DownloadedFile:
		return await self._get_file(f"/api/v1/downloads/{job_id}/clips/{index}/file", f"parte{index}.mp4")
