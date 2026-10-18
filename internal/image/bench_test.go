package image_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	vipsimage "github.com/AlexGalhardo/vukem-tools/internal/image"
)

// PeakRSSCeiling2K is the recorded ceiling for a 2K conversion, in bytes.
//
// The measurement in docs/benchmarks.md is 47.7 MB inside the production
// image; this is 128 MB. The headroom is deliberate and bounded: an ordinary
// libvips upgrade should not fail the build, while losing streaming would
// cost a factor of ten rather than a third and would still be caught. It is a
// ceiling on the *engine subprocess*, which is where the pixels are — the Go
// side never holds a frame, and measuring this process would pass whatever
// libvips did.
const PeakRSSCeiling2K = 128 << 20

// TestPeakMemoryForA2KConversionStaysUnderTheCeiling is the S4.1 memory
// assert.
//
// It measures the engine's own peak resident memory, which the runner reads
// from the kernel's rusage, rather than Go's heap: the whole design puts the
// pixels in a subprocess, so measuring this process would measure nothing and
// pass whatever libvips did.
func TestPeakMemoryForA2KConversionStaysUnderTheCeiling(t *testing.T) {
	e, r := engines(t)
	dir := t.TempDir()

	// 2048x2048 RGBA is 16 megapixels, 64 MB decoded in one frame. A
	// pipeline that materialises the image twice is immediately visible
	// against the ceiling.
	input := seedPNG(t, dir, 2048, 2048)

	got, err := vipsimage.Convert(context.Background(), e, r, dir, input, vipsimage.Options{
		To:     vipsimage.JPEG,
		Resize: vipsimage.ResizeFit,
		Width:  1024,
	})
	if err != nil {
		t.Fatalf("convert: %v", err)
	}

	if got.MaxRSS == 0 {
		t.Skip("this platform does not report peak resident memory")
	}

	t.Logf("2048x2048 PNG to a 1024-wide JPEG: peak RSS %.1f MB, CPU %d ms",
		float64(got.MaxRSS)/(1<<20), got.CPUTime)

	if got.MaxRSS > PeakRSSCeiling2K {
		t.Errorf("peak RSS was %.1f MB, past the recorded %.0f MB ceiling",
			float64(got.MaxRSS)/(1<<20), float64(PeakRSSCeiling2K)/(1<<20))
	}
}

// TestShrinkOnLoadIsCheaperThanAFullDecode is the evidence for the claim this
// package is built on.
//
// A JPEG downscaled to an eighth can be read by asking the decoder for a 1/8
// scale image, which never materialises the full frame. The assertion is that
// the small target really is cheaper in memory than the same conversion at
// full size — if it were not, "libvips streams" would be a slogan rather than
// a measurement, and the whole reason for choosing it would be unproven.
func TestShrinkOnLoadIsCheaperThanAFullDecode(t *testing.T) {
	e, r := engines(t)
	base := t.TempDir()

	// A JPEG source, because shrink-on-load is a JPEG decoder feature: the
	// PNG path has no equivalent and would show no difference.
	seed := seedPNG(t, base, 3000, 2000)

	source := filepath.Join(base, "source.jpg")
	runVips(t, e, r, base, "copy", seed+"[access=sequential]", source+"[Q=90]")

	peak := func(width int) int64 {
		t.Helper()

		dir := t.TempDir()
		input := copyInto(t, source, dir)

		opts := vipsimage.Options{To: vipsimage.JPEG}
		if width > 0 {
			opts.Resize = vipsimage.ResizeFit
			opts.Width = width
		}

		got, err := vipsimage.Convert(context.Background(), e, r, dir, input, opts)
		if err != nil {
			t.Fatalf("convert at width %d: %v", width, err)
		}

		return got.MaxRSS
	}

	shrunk, full := peak(375), peak(0)
	if shrunk == 0 || full == 0 {
		t.Skip("this platform does not report peak resident memory")
	}

	t.Logf("3000x2000 JPEG: shrink-on-load to 375 wide peaks at %.1f MB, full size at %.1f MB",
		float64(shrunk)/(1<<20), float64(full)/(1<<20))

	if shrunk >= full {
		t.Errorf("shrink-on-load peaked at %d bytes and a full decode at %d: the decoder is not being "+
			"asked for a reduced image", shrunk, full)
	}
}

// BenchmarkConvert measures the conversions the tools actually run.
//
// Every case reports the engine's peak RSS as a custom metric, because
// -benchmem measures this process's allocations and this process does not
// touch a pixel. Wall-clock is the benchmark's own ns/op.
func BenchmarkConvert(b *testing.B) {
	e, r := engines(b)

	cases := []struct {
		name string
		w, h int
		opts vipsimage.Options
	}{
		{"1080p_jpeg_to_webp", 1920, 1080, vipsimage.Options{To: vipsimage.WebP}},
		{"1080p_jpeg_to_jpeg", 1920, 1080, vipsimage.Options{To: vipsimage.JPEG}},
		{"2k_png_to_jpeg", 2048, 2048, vipsimage.Options{To: vipsimage.JPEG}},
		{
			"2k_png_thumbnail_512",
			2048, 2048,
			vipsimage.Options{To: vipsimage.JPEG, Resize: vipsimage.ResizeFit, Width: 512},
		},
		{"4k_png_to_jpeg", 3840, 2160, vipsimage.Options{To: vipsimage.JPEG}},
		{
			"4k_png_thumbnail_512",
			3840, 2160,
			vipsimage.Options{To: vipsimage.JPEG, Resize: vipsimage.ResizeFit, Width: 512},
		},
	}

	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			source := seedPNG(b, b.TempDir(), c.w, c.h)

			var peak, cpu int64

			for b.Loop() {
				dir := b.TempDir()

				body, err := os.ReadFile(source)
				if err != nil {
					b.Fatalf("read the source: %v", err)
				}

				input := filepath.Join(dir, filepath.Base(source))
				if err := os.WriteFile(input, body, 0o600); err != nil {
					b.Fatalf("write the source: %v", err)
				}

				got, err := vipsimage.Convert(context.Background(), e, r, dir, input, c.opts)
				if err != nil {
					b.Fatalf("convert: %v", err)
				}

				peak = max(peak, got.MaxRSS)
				cpu = max(cpu, got.CPUTime)
			}

			b.ReportMetric(float64(peak)/(1<<20), "peakMB")
			b.ReportMetric(float64(cpu), "engine_ms")
		})
	}
}
