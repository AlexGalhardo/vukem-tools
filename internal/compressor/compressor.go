// Package compressor makes a file smaller, or says honestly that it could
// not.
//
// The second half of that sentence is the design. Every compressor
// eventually meets a file that is already optimal — an H.264 clip somebody
// else encoded at CRF 28, a PDF that is nothing but text, an MP3 already at
// 128k — and re-encoding it produces a *larger* file that is also worse.
// Returning that would be a bug the user pays for twice, in bytes and in
// quality, so the candidate output is measured against the input and thrown
// away when it did not win. The original is then returned unchanged, the
// saving is reported as zero, and the interface says so.
//
// Nothing here knows about jobs, conversions, HTTP or storage: it takes a
// directory, runs engines through internal/engine, and reports what it
// produced.
package compressor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/AlexGalhardo/vukem-tools/internal/editor"
	"github.com/AlexGalhardo/vukem-tools/internal/engine"
)

// Runner runs one engine under its limits. *engine.Runner satisfies it.
type Runner interface {
	Run(ctx context.Context, cmd engine.Command) (engine.Result, error)
}

// Prober reads a file's real shape, which is where the progress percentage
// gets its denominator. *editor.Editor satisfies it.
//
// It is an interface rather than a concrete editor so this package can be
// tested without one, and so the dependency runs one way: the compressor
// knows it needs a duration, not how ffprobe is spelled.
type Prober interface {
	Probe(ctx context.Context, dir, path string) (editor.Media, error)
}

// Options configures a Compressor. Runner and FFmpeg are required;
// Ghostscript is required only for PDF.
type Options struct {
	Runner Runner
	FFmpeg string
	// Ghostscript is the path of gs. Empty disables PDF compression rather
	// than failing construction: the audio and video compressors are
	// useful without it.
	Ghostscript string
	// Prober supplies the input's duration. Optional: without one the
	// progress percentage is unknown rather than wrong.
	Prober Prober
	Log    *slog.Logger
}

// Compressor drives ffmpeg and Ghostscript.
type Compressor struct {
	run    Runner
	ffmpeg string
	gs     string
	prober Prober
	log    *slog.Logger
}

// New returns a compressor.
func New(o Options) (*Compressor, error) {
	switch {
	case o.Runner == nil:
		return nil, errors.New("compressor: a runner is required")
	case o.FFmpeg == "":
		return nil, errors.New("compressor: the path of ffmpeg is required")
	}

	if o.Log == nil {
		o.Log = slog.Default()
	}

	return &Compressor{run: o.Runner, ffmpeg: o.FFmpeg, gs: o.Ghostscript, prober: o.Prober, log: o.Log}, nil
}

// CanCompressPDF reports whether Ghostscript is available, which is what the
// capabilities endpoint and the tools page ask.
func (c *Compressor) CanCompressPDF() bool { return c.gs != "" }

// Request is one compression.
type Request struct {
	// Dir is the working directory. Input must be inside it and the
	// candidate output is written into it.
	Dir string
	// Input is the file to compress.
	Input string
	// Output is the candidate's file name, relative to Dir. It must differ
	// from the input's name, or the engine would be asked to overwrite what
	// it is reading.
	Output string
	// Progress receives each update. Optional.
	Progress func(editor.Progress)
}

// Result is a finished compression.
type Result struct {
	// Path is the file to deliver: the compressed candidate when it is
	// smaller, and the original input when it is not.
	Path string `json:"-"`
	// Bytes is the size of Path.
	Bytes int64 `json:"bytes"`
	// OriginalBytes is the size of the input.
	OriginalBytes int64 `json:"original_bytes"`
	// Smaller says whether compressing actually helped. When it is false,
	// Path is the input and nothing was kept.
	Smaller bool `json:"smaller"`
	// CPUTime is what the engine actually used, which is charged to the
	// account whether the result was kept or not: the work happened.
	CPUTime time.Duration `json:"-"`
}

// SavedBytes is how many bytes were saved, zero when nothing was.
func (r Result) SavedBytes() int64 {
	if !r.Smaller {
		return 0
	}

	return max(r.OriginalBytes-r.Bytes, 0)
}

// SavedPercent is the saving as a percentage of the original.
func (r Result) SavedPercent() float64 {
	if !r.Smaller || r.OriginalBytes <= 0 {
		return 0
	}

	return float64(r.SavedBytes()) / float64(r.OriginalBytes) * 100
}

// SavedMB is the saving in megabytes, which is the figure the interface
// shows.
func (r Result) SavedMB() float64 { return float64(r.SavedBytes()) / float64(1<<20) }

// Summary is the sentence to show. It is the one place the wording of "it did
// not get smaller" lives, so the API, the interface and the bot cannot word
// it differently.
func (r Result) Summary() string {
	if !r.Smaller {
		return "That file is already as small as this tool can make it, so the original is unchanged."
	}

	return fmt.Sprintf("Saved %s MB (%s%%): %s down to %s.",
		strconv.FormatFloat(r.SavedMB(), 'f', 2, 64),
		strconv.FormatFloat(r.SavedPercent(), 'f', 1, 64),
		humanBytes(r.OriginalBytes), humanBytes(r.Bytes))
}

// humanBytes renders a size the way a person reads it.
func humanBytes(n int64) string {
	const unit = 1 << 20

	if n < unit {
		return strconv.FormatInt(max(n, 0), 10) + " bytes"
	}

	return strconv.FormatFloat(float64(n)/float64(unit), 'f', 1, 64) + " MB"
}

// Video compresses to MP4: H.264 at CRF 28 with AAC at 128k.
//
// CRF 28 is the ROADMAP's figure and is the right one for this job: it is
// visibly lossy on a still frame examined closely and invisible on anything
// moving, which is the trade somebody who asked for a smaller file has
// already accepted.
func (c *Compressor) Video(ctx context.Context, in Request) (Result, error) {
	return c.ffmpegPass(ctx, in, []string{
		"-c:v", "libx264",
		"-preset", "veryfast",
		"-crf", "28",
		"-pix_fmt", "yuv420p",
		"-c:a", "aac",
		"-b:a", "128k",
		"-movflags", "+faststart",
	})
}

// Audio compresses to MP3 at 128k.
func (c *Compressor) Audio(ctx context.Context, in Request) (Result, error) {
	return c.ffmpegPass(ctx, in, []string{
		// -vn drops the cover art. Keeping it would re-encode a JPEG into
		// the MP3 and is the commonest reason an "compressed" audio file
		// comes out larger than it went in.
		"-vn",
		"-c:a", "libmp3lame",
		"-b:a", "128k",
	})
}

// ffmpegPass runs one ffmpeg encode and decides whether to keep it.
func (c *Compressor) ffmpegPass(ctx context.Context, in Request, encode []string) (Result, error) {
	if err := in.validate(); err != nil {
		return Result{}, err
	}

	original, err := sizeOf(in.Input)
	if err != nil {
		return Result{}, err
	}

	var total time.Duration

	if c.prober != nil {
		if media, probeErr := c.prober.Probe(ctx, in.Dir, in.Input); probeErr == nil {
			total = media.Duration
		}
	}

	candidate := filepath.Join(in.Dir, in.Output)

	args := []string{
		"-hide_banner",
		"-nostdin",
		"-nostats",
		"-loglevel", "error",
		"-y",
		"-progress", "pipe:1",
		"-i", in.Input,
	}
	args = append(args, encode...)
	args = append(args, candidate)

	result, err := c.run.Run(ctx, engine.Command{
		Path:     c.ffmpeg,
		Args:     args,
		Dir:      in.Dir,
		Progress: progressReader(total, in.Progress),
	})
	if err != nil {
		return Result{}, Classify(err)
	}

	return c.decide(ctx, in.Input, candidate, original, result.CPUTime)
}

// PDF compresses a PDF through Ghostscript, resampling its images to roughly
// 150 dpi.
func (c *Compressor) PDF(ctx context.Context, in Request) (Result, error) {
	if c.gs == "" {
		return Result{}, fail(ReasonEngine, "PDF compression is not available on this server.",
			"Ghostscript is not installed", nil)
	}

	if err := in.validate(); err != nil {
		return Result{}, err
	}

	original, err := sizeOf(in.Input)
	if err != nil {
		return Result{}, err
	}

	candidate := filepath.Join(in.Dir, in.Output)

	result, err := c.run.Run(ctx, engine.Command{
		Path: c.gs,
		Args: ghostscriptArgs(in.Input, candidate),
		Dir:  in.Dir,
	})
	if err != nil {
		return Result{}, Classify(err)
	}

	return c.decide(ctx, in.Input, candidate, original, result.CPUTime)
}

// imageDPI is what raster images inside a PDF are resampled to.
//
// 150 is the ROADMAP's figure and is the sweet spot for a document that will
// be read on a screen and occasionally printed: 72 is visibly soft in print,
// and 300 saves almost nothing over the scans people actually upload.
const imageDPI = "150"

// monoDPI is for the 1-bit images a scanner produces. They are left at a
// higher resolution than the colour ones because downsampling bilevel text
// to 150 dpi turns it into mush, and it compresses so well that there is
// nothing to gain.
const monoDPI = "300"

// ghostscriptArgs builds the command line.
//
// -dSAFER is the one argument here that is not about size. Ghostscript's
// input language can open files, write files and run commands; -dSAFER
// restricts it to the files named on the command line. A PDF is an untrusted
// document from a stranger, and this service's whole job is running strangers'
// documents through it, so the flag is the boundary rather than a precaution.
// It is on by default in Ghostscript 9.50 and later; it is passed explicitly
// because the default is not the contract.
func ghostscriptArgs(input, output string) []string {
	return []string{
		"-sDEVICE=pdfwrite",
		"-dCompatibilityLevel=1.7",
		// /ebook is the preset that targets a screen-readable document. The
		// explicit resolutions below override its image settings, so the
		// preset only supplies the font and transparency handling.
		"-dPDFSETTINGS=/ebook",
		"-dNOPAUSE",
		"-dBATCH",
		"-dQUIET",
		"-dSAFER",
		"-dNOOUTERSAVE",
		"-dDetectDuplicateImages=true",
		"-dDownsampleColorImages=true",
		"-dColorImageDownsampleType=/Bicubic",
		"-dColorImageResolution=" + imageDPI,
		"-dDownsampleGrayImages=true",
		"-dGrayImageDownsampleType=/Bicubic",
		"-dGrayImageResolution=" + imageDPI,
		"-dDownsampleMonoImages=true",
		"-dMonoImageDownsampleType=/Subsample",
		"-dMonoImageResolution=" + monoDPI,
		"-sOutputFile=" + output,
		input,
	}
}

// validate refuses a request that cannot work.
func (in Request) validate() error {
	switch {
	case in.Dir == "":
		return fail(ReasonEngine, "That file could not be compressed.",
			"a compression needs its own working directory", nil)
	case in.Input == "":
		return fail(ReasonEngine, "That file could not be compressed.", "no input was given", nil)
	case in.Output == "":
		return fail(ReasonEngine, "That file could not be compressed.", "no output name was given", nil)
	case filepath.Base(in.Input) == in.Output:
		// The engine would be reading and writing the same file. ffmpeg
		// refuses this with a prompt that -y turns into a truncated input.
		return fail(ReasonEngine, "That file could not be compressed.",
			"the output name is the same as the input's", nil)
	}

	return nil
}

// decide keeps the candidate when it is smaller and throws it away when it is
// not.
//
// Throwing it away matters beyond tidiness: the ROADMAP asks that a file which
// cannot be made smaller leaves no artefact behind, and a candidate left in
// the working directory would be picked up by whatever scans that directory
// for results.
func (c *Compressor) decide(
	ctx context.Context,
	input, candidate string,
	original int64,
	cpu time.Duration,
) (Result, error) {
	size, err := sizeOf(candidate)
	if err != nil {
		return Result{}, err
	}

	if size < original {
		return Result{
			Path:          candidate,
			Bytes:         size,
			OriginalBytes: original,
			Smaller:       true,
			CPUTime:       cpu,
		}, nil
	}

	if err := os.Remove(candidate); err != nil && !errors.Is(err, os.ErrNotExist) {
		// The result is still correct — the original is what gets delivered
		// — so this is logged rather than returned. The job's working
		// directory is removed when the job ends either way.
		c.log.WarnContext(ctx, "compressor: could not remove a candidate that was not smaller",
			"path", candidate, "err", err)
	}

	return Result{
		Path:          input,
		Bytes:         original,
		OriginalBytes: original,
		Smaller:       false,
		CPUTime:       cpu,
	}, nil
}

// sizeOf stats a file an engine was supposed to write.
func sizeOf(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, fail(ReasonEngine, "That file could not be compressed.",
			"the engine wrote no output: "+err.Error(), err)
	}

	if info.Size() == 0 {
		return 0, fail(ReasonEngine, "That file could not be compressed.",
			"the engine wrote an empty file", nil)
	}

	return info.Size(), nil
}

// progressReader returns the line handler for engine.Command.Progress, or nil
// when nobody is listening.
//
// The parsing is internal/editor's, because it is ffmpeg's progress stream in
// both packages and two copies of the out_time_ms misnomer is one copy too
// many.
func progressReader(total time.Duration, report func(editor.Progress)) func(string) {
	if report == nil {
		return nil
	}

	return func(line string) {
		p, ok := editor.ParseProgress(line)
		if !ok {
			return
		}

		if !p.Done && total > 0 {
			p.Percent = min(max(int(float64(p.OutTime)/float64(total)*100), 0), 100)
		}

		report(p)
	}
}
