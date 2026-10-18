package image_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	vipsimage "github.com/AlexGalhardo/vukem-tools/internal/image"
)

// TestTheFormatMatrixConvertsAndDecodes is the S4.1 matrix assert: a fixture
// of each format this build can read is converted to each format it can
// write, and every result is read back *with the engine* to prove it decodes
// and has the dimensions that were asked for.
//
// Reading the result back is the whole assertion. Several libvips savers exit
// zero having written a file no decoder accepts, so "the command succeeded"
// proves nothing, while "the engine decodes what we wrote and it is 320x200"
// does.
func TestTheFormatMatrixConvertsAndDecodes(t *testing.T) {
	e, r := engines(t)
	available := capabilities(t, e, r, t.TempDir())

	const w, h = 320, 200

	var pairs int

	for _, in := range available {
		// An input fixture must be written before it can be read, so the
		// matrix's inputs are the formats this build can do both with. The
		// read-only half (BMP, ICO) is covered by the capability test and by
		// TestAFormatThisBuildCannotWriteIsRefused.
		if !in.Read || !in.Write {
			continue
		}

		t.Run(string(in.Format), func(t *testing.T) {
			source := fixture(t, e, r, t.TempDir(), in.Format, w, h)

			for _, out := range available {
				if !out.Write {
					continue
				}

				pairs++

				t.Run("to_"+string(out.Format), func(t *testing.T) {
					dir := t.TempDir()
					input := copyInto(t, source, dir)

					got, err := vipsimage.Convert(context.Background(), e, r, dir, input,
						vipsimage.Options{To: out.Format})
					if err != nil {
						t.Fatalf("convert %s to %s: %v", in.Format, out.Format, err)
					}

					if got.Output.Width != w || got.Output.Height != h {
						t.Errorf("result is %dx%d, want %dx%d",
							got.Output.Width, got.Output.Height, w, h)
					}

					info, err := os.Stat(got.Path)
					if err != nil || info.Size() == 0 {
						t.Errorf("result file is missing or empty: %v", err)
					}
				})
			}
		})
	}

	if pairs == 0 {
		t.Fatal("the matrix ran no pair, so it proved nothing")
	}

	t.Logf("matrix covered %d format pairs", pairs)
}

// TestCapabilitiesMatchTheEngine proves the capability probe agrees with the
// engine rather than with this package's own table: every format reported
// writable is actually written, and no format reported unwritable is.
func TestCapabilitiesMatchTheEngine(t *testing.T) {
	e, r := engines(t)
	dir := t.TempDir()
	available := capabilities(t, e, r, dir)

	if len(available) == 0 {
		t.Fatal("the probe reported no formats at all")
	}

	var writable, readable int

	for _, c := range available {
		if c.Write {
			writable++
		}

		if c.Read {
			readable++
		}
	}

	// A build with no writable format would make every test below pass
	// vacuously, which is the failure mode this guards.
	if writable < 2 || readable < 2 {
		t.Fatalf("this build reports %d readable and %d writable formats, which cannot be right",
			readable, writable)
	}

	t.Logf("this build reads %d and writes %d of the %d declared formats",
		readable, writable, len(available))
}

// TestAFormatThisBuildCannotWriteIsRefused proves the refusal happens here,
// before a process is started, so the message names the format rather than
// repeating libvips's own "unable to save".
func TestAFormatThisBuildCannotWriteIsRefused(t *testing.T) {
	e, r := engines(t)
	dir := t.TempDir()
	src := fixture(t, e, r, dir, vipsimage.PNG, 32, 32)

	_, err := vipsimage.Convert(context.Background(), e, r, dir, src,
		vipsimage.Options{To: vipsimage.BMP})

	var unsupported *vipsimage.UnsupportedError
	if !errors.As(err, &unsupported) {
		t.Fatalf("want an UnsupportedError, got %v", err)
	}

	if !strings.Contains(unsupported.Message(), "BMP") {
		t.Errorf("the message should name the format, got %q", unsupported.Message())
	}
}

// TestResizeGeometryIsExactForEveryMode is the S4.3 geometry assert: every
// mode checked numerically, including an odd target, because an off-by-one in
// a rounding rule is invisible on round numbers.
func TestResizeGeometryIsExactForEveryMode(t *testing.T) {
	e, r := engines(t)
	source := fixture(t, e, r, t.TempDir(), vipsimage.PNG, 400, 300)

	cases := []struct {
		name          string
		opts          vipsimage.Options
		width, height int
	}{
		// A 200x200 box and a 4:3 source: fit reaches the box in one axis
		// and stays inside it in the other.
		{
			"fit",
			vipsimage.Options{To: vipsimage.PNG, Resize: vipsimage.ResizeFit, Width: 200, Height: 200},
			200, 150,
		},
		// fill covers the box and crops the overflow, so it is exactly the
		// box.
		{
			"fill",
			vipsimage.Options{To: vipsimage.PNG, Resize: vipsimage.ResizeFill, Width: 200, Height: 200},
			200, 200,
		},
		// exact stretches each axis, so the proportions change.
		{
			"exact",
			vipsimage.Options{To: vipsimage.PNG, Resize: vipsimage.ResizeExact, Width: 200, Height: 200},
			200, 200,
		},
		// An odd target, to catch a rounding rule that only works on even
		// numbers: 101/400 of 300 is 75.75, and libvips rounds to 76.
		{
			"fit_odd",
			vipsimage.Options{To: vipsimage.PNG, Resize: vipsimage.ResizeFit, Width: 101, Height: 999},
			101, 76,
		},
		// Without Upscale a box larger than the image leaves it alone.
		{
			"no_upscale",
			vipsimage.Options{To: vipsimage.PNG, Resize: vipsimage.ResizeFit, Width: 4000, Height: 4000},
			400, 300,
		},
		// With Upscale the same kind of box is reached.
		{
			"upscale",
			vipsimage.Options{
				To: vipsimage.PNG, Resize: vipsimage.ResizeFit,
				Width: 800, Height: 800, Upscale: true,
			},
			800, 600,
		},
		// No resize at all is a re-encode at the original size.
		{"none", vipsimage.Options{To: vipsimage.PNG}, 400, 300},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			input := copyInto(t, source, dir)

			got, err := vipsimage.Convert(context.Background(), e, r, dir, input, c.opts)
			if err != nil {
				t.Fatalf("convert: %v", err)
			}

			if got.Output.Width != c.width || got.Output.Height != c.height {
				t.Errorf("got %dx%d, want %dx%d",
					got.Output.Width, got.Output.Height, c.width, c.height)
			}
		})
	}
}

// TestCropRotateAndFlipApplyInAFixedOrder proves the documented order: a
// 400x300 image cropped to 200x100 and then turned a quarter is 100x200, not
// 200x100.
func TestCropRotateAndFlipApplyInAFixedOrder(t *testing.T) {
	e, r := engines(t)
	dir := t.TempDir()
	input := fixture(t, e, r, dir, vipsimage.PNG, 400, 300)

	got, err := vipsimage.Edit(context.Background(), e, r, dir, input, vipsimage.EditOptions{
		Crop:   &vipsimage.Rect{Left: 10, Top: 20, Width: 200, Height: 100},
		Rotate: vipsimage.Rotate90,
		Flip:   vipsimage.FlipHorizontal,
		Encode: vipsimage.Options{To: vipsimage.PNG},
	})
	if err != nil {
		t.Fatalf("edit: %v", err)
	}

	if got.Output.Width != 100 || got.Output.Height != 200 {
		t.Errorf("got %dx%d, want 100x200 after a crop and a quarter turn",
			got.Output.Width, got.Output.Height)
	}

	// The source reported is the original's, not the last intermediate's.
	if got.Source.Width != 400 || got.Source.Height != 300 {
		t.Errorf("source reported as %dx%d, want 400x300", got.Source.Width, got.Source.Height)
	}
}

// TestACropBeyondTheImageIsRefusedWithTheRealDimensions: the refusal names
// the real size, which is the only thing the caller can act on.
func TestACropBeyondTheImageIsRefusedWithTheRealDimensions(t *testing.T) {
	e, r := engines(t)
	dir := t.TempDir()
	input := fixture(t, e, r, dir, vipsimage.PNG, 400, 300)

	_, err := vipsimage.Edit(context.Background(), e, r, dir, input, vipsimage.EditOptions{
		Crop:   &vipsimage.Rect{Left: 300, Top: 200, Width: 200, Height: 200},
		Encode: vipsimage.Options{To: vipsimage.PNG},
	})

	var crop *vipsimage.CropError
	if !errors.As(err, &crop) {
		t.Fatalf("want a CropError, got %v", err)
	}

	if !strings.Contains(crop.Message(), "400x300") {
		t.Errorf("the message should name the real dimensions, got %q", crop.Message())
	}
}

// TestMetadataIsStrippedByDefaultAndKeptOnRequest is the privacy rule: a
// photograph's EXIF carries GPS coordinates and a device serial number, so it
// goes unless the caller asks for it.
func TestMetadataIsStrippedByDefaultAndKeptOnRequest(t *testing.T) {
	e, r := engines(t)
	source := jpegWithEXIF(t, e, r, t.TempDir())

	for _, keep := range []bool{false, true} {
		t.Run("keep_"+strconv.FormatBool(keep), func(t *testing.T) {
			dir := t.TempDir()
			input := copyInto(t, source, dir)

			got, err := vipsimage.Convert(context.Background(), e, r, dir, input,
				vipsimage.Options{To: vipsimage.JPEG, KeepMetadata: keep})
			if err != nil {
				t.Fatalf("convert: %v", err)
			}

			present := strings.Contains(header(t, e, r, dir, got.Path), "exif-data")
			if present != keep {
				t.Errorf("EXIF present = %v, want %v", present, keep)
			}
		})
	}
}

// TestAnEmbeddedProfileIsConvertedToSRGBBeforeTheStrip is the colour assert:
// an image with a wide-gamut profile must still look like itself after the
// profile is dropped, which only happens if the pixels were transformed
// first.
//
// Asserted as a pixel difference rather than by eye: the same source is
// converted twice, once through the profile-aware path and once with the
// profile simply discarded, and the two must differ. If they did not, the
// transform did nothing and the "colours changed" bug is present.
func TestAnEmbeddedProfileIsConvertedToSRGBBeforeTheStrip(t *testing.T) {
	e, r := engines(t)
	dir := t.TempDir()

	profile := findProfile(t, e, r, dir)
	if profile == "" {
		t.Skip("this build ships no ICC profile to test against")
	}

	seed := seedPNG(t, dir, 64, 64)

	// A copy of the seed that claims a wide-gamut profile without its pixels
	// being changed: the numbers are the same, the meaning is not. Converting
	// it to sRGB must therefore change the numbers.
	tagged := filepath.Join(dir, "tagged.png")
	runVips(t, e, r, dir, "copy", seed+"[access=sequential]", tagged+"[profile="+profile+",keep=icc]")

	converted, err := vipsimage.Convert(context.Background(), e, r, dir, tagged,
		vipsimage.Options{To: vipsimage.PNG})
	if err != nil {
		t.Fatalf("convert the tagged image: %v", err)
	}

	difference := meanAbsoluteDifference(t, e, r, dir, seed, converted.Path)
	if difference <= 0 {
		t.Errorf("the profile was dropped without transforming the pixels: mean difference %f", difference)
	}

	t.Logf("sRGB transform moved the pixels by a mean of %.2f levels", difference)
}

// TestAnImagePastThePixelCapIsRefusedBeforeItIsDecoded proves the cap is
// enforced from the header: the file is a few kilobytes, its claimed frame is
// not, and no decode happens.
func TestAnImagePastThePixelCapIsRefusedBeforeItIsDecoded(t *testing.T) {
	e, r := engines(t)
	dir := t.TempDir()

	// A flat PNG past the cap compresses to almost nothing, which is exactly
	// the shape of a decompression bomb: small on disk, enormous decoded.
	big := filepath.Join(dir, "big.png")
	runVips(t, e, r, dir, "black", big, "8000", "6000")

	_, err := vipsimage.Convert(context.Background(), e, r, dir, big,
		vipsimage.Options{To: vipsimage.JPEG})

	var dimension *vipsimage.DimensionError
	if !errors.As(err, &dimension) {
		t.Fatalf("want a DimensionError, got %v", err)
	}

	if !strings.Contains(dimension.Message(), "8000x6000") {
		t.Errorf("the message should name the real size, got %q", dimension.Message())
	}
}

// TestAFileWhoseNameLiesIsConvertedByItsRealType is the roadmap's assert that
// a file claiming to be PNG while holding JPEG bytes is handled by the
// detected type.
//
// This package never looks at a name, which is the point: it is handed a path
// and asks libvips what the bytes are. The assertion is that a JPEG named
// .png converts correctly and that the engine reports the real loader.
func TestAFileWhoseNameLiesIsConvertedByItsRealType(t *testing.T) {
	e, r := engines(t)
	dir := t.TempDir()

	jpeg := fixture(t, e, r, dir, vipsimage.JPEG, 120, 80)

	body, err := os.ReadFile(jpeg)
	if err != nil {
		t.Fatalf("read the JPEG: %v", err)
	}

	liar := filepath.Join(dir, "actually-a-jpeg.png")
	if err := os.WriteFile(liar, body, 0o600); err != nil {
		t.Fatalf("write the mislabelled file: %v", err)
	}

	probed, err := vipsimage.Probe(context.Background(), e, r, dir, liar)
	if err != nil {
		t.Fatalf("probe the mislabelled file: %v", err)
	}

	if probed.Loader != "jpegload" {
		t.Errorf("loader is %q, want jpegload: the name was trusted", probed.Loader)
	}

	got, err := vipsimage.Convert(context.Background(), e, r, dir, liar,
		vipsimage.Options{To: vipsimage.WebP})
	if err != nil {
		t.Fatalf("convert the mislabelled file: %v", err)
	}

	if got.Output.Width != 120 || got.Output.Height != 80 {
		t.Errorf("got %dx%d, want 120x80", got.Output.Width, got.Output.Height)
	}
}

// TestQualityChangesTheFileSize proves the quality option reaches the encoder
// rather than being built into an argument string nothing reads.
func TestQualityChangesTheFileSize(t *testing.T) {
	e, r := engines(t)
	source := seedPNG(t, t.TempDir(), 400, 300)

	size := func(quality int) int64 {
		t.Helper()

		dir := t.TempDir()
		input := copyInto(t, source, dir)

		got, err := vipsimage.Convert(context.Background(), e, r, dir, input,
			vipsimage.Options{To: vipsimage.JPEG, Quality: quality})
		if err != nil {
			t.Fatalf("convert at Q=%d: %v", quality, err)
		}

		info, err := os.Stat(got.Path)
		if err != nil {
			t.Fatalf("stat the result: %v", err)
		}

		return info.Size()
	}

	low, high := size(20), size(95)
	if low >= high {
		t.Errorf("Q=20 produced %d bytes and Q=95 produced %d: the quality option is not reaching the encoder",
			low, high)
	}

	t.Logf("JPEG Q=20 is %d bytes, Q=95 is %d bytes", low, high)
}
