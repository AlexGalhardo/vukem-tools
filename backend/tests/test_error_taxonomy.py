"""
Testes da taxonomia de erros estruturados (Tarefa 3).

Para cada cenário exigido no enunciado forçamos o erro real e conferimos que a
resposta traz ``error_type`` estável + status HTTP correto:

  * TIMEOUT                -> processamento estourou o tempo limite
  * VIDEO_PROCESSING_FAILED-> ffmpeg retornou != 0 (sem causa específica)
  * FILE_TOO_LARGE         -> vídeo/upload acima do limite
  * OUT_OF_MEMORY          -> ffmpeg morto por SIGKILL / OOM killer
  * NO_DISK_SPACE          -> "No space left on device" / ENOSPC
  * NETWORK_ERROR          -> falha de rede/DNS ao baixar da origem
"""

import errno
import json
import subprocess

import pytest

from app.errors import (
	DownloadFailedError,
	MediaTooLargeError,
	NetworkError,
	NoDiskSpaceError,
	OutOfMemoryError,
	ProcessingTimeoutError,
	VideoProcessingError,
	classify_ffmpeg_failure,
	classify_oserror,
	classify_ytdlp_error,
)
from app.services import editor

# --------------------------- classificadores (unitário) ---------------------------


@pytest.mark.parametrize(
	"returncode,stderr,expected",
	[
		(137, "frame=  10 ...\nKilled", OutOfMemoryError),
		(-9, "", OutOfMemoryError),
		(1, "x264: cannot allocate memory", OutOfMemoryError),
		(1, "av_interleaved_write_frame(): No space left on device", NoDiskSpaceError),
		(1, "Error: disk is full", NoDiskSpaceError),
		(1, "Invalid data found when processing input", VideoProcessingError),
		(1, "", VideoProcessingError),
	],
)
def test_classify_ffmpeg_failure(returncode, stderr, expected):
	err = classify_ffmpeg_failure(returncode, stderr)
	assert isinstance(err, expected)
	assert err.error_type == expected.error_type
	assert err.status_code == expected.status_code


@pytest.mark.parametrize(
	"message,expected",
	[
		(
			"ERROR: unable to download video data: <urlopen error [Errno -2] Name or service not known>",
			NetworkError,
		),
		("ERROR: Unable to download webpage: The read operation timed out", NetworkError),
		("ERROR: unable to download video data: HTTP Error 503: Service Unavailable", NetworkError),
		("ERROR: requested format is larger than max-filesize", MediaTooLargeError),
		("ERROR: Video unavailable. This video is private", DownloadFailedError),
	],
)
def test_classify_ytdlp_error(message, expected):
	err = classify_ytdlp_error(message)
	assert isinstance(err, expected)
	assert err.error_type == expected.error_type


def test_classify_oserror_enospc_and_enomem():
	assert isinstance(classify_oserror(OSError(errno.ENOSPC, "No space left on device")), NoDiskSpaceError)
	assert isinstance(classify_oserror(OSError(errno.ENOMEM, "Cannot allocate memory")), OutOfMemoryError)
	assert classify_oserror(OSError(errno.EACCES, "Permission denied")) is None


def test_timeout_maps_to_504():
	err = ProcessingTimeoutError()
	assert err.status_code == 504 and err.error_type == "TIMEOUT"


# --------------------------- endpoint /info: NETWORK_ERROR ---------------------------


def test_info_network_error_has_structured_type(client, monkeypatch):
	class NetDownService:
		def __init__(self, platform, **kw):
			self.platform = platform

		async def extract_info(self, url):
			raise NetworkError()

	monkeypatch.setattr("app.routers.platforms.get_service", lambda platform: NetDownService(platform))
	r = client.post(
		"/api/v1/youtube/info",
		json={"url": "https://www.youtube.com/watch?v=abc", "format": "mp4"},
	)
	assert r.status_code == 502
	body = r.json()
	assert body["error_type"] == "NETWORK_ERROR"
	assert "rede" in body["detail"].lower()


# --------------------------- job de download: OUT_OF_MEMORY ---------------------------


def test_download_job_out_of_memory_propagates_type(client, monkeypatch):
	class OOMService:
		def __init__(self, platform, **kw):
			self.platform = platform

		async def download_file(self, *a, **kw):
			raise OutOfMemoryError()

	monkeypatch.setattr("app.services.jobs.get_service", lambda platform: OOMService(platform))
	job_id = client.post(
		"/api/v1/twitter/download/start",
		json={"url": "https://x.com/a/status/1", "format": "mp4"},
	).json()["job_id"]

	import time

	for _ in range(100):
		s = client.get(f"/api/v1/downloads/{job_id}").json()
		if s["status"] in ("finished", "error"):
			break
		time.sleep(0.05)
	assert s["status"] == "error"
	assert s["error_type"] == "OUT_OF_MEMORY"

	f = client.get(f"/api/v1/downloads/{job_id}/file")
	assert f.status_code == 503
	assert f.json()["error_type"] == "OUT_OF_MEMORY"


# --------------------------- cut-crop: ffmpeg morto por OOM / disco cheio ---------------------------


def _fake_run_factory(fail_code: int, fail_stderr: str):
	async def fake_run(cmd, timeout=600.0):
		exe = str(cmd[0])
		if exe.endswith("ffprobe") or "ffprobe" in exe:
			payload = {
				"format": {"duration": "5.0", "format_name": "mov,mp4,m4a,3gp,3g2,mj2"},
				"streams": [{"codec_type": "video", "width": 320, "height": 240}],
			}
			return (0, json.dumps(payload).encode(), b"")
		return (fail_code, b"", fail_stderr.encode())

	return fake_run


@pytest.mark.parametrize(
	"code,stderr,http_status,error_type",
	[
		(137, "Killed", 503, "OUT_OF_MEMORY"),
		(1, "Output #0 ... av_interleaved_write_frame(): No space left on device", 507, "NO_DISK_SPACE"),
		(1, "Conversion failed!", 502, "VIDEO_PROCESSING_FAILED"),
	],
)
def test_cut_crop_ffmpeg_failure_maps_to_structured_error(client, monkeypatch, code, stderr, http_status, error_type):
	monkeypatch.setattr(editor, "ffmpeg_available", lambda: True)
	monkeypatch.setattr("app.routers.edit.ffmpeg_available", lambda: True)
	monkeypatch.setattr(editor, "_run", _fake_run_factory(code, stderr))

	r = client.post(
		"/api/v1/edit/cut-crop",
		files={"file": ("v.mp4", b"not-a-real-mp4-but-probe-is-faked", "video/mp4")},
		data={"start": "0", "end": "3", "output_format": "mp4"},
	)
	assert r.status_code == http_status, r.text
	assert r.json()["error_type"] == error_type


def test_run_sync_translates_timeout_expired():
	"""A tradução TimeoutExpired -> ProcessingTimeoutError acontece em _run_sync."""

	def boom(*a, **kw):
		raise subprocess.TimeoutExpired(cmd=["ffmpeg"], timeout=1)

	import app.services.editor as ed

	orig = ed.subprocess.run
	ed.subprocess.run = boom
	try:
		with pytest.raises(ProcessingTimeoutError) as ei:
			ed._run_sync(["ffmpeg", "-i", "x"], 1.0)
		assert ei.value.status_code == 504
		assert ei.value.error_type == "TIMEOUT"
	finally:
		ed.subprocess.run = orig


def test_cut_crop_timeout_maps_to_504(client, monkeypatch):
	monkeypatch.setattr(editor, "ffmpeg_available", lambda: True)
	monkeypatch.setattr("app.routers.edit.ffmpeg_available", lambda: True)

	def boom(*a, **kw):
		raise subprocess.TimeoutExpired(cmd=["ffmpeg"], timeout=600)

	monkeypatch.setattr(editor.subprocess, "run", boom)
	r = client.post(
		"/api/v1/edit/cut-crop",
		files={"file": ("v.mp4", b"xxxx", "video/mp4")},
		data={"start": "0", "end": "3"},
	)
	assert r.status_code == 504
	assert r.json()["error_type"] == "TIMEOUT"


def test_upload_too_large_maps_to_413(client, monkeypatch):
	monkeypatch.setattr(editor, "ffmpeg_available", lambda: True)
	monkeypatch.setattr("app.routers.edit.ffmpeg_available", lambda: True)
	# 1 byte de limite -> qualquer upload estoura.
	monkeypatch.setattr("app.config.settings.MAX_UPLOAD_SIZE_MB", 0.0000009, raising=False)
	r = client.post(
		"/api/v1/edit/cut-crop",
		files={"file": ("v.mp4", b"0123456789" * 50, "video/mp4")},
		data={"start": "0"},
	)
	assert r.status_code == 413
	assert r.json()["error_type"] == "FILE_TOO_LARGE"
