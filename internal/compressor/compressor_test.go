package compressor_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/AlexGalhardo/vukem-tools/internal/compressor"
	"github.com/AlexGalhardo/vukem-tools/internal/editor"
	"github.com/AlexGalhardo/vukem-tools/internal/engine"
)

const (
	ffmpegPath = "/usr/bin/ffmpeg"
	gsPath     = "/usr/bin/gs"
)

// fakeRunner answers every engine invocation from a handler.
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

func (f *fakeRunner) last(t *testing.T) engine.Command {
	t.Helper()

	if len(f.calls) == 0 {
		t.Fatal("the runner was never called")
	}

	return f.calls[len(f.calls)-1]
}

// writes returns a handler that produces an output of the given size, which
// is how a fake engine either beats the original or does not.
func writes(size int) func(engine.Command) (engine.Result, error) {
	return func(cmd engine.Command) (engine.Result, error) {
		name := outputName(cmd)
		if name == "" {
			return engine.Result{}, errors.New("no output in the command line")
		}

		return engine.Result{CPUTime: 500 * time.Millisecond},
			os.WriteFile(filepath.Join(cmd.Dir, name), make([]byte, size), 0o600)
	}
}

// outputName reads the file the engine was told to write: the last argument
// for ffmpeg, and -sOutputFile for Ghostscript.
func outputName(cmd engine.Command) string {
	for _, a := range cmd.Args {
		if after, found := strings.CutPrefix(a, "-sOutputFile="); found {
			return filepath.Base(after)
		}
	}

	if len(cmd.Args) == 0 {
		return ""
	}

	return filepath.Base(cmd.Args[len(cmd.Args)-1])
}

func newCompressor(t *testing.T, r *fakeRunner, gs string) *compressor.Compressor {
	t.Helper()

	c, err := compressor.New(compressor.Options{Runner: r, FFmpeg: ffmpegPath, Ghostscript: gs})
	if err != nil {
		t.Fatalf("new compressor: %v", err)
	}

	return c
}

// writeInput makes an input file of the given size.
func writeInput(t *testing.T, dir, name string, size int) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, make([]byte, size), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

func TestVideoCompressionReportsTheRealSaving(t *testing.T) {
	t.Parallel()

	const (
		original  = 10 << 20
		compressd = 4 << 20
	)

	runner := &fakeRunner{handle: writes(compressd)}
	c := newCompressor(t, runner, "")

	dir := t.TempDir()

	result, err := c.Video(t.Context(), compressor.Request{
		Dir:    dir,
		Input:  writeInput(t, dir, "input.mp4", original),
		Output: "compressed.mp4",
	})
	if err != nil {
		t.Fatalf("video: %v", err)
	}

	if !result.Smaller {
		t.Fatal("a smaller result was reported as not smaller")
	}

	// The ROADMAP's assert: the reported MB and percentage match the real
	// byte sizes.
	if result.Bytes != compressd || result.OriginalBytes != original {
		t.Fatalf("bytes %d of %d, want %d of %d", result.Bytes, result.OriginalBytes, compressd, original)
	}

	if want := int64(original - compressd); result.SavedBytes() != want {
		t.Errorf("saved %d bytes, want %d", result.SavedBytes(), want)
	}

	if want := 6.0; result.SavedMB() != want {
		t.Errorf("saved %v MB, want %v", result.SavedMB(), want)
	}

	if want := 60.0; result.SavedPercent() != want {
		t.Errorf("saved %v%%, want %v%%", result.SavedPercent(), want)
	}

	if !strings.Contains(result.Summary(), "6.00 MB") || !strings.Contains(result.Summary(), "60.0%") {
		t.Errorf("the summary does not match the figures: %q", result.Summary())
	}

	args := runner.last(t).Args

	if !slices.Contains(args, "28") || !slices.Contains(args, "libx264") {
		t.Errorf("the video profile is not H.264 CRF 28: %v", args)
	}

	if !slices.Contains(args, "128k") || !slices.Contains(args, "aac") {
		t.Errorf("the audio profile is not AAC 128k: %v", args)
	}
}

func TestAFileThatCannotBeMadeSmallerIsReportedAsSuchAndLeavesNoArtefact(t *testing.T) {
	t.Parallel()

	const original = 1 << 20

	// The candidate came out larger, which is what happens to a clip
	// somebody already encoded at CRF 28.
	runner := &fakeRunner{handle: writes(original + 4096)}
	c := newCompressor(t, runner, "")

	dir := t.TempDir()
	input := writeInput(t, dir, "input.mp4", original)

	result, err := c.Video(t.Context(), compressor.Request{
		Dir:    dir,
		Input:  input,
		Output: "compressed.mp4",
	})
	if err != nil {
		t.Fatalf("video: %v", err)
	}

	if result.Smaller {
		t.Fatal("a larger result was reported as smaller")
	}

	if result.Path != input {
		t.Errorf("path %q, want the original %q", result.Path, input)
	}

	if result.Bytes != original || result.SavedBytes() != 0 || result.SavedPercent() != 0 {
		t.Errorf("result %+v, want the original reported unchanged", result)
	}

	if !strings.Contains(result.Summary(), "already as small") {
		t.Errorf("the summary does not say it could not be improved: %q", result.Summary())
	}

	// The ROADMAP's assert: no artefact is left behind.
	if _, err := os.Stat(filepath.Join(dir, "compressed.mp4")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the discarded candidate is still on disk: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 1 {
		t.Errorf("%d files in the working directory, want only the original", len(entries))
	}
}

func TestACandidateTheSameSizeIsNotKept(t *testing.T) {
	t.Parallel()

	// Equal is not smaller: re-encoding to the same size spent CPU and lost
	// quality, so the original wins.
	const original = 4096

	runner := &fakeRunner{handle: writes(original)}
	c := newCompressor(t, runner, "")

	dir := t.TempDir()

	result, err := c.Audio(t.Context(), compressor.Request{
		Dir:    dir,
		Input:  writeInput(t, dir, "input.mp3", original),
		Output: "compressed.mp3",
	})
	if err != nil {
		t.Fatalf("audio: %v", err)
	}

	if result.Smaller {
		t.Error("a candidate of exactly the same size was kept")
	}
}

func TestAudioCompressionAsksForMP3At128k(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{handle: writes(1024)}
	c := newCompressor(t, runner, "")

	dir := t.TempDir()

	if _, err := c.Audio(t.Context(), compressor.Request{
		Dir:    dir,
		Input:  writeInput(t, dir, "input.wav", 1<<20),
		Output: "compressed.mp3",
	}); err != nil {
		t.Fatalf("audio: %v", err)
	}

	args := runner.last(t).Args

	if !slices.Contains(args, "libmp3lame") || !slices.Contains(args, "128k") {
		t.Errorf("the audio profile is not MP3 128k: %v", args)
	}

	// Cover art re-encoded into an MP3 is the commonest reason a compressed
	// audio file comes out larger than it went in.
	if !slices.Contains(args, "-vn") {
		t.Errorf("the cover art is not dropped: %v", args)
	}
}

func TestPDFCompressionRunsGhostscriptSafelyAndResamplesTo150DPI(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{handle: writes(1 << 19)}
	c := newCompressor(t, runner, gsPath)

	dir := t.TempDir()

	result, err := c.PDF(t.Context(), compressor.Request{
		Dir:    dir,
		Input:  writeInput(t, dir, "input.pdf", 1<<20),
		Output: "compressed.pdf",
	})
	if err != nil {
		t.Fatalf("pdf: %v", err)
	}

	if !result.Smaller {
		t.Error("a smaller PDF was reported as not smaller")
	}

	cmd := runner.last(t)

	if cmd.Path != gsPath {
		t.Errorf("engine %q, want Ghostscript", cmd.Path)
	}

	// -dSAFER is the boundary around an untrusted document: Ghostscript's
	// input language can otherwise open files, write files and run commands.
	if !slices.Contains(cmd.Args, "-dSAFER") {
		t.Errorf("Ghostscript ran without -dSAFER: %v", cmd.Args)
	}

	// The job's own directory, never the server's: a PostScript program that
	// does escape must land somewhere that is deleted with the job.
	if cmd.Dir != dir {
		t.Errorf("working directory %q, want the job's %q", cmd.Dir, dir)
	}

	for _, want := range []string{
		"-dColorImageResolution=150",
		"-dGrayImageResolution=150",
		"-dDownsampleColorImages=true",
		"-sOutputFile=" + filepath.Join(dir, "compressed.pdf"),
	} {
		if !slices.Contains(cmd.Args, want) {
			t.Errorf("%s is missing from %v", want, cmd.Args)
		}
	}
}

func TestPDFCompressionIsUnavailableWithoutGhostscript(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{handle: writes(1024)}
	c := newCompressor(t, runner, "")

	if c.CanCompressPDF() {
		t.Error("PDF compression reported itself available without Ghostscript")
	}

	dir := t.TempDir()

	_, err := c.PDF(t.Context(), compressor.Request{
		Dir:    dir,
		Input:  writeInput(t, dir, "input.pdf", 1<<20),
		Output: "compressed.pdf",
	})

	if _, ok := errors.AsType[*compressor.CompressError](err); !ok {
		t.Fatalf("error %v, want a CompressError", err)
	}

	if len(runner.calls) != 0 {
		t.Error("an engine was started although Ghostscript is absent")
	}
}

func TestARequestThatWouldOverwriteItsOwnInputIsRefused(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{handle: writes(1024)}
	c := newCompressor(t, runner, "")

	dir := t.TempDir()

	_, err := c.Video(t.Context(), compressor.Request{
		Dir:    dir,
		Input:  writeInput(t, dir, "input.mp4", 1<<20),
		Output: "input.mp4",
	})

	if _, ok := errors.AsType[*compressor.CompressError](err); !ok {
		t.Fatalf("error %v, want a CompressError", err)
	}

	if len(runner.calls) != 0 {
		t.Error("ffmpeg was asked to read and write the same file")
	}
}

func TestAnEngineThatWroteNothingIsAFailure(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{}
	c := newCompressor(t, runner, "")

	dir := t.TempDir()

	_, err := c.Video(t.Context(), compressor.Request{
		Dir:    dir,
		Input:  writeInput(t, dir, "input.mp4", 1<<20),
		Output: "compressed.mp4",
	})

	var compressErr *compressor.CompressError
	if !errors.As(err, &compressErr) || compressErr.Reason != compressor.ReasonEngine {
		t.Fatalf("error %v, want an engine CompressError", err)
	}
}

// fakeProber answers with a fixed duration, which is what turns a position
// into a percentage.
type fakeProber struct{ duration time.Duration }

func (p fakeProber) Probe(context.Context, string, string) (editor.Media, error) {
	return editor.Media{Duration: p.duration, VideoCodec: "h264", Width: 1280, Height: 720}, nil
}

func TestProgressIsAPercentageOfTheInputDuration(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{handle: func(cmd engine.Command) (engine.Result, error) {
		cmd.Progress("out_time_us=15000000")
		cmd.Progress("progress=end")

		return writes(1024)(cmd)
	}}

	c, err := compressor.New(compressor.Options{
		Runner: runner,
		FFmpeg: ffmpegPath,
		Prober: fakeProber{duration: 60 * time.Second},
	})
	if err != nil {
		t.Fatalf("new compressor: %v", err)
	}

	dir := t.TempDir()

	var seen []int

	if _, err := c.Video(t.Context(), compressor.Request{
		Dir:      dir,
		Input:    writeInput(t, dir, "input.mp4", 1<<20),
		Output:   "compressed.mp4",
		Progress: func(p editor.Progress) { seen = append(seen, p.Percent) },
	}); err != nil {
		t.Fatalf("video: %v", err)
	}

	want := []int{25, 100}
	if len(seen) != len(want) || seen[0] != want[0] || seen[1] != want[1] {
		t.Fatalf("progress %v, want %v", seen, want)
	}
}

func TestACompressorNeedsARunnerAndFFmpeg(t *testing.T) {
	t.Parallel()

	if _, err := compressor.New(compressor.Options{FFmpeg: ffmpegPath}); err == nil {
		t.Error("a compressor was built without a runner")
	}

	if _, err := compressor.New(compressor.Options{Runner: &fakeRunner{}}); err == nil {
		t.Error("a compressor was built without ffmpeg")
	}
}

func TestGhostscriptFailuresAreClassifiedFromFixtures(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		stderr string
		reason compressor.Reason
	}{
		{
			name:   "an encrypted PDF",
			stderr: "   **** This file requires a password for access.",
			reason: compressor.ReasonEncrypted,
		},
		{
			name:   "a PDF that is not a PDF",
			stderr: "Error: /undefined in --nostringval--\nUnrecoverable error, exit code 1",
			reason: compressor.ReasonUnsupportedInput,
		},
		{
			name:   "Ghostscript running out of memory",
			stderr: "Error: /VMerror in --run--",
			reason: compressor.ReasonOutOfMemory,
		},
		{
			name:   "a full disk",
			stderr: "**** Unable to open the initial device: no space left on device",
			reason: compressor.ReasonDiskFull,
		},
		{
			name:   "ffmpeg refusing the input",
			stderr: "input.mp4: Invalid data found when processing input",
			reason: compressor.ReasonUnsupportedInput,
		},
		{
			name:   "nothing recognisable",
			stderr: "something nobody has written down",
			reason: compressor.ReasonEngine,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			got := compressor.Classify(&engine.ExitError{Code: 1, Stderr: c.stderr})

			if got == nil {
				t.Fatal("classified as success")
			}

			if got.Reason != c.reason {
				t.Fatalf("reason %q, want %q (stderr %q)", got.Reason, c.reason, c.stderr)
			}

			if got.Public == "" {
				t.Error("no sentence to show the user")
			}
		})
	}

	if got := compressor.Classify(engine.ErrTimeout); got.Reason != compressor.ReasonTimeout {
		t.Errorf("reason %q, want timeout", got.Reason)
	}

	if compressor.Classify(nil) != nil {
		t.Error("a successful compression was classified as a failure")
	}
}

func TestAnOutputNameThatIsAPathIsRefused(t *testing.T) {
	t.Parallel()

	// Output is joined onto the job's directory. Every caller in this
	// repository passes a constant, which is why the field being exported is
	// the hazard: the first caller to derive it from a title or an upload
	// name must not be able to write outside the job.
	for _, name := range []string{
		"../escaped.mp4",
		"../../etc/cron.d/x",
		"sub/dir.mp4",
		`sub\dir.mp4`,
		"/absolute.mp4",
		".",
		"..",
		"",
	} {
		runner := &fakeRunner{handle: writes(1024)}
		c := newCompressor(t, runner, gsPath)

		dir := t.TempDir()
		input := writeInput(t, dir, "input.mp4", 1<<20)

		if _, err := c.Video(t.Context(), compressor.Request{Dir: dir, Input: input, Output: name}); err == nil {
			t.Errorf("Video accepted the output name %q", name)
		}

		if _, err := c.PDF(t.Context(), compressor.Request{Dir: dir, Input: input, Output: name}); err == nil {
			t.Errorf("PDF accepted the output name %q", name)
		}

		if len(runner.calls) != 0 {
			t.Errorf("an engine was started for the output name %q", name)
		}
	}
}
