package editor_test

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/AlexGalhardo/vukem-tools/internal/editor"
	"github.com/AlexGalhardo/vukem-tools/internal/engine"
)

// The benchmarks here run the real ffmpeg, because the number they exist to
// produce — how much cheaper a copied cut is than a re-encoded one — is a
// property of ffmpeg and not of this package. They are what makes the
// copy-or-re-encode decision in Trim worth its complexity, and the figures
// are recorded in docs/benchmarks.md.

// benchEditor builds an editor on the engines found on PATH, skipping when
// they are not there.
func benchEditor(b *testing.B) (*editor.Editor, string) {
	b.Helper()

	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		b.Skip("ffmpeg is not installed; run this inside the image (docs/deployment.md)")
	}

	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		b.Skip("ffprobe is not installed; run this inside the image (docs/deployment.md)")
	}

	runner, err := engine.NewRunner(engine.RunnerOptions{Limits: engine.Limits{Timeout: 5 * time.Minute}})
	if err != nil {
		b.Fatalf("new runner: %v", err)
	}

	e, err := editor.New(editor.Options{Runner: runner, FFmpeg: ffmpeg, FFprobe: ffprobe})
	if err != nil {
		b.Fatalf("new editor: %v", err)
	}

	return e, ffmpeg
}

// benchSource generates the clip both benchmarks cut, once, outside the
// timed loop.
func benchSource(b *testing.B, ffmpeg, dir string) string {
	b.Helper()

	path := filepath.Join(dir, "source.mp4")

	cmd := exec.CommandContext(b.Context(), ffmpeg,
		"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=duration=30:size=1280x720:rate=30",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=30",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
		"-g", "30", "-keyint_min", "30", "-sc_threshold", "0",
		"-c:a", "aac", "-shortest",
		path,
	)
	cmd.Dir = dir

	if out, err := cmd.CombinedOutput(); err != nil {
		b.Fatalf("generate the source video: %v: %s", err, out)
	}

	return path
}

// BenchmarkTrimCopy cuts ten seconds out of a 720p clip without re-encoding.
func BenchmarkTrimCopy(b *testing.B) {
	e, ffmpeg := benchEditor(b)
	dir := b.TempDir()
	source := benchSource(b, ffmpeg, dir)

	b.ResetTimer()

	for i := range b.N {
		if _, err := e.Trim(b.Context(), editor.TrimRequest{
			Dir:      dir,
			Input:    source,
			Output:   "copy" + strconv.Itoa(i) + ".mp4",
			Start:    5 * time.Second,
			Duration: 10 * time.Second,
		}); err != nil {
			b.Fatalf("trim: %v", err)
		}
	}
}

// BenchmarkTrimReEncode cuts the same ten seconds with frame accuracy, which
// forces the re-encode.
func BenchmarkTrimReEncode(b *testing.B) {
	e, ffmpeg := benchEditor(b)
	dir := b.TempDir()
	source := benchSource(b, ffmpeg, dir)

	b.ResetTimer()

	for i := range b.N {
		if _, err := e.Trim(b.Context(), editor.TrimRequest{
			Dir:           dir,
			Input:         source,
			Output:        "encode" + strconv.Itoa(i) + ".mp4",
			Start:         5 * time.Second,
			Duration:      10 * time.Second,
			FrameAccurate: true,
		}); err != nil {
			b.Fatalf("trim: %v", err)
		}
	}
}
