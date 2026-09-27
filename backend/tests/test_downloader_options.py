import time

import pytest

from app.errors import DownloadFailedError, YoutubeBotCheckError
from app.services.downloader import VideoDownloadService


def _service(platform: str = "youtube", cookies_file: str | None = None) -> VideoDownloadService:
	return VideoDownloadService(platform=platform, cookies_file=cookies_file)


def test_pot_provider_base_url_is_a_list():
	# yt-dlp lê extractor_args como lista; uma string virava lista de caracteres ("h").
	opts = _service()._base_opts(None)
	assert opts["extractor_args"]["youtubepot-bgutilhttp"] == {"base_url": ["http://127.0.0.1:4416"]}


def test_youtube_uses_default_player_clients_and_js_runtime():
	opts = _service()._base_opts(None)
	assert "player_client" not in opts["extractor_args"]["youtube"]
	assert opts["js_runtimes"] == {"node": {}}
	assert opts["force_ipv4"] is True


def test_youtube_proxy_only_applies_to_youtube(monkeypatch):
	monkeypatch.setattr("app.services.downloader.settings.YOUTUBE_PROXY", "socks5h://127.0.0.1:1080")
	assert _service("youtube")._base_opts(None)["proxy"] == "socks5h://127.0.0.1:1080"
	assert "proxy" not in _service("instagram")._base_opts(None)
	assert "youtubepot-bgutilhttp" not in _service("tiktok")._base_opts(None)["extractor_args"]


def test_allowed_extractors_restricted_per_platform():
	assert _service("twitter")._base_opts(None)["allowed_extractors"] == [r"twitter(:.*)?"]


def test_youtube_tries_without_cookies_then_with_configured_cookies():
	attempts: list[str | None] = []

	def action(opts):
		attempts.append(opts.get("cookiefile"))
		if len(attempts) == 1:
			raise YoutubeBotCheckError()
		return "OK"

	assert _service(cookies_file="/c.txt")._run(action) == "OK"
	assert attempts == [None, "/c.txt"]


def test_non_bot_check_errors_do_not_retry():
	calls = 0

	def action(opts):
		nonlocal calls
		calls += 1
		raise DownloadFailedError("vídeo privado")

	with pytest.raises(DownloadFailedError):
		_service(cookies_file="/c.txt")._run(action)
	assert calls == 1


def test_other_platforms_use_cookies_directly():
	seen: list[str | None] = []
	_service("instagram", "/ig.txt")._run(lambda opts: seen.append(opts.get("cookiefile")))
	assert seen == ["/ig.txt"]


def test_bot_check_error_mentions_expired_cookies(tmp_path):
	cookies = tmp_path / "c.txt"
	cookies.write_text(f"#HttpOnly_.youtube.com\tTRUE\t/\tTRUE\t{int(time.time()) - 60}\tSID\tx\n")

	def action(opts):
		raise YoutubeBotCheckError()

	with pytest.raises(YoutubeBotCheckError) as exc:
		_service(cookies_file=str(cookies))._run(action)
	assert "expiraram" in exc.value.message
