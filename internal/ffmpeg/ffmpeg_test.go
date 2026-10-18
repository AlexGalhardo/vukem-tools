package ffmpeg

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/AlexGalhardo/vukem-tools/internal/engine"
)

// These tests need no ffmpeg: they cover the capability parsing, the ffprobe
// JSON shape, the progress stream and the error classification, which are the
// parts that decide what the engine is asked to do and how its answers are
// read.

// The configuration line of Debian's GPL build, abbreviated to the part that
// matters. A real one is two thousand characters of flags.
const gplVersion = `ffmpeg version 7.1.5-0+deb13u1 Copyright (c) 2000-2026 the FFmpeg developers
built with gcc 14 (Debian 14.2.0-19)
configuration: --prefix=/usr --enable-gpl --enable-libx264 --enable-libx265 --enable-libvpx
libavutil      59. 39.100 / 59. 39.100`

// The same build without --enable-gpl, which is what a distribution that
// cannot ship GPL encoders produces. x264 and x265 are simply absent.
const lgplVersion = `ffmpeg version 7.1.5 Copyright (c) 2000-2026 the FFmpeg developers
built with gcc 14 (Debian 14.2.0-19)
configuration: --prefix=/usr --enable-libvpx --enable-libopus --enable-shared
libavutil      59. 39.100 / 59. 39.100`

const gplEncoders = `Encoders:
 V..... = Video
 ------
 V....D libx264              libx264 H.264 / AVC (codec h264)
 V....D libx265              libx265 H.265 / HEVC (codec hevc)
 V....D libvpx-vp9           libvpx VP9 (codec vp9)
 V..... libsvtav1            SVT-AV1 encoder (codec av1)
 A....D aac                  AAC (Advanced Audio Coding)
 A....D libmp3lame           libmp3lame MP3 (codec mp3)
 A....D libopus              libopus Opus (codec opus)`

const lgplEncoders = `Encoders:
 V..... = Video
 ------
 V....D libvpx-vp9           libvpx VP9 (codec vp9)
 V..... libsvtav1            SVT-AV1 encoder (codec av1)
 A....D aac                  AAC (Advanced Audio Coding)
 A....D libopus              libopus Opus (codec opus)`

func TestAGPLBuildIsRecognisedAndAnLGPLOneIsNot(t *testing.T) {
	gpl := CapabilitiesFrom(gplVersion, gplEncoders)
	if !gpl.GPL {
		t.Error("a build configured with --enable-gpl was not recognised as one")
	}

	if !gpl.Has("libx264") || !gpl.Has("libx265") {
		t.Error("the GPL build's x264 and x265 encoders were not found")
	}

	if got := gpl.Version; !strings.HasPrefix(got, "ffmpeg version 7.1.5") {
		t.Errorf("version = %q", got)
	}

	lgpl := CapabilitiesFrom(lgplVersion, lgplEncoders)
	if lgpl.GPL {
		t.Error("a build without --enable-gpl was reported as GPL")
	}

	if lgpl.Has("libx264") || lgpl.Has("libx265") {
		t.Error("the LGPL build was reported as having the GPL-only encoders")
	}

	// It still has the royalty-free ones, which is what the refusal message
	// points somebody at.
	if !lgpl.Has("libvpx-vp9") || !lgpl.Has("libsvtav1") {
		t.Error("the LGPL build's VP9 and AV1 encoders were not found")
	}
}

func TestTheEncoderListHeaderIsNotMistakenForAnEncoder(t *testing.T) {
	caps := CapabilitiesFrom(gplVersion, gplEncoders)

	for _, notAnEncoder := range []string{"Encoders:", "=", "Video", "------", "-"} {
		if caps.Has(notAnEncoder) {
			t.Errorf("%q was read as an encoder name", notAnEncoder)
		}
	}
}

func TestAMissingGPLEncoderSaysWhyAndWhatToUseInstead(t *testing.T) {
	err := &MissingEncoderError{
		Encoder: "libx264", Codec: "H.264", GPLOnly: true, BuildIsGPL: false,
	}

	message := err.Message()

	for _, want := range []string{"H.264", "GPL", "VP9", "AV1"} {
		if !strings.Contains(message, want) {
			t.Errorf("the message should mention %q, got %q", want, message)
		}
	}

	// On a GPL build, a missing encoder is not a licence problem, so the
	// message must not blame one.
	onGPL := (&MissingEncoderError{
		Encoder: "libaom-av1", Codec: "AV1", GPLOnly: false, BuildIsGPL: true,
	}).Message()

	if strings.Contains(onGPL, "GPL") {
		t.Errorf("a non-GPL encoder's absence was blamed on GPL: %q", onGPL)
	}
}

// A real ffprobe answer for a short H.264 and AAC MP4, trimmed to the fields
// this package reads. Every number is a string, which is ffprobe's own
// choice and the reason the decoding is explicit.
const probeAnswer = `{
	"streams": [
		{
			"index": 0, "codec_name": "h264", "codec_type": "video",
			"width": 1920, "height": 1080,
			"r_frame_rate": "30000/1001", "avg_frame_rate": "30000/1001",
			"bit_rate": "2500000", "duration": "10.010000"
		},
		{
			"index": 1, "codec_name": "aac", "codec_type": "audio",
			"channels": 2, "sample_rate": "48000",
			"bit_rate": "128000", "duration": "10.016000"
		}
	],
	"format": {
		"format_name": "mov,mp4,m4a,3gp,3g2,mj2",
		"duration": "10.016000",
		"bit_rate": "2640000"
	}
}`

func TestTheProbeReadsTheContainerAndBothStreams(t *testing.T) {
	got, err := parseProbe(probeAnswer)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if got.FormatName != "mov,mp4,m4a,3gp,3g2,mj2" {
		t.Errorf("format = %q", got.FormatName)
	}

	if want := 10016 * time.Millisecond; got.Duration != want {
		t.Errorf("duration = %v, want %v", got.Duration, want)
	}

	if !got.HasVideo() || !got.HasAudio() {
		t.Fatalf("video = %v, audio = %v", got.HasVideo(), got.HasAudio())
	}

	if got.Video.Width != 1920 || got.Video.Height != 1080 {
		t.Errorf("video is %dx%d", got.Video.Width, got.Video.Height)
	}

	// 30000/1001 is 29.97, the rate every NTSC-derived file actually has.
	// Reading it as 30 would make every duration computed from a frame count
	// wrong by a tenth of a per cent, which on an hour is four seconds.
	if rate := got.Video.FrameRate; rate < 29.96 || rate > 29.98 {
		t.Errorf("frame rate = %v, want about 29.97", rate)
	}

	if got.Audio.Channels != 2 || got.Audio.SampleRate != 48000 {
		t.Errorf("audio is %d channels at %d Hz", got.Audio.Channels, got.Audio.SampleRate)
	}
}

func TestAProbeWithNoStreamsIsRefused(t *testing.T) {
	// What ffprobe answers for a text file it was handed: a format it
	// guessed and no streams at all.
	raw := `{"streams":[],"format":{"format_name":"tty","duration":"N/A"}}`

	if _, err := parseProbe(raw); !errors.Is(err, ErrNoStreams) {
		t.Errorf("want ErrNoStreams, got %v", err)
	}
}

func TestADurationOfNAFallsBackToTheLongestStream(t *testing.T) {
	// A Matroska file often carries no container duration. Without the
	// fallback there is no total, and every progress report would be -1.
	raw := `{
		"streams": [
			{"index":0,"codec_name":"vp9","codec_type":"video","width":640,"height":360,
			 "avg_frame_rate":"25/1","duration":"42.500000"},
			{"index":1,"codec_name":"opus","codec_type":"audio","channels":2,
			 "sample_rate":"48000","duration":"42.000000"}
		],
		"format": {"format_name":"matroska,webm","duration":"N/A"}
	}`

	got, err := parseProbe(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if want := 42500 * time.Millisecond; got.Duration != want {
		t.Errorf("duration = %v, want %v from the longest stream", got.Duration, want)
	}
}

func TestAStillImageStreamFallsBackToTheRealTimeFrameRate(t *testing.T) {
	// A PNG probed as a stream reports "0/0" for the average rate, which
	// would read as zero and make a sprite sheet's sampling rate a division
	// by zero.
	raw := `{
		"streams": [
			{"index":0,"codec_name":"png","codec_type":"video","width":320,"height":200,
			 "avg_frame_rate":"0/0","r_frame_rate":"25/1"}
		],
		"format": {"format_name":"png_pipe","duration":"N/A"}
	}`

	got, err := parseProbe(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if got.Video.FrameRate != 25 {
		t.Errorf("frame rate = %v, want 25 from the real-time rate", got.Video.FrameRate)
	}
}

func TestTheProgressStreamIsReadPerBlockNotPerLine(t *testing.T) {
	// ffmpeg prints a block of key=value lines and closes it with
	// "progress=". Reporting per line would send five reports per block,
	// four of them with a stale time.
	var state progressState

	total := 100 * time.Second

	for _, line := range []string{
		"frame=250",
		"fps=25.0",
		"bitrate=1000.0kbits/s",
		"out_time_us=10000000",
		"speed=1.85x",
	} {
		if _, ok := state.feed(line, total); ok {
			t.Fatalf("%q closed a block on its own", line)
		}
	}

	got, ok := state.feed("progress=continue", total)
	if !ok {
		t.Fatal("progress=continue did not close the block")
	}

	if got.Percent != 10 {
		t.Errorf("percent = %d, want 10 of a hundred-second file at ten seconds", got.Percent)
	}

	if got.OutTime != 10*time.Second {
		t.Errorf("out time = %v, want 10s: out_time_us is microseconds", got.OutTime)
	}

	if got.Frame != 250 || got.Speed != "1.85x" {
		t.Errorf("frame = %d, speed = %q", got.Frame, got.Speed)
	}

	if got.Done {
		t.Error("a continuing block was reported as done")
	}

	final, ok := state.feed("progress=end", total)
	if !ok || !final.Done {
		t.Errorf("progress=end gave ok=%v done=%v", ok, final.Done)
	}
}

func TestOutTimeMsIsMicrosecondsDespiteItsName(t *testing.T) {
	// ffmpeg's out_time_ms has been microseconds for years. Reading it as
	// milliseconds gives a percentage a thousand times too small, which
	// looks exactly like a stuck conversion.
	var state progressState

	state.feed("out_time_ms=5000000", 10*time.Second)

	got, _ := state.feed("progress=continue", 10*time.Second)
	if got.OutTime != 5*time.Second {
		t.Errorf("out time = %v, want 5s", got.OutTime)
	}

	if got.Percent != 50 {
		t.Errorf("percent = %d, want 50", got.Percent)
	}
}

func TestAnUnknownTotalGivesNoFalsePercentage(t *testing.T) {
	var state progressState

	state.feed("out_time_us=3000000", 0)

	got, _ := state.feed("progress=continue", 0)
	if got.Percent != -1 {
		t.Errorf("percent = %d, want -1 when the duration is unknown", got.Percent)
	}
}

func TestTheRunningPercentageNeverReaches100(t *testing.T) {
	// ffmpeg's out_time passes the container's reported duration by a frame
	// or two on many files. A bar that reaches 100 and keeps going is worse
	// than one that waits at 99.
	if got := percentOf(11*time.Second, 10*time.Second); got != 99 {
		t.Errorf("percent = %d, want 99", got)
	}
}

func TestProgressStopsTheEngineWhenTheCallerSaysSo(t *testing.T) {
	// The owner cancelling has to reach ffmpeg promptly: a cancelled
	// transcode of a long video would otherwise hold a worker and a CPU for
	// minutes after nobody wanted the result.
	stop := errors.New("the owner cancelled it")

	fake := &progressRunner{lines: []string{"out_time_us=1000", "progress=continue"}}

	_, err := Run(context.Background(), Engine{FFmpeg: "ffmpeg"}, fake, t.TempDir(),
		[]string{"-i", "in.mp4", "out.mp4"}, time.Minute,
		func(Progress) error { return stop })

	if !errors.Is(err, stop) {
		t.Fatalf("want the caller's own error, got %v", err)
	}

	if !fake.cancelled {
		t.Error("the engine's context was not cancelled, so ffmpeg would keep running")
	}
}

// progressRunner feeds scripted progress lines to the command's callback and
// records whether the context it was given was cancelled.
type progressRunner struct {
	lines     []string
	cancelled bool
}

func (f *progressRunner) Run(ctx context.Context, cmd engine.Command) (engine.Result, error) {
	for _, line := range f.lines {
		if cmd.Progress != nil {
			cmd.Progress(line)
		}
	}

	f.cancelled = ctx.Err() != nil

	return engine.Result{}, ctx.Err()
}

func TestFFmpegErrorsAreClassifiedFromItsOwnWords(t *testing.T) {
	cases := []struct {
		stderr string
		want   ErrorKind
	}{
		{"[in#0 @ 0x5] Error opening input: Invalid data found when processing input", KindUnreadable},
		{"[mov,mp4 @ 0x5] moov atom not found", KindUnreadable},
		{"Unknown encoder 'libx264'", KindUnsupported},
		{"Unable to find a suitable output format for 'out.xyz'", KindUnsupported},
		{"av_interleaved_write_frame(): No space left on device", KindDiskFull},
		{"Cannot allocate memory", KindOutOfMemory},
		{"something nobody has seen before", KindOther},
	}

	for _, c := range cases {
		err := Classify(&engine.ExitError{Code: 1, Stderr: c.stderr}, "convert the file")

		var engineErr *EngineError
		if !errors.As(err, &engineErr) {
			t.Fatalf("classify returned %T", err)
		}

		if engineErr.Kind != c.want {
			t.Errorf("%q classified as %s, want %s", c.stderr, engineErr.Kind, c.want)
		}

		if engineErr.Stderr != c.stderr {
			t.Errorf("the engine's output was lost: %q", engineErr.Stderr)
		}
	}
}

func TestATimeoutIsNotMistakenForABadFile(t *testing.T) {
	err := Classify(engine.ErrTimeout, "convert the file")

	var engineErr *EngineError
	if !errors.As(err, &engineErr) || engineErr.Kind != KindTimeout {
		t.Fatalf("want a timeout, got %v", err)
	}
}

func TestEveryInvocationRefusesTheNetworkAndTheTerminal(t *testing.T) {
	args := strings.Join(Base(), " ")

	for _, want := range []string{
		// A crafted playlist that made ffmpeg fetch a URL would be a
		// server-side request forgery with the engine as the client.
		"-protocol_whitelist file",
		// ffmpeg reads the terminal by default, and a worker has none.
		"-nostdin",
		// Progress comes from a structured stream, so the stderr stats line
		// is off and stderr stays the error channel.
		"-nostats",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("the base arguments are missing %q: %s", want, args)
		}
	}

	// The thread count is bounded from the container's quota, not left to
	// ffmpeg's guess at the host's core count.
	if !strings.Contains(args, "-threads") {
		t.Errorf("the base arguments do not bound the thread count: %s", args)
	}
}
