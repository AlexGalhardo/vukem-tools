"""
Testes do limite de tamanho (requisito: máximo 1 GB por vídeo).

  * _to_response marca exceeds_size_limit quando o tamanho estimado passa
  * _download_sync repassa max_filesize ao yt-dlp e levanta FileTooLargeError
    quando o arquivo final excede o limite
  * o endpoint /download traduz FileTooLargeError em HTTP 413
"""

from pathlib import Path

import pytest

from app.config import settings
from app.errors import MediaTooLargeError as FileTooLargeError
from app.models.schemas import OutputFormat, VideoQuality
from app.services import downloader as dl
from app.services.downloader import VideoDownloadService


def test_default_limit_is_1gb():
	assert settings.MAX_FILE_SIZE_MB == 1024
	assert settings.max_file_size_bytes == 1024 * 1024 * 1024


def test_info_flags_when_estimated_size_exceeds(monkeypatch):
	svc = VideoDownloadService(platform="youtube", cookies_file=None)
	huge = settings.max_file_size_bytes + 5 * 1024 * 1024
	info = {
		"id": "x",
		"title": "Huge",
		"webpage_url": "https://y",
		"filesize_approx": huge,
		"formats": [],
	}
	resp = svc._to_response(info)
	assert resp.exceeds_size_limit is True
	assert resp.estimated_filesize_mb > settings.MAX_FILE_SIZE_MB


def test_info_not_flagged_for_small_video():
	svc = VideoDownloadService(platform="twitter", cookies_file=None)
	info = {
		"id": "x",
		"title": "Small",
		"webpage_url": "https://y",
		"filesize": 5 * 1024 * 1024,
		"formats": [],
	}
	resp = svc._to_response(info)
	assert resp.exceeds_size_limit is False


class _FakeYDL:
	"""Substitui yt_dlp.YoutubeDL: grava um arquivo do tamanho pedido no outtmpl."""

	recorded_opts: dict = {}
	file_size_bytes = 0

	def __init__(self, opts):
		_FakeYDL.recorded_opts = opts
		self.opts = opts

	def __enter__(self):
		return self

	def __exit__(self, *a):
		return False

	def extract_info(self, url, download=False):
		info = {"id": "abc", "title": "Big Buck", "ext": "mp4", "webpage_url": url, "formats": []}
		if download:
			target = Path(self.opts["outtmpl"].replace("%(ext)s", "mp4"))
			target.parent.mkdir(parents=True, exist_ok=True)
			with target.open("wb") as fh:
				fh.seek(_FakeYDL.file_size_bytes - 1)
				fh.write(b"\0")
		return info

	def prepare_filename(self, info):
		return self.opts["outtmpl"].replace("%(ext)s", info["ext"])


@pytest.fixture()
def patch_ydl(monkeypatch):
	monkeypatch.setattr(dl.yt_dlp, "YoutubeDL", _FakeYDL)
	yield _FakeYDL


def test_download_sync_sets_max_filesize(patch_ydl):
	svc = VideoDownloadService(platform="twitter", cookies_file=None)
	patch_ydl.file_size_bytes = 1024  # 1 KB, bem abaixo do limite
	path, pretty = svc._download_sync("https://x.com/a/status/1", OutputFormat.MP4, VideoQuality.BEST, "task1")
	assert patch_ydl.recorded_opts["max_filesize"] == settings.max_file_size_bytes
	assert path.exists()
	assert pretty.endswith(".mp4")
	path.unlink(missing_ok=True)


def test_download_sync_raises_when_output_too_large(patch_ydl, monkeypatch):
	monkeypatch.setattr(settings, "MAX_FILE_SIZE_MB", 1)  # limite de 1 MB
	patch_ydl.file_size_bytes = 3 * 1024 * 1024  # 3 MB de saída
	svc = VideoDownloadService(platform="twitter", cookies_file=None)
	with pytest.raises(FileTooLargeError):
		svc._download_sync("https://x.com/a/status/1", OutputFormat.MP4, VideoQuality.BEST, "task2")
	# não deixou lixo
	assert list(settings.DOWNLOAD_DIR.glob("task2.*")) == []


def test_endpoint_returns_413(client, monkeypatch):
	class TooBigService:
		def __init__(self, platform, **kw):
			self.platform = platform

		async def download_file(self, *a, **kw):
			raise FileTooLargeError("O vídeo excede o limite de 1024 MB.")

	monkeypatch.setattr("app.routers.platforms.get_service", lambda platform: TooBigService(platform))
	r = client.post(
		"/api/v1/twitter/download",
		json={"url": "https://x.com/a/status/1", "format": "mp4"},
	)
	assert r.status_code == 413
	assert "excede o limite" in r.json()["detail"]
