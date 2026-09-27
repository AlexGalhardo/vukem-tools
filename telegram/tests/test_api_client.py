import json

import httpx
import pytest

from app.api_client import BackendClient, BackendError, filename_from_content_disposition


def _client(handler) -> BackendClient:
	client = BackendClient("http://backend", api_key="k3y")
	client._client = httpx.AsyncClient(
		base_url="http://backend", transport=httpx.MockTransport(handler), headers={"X-API-Key": "k3y"}
	)
	return client


async def test_sends_api_key_and_parses_job_with_clips():
	seen: dict[str, str] = {}

	def handler(request: httpx.Request) -> httpx.Response:
		seen["key"] = request.headers["x-api-key"]
		body = {
			"job_id": "a" * 32,
			"status": "finished",
			"percent": 100,
			"phase": "Concluído",
			"total_bytes": 10,
			"clips": [{"index": 1, "filename": "p1.mp4", "size_mb": 1.5}, {"index": 2, "filename": "p2.mp4"}],
		}
		return httpx.Response(200, json=body)

	job = await _client(handler).get_job_status("a" * 32)
	assert seen["key"] == "k3y"
	assert [c.index for c in job.clips] == [1, 2]
	assert job.clips[1].size_mb is None


async def test_structured_backend_error_is_raised():
	def handler(request: httpx.Request) -> httpx.Response:
		return httpx.Response(503, json={"detail": "ocupado", "error_type": "SERVER_BUSY"})

	with pytest.raises(BackendError) as exc:
		await _client(handler).start_job("youtube", "https://youtu.be/a", "mp4", "full")
	assert exc.value.error_type == "SERVER_BUSY"


async def test_non_json_error_falls_back_to_status():
	def handler(request: httpx.Request) -> httpx.Response:
		return httpx.Response(502, text="<html>bad gateway</html>")

	with pytest.raises(BackendError) as exc:
		await _client(handler).detect_platform("https://youtu.be/a")
	assert "502" in exc.value.detail


async def test_download_clip_uses_clip_endpoint_and_filename():
	def handler(request: httpx.Request) -> httpx.Response:
		assert request.url.path == f"/api/v1/downloads/{'b' * 32}/clips/3/file"
		headers = {
			"content-type": "video/mp4",
			"content-disposition": "attachment; filename=\"VIDEO_parte3.mp4\"; filename*=UTF-8''V%C3%ADdeo_parte3.mp4",
		}
		return httpx.Response(200, content=b"data", headers=headers)

	file = await _client(handler).download_clip("b" * 32, 3)
	assert file.filename == "Vídeo_parte3.mp4"
	assert file.media_type == "video/mp4"
	assert file.content == b"data"


@pytest.mark.parametrize(
	"header,expected",
	[
		(None, None),
		('attachment; filename="a.mp4"', "a.mp4"),
		("attachment; filename=b.mp3", "b.mp3"),
	],
)
def test_filename_from_content_disposition(header, expected):
	assert filename_from_content_disposition(header) == expected


async def test_info_request_body_is_minimal():
	captured: list[dict] = []

	def handler(request: httpx.Request) -> httpx.Response:
		captured.append(json.loads(request.content))
		return httpx.Response(200, json={"title": "t", "max_file_size_mb": 1024})

	info = await _client(handler).get_info("tiktok", "https://www.tiktok.com/@a/video/1")
	assert captured == [{"url": "https://www.tiktok.com/@a/video/1"}]
	assert info.title == "t"
