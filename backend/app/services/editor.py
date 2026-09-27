"""
editor.py
---------
Feature "Cut and Crop": corta um vídeo por intervalo de tempo e/ou recorta a
imagem (crop) por dimensões, aplicando o crop a todo o trecho cortado.

Toda a manipulação de mídia é feita chamando ``ffmpeg`` / ``ffprobe`` como
subprocessos. Rodamos o ``subprocess.run`` (bloqueante) dentro de
``asyncio.to_thread`` em vez de ``asyncio.create_subprocess_exec`` porque, no
Windows, o loop de evento que o uvicorn usa é o ``SelectorEventLoop``, que
**não** suporta subprocessos assíncronos (``NotImplementedError``). Rodar em
thread funciona igual em Windows, Linux e macOS. Nada é carregado inteiro em
memória: o ffmpeg lê o arquivo de entrada do disco e escreve o resultado direto
em outro arquivo, que depois é devolvido ao cliente em streaming.
"""

import asyncio
import json
import logging
import math
import shutil
import subprocess
import uuid
import zipfile
from dataclasses import dataclass
from pathlib import Path

from app.config import settings
from app.errors import (
	AppError,
	InvalidMediaError,
	InvalidRequestError,
	OutOfMemoryError,
	ProcessingTimeoutError,
	VideoProcessingError,
	classify_ffmpeg_failure,
)

logger = logging.getLogger("video-api")


class EditError(InvalidRequestError):
	"""Parâmetro de corte/crop inválido — HTTP 400, ``error_type`` INVALID_REQUEST.

	Falhas de execução do ffmpeg NÃO usam esta classe: são classificadas em
	``VideoProcessingError`` / ``OutOfMemoryError`` / ``NoDiskSpaceError`` /
	``ProcessingTimeoutError`` (ver ``app.errors.classify_ffmpeg_failure``)."""


@dataclass
class MediaInfo:
	duration: float
	width: int
	height: int


@dataclass
class CutCropParams:
	"""Parâmetros já validados de um pedido de corte + crop."""

	start: float
	end: float | None
	crop_x: int | None
	crop_y: int | None
	crop_w: int | None
	crop_h: int | None
	output_format: str  # "mp4" | "mp3"

	@property
	def has_crop(self) -> bool:
		return None not in (self.crop_x, self.crop_y, self.crop_w, self.crop_h)


# Um upload pode ser uma playlist HLS/concat disfarçada de .mp4 que faz o ffmpeg ler arquivos
# locais ou abrir URLs (LFI/SSRF). Só o protocolo "file" e demuxers de mídia comuns passam.
_INPUT_HARDENING = ["-protocol_whitelist", "file"]
_ALLOWED_DEMUXERS = {
    "mov", "mp4", "m4a", "3gp", "3g2", "mj2", "matroska", "webm", "avi", "flv", "mpegts",
    "mp3", "aac", "wav", "ogg", "flac", "asf", "mpeg",
}  # fmt: skip


def ffmpeg_available() -> bool:
	return bool(shutil.which(settings.FFMPEG_BIN) and shutil.which(settings.FFPROBE_BIN))


def _threads_args() -> list[str]:
	"""``-threads N`` quando ``FFMPEG_THREADS`` > 0 (limita o pico de RAM)."""
	n = settings.FFMPEG_THREADS
	return ["-threads", str(n)] if n and n > 0 else []


def _run_sync(cmd: list[str], timeout: float) -> tuple[int, bytes, bytes]:
	try:
		proc = subprocess.run(  # noqa: S603 - cmd é sempre uma lista montada por nós
			cmd,
			capture_output=True,
			timeout=timeout,
			check=False,
		)
	except FileNotFoundError as e:  # ffmpeg/ffprobe não encontrado
		raise VideoProcessingError(f"Binário de mídia não encontrado no servidor: {cmd[0]!r}.") from e
	except subprocess.TimeoutExpired as e:
		raise ProcessingTimeoutError(
			f"O processamento de mídia passou do tempo limite ({int(timeout)}s). "
			f"Tente um vídeo mais curto ou um trecho menor."
		) from e
	except MemoryError as e:
		raise OutOfMemoryError() from e
	return proc.returncode, proc.stdout or b"", proc.stderr or b""


async def _run(cmd: list[str], timeout: float = 600.0) -> tuple[int, bytes, bytes]:
	"""Roda ``cmd`` numa thread (ver docstring do módulo)."""
	return await asyncio.to_thread(_run_sync, cmd, timeout)


async def probe(path: Path) -> MediaInfo:
	"""Lê duração e dimensões do vídeo com ffprobe."""
	cmd = [
		settings.FFPROBE_BIN,
		"-v",
		"error",
		*_INPUT_HARDENING,
		"-print_format",
		"json",
		"-show_format",
		"-show_streams",
		str(path),
	]
	code, out, err = await _run(cmd)
	if code != 0:
		raise InvalidMediaError(
			"Não foi possível ler esse arquivo de vídeo (ffprobe falhou). "
			"Ele pode estar corrompido ou num formato não suportado."
		)

	data = json.loads(out or b"{}")
	demuxers = set((data.get("format", {}).get("format_name") or "").split(","))
	if not demuxers <= _ALLOWED_DEMUXERS:
		raise InvalidMediaError("Formato de arquivo não suportado. Envie um vídeo MP4, MOV, MKV, WEBM ou AVI.")
	streams = data.get("streams", [])
	video = next((s for s in streams if s.get("codec_type") == "video"), None)
	if video is None:
		raise InvalidMediaError("O arquivo enviado não contém uma faixa de vídeo.")

	try:
		duration = float(data.get("format", {}).get("duration") or video.get("duration") or 0.0)
	except (TypeError, ValueError):
		duration = 0.0

	width = int(video.get("width") or 0)
	height = int(video.get("height") or 0)
	if width <= 0 or height <= 0:
		raise EditError("Não foi possível determinar as dimensões do vídeo.")
	return MediaInfo(duration=duration, width=width, height=height)


def validate_params(
	media: MediaInfo,
	*,
	start: float | None,
	end: float | None,
	crop_x: int | None,
	crop_y: int | None,
	crop_w: int | None,
	crop_h: int | None,
	output_format: str,
) -> CutCropParams:
	"""
	Valida os parâmetros contra as dimensões/duração reais do vídeo e devolve
	um ``CutCropParams`` normalizado. Levanta ``EditError`` (HTTP 400) quando
	algo é inconsistente.
	"""
	output_format = (output_format or "mp4").lower()
	if output_format not in ("mp4", "mp3"):
		raise EditError("output_format deve ser 'mp4' ou 'mp3'.")

	start = 0.0 if start is None else float(start)
	if start < 0:
		raise EditError("O tempo inicial não pode ser negativo.")
	if media.duration and start >= media.duration:
		raise EditError(f"O tempo inicial ({start}s) é maior ou igual à duração do vídeo ({media.duration:.2f}s).")

	if end is not None:
		end = float(end)
		if end <= start:
			raise EditError("O tempo final deve ser maior que o tempo inicial.")
		if media.duration and end > media.duration + 0.5:
			raise EditError(f"O tempo final ({end}s) ultrapassa a duração do vídeo ({media.duration:.2f}s).")

	crop_values = (crop_x, crop_y, crop_w, crop_h)
	if any(v is not None for v in crop_values):
		if any(v is None for v in crop_values):
			raise EditError("Para recortar (crop) informe crop_x, crop_y, crop_w e crop_h juntos.")
		crop_x, crop_y, crop_w, crop_h = (int(v) for v in crop_values)
		if crop_x < 0 or crop_y < 0:
			raise EditError("crop_x e crop_y não podem ser negativos.")
		if crop_w <= 0 or crop_h <= 0:
			raise EditError("crop_w e crop_h devem ser positivos.")
		if crop_x + crop_w > media.width or crop_y + crop_h > media.height:
			raise EditError(
				f"A área de crop ({crop_x},{crop_y} {crop_w}x{crop_h}) sai dos limites "
				f"do vídeo ({media.width}x{media.height})."
			)
		# libx264 + yuv420p exige dimensões pares: arredonda para baixo.
		crop_w -= crop_w % 2
		crop_h -= crop_h % 2
		if crop_w <= 0 or crop_h <= 0:
			raise EditError("A área de crop é pequena demais (mínimo 2x2 px).")
	else:
		crop_x = crop_y = crop_w = crop_h = None

	return CutCropParams(
		start=start,
		end=end,
		crop_x=crop_x,
		crop_y=crop_y,
		crop_w=crop_w,
		crop_h=crop_h,
		output_format=output_format,
	)


def build_ffmpeg_command(src: Path, dst: Path, params: CutCropParams) -> list[str]:
	"""
	Monta a lista de argumentos do ffmpeg. Isolada para poder ser testada sem
	executar o ffmpeg de verdade.

	``-ss`` / ``-to`` entram como opções de entrada (mais rápido); como sempre
	há re-encode (crop e/ou conversão), o corte fica preciso o suficiente.
	"""
	cmd: list[str] = [settings.FFMPEG_BIN, "-y", "-hide_banner", "-loglevel", "error"]

	cmd += ["-ss", f"{params.start:.3f}"]
	if params.end is not None:
		cmd += ["-to", f"{params.end:.3f}"]

	cmd += [*_INPUT_HARDENING, "-i", str(src)]
	cmd += _threads_args()

	if params.output_format == "mp3":
		cmd += ["-vn", "-c:a", "libmp3lame", "-b:a", "192k"]
	else:
		if params.has_crop:
			cmd += ["-vf", f"crop={params.crop_w}:{params.crop_h}:{params.crop_x}:{params.crop_y}"]
		cmd += [
			"-c:v",
			"libx264",
			"-preset",
			"veryfast",
			"-crf",
			"20",
			"-c:a",
			"aac",
			"-b:a",
			"192k",
			"-movflags",
			"+faststart",
		]

	cmd.append(str(dst))
	return cmd


async def cut_and_crop(src: Path, params: CutCropParams) -> Path:
	"""Executa o ffmpeg e devolve o caminho do arquivo de saída."""
	if not ffmpeg_available():
		raise VideoProcessingError("ffmpeg/ffprobe não estão instalados ou disponíveis no servidor.")

	ext = "mp3" if params.output_format == "mp3" else "mp4"
	dst = settings.DOWNLOAD_DIR / f"edit_{uuid.uuid4().hex}.{ext}"
	cmd = build_ffmpeg_command(src, dst, params)
	logger.info("Executando ffmpeg: %s", " ".join(cmd))

	code, _out, err = await _run(cmd)
	if code != 0 or not dst.exists():
		dst.unlink(missing_ok=True)
		raise classify_ffmpeg_failure(code, err.decode(errors="ignore"))
	return dst


# ---------------------------------------------------------------------------
# "Clipes p/ Stories": divide um arquivo em blocos consecutivos de <= N segundos
# ---------------------------------------------------------------------------

SEGMENT_SECONDS = 60

# Tolerância documentada para a duração de cada clipe. O muxer ``segment`` corta
# no primeiro frame com ``t >= N*segment_seconds``; com o keyframe forçado
# exatamente nesse instante o clipe sai com ``segment_seconds`` +/- 1 frame
# (tipicamente < 0.05s a 24-60fps). A margem de 0.5s cobre o frame residual e o
# preenchimento (priming) do encoder de áudio AAC. Nenhum clipe pode exceder
# ``segment_seconds + MAX_CLIP_TOLERANCE_SECONDS``.
MAX_CLIP_TOLERANCE_SECONDS = 0.5

# Extensões tratadas como "somente áudio": não recebem o filtro de vídeo/keyframe.
_AUDIO_EXTS = {".mp3", ".m4a", ".aac", ".opus", ".ogg", ".oga", ".wav", ".flac"}


@dataclass
class SegmentInfo:
	index: int  # 1-based
	path: Path
	filename: str
	duration_seconds: float
	size_bytes: int


def expected_segment_count(duration: float, segment_seconds: int = SEGMENT_SECONDS) -> int:
	"""Quantos blocos um vídeo dessa duração deve gerar (último pode ser parcial)."""
	if duration <= 0:
		return 0
	return max(1, math.ceil(duration / segment_seconds - 1e-6))


def build_split_command(
	src: Path,
	pattern: str,
	segment_seconds: int = SEGMENT_SECONDS,
	*,
	is_audio: bool = False,
) -> list[str]:
	"""
	Monta o comando ffmpeg do fatiamento em clipes. Isolado para poder ser
	testado sem executar o ffmpeg.

	**Por que re-encodar em vez de ``-c copy``:** com *stream copy* o muxer
	``segment`` só consegue cortar em keyframes já existentes. Vídeos de
	YouTube/Instagram/X/TikTok costumam ter keyframes a cada 2-10s (às vezes
	mais), então cada corte "escorrega" para frente até o próximo keyframe e o
	clipe estoura os 60s — era a causa dos clipes de ~63s. Aqui:

	* ``-force_key_frames expr:gte(t,n_forced*N)`` põe um keyframe exatamente em
	  cada múltiplo de ``segment_seconds``;
	* ``-x264-params scenecut=0`` (+ ``-sc_threshold 0``) desliga os keyframes de
	  corte de cena — sem isso o libx264 insere keyframes em posições
	  arbitrárias e o muxer, por causa da folga ``-segment_time_delta``, pode
	  cortar num deles um pouco antes de 60s, jogando a sobra para o clipe
	  seguinte e fazendo-o passar de 60s;
	* ``-g``/``keyint`` grande evita keyframes periódicos "extras".

	Resultado: os únicos keyframes são os forçados, e o corte cai no instante
	exato. O último bloco fica com a sobra.
	"""
	cmd = [
		settings.FFMPEG_BIN,
		"-y",
		"-hide_banner",
		"-loglevel",
		"error",
		*_INPUT_HARDENING,
		"-i",
		str(src),
		*_threads_args(),
		"-map",
		"0",
	]
	if is_audio:
		# Sem faixa de vídeo: apenas re-encoda o áudio e deixa o muxer cortar
		# nos limites de pacote (áudio não tem "keyframe esparso").
		cmd += ["-c:a", "aac", "-b:a", "192k"]
		seg_fmt_opts: list[str] = []
	else:
		big_gop = max(segment_seconds * 60, 3600)  # keyint bem maior que 1 clipe
		cmd += [
			"-c:v",
			"libx264",
			"-preset",
			"veryfast",
			"-crf",
			"23",
			"-force_key_frames",
			f"expr:gte(t,n_forced*{segment_seconds})",
			"-sc_threshold",
			"0",
			"-g",
			str(big_gop),
			"-keyint_min",
			str(big_gop),
			"-x264-params",
			"scenecut=0",
			"-c:a",
			"aac",
			"-b:a",
			"160k",
		]
		# mp4 em segmentos precisa de um movflags que feche cada arquivo:
		seg_fmt_opts = ["-segment_format_options", "movflags=+faststart"]

	cmd += [
		"-f",
		"segment",
		"-segment_time",
		str(segment_seconds),
		# Folga mínima: sem ela o muxer ``segment`` às vezes ignora o keyframe
		# que cai exatamente no limite (o pts interno fica em 59.999999... e a
		# comparação ``>= 60`` falha), emendando tudo num único arquivo. 0.25s
		# basta para pegar o keyframe forçado do limite e — como com
		# ``scenecut=0`` esse é o ÚNICO keyframe naquela região — não há risco de
		# cortar cedo demais. O clipe sai com ``segment_seconds`` + poucos ms.
		"-segment_time_delta",
		"0.25",
		"-reset_timestamps",
		"1",
		"-segment_start_number",
		"1",
		*seg_fmt_opts,
		pattern,
	]
	return cmd


async def split_into_segments(
	src: Path,
	out_dir: Path,
	base_name: str,
	segment_seconds: int = SEGMENT_SECONDS,
) -> list[SegmentInfo]:
	"""
	Divide ``src`` em ``base_name_parte1.ext``, ``_parte2.ext``, ... dentro de
	``out_dir``. Re-encoda forçando um keyframe no início de cada bloco para que
	o corte caia no instante exato (ver ``build_split_command``); o último bloco
	fica com a sobra (nunca é descartado). Nenhum clipe excede
	``segment_seconds + MAX_CLIP_TOLERANCE_SECONDS``.
	"""
	if not ffmpeg_available():
		raise VideoProcessingError("ffmpeg/ffprobe não estão instalados ou disponíveis no servidor.")

	ext = src.suffix.lower() or ".mp4"
	out_dir.mkdir(parents=True, exist_ok=True)
	pattern = str(out_dir / f"{base_name}_parte%d{ext}")

	cmd = build_split_command(src, pattern, segment_seconds, is_audio=ext in _AUDIO_EXTS)
	logger.info("Fatiando em clipes de %ss: %s", segment_seconds, " ".join(cmd))
	code, _out, err = await _run(cmd)

	produced = sorted(
		out_dir.glob(f"{base_name}_parte*{ext}"),
		key=lambda p: int("".join(filter(str.isdigit, p.stem.split("_parte")[-1])) or 0),
	)
	if code != 0 or not produced:
		for p in produced:
			p.unlink(missing_ok=True)
		raise classify_ffmpeg_failure(code, err.decode(errors="ignore"))

	segments: list[SegmentInfo] = []
	for i, path in enumerate(produced, start=1):
		try:
			info = await probe(path)
			duration = info.duration
		except AppError:
			duration = 0.0
		segments.append(
			SegmentInfo(
				index=i,
				path=path,
				filename=path.name,
				duration_seconds=round(duration, 2),
				size_bytes=path.stat().st_size,
			)
		)
	return segments


def build_clips_zip(segments: list[SegmentInfo], dst: Path) -> Path:
	"""Empacota os clipes num único ``.zip`` (sem compressão — vídeo já é comprimido)."""
	with zipfile.ZipFile(dst, "w", compression=zipfile.ZIP_STORED) as zf:
		for seg in segments:
			zf.write(seg.path, arcname=seg.filename)
	return dst
