package editor_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/AlexGalhardo/vukem-tools/internal/editor"
	"github.com/AlexGalhardo/vukem-tools/internal/engine"
)

const (
	ffmpegPath  = "/usr/bin/ffmpeg"
	ffprobePath = "/usr/bin/ffprobe"
)

// fakeRunner answers every engine invocation from a handler. The real runner
// is tested in internal/engine; what matters here is the command lines this
// package builds and the decisions it makes from what ffprobe answered.
type fakeRunner struct {
	handle func(cmd engine.Command) (engine.Result, error)
	calls  []engine.Command
}

func (f *fakeRunner) Run(_ context.Context, cmd engine.Command) (engine.Result, error) {
	f.calls = append(f.calls, cmd)

	if f.handle == nil {
		return engine.Result{}, nil
	}

	return f.handle(cmd)
}

// ffmpegCalls returns every ffmpeg invocation, which is what a test asserting
// "no work was started" has to count.
func (f *fakeRunner) ffmpegCalls() []engine.Command {
	var out []engine.Command

	for i := range f.calls {
		if f.calls[i].Path == ffmpegPath {
			out = append(out, f.calls[i])
		}
	}

	return out
}

// probeJSON is an ffprobe document for a video of the given shape.
func probeJSON(width, height int, duration float64) string {
	return fmt.Sprintf(`{
		"format": {"format_name":"mov,mp4,m4a,3gp,3g2,mj2","duration":"%.3f","bit_rate":"2500000"},
		"streams": [
			{"codec_type":"video","codec_name":"h264","width":%d,"height":%d,"duration":"%.3f"},
			{"codec_type":"audio","codec_name":"aac"}
		]
	}`, duration, width, height, duration)
}

// keyframeJSON is an ffprobe packet listing with keyframes at the given
// times.
func keyframeJSON(times ...float64) string {
	parts := make([]string, 0, len(times))
	for _, t := range times {
		parts = append(parts, fmt.Sprintf(`{"pts_time":"%.3f","flags":"K_"}`, t))
	}

	return `{"packets":[` + strings.Join(parts, ",") + `]}`
}

// newEditor builds an editor on a fake runner.
func newEditor(t *testing.T, r *fakeRunner, maxOutput time.Duration) *editor.Editor {
	t.Helper()

	e, err := editor.New(editor.Options{
		Runner:            r,
		FFmpeg:            ffmpegPath,
		FFprobe:           ffprobePath,
		MaxOutputDuration: maxOutput,
	})
	if err != nil {
		t.Fatalf("new editor: %v", err)
	}

	return e
}

// writeInput makes a file for the editor to treat as the input.
func writeInput(t *testing.T, dir string) string {
	t.Helper()

	path := filepath.Join(dir, "input.mp4")
	if err := os.WriteFile(path, make([]byte, 4096), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

// probeAnd answers ffprobe with the given documents and ffmpeg by writing
// out, so one line sets up the common case.
func probeAnd(probe string, keyframes string, out string, size int) func(engine.Command) (engine.Result, error) {
	return func(cmd engine.Command) (engine.Result, error) {
		if cmd.Path == ffprobePath {
			body := probe
			if slices.Contains(cmd.Args, "-show_entries") {
				body = keyframes
			}

			_, err := cmd.Stdout.Write([]byte(body))

			return engine.Result{}, err
		}

		if out != "" {
			if err := os.WriteFile(filepath.Join(cmd.Dir, out), make([]byte, size), 0o600); err != nil {
				return engine.Result{}, err
			}
		}

		return engine.Result{CPUTime: 250 * time.Millisecond}, nil
	}
}

func TestProbeReadsTheRealShapeOfTheFile(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{handle: probeAnd(probeJSON(1280, 720, 61.5), "", "", 0)}
	e := newEditor(t, runner, 0)

	dir := t.TempDir()

	media, err := e.Probe(t.Context(), dir, writeInput(t, dir))
	if err != nil {
		t.Fatalf("probe: %v", err)
	}

	if media.Width != 1280 || media.Height != 720 {
		t.Errorf("dimensions %s, want 1280x720", media.Dimensions())
	}

	if media.Duration != 61500*time.Millisecond {
		t.Errorf("duration %s, want 1m1.5s", media.Duration)
	}

	if media.VideoCodec != "h264" || media.AudioCodec != "aac" {
		t.Errorf("codecs %q / %q", media.VideoCodec, media.AudioCodec)
	}

	if media.Bitrate != 2500000 {
		t.Errorf("bitrate %d", media.Bitrate)
	}

	if !media.HasVideo() || !media.HasAudio() {
		t.Error("a file with both streams reported one missing")
	}
}

func TestAFileWithNoUsableStreamIsRefused(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{handle: probeAnd(`{"format":{"format_name":"data"},"streams":[]}`, "", "", 0)}
	e := newEditor(t, runner, 0)

	dir := t.TempDir()

	_, err := e.Probe(t.Context(), dir, writeInput(t, dir))

	var editErr *editor.EditError
	if !errors.As(err, &editErr) || editErr.Reason != editor.ReasonUnsupportedInput {
		t.Fatalf("error %v, want unsupported_input", err)
	}
}

func TestACropBeyondTheStreamIsRefusedWithTheRealDimensions(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{handle: probeAnd(probeJSON(1280, 720, 30), keyframeJSON(0), "out.mp4", 1024)}
	e := newEditor(t, runner, 0)

	dir := t.TempDir()
	input := writeInput(t, dir)

	_, err := e.Trim(t.Context(), editor.TrimRequest{
		Dir:    dir,
		Input:  input,
		Output: "out.mp4",
		Crop:   editor.Crop{X: 100, Y: 100, Width: 1280, Height: 720},
	})

	var editErr *editor.EditError
	if !errors.As(err, &editErr) || editErr.Reason != editor.ReasonOutOfRange {
		t.Fatalf("error %v, want out_of_range", err)
	}

	// The ROADMAP's assert: the message names the real dimensions.
	if !strings.Contains(editErr.Public, "1280x720") {
		t.Errorf("the refusal does not name the real dimensions: %q", editErr.Public)
	}

	if len(runner.ffmpegCalls()) != 0 {
		t.Error("ffmpeg was started for a crop that cannot work")
	}
}

func TestACropInsideTheStreamIsAccepted(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{handle: probeAnd(probeJSON(1280, 720, 30), keyframeJSON(0), "out.mp4", 1024)}
	e := newEditor(t, runner, 0)

	dir := t.TempDir()

	result, err := e.Trim(t.Context(), editor.TrimRequest{
		Dir:    dir,
		Input:  writeInput(t, dir),
		Output: "out.mp4",
		Crop:   editor.Crop{X: 0, Y: 0, Width: 640, Height: 360},
	})
	if err != nil {
		t.Fatalf("trim: %v", err)
	}

	// A crop cannot be done while copying, so this has to re-encode.
	if !result.ReEncoded {
		t.Error("a crop was attempted with -c copy")
	}

	args := runner.ffmpegCalls()[0].Args

	if !slices.Contains(args, "crop=640:360:0:0") {
		t.Errorf("the crop filter is missing from %v", args)
	}
}

func TestAMaximumOutputDurationIsEnforcedBeforeAnyWorkStarts(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{handle: probeAnd(probeJSON(1920, 1080, 3600), keyframeJSON(0), "out.mp4", 1024)}
	e := newEditor(t, runner, 2*time.Minute)

	dir := t.TempDir()

	_, err := e.Trim(t.Context(), editor.TrimRequest{
		Dir:      dir,
		Input:    writeInput(t, dir),
		Output:   "out.mp4",
		Duration: 10 * time.Minute,
	})

	var editErr *editor.EditError
	if !errors.As(err, &editErr) || editErr.Reason != editor.ReasonTooLong {
		t.Fatalf("error %v, want too_long", err)
	}

	if len(runner.ffmpegCalls()) != 0 {
		t.Error("ffmpeg was started for an output that is longer than the limit")
	}

	if !strings.Contains(editErr.Public, "2m0s") {
		t.Errorf("the refusal does not name the limit: %q", editErr.Public)
	}
}

func TestAStartPastTheEndIsRefused(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{handle: probeAnd(probeJSON(640, 360, 10), keyframeJSON(0), "out.mp4", 1024)}
	e := newEditor(t, runner, 0)

	dir := t.TempDir()

	_, err := e.Trim(t.Context(), editor.TrimRequest{
		Dir:    dir,
		Input:  writeInput(t, dir),
		Output: "out.mp4",
		Start:  30 * time.Second,
	})

	var editErr *editor.EditError
	if !errors.As(err, &editErr) || editErr.Reason != editor.ReasonOutOfRange {
		t.Fatalf("error %v, want out_of_range", err)
	}
}

func TestACutOnAKeyframeCopiesAndACutBetweenKeyframesReEncodes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		start     time.Duration
		keyframes []float64
		reEncode  bool
	}{
		{
			name:      "exactly on a keyframe",
			start:     10 * time.Second,
			keyframes: []float64{8, 10, 12},
			reEncode:  false,
		},
		{
			name:      "within one frame of a keyframe",
			start:     10*time.Second + 20*time.Millisecond,
			keyframes: []float64{10},
			reEncode:  false,
		},
		{
			name:      "between keyframes",
			start:     10 * time.Second,
			keyframes: []float64{8, 13},
			reEncode:  true,
		},
		{
			name:      "no keyframe found in the window",
			start:     10 * time.Second,
			keyframes: nil,
			reEncode:  true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			runner := &fakeRunner{
				handle: probeAnd(probeJSON(1280, 720, 60), keyframeJSON(c.keyframes...), "out.mp4", 2048),
			}
			e := newEditor(t, runner, 0)

			dir := t.TempDir()

			result, err := e.Trim(t.Context(), editor.TrimRequest{
				Dir:      dir,
				Input:    writeInput(t, dir),
				Output:   "out.mp4",
				Start:    c.start,
				Duration: 5 * time.Second,
			})
			if err != nil {
				t.Fatalf("trim: %v", err)
			}

			if result.ReEncoded != c.reEncode {
				t.Fatalf("re-encoded=%v, want %v", result.ReEncoded, c.reEncode)
			}

			args := runner.ffmpegCalls()[0].Args
			copying := slices.Contains(args, "copy")

			if copying == c.reEncode {
				t.Errorf("the command line disagrees with the decision: %v", args)
			}

			if !c.reEncode && !slices.Contains(args, "-avoid_negative_ts") {
				t.Errorf("a copy without -avoid_negative_ts: %v", args)
			}

			if c.reEncode && !slices.Contains(args, "libx264") {
				t.Errorf("a re-encode without an encoder: %v", args)
			}
		})
	}
}

func TestFrameAccuracyForcesAReEncodeEvenOnAKeyframe(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{handle: probeAnd(probeJSON(1280, 720, 60), keyframeJSON(10), "out.mp4", 2048)}
	e := newEditor(t, runner, 0)

	dir := t.TempDir()

	result, err := e.Trim(t.Context(), editor.TrimRequest{
		Dir:           dir,
		Input:         writeInput(t, dir),
		Output:        "out.mp4",
		Start:         10 * time.Second,
		Duration:      5 * time.Second,
		FrameAccurate: true,
	})
	if err != nil {
		t.Fatalf("trim: %v", err)
	}

	if !result.ReEncoded {
		t.Error("frame accuracy was asked for and the cut was copied")
	}
}

func TestACutFromTheStartNeedsNoKeyframeProbe(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{handle: probeAnd(probeJSON(1280, 720, 60), keyframeJSON(0), "out.mp4", 2048)}
	e := newEditor(t, runner, 0)

	dir := t.TempDir()

	result, err := e.Trim(t.Context(), editor.TrimRequest{
		Dir:      dir,
		Input:    writeInput(t, dir),
		Output:   "out.mp4",
		Duration: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("trim: %v", err)
	}

	if result.ReEncoded {
		t.Error("a cut from zero re-encoded, although zero is always a keyframe")
	}

	for i := range runner.calls {
		if slices.Contains(runner.calls[i].Args, "-show_entries") {
			t.Error("the keyframes were listed for a cut at zero")
		}
	}
}

func TestTheTrimCarriesTheSourceShapeBack(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{handle: probeAnd(probeJSON(1920, 1080, 42), keyframeJSON(0), "out.mp4", 9999)}
	e := newEditor(t, runner, 0)

	dir := t.TempDir()

	result, err := e.Trim(t.Context(), editor.TrimRequest{
		Dir:    dir,
		Input:  writeInput(t, dir),
		Output: "out.mp4",
	})
	if err != nil {
		t.Fatalf("trim: %v", err)
	}

	if result.Source.Dimensions() != "1920x1080" {
		t.Errorf("source %s", result.Source.Dimensions())
	}

	if result.Bytes != 9999 {
		t.Errorf("bytes %d, want 9999", result.Bytes)
	}

	if result.CPUTime == 0 {
		t.Error("no CPU time was reported")
	}
}

func TestAnEditorNeedsBothEngines(t *testing.T) {
	t.Parallel()

	if _, err := editor.New(editor.Options{FFmpeg: ffmpegPath, FFprobe: ffprobePath}); err == nil {
		t.Error("an editor was built without a runner")
	}

	if _, err := editor.New(editor.Options{Runner: &fakeRunner{}, FFprobe: ffprobePath}); err == nil {
		t.Error("an editor was built without ffmpeg")
	}

	if _, err := editor.New(editor.Options{Runner: &fakeRunner{}, FFmpeg: ffmpegPath}); err == nil {
		t.Error("an editor was built without ffprobe")
	}
}
