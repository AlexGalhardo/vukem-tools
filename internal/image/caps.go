package image

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/AlexGalhardo/vukem-tools/internal/engine"
)

// Capability is what this build of libvips can do with one format.
type Capability struct {
	Format Format `json:"format"`
	// MIME is the media type, so a client can match it against an upload.
	MIME string `json:"mime"`
	// Extension is the canonical extension, including the dot.
	Extension string `json:"extension"`
	// Label is what to call it in an interface.
	Label string `json:"label"`
	// Read and Write are what the engine actually supports. Both are false
	// for a format whose module was compiled out, which is why this is
	// probed rather than declared: Debian's libvips has AVIF and JPEG XL,
	// Alpine's has neither, and the only honest way to know is to ask.
	Read  bool `json:"read"`
	Write bool `json:"write"`
	// Animated says the format can hold more than one frame.
	Animated bool `json:"animated"`
}

// Capabilities asks libvips which formats this build supports.
//
// This exists because of a specific roadmap requirement — format support is
// surfaced by the API rather than hard-coded in the frontend — and because
// the alternative fails in a way that is easy to miss: a hard-coded list is a
// promise the binary may not keep, and the failure shows up as a conversion
// that is offered, accepted, queued and only then rejected.
//
// Asking takes two steps, and the second one is not optional. Listing the
// operations is not enough: Debian's libvips has `heifsave`, so AVIF and HEIC
// look available, and the operation then fails with "Unsupported compression"
// because libheif was built with no encoder. An operation being present and a
// format being writable are different facts, and only a real encode
// distinguishes them. So each candidate saver is handed a four-pixel image
// with this project's own save options, which also means a typo in one of
// those options fails the boot rather than the first conversion.
//
// It is meant to be called once, at boot, and the answer kept. That costs
// about one process per format on top of the engine version probes, and buys
// a tools page that never advertises a format the binary cannot produce.
func Capabilities(ctx context.Context, e Engine, r Runner, dir string) ([]Capability, error) {
	var listing strings.Builder

	_, err := r.Run(ctx, engine.Command{
		Path:   e.Vips,
		Args:   []string{"-l"},
		Dir:    dir,
		Stdout: &listing,
		Env:    e.env(),
	})
	if err != nil {
		return nil, classify(err, "list the engine's operations")
	}

	caps := capabilitiesFrom(listing.String())

	probe, err := probeImage(ctx, e, r, dir)
	if err != nil {
		return nil, err
	}

	for i := range caps {
		if caps[i].Write {
			caps[i].Write = canEncode(ctx, e, r, dir, probe, caps[i].Format)
		}
	}

	return caps, nil
}

// probeImage writes the tiny image the encode trials use.
//
// Four pixels and three bands: small enough that every encoder finishes in
// milliseconds, and still a real RGB image, which matters because some savers
// refuse a one-band input rather than the format.
func probeImage(ctx context.Context, e Engine, r Runner, dir string) (string, error) {
	path := filepath.Join(dir, "capability-probe.v")

	if _, err := r.Run(ctx, engine.Command{
		Path: e.Vips,
		Args: []string{"black", path, "4", "4", "--bands", "3"},
		Dir:  dir,
		Env:  e.env(),
	}); err != nil {
		return "", classify(err, "write the capability probe image")
	}

	return path, nil
}

// canEncode reports whether the saver actually writes this format, with the
// options this package would really use.
func canEncode(ctx context.Context, e Engine, r Runner, dir, probe string, f Format) bool {
	out := filepath.Join(dir, "capability-probe"+f.Extension())

	_, err := r.Run(ctx, engine.Command{
		Path: e.Vips,
		Args: []string{"copy", probe, out + saveSuffix(Options{To: f})},
		Dir:  dir,
		Env:  e.env(),
	})

	// The file is removed either way: a successful trial leaves a stray
	// output in whatever directory the caller gave, and at boot that is a
	// real directory rather than a test's temporary one.
	defer func() { _ = os.Remove(out) }()

	return err == nil
}

// capabilitiesFrom reads the operation list.
//
// `vips -l` prints one indented line per operation, as the class name, then
// the operation's nickname in parentheses, then a description:
//
//	VipsForeignSaveJpegFile (jpegsave), save image to jpeg file (.jpg, ...)
//
// The nickname is what the command line accepts, so that is what is matched,
// and it is matched whole rather than with strings.Contains: "jpegsave" is a
// substring of "jpegsave_target", and a build whose only JPEG saver was the
// target variant would otherwise be reported as able to write a .jpg file
// when it cannot.
func capabilitiesFrom(listing string) []Capability {
	ops := make(map[string]bool, 512)

	for line := range strings.SplitSeq(listing, "\n") {
		if nickname := nicknameOf(line); nickname != "" {
			ops[nickname] = true
		}
	}

	out := make([]Capability, 0, len(specs))

	for _, s := range specs {
		out = append(out, Capability{
			Format:    s.format,
			MIME:      s.mime,
			Extension: s.extension,
			Label:     s.label,
			Read:      s.loader != "" && ops[s.loader],
			// A format with no saver is never writable, whatever the
			// listing says: BMP's "saver" would be magicksave, which exists
			// and which libvips will not route a .bmp through.
			Write:    s.saver != "" && ops[s.saver],
			Animated: s.animated,
		})
	}

	return out
}

// nicknameOf returns the operation nickname in the first parentheses of a
// listing line, or "" for a line that has none.
//
// The *first* parentheses, because the description that follows holds more:
// the file extensions a saver accepts are also parenthesised, and reading
// those as operation names would fill the set with ".jpg" and ".jpeg".
func nicknameOf(line string) string {
	_, rest, ok := strings.Cut(line, "(")
	if !ok {
		return ""
	}

	nickname, _, ok := strings.Cut(rest, ")")
	if !ok {
		return ""
	}

	return strings.TrimSpace(nickname)
}

// Readable returns the MIME types this build can read, which is what a tool's
// accepted list should be.
func Readable(caps []Capability) []string {
	var out []string

	for _, c := range caps {
		if c.Read {
			out = append(out, c.MIME)
		}
	}

	return out
}

// Writable returns the formats this build can produce.
func Writable(caps []Capability) []Format {
	var out []Format

	for _, c := range caps {
		if c.Write {
			out = append(out, c.Format)
		}
	}

	return out
}

// CanWrite reports whether f can be produced.
func CanWrite(caps []Capability, f Format) bool {
	for _, c := range caps {
		if c.Format == f {
			return c.Write
		}
	}

	return false
}
