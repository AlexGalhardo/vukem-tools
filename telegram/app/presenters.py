"""Textos exibidos no chat (funções puras, sem I/O)."""

from html import escape

from app.api_client import VideoInfo

PLATFORM_LABELS = {"youtube": "YouTube", "instagram": "Instagram", "twitter": "X (Twitter)", "tiktok": "TikTok"}

START_TEXT = (
	"👋 Envie um link do <b>YouTube</b>, <b>Instagram</b>, <b>X (Twitter)</b> ou <b>TikTok</b> "
	"que eu baixo pra você — vídeo (MP4) ou áudio (MP3), completo ou no padrão Stories (clipes de 60s).\n\n"
	"/status mostra em qual servidor o bot e a API estão rodando."
)


def format_duration(seconds: float | None) -> str:
	if not seconds:
		return "desconhecida"
	minutes, secs = divmod(int(seconds), 60)
	hours, minutes = divmod(minutes, 60)
	return f"{hours}h{minutes:02d}m{secs:02d}s" if hours else f"{minutes}m{secs:02d}s"


def video_caption(platform: str, info: VideoInfo) -> str:
	"""Legenda em HTML: título/uploader vêm da plataforma e são escapados."""
	lines = [
		f"✅ URL válida ({PLATFORM_LABELS.get(platform, platform)})",
		"",
		f"<b>{escape(info.title)}</b>",
		f"👤 {escape(info.uploader or 'desconhecido')}",
		f"⏱️ {format_duration(info.duration_seconds)}",
	]
	if info.estimated_filesize_mb:
		lines.append(f"📦 ~{info.estimated_filesize_mb:.1f} MB")
	if info.exceeds_size_limit:
		lines.append(f"⚠️ Acima do limite de {info.max_file_size_mb} MB — o download pode falhar.")
	return "\n".join(lines)


def progress_text(status: str, percent: float, phase: str) -> str:
	verb = "Processando" if status == "processing" else "Baixando"
	return f"⏳ {verb} vídeo {percent:.0f}%... ({phase})"


def clip_caption(index: int, total: int) -> str:
	return f"🎬 Parte {index}/{total}"


def deploy_footer(bot_deploy: str, api_deploy: str | None) -> str:
	"""Rodapé que identifica qual deploy (Koyeb, VPS...) atendeu.

	Escapado porque também entra em legendas com ``parse_mode="HTML"`` e o rótulo da API vem de um header.
	"""
	return f"🛰️ Bot: {escape(bot_deploy)} · API: {escape(api_deploy or 'desconhecida')}"


def with_footer(text: str, footer: str) -> str:
	return f"{text}\n\n{footer}"


def status_text(bot_deploy: str, api_deploy: str | None) -> str:
	api = escape(api_deploy) if api_deploy else "❌ fora do ar"
	return f"🛰️ <b>Status do deploy</b>\n\n🤖 Bot: {escape(bot_deploy)}\n🖥️ API: {api}"
