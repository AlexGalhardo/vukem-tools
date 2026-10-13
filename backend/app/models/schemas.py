"""
schemas.py
----------
Modelos Pydantic que definem o formato dos dados de entrada (request body) e
saída (response body) da API. Além da validação automática, alimentam a
documentação interativa em ``/docs``.
"""

from enum import Enum
from typing import Literal, Optional

from pydantic import BaseModel, Field, HttpUrl


class OutputFormat(str, Enum):
	"""Formato final entregue ao usuário."""

	MP4 = "mp4"  # vídeo + áudio (merge via ffmpeg quando necessário)
	MP3 = "mp3"  # somente áudio, convertido para mp3 192 kbps


class VideoQuality(str, Enum):
	"""Só se aplica quando ``format == mp4``."""

	BEST = "best"
	WORST = "worst"


class DownloadRequest(BaseModel):
	"""Corpo das requisições de ``/info`` e ``/download``."""

	url: HttpUrl = Field(..., description="URL completa do vídeo/post/tweet")
	format: OutputFormat = Field(
		default=OutputFormat.MP4,
		description="mp4 (vídeo) ou mp3 (áudio)",
	)
	quality: VideoQuality = Field(
		default=VideoQuality.BEST,
		description="Qualidade do vídeo quando format=mp4 (ignorado em mp3)",
	)
	compress: bool = Field(
		default=False,
		description="Comprime o MP4 depois de baixar (só em /download/start; ignorado em mp3 e nos clipes)",
	)

	model_config = {
		"json_schema_extra": {
			"example": {
				"url": "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
				"format": "mp4",
				"quality": "best",
			}
		}
	}


class FormatInfo(BaseModel):
	"""Um formato/qualidade individual disponível para o vídeo."""

	format_id: str
	ext: str
	resolution: Optional[str] = None
	fps: Optional[float] = None
	filesize_mb: Optional[float] = None
	acodec: Optional[str] = None
	vcodec: Optional[str] = None
	note: Optional[str] = None


class VideoInfoResponse(BaseModel):
	"""Resposta de ``/info``: metadados do vídeo, sem baixar o arquivo."""

	platform: Literal["youtube", "instagram", "twitter", "tiktok"]
	id: str
	title: str
	uploader: Optional[str] = None
	duration_seconds: Optional[float] = None
	thumbnail: Optional[str] = None
	view_count: Optional[int] = None
	upload_date: Optional[str] = None
	webpage_url: str
	estimated_filesize_mb: Optional[float] = Field(
		default=None,
		description="Tamanho aproximado do melhor formato, quando conhecido.",
	)
	exceeds_size_limit: bool = Field(
		default=False,
		description="True se o tamanho estimado já ultrapassa o limite configurado.",
	)
	max_file_size_mb: int
	formats: list[FormatInfo] = []


class DownloadJobCreated(BaseModel):
	"""Resposta de ``POST {prefix}/download/start`` e ``.../download/clips/start``."""

	job_id: str


class ClipInfo(BaseModel):
	"""Um clipe de 60s gerado pelo modo "Clipes p/ Stories"."""

	index: int  # 1-based
	filename: str
	duration_seconds: float
	size_mb: Optional[float] = None


class DownloadJobStatus(BaseModel):
	"""Progresso de um job de download (polling do front-end)."""

	job_id: str
	status: Literal["pending", "downloading", "processing", "finished", "error"]
	percent: float
	downloaded_bytes: int = 0
	total_bytes: Optional[int] = None
	downloaded_mb: Optional[float] = None
	total_mb: Optional[float] = None
	speed_bps: Optional[float] = None
	eta_seconds: Optional[int] = None
	phase: str
	filename: Optional[str] = None
	detail: Optional[str] = None
	error_type: Optional[str] = Field(
		default=None,
		description=("Código estável do erro quando status=='error' (ver app/errors.py)."),
	)
	# Preenchidos quando o vídeo foi comprimido depois de baixado e ficou menor.
	original_mb: Optional[float] = None
	compressed_mb: Optional[float] = None
	# Preenchidos apenas quando o job é do modo "Clipes p/ Stories" e terminou.
	clips: Optional[list[ClipInfo]] = None
	clip_count: Optional[int] = None


class ErrorResponse(BaseModel):
	"""Formato padrão de erro da API."""

	detail: str
	error_type: Optional[str] = None
