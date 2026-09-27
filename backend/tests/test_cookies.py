import base64
from pathlib import Path

from app.config import settings
from app.utils.cookies import cookies_min_expiry, resolve_cookies_file


def test_inline_base64_cookies_are_materialized(monkeypatch):
	raw = ".instagram.com\tTRUE\t/\tTRUE\t0\tsessionid\tabc"
	monkeypatch.setattr(settings, "INSTAGRAM_COOKIES_FILE", None)
	monkeypatch.setattr(settings, "INSTAGRAM_COOKIES", base64.b64encode(raw.encode()).decode())
	monkeypatch.setattr(settings, "COOKIES_FROM_BASE64", True)
	content = Path(resolve_cookies_file("instagram")).read_text(encoding="utf-8")
	assert content.startswith("# Netscape HTTP Cookie File\n")
	assert "sessionid" in content


def test_missing_cookie_file_is_ignored(monkeypatch):
	monkeypatch.setattr(settings, "TIKTOK_COOKIES_FILE", "/nao/existe.txt")
	monkeypatch.setattr(settings, "TIKTOK_COOKIES", None)
	assert resolve_cookies_file("tiktok") is None


def test_min_expiry_counts_httponly_cookies(tmp_path):
	f = tmp_path / "c.txt"
	f.write_text(
		"# Netscape HTTP Cookie File\n"
		"#HttpOnly_.youtube.com\tTRUE\t/\tTRUE\t1000\tHSID\tx\n"
		".youtube.com\tTRUE\t/\tTRUE\t2000\tSID\ty\n"
		".youtube.com\tTRUE\t/\tTRUE\t0\tPREF\tz\n"
	)
	assert cookies_min_expiry(str(f)).timestamp() == 1000
