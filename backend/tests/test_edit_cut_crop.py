"""
Testes da feature Cut and Crop.

  * validate_params: validações de tempo e de área de crop (unitário, sem ffmpeg)
  * build_ffmpeg_command: forma do comando ffmpeg (unitário, sem executar)
  * integração: sobe um vídeo de teste real e verifica que o resultado foi
    cortado e recortado (executa o ffmpeg de verdade; pulado se ausente)
"""

import json
import subprocess

import pytest

from app.services.editor import (
	EditError,
	MediaInfo,
	build_ffmpeg_command,
	validate_params,
)
from tests.conftest import FFPROBE, requires_ffmpeg

MEDIA = MediaInfo(duration=10.0, width=1280, height=720)


def _params(**over):
	base = {
		"start": 1.0,
		"end": 5.0,
		"crop_x": None,
		"crop_y": None,
		"crop_w": None,
		"crop_h": None,
		"output_format": "mp4",
	}
	base.update(over)
	return validate_params(MEDIA, **base)


# --------------------------- validate_params ---------------------------


def test_valid_time_range():
	p = _params(start=1.0, end=5.0)
	assert p.start == 1.0 and p.end == 5.0
	assert p.has_crop is False


def test_end_before_start_rejected():
	with pytest.raises(EditError):
		_params(start=5.0, end=2.0)


def test_negative_start_rejected():
	with pytest.raises(EditError):
		_params(start=-1.0, end=2.0)


def test_start_after_duration_rejected():
	with pytest.raises(EditError):
		_params(start=99.0, end=None)


def test_end_after_duration_rejected():
	with pytest.raises(EditError):
		_params(start=1.0, end=999.0)


def test_valid_crop_within_bounds():
	p = _params(crop_x=100, crop_y=50, crop_w=640, crop_h=360)
	assert p.has_crop is True
	assert (p.crop_w, p.crop_h) == (640, 360)


def test_crop_out_of_bounds_rejected():
	with pytest.raises(EditError):
		_params(crop_x=1000, crop_y=0, crop_w=640, crop_h=360)


def test_partial_crop_rejected():
	with pytest.raises(EditError):
		_params(crop_x=10, crop_y=10, crop_w=None, crop_h=100)


def test_zero_crop_dimension_rejected():
	with pytest.raises(EditError):
		_params(crop_x=0, crop_y=0, crop_w=0, crop_h=100)


def test_bad_output_format_rejected():
	with pytest.raises(EditError):
		_params(output_format="avi")


def test_start_defaults_to_zero():
	p = _params(start=None, end=None)
	assert p.start == 0.0 and p.end is None


# ------------------------ build_ffmpeg_command ------------------------


def test_command_has_trim_and_crop_filter():
	p = _params(start=2.0, end=6.5, crop_x=10, crop_y=20, crop_w=300, crop_h=200)
	cmd = build_ffmpeg_command("in.mp4", "out.mp4", p)
	assert "-ss" in cmd and cmd[cmd.index("-ss") + 1] == "2.000"
	assert "-to" in cmd and cmd[cmd.index("-to") + 1] == "6.500"
	assert "-vf" in cmd
	assert cmd[cmd.index("-vf") + 1] == "crop=300:200:10:20"
	assert cmd[-1] == "out.mp4"
	assert "libx264" in cmd


def test_command_without_end_has_no_to():
	p = _params(start=1.0, end=None)
	cmd = build_ffmpeg_command("in.mp4", "out.mp4", p)
	assert "-to" not in cmd


def test_command_mp3_is_audio_only():
	p = _params(start=0.0, end=3.0, output_format="mp3")
	cmd = build_ffmpeg_command("in.mp4", "out.mp3", p)
	assert "-vn" in cmd
	assert "libmp3lame" in cmd
	assert "-vf" not in cmd


# ----------------------------- integração -----------------------------


@requires_ffmpeg
def test_cut_crop_endpoint_produces_shorter_cropped_video(client, sample_video):
	with sample_video.open("rb") as fh:
		r = client.post(
			"/api/v1/edit/cut-crop",
			files={"file": ("sample.mp4", fh, "video/mp4")},
			data={
				"start": "0.5",
				"end": "2.0",
				"crop_x": "40",
				"crop_y": "30",
				"crop_w": "160",
				"crop_h": "120",
				"output_format": "mp4",
			},
		)
	assert r.status_code == 200, r.text
	assert r.headers["content-type"].startswith("video/mp4")
	assert "attachment" in r.headers["content-disposition"]

	tmp = sample_video.parent / "result.mp4"
	tmp.write_bytes(r.content)
	meta = json.loads(
		subprocess.run(
			[FFPROBE, "-v", "error", "-print_format", "json", "-show_format", "-show_streams", str(tmp)],
			capture_output=True,
			text=True,
			check=True,
		).stdout
	)
	vstream = next(s for s in meta["streams"] if s["codec_type"] == "video")
	assert vstream["width"] == 160
	assert vstream["height"] == 120
	assert float(meta["format"]["duration"]) < 2.0  # ~1.5s


@requires_ffmpeg
def test_cut_crop_endpoint_rejects_crop_out_of_bounds(client, sample_video):
	with sample_video.open("rb") as fh:
		r = client.post(
			"/api/v1/edit/cut-crop",
			files={"file": ("sample.mp4", fh, "video/mp4")},
			data={
				"start": "0",
				"crop_x": "0",
				"crop_y": "0",
				"crop_w": "9999",
				"crop_h": "120",
				"output_format": "mp4",
			},
		)
	assert r.status_code == 400
	assert "crop" in r.json()["detail"].lower()


@requires_ffmpeg
def test_cut_crop_endpoint_mp3_output(client, sample_video):
	with sample_video.open("rb") as fh:
		r = client.post(
			"/api/v1/edit/cut-crop",
			files={"file": ("sample.mp4", fh, "video/mp4")},
			data={"start": "0", "end": "1.5", "output_format": "mp3"},
		)
	assert r.status_code == 200, r.text
	assert r.headers["content-type"].startswith("audio/mpeg")
	assert r.content[:3] == b"ID3" or r.content[0] == 0xFF
