"""Entrega arquivos grandes em streaming (blocos de ``STREAM_CHUNK_SIZE``) e limpa os temporários."""

import logging
from collections.abc import Callable, Iterator
from pathlib import Path
from urllib.parse import quote

from fastapi.responses import StreamingResponse
from starlette.background import BackgroundTask

from app.config import settings
from app.utils.filenames import sanitize_filename, uppercase_ascii_slug

logger = logging.getLogger("video-api")


def cleanup_files(*paths: Path | str | None) -> None:
	"""Remove arquivos temporários; nunca levanta exceção."""
	for path in paths:
		if not path:
			continue
		try:
			Path(path).unlink(missing_ok=True)
		except OSError as exc:
			logger.warning("Falha ao remover arquivo temporário %s: %s", path, exc)


def content_disposition(download_name: str) -> str:
	"""``attachment`` com fallback ASCII + ``filename*`` UTF-8 (RFC 6266/5987).

	Headers HTTP são latin-1: um nome Unicode cru derrubaria a resposta com 500.
	"""
	name = Path(download_name)
	safe = sanitize_filename(name.stem) + name.suffix
	ascii_fallback = uppercase_ascii_slug(name.stem) + name.suffix
	return f"attachment; filename=\"{ascii_fallback}\"; filename*=UTF-8''{quote(safe)}"


def _iter_file(path: Path, chunk_size: int) -> Iterator[bytes]:
	with path.open("rb") as fh:
		while chunk := fh.read(chunk_size):
			yield chunk


def stream_file_response(
	path: Path,
	*,
	download_name: str,
	media_type: str = "application/octet-stream",
	cleanup: bool = True,
	cleanup_extra: tuple[Path | str | None, ...] = (),
	after: Callable[[], None] | None = None,
) -> StreamingResponse:
	"""Transmite ``path``. Com ``cleanup`` apaga ``path`` + ``cleanup_extra`` ao final; depois chama ``after``."""
	path = Path(path)
	headers = {
		"Content-Length": str(path.stat().st_size),
		"Content-Disposition": content_disposition(download_name),
		"Cache-Control": "no-store",
	}

	def finalize() -> None:
		if cleanup:
			cleanup_files(path, *cleanup_extra)
		if after is not None:
			after()

	return StreamingResponse(
		_iter_file(path, settings.STREAM_CHUNK_SIZE),
		media_type=media_type,
		headers=headers,
		background=BackgroundTask(finalize),
	)
