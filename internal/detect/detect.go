// Package detect decides what a file actually is.
//
// Never from its name, never from the client's Content-Type: both are the
// client's to choose, and a `.mp4` renamed to `.png` is the cheapest attack
// on a conversion service there is. The type comes from the bytes, and the
// name is then checked against it — a file whose extension disagrees with its
// content is refused even when both are types the tool accepts, because the
// disagreement itself is the signal.
package detect

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"

	"github.com/gabriel-vasile/mimetype"
)

// HeaderBytes is how much of a file is read to identify it. mimetype needs
// only the first few kilobytes, and reading more would mean buffering part of
// a half-gigabyte upload to learn something the header already says.
const HeaderBytes = 3072

// Kind is a file type this service recognises: the MIME type, the canonical
// extension, and what the user should be told it is.
type Kind struct {
	// MIME is the detected media type, e.g. "image/png".
	MIME string
	// Extension is the canonical extension including the dot, e.g. ".png".
	Extension string
	// Label is what a message calls it, e.g. "a PNG image".
	Label string
}

// ErrUnknown means the bytes match no type this service recognises.
var ErrUnknown = errors.New("detect: unrecognised file type")

// MismatchError reports a file whose name disagrees with its content.
//
// It carries both so the message can name the detected type: telling someone
// "that is not a PNG" is useless, and telling them "that is an MP4 video, not
// the PNG image its name claims" is something they can act on.
type MismatchError struct {
	// Detected is what the bytes say.
	Detected Kind
	// Extension is what the name claimed, including the dot.
	Extension string
}

// Error implements error.
func (e *MismatchError) Error() string {
	return fmt.Sprintf("detect: the file is %s, not what %q claims", e.Detected.Label, e.Extension)
}

// Message is the sentence to show the user. It names the detected type,
// which is the whole point of refusing the file.
func (e *MismatchError) Message() string {
	return fmt.Sprintf("That file is %s, but it is named %q. Rename it or upload the right file.",
		e.Detected.Label, e.Extension)
}

// UnsupportedError reports a file of a type the tool does not accept.
type UnsupportedError struct {
	// Detected is what the bytes say; the zero Kind when nothing matched.
	Detected Kind
	// Accepted lists the MIME types the tool does take, for the message.
	Accepted []string
}

// Error implements error.
func (e *UnsupportedError) Error() string {
	if e.Detected.MIME == "" {
		return "detect: unrecognised file type"
	}

	return "detect: " + e.Detected.MIME + " is not accepted here"
}

// Message is the sentence to show the user.
func (e *UnsupportedError) Message() string {
	if e.Detected.MIME == "" {
		return "That file is not a type this tool can read."
	}

	return "That file is " + e.Detected.Label + ", which this tool cannot read."
}

// known maps a MIME type to how this service talks about it. It is the single
// place a new format is declared: a type absent from here is refused, so
// adding a format to a tool without describing it here cannot work by
// accident.
//
// Several extensions can map to one type (.jpg and .jpeg), which is why
// Extension is the canonical one and Allows does the matching.
var known = map[string]Kind{
	"image/png":       {"image/png", ".png", "a PNG image"},
	"image/jpeg":      {"image/jpeg", ".jpg", "a JPEG image"},
	"image/gif":       {"image/gif", ".gif", "a GIF image"},
	"image/webp":      {"image/webp", ".webp", "a WebP image"},
	"image/bmp":       {"image/bmp", ".bmp", "a BMP image"},
	"image/tiff":      {"image/tiff", ".tiff", "a TIFF image"},
	"image/avif":      {"image/avif", ".avif", "an AVIF image"},
	"image/jxl":       {"image/jxl", ".jxl", "a JPEG XL image"},
	"image/svg+xml":   {"image/svg+xml", ".svg", "an SVG image"},
	"image/x-icon":    {"image/x-icon", ".ico", "an icon"},
	"application/pdf": {"application/pdf", ".pdf", "a PDF document"},
	"video/mp4":       {"video/mp4", ".mp4", "an MP4 video"},
	"video/webm":      {"video/webm", ".webm", "a WebM video"},
	"video/x-matroska": {
		"video/x-matroska", ".mkv", "a Matroska video",
	},
	"video/quicktime":  {"video/quicktime", ".mov", "a QuickTime video"},
	"video/x-msvideo":  {"video/x-msvideo", ".avi", "an AVI video"},
	"audio/mpeg":       {"audio/mpeg", ".mp3", "an MP3 audio file"},
	"audio/wav":        {"audio/wav", ".wav", "a WAV audio file"},
	"audio/flac":       {"audio/flac", ".flac", "a FLAC audio file"},
	"audio/ogg":        {"audio/ogg", ".ogg", "an Ogg audio file"},
	"audio/aac":        {"audio/aac", ".aac", "an AAC audio file"},
	"application/zip":  {"application/zip", ".zip", "a ZIP archive"},
	"application/gzip": {"application/gzip", ".gz", "a gzip archive"},
	// TAR is accepted because archive-extract reads it. It widens the upload
	// gate by one format, and the format is parsed by archive/tar from the
	// standard library rather than by a C library, so the widening carries no
	// new attack surface of the kind docs/deployment.md tracks.
	"application/x-tar": {"application/x-tar", ".tar", "a TAR archive"},
}

// extensions maps every accepted extension to its MIME type, built from
// known plus the aliases people actually type. Built once at load, so an
// alias cannot be added without the type existing.
var extensions = func() map[string]string {
	m := make(map[string]string, len(known)+4)
	for mime, k := range known {
		m[k.Extension] = mime
	}

	// Aliases: the same bytes, a different spelling.
	m[".jpeg"] = "image/jpeg"
	m[".jpe"] = "image/jpeg"
	m[".tif"] = "image/tiff"
	m[".gzip"] = "application/gzip"
	// A gzipped tar, which is one stream and two conventional spellings.
	m[".tgz"] = "application/gzip"

	return m
}()

// Of identifies the bytes in r.
//
// It reads at most HeaderBytes and does not consume more, so the caller can
// go on reading the rest — but r must be a reader the caller is willing to
// have advanced by that much, which in practice means a file on disk the
// caller will seek back, or a header already in memory.
func Of(r io.Reader) (Kind, error) {
	head := make([]byte, HeaderBytes)

	n, err := io.ReadFull(r, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return Kind{}, fmt.Errorf("detect: read the header: %w", err)
	}

	return OfBytes(head[:n])
}

// OfBytes identifies head, which must hold at least the start of the file.
func OfBytes(head []byte) (Kind, error) {
	mt := mimetype.Detect(head)

	// mimetype walks a tree from the most specific match upward, so a type
	// it does not know still reports a parent such as "text/plain" or
	// "application/octet-stream". Walking the chain lets a subtype this
	// service has not listed still be recognised as its parent, which is
	// how "image/x-canon-cr2" ends up refused as an unlisted type rather
	// than silently detected as something else.
	for t := mt; t != nil; t = t.Parent() {
		if k, ok := known[cleanMIME(t.String())]; ok {
			return k, nil
		}
	}

	return Kind{}, ErrUnknown
}

// cleanMIME drops the parameters mimetype appends to some types, e.g.
// "text/plain; charset=utf-8".
func cleanMIME(s string) string {
	base, _, _ := strings.Cut(s, ";")
	return strings.TrimSpace(base)
}

// Check identifies the bytes in r, refuses a type that accepted does not
// list, and refuses a name whose extension disagrees with the content.
//
// accepted holds MIME types. An empty list accepts every type this service
// knows, which is only right for a tool that genuinely takes anything; a tool
// should name its types.
func Check(r io.Reader, name string, accepted []string) (Kind, error) {
	kind, err := Of(r)
	if err != nil {
		if errors.Is(err, ErrUnknown) {
			return Kind{}, &UnsupportedError{Accepted: accepted}
		}

		return Kind{}, err
	}

	// The name is checked before the accepted list, because a disagreement
	// is the more specific diagnosis: "that file is an MP4 video, not the PNG
	// its name claims" tells the user what happened, while "this tool cannot
	// read MP4" leaves them wondering why they uploaded a PNG. An extension
	// the user did not supply is not a disagreement; one that disagrees is,
	// even when both types are accepted, because a file arriving under the
	// wrong name is either a mistake worth reporting or an attempt worth
	// refusing — never something to convert quietly.
	if ext := strings.ToLower(filepath.Ext(name)); ext != "" {
		if mime, ok := extensions[ext]; !ok || mime != kind.MIME {
			return Kind{}, &MismatchError{Detected: kind, Extension: ext}
		}
	}

	if len(accepted) > 0 && !slices.Contains(accepted, kind.MIME) {
		return Kind{}, &UnsupportedError{Detected: kind, Accepted: accepted}
	}

	return kind, nil
}

// Known reports whether this service recognises the MIME type, which is what
// a tool's accepted list is validated against at boot.
//
// Only the types that may be uploaded: a result type is not something a tool
// accepts, and reporting it as known here would let one into an accepted
// list. See results.go.
func Known(mime string) bool {
	_, ok := known[mime]
	return ok
}

// KindOf returns the Kind for a MIME type, whether it is a type that may be
// uploaded or one this service only produces.
//
// Both, because this is the lookup used when a file is stored, and a
// conversion's result has to be storable without being uploadable.
func KindOf(mime string) (Kind, bool) {
	if k, ok := known[mime]; ok {
		return k, true
	}

	k, ok := resultOnly[mime]

	return k, ok
}

// Types returns every recognised MIME type, sorted, for the tools page and
// the API documentation.
func Types() []string {
	out := make([]string, 0, len(known))
	for mime := range known {
		out = append(out, mime)
	}

	slices.Sort(out)

	return out
}
