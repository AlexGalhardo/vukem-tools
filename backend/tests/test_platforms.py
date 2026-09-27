import pytest
import yt_dlp

from app.platforms import PLATFORMS, InvalidPlatformURLError, detect_platform, validate_platform_url
from app.utils.filenames import sanitize_filename, uppercase_ascii_slug


@pytest.mark.parametrize(
	"url,expected",
	[
		("https://www.youtube.com/watch?v=abc", "youtube"),
		("https://youtu.be/abc", "youtube"),
		("https://music.youtube.com/watch?v=abc", "youtube"),
		("https://www.instagram.com/reel/abc/", "instagram"),
		("https://x.com/a/status/1", "twitter"),
		("https://mobile.twitter.com/a/status/1", "twitter"),
		("https://www.tiktok.com/@a/video/1", "tiktok"),
		("https://vm.tiktok.com/ZMabc/", "tiktok"),
		("https://vimeo.com/12345", None),
		("not-a-url", None),
		# userinfo/porta/esquema não podem enganar a detecção por host
		("https://youtube.com@evil.example/watch?v=1", None),
		("https://YOUTUBE.com:443/watch?v=1", "youtube"),
		("ftp://youtube.com/watch?v=1", None),
		("file:///etc/passwd", None),
	],
)
def test_detect_platform(url, expected):
	assert detect_platform(url) == expected


def test_validate_rejects_cross_platform_url_without_echoing_it():
	with pytest.raises(InvalidPlatformURLError) as exc:
		validate_platform_url("youtube", "https://x.com/<script>")
	assert "<script>" not in str(exc.value)


def test_validate_accepts_own_domain():
	validate_platform_url("instagram", "https://instagram.com/p/abc/")


@pytest.mark.parametrize("platform", list(PLATFORMS))
def test_extractor_allowlist_matches_real_yt_dlp_extractors(platform):
	with yt_dlp.YoutubeDL({"quiet": True, "allowed_extractors": list(PLATFORMS[platform].extractors)}) as ydl:
		names = {name.lower() for name in ydl._ies}
	assert names, f"nenhum extractor casou para {platform}"
	assert "generic" not in names


@pytest.mark.parametrize(
	"raw,expected",
	[
		("Meu Vídeo Incrível!", "Meu_Vídeo_Incrível"),
		("a/b\\c:d*e?", "a_b_c_d_e"),
		("../../etc/passwd", "etc_passwd"),
		("", "download"),
	],
)
def test_sanitize_filename(raw, expected):
	assert sanitize_filename(raw) == expected


@pytest.mark.parametrize(
	"raw,expected",
	[("Meu Vídeo Incrível!", "MEU_VIDEO_INCRIVEL"), ("Ção & Emoção", "CAO_EMOCAO"), ("///", "DOWNLOAD")],
)
def test_uppercase_ascii_slug(raw, expected):
	assert uppercase_ascii_slug(raw) == expected
