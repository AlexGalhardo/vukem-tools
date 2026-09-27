"""Registro único das plataformas suportadas.

Para adicionar uma plataforma basta uma entrada em ``PLATFORMS``: as rotas
(``routers/platforms.py``), a validação de URL e a allowlist de extractors do yt-dlp
derivam daqui.
"""

from dataclasses import dataclass
from typing import Literal
from urllib.parse import urlparse

PlatformName = Literal["youtube", "instagram", "twitter", "tiktok"]


class InvalidPlatformURLError(ValueError):
	"""A URL não pertence à plataforma esperada."""


@dataclass(frozen=True)
class Platform:
	name: PlatformName
	label: str
	domains: frozenset[str]
	# Regexes de nomes de extractor do yt-dlp (``allowed_extractors``). Impede que uma URL
	# que redireciona para fora da plataforma caia no extractor "generic" (SSRF).
	extractors: tuple[str, ...]


PLATFORMS: dict[str, Platform] = {
	"youtube": Platform(
		name="youtube",
		label="YouTube",
		domains=frozenset(
			{
				"youtube.com",
				"www.youtube.com",
				"m.youtube.com",
				"music.youtube.com",
				"youtu.be",
				"www.youtu.be",
			}
		),
		extractors=(r"youtube(:.*)?", r"youtubeytbe"),
	),
	"instagram": Platform(
		name="instagram",
		label="Instagram",
		domains=frozenset({"instagram.com", "www.instagram.com"}),
		extractors=(r"instagram.*",),
	),
	"twitter": Platform(
		name="twitter",
		label="X/Twitter",
		domains=frozenset(
			{"twitter.com", "www.twitter.com", "x.com", "www.x.com", "mobile.twitter.com", "mobile.x.com"}
		),
		extractors=(r"twitter(:.*)?",),
	),
	"tiktok": Platform(
		name="tiktok",
		label="TikTok",
		domains=frozenset({"tiktok.com", "www.tiktok.com", "m.tiktok.com", "vm.tiktok.com", "vt.tiktok.com"}),
		extractors=(r"tiktok(:.*)?", r"vm\.tiktok"),
	),
}


def _host(url: str) -> str:
	parsed = urlparse(str(url))
	if parsed.scheme not in ("http", "https"):
		return ""
	return (parsed.hostname or "").lower()


def detect_platform(url: str) -> PlatformName | None:
	host = _host(url)
	return next((p.name for p in PLATFORMS.values() if host in p.domains), None)


def validate_platform_url(platform: str, url: str) -> None:
	spec = PLATFORMS[platform]
	if _host(url) not in spec.domains:
		raise InvalidPlatformURLError(
			f"A URL não parece ser do {spec.label}. Domínios aceitos: {', '.join(sorted(spec.domains))}."
		)
