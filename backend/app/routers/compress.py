"""Compressão de vídeo, áudio e PDF enviados por upload (multipart).

Mesmo fluxo do Cut & Crop: o upload vai para o disco em blocos, é processado e volta em
streaming. Quando a compressão não reduz o arquivo, a resposta é 204: quem chamou fica com
o original que já tem.
"""

from pathlib import Path
from typing import Annotated

from fastapi import APIRouter, File, Response, UploadFile

from app.errors import VideoProcessingError
from app.models.schemas import ErrorResponse
from app.routers.edit import _require_ffmpeg, _save_upload, _stem
from app.services.compressor import compress_media, compress_pdf, ghostscript_available
from app.utils.streaming import cleanup_files, stream_file_response

router = APIRouter(prefix="/api/v1/compress", tags=["Compressão"])

_ERRORS = {400: {"model": ErrorResponse}, 413: {"model": ErrorResponse}, 502: {"model": ErrorResponse}}
_RESPONSES = {
	204: {"description": "A compressão não reduziu o arquivo: fique com o original."},
	**_ERRORS,
}
_MEDIA_TYPES = {".mp4": "video/mp4", ".mp3": "audio/mpeg", ".pdf": "application/pdf"}


def _respond(src: Path, out: Path, stem: str) -> Response:
	original, compressed = src.stat().st_size, out.stat().st_size
	if compressed >= original:
		cleanup_files(src, out)
		return Response(status_code=204, headers={"Cache-Control": "no-store"})
	response = stream_file_response(
		out,
		download_name=f"{stem}{out.suffix}",
		media_type=_MEDIA_TYPES[out.suffix],
		cleanup_extra=(src,),
	)
	response.headers["X-Original-Size"] = str(original)
	response.headers["X-Compressed-Size"] = str(compressed)
	return response


@router.post(
	"/media",
	responses={200: {"content": {"video/mp4": {}, "audio/mpeg": {}}}, **_RESPONSES},
	summary="Comprime um vídeo (devolve .mp4) ou um áudio (devolve .mp3)",
)
async def media(file: Annotated[UploadFile, File(description="Arquivo de vídeo ou áudio")]):
	_require_ffmpeg()
	src = await _save_upload(file)
	try:
		out = await compress_media(src)
	except Exception:
		cleanup_files(src)
		raise
	return _respond(src, out, _stem(file))


@router.post(
	"/pdf",
	responses={200: {"content": {"application/pdf": {}}}, **_RESPONSES},
	summary="Comprime um PDF (imagens reamostradas para ~150 dpi)",
)
async def pdf(file: Annotated[UploadFile, File(description="Arquivo PDF")]):
	if not ghostscript_available():
		raise VideoProcessingError("O Ghostscript não está disponível no servidor.")
	src = await _save_upload(file, suffix=".pdf")
	try:
		out = await compress_pdf(src)
	except Exception:
		cleanup_files(src)
		raise
	return _respond(src, out, _stem(file))
