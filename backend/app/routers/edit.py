"""Cut & Crop e fatiamento em clipes de vídeos enviados pelo usuário (multipart).

O upload é gravado em disco em blocos (nunca inteiro em memória), processado pelo ffmpeg e
devolvido em streaming; os temporários são apagados ao final.
"""

import asyncio
import shutil
import uuid
from pathlib import Path
from typing import Annotated

from fastapi import APIRouter, File, Form, UploadFile

from app.config import settings
from app.errors import (
	AppError,
	InvalidRequestError,
	MediaTooLargeError,
	VideoProcessingError,
	classify_oserror,
)
from app.models.schemas import ErrorResponse
from app.services.editor import (
	SEGMENT_SECONDS,
	build_clips_zip,
	cut_and_crop,
	ffmpeg_available,
	probe,
	split_into_segments,
	validate_params,
)
from app.utils.filenames import sanitize_filename
from app.utils.streaming import cleanup_files, stream_file_response

router = APIRouter(prefix="/api/v1/edit", tags=["Cut & Crop"])

_CHUNK = 1024 * 1024
_UPLOAD_SUFFIXES = {".mp4", ".mov", ".m4v", ".mkv", ".webm", ".avi", ".flv", ".ts", ".mp3", ".m4a", ".wav"}
_ERRORS = {400: {"model": ErrorResponse}, 413: {"model": ErrorResponse}, 502: {"model": ErrorResponse}}


def _require_ffmpeg() -> None:
	if not ffmpeg_available():
		raise VideoProcessingError("ffmpeg não está disponível no servidor.")


async def _save_upload(file: UploadFile, suffix: str | None = None) -> Path:
	"""Grava o upload em blocos. ``suffix`` fixa a extensão; sem ele vale a do nome, se for de mídia."""
	if suffix is None:
		named = Path(file.filename or "").suffix.lower()
		suffix = named if named in _UPLOAD_SUFFIXES else ".mp4"
	dst = settings.DOWNLOAD_DIR / f"upload_{uuid.uuid4().hex}{suffix}"
	total = 0
	try:
		with dst.open("wb") as out:
			while chunk := await file.read(_CHUNK):
				total += len(chunk)
				if total > settings.max_upload_size_bytes:
					raise MediaTooLargeError(f"O upload excede o limite de {settings.MAX_UPLOAD_SIZE_MB} MB.")
				out.write(chunk)
	except OSError as e:
		cleanup_files(dst)
		raise (classify_oserror(e) or e) from e
	except AppError:
		cleanup_files(dst)
		raise
	finally:
		await file.close()
	if total == 0:
		cleanup_files(dst)
		raise InvalidRequestError("Arquivo vazio.")
	return dst


def _stem(file: UploadFile) -> str:
	return sanitize_filename(Path(file.filename or "video").stem) or "video"


@router.get("/capabilities", summary="Informa se o ffmpeg está disponível no servidor")
async def capabilities() -> dict:
	return {
		"ffmpeg_available": ffmpeg_available(),
		"max_upload_size_mb": settings.MAX_UPLOAD_SIZE_MB,
		"output_formats": ["mp4", "mp3"],
	}


@router.post(
	"/cut-crop",
	responses={200: {"content": {"application/octet-stream": {}}}, **_ERRORS},
	summary="Corta por tempo e recorta (crop) por dimensões um vídeo enviado",
)
async def cut_crop(
	file: Annotated[UploadFile, File(description="Arquivo de vídeo")],
	start: Annotated[float, Form(ge=0)] = 0.0,
	end: Annotated[float | None, Form(gt=0)] = None,
	crop_x: Annotated[int | None, Form(ge=0)] = None,
	crop_y: Annotated[int | None, Form(ge=0)] = None,
	crop_w: Annotated[int | None, Form(gt=0)] = None,
	crop_h: Annotated[int | None, Form(gt=0)] = None,
	output_format: Annotated[str, Form(pattern="^(mp4|mp3)$")] = "mp4",
):
	_require_ffmpeg()
	src = await _save_upload(file)
	try:
		params = validate_params(
			await probe(src),
			start=start,
			end=end,
			crop_x=crop_x,
			crop_y=crop_y,
			crop_w=crop_w,
			crop_h=crop_h,
			output_format=output_format,
		)
		out_path = await cut_and_crop(src, params)
	except Exception:
		cleanup_files(src)
		raise

	media_type = "audio/mpeg" if params.output_format == "mp3" else "video/mp4"
	return stream_file_response(
		out_path,
		download_name=f"{_stem(file)}_edit.{params.output_format}",
		media_type=media_type,
		cleanup_extra=(src,),
	)


@router.post(
	"/split",
	responses={200: {"content": {"application/zip": {}}}, **_ERRORS},
	summary="Divide o vídeo enviado em clipes (padrão 60s) e devolve um .zip",
)
async def split(
	file: Annotated[UploadFile, File(description="Arquivo de vídeo")],
	segment_seconds: Annotated[int, Form(ge=5, le=600)] = SEGMENT_SECONDS,
):
	_require_ffmpeg()
	src = await _save_upload(file)
	split_dir = settings.DOWNLOAD_DIR / f"split_{uuid.uuid4().hex}"
	zip_path = split_dir.with_suffix(".zip")
	base_name = _stem(file)
	try:
		await probe(src)
		segments = await split_into_segments(src, split_dir, base_name, segment_seconds)
		await asyncio.to_thread(build_clips_zip, segments, zip_path)
	except Exception:
		cleanup_files(src, zip_path)
		shutil.rmtree(split_dir, ignore_errors=True)
		raise

	response = stream_file_response(
		zip_path,
		download_name=f"{base_name}_clipes.zip",
		media_type="application/zip",
		cleanup_extra=(src,),
		after=lambda: shutil.rmtree(split_dir, ignore_errors=True),
	)
	response.headers["X-Clip-Count"] = str(len(segments))
	return response
