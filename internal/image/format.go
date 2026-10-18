// Package image converts raster images through libvips.
//
// libvips rather than ImageMagick, and the reason is memory: libvips streams a
// region at a time and never materialises the whole image, while ImageMagick
// builds a pixel cache of the full frame. A 4K RGBA frame is 33 MB decoded and
// several times that once a pipeline holds two of them, which on a small
// container is the difference between a conversion and an OOM kill. The
// brainstorming decision was libvips first and ImageMagick not installed at
// all, so anything libvips cannot read is refused rather than quietly handed
// to a second engine.
//
// Everything here goes through internal/engine, so every conversion carries a
// timeout, an address-space cap, a file-size cap and a process-group kill. The
// package itself knows nothing about jobs, storage or HTTP: it takes a path in
// and writes a path out, which is what makes it benchmarkable without a queue.
package image

import (
	"slices"
	"strings"
)

// Format is an image format this package can name. The string is the stable
// public identifier: it is what the API accepts and what the interface shows,
// so renaming one is a breaking change.
type Format string

// The formats. Every one is either readable or writable by libvips in some
// build; which ones this build actually supports is what Capabilities
// reports, because a format compiled out is indistinguishable from one that
// was never implemented until you ask the binary.
const (
	JPEG Format = "jpeg"
	PNG  Format = "png"
	WebP Format = "webp"
	AVIF Format = "avif"
	JXL  Format = "jxl"
	HEIC Format = "heic"
	GIF  Format = "gif"
	TIFF Format = "tiff"
	BMP  Format = "bmp"
	ICO  Format = "ico"
)

// spec describes one format: how libvips names its loader and saver, the
// extension a file must carry for the saver to be chosen, and the MIME type
// internal/detect reports for it.
//
// The loader and saver names are the operation names `vips -l` prints, which
// is how Capabilities tells a format that is compiled in from one that is
// not. They are deliberately the *operation* names rather than the module
// names: a module can be present and its operation still absent when a
// dependency was not found at build time.
type spec struct {
	format Format
	// mime is the type internal/detect reports, so a detected input maps to
	// a format without a second table.
	mime string
	// extension is what the output file is named, which is how libvips picks
	// the saver.
	extension string
	// loader and saver are the libvips operation names. An empty string
	// means libvips has none, which is the honest answer for BMP output and
	// ICO output: there is no bmpsave and no icosave in any build.
	loader string
	saver  string
	// animated marks a format that can hold more than one frame, which
	// decides whether the n=-1 load option is worth passing.
	animated bool
	// label is what a message calls it.
	label string
}

// specs is the format table. It is the single place a format is declared:
// Formats, Capabilities and the MIME mapping all read it, so a format cannot
// be half-added.
var specs = []spec{
	{JPEG, "image/jpeg", ".jpg", "jpegload", "jpegsave", false, "JPEG"},
	{PNG, "image/png", ".png", "pngload", "pngsave", false, "PNG"},
	{WebP, "image/webp", ".webp", "webpload", "webpsave", true, "WebP"},
	{AVIF, "image/avif", ".avif", "heifload", "heifsave", false, "AVIF"},
	{JXL, "image/jxl", ".jxl", "jxlload", "jxlsave", false, "JPEG XL"},
	{HEIC, "image/heic", ".heic", "heifload", "heifsave", false, "HEIC"},
	{GIF, "image/gif", ".gif", "gifload", "gifsave", true, "GIF"},
	{TIFF, "image/tiff", ".tiff", "tiffload", "tiffsave", false, "TIFF"},
	// BMP and ICO are read through libvips's ImageMagick loader when the
	// build has one, and cannot be written at all: libvips has no bmpsave
	// and no icosave. Saying so here is what makes the API's output list
	// honest rather than a promise the engine cannot keep.
	{BMP, "image/bmp", ".bmp", "magickload", "", false, "BMP"},
	{ICO, "image/x-icon", ".ico", "magickload", "", false, "icon"},
}

// byFormat indexes specs. Built once at load, so a format added to specs is
// reachable without touching a second map.
var byFormat = func() map[Format]spec {
	m := make(map[Format]spec, len(specs))
	for _, s := range specs {
		m[s.format] = s
	}

	return m
}()

// byMIME maps a detected MIME type to a format. AVIF and HEIC share heifload
// but are separate formats, which is why this is keyed on the MIME type
// rather than on the loader.
var byMIME = func() map[string]Format {
	m := make(map[string]Format, len(specs))
	for _, s := range specs {
		m[s.mime] = s.format
	}

	return m
}()

// FormatOf returns the format for a MIME type from internal/detect.
func FormatOf(mime string) (Format, bool) {
	f, ok := byMIME[strings.TrimSpace(strings.ToLower(mime))]
	return f, ok
}

// MIME returns the media type of a format, or "" for a format this package
// does not know.
func (f Format) MIME() string { return byFormat[f].mime }

// Extension returns the canonical extension including the dot.
func (f Format) Extension() string { return byFormat[f].extension }

// Label returns what a message should call the format.
func (f Format) Label() string {
	if s, ok := byFormat[f]; ok {
		return s.label
	}

	return string(f)
}

// Known reports whether f is a format this package declares.
func (f Format) Known() bool {
	_, ok := byFormat[f]
	return ok
}

// Formats returns every declared format, in table order.
func Formats() []Format {
	out := make([]Format, 0, len(specs))
	for _, s := range specs {
		out = append(out, s.format)
	}

	return out
}

// Animated reports whether the format can hold more than one frame.
func (f Format) Animated() bool { return byFormat[f].animated }

// MIMETypes returns the media types this package can read, sorted, for a
// tool's accepted list.
func MIMETypes() []string {
	out := make([]string, 0, len(specs))

	for _, s := range specs {
		if s.loader != "" {
			out = append(out, s.mime)
		}
	}

	slices.Sort(out)

	return slices.Compact(out)
}
