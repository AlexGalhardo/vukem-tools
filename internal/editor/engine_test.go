package editor_test

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/AlexGalhardo/vukem-tools/internal/editor"
	"github.com/AlexGalhardo/vukem-tools/internal/engine"
)

// The tests in this file run the real ffmpeg and ffprobe.
//
// Everything else in this package uses a fake runner, which proves the
// command lines and the decisions but cannot prove that ffmpeg agrees with
// them. These do: the output's duration and resolution are read back with
// ffprobe rather than eyeballed, which is what the ROADMAP asks for. They
// skip where the engines are absent, so a machine without ffmpeg still runs
// the rest of the suite rather than failing it.

// realEditor builds an editor on the engines found on PATH, skipping when
// they are not there.
func realEditor(t *testing.T) (*editor.Editor, string, string) {
	t.Helper()

	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not installed; run this inside the image (docs/deployment.md)")
	}

	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe is not installed; run this inside the image (docs/deployment.md)")
	}

	runner, err := engine.NewRunner(engine.RunnerOptions{
		Limits: engine.Limits{Timeout: 3 * time.Minute},
	})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	e, err := editor.New(editor.Options{
		Runner:            runner,
		FFmpeg:            ffmpeg,
		FFprobe:           ffprobe,
		MaxOutputDuration: 10 * time.Minute,
	})
	if err != nil {
		t.Fatalf("new editor: %v", err)
	}

	return e, ffmpeg, ffprobe
}

// sourceVideo generates a test clip with ffmpeg: no fixture to keep in the
// repository, and the keyframe interval is under this test's control, which
// is the thing the copy-or-re-encode decision turns on.
//
// gop is the keyframe interval in frames at 30fps, so gop=30 is a keyframe
// every second.
func sourceVideo(t *testing.T, ffmpeg, dir string, seconds, gop int) string {
	t.Helper()

	path := filepath.Join(dir, "source.mp4")

	cmd := exec.CommandContext(t.Context(), ffmpeg,
		"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=duration="+strconv.Itoa(seconds)+":size=640x360:rate=30",
		"-f", "lavfi", "-i", "sine=frequency=440:duration="+strconv.Itoa(seconds),
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
		"-g", strconv.Itoa(gop), "-keyint_min", strconv.Itoa(gop), "-sc_threshold", "0",
		"-c:a", "aac", "-shortest",
		path,
	)
	cmd.Dir = dir

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate the source video: %v: %s", err, out)
	}

	return path
}

func TestATrimProducesTheDurationThatWasAskedFor(t *testing.T) {
	t.Parallel()

	e, ffmpeg, _ := realEditor(t)
	dir := t.TempDir()
	source := sourceVideo(t, ffmpeg, dir, 12, 30)

	result, err := e.Trim(t.Context(), editor.TrimRequest{
		Dir:      dir,
		Input:    source,
		Output:   "trimmed.mp4",
		Start:    2 * time.Second,
		Duration: 4 * time.Second,
	})
	if err != nil {
		t.Fatalf("trim: %v", err)
	}

	// The cut is on a keyframe (one per second), so it must have copied.
	if result.ReEncoded {
		t.Error("a cut on a keyframe re-encoded")
	}

	// Asserted with ffprobe, not eyeballed.
	measured, err := e.Probe(t.Context(), dir, result.Path)
	if err != nil {
		t.Fatalf("probe the result: %v", err)
	}

	if diff := absolute(measured.Duration - 4*time.Second); diff > 250*time.Millisecond {
		t.Errorf("duration %s, want 4s (off by %s)", measured.Duration, diff)
	}

	if measured.Width != 640 || measured.Height != 360 {
		t.Errorf("resolution %s, want 640x360", measured.Dimensions())
	}
}

func TestAFrameAccurateTrimProducesTheDurationThatWasAskedFor(t *testing.T) {
	t.Parallel()

	e, ffmpeg, _ := realEditor(t)
	dir := t.TempDir()
	source := sourceVideo(t, ffmpeg, dir, 12, 30)

	result, err := e.Trim(t.Context(), editor.TrimRequest{
		Dir:           dir,
		Input:         source,
		Output:        "trimmed.mp4",
		Start:         2500 * time.Millisecond,
		Duration:      3 * time.Second,
		FrameAccurate: true,
	})
	if err != nil {
		t.Fatalf("trim: %v", err)
	}

	if !result.ReEncoded {
		t.Error("frame accuracy was asked for and the cut was copied")
	}

	measured, err := e.Probe(t.Context(), dir, result.Path)
	if err != nil {
		t.Fatalf("probe the result: %v", err)
	}

	// A re-encode is accurate to a frame, which at 30fps is 33 ms.
	if diff := absolute(measured.Duration - 3*time.Second); diff > 100*time.Millisecond {
		t.Errorf("duration %s, want 3s (off by %s)", measured.Duration, diff)
	}
}

func TestACropProducesTheResolutionThatWasAskedFor(t *testing.T) {
	t.Parallel()

	e, ffmpeg, _ := realEditor(t)
	dir := t.TempDir()
	source := sourceVideo(t, ffmpeg, dir, 6, 30)

	result, err := e.Trim(t.Context(), editor.TrimRequest{
		Dir:    dir,
		Input:  source,
		Output: "cropped.mp4",
		Crop:   editor.Crop{X: 40, Y: 20, Width: 320, Height: 180},
	})
	if err != nil {
		t.Fatalf("crop: %v", err)
	}

	measured, err := e.Probe(t.Context(), dir, result.Path)
	if err != nil {
		t.Fatalf("probe the result: %v", err)
	}

	if measured.Width != 320 || measured.Height != 180 {
		t.Errorf("resolution %s, want 320x180", measured.Dimensions())
	}
}

func TestACropBeyondTheRealStreamIsRefusedByTheRealProbe(t *testing.T) {
	t.Parallel()

	e, ffmpeg, _ := realEditor(t)
	dir := t.TempDir()
	source := sourceVideo(t, ffmpeg, dir, 4, 30)

	_, err := e.Trim(t.Context(), editor.TrimRequest{
		Dir:    dir,
		Input:  source,
		Output: "cropped.mp4",
		Crop:   editor.Crop{X: 0, Y: 0, Width: 1920, Height: 1080},
	})

	editErr := editor.Classify(err)
	if editErr == nil || editErr.Reason != editor.ReasonOutOfRange {
		t.Fatalf("error %v, want out_of_range", err)
	}

	if want := "640x360"; !strings.Contains(editErr.Public, want) {
		t.Errorf("the refusal does not name the real dimensions %s: %q", want, editErr.Public)
	}
}

func TestEveryClipIsAtMostTheSegmentLengthEvenWithSparseKeyframes(t *testing.T) {
	t.Parallel()

	e, ffmpeg, _ := realEditor(t)
	dir := t.TempDir()

	// A keyframe every 5 seconds against a 2-second segment: the segment
	// muxer copying streams cannot honour that, so this is the case that
	// forces the re-encode pass.
	source := sourceVideo(t, ffmpeg, dir, 12, 150)

	clips, err := e.Segment(t.Context(), editor.SegmentRequest{
		Dir:     dir,
		Input:   source,
		Segment: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("segment: %v", err)
	}

	if len(clips) < 5 {
		t.Fatalf("%d clips from 12 seconds at 2 seconds each, want at least 5", len(clips))
	}

	for i := range clips {
		// The reported duration is ffprobe's, read back out of the clip.
		if clips[i].Duration > 2*time.Second+250*time.Millisecond {
			t.Errorf("clip %d is %s, past the 2s bound", i, clips[i].Duration)
		}

		if clips[i].Bytes == 0 {
			t.Errorf("clip %d is empty", i)
		}
	}
}

// absolute is the absolute value of a duration.
func absolute(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}

	return d
}
