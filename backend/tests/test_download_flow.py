"""
Fluxo de download com o serviço yt-dlp mockado (sem rede).

Cobre:
  * /info devolve os metadados mapeados no schema
  * /download devolve o arquivo em streaming, com Content-Disposition
  * escolha de formato (mp4 vs mp3) chega ao serviço
  * URL de outra plataforma -> HTTP 400
  * erro de extração -> HTTP 502
"""

import uuid

import pytest

from app.config import settings
from app.errors import DownloadFailedError as DownloadError
from app.models.schemas import OutputFormat, VideoInfoResponse, VideoQuality


class FakeService:
	calls: list[tuple] = []

	def __init__(self, platform, cookies_file=None):
		self.platform = platform

	async def extract_info(self, url: str) -> VideoInfoResponse:
		if "boom" in url:
			raise DownloadError("extractor exploded")
		return VideoInfoResponse(
			platform=self.platform,
			id="vid123",
			title="Título de Teste",
			uploader="Canal Teste",
			duration_seconds=42.0,
			thumbnail="https://img/thumb.jpg",
			view_count=1000,
			upload_date="20250101",
			webpage_url=url,
			estimated_filesize_mb=12.5,
			exceeds_size_limit=False,
			max_file_size_mb=settings.MAX_FILE_SIZE_MB,
			formats=[],
		)

	async def download_file(self, url: str, fmt: OutputFormat, quality: VideoQuality):
		FakeService.calls.append((self.platform, url, fmt, quality))
		ext = "mp3" if fmt == OutputFormat.MP3 else "mp4"
		path = settings.DOWNLOAD_DIR / f"fake_{uuid.uuid4().hex}.{ext}"
		path.write_bytes(b"BINARY-CONTENT-" * 1000)
		media_type = "audio/mpeg" if ext == "mp3" else "video/mp4"
		return path, f"Titulo_de_Teste.{ext}", media_type


@pytest.fixture(autouse=True)
def _patch_service(monkeypatch):
	FakeService.calls = []
	monkeypatch.setattr("app.routers.platforms.get_service", lambda platform: FakeService(platform))
	yield


def test_info_maps_metadata(client):
	r = client.post(
		"/api/v1/youtube/info",
		json={"url": "https://www.youtube.com/watch?v=abc", "format": "mp4"},
	)
	assert r.status_code == 200
	body = r.json()
	assert body["platform"] == "youtube"
	assert body["title"] == "Título de Teste"
	assert body["duration_seconds"] == 42.0
	assert body["max_file_size_mb"] == settings.MAX_FILE_SIZE_MB


def test_info_extractor_error_is_502(client):
	r = client.post(
		"/api/v1/twitter/info",
		json={"url": "https://x.com/a/status/boom", "format": "mp4"},
	)
	assert r.status_code == 502
	assert "exploded" in r.json()["detail"]


def test_info_rejects_cross_platform_url(client):
	r = client.post(
		"/api/v1/youtube/info",
		json={"url": "https://x.com/a/status/1", "format": "mp4"},
	)
	assert r.status_code == 400


def test_download_streams_mp4_and_cleans_up(client):
	r = client.post(
		"/api/v1/twitter/download",
		json={"url": "https://x.com/a/status/1", "format": "mp4", "quality": "best"},
	)
	assert r.status_code == 200
	assert r.headers["content-type"].startswith("video/mp4")
	assert "attachment" in r.headers["content-disposition"]
	assert ".mp4" in r.headers["content-disposition"]
	assert r.content.startswith(b"BINARY-CONTENT-")
	assert int(r.headers["content-length"]) == len(r.content)
	# a StreamingResponse roda o BackgroundTask de limpeza após enviar tudo
	leftovers = list(settings.DOWNLOAD_DIR.glob("fake_*"))
	assert leftovers == []


def test_download_passes_mp3_format_to_service(client):
	r = client.post(
		"/api/v1/youtube/download",
		json={"url": "https://youtu.be/abc", "format": "mp3"},
	)
	assert r.status_code == 200
	assert r.headers["content-type"].startswith("audio/mpeg")
	assert FakeService.calls[-1][2] == OutputFormat.MP3
