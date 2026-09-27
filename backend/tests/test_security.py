import uuid

import pytest

from app.config import settings
from app.services import jobs
from app.utils.streaming import content_disposition
from tests.conftest import requires_ffmpeg


def test_security_headers_present(client):
	r = client.get("/health")
	assert r.headers["x-content-type-options"] == "nosniff"
	assert r.headers["x-frame-options"] == "DENY"


def test_api_key_required_when_configured(client, monkeypatch):
	monkeypatch.setattr(settings, "API_KEY", "s3cret")
	params = {"url": "https://youtu.be/a"}
	assert client.get("/api/v1/detect", params=params).status_code == 401
	assert client.get("/api/v1/detect", params=params, headers={"X-API-Key": "wrong"}).status_code == 401
	assert client.get("/api/v1/detect", params=params, headers={"X-API-Key": "s3cret"}).status_code == 200
	assert client.get("/health").status_code == 200


def test_job_id_must_be_hex(client):
	assert client.get("/api/v1/downloads/not-a-job").status_code == 422
	assert client.get(f"/api/v1/downloads/{uuid.uuid4().hex}").status_code == 404


def test_validation_errors_use_structured_body(client):
	r = client.post("/api/v1/youtube/info", json={"url": "nope"})
	assert r.status_code == 422
	assert r.json()["error_type"] == "INVALID_REQUEST"


def test_unicode_download_name_is_header_safe():
	header = content_disposition("日本語 vídeo.mp4")
	header.encode("latin-1")
	assert "filename*=UTF-8''" in header


def test_concurrent_job_limit(client, monkeypatch):
	monkeypatch.setattr(settings, "MAX_CONCURRENT_JOBS", 1)
	monkeypatch.setitem(jobs._JOBS, "busy", jobs.DownloadJob(id="busy", status="downloading"))
	r = client.post("/api/v1/youtube/download/start", json={"url": "https://youtu.be/abc"})
	assert r.status_code == 503
	assert r.json()["error_type"] == "SERVER_BUSY"


@requires_ffmpeg
def test_hls_playlist_disguised_as_mp4_is_rejected(client):
	playlist = b"#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXTINF:1,\nfile:///etc/passwd\n#EXT-X-ENDLIST\n"
	r = client.post("/api/v1/edit/cut-crop", files={"file": ("evil.mp4", playlist, "video/mp4")})
	assert r.status_code == 400
	assert r.json()["error_type"] == "INVALID_MEDIA"


@pytest.mark.parametrize("segment_seconds", [0, 1, 601])
def test_split_segment_bounds(client, segment_seconds):
	r = client.post(
		"/api/v1/edit/split",
		files={"file": ("a.mp4", b"x", "video/mp4")},
		data={"segment_seconds": str(segment_seconds)},
	)
	assert r.status_code == 422
