package image

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/AlexGalhardo/vukem-tools/internal/engine"
)

// Metadata is what the engine reports about an image, before anything is
// decoded.
//
// It is read first and used for three different decisions, which is why it is
// a type rather than three separate probes: the pixel cap is enforced from it,
// the ICC and orientation handling is chosen from it, and the result's
// dimensions are asserted against it.
type Metadata struct {
	Width  int
	Height int
	// Bands is the number of channels, so 4 means there is an alpha channel
	// on an sRGB image.
	Bands int
	// Pages is the frame count: 1 for a still, more for an animated GIF or
	// WebP.
	Pages int
	// Interpretation is libvips's colour interpretation, e.g. "srgb",
	// "cmyk", "b-w".
	Interpretation string
	// Loader is the libvips operation that read the file, e.g. "jpegload".
	// It is the engine's own answer to "what is this file", which is checked
	// against the detected type rather than trusted on its own.
	Loader string
	// HasProfile reports an embedded ICC profile. Stripping metadata drops
	// it, so the pipeline converts to sRGB first when this is set.
	HasProfile bool
	// Orientation is the EXIF orientation tag, 1 when upright or absent.
	// Stripping EXIF loses it, so it has to be applied to the pixels first.
	Orientation int
}

// Pixels is the frame area, which is what a decode actually costs. A pixel
// cap is expressed in these rather than in bytes, because a 50 KB file can
// decode to a gigabyte.
func (m Metadata) Pixels() int64 { return int64(m.Width) * int64(m.Height) }

// Animated reports whether the file holds more than one frame.
func (m Metadata) Animated() bool { return m.Pages > 1 }

// Probe reads an image's header with vipsheader.
//
// A separate process before the conversion, not a flag on it: the pixel cap
// has to be enforced *before* anything is decoded, and the header is a few
// kilobytes whatever the file's size. The cost is one exec, which is tens of
// milliseconds and buys the guarantee that a decompression bomb is refused
// rather than merely limited.
func Probe(ctx context.Context, e Engine, r Runner, dir, path string) (Metadata, error) {
	if err := checkPath(path); err != nil {
		return Metadata{}, err
	}

	var out strings.Builder

	_, err := r.Run(ctx, engine.Command{
		Path:   e.VipsHeader,
		Args:   []string{"-a", path},
		Dir:    dir,
		Stdout: &out,
		Env:    e.env(),
	})
	if err != nil {
		return Metadata{}, classify(err, "read the image header")
	}

	return parseHeader(out.String())
}

// errNoHeader means vipsheader printed something parseHeader could not read a
// size out of, which is a broken file or a format libvips does not recognise.
var errNoHeader = errors.New("image: the engine reported no image size")

// parseHeader reads `vipsheader -a` output.
//
// The format is one `key: value` per line after a summary line. It is parsed
// rather than regexped because the fields that matter are exact names, and a
// field that is absent must read as absent rather than as zero by accident:
// orientation defaults to 1, not 0, and a missing n-pages means one page.
func parseHeader(raw string) (Metadata, error) {
	m := Metadata{Pages: 1, Orientation: 1}

	for line := range strings.SplitSeq(raw, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}

		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)

		switch key {
		case "width":
			m.Width = atoi(value)
		case "height":
			m.Height = atoi(value)
		case "bands":
			m.Bands = atoi(value)
		case "n-pages":
			if n := atoi(value); n > 0 {
				m.Pages = n
			}
		case "interpretation":
			m.Interpretation = value
		case "vips-loader":
			m.Loader = value
		case "orientation":
			if n := atoi(value); n > 0 {
				m.Orientation = n
			}
		case "icc-profile-data":
			m.HasProfile = true
		}
	}

	if m.Width <= 0 || m.Height <= 0 {
		return Metadata{}, errNoHeader
	}

	// An animated image's height field is the whole strip of frames, not one
	// frame: libvips stores pages stacked vertically. The per-frame height is
	// what a user means by "the size", and what a resize must be computed
	// from, so it is normalised here rather than in three callers.
	if m.Pages > 1 && m.Height%m.Pages == 0 {
		m.Height /= m.Pages
	}

	return m, nil
}

// atoi reads an integer, returning 0 for anything else. A field that is not a
// number is a field that is absent as far as this package is concerned, and
// the caller's own validation decides whether that matters.
func atoi(s string) int {
	// The first field, not the whole value: several fields carry a unit, as
	// in "3144 bytes", and Atoi on the whole string would read them as
	// absent.
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return 0
	}

	n, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0
	}

	return n
}

// Describe returns a one-line summary of an image, for a log or an owner-only
// error detail. It names nothing but the image's own shape.
func (m Metadata) Describe() string {
	return fmt.Sprintf("%dx%d, %d bands, %s, %d page(s), loader %s",
		m.Width, m.Height, m.Bands, m.Interpretation, m.Pages, m.Loader)
}
