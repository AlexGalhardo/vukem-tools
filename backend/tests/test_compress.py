"""
Testes da compressão de mídia e de PDF.

  * build_media_command / build_pdf_command: forma dos comandos (unitário, sem executar)
  * integração: comprime um vídeo e um áudio reais (executa o ffmpeg; pulado se ausente)
  * PDF: entradas inválidas são recusadas antes de o Ghostscript rodar
"""

import shutil
import subprocess
from pathlib import Path

import pytest

from app.config import settings
from app.services import compressor
from app.services.compressor import build_media_command, build_pdf_command
from tests.conftest import FFMPEG, requires_ffmpeg

requires_ghostscript = pytest.mark.skipif(not shutil.which(settings.GHOSTSCRIPT_BIN), reason="Ghostscript ausente")


def test_media_command_for_video_and_audio():
	video = build_media_command(Path("in.mov"), Path("out.mp4"), has_video=True)
	assert video[video.index("-protocol_whitelist") + 1] == "file"
	assert video[video.index("-crf") + 1] == "28"
	assert "libx264" in video and "aac" in video
	audio = build_media_command(Path("in.mp3"), Path("out.mp3"), has_video=False)
	assert "-vn" in audio and "libmp3lame" in audio and "libx264" not in audio


def test_pdf_command_runs_ghostscript_in_safer_mode():
	cmd = build_pdf_command(Path("in.pdf"), Path("out.pdf"))
	assert cmd[1] == "-dSAFER"
	assert "-sDEVICE=pdfwrite" in cmd and "-dPDFSETTINGS=/ebook" in cmd
	assert cmd[-1] == "in.pdf"


@requires_ffmpeg
def test_video_is_compressed_to_a_smaller_mp4(client, tmp_path):
	# Ruído em alta taxa de bits: há o que comprimir, ao contrário do vídeo sintético do conftest.
	src = tmp_path / "ruido.mp4"
	subprocess.run(
		[FFMPEG, "-y", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc2=duration=2:size=640x360:rate=24"]
		+ ["-c:v", "libx264", "-crf", "5", "-pix_fmt", "yuv420p", str(src)],
		check=True,
	)
	r = client.post("/api/v1/compress/media", files={"file": ("Meu Vídeo.mp4", src.read_bytes(), "video/mp4")})
	assert r.status_code == 200, r.text
	assert r.headers["content-type"] == "video/mp4"
	assert int(r.headers["x-compressed-size"]) == len(r.content) < int(r.headers["x-original-size"])
	assert r.content[4:8] == b"ftyp"
	assert not list(settings.DOWNLOAD_DIR.glob("*"))


@requires_ffmpeg
def test_audio_is_compressed_to_mp3(client, tmp_path):
	src = tmp_path / "tom.wav"
	subprocess.run(
		[FFMPEG, "-y", "-loglevel", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=3", str(src)],
		check=True,
	)
	r = client.post("/api/v1/compress/media", files={"file": ("tom.wav", src.read_bytes(), "audio/wav")})
	assert r.status_code == 200, r.text
	assert r.headers["content-type"] == "audio/mpeg"
	assert len(r.content) < src.stat().st_size


@requires_ffmpeg
def test_no_gain_answers_204(client, sample_video, monkeypatch):
	async def same_size(src: Path) -> Path:
		out = settings.DOWNLOAD_DIR / "compress_igual.mp4"
		out.write_bytes(src.read_bytes())
		return out

	monkeypatch.setattr("app.routers.compress.compress_media", same_size)
	r = client.post("/api/v1/compress/media", files={"file": ("a.mp4", sample_video.read_bytes(), "video/mp4")})
	assert r.status_code == 204
	assert r.content == b""
	assert not list(settings.DOWNLOAD_DIR.glob("*"))


def test_pdf_needs_ghostscript(client, monkeypatch):
	monkeypatch.setattr("app.routers.compress.ghostscript_available", lambda: False)
	r = client.post("/api/v1/compress/pdf", files={"file": ("a.pdf", b"%PDF-1.4\n", "application/pdf")})
	assert r.status_code == 502
	assert r.json()["error_type"] == "VIDEO_PROCESSING_FAILED"


def test_a_file_that_is_not_a_pdf_never_reaches_ghostscript(client, monkeypatch):
	async def never(*_args, **_kwargs):
		raise AssertionError("o Ghostscript não pode rodar para um arquivo que não é PDF")

	monkeypatch.setattr("app.routers.compress.ghostscript_available", lambda: True)
	monkeypatch.setattr(compressor, "_run", never)
	postscript = b"%!PS\n(/etc/passwd) (r) file\n"
	r = client.post("/api/v1/compress/pdf", files={"file": ("evil.pdf", postscript, "application/pdf")})
	assert r.status_code == 400
	assert r.json()["error_type"] == "INVALID_MEDIA"
	assert not list(settings.DOWNLOAD_DIR.glob("*"))


@requires_ghostscript
def test_pdf_is_compressed(client):
	# Um PDF mínimo e válido: o Ghostscript o reescreve; o resultado é 200 (menor) ou 204 (sem ganho).
	body = (
		b"%PDF-1.4\n1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj\n"
		b"2 0 obj<</Type/Pages/Kids[3 0 R]/Count 1>>endobj\n"
		b"3 0 obj<</Type/Page/Parent 2 0 R/MediaBox[0 0 72 72]>>endobj\n"
		b"trailer<</Root 1 0 R>>\n%%EOF\n"
	)
	r = client.post("/api/v1/compress/pdf", files={"file": ("a.pdf", body, "application/pdf")})
	assert r.status_code in (200, 204), r.text
	if r.status_code == 200:
		assert r.content.startswith(b"%PDF-")
