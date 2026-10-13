"""
compressor.py
-------------
Compressão de mídia (ffmpeg) e de PDF (Ghostscript) para arquivos enviados por upload.

Segue as mesmas regras do ``editor.py``: subprocesso com lista de argumentos dentro de uma
thread, entrada do ffmpeg restrita ao protocolo ``file`` e a demuxers de mídia comuns, nada
carregado inteiro em memória.
"""

import json
import logging
import shutil
import uuid
from pathlib import Path

from app.config import settings
from app.errors import InvalidMediaError, VideoProcessingError, classify_ffmpeg_failure
from app.services.editor import _ALLOWED_DEMUXERS, _INPUT_HARDENING, _run, _threads_args

logger = logging.getLogger("video-api")

# Nível "equilibrado": bem menor que o original, com perda pouco visível.
VIDEO_CRF = 28
AUDIO_BITRATE = "128k"
# Reamostra as imagens do PDF para ~150 dpi; o texto continua vetorial.
PDF_SETTINGS = "/ebook"
PDF_TIMEOUT_SECONDS = 300.0


def ghostscript_available() -> bool:
	return bool(shutil.which(settings.GHOSTSCRIPT_BIN))


async def _has_video(src: Path) -> bool:
	"""Valida a mídia com ffprobe e diz se há faixa de vídeo (a capa de um MP3 não conta)."""
	cmd = [
		settings.FFPROBE_BIN,
		"-v",
		"error",
		*_INPUT_HARDENING,
		"-print_format",
		"json",
		"-show_format",
		"-show_streams",
		str(src),
	]
	code, out, _err = await _run(cmd)
	if code != 0:
		raise InvalidMediaError(
			"Não foi possível ler esse arquivo de mídia (ffprobe falhou). "
			"Ele pode estar corrompido ou num formato não suportado."
		)
	data = json.loads(out or b"{}")
	demuxers = set((data.get("format", {}).get("format_name") or "").split(","))
	if not demuxers <= _ALLOWED_DEMUXERS:
		raise InvalidMediaError("Formato de arquivo não suportado. Envie um vídeo ou um áudio comum.")
	streams = data.get("streams", [])
	if not any(s.get("codec_type") in ("video", "audio") for s in streams):
		raise InvalidMediaError("O arquivo enviado não contém áudio nem vídeo.")
	return any(s.get("codec_type") == "video" and not (s.get("disposition") or {}).get("attached_pic") for s in streams)


def build_media_command(src: Path, dst: Path, *, has_video: bool) -> list[str]:
	"""Comando ffmpeg da compressão. Isolado para ser testado sem executar o ffmpeg."""
	head = [settings.FFMPEG_BIN, "-y", "-hide_banner", "-loglevel", "error", *_INPUT_HARDENING, "-i", str(src)]
	if not has_video:
		return [*head, "-vn", "-c:a", "libmp3lame", "-b:a", AUDIO_BITRATE, *_threads_args(), str(dst)]
	return [
		*head,
		"-c:v",
		"libx264",
		"-preset",
		"veryfast",
		"-crf",
		str(VIDEO_CRF),
		"-pix_fmt",
		"yuv420p",
		"-c:a",
		"aac",
		"-b:a",
		AUDIO_BITRATE,
		"-movflags",
		"+faststart",
		*_threads_args(),
		str(dst),
	]


async def compress_media(src: Path) -> Path:
	"""Comprime um vídeo (saída ``.mp4``) ou um áudio (saída ``.mp3``) e devolve o arquivo novo."""
	has_video = await _has_video(src)
	dst = settings.DOWNLOAD_DIR / f"compress_{uuid.uuid4().hex}.{'mp4' if has_video else 'mp3'}"
	code, _out, err = await _run(build_media_command(src, dst, has_video=has_video))
	if code != 0 or not dst.exists():
		dst.unlink(missing_ok=True)
		raise classify_ffmpeg_failure(code, err.decode(errors="ignore"))
	return dst


def build_pdf_command(src: Path, dst: Path) -> list[str]:
	"""Comando Ghostscript. ``-dSAFER`` impede o PostScript do arquivo de ler ou escrever no disco."""
	return [
		settings.GHOSTSCRIPT_BIN,
		"-dSAFER",
		"-dBATCH",
		"-dNOPAUSE",
		"-dQUIET",
		"-sDEVICE=pdfwrite",
		"-dCompatibilityLevel=1.5",
		f"-dPDFSETTINGS={PDF_SETTINGS}",
		f"-sOutputFile={dst}",
		str(src),
	]


async def compress_pdf(src: Path) -> Path:
	"""Comprime um PDF e devolve o arquivo novo."""
	with src.open("rb") as fh:
		if fh.read(5) != b"%PDF-":
			raise InvalidMediaError("O arquivo enviado não é um PDF válido.")
	dst = settings.DOWNLOAD_DIR / f"compress_{uuid.uuid4().hex}.pdf"
	code, _out, _err = await _run(build_pdf_command(src, dst), timeout=PDF_TIMEOUT_SECONDS)
	if code != 0 or not dst.exists() or dst.stat().st_size == 0:
		dst.unlink(missing_ok=True)
		logger.warning("Ghostscript falhou (código %s)", code)
		raise VideoProcessingError("Não foi possível comprimir esse PDF. Ele pode estar corrompido ou protegido.")
	return dst
