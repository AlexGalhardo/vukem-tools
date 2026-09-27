import pytest

from app.api_client import VideoInfo
from app.config import sanitize_deploy_label
from app.errors import ERROR_COPY, resolve_error_message
from app.presenters import (
	clip_caption,
	deploy_footer,
	format_duration,
	progress_text,
	status_text,
	video_caption,
)

BACKEND_ERROR_TYPES = {
	"TIMEOUT",
	"VIDEO_PROCESSING_FAILED",
	"FILE_TOO_LARGE",
	"OUT_OF_MEMORY",
	"NO_DISK_SPACE",
	"NETWORK_ERROR",
	"DOWNLOAD_FAILED",
	"YOUTUBE_BOT_CHECK",
	"INVALID_MEDIA",
	"INVALID_REQUEST",
	"UNAUTHORIZED",
	"NOT_FOUND",
	"CONFLICT",
	"SERVER_BUSY",
	"INTERNAL_ERROR",
}


def test_every_backend_error_type_has_copy():
	assert set(ERROR_COPY) == BACKEND_ERROR_TYPES


@pytest.mark.parametrize(
	"seconds,expected", [(None, "desconhecida"), (0, "desconhecida"), (65, "1m05s"), (3725, "1h02m05s")]
)
def test_format_duration(seconds, expected):
	assert format_duration(seconds) == expected


def test_caption_escapes_html_from_platform_text():
	info = VideoInfo(
		title="<b>x</b> & _*[",
		uploader=None,
		duration_seconds=10,
		thumbnail=None,
		estimated_filesize_mb=None,
		exceeds_size_limit=True,
		max_file_size_mb=1024,
	)
	caption = video_caption("youtube", info)
	assert "&lt;b&gt;x&lt;/b&gt; &amp; _*[" in caption
	assert "desconhecido" in caption
	assert "1024 MB" in caption


def test_resolve_error_prefers_curated_copy_then_detail():
	assert resolve_error_message("SERVER_BUSY", "x") == ERROR_COPY["SERVER_BUSY"]
	assert resolve_error_message("UNKNOWN", "detalhe") == "❌ detalhe"
	assert resolve_error_message(None, None).startswith("❌")


def test_bot_check_surfaces_expired_cookies_detail():
	message = resolve_error_message("YOUTUBE_BOT_CHECK", "Os cookies expiraram em 01/01/2026")
	assert "expiraram" in message


def test_progress_and_clip_texts():
	assert progress_text("processing", 42.4, "Merge") == "⏳ Processando vídeo 42%... (Merge)"
	assert clip_caption(2, 5) == "🎬 Parte 2/5"


def test_deploy_footer_and_status_escape_labels():
	assert deploy_footer("VPS Hostinger", "Koyeb (fra)") == "🛰️ Bot: VPS Hostinger · API: Koyeb (fra)"
	assert "desconhecida" in deploy_footer("local", None)
	assert "&lt;b&gt;" in status_text("local", "<b>")
	assert "fora do ar" in status_text("local", None)


def test_sanitize_deploy_label():
	assert sanitize_deploy_label("VPS\r\nHostinger<script>") == "VPSHostingerscript"
