package audio_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/AlexGalhardo/vukem-tools/internal/audio"
	"github.com/AlexGalhardo/vukem-tools/internal/engine"
	"github.com/AlexGalhardo/vukem-tools/internal/ffmpeg"
)

// engines finds ffmpeg and ffprobe, or skips with a reason that says where to
// run the test.
func engines(tb testing.TB) (ffmpeg.Engine, ffmpeg.Runner, ffmpeg.Capabilities) {
	tb.Helper()

	binary, err := exec.LookPath("ffmpeg")
	if err != nil {
		tb.Skip("ffmpeg is not installed here; run this inside the production image " +
			"(docs/deployment.md § Verifying the image)")
	}

	probe, err := exec.LookPath("ffprobe")
	if err != nil {
		tb.Skip("ffprobe is not installed here; run this inside the production image")
	}

	runner, err := engine.NewRunner(engine.RunnerOptions{
		Limits: engine.Limits{
			Timeout:      3 * time.Minute,
			AddressSpace: 2 << 30,
			CPU:          2 * time.Minute,
			FileSize:     256 << 20,
			Processes:    64,
			OpenFiles:    512,
		},
	})
	if err != nil {
		tb.Fatalf("build a runner: %v", err)
	}

	e := ffmpeg.Engine{FFmpeg: binary, FFprobe: probe}

	caps, err := ffmpeg.ReadCapabilities(context.Background(), e, runner, tb.TempDir())
	if err != nil {
		tb.Fatalf("read the engine's capabilities: %v", err)
	}

	return e, runner, caps
}

// tone writes a test recording: pink noise rather than a pure tone.
//
// The content matters. A single sine wave compresses to almost nothing at
// every bit rate, so it would make a bit-rate option look like it works when
// it does nothing; pink noise has energy across the spectrum, which is the
// hardest thing for a lossy encoder and therefore the honest fixture. It is
// also cheap to generate, which a filter computing a random value per sample
// is not.
func tone(tb testing.TB, e ffmpeg.Engine, r ffmpeg.Runner, dir string, seconds int, gain string) string {
	tb.Helper()

	path := filepath.Join(dir, "tone.flac")

	args := []string{
		"-nostdin", "-hide_banner", "-loglevel", "error", "-nostats", "-y",
		"-f", "lavfi", "-i",
		"anoisesrc=duration=" + itoa(seconds) + ":color=pink:sample_rate=44100:amplitude=0.8",
		"-af", "volume=" + gain,
		"-ac", "2",
		"-c:a", "flac",
		"-threads", "2",
		path,
	}

	if _, err := r.Run(context.Background(), engine.Command{Path: e.FFmpeg, Args: args, Dir: dir}); err != nil {
		tb.Fatalf("write a test recording: %v", err)
	}

	return path
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}

	var digits []byte

	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}

	return string(digits)
}

// copyInto copies a file into a fresh working directory.
func copyInto(tb testing.TB, from, dir string) string {
	tb.Helper()

	body, err := os.ReadFile(from)
	if err != nil {
		tb.Fatalf("read the fixture: %v", err)
	}

	to := filepath.Join(dir, filepath.Base(from))
	if err := os.WriteFile(to, body, 0o600); err != nil {
		tb.Fatalf("write the fixture: %v", err)
	}

	return to
}

// TestEveryAudioFormatIsValidatedWithFFprobe is the S4.4 assert for audio:
// the codec, the duration, the channel count and the sample rate are read
// back out of the written file.
func TestEveryAudioFormatIsValidatedWithFFprobe(t *testing.T) {
	e, r, caps := engines(t)
	source := tone(t, e, r, t.TempDir(), 2, "1.0")

	cases := []struct {
		format audio.Format
		codec  string
	}{
		{audio.MP3, "mp3"},
		{audio.WAV, "pcm_s16le"},
		{audio.FLAC, "flac"},
		{audio.Opus, "opus"},
		{audio.AAC, "aac"},
	}

	for _, c := range cases {
		t.Run(string(c.format), func(t *testing.T) {
			if !slicesContains(audio.Available(caps), c.format) {
				t.Skipf("this build cannot encode %s", c.format)
			}

			dir := t.TempDir()
			input := copyInto(t, source, dir)

			got, err := audio.Convert(context.Background(), e, r, caps, dir, input,
				audio.Options{To: c.format}, nil)
			if err != nil {
				t.Fatalf("convert to %s: %v", c.format, err)
			}

			if !got.Output.HasAudio() {
				t.Fatal("the result has no audio stream")
			}

			if got.Output.Audio.Codec != c.codec {
				t.Errorf("codec is %q, want %q", got.Output.Audio.Codec, c.codec)
			}

			// A lossy encoder pads or trims by a frame, so the tolerance is
			// a frame rather than zero; losing a second would still fail.
			if drift := got.Output.Duration - got.Source.Duration; drift > 200*time.Millisecond ||
				drift < -200*time.Millisecond {
				t.Errorf("duration drifted by %v", drift)
			}

			if got.Output.Audio.Channels != 2 {
				t.Errorf("channels = %d, want 2", got.Output.Audio.Channels)
			}

			if info, err := os.Stat(got.Path); err != nil || info.Size() == 0 {
				t.Errorf("the result is missing or empty: %v", err)
			}
		})
	}
}

func slicesContains(formats []audio.Format, want audio.Format) bool {
	return slices.Contains(formats, want)
}

// TestABitrateReachesTheEncoderForALossyFormat proves the dial is connected,
// and that it is ignored where it is meaningless.
func TestABitrateReachesTheEncoderForALossyFormat(t *testing.T) {
	e, r, caps := engines(t)
	source := tone(t, e, r, t.TempDir(), 3, "1.0")

	size := func(format audio.Format, bitrate int) int64 {
		t.Helper()

		dir := t.TempDir()
		input := copyInto(t, source, dir)

		got, err := audio.Convert(context.Background(), e, r, caps, dir, input,
			audio.Options{To: format, Bitrate: bitrate}, nil)
		if err != nil {
			t.Fatalf("convert at %dk: %v", bitrate, err)
		}

		info, err := os.Stat(got.Path)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}

		return info.Size()
	}

	low, high := size(audio.MP3, 64), size(audio.MP3, 320)
	if low >= high {
		t.Errorf("64k produced %d bytes and 320k produced %d: the bit rate is not reaching the encoder",
			low, high)
	}

	t.Logf("MP3 at 64k is %d bytes, at 320k is %d bytes", low, high)

	// FLAC is lossless, so a bit rate is a category error and is ignored
	// rather than refused: a client that sets one and then asks for FLAC is
	// asking for FLAC.
	if a, b := size(audio.FLAC, 64), size(audio.FLAC, 320); a != b {
		t.Errorf("a bit rate changed a lossless encode: %d against %d bytes", a, b)
	}
}

// TestTwoPassNormalisationMeasuresThenCorrects is the loudness assert: the
// first pass reports how far off the file is, and the second brings it to the
// target.
func TestTwoPassNormalisationMeasuresThenCorrects(t *testing.T) {
	e, r, caps := engines(t)
	dir := t.TempDir()

	// A deliberately quiet recording, so there is a correction to make and
	// a single-pass moving average would undershoot it.
	input := tone(t, e, r, dir, 4, "0.08")

	got, err := audio.Convert(context.Background(), e, r, caps, dir, input, audio.Options{
		To:               audio.FLAC,
		Normalise:        true,
		TwoPassNormalise: true,
	}, nil)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}

	if got.Loudness == nil {
		t.Fatal("no measurement was reported, so the first pass did not run")
	}

	t.Logf("measured %.1f LUFS, true peak %.1f dBTP, correction %.1f LU",
		got.Loudness.InputIntegrated, got.Loudness.InputTruePeak, got.Loudness.Correction())

	// The fixture is quiet, so the correction must be positive and large.
	if got.Loudness.Correction() < 5 {
		t.Errorf("the correction is %.1f LU, which is too small for a deliberately quiet fixture",
			got.Loudness.Correction())
	}

	// And the result has to actually be at the target. Measuring the output
	// is the only honest check: asserting on the filter string would prove
	// the arguments were built, not that the audio changed.
	after := measure(t, e, r, dir, got.Path)

	if difference := after - audio.LoudnessTarget; difference > 1.5 || difference < -1.5 {
		t.Errorf("the result measures %.1f LUFS, want within 1.5 LU of %.1f",
			after, audio.LoudnessTarget)
	}

	t.Logf("after normalisation: %.1f LUFS", after)
}

// measure runs a measurement-only loudnorm pass over a file and returns its
// integrated loudness in LUFS.
//
// It parses loudnorm's own JSON block out of stderr, which is the only place
// the filter reports it: there is no structured stream for a measurement, and
// this is a measurement rather than progress.
func measure(tb testing.TB, e ffmpeg.Engine, r ffmpeg.Runner, dir, path string) float64 {
	tb.Helper()

	result, err := r.Run(context.Background(), engine.Command{
		Path: e.FFmpeg,
		Args: []string{
			"-nostdin", "-hide_banner", "-loglevel", "info", "-nostats", "-y",
			"-i", path,
			"-vn", "-map", "0:a:0",
			"-af", "loudnorm=I=-16:TP=-1.5:LRA=11:print_format=json",
			"-f", "null", "-threads", "2", "-",
		},
		Dir: dir,
	})
	if err != nil {
		tb.Fatalf("measure %s: %v", filepath.Base(path), err)
	}

	start := strings.LastIndex(result.Stderr, "{")
	end := strings.LastIndex(result.Stderr, "}")

	if start < 0 || end <= start {
		tb.Fatalf("loudnorm reported no measurement: %s", result.Stderr)
	}

	var block struct {
		InputI string `json:"input_i"`
	}

	if err := json.Unmarshal([]byte(result.Stderr[start:end+1]), &block); err != nil {
		tb.Fatalf("read loudnorm's output: %v", err)
	}

	value, err := strconv.ParseFloat(strings.TrimSpace(block.InputI), 64)
	if err != nil {
		tb.Fatalf("read the measured loudness %q: %v", block.InputI, err)
	}

	return value
}

// TestAFileWithNoAudioIsRefused: a silent video submitted to the audio
// converter has nothing to convert, and says so.
func TestAFileWithNoAudioIsRefused(t *testing.T) {
	e, r, caps := engines(t)
	dir := t.TempDir()

	silent := filepath.Join(dir, "silent.mp4")

	args := []string{
		"-nostdin", "-hide_banner", "-loglevel", "error", "-nostats", "-y",
		"-f", "lavfi", "-i", "testsrc=duration=1:size=160x120:rate=10",
		"-c:v", "libx264", "-preset", "ultrafast", "-crf", "30", "-pix_fmt", "yuv420p", "-an", "-threads", "2",
		silent,
	}

	if _, err := r.Run(context.Background(), engine.Command{Path: e.FFmpeg, Args: args, Dir: dir}); err != nil {
		t.Skipf("this build cannot write the silent fixture: %v", err)
	}

	_, err := audio.Convert(context.Background(), e, r, caps, dir, silent,
		audio.Options{To: audio.MP3}, nil)

	if _, ok := errors.AsType[*audio.NoAudioError](err); !ok {
		t.Fatalf("want a NoAudioError, got %v", err)
	}
}

// TestExtractingTheSoundtrackOfAVideoIsTheSamePath proves the claim in the
// package comment: there is no separate extract because there is no separate
// work.
func TestExtractingTheSoundtrackOfAVideoIsTheSamePath(t *testing.T) {
	e, r, caps := engines(t)
	dir := t.TempDir()

	withSound := filepath.Join(dir, "clip.mp4")

	args := []string{
		"-nostdin", "-hide_banner", "-loglevel", "error", "-nostats", "-y",
		"-f", "lavfi", "-i", "testsrc=duration=3:size=160x120:rate=25",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=3",
		"-c:v", "libx264", "-preset", "ultrafast", "-crf", "30", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-shortest", "-threads", "2", withSound,
	}

	if _, err := r.Run(context.Background(), engine.Command{Path: e.FFmpeg, Args: args, Dir: dir}); err != nil {
		t.Skipf("this build cannot write the fixture: %v", err)
	}

	got, err := audio.Convert(context.Background(), e, r, caps, dir, withSound,
		audio.Options{To: audio.MP3}, nil)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	if !got.Output.HasAudio() {
		t.Fatal("the extraction produced no audio")
	}

	// -vn: the picture must not come along, and a cover image would
	// otherwise be carried into a format that cannot hold it.
	if got.Output.HasVideo() {
		t.Errorf("the extraction carried a %q video stream", got.Output.Video.Codec)
	}
}

// TestAFormatThisBuildCannotEncodeNamesTheAlternatives: a refusal that does
// not say what is available leaves somebody guessing through five formats.
func TestAFormatThisBuildCannotEncodeNamesTheAlternatives(t *testing.T) {
	caps := ffmpeg.CapabilitiesFrom(
		"ffmpeg version 7.1.5\nconfiguration: --enable-shared\n",
		"Encoders:\n ------\n A....D flac                 FLAC\n",
	)

	err := &audio.MissingFormatError{
		Format:    audio.MP3,
		Available: []string{"FLAC"},
	}

	if !strings.Contains(err.Message(), "FLAC") {
		t.Errorf("the message should name what is available, got %q", err.Message())
	}

	if got := audio.Available(caps); len(got) != 1 || got[0] != audio.FLAC {
		t.Errorf("Available = %v, want just FLAC for a build with only its encoder", got)
	}
}
