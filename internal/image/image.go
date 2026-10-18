package image

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/AlexGalhardo/vukem-tools/internal/cpu"
	"github.com/AlexGalhardo/vukem-tools/internal/engine"
)

// Runner runs one engine subprocess under limits. engine.Runner satisfies it.
//
// It is an interface rather than the concrete runner so this package can be
// tested against a recorded command line without an engine installed, which
// is the only way the argument building is covered on a machine that has no
// libvips.
type Runner interface {
	Run(ctx context.Context, cmd engine.Command) (engine.Result, error)
}

// Engine is where the libvips programs are. Both paths come from the
// registry, which resolved them at boot and logged their versions.
type Engine struct {
	// Vips is the `vips` binary: every conversion and every edit.
	Vips string
	// VipsHeader is `vipsheader`: the metadata probe.
	VipsHeader string
}

// env is the environment libvips runs in.
//
// VIPS_CONCURRENCY is the one setting that matters here. libvips sizes its own
// thread pool from the host's core count, which inside a one-CPU container
// means dozens of threads fighting over one CPU and each holding its own
// region buffers — slower and far hungrier than a single thread. It is set
// from the container's quota, which is the same rule every pool in this app
// follows.
//
// VIPS_DISC_THRESHOLD makes libvips spill to a temporary file instead of RAM
// once an image would need more than this in memory, which is what keeps a
// large conversion inside the address-space limit rather than hitting it.
func (e Engine) env() []string {
	return []string{
		"VIPS_CONCURRENCY=" + strconv.Itoa(cpu.Limit()),
		"VIPS_DISC_THRESHOLD=" + strconv.Itoa(discThresholdMB) + "m",
		// Warnings on a malformed-but-readable file are useful in the owner's
		// log and must not reach a user, so they go to stderr, which the
		// runner keeps as an owner-only tail.
		"VIPS_WARNING=1",
	}
}

// discThresholdMB is when libvips starts spilling to disc rather than holding
// an image in memory. Below the address-space limit on purpose: spilling is
// slower, and hitting RLIMIT_AS is a failed conversion.
const discThresholdMB = 256

// MaxPixels is the largest frame this package will decode, in pixels.
//
// 4K is 8.3 megapixels; this allows a little over 8K, which covers every
// photograph and scan a person uploads while refusing the 60000x60000 header
// that a decompression bomb claims. It is checked against the header before
// anything is decoded, so a bomb costs one header read.
const MaxPixels = 40_000_000

// DimensionError reports an image whose frame is past MaxPixels.
type DimensionError struct {
	Width  int
	Height int
}

// Error implements error.
func (e *DimensionError) Error() string {
	return fmt.Sprintf("image: %dx%d is %.1f megapixels, past the %.0f megapixel limit",
		e.Width, e.Height, float64(e.Width)*float64(e.Height)/1e6, float64(MaxPixels)/1e6)
}

// Message is the sentence to show the user. It names the real size and the
// limit, which is the only form of this message somebody can act on.
func (e *DimensionError) Message() string {
	return fmt.Sprintf("That image is %dx%d, which is larger than the %.0f megapixel limit.",
		e.Width, e.Height, float64(MaxPixels)/1e6)
}

// UnsupportedError reports a format this build cannot read or write.
type UnsupportedError struct {
	Format Format
	// Writing distinguishes "cannot be read" from "cannot be written", which
	// are different answers: BMP can be read and never written.
	Writing bool
}

// Error implements error.
func (e *UnsupportedError) Error() string {
	if e.Writing {
		return "image: " + string(e.Format) + " cannot be written by this build"
	}

	return "image: " + string(e.Format) + " cannot be read by this build"
}

// Message is the sentence to show the user.
func (e *UnsupportedError) Message() string {
	if e.Writing {
		return e.Format.Label() + " is not a format this converter can produce."
	}

	return e.Format.Label() + " is not a format this converter can read."
}

// Resize is how a target size is reached.
type Resize string

// The resize modes. Each one answers a different question, and the difference
// matters enough that a single "resize" with a boolean would be wrong: fit
// never changes the framing, fill always does, and exact may change the
// proportions.
const (
	// ResizeNone leaves the geometry alone; the image is only re-encoded.
	ResizeNone Resize = ""
	// ResizeFit scales the image down until it fits inside the box, keeping
	// its proportions. Nothing is cropped and the result is usually smaller
	// than the box in one dimension.
	ResizeFit Resize = "fit"
	// ResizeFill scales until the box is covered and crops the overflow from
	// the centre, so the result is exactly the box.
	ResizeFill Resize = "fill"
	// ResizeExact scales each axis independently to the box, which changes
	// the proportions.
	ResizeExact Resize = "exact"
)

// Options is one conversion.
type Options struct {
	// To is the output format. Required.
	To Format
	// Quality is 1 to 100 for the lossy formats. Zero takes the format's
	// default, which is the one recorded in defaults below rather than
	// libvips's: libvips's own JPEG default is 75, which is visibly soft.
	Quality int
	// Effort trades encode time for file size, 0 to 10, where the meaning is
	// per format. Zero takes the default.
	Effort int
	// Lossless asks for a lossless encode where the format has one (WebP,
	// AVIF, JPEG XL). It is ignored by a format that has no such mode rather
	// than refused, because a client asking for lossless PNG is asking for
	// what PNG already is.
	Lossless bool
	// Resize and Width/Height are the target geometry. Width or Height may
	// be zero in fit mode, which then bounds only the other axis.
	Resize Resize
	Width  int
	Height int
	// Upscale allows the image to be made larger than it is. Off by default:
	// upscaling is almost never what somebody who typed a size wanted, and
	// it produces a bigger file that looks worse.
	Upscale bool
	// KeepMetadata keeps EXIF, XMP and IPTC. Off by default, because a
	// photograph's EXIF routinely carries GPS coordinates, a device serial
	// number and a timestamp, and a conversion service that silently
	// republishes those is a privacy problem. The interface states this.
	KeepMetadata bool
	// KeepAnimation keeps every frame of an animated input. Off by default:
	// the common case is one frame out of a GIF, and loading every frame of
	// a long animation is the one way to make an image conversion expensive.
	KeepAnimation bool
}

// Result is what a conversion produced.
type Result struct {
	// Path is the output file.
	Path string
	// Format is what was written.
	Format Format
	// Source is the input's metadata, so a caller can report what changed
	// without probing twice.
	Source Metadata
	// Output is the result's metadata, read back from the file that was
	// written rather than predicted: the assertion that a conversion
	// produced a decodable image of the expected size is only worth
	// anything if it reads the real file.
	Output Metadata
	// CPUTime and MaxRSS are what the engine actually spent, from the
	// runner.
	CPUTime int64
	MaxRSS  int64
}

// Convert re-encodes an image, optionally resizing it.
//
// One libvips process does all of it, and that is a deliberate choice rather
// than a convenience. `vips thumbnail` is the only operation that reads with
// shrink-on-load — it asks the JPEG decoder for a 1/2, 1/4 or 1/8 scale image
// and never materialises the full frame — and it is also the one that applies
// the EXIF orientation and converts to sRGB on the way through. Doing those
// three things in three processes would decode the image three times and, for
// a JPEG, lose quality twice.
//
// It is used even when nothing is being resized, with the target set to the
// image's own size and downsizing only, which is a re-encode with the
// orientation and the colour profile handled correctly.
func Convert(ctx context.Context, e Engine, r Runner, dir, input string, o Options) (Result, error) {
	source, err := Probe(ctx, e, r, dir, input)
	if err != nil {
		return Result{}, err
	}

	if source.Pixels() > MaxPixels {
		return Result{}, &DimensionError{Width: source.Width, Height: source.Height}
	}

	if !o.To.Known() || byFormat[o.To].saver == "" {
		return Result{}, &UnsupportedError{Format: o.To, Writing: true}
	}

	width, height, size := target(source, o)

	output := filepath.Join(dir, "output"+o.To.Extension())

	args := []string{
		"thumbnail",
		input + loadSuffix(source, o),
		output + saveSuffix(o),
		strconv.Itoa(width),
		"--height", strconv.Itoa(height),
		"--size", size,
		// The colour-profile rule, in one flag: libvips converts the image
		// to sRGB using its embedded profile before the save strips that
		// profile. Without it, stripping metadata would drop the profile and
		// leave the numbers untouched, so a wide-gamut photograph would come
		// out washed out — the classic "my colours changed" bug.
		"--export-profile", "srgb",
		"--intent", "relative",
		// Fail on a decode error rather than writing whatever was recovered.
		// A half-decoded image that saves without complaint is worse than a
		// refusal: the user keeps the broken file and finds out later.
		"--fail-on", "error",
	}

	if o.Resize == ResizeFill {
		args = append(args, "--crop", "centre")
	}

	res, err := r.Run(ctx, engine.Command{
		Path: e.Vips,
		Args: args,
		Dir:  dir,
		Env:  e.env(),
	})
	if err != nil {
		return Result{}, classify(err, "convert the image")
	}

	out, err := Probe(ctx, e, r, dir, output)
	if err != nil {
		return Result{}, err
	}

	return Result{
		Path:    output,
		Format:  o.To,
		Source:  source,
		Output:  out,
		CPUTime: res.CPUTime.Milliseconds(),
		MaxRSS:  res.MaxRSS,
	}, nil
}

// target returns the width, height and libvips size mode for a conversion.
//
// A conversion with no resize still needs a target, because the single
// operation that handles orientation and colour is a thumbnail: the image's
// own size with downsizing only is the identity, and it keeps one code path
// instead of two.
func target(source Metadata, o Options) (width, height int, size string) {
	switch o.Resize {
	case ResizeExact:
		return positive(o.Width, source.Width), positive(o.Height, source.Height), "force"
	case ResizeFill, ResizeFit:
		w := positive(o.Width, veryLarge)
		h := positive(o.Height, veryLarge)

		if o.Upscale {
			return w, h, "both"
		}

		return w, h, "down"
	case ResizeNone:
		return source.Width, source.Height, "down"
	default:
		return source.Width, source.Height, "down"
	}
}

// veryLarge is the bound for an axis the caller left open in fit mode. It is
// above MaxPixels in either dimension, so it never constrains anything, and
// finite, because libvips wants a number.
const veryLarge = 100_000

// positive returns v when it is positive, and fallback otherwise.
func positive(v, fallback int) int {
	if v > 0 {
		return v
	}

	return fallback
}

// loadSuffix is the libvips load options appended to the input filename.
//
// libvips takes loader options in the filename, which looks odd and is the
// documented interface: `in.jpg[access=sequential]`. Sequential access is the
// one that matters for memory — it tells libvips the image will be read top
// to bottom once, so it keeps a few scanlines rather than the whole frame.
func loadSuffix(source Metadata, o Options) string {
	opts := []string{"access=sequential"}

	// Every frame, or just the first. n=-1 means all pages; without it
	// libvips reads page 0, which is the right default because decoding a
	// 300-frame GIF to produce one still is pure waste.
	if o.KeepAnimation && source.Animated() && o.To.Animated() {
		opts = append(opts, "n=-1")
	}

	return "[" + strings.Join(opts, ",") + "]"
}

// saveSuffix is the libvips save options appended to the output filename.
func saveSuffix(o Options) string {
	d := defaults[o.To]

	opts := []string{"keep=" + keepFlags(o.KeepMetadata)}

	if d.quality {
		opts = append(opts, "Q="+strconv.Itoa(clamp(positive(o.Quality, d.Quality), 1, 100)))
	}

	if d.effort {
		opts = append(opts, "effort="+strconv.Itoa(clamp(positive(o.Effort, d.Effort), d.minEffort, d.maxEffort)))
	}

	if o.Lossless && d.lossless {
		opts = append(opts, "lossless=true")
	}

	opts = append(opts, d.extra...)

	return "[" + strings.Join(opts, ",") + "]"
}

// keepFlags is the libvips `keep` value for the metadata choice.
//
// Not "all" when metadata is kept: "all" keeps the embedded ICC profile too,
// and the pixels have already been converted to sRGB, so keeping the original
// profile would describe the new numbers with the old profile and shift every
// colour. Keeping EXIF, XMP and IPTC is what a user asking to keep metadata
// means.
func keepFlags(keep bool) string {
	if keep {
		return "exif:xmp:iptc"
	}

	return "none"
}

// clamp bounds v to [lo, hi].
func clamp(v, lo, hi int) int { return min(max(v, lo), hi) }

// encodeDefaults is one format's encoder settings: which knobs it has, and
// what this project uses when the client names none.
type encodeDefaults struct {
	// quality and effort say whether the saver has those options at all.
	// Passing Q to pngsave is an error, not a no-op.
	quality bool
	effort  bool
	// lossless says whether the format has a lossless mode.
	lossless bool
	// Quality and Effort are this project's defaults.
	Quality int
	Effort  int
	// minEffort and maxEffort are the saver's own range, which differs per
	// format: webpsave is 0 to 6, heifsave 0 to 9, pngsave 1 to 10.
	minEffort int
	maxEffort int
	// extra holds the per-format options that are not a dial, as libvips
	// spells them.
	extra []string
}

// defaults is the encoder settings per format.
//
// The numbers are chosen for "indistinguishable from the original at a
// sensible size", not for the smallest possible file: somebody converting a
// photograph expects it to still look like the photograph. Where libvips's own
// default is worse than that — JPEG at Q=75 — this overrides it.
var defaults = map[Format]encodeDefaults{
	JPEG: {
		quality: true, Quality: 82,
		// mozjpeg-style settings, all of which libjpeg-turbo supports and
		// none of which costs quality: optimised Huffman tables and trellis
		// quantisation are smaller at the same Q, progressive scans decode
		// in passes, and overshoot deringing cleans up hard edges.
		extra: []string{
			"optimize-coding=true",
			"trellis-quant=true",
			"overshoot-deringing=true",
			"optimize-scans=true",
			"interlace=true",
			"subsample-mode=auto",
		},
	},
	PNG: {
		effort: true, Effort: 7, minEffort: 1, maxEffort: 10,
		extra: []string{"compression=9"},
	},
	WebP: {
		quality: true, Quality: 82,
		effort: true, Effort: 4, minEffort: 0, maxEffort: 6,
		lossless: true,
		extra:    []string{"smart-subsample=true"},
	},
	AVIF: {
		quality: true, Quality: 60,
		effort: true, Effort: 4, minEffort: 0, maxEffort: 9,
		lossless: true,
		// compression=av1 is what makes heifsave write AVIF rather than
		// HEIC; the extension alone does not choose the codec. bitdepth=8
		// because heifsave defaults to 12, which is larger and which several
		// browsers decode more slowly for no visible gain on an 8-bit
		// source.
		extra: []string{"compression=av1", "bitdepth=8", "subsample-mode=auto"},
	},
	HEIC: {
		quality: true, Quality: 60,
		effort: true, Effort: 4, minEffort: 0, maxEffort: 9,
		lossless: true,
		extra:    []string{"compression=hevc", "bitdepth=8", "subsample-mode=auto"},
	},
	JXL: {
		quality: true, Quality: 80,
		effort: true, Effort: 7, minEffort: 1, maxEffort: 9,
		lossless: true,
	},
	GIF: {
		effort: true, Effort: 7, minEffort: 1, maxEffort: 10,
		extra: []string{"bitdepth=8"},
	},
	TIFF: {
		quality: true, Quality: 85,
		extra: []string{"compression=deflate"},
	},
	// BMP and ICO have no saver, so they never reach here; the entries exist
	// so a lookup cannot silently produce an empty option list that looks
	// deliberate.
	BMP: {},
	ICO: {},
}
