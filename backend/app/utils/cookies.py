"""Resolve o arquivo de cookies (formato Netscape) que o yt-dlp usa por plataforma.

Duas formas de configuração (ver ``app/config.py``):

* ``<PLATAFORMA>_COOKIES_FILE`` — caminho de um arquivo existente;
* ``<PLATAFORMA>_COOKIES`` — o conteúdo do ``cookies.txt`` na própria variável (base64 se
  ``COOKIES_FROM_BASE64=true``), materializado em ``COOKIES_DIR/<plataforma>_cookies.txt``.

Cookies são credenciais: nunca versione nem logue o conteúdo.
"""

import base64
import contextlib
import logging
from datetime import UTC, datetime
from pathlib import Path

from app.config import settings

logger = logging.getLogger("video-api")

_NETSCAPE_HEADER = "# Netscape HTTP Cookie File\n"


def _decode_inline(raw: str) -> str:
	raw = raw.strip()
	if settings.COOKIES_FROM_BASE64:
		raw = base64.b64decode(raw).decode("utf-8")
	# Painéis web costumam transformar quebras de linha em "\n" literais.
	if "\\n" in raw and "\n" not in raw:
		raw = raw.replace("\\n", "\n")
	if not raw.startswith(("# Netscape", "# HTTP Cookie File")):
		raw = _NETSCAPE_HEADER + raw
	return raw if raw.endswith("\n") else raw + "\n"


def _materialize(platform: str, raw: str) -> str:
	target = settings.COOKIES_DIR / f"{platform}_cookies.txt"
	content = _decode_inline(raw)
	if not target.exists() or target.read_text(encoding="utf-8") != content:
		settings.COOKIES_DIR.mkdir(parents=True, exist_ok=True)
		target.write_text(content, encoding="utf-8")
		with contextlib.suppress(OSError):  # chmod não existe no Windows
			target.chmod(0o600)
	return str(target)


def resolve_cookies_file(platform: str) -> str | None:
	"""Caminho de um cookies.txt utilizável para ``platform``, ou ``None``."""
	prefix = platform.upper()
	configured_path = getattr(settings, f"{prefix}_COOKIES_FILE", None)
	if configured_path:
		if Path(configured_path).is_file():
			return str(configured_path)
		logger.warning("%s_COOKIES_FILE aponta para um arquivo inexistente; ignorando.", prefix)

	inline = getattr(settings, f"{prefix}_COOKIES", None)
	if inline and inline.strip():
		try:
			return _materialize(platform, inline)
		except (ValueError, OSError) as exc:
			logger.error("Cookies inline de %s inválidos: %s", platform, type(exc).__name__)
	return None


def cookies_min_expiry(cookies_file: str) -> datetime | None:
	"""Menor expiração entre os cookies com prazo (``expiry > 0``); ``None`` se não houver."""
	try:
		lines = Path(cookies_file).read_text(encoding="utf-8").splitlines()
	except OSError:
		return None

	expiries: list[int] = []
	for line in lines:
		fields = line.split("\t")
		# "#HttpOnly_<domínio>" é um cookie de verdade, não um comentário.
		is_comment = line.startswith("#") and not line.startswith("#HttpOnly_")
		if is_comment or len(fields) != 7:
			continue
		with contextlib.suppress(ValueError):
			if (expiry := int(fields[4])) > 0:
				expiries.append(expiry)
	return datetime.fromtimestamp(min(expiries), tz=UTC) if expiries else None
