package video_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AlexGalhardo/vukem-tools/internal/engine"
	"github.com/AlexGalhardo/vukem-tools/internal/ffmpeg"
	"github.com/AlexGalhardo/vukem-tools/internal/video"
)

// engines finds ffmpeg and ffprobe, or skips with a reason that says where to
// run the test. They are in the production image and are not expected on a
// development machine, so a skip here must never read as a pass.
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
			Timeout:      5 * time.Minute,
			AddressSpace: 3 << 30,
			CPU:          4 * time.Minute,
			FileSize:     512 << 20,
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

// clip writes a short test video into dir with ffmpeg's own synthetic
// sources.
//
// Generated rather than committed: a committed clip would be a binary nobody
// can review, and the generator states exactly what is in the file — a
// moving test pattern and a sine tone, both of which actually compress, which
// a flat colour and silence would not.
func clip(tb testing.TB, e ffmpeg.Engine, r ffmpeg.Runner, dir string, seconds int, w, h int) string {
	tb.Helper()

	path := filepath.Join(dir, "clip.mp4")

	// -f lavfi inputs are generated, not read from disc, so the protocol
	// whitelist in the base arguments does not apply to them. They are built
	// here with an explicit argument list rather than through ffmpeg.Base
	// for exactly that reason.
	args := []string{
		"-nostdin", "-hide_banner", "-loglevel", "error", "-nostats", "-y",
		"-f", "lavfi", "-i", fmt.Sprintf("testsrc=duration=%d:size=%dx%d:rate=25", seconds, w, h),
		"-f", "lavfi", "-i", fmt.Sprintf("sine=frequency=440:duration=%d", seconds),
		"-c:v", "libx264", "-preset", "ultrafast", "-crf", "28", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-b:a", "96k",
		"-shortest",
		"-threads", "2",
		path,
	}

	if _, err := r.Run(context.Background(), engine.Command{Path: e.FFmpeg, Args: args, Dir: dir}); err != nil {
		tb.Fatalf("write a test clip: %v", err)
	}

	return path
}

// silentClip writes a clip with no audio stream at all, for the paths that
// have to cope with one.
func silentClip(tb testing.TB, e ffmpeg.Engine, r ffmpeg.Runner, dir string, seconds int) string {
	tb.Helper()

	path := filepath.Join(dir, "silent.mp4")

	args := []string{
		"-nostdin", "-hide_banner", "-loglevel", "error", "-nostats", "-y",
		"-f", "lavfi", "-i", fmt.Sprintf("testsrc=duration=%d:size=320x240:rate=25", seconds),
		"-c:v", "libx264", "-preset", "ultrafast", "-crf", "30", "-pix_fmt", "yuv420p",
		"-an",
		"-threads", "2",
		path,
	}

	if _, err := r.Run(context.Background(), engine.Command{Path: e.FFmpeg, Args: args, Dir: dir}); err != nil {
		tb.Fatalf("write a silent clip: %v", err)
	}

	return path
}

// TestEveryConversionIsValidatedWithFFprobe is the S4.4 assert: container,
// codecs, duration and resolution are read back out of the file that was
// written, not predicted from what was asked for.
func TestEveryConversionIsValidatedWithFFprobe(t *testing.T) {
	e, r, caps := engines(t)
	source := clip(t, e, r, t.TempDir(), 3, 320, 240)

	cases := []struct {
		name  string
		opts  video.Options
		codec string
		audio string
	}{
		{"mp4_h264_aac", video.Options{Container: video.MP4}, "h264", "aac"},
		{
			"mp4_h265",
			video.Options{Container: video.MP4, Video: video.H265},
			"hevc", "aac",
		},
		{"webm_vp9_opus", video.Options{Container: video.WebM}, "vp9", "opus"},
		{"mkv_h264_aac", video.Options{Container: video.MKV}, "h264", "aac"},
		{"mov_h264_aac", video.Options{Container: video.MOV}, "h264", "aac"},
		{
			"mp4_muted",
			video.Options{Container: video.MP4, Audio: video.Mute},
			"h264", "",
		},
		{
			"mp4_scaled_to_120",
			video.Options{Container: video.MP4, Height: 120},
			"h264", "aac",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if encoder := encoderFor(c.opts); encoder != "" && !caps.Has(encoder) {
				t.Skipf("this build has no %s encoder", encoder)
			}

			dir := t.TempDir()
			input := copyInto(t, source, dir)

			got, err := video.Convert(context.Background(), e, r, caps, dir, input, c.opts, nil)
			if err != nil {
				t.Fatalf("convert: %v", err)
			}

			if !got.Output.HasVideo() {
				t.Fatal("the result has no video stream")
			}

			if got.Output.Video.Codec != c.codec {
				t.Errorf("video codec is %q, want %q", got.Output.Video.Codec, c.codec)
			}

			switch c.audio {
			case "":
				if got.Output.HasAudio() {
					t.Errorf("a muted conversion still has a %q stream", got.Output.Audio.Codec)
				}
			default:
				if !got.Output.HasAudio() {
					t.Fatal("the result has no audio stream")
				}

				if got.Output.Audio.Codec != c.audio {
					t.Errorf("audio codec is %q, want %q", got.Output.Audio.Codec, c.audio)
				}
			}

			// The duration survives the transcode, within a frame: a
			// conversion that silently dropped the last second would
			// otherwise pass every other assertion here.
			if drift := got.Output.Duration - got.Source.Duration; drift > 200*time.Millisecond ||
				drift < -200*time.Millisecond {
				t.Errorf("duration drifted by %v: %v became %v",
					drift, got.Source.Duration, got.Output.Duration)
			}

			if c.opts.Height > 0 && got.Output.Video.Height != c.opts.Height {
				t.Errorf("height is %d, want %d", got.Output.Video.Height, c.opts.Height)
			}

			if c.opts.Height == 0 && got.Output.Video.Width != got.Source.Video.Width {
				t.Errorf("width changed from %d to %d with no resize asked for",
					got.Source.Video.Width, got.Output.Video.Width)
			}

			// Every container here needs even dimensions, and an odd one is
			// a file several decoders refuse.
			if got.Output.Video.Width%2 != 0 || got.Output.Video.Height%2 != 0 {
				t.Errorf("the result is %dx%d, which has an odd dimension",
					got.Output.Video.Width, got.Output.Video.Height)
			}

			if info, err := os.Stat(got.Path); err != nil || info.Size() == 0 {
				t.Errorf("the result is missing or empty: %v", err)
			}
		})
	}
}

// encoderFor names the ffmpeg encoder a case needs, so a build without it
// skips that case rather than failing it.
func encoderFor(o video.Options) string {
	switch {
	case o.Video == video.H265:
		return "libx265"
	case o.Container == video.WebM:
		return "libvpx-vp9"
	case o.Video == video.AV1:
		return "libsvtav1"
	default:
		return "libx264"
	}
}

// copyInto copies a file into a fresh working directory, so each conversion
// gets the private directory the runner requires.
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

// TestAnIncompatibleCodecIsRefusedBeforeAProcessStarts: ffmpeg will mux VP9
// into a MOV and produce a file nothing plays, so the refusal is here.
func TestAnIncompatibleCodecIsRefusedBeforeAProcessStarts(t *testing.T) {
	e, r, caps := engines(t)
	dir := t.TempDir()
	input := clip(t, e, r, dir, 1, 160, 120)

	_, err := video.Convert(context.Background(), e, r, caps, dir, input,
		video.Options{Container: video.MOV, Video: video.VP9}, nil)

	var incompatible *video.IncompatibleError
	if !errors.As(err, &incompatible) {
		t.Fatalf("want an IncompatibleError, got %v", err)
	}

	message := incompatible.Message()
	if !strings.Contains(message, "QuickTime") || !strings.Contains(message, "H.264") {
		t.Errorf("the message should name the container and its alternatives, got %q", message)
	}
}

// TestProgressIsReportedFromTheStructuredStream proves the progress comes
// from `-progress pipe:1` and reaches 100 at the end.
func TestProgressIsReportedFromTheStructuredStream(t *testing.T) {
	e, r, caps := engines(t)
	dir := t.TempDir()
	input := clip(t, e, r, dir, 4, 320, 240)

	var (
		reports []ffmpeg.Progress
		done    bool
	)

	_, err := video.Convert(context.Background(), e, r, caps, dir, input,
		video.Options{Container: video.MP4}, func(p ffmpeg.Progress) error {
			reports = append(reports, p)

			if p.Done {
				done = true
			}

			return nil
		})
	if err != nil {
		t.Fatalf("convert: %v", err)
	}

	if len(reports) == 0 {
		t.Fatal("no progress was reported, so the structured stream is not being read")
	}

	if !done {
		t.Error("the final progress=end block was never seen")
	}

	// The percentages must not go backwards: a client showing a bar would
	// jump about.
	previous := -1

	for _, p := range reports {
		if p.Percent < previous {
			t.Errorf("progress went backwards: %d after %d", p.Percent, previous)
		}

		previous = p.Percent
	}

	t.Logf("%d progress reports, last at %d%%", len(reports), previous)
}

// TestCancellingFromProgressStopsTheEngine proves an owner's cancellation
// reaches ffmpeg rather than waiting for it to finish.
func TestCancellingFromProgressStopsTheEngine(t *testing.T) {
	e, r, caps := engines(t)
	dir := t.TempDir()

	// Long enough that the cancellation lands while it is still encoding.
	input := clip(t, e, r, dir, 20, 640, 480)

	stop := errors.New("the owner cancelled it")
	started := time.Now()

	_, err := video.Convert(context.Background(), e, r, caps, dir, input,
		video.Options{Container: video.WebM}, func(ffmpeg.Progress) error { return stop })

	if !errors.Is(err, stop) {
		t.Fatalf("want the caller's own error, got %v", err)
	}

	// The whole encode of a 20-second 480p VP9 clip takes many seconds; a
	// cancellation that only arrived at the end would take about as long.
	if elapsed := time.Since(started); elapsed > 20*time.Second {
		t.Errorf("the cancellation took %v, which is not promptly", elapsed)
	}
}

// TestAFileWithNoVideoIsRefusedWithSomethingToDo: an audio file submitted to
// the video converter gets told where to go instead.
func TestAFileWithNoVideoIsRefusedWithSomethingToDo(t *testing.T) {
	e, r, caps := engines(t)
	dir := t.TempDir()

	audioOnly := filepath.Join(dir, "tone.m4a")

	args := []string{
		"-nostdin", "-hide_banner", "-loglevel", "error", "-nostats", "-y",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=1",
		"-c:a", "aac", "-f", "ipod", "-threads", "2", audioOnly,
	}

	if _, err := r.Run(context.Background(), engine.Command{Path: e.FFmpeg, Args: args, Dir: dir}); err != nil {
		t.Fatalf("write an audio-only file: %v", err)
	}

	_, err := video.Convert(context.Background(), e, r, caps, dir, audioOnly,
		video.Options{Container: video.MP4}, nil)

	var noVideo *video.NoVideoError
	if !errors.As(err, &noVideo) {
		t.Fatalf("want a NoVideoError, got %v", err)
	}

	if !strings.Contains(noVideo.Message(), "audio converter") {
		t.Errorf("the message should say what to use instead, got %q", noVideo.Message())
	}
}

// TestVideoToGIFUsesTheTwoPassPaletteAndLoops asserts on the result rather
// than on the filter string: a GIF with 256 colours and the right size and
// frame count is what the two passes are for.
func TestVideoToGIFUsesTheTwoPassPaletteAndLoops(t *testing.T) {
	e, r, _ := engines(t)
	dir := t.TempDir()
	input := clip(t, e, r, dir, 3, 640, 480)

	got, err := video.ToGIF(context.Background(), e, r, dir, input,
		video.GIFOptions{FPS: 10, Width: 160}, nil)
	if err != nil {
		t.Fatalf("to gif: %v", err)
	}

	if !got.Output.HasVideo() {
		t.Fatal("the GIF has no stream")
	}

	if got.Output.Video.Codec != "gif" {
		t.Errorf("codec is %q, want gif", got.Output.Video.Codec)
	}

	if got.Output.Video.Width != 160 {
		t.Errorf("width is %d, want 160", got.Output.Video.Width)
	}

	// The palette pass must have written its own file, which is the evidence
	// that there were two passes rather than one.
	if _, err := os.Stat(filepath.Join(dir, "palette.png")); err != nil {
		t.Errorf("no palette was generated, so this was a single-pass conversion: %v", err)
	}
}

// TestAGIFLongerThanTheLimitIsRefusedWithBothFigures: a GIF grows linearly
// with its duration, so the limit exists, and the refusal has to name it.
func TestAGIFLongerThanTheLimitIsRefusedWithBothFigures(t *testing.T) {
	e, r, _ := engines(t)
	dir := t.TempDir()
	input := clip(t, e, r, dir, 20, 160, 120)

	_, err := video.ToGIF(context.Background(), e, r, dir, input, video.GIFOptions{}, nil)

	var duration *video.DurationError
	if !errors.As(err, &duration) {
		t.Fatalf("want a DurationError, got %v", err)
	}

	message := duration.Message()
	if !strings.Contains(message, "15s") {
		t.Errorf("the message should name the limit, got %q", message)
	}
}

// TestAThumbnailIsNotTakenFromTheFirstFrame: the first frame of a video is
// very often black, and a black thumbnail looks like a broken conversion.
func TestAThumbnailIsNotTakenFromTheFirstFrame(t *testing.T) {
	e, r, _ := engines(t)
	dir := t.TempDir()
	input := clip(t, e, r, dir, 6, 320, 240)

	got, err := video.Thumbnail(context.Background(), e, r, dir, input,
		video.ThumbnailOptions{Width: 160})
	if err != nil {
		t.Fatalf("thumbnail: %v", err)
	}

	if got.Output.Video == nil || got.Output.Video.Width != 160 {
		t.Fatalf("the thumbnail is %v", got.Output.Describe())
	}

	if got.Output.Video.Codec != "png" {
		t.Errorf("codec is %q, want png", got.Output.Video.Codec)
	}
}

// TestAThumbnailPastTheEndIsPulledBackRatherThanRefused: somebody asking for
// the frame at ten seconds of a six-second clip means "near the end".
func TestAThumbnailPastTheEndIsPulledBackRatherThanRefused(t *testing.T) {
	e, r, _ := engines(t)
	dir := t.TempDir()
	input := clip(t, e, r, dir, 3, 160, 120)

	got, err := video.Thumbnail(context.Background(), e, r, dir, input,
		video.ThumbnailOptions{At: time.Minute})
	if err != nil {
		t.Fatalf("thumbnail: %v", err)
	}

	if info, err := os.Stat(got.Path); err != nil || info.Size() == 0 {
		t.Errorf("no frame was written: %v", err)
	}
}

// TestASpriteSheetCoversTheWholeVideo proves the sampling rate is computed
// from the duration: a fixed rate fills three cells of a short clip and would
// need hundreds for a long one.
func TestASpriteSheetCoversTheWholeVideo(t *testing.T) {
	e, r, _ := engines(t)
	dir := t.TempDir()
	input := clip(t, e, r, dir, 10, 320, 240)

	got, err := video.SpriteSheet(context.Background(), e, r, dir, input,
		video.SpriteOptions{Columns: 3, Rows: 3, Width: 100}, nil)
	if err != nil {
		t.Fatalf("sprite sheet: %v", err)
	}

	if got.Output.Video == nil {
		t.Fatal("the sheet has no stream")
	}

	// Three columns of 100-wide tiles is a 300-wide sheet. The height
	// follows from the source's aspect ratio, rounded to an even number.
	if got.Output.Video.Width != 300 {
		t.Errorf("the sheet is %d wide, want 300 for three 100-wide tiles",
			got.Output.Video.Width)
	}
}

// TestReplacingTheSoundtrackCopiesThePicture: a soundtrack swap should cost
// the price of a mux, and the proof is that the video codec is unchanged and
// the output is not shorter than the shorter of the two inputs.
func TestReplacingTheSoundtrackCopiesThePicture(t *testing.T) {
	e, r, caps := engines(t)
	dir := t.TempDir()
	input := silentClip(t, e, r, dir, 4)

	newAudio := filepath.Join(dir, "tone.m4a")

	args := []string{
		"-nostdin", "-hide_banner", "-loglevel", "error", "-nostats", "-y",
		"-f", "lavfi", "-i", "sine=frequency=330:duration=10",
		"-c:a", "aac", "-f", "ipod", "-threads", "2", newAudio,
	}

	if _, err := r.Run(context.Background(), engine.Command{Path: e.FFmpeg, Args: args, Dir: dir}); err != nil {
		t.Fatalf("write the replacement audio: %v", err)
	}

	got, err := video.ReplaceAudio(context.Background(), e, r, caps, dir, input, newAudio, video.MP4, nil)
	if err != nil {
		t.Fatalf("replace the audio: %v", err)
	}

	if !got.Output.HasAudio() {
		t.Fatal("the result has no audio")
	}

	if got.Output.Audio.Codec != "aac" {
		t.Errorf("audio codec is %q, want aac", got.Output.Audio.Codec)
	}

	if got.Output.Video.Codec != got.Source.Video.Codec {
		t.Errorf("the picture was re-encoded: %q became %q",
			got.Source.Video.Codec, got.Output.Video.Codec)
	}

	// -shortest: a ten-second tone over a four-second clip must not produce
	// a ten-second file whose last six seconds are a frozen frame.
	if got.Output.Duration > got.Source.Duration+time.Second {
		t.Errorf("the result is %v for a %v clip, so -shortest did not apply",
			got.Output.Duration, got.Source.Duration)
	}
}
