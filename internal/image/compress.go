package image

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/AlexGalhardo/vukem-tools/internal/engine"
)

// Compression: making a file smaller without changing what it is.
//
// The format never changes. That is image-convert's job, and a tool that
// returned a WebP to somebody who uploaded a PNG would break every link to
// the file they expected back. What changes is the encoder's settings.
//
// libvips does all of it, and nothing else is installed to help. The ROADMAP
// planned a chain of specialist optimisers — oxipng, jpegtran, pngquant,
// gifsicle, cwebp — and that is **not** what shipped: five more pinned
// packages on a 1.2 GB image, two of them GPL and so two more entries on
// /licenses, to beat what libvips already does by a few per cent. The
// decision and the figures are in docs/brainstorming.md; the seam is here if
// it is ever revisited, because every recipe below is one table entry.
//
// Two honest consequences of that choice, both surfaced rather than hidden:
// there is no lossless JPEG optimisation at all, because libvips has no such
// re-encode and jpegtran is what does it; and a file this encoder cannot beat
// comes back as itself with the fact stated, which happens more often with a
// single general-purpose encoder than with a specialist chain.

// CompressMode is how far a compression may move the pixels.
type CompressMode string

// The modes.
const (
	// CompressAuto picks the mode from the format: lossless where the format
	// has a lossless encoder worth running, lossy where it does not.
	CompressAuto CompressMode = ""
	// CompressLossless re-encodes without moving a pixel.
	CompressLossless CompressMode = "lossless"
	// CompressLossy re-encodes at a quality, which is how a photograph gets
	// appreciably smaller.
	CompressLossy CompressMode = "lossy"
)

// CompressOptions configures Compress.
type CompressOptions struct {
	// Mode is how far the pixels may move. Empty picks by format.
	Mode CompressMode
	// Quality is 1 to 100, for a lossy compression. Zero takes the format's
	// default.
	Quality int
	// Effort trades encode time for size, 0 to 10, meaning per format. Zero
	// takes the default, which for compression is the highest the format's
	// encoder supports: somebody asking for a smaller file has said what
	// they want traded.
	Effort int
}

// CompressResult is what a compression did.
type CompressResult struct {
	// Path is the file to serve. It is the **input's** path when Unchanged.
	Path string
	// Format is the input's format, which is also the output's.
	Format Format
	// Source and Output are the metadata either side. Output equals Source
	// when Unchanged.
	Source Metadata
	Output Metadata
	// BytesIn and BytesOut are the sizes. They are equal when Unchanged.
	BytesIn  int64
	BytesOut int64
	// Mode is what was actually done, never CompressAuto.
	Mode CompressMode
	// Unchanged reports that the re-encode was not smaller, so the original
	// was kept. It is a normal outcome, not a failure: a file that has
	// already been through a good encoder cannot be beaten by another pass,
	// and returning the larger re-encode would be the service making the
	// file worse.
	Unchanged bool
	// Did is the sentence to show: what ran, in which mode, and what it
	// saved. It names the real tool, because a report that names a tool that
	// did not run is a claim rather than a report.
	Did string
	// CPUTime and MaxRSS are what the engine spent.
	CPUTime int64
	MaxRSS  int64
}

// Saved returns how many bytes the compression removed, which is zero when
// the original was kept.
func (r CompressResult) Saved() int64 { return r.BytesIn - r.BytesOut }

// Percent returns the saving as a percentage of the original, 0 when the
// original was kept or empty.
func (r CompressResult) Percent() float64 {
	if r.BytesIn <= 0 {
		return 0
	}

	return float64(r.Saved()) * 100 / float64(r.BytesIn)
}

// CompressModeError reports a mode the format cannot offer.
type CompressModeError struct {
	Format Format
	Mode   CompressMode
	// Why is the reason, which is always about the encoder rather than about
	// the file.
	Why string
}

// Error implements error.
func (e *CompressModeError) Error() string {
	return fmt.Sprintf("image: %s compression is not available for %s: %s", e.Mode, e.Format, e.Why)
}

// Message is the sentence to show the person.
func (e *CompressModeError) Message() string {
	return fmt.Sprintf("%s cannot be compressed losslessly here: %s. Use lossy compression instead.",
		strings.ToUpper(string(e.Format)), e.Why)
}

// candidate is one encoder setting to try.
//
// A compressor that runs the encoder once and hands back whatever came out is
// not compressing, it is re-encoding. The settings that win depend on the
// picture: PNG's row filters make a gradient seventy times smaller and a
// field of noise two per cent larger, and a palette beats neither on a smooth
// gradient. So each mode lists what is worth trying, every one of them is
// run, and the smallest file wins. The measurements behind the lists are in
// docs/benchmarks.md.
type candidate struct {
	// name is what the report calls this setting.
	name string
	// opts are the libvips save options, without the quality.
	opts []string
	// quality is the default Q for this candidate, 0 when its encoder has
	// none.
	quality int
}

// recipe is one format's compression settings.
type recipe struct {
	// auto is the mode used when the caller names none.
	auto CompressMode
	// losslessWhy is empty when the format has a lossless mode here, and the
	// reason it does not otherwise.
	losslessWhy string
	// lossless and lossy are the candidates for each mode.
	//
	// The lossy list includes the lossless candidates where they can win,
	// which is not a contradiction: "lossy" is permission to move the pixels,
	// not an obligation, and handing back a larger file because the smaller
	// one happened to be lossless would be absurd.
	lossless []candidate
	lossy    []candidate
}

// recipes is the compression settings per format.
//
// Only the four formats the catalogue offers. A format absent from here is
// refused by name rather than compressed with a guess: the whole value of
// this tool is that it knows what to do with what it is given, and an encoder
// run with default settings on a format nobody chose settings for is not
// compression, it is a re-encode that may well be larger.
var recipes = map[Format]recipe{
	PNG: {
		auto: CompressLossless,
		lossless: []candidate{
			// libvips's pngsave defaults to no row filtering, which is
			// catastrophic on anything with structure: the project's own
			// gradient fixture is 150 527 bytes unfiltered and 2 026 bytes
			// with the filters on. `filter=all` asks libpng to pick the best
			// filter per row, which is what every other PNG encoder does.
			{name: "filtered", opts: []string{"compression=9", "effort=10", "palette=false", "filter=all"}},
			// And unfiltered, because on a field of noise the per-row filter
			// bytes are pure overhead and this wins by a couple of per cent.
			// Two encodes of one image is a cheap way to stop guessing.
			{name: "unfiltered", opts: []string{"compression=9", "effort=10", "palette=false", "filter=none"}},
		},
		lossy: []candidate{
			// The palette is what pngquant does, and it is where the large
			// wins on a screenshot or a logo come from. It is also much
			// *worse* than lossless on a smooth gradient, where dithering
			// adds noise that defeats the compressor — which is exactly why
			// the lossless candidates are in this list too.
			{
				name:    "palette",
				opts:    []string{"compression=9", "effort=10", "palette=true", "dither=1", "filter=all"},
				quality: 80,
			},
			{name: "filtered", opts: []string{"compression=9", "effort=10", "palette=false", "filter=all"}},
			{name: "unfiltered", opts: []string{"compression=9", "effort=10", "palette=false", "filter=none"}},
		},
	},
	JPEG: {
		auto: CompressLossy,
		losslessWhy: "libvips has no lossless JPEG re-encode, and jpegtran, which does, " +
			"is not installed in this image",
		lossy: []candidate{{
			name: "mozjpeg-style",
			// Every one of these is smaller at the same quality rather than
			// cheaper at a lower one.
			opts: []string{
				"optimize-coding=true",
				"trellis-quant=true",
				"overshoot-deringing=true",
				"optimize-scans=true",
				"interlace=true",
				"subsample-mode=auto",
			},
			quality: 80,
		}},
	},
	WebP: {
		// Lossy by default even though WebP has a lossless mode: the
		// overwhelming majority of WebP files in the world are already
		// lossy, and re-encoding one of those losslessly preserves its
		// compression artefacts at several times the size. Lossless is
		// available by asking, which is the right shape — somebody who knows
		// their WebP is lossless can say so.
		auto:     CompressLossy,
		lossless: []candidate{{name: "lossless", opts: []string{"lossless=true", "effort=6"}}},
		lossy: []candidate{
			{name: "lossy", opts: []string{"effort=6", "smart-subsample=true"}, quality: 80},
			{name: "lossless", opts: []string{"lossless=true", "effort=6"}},
		},
	},
	GIF: {
		auto: CompressLossless,
		// gifsave is palette-based already, so there is no lossy dial worth
		// exposing: the win is the encoder's own search at full effort.
		lossless: []candidate{{name: "re-palettised", opts: []string{"effort=10", "bitdepth=8"}}},
	},
}

// CompressFormats returns the formats this tool compresses, in a stable
// order, for the documentation and the interface.
func CompressFormats() []Format {
	out := make([]Format, 0, len(recipes))
	for _, f := range Formats() {
		if _, ok := recipes[f]; ok {
			out = append(out, f)
		}
	}

	return out
}

// CompressModes returns the modes a client may name.
func CompressModes() []CompressMode {
	return []CompressMode{CompressLossless, CompressLossy}
}

// Compress re-encodes an image in its own format, as small as this encoder
// can make it.
//
// The input's format is read from the file rather than taken from its name,
// so a `.png` holding a JPEG is compressed as the JPEG it is.
func Compress(
	ctx context.Context,
	e Engine,
	r Runner,
	dir, input string,
	o CompressOptions,
) (CompressResult, error) {
	if err := checkPath(input); err != nil {
		return CompressResult{}, err
	}

	source, err := Probe(ctx, e, r, dir, input)
	if err != nil {
		return CompressResult{}, err
	}

	if source.Pixels() > MaxPixels {
		return CompressResult{}, &DimensionError{Width: source.Width, Height: source.Height}
	}

	// From the loader libvips actually used, not from the file's name: a
	// `.png` holding a JPEG is compressed as the JPEG it is, and the name is
	// never the basis of a decision here.
	format, ok := FormatOfLoader(source.Loader)
	if !ok {
		return CompressResult{}, &UnsupportedError{Format: Format(source.Loader)}
	}

	rec, ok := recipes[format]
	if !ok {
		return CompressResult{}, &UnsupportedError{Format: format}
	}

	mode := o.Mode
	if mode == CompressAuto {
		mode = rec.auto
	}

	if mode == CompressLossless && rec.losslessWhy != "" {
		return CompressResult{}, &CompressModeError{Format: format, Mode: mode, Why: rec.losslessWhy}
	}

	tries := rec.lossless
	if mode == CompressLossy {
		tries = rec.lossy
	}

	if len(tries) == 0 {
		return CompressResult{}, &CompressModeError{
			Format: format, Mode: mode,
			Why: "its encoder is palette-based already, so there is no quality to trade",
		}
	}

	bytesIn, err := fileSize(input)
	if err != nil {
		return CompressResult{}, err
	}

	best, err := smallest(ctx, e, r, dir, input, format, tries, o)
	if err != nil {
		return CompressResult{}, err
	}

	output, bytesOut := best.path, best.size

	result := CompressResult{
		Format:  format,
		Source:  source,
		BytesIn: bytesIn,
		Mode:    mode,
		CPUTime: best.cpu,
		MaxRSS:  best.rss,
	}

	// The one rule this tool exists to keep: never hand back a bigger file
	// than the one that was uploaded. A general-purpose encoder loses to a
	// file that has already been through a specialist one, and when it does,
	// the right answer is the original and a sentence saying so.
	if bytesOut >= bytesIn {
		if err := os.Remove(output); err != nil && !os.IsNotExist(err) {
			return CompressResult{}, fmt.Errorf("image: remove the larger re-encode: %w", err)
		}

		result.Path = input
		result.Output = source
		result.BytesOut = bytesIn
		result.Unchanged = true
		result.Did = fmt.Sprintf(
			"libvips re-encoded this %s %s and could not beat it, "+
				"so the file is already as small as this can make it and comes back unchanged.",
			strings.ToUpper(string(format)), triedPhrase(tries))

		return result, nil
	}

	out, err := Probe(ctx, e, r, dir, output)
	if err != nil {
		return CompressResult{}, err
	}

	result.Path = output
	result.Output = out
	result.BytesOut = bytesOut
	result.Did = fmt.Sprintf(
		"libvips re-encoded this %s %s and kept the %s one, %s compression: "+
			"saved %d of %d bytes (%.1f%%).",
		strings.ToUpper(string(format)), triedPhrase(tries), best.name, mode,
		result.Saved(), bytesIn, result.Percent())

	return result, nil
}

// attempt is one candidate's output.
type attempt struct {
	name string
	path string
	size int64
	cpu  int64
	rss  int64
}

// smallest runs every candidate and returns the one that produced the
// smallest file, deleting the rest.
//
// Sequential rather than concurrent, deliberately. These run inside a worker
// that the pipeline has already sized to one conversion per available CPU
// (jobs.Concurrency), so a pool here would oversubscribe the container's
// quota and make every *other* conversion slower to finish this one sooner.
// The candidates are two or three encodes of one image.
func smallest(
	ctx context.Context,
	e Engine,
	r Runner,
	dir, input string,
	format Format,
	tries []candidate,
	o CompressOptions,
) (attempt, error) {
	var best attempt

	for i, c := range tries {
		if err := ctx.Err(); err != nil {
			return attempt{}, err
		}

		path := filepath.Join(dir, "compressed-"+strconv.Itoa(i)+format.Extension())

		// `copy` rather than `thumbnail`: nothing is being resized, and
		// thumbnail's shrink-on-load would re-sample an image whose pixels
		// this is supposed to preserve. The orientation and the colour
		// profile are left exactly as they are for the same reason — this is
		// a re-encode, not a conversion.
		res, err := r.Run(ctx, engine.Command{
			Path: e.Vips,
			Args: []string{"copy", input + "[access=sequential]", path + candidateSuffix(c, o)},
			Dir:  dir,
			Env:  e.env(),
		})
		if err != nil {
			return attempt{}, classify(err, "compress the image")
		}

		size, err := fileSize(path)
		if err != nil {
			return attempt{}, err
		}

		// The processor time is the sum: every candidate really was run, and
		// charging a quota for one of three would undercount what the
		// conversion cost.
		cpu := best.cpu + res.CPUTime.Milliseconds()

		if best.path == "" || size < best.size {
			if best.path != "" {
				if err := os.Remove(best.path); err != nil && !os.IsNotExist(err) {
					return attempt{}, fmt.Errorf("image: remove a losing candidate: %w", err)
				}
			}

			best = attempt{name: c.name, path: path, size: size, rss: max(best.rss, res.MaxRSS)}
		} else if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return attempt{}, fmt.Errorf("image: remove a losing candidate: %w", err)
		}

		best.cpu = cpu
	}

	return best, nil
}

// triedPhrase names what was tried, for the report.
func triedPhrase(tries []candidate) string {
	names := make([]string, 0, len(tries))
	for _, c := range tries {
		names = append(names, c.name)
	}

	if len(names) == 1 {
		return names[0]
	}

	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

// candidateSuffix is the libvips save options for one candidate.
func candidateSuffix(c candidate, o CompressOptions) string {
	// Metadata is always dropped. Somebody asking for a smaller file is
	// asking for the bytes that are not the picture to go, and a
	// photograph's EXIF routinely carries GPS coordinates and a device
	// serial number. image-convert is where keeping it is an option.
	opts := append([]string{"keep=none"}, c.opts...)

	if c.quality > 0 {
		opts = append(opts, "Q="+strconv.Itoa(clamp(positive(o.Quality, c.quality), 1, 100)))
	}

	// A caller's effort overrides the candidate's, which is why it is
	// appended last: libvips takes the final occurrence of a repeated option.
	if o.Effort > 0 && slices.ContainsFunc(opts, hasEffort) {
		opts = append(opts, "effort="+strconv.Itoa(clamp(o.Effort, 1, 10)))
	}

	return "[" + strings.Join(opts, ",") + "]"
}

// hasEffort reports whether a save option sets the effort, so a caller's
// effort is only appended for an encoder that has one.
func hasEffort(opt string) bool { return strings.HasPrefix(opt, "effort=") }

// fileSize is the size of a file in bytes.
func fileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, fmt.Errorf("image: measure %s: %w", filepath.Base(path), err)
	}

	return info.Size(), nil
}
