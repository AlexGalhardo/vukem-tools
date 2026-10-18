package image_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AlexGalhardo/vukem-tools/internal/engine"
	vipsimage "github.com/AlexGalhardo/vukem-tools/internal/image"
)

// The tests in this package need libvips. It is in the production image and
// deliberately not installed on a development machine, so they skip with a
// reason that says where to run them rather than passing vacuously.
//
// docs/deployment.md § "Verifying the image" has the commands.
func engines(tb testing.TB) (vipsimage.Engine, vipsimage.Runner) {
	tb.Helper()

	vips, err := exec.LookPath("vips")
	if err != nil {
		tb.Skip("libvips is not installed here; run this inside the production image " +
			"(docs/deployment.md § Verifying the image)")
	}

	header, err := exec.LookPath("vipsheader")
	if err != nil {
		tb.Skip("vipsheader is not installed here; run this inside the production image")
	}

	runner, err := engine.NewRunner(engine.RunnerOptions{
		Limits: engine.Limits{
			Timeout:      2 * time.Minute,
			AddressSpace: 3 << 30,
			CPU:          90 * time.Second,
			FileSize:     256 << 20,
			Processes:    64,
			OpenFiles:    512,
		},
	})
	if err != nil {
		tb.Fatalf("build a runner: %v", err)
	}

	return vipsimage.Engine{Vips: vips, VipsHeader: header}, runner
}

// capabilities reads what this build supports. Every matrix test needs it,
// and `vips -l` is several hundred lines, so it is read once per test.
func capabilities(tb testing.TB, e vipsimage.Engine, r vipsimage.Runner, dir string) []vipsimage.Capability {
	tb.Helper()

	got, err := vipsimage.Capabilities(context.Background(), e, r, dir)
	if err != nil {
		tb.Fatalf("read the engine's capabilities: %v", err)
	}

	return got
}

// seedPNG writes a PNG generated in Go, with no engine involved.
//
// Generated in Go rather than committed or produced by libvips, for two
// reasons. A committed fixture per format would be ten unreviewable binaries;
// and generating the seed with the engine under test would mean a bug in the
// engine's writer could hide a bug in its reader. Go's PNG encoder is the
// independent party here.
//
// The content is a two-axis gradient with a plaid overlay, which matters: a
// flat colour compresses to almost nothing, encodes identically at every
// quality setting, and would make a quality option look like it works when it
// does nothing.
func seedPNG(tb testing.TB, dir string, w, h int) string {
	tb.Helper()

	img := image.NewNRGBA(image.Rect(0, 0, w, h))

	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8(x * 255 / max(w-1, 1)),
				G: uint8(y * 255 / max(h-1, 1)),
				B: uint8((x/8 + y/8) % 256),
				A: 255,
			})
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		tb.Fatalf("encode the seed PNG: %v", err)
	}

	path := filepath.Join(dir, "seed.png")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		tb.Fatalf("write the seed PNG: %v", err)
	}

	return path
}

// fixture returns a file of the requested format in dir, at w by h.
//
// Everything but PNG is produced by converting the Go-generated seed with
// libvips, because libvips is the only thing here that can write AVIF, JPEG
// XL and the rest. A format this build cannot write has no fixture, which is
// why the matrix test only uses formats whose capability says both read and
// write.
func fixture(
	tb testing.TB,
	e vipsimage.Engine,
	r vipsimage.Runner,
	dir string,
	f vipsimage.Format,
	w, h int,
) string {
	tb.Helper()

	seed := seedPNG(tb, dir, w, h)
	if f == vipsimage.PNG {
		return seed
	}

	path := filepath.Join(dir, "fixture"+f.Extension())

	args := []string{"copy", seed + "[access=sequential]", path + "[keep=none]"}
	if _, err := r.Run(context.Background(), engine.Command{Path: e.Vips, Args: args, Dir: dir}); err != nil {
		tb.Fatalf("write a %s fixture: %v", f, err)
	}

	return path
}

// copyInto copies a file into a fresh working directory, so each conversion
// gets the private directory the runner requires and nothing is read twice
// from the same place.
func copyInto(tb testing.TB, from, dir string) string {
	tb.Helper()

	body, err := os.ReadFile(from)
	if err != nil {
		tb.Fatalf("read the fixture: %v", err)
	}

	to := filepath.Join(dir, filepath.Base(from))
	if err := os.WriteFile(to, body, 0o600); err != nil {
		tb.Fatalf("write the fixture: %v", err)
	}

	return to
}

// jpegWithEXIF writes a JPEG carrying a real EXIF block.
//
// The block is built by hand rather than borrowed from a photograph, which
// keeps the fixture reviewable: it is one IFD with one entry, the orientation
// tag, and the bytes below are that structure spelled out. A committed
// photograph would also carry whatever else its camera wrote, which is
// exactly the data this test exists to prove gets removed.
func jpegWithEXIF(tb testing.TB, e vipsimage.Engine, r vipsimage.Runner, dir string) string {
	tb.Helper()

	plain := fixture(tb, e, r, dir, vipsimage.JPEG, 64, 64)

	body, err := os.ReadFile(plain)
	if err != nil {
		tb.Fatalf("read the plain JPEG: %v", err)
	}

	if len(body) < 2 || body[0] != 0xFF || body[1] != 0xD8 {
		tb.Fatalf("the fixture is not a JPEG: % x", body[:min(len(body), 4)])
	}

	app1 := []byte{
		0xFF, 0xE1, // APP1
		0x00, 0x22, // segment length, 34: two for itself, six for the tag, 26 of TIFF
		'E', 'x', 'i', 'f', 0x00, 0x00,
		'I', 'I', 0x2A, 0x00, // little-endian TIFF magic
		0x08, 0x00, 0x00, 0x00, // the first IFD is at offset 8
		0x01, 0x00, // one entry
		0x12, 0x01, // tag 0x0112, Orientation
		0x03, 0x00, // type 3, SHORT
		0x01, 0x00, 0x00, 0x00, // one value
		0x01, 0x00, 0x00, 0x00, // upright
		0x00, 0x00, 0x00, 0x00, // no next IFD
	}

	out := filepath.Join(dir, "exif.jpg")

	withEXIF := slicesConcat(body[:2], app1, body[2:])
	if err := os.WriteFile(out, withEXIF, 0o600); err != nil {
		tb.Fatalf("write the EXIF JPEG: %v", err)
	}

	return out
}

// slicesConcat joins byte slices. It is here rather than inline so the EXIF
// insertion above reads as what it is: the segment goes immediately after the
// start-of-image marker, which is where a JPEG's APP segments live.
func slicesConcat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}

	return out
}

// header returns `vipsheader -a` for a file, which is how a test asserts on
// what was actually written rather than on what was asked for.
func header(tb testing.TB, e vipsimage.Engine, r vipsimage.Runner, dir, path string) string {
	tb.Helper()

	var out strings.Builder

	if _, err := r.Run(context.Background(), engine.Command{
		Path:   e.VipsHeader,
		Args:   []string{"-a", path},
		Dir:    dir,
		Stdout: &out,
	}); err != nil {
		tb.Fatalf("read the header of %s: %v", filepath.Base(path), err)
	}

	return out.String()
}

// findProfile returns a profile libvips can use, or "" when the build ships
// none. libvips installs its own sRGB and CMYK profiles; a wide-gamut one is
// only present on some builds, so the test uses whichever is there and says
// so when there is none.
func findProfile(tb testing.TB, _ vipsimage.Engine, _ vipsimage.Runner, _ string) string {
	tb.Helper()

	for _, candidate := range []string{
		"/usr/share/libvips/colour/cmyk.icm",
		"/usr/share/color/icc/colord/AdobeRGB1998.icc",
		"/usr/share/color/icc/ghostscript/a98.icc",
	} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	return ""
}

// meanAbsoluteDifference is the mean absolute difference, in levels out of
// 255, between two PNGs of the same size.
//
// Computed in Go with the standard library's decoder, deliberately. Asking
// libvips to grade its own output would make the engine under test both the
// subject and the judge, and an independent decoder is the stronger check —
// if Go and libvips disagreed about what these pixels are, that would itself
// be worth failing over.
//
// It also replaced an engine-side version that chained subtract, abs and avg
// through two intermediate files. That version was observed being killed with
// no output when all four engine test binaries ran back to back in one
// container; the cause was never established, and since this is a test helper
// rather than a product path, removing three subprocesses was the better
// answer than explaining them.
func meanAbsoluteDifference(tb testing.TB, _ vipsimage.Engine, _ vipsimage.Runner, _, a, b string) float64 {
	tb.Helper()

	left, right := decodePNG(tb, a), decodePNG(tb, b)

	if left.Bounds() != right.Bounds() {
		tb.Fatalf("the images are different sizes: %v against %v", left.Bounds(), right.Bounds())
	}

	var (
		total  float64
		counts int
	)

	bounds := left.Bounds()

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			lr, lg, lb, _ := left.At(x, y).RGBA()
			rr, rg, rb, _ := right.At(x, y).RGBA()

			// RGBA returns 16-bit values; the difference is reported in
			// 8-bit levels because that is the unit a threshold is readable
			// in.
			total += absDiff(lr, rr) + absDiff(lg, rg) + absDiff(lb, rb)
			counts += 3
		}
	}

	if counts == 0 {
		tb.Fatal("the images hold no pixels")
	}

	return total / float64(counts) / 257
}

// absDiff is the absolute difference between two channel values.
func absDiff(a, b uint32) float64 {
	if a > b {
		return float64(a - b)
	}

	return float64(b - a)
}

// decodePNG reads a PNG with the standard library.
func decodePNG(tb testing.TB, path string) image.Image {
	tb.Helper()

	file, err := os.Open(path)
	if err != nil {
		tb.Fatalf("open %s: %v", filepath.Base(path), err)
	}

	defer func() { _ = file.Close() }()

	decoded, err := png.Decode(file)
	if err != nil {
		tb.Fatalf("decode %s: %v", filepath.Base(path), err)
	}

	return decoded
}

// runVips runs one vips operation and fails the test with the engine's own
// message.
func runVips(tb testing.TB, e vipsimage.Engine, r vipsimage.Runner, dir string, args ...string) {
	tb.Helper()

	if _, err := r.Run(context.Background(), engine.Command{
		Path: e.Vips,
		Args: args,
		Dir:  dir,
	}); err != nil {
		tb.Fatalf("vips %s: %v", strings.Join(args, " "), err)
	}
}
