package image_test

import (
	"context"
	"errors"
	"image"
	"math"
	"os"
	"strings"
	"testing"

	vipsimage "github.com/AlexGalhardo/vukem-tools/internal/image"
)

// sizeOf is the file's size in bytes.
func sizeOf(tb testing.TB, path string) int64 {
	tb.Helper()

	info, err := os.Stat(path)
	if err != nil {
		tb.Fatalf("measure %s: %v", path, err)
	}

	return info.Size()
}

// A PNG is compressed losslessly: smaller bytes, and not one pixel moved.
//
// The pixel comparison is the whole point of the word "lossless". A test that
// only asserted the file shrank would pass just as happily on a compressor
// that quantised the palette, which is what pngquant does and what this mode
// promises not to do.
func TestCompressingAPNGIsSmallerAndPixelIdentical(t *testing.T) {
	t.Parallel()

	e, r := engines(t)
	dir := t.TempDir()

	// Encoded by Go, which does not try hard, so there is something to win.
	src := seedPNG(t, dir, 600, 400)

	got, err := vipsimage.Compress(context.Background(), e, r, dir, src, vipsimage.CompressOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if got.Mode != vipsimage.CompressLossless {
		t.Errorf("Mode = %q, want lossless: a PNG has no reason to be re-encoded lossily by default", got.Mode)
	}

	if got.Unchanged {
		t.Fatalf("the PNG came back unchanged; Go's encoder should be beatable. %s", got.Did)
	}

	if got.BytesOut >= got.BytesIn {
		t.Errorf("BytesOut = %d, BytesIn = %d: a compression that grew the file must report Unchanged",
			got.BytesOut, got.BytesIn)
	}

	before, after := decodePNG(t, src), decodePNG(t, got.Path)
	if diff := firstPixelDifference(before, after); diff != "" {
		t.Errorf("lossless compression changed the image: %s", diff)
	}
}

// A JPEG has no lossless path in libvips, so it is re-encoded lossily by
// default and the result still has to look like the original.
func TestCompressingAJPEGIsSmallerAndStaysFaithful(t *testing.T) {
	t.Parallel()

	e, r := engines(t)
	dir := t.TempDir()
	src := fixture(t, e, r, dir, vipsimage.JPEG, 600, 400)

	got, err := vipsimage.Compress(context.Background(), e, r, dir, src, vipsimage.CompressOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if got.Mode != vipsimage.CompressLossy {
		t.Errorf("Mode = %q, want lossy: libvips has no lossless JPEG re-encode", got.Mode)
	}

	if got.Unchanged {
		t.Skipf("the fixture JPEG could not be beaten, which is a legitimate outcome: %s", got.Did)
	}

	// SSIM rather than a mean difference: a mean is dominated by the flat
	// areas and stays high while an edge turns to mush, which is exactly the
	// failure a quality regression produces.
	score := ssim(t, decodeAny(t, e, r, dir, src), decodeAny(t, e, r, dir, got.Path))
	t.Logf("SSIM after compression: %.4f (saved %d of %d bytes)", score, got.BytesIn-got.BytesOut, got.BytesIn)

	if score < ssimFloor {
		t.Errorf("SSIM = %.4f, below the recorded floor of %.2f: the default quality has regressed",
			score, ssimFloor)
	}
}

// ssimFloor is the recorded structural-similarity floor for a default
// compression, in docs/benchmarks.md. A change to the default quality that
// drops below it is a regression, not a tuning choice.
const ssimFloor = 0.95

// A file that is already as small as this encoder can make it comes back as
// itself, with the fact stated, rather than as a re-encode that is larger.
func TestAnIncompressibleFileComesBackUnchanged(t *testing.T) {
	t.Parallel()

	e, r := engines(t)
	dir := t.TempDir()

	// Compressed once at full effort, so a second pass has nothing to win.
	src := seedPNG(t, dir, 240, 160)

	first, err := vipsimage.Compress(context.Background(), e, r, dir, src, vipsimage.CompressOptions{})
	if err != nil {
		t.Fatal(err)
	}

	second, err := vipsimage.Compress(context.Background(), e, r, t.TempDir(), first.Path, vipsimage.CompressOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if !second.Unchanged {
		t.Fatalf("a second pass shrank the file again by %d bytes, which this encoder should not manage",
			second.BytesIn-second.BytesOut)
	}

	if second.BytesOut != second.BytesIn {
		t.Errorf("BytesOut = %d, want BytesIn = %d when unchanged", second.BytesOut, second.BytesIn)
	}

	if second.Path != first.Path {
		t.Errorf("Path = %q, want the original %q: an unchanged result is the original file",
			second.Path, first.Path)
	}

	if !strings.Contains(second.Did, "already") {
		t.Errorf("Did = %q, want it to say the file was already as small as this can make it", second.Did)
	}
}

// Asking for lossless on a JPEG is refused rather than quietly done lossily.
// libvips has no lossless JPEG re-encode; jpegtran does, and this image does
// not ship it.
func TestLosslessIsRefusedForJPEG(t *testing.T) {
	t.Parallel()

	e, r := engines(t)
	dir := t.TempDir()
	src := fixture(t, e, r, dir, vipsimage.JPEG, 200, 150)

	_, err := vipsimage.Compress(context.Background(), e, r, dir, src,
		vipsimage.CompressOptions{Mode: vipsimage.CompressLossless})

	mode, ok := errors.AsType[*vipsimage.CompressModeError](err)
	if !ok {
		t.Fatalf("err = %v, want a *CompressModeError", err)
	}

	if !strings.Contains(mode.Message(), "lossy") {
		t.Errorf("the message does not name the way forward: %q", mode.Message())
	}
}

// A format this tool does not compress is refused by name, with the four it
// does compress listed, rather than being converted to something else.
func TestAnUnsupportedFormatIsRefusedByName(t *testing.T) {
	t.Parallel()

	e, r := engines(t)
	dir := t.TempDir()

	caps := capabilities(t, e, r, dir)
	if !vipsimage.CanWrite(caps, vipsimage.TIFF) {
		t.Skip("this build cannot write TIFF, so there is no fixture to refuse")
	}

	src := fixture(t, e, r, dir, vipsimage.TIFF, 200, 150)

	_, err := vipsimage.Compress(context.Background(), e, r, dir, src, vipsimage.CompressOptions{})

	if _, ok := errors.AsType[*vipsimage.UnsupportedError](err); !ok {
		t.Fatalf("err = %v, want an *UnsupportedError", err)
	}
}

// The reported sentence names what actually ran, which is the ROADMAP's own
// assert: a "what it did" string that does not name the real tool is a claim
// rather than a report.
func TestTheReportNamesTheEngineAndTheMode(t *testing.T) {
	t.Parallel()

	e, r := engines(t)
	dir := t.TempDir()
	src := seedPNG(t, dir, 300, 200)

	got, err := vipsimage.Compress(context.Background(), e, r, dir, src, vipsimage.CompressOptions{})
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"libvips", "lossless", "PNG"} {
		if !strings.Contains(got.Did, want) {
			t.Errorf("Did = %q, want it to mention %q", got.Did, want)
		}
	}
}

// Compression never changes the format: that is what image-convert is for,
// and a tool that silently returned a WebP to somebody who uploaded a PNG
// would break every link to the file they expected back.
func TestCompressionKeepsTheFormat(t *testing.T) {
	t.Parallel()

	e, r := engines(t)
	dir := t.TempDir()
	src := seedPNG(t, dir, 300, 200)

	got, err := vipsimage.Compress(context.Background(), e, r, dir, src, vipsimage.CompressOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if got.Format != vipsimage.PNG {
		t.Errorf("Format = %q, want png", got.Format)
	}
}

// firstPixelDifference reports the first pixel at which two images differ,
// or "" when they are identical. The coordinates matter: "they differ" sends
// somebody hunting, "they differ at 0,0" says the alpha handling is wrong and
// "they differ at 417,6" says it is an encoder edge case.
func firstPixelDifference(a, b image.Image) string {
	if a.Bounds() != b.Bounds() {
		return "the bounds differ: " + a.Bounds().String() + " against " + b.Bounds().String()
	}

	for y := a.Bounds().Min.Y; y < a.Bounds().Max.Y; y++ {
		for x := a.Bounds().Min.X; x < a.Bounds().Max.X; x++ {
			r1, g1, b1, a1 := a.At(x, y).RGBA()
			r2, g2, b2, a2 := b.At(x, y).RGBA()

			if r1 != r2 || g1 != g2 || b1 != b2 || a1 != a2 {
				return "first at " +
					itoa(x) + "," + itoa(y) +
					": " + rgba(r1, g1, b1, a1) + " became " + rgba(r2, g2, b2, a2)
			}
		}
	}

	return ""
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}

	var out []byte
	for n > 0 {
		out = append([]byte{byte('0' + n%10)}, out...)
		n /= 10
	}

	return string(out)
}

func rgba(r, g, b, a uint32) string {
	return "(" + itoa(int(r>>8)) + "," + itoa(int(g>>8)) + "," + itoa(int(b>>8)) + "," + itoa(int(a>>8)) + ")"
}

// decodeAny renders any image this engine reads into a PNG and decodes that,
// so the comparison below works on formats Go cannot read.
func decodeAny(
	tb testing.TB,
	e vipsimage.Engine,
	r vipsimage.Runner,
	dir, path string,
) image.Image {
	tb.Helper()

	out := dir + "/decoded-" + itoa(int(sizeOf(tb, path)%100000)) + ".png"
	runVips(tb, e, r, dir, "copy", path+"[access=sequential]", out+"[keep=none,compression=0]")

	return decodePNG(tb, out)
}

// ssim is the mean structural similarity of two images, on luma, over 8x8
// windows.
//
// Written here rather than taken from a library because it is forty lines and
// the alternative is a dependency for one assertion in one test. The window
// size and the stabilising constants are the standard ones from Wang et al.
// 2004; the result is 1.0 for identical images and falls as structure is lost.
func ssim(tb testing.TB, a, b image.Image) float64 {
	tb.Helper()

	if a.Bounds() != b.Bounds() {
		tb.Fatalf("SSIM needs matching bounds: %s against %s", a.Bounds(), b.Bounds())
	}

	const (
		window = 8
		// 8-bit dynamic range, with the paper's k1 and k2.
		c1 = (0.01 * 255) * (0.01 * 255)
		c2 = (0.03 * 255) * (0.03 * 255)
	)

	bounds := a.Bounds()

	var total float64

	var windows int

	for y := bounds.Min.Y; y+window <= bounds.Max.Y; y += window {
		for x := bounds.Min.X; x+window <= bounds.Max.X; x += window {
			var sumA, sumB, sumAA, sumBB, sumAB float64

			for dy := range window {
				for dx := range window {
					la := luma(a.At(x+dx, y+dy).RGBA())
					lb := luma(b.At(x+dx, y+dy).RGBA())

					sumA += la
					sumB += lb
					sumAA += la * la
					sumBB += lb * lb
					sumAB += la * lb
				}
			}

			n := float64(window * window)
			meanA, meanB := sumA/n, sumB/n
			varA := sumAA/n - meanA*meanA
			varB := sumBB/n - meanB*meanB
			cov := sumAB/n - meanA*meanB

			numerator := (2*meanA*meanB + c1) * (2*cov + c2)
			denominator := (meanA*meanA + meanB*meanB + c1) * (varA + varB + c2)

			if denominator != 0 {
				total += numerator / denominator
				windows++
			}
		}
	}

	if windows == 0 {
		tb.Fatal("the images are smaller than one SSIM window")
	}

	return total / float64(windows)
}

// luma is Rec. 601 luminance, on the 0 to 255 scale the constants above use.
func luma(r, g, b, _ uint32) float64 {
	return math.Round(0.299*float64(r>>8) + 0.587*float64(g>>8) + 0.114*float64(b>>8))
}

// Converting to PNG must not produce an absurdly large file.
//
// A regression test for a real bug rather than a hypothetical one: libvips's
// pngsave defaults to no row filtering, so every PNG this service wrote was
// as large as the compressor could make it. The gradient fixture was 150 527
// bytes and should be around 2 000. The bound is deliberately loose — this
// asserts "the filters are on", not a byte count that would break on a
// libpng upgrade.
func TestConvertingToPNGUsesRowFilters(t *testing.T) {
	t.Parallel()

	e, r := engines(t)
	dir := t.TempDir()
	src := seedPNG(t, dir, 600, 400)

	got, err := vipsimage.Convert(context.Background(), e, r, dir, src, vipsimage.Options{To: vipsimage.PNG})
	if err != nil {
		t.Fatal(err)
	}

	size := sizeOf(t, got.Path)

	// Ten times Go's own encoder, which is itself unremarkable. Unfiltered
	// output of this fixture is seventy times larger and fails this easily.
	if ceiling := sizeOf(t, src) * 10; size > ceiling {
		t.Errorf("the converted PNG is %d bytes against a %d-byte source (ceiling %d): "+
			"pngsave is writing unfiltered rows", size, sizeOf(t, src), ceiling)
	}
}
