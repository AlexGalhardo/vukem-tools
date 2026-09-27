"""
Download em background com acompanhamento de progresso.

  * hooks de progresso do yt-dlp -> campos do job (percent, eta como int, fase)
  * POST {prefix}/download/start -> job_id
  * GET  /api/v1/downloads/{id}  -> polling até "finished"
  * GET  /api/v1/downloads/{id}/file -> streaming + descarte do job
  * caminho de erro -> status "error" e /file devolvendo o status HTTP correto
  * job desconhecido -> 404
"""

import time
import uuid

import pytest

from app.config import settings
from app.errors import DownloadFailedError as DownloadError
from app.errors import MediaTooLargeError as FileTooLargeError
from app.models.schemas import OutputFormat
from app.services import jobs
from app.services.jobs import DownloadJob, _postprocessor_hook, _progress_hook

# --------------------------- hooks (unitário) ---------------------------


def test_progress_hook_maps_fields_and_caps_at_99():
	job = DownloadJob(id="j1")
	_progress_hook(job)(
		{
			"status": "downloading",
			"downloaded_bytes": 500,
			"total_bytes": 1000,
			"speed": 1234.5,
			"eta": 2.7,  # yt-dlp às vezes manda float
		}
	)
	assert job.status == "downloading"
	assert job.percent == 50.0
	assert job.eta == 3 and isinstance(job.eta, int)
	assert job.speed == pytest.approx(1234.5)
	assert job.phase == "Baixando"

	_progress_hook(job)({"status": "downloading", "downloaded_bytes": 1000, "total_bytes": 1000})
	assert job.percent == 99.0  # reserva o último 1% para o pós-processamento


def test_progress_hook_uses_estimate_when_total_unknown():
	job = DownloadJob(id="j2")
	_progress_hook(job)({"status": "downloading", "downloaded_bytes": 250, "total_bytes_estimate": 1000})
	assert job.percent == 25.0
	assert job.total_bytes == 1000


def test_pp_hook_sets_processing_phase():
	job = DownloadJob(id="j3")
	_postprocessor_hook(job)({"status": "started", "postprocessor": "FFmpegExtractAudio"})
	assert job.status == "processing"
	assert job.percent == 99.0
	assert "MP3" in job.phase

	job2 = DownloadJob(id="j4")
	_postprocessor_hook(job2)({"status": "processing", "postprocessor": "Merger"})
	assert "udio" in job2.phase  # "Juntando vídeo e áudio"


# --------------------------- fluxo HTTP ---------------------------


class FakeService:
	"""download_file cria um arquivo real rápido; sem rede, sem hooks."""

	mode = "ok"  # "ok" | "toolarge" | "error"

	def __init__(self, platform, **kw):
		self.platform = platform

	async def download_file(self, url, fmt, quality, progress_hook=None, postprocessor_hook=None):
		if FakeService.mode == "toolarge":
			raise FileTooLargeError("O vídeo excede o limite de 1024 MB.")
		if FakeService.mode == "error":
			raise DownloadError("extractor tossed")
		ext = "mp3" if fmt == OutputFormat.MP3 else "mp4"
		p = settings.DOWNLOAD_DIR / f"job_{uuid.uuid4().hex}.{ext}"
		p.write_bytes(b"JOBDATA-" * 512)
		mt = "audio/mpeg" if ext == "mp3" else "video/mp4"
		return p, f"Video.{ext}", mt


@pytest.fixture(autouse=True)
def _patch(monkeypatch):
	FakeService.mode = "ok"
	jobs._JOBS.clear()
	monkeypatch.setattr("app.services.jobs.get_service", lambda platform: FakeService(platform))
	yield
	jobs._JOBS.clear()


def _wait(client, job_id, timeout=5.0):
	deadline = time.time() + timeout
	while time.time() < deadline:
		s = client.get(f"/api/v1/downloads/{job_id}").json()
		if s["status"] in ("finished", "error"):
			return s
		time.sleep(0.05)
	raise AssertionError(f"job não finalizou: {s}")


def test_start_then_poll_then_download(client):
	r = client.post(
		"/api/v1/twitter/download/start",
		json={"url": "https://x.com/a/status/1", "format": "mp4"},
	)
	assert r.status_code == 200
	job_id = r.json()["job_id"]

	s = _wait(client, job_id)
	assert s["status"] == "finished"
	assert s["percent"] == 100.0

	f = client.get(f"/api/v1/downloads/{job_id}/file")
	assert f.status_code == 200
	assert f.headers["content-type"].startswith("video/mp4")
	assert "attachment" in f.headers["content-disposition"]
	assert f.content.startswith(b"JOBDATA-")

	# job descartado + arquivo removido
	assert client.get(f"/api/v1/downloads/{job_id}").status_code == 404
	assert list(settings.DOWNLOAD_DIR.glob("job_*")) == []


def test_start_rejects_cross_platform_url(client):
	r = client.post(
		"/api/v1/youtube/download/start",
		json={"url": "https://x.com/a/status/1", "format": "mp4"},
	)
	assert r.status_code == 400


def test_unknown_job_is_404(client):
	missing = "0" * 32
	assert client.get(f"/api/v1/downloads/{missing}").status_code == 404
	assert client.get(f"/api/v1/downloads/{missing}/file").status_code == 404


def test_error_job_reports_status_and_file_502(client):
	FakeService.mode = "error"
	job_id = client.post(
		"/api/v1/twitter/download/start",
		json={"url": "https://x.com/a/status/1", "format": "mp4"},
	).json()["job_id"]

	s = _wait(client, job_id)
	assert s["status"] == "error"
	assert "tossed" in s["detail"]

	f = client.get(f"/api/v1/downloads/{job_id}/file")
	assert f.status_code == 502


def test_toolarge_job_file_returns_413(client):
	FakeService.mode = "toolarge"
	job_id = client.post(
		"/api/v1/twitter/download/start",
		json={"url": "https://x.com/a/status/1", "format": "mp4"},
	).json()["job_id"]

	s = _wait(client, job_id)
	assert s["status"] == "error"

	f = client.get(f"/api/v1/downloads/{job_id}/file")
	assert f.status_code == 413
