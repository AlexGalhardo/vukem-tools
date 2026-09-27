"""
conftest.py
-----------
Fixtures compartilhadas pelos testes.

Os testes NUNCA acessam a rede real: o download do yt-dlp é sempre
monkeypatchado. Só o teste de integração de corte/crop executa o ffmpeg de
verdade (e é pulado automaticamente se o ffmpeg não estiver instalado).
"""

# Garante que os diretórios de trabalho apontem para uma pasta temporária
# ANTES de qualquer import de app.*, para não sujar ./downloads durante os testes.
import os
import shutil
import subprocess
import uuid
from pathlib import Path

import pytest

_TMP_ROOT = Path(__file__).resolve().parent / "_tmp"
_TMP_ROOT.mkdir(exist_ok=True)
os.environ.setdefault("DOWNLOAD_DIR", str(_TMP_ROOT / "downloads"))
os.environ.setdefault("COOKIES_DIR", str(_TMP_ROOT / "cookies"))
os.environ["ENV_FILE"] = ""
os.environ.setdefault("MAX_FILE_SIZE_MB", "1024")

from app.config import settings  # noqa: E402
from app.main import app  # noqa: E402

try:  # httpx é dependência de dev
	from fastapi.testclient import TestClient
except Exception:  # pragma: no cover
	TestClient = None


@pytest.fixture()
def client():
	with TestClient(app) as c:
		yield c


@pytest.fixture(autouse=True)
def _clean_download_dir():
	settings.DOWNLOAD_DIR.mkdir(parents=True, exist_ok=True)
	yield
	for p in settings.DOWNLOAD_DIR.glob("*"):
		if p.is_file():
			p.unlink(missing_ok=True)


FFMPEG = shutil.which("ffmpeg")
FFPROBE = shutil.which("ffprobe")
requires_ffmpeg = pytest.mark.skipif(not (FFMPEG and FFPROBE), reason="ffmpeg/ffprobe não instalados")


@pytest.fixture(scope="session")
def sample_video(tmp_path_factory) -> Path:
	"""Gera um vídeo de teste 320x240, 3s, 10fps com o ffmpeg (testsrc)."""
	if not FFMPEG:
		pytest.skip("ffmpeg não instalado")
	out = tmp_path_factory.mktemp("media") / f"sample_{uuid.uuid4().hex}.mp4"
	subprocess.run(
		[
			FFMPEG,
			"-y",
			"-hide_banner",
			"-loglevel",
			"error",
			"-f",
			"lavfi",
			"-i",
			"testsrc=duration=3:size=320x240:rate=10",
			"-f",
			"lavfi",
			"-i",
			"sine=frequency=440:duration=3",
			"-c:v",
			"libx264",
			"-pix_fmt",
			"yuv420p",
			"-c:a",
			"aac",
			"-shortest",
			str(out),
		],
		check=True,
	)
	return out
