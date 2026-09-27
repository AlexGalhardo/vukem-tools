"""Normalização de nomes de arquivo entregues ao cliente."""

import re
import unicodedata


def sanitize_filename(name: str, max_length: int = 150) -> str:
	"""Remove caracteres perigosos (path traversal, separadores) mantendo letras Unicode."""
	name = re.sub(r"[^\w\s.\-]", "_", name, flags=re.UNICODE)
	name = re.sub(r"_+", "_", name.strip().replace(" ", "_")).strip("._") or "download"
	return name[:max_length]


def uppercase_ascii_slug(name: str, max_length: int = 150) -> str:
	"""``"Vídeo: Título!"`` -> ``"VIDEO_TITULO"`` (padrão ``VIDEO_<SLUG>.mp4`` / ``AUDIO_<SLUG>.mp3``)."""
	ascii_name = unicodedata.normalize("NFKD", name).encode("ascii", "ignore").decode("ascii")
	slug = re.sub(r"[^A-Za-z0-9]+", "_", ascii_name).strip("_").upper()
	return (slug or "DOWNLOAD")[:max_length]
