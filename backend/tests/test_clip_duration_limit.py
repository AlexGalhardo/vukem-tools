"""
Testes do bug "clipe para Stories com mais de 60s".

Contexto do bug
---------------
O fatiamento antigo usava ``ffmpeg -f segment -segment_time 60 -c copy``. Com
*stream copy* o muxer ``segment`` só corta em keyframes já presentes no arquivo.
Vídeos reais das plataformas têm keyframes a cada 2-10s, então cada corte
"escorregava" para o próximo keyframe e os clipes saíam com ~63s em média.

A correção (``app/services/editor.build_split_command``) re-encoda forçando um
keyframe exatamente em cada múltiplo de ``segment_seconds``
(``-force_key_frames expr:gte(t,n_forced*N)``), de modo que o corte cai no
instante exato.

Estes testes:
  1. Reproduzem o bug: o comando antigo (``-c copy``) estoura os 60s.
  2. Validam a correção em 3 vídeos de durações diferentes (65s, 130s, 187s):
     nenhum clipe passa de ``60 + MAX_CLIP_TOLERANCE_SECONDS``.
  3. Validam o mesmo pelo endpoint ``POST /api/v1/edit/split``.
"""

import json
import subprocess
import uuid
import zipfile

import pytest

from app.services.editor import (
	MAX_CLIP_TOLERANCE_SECONDS,
	SEGMENT_SECONDS,
	build_split_command,
	expected_segment_count,
	split_into_segments,
)
from tests.conftest import FFMPEG, FFPROBE, requires_ffmpeg

# Durações escolhidas para exercitar 2, 3 e 4 blocos, sempre com um último bloco
# parcial (5s, 10s, 7s de sobra).
DURATIONS = [65, 130, 187]

# Keyframes forçados a cada 7s na GERAÇÃO do vídeo de teste: 0, 7, 14, ... 56, 63.
# Nenhum cai em múltiplo de 60 -> com ``-c copy`` o primeiro corte "escorrega"
# de 60s para 63s, reproduzindo o sintoma relatado.
GEN_KEYFRAME_INTERVAL = 7


def _probe_duration(path) -> float:
	out = subprocess.run(
		[FFPROBE, "-v", "error", "-print_format", "json", "-show_format", str(path)],
		capture_output=True,
		text=True,
		check=True,
	).stdout
	return float(json.loads(out)["format"]["duration"])


def _make_video(dirpath, seconds: int):
	"""Vídeo testsrc 320x240 30fps com keyframes esparsos e desalinhados de 60s."""
	out = dirpath / f"src_{seconds}s_{uuid.uuid4().hex}.mp4"
	subprocess.run(
		[
			FFMPEG,
			"-y",
			"-hide_banner",
			"-loglevel",
			"error",
			"-f",
			"lavfi",
			"-i",
			f"testsrc=duration={seconds}:size=320x240:rate=30",
			"-f",
			"lavfi",
			"-i",
			f"sine=frequency=440:duration={seconds}",
			"-c:v",
			"libx264",
			"-preset",
			"ultrafast",
			"-pix_fmt",
			"yuv420p",
			"-force_key_frames",
			f"expr:gte(t,n_forced*{GEN_KEYFRAME_INTERVAL})",
			"-c:a",
			"aac",
			"-shortest",
			str(out),
		],
		check=True,
	)
	return out


@pytest.fixture(scope="module")
def videos(tmp_path_factory):
	if not (FFMPEG and FFPROBE):
		pytest.skip("ffmpeg/ffprobe não instalados")
	d = tmp_path_factory.mktemp("clip_dur")
	return {secs: _make_video(d, secs) for secs in DURATIONS}


# --------------------------------------------------------------------------
# 1. Reprodução do bug: o comando antigo (-c copy) NÃO respeita os 60s
# --------------------------------------------------------------------------


@requires_ffmpeg
def test_stream_copy_split_overshoots_60s(videos, tmp_path):
	"""O fatiamento por stream-copy gera pelo menos um clipe não-final > 60.5s."""
	src = videos[130]
	out_dir = tmp_path / "copy_out"
	out_dir.mkdir()
	pattern = str(out_dir / f"legacy_parte%d{src.suffix}")

	legacy_cmd = [
		FFMPEG,
		"-y",
		"-hide_banner",
		"-loglevel",
		"error",
		"-i",
		str(src),
		"-c",
		"copy",
		"-map",
		"0",
		"-f",
		"segment",
		"-segment_time",
		str(SEGMENT_SECONDS),
		"-reset_timestamps",
		"1",
		"-segment_start_number",
		"1",
		pattern,
	]
	subprocess.run(legacy_cmd, check=True)

	clips = sorted(out_dir.glob(f"legacy_parte*{src.suffix}"))
	durations = [_probe_duration(c) for c in clips]
	assert len(durations) >= 2
	non_final_max = max(durations[:-1])
	assert non_final_max > SEGMENT_SECONDS + MAX_CLIP_TOLERANCE_SECONDS, (
		f"esperava ver o bug (clipe > 60.5s), mas o maior clipe não-final teve "
		f"{non_final_max:.3f}s — durações: {[round(d, 3) for d in durations]}"
	)


# --------------------------------------------------------------------------
# 2. Correção: nenhum clipe passa de 60s + tolerância (3 durações)
# --------------------------------------------------------------------------


@requires_ffmpeg
@pytest.mark.parametrize("seconds", DURATIONS)
async def test_split_into_segments_respects_60s(videos, tmp_path, seconds):
	src = videos[seconds]
	out_dir = tmp_path / f"fixed_{seconds}"
	segments = await split_into_segments(src, out_dir, "video", SEGMENT_SECONDS)

	assert len(segments) == expected_segment_count(seconds, SEGMENT_SECONDS)

	durations = [_probe_duration(seg.path) for seg in segments]
	limit = SEGMENT_SECONDS + MAX_CLIP_TOLERANCE_SECONDS

	# Nenhum clipe excede o limite.
	for i, dur in enumerate(durations, start=1):
		assert dur <= limit, (
			f"clipe {i}/{len(durations)} do vídeo de {seconds}s tem {dur:.3f}s "
			f"(> {limit}s) — durações: {[round(d, 3) for d in durations]}"
		)

	# Os clipes não-finais ficam perto de 60s (o corte não "escorrega" nem
	# antecipa demais).
	for i, dur in enumerate(durations[:-1], start=1):
		assert 59.0 <= dur <= limit, f"clipe não-final {i} tem {dur:.3f}s, fora de [59.0, {limit}]"

	# A soma das durações reconstrói o vídeo original (sem perder a sobra).
	assert abs(sum(durations) - seconds) <= 1.0


@requires_ffmpeg
def test_build_split_command_forces_keyframes_on_boundaries():
	cmd = build_split_command("in.mp4", "out_parte%d.mp4", 60)
	assert "-force_key_frames" in cmd
	assert cmd[cmd.index("-force_key_frames") + 1] == "expr:gte(t,n_forced*60)"
	assert "libx264" in cmd
	assert "copy" not in cmd  # não é mais stream-copy
	assert cmd[cmd.index("-segment_time") + 1] == "60"
	# keyframes de corte de cena desligados -> o único keyframe perto de N*60s é
	# o forçado, então o muxer não corta cedo demais.
	assert cmd[cmd.index("-sc_threshold") + 1] == "0"
	assert "scenecut=0" in cmd[cmd.index("-x264-params") + 1]
	assert float(cmd[cmd.index("-segment_time_delta") + 1]) <= 0.5


@requires_ffmpeg
def test_build_split_command_audio_only_skips_video_filter():
	cmd = build_split_command("in.mp3", "out_parte%d.mp3", 60, is_audio=True)
	assert "libx264" not in cmd
	assert "-force_key_frames" not in cmd
	assert "-c:a" in cmd


# --------------------------------------------------------------------------
# 3. Mesmo teste, mas pelo endpoint HTTP /api/v1/edit/split
# --------------------------------------------------------------------------


@requires_ffmpeg
def test_split_endpoint_clips_within_limit(client, videos, tmp_path):
	src = videos[65]
	with src.open("rb") as fh:
		r = client.post(
			"/api/v1/edit/split",
			files={"file": ("video.mp4", fh, "video/mp4")},
			data={"segment_seconds": str(SEGMENT_SECONDS)},
		)
	assert r.status_code == 200, r.text
	assert r.headers["content-type"].startswith("application/zip")

	zip_path = tmp_path / "clips.zip"
	zip_path.write_bytes(r.content)
	extract_dir = tmp_path / "extract"
	extract_dir.mkdir()
	with zipfile.ZipFile(zip_path) as zf:
		zf.extractall(extract_dir)

	clips = sorted(extract_dir.glob("*.mp4"))
	assert len(clips) == expected_segment_count(65, SEGMENT_SECONDS)
	limit = SEGMENT_SECONDS + MAX_CLIP_TOLERANCE_SECONDS
	for c in clips:
		dur = _probe_duration(c)
		assert dur <= limit, f"{c.name} tem {dur:.3f}s (> {limit}s)"
