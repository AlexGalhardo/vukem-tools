package pixelsvg

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// encodePNG encodes an image the way a caller would upload it.
func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}

	return buf.Bytes()
}

// encodeGIF encodes an image as GIF.
func encodeGIF(t *testing.T, img image.Image) []byte {
	t.Helper()

	var buf bytes.Buffer
	if err := gif.Encode(&buf, img, nil); err != nil {
		t.Fatalf("encode gif: %v", err)
	}

	return buf.Bytes()
}

// encodeJPEG encodes an image as JPEG.
func encodeJPEG(t *testing.T, img image.Image) []byte {
	t.Helper()

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}

	return buf.Bytes()
}

// TestDimensionLimit checks that an input one pixel over the cap is refused,
// that the message names both the limit and the actual size, and that the
// refusal costs no memory: the header is enough to decide, so the pixel data
// is never decoded.
func TestDimensionLimit(t *testing.T) {
	cases := []struct {
		name          string
		width, height int
	}{
		{"one-too-wide", MaxDimension + 1, 1},
		{"one-too-tall", 1, MaxDimension + 1},
		{"both-too-big", MaxDimension + 1, MaxDimension + 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := encodePNG(t, image.NewNRGBA(image.Rect(0, 0, tc.width, tc.height)))

			var before, after runtime.MemStats

			runtime.GC()
			runtime.ReadMemStats(&before)

			_, err := Convert(t.Context(), io.Discard, bytes.NewReader(data), Options{})

			runtime.GC()
			runtime.ReadMemStats(&after)

			var dim *DimensionError
			if !errors.As(err, &dim) {
				t.Fatalf("error = %v, want a *DimensionError", err)
			}

			if dim.Width != tc.width || dim.Height != tc.height {
				t.Errorf("error reports %dx%d, want %dx%d", dim.Width, dim.Height, tc.width, tc.height)
			}

			if dim.Limit != MaxDimension {
				t.Errorf("error reports a limit of %d, want %d", dim.Limit, MaxDimension)
			}

			msg := err.Error()
			for _, want := range []string{
				strconv.Itoa(tc.width) + "x" + strconv.Itoa(tc.height),
				strconv.Itoa(MaxDimension) + "x" + strconv.Itoa(MaxDimension),
			} {
				if !strings.Contains(msg, want) {
					t.Errorf("message %q does not name %q", msg, want)
				}
			}

			// A 2049-pixel-wide RGBA buffer alone would be 8 KiB per row;
			// refusing from the header must not allocate anything of that
			// order, let alone a whole grid.
			if grew := int64(after.HeapAlloc) - int64(before.HeapAlloc); grew > 1<<20 {
				t.Errorf("heap grew by %d bytes while refusing the input", grew)
			}
		})
	}
}

// TestDimensionLimitAtCap checks the cap itself is accepted, so the refusal is
// strictly above it.
func TestDimensionLimitAtCap(t *testing.T) {
	src := solidImage(MaxDimension, 2, color.NRGBA{G: 0xff, A: 0xff})

	res, err := ConvertImage(t.Context(), io.Discard, src, Options{})
	if err != nil {
		t.Fatalf("convert at the cap: %v", err)
	}

	if res.Width != MaxDimension {
		t.Errorf("width = %d, want %d", res.Width, MaxDimension)
	}
}

// TestDimensionLimitFromDecodedImage checks ConvertImage refuses an oversized
// image too, not only Convert.
func TestDimensionLimitFromDecodedImage(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, MaxDimension+1, 1))

	_, err := ConvertImage(t.Context(), io.Discard, src, Options{})
	if _, ok := errors.AsType[*DimensionError](err); !ok {
		t.Fatalf("error = %v, want a *DimensionError", err)
	}
}

// manyColorsImage returns an image with n distinct opaque colours.
func manyColorsImage(n int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, n, 1))

	for i := range n {
		img.SetNRGBA(i, 0, color.NRGBA{
			R: uint8(i & 0xff),
			G: uint8((i >> 8) & 0xff),
			B: uint8((i / 65536) & 0xff),
			A: 0xff,
		})
	}

	return img
}

// TestColorCapRefused checks an image over the colour cap is refused when no
// quantiser is selected, with the limit and the actual count in the message.
func TestColorCapRefused(t *testing.T) {
	src := manyColorsImage(300)

	opts := Options{Mode: ModeRect, Quantizer: QuantizerNone}

	_, err := ConvertImage(t.Context(), io.Discard, src, opts)

	var cc *ColorCountError
	if !errors.As(err, &cc) {
		t.Fatalf("error = %v, want a *ColorCountError", err)
	}

	if cc.Count != 300 || cc.Limit != ColorCap {
		t.Errorf("error reports %d colours over a limit of %d, want 300 over %d", cc.Count, cc.Limit, ColorCap)
	}

	for _, want := range []string{"300", strconv.Itoa(ColorCap)} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message %q does not name %q", err.Error(), want)
		}
	}
}

// TestColorCapQuantised checks the default quantiser brings the same image
// under the cap instead of refusing it.
func TestColorCapQuantised(t *testing.T) {
	src := manyColorsImage(300)

	res, err := ConvertImage(t.Context(), io.Discard, src, Options{Mode: ModeRect})
	if err != nil {
		t.Fatalf("convert: %v", err)
	}

	if res.Colors > ColorCap {
		t.Errorf("colours = %d, want at most %d", res.Colors, ColorCap)
	}

	if res.Colors < 2 {
		t.Errorf("colours = %d, want the palette to survive quantisation", res.Colors)
	}
}

// TestColorCapOption checks a MaxColors above the cap is a configuration
// error, naming the limit and the value given.
func TestColorCapOption(t *testing.T) {
	_, err := ConvertImage(t.Context(), io.Discard, solidImage(2, 2, color.NRGBA{A: 0xff}),
		Options{MaxColors: 300})

	var oe *OptionError
	if !errors.As(err, &oe) {
		t.Fatalf("error = %v, want an *OptionError", err)
	}

	if oe.Field != "MaxColors" {
		t.Errorf("error names field %q, want MaxColors", oe.Field)
	}

	for _, want := range []string{"300", strconv.Itoa(ColorCap)} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message %q does not name %q", err.Error(), want)
		}
	}
}

// TestOptionValidation checks every rejected option value.
func TestOptionValidation(t *testing.T) {
	cases := []struct {
		name  string
		opts  Options
		field string
	}{
		{"mode", Options{Mode: "curves"}, "Mode"},
		{"alpha", Options{Alpha: "premultiply"}, "Alpha"},
		{"quantizer", Options{Quantizer: "octree"}, "Quantizer"},
		{"max-colors-high", Options{MaxColors: ColorCap + 1}, "MaxColors"},
		{"max-colors-low", Options{MaxColors: -2}, "MaxColors"},
		{"tolerance", Options{Tolerance: 256}, "Tolerance"},
		{"scale", Options{Scale: -1}, "Scale"},
		{"simplify", Options{SimplifyTolerance: -0.5}, "SimplifyTolerance"},
		{"workers", Options{Workers: -1}, "Workers"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ConvertImage(t.Context(), io.Discard, solidImage(2, 2, color.NRGBA{A: 0xff}), tc.opts)

			var oe *OptionError
			if !errors.As(err, &oe) {
				t.Fatalf("error = %v, want an *OptionError", err)
			}

			if oe.Field != tc.field {
				t.Errorf("error names field %q, want %q", oe.Field, tc.field)
			}
		})
	}
}

// TestInputSizeLimit checks encoded input longer than the limit is refused
// rather than streamed indefinitely.
func TestInputSizeLimit(t *testing.T) {
	data := encodePNG(t, generatedImage(128, 128, 8, 3))

	_, err := decodeLimited(bytes.NewReader(data), int64(len(data)/2))

	var se *InputSizeError
	if !errors.As(err, &se) {
		t.Fatalf("error = %v, want an *InputSizeError", err)
	}

	if !strings.Contains(err.Error(), strconv.FormatInt(se.Limit, 10)) {
		t.Errorf("message %q does not name the limit", err.Error())
	}
}

// TestEmptyAndNilInput checks the degenerate inputs.
func TestEmptyAndNilInput(t *testing.T) {
	if _, err := ConvertImage(t.Context(), io.Discard, nil, Options{}); !errors.Is(err, ErrNilImage) {
		t.Errorf("nil image error = %v, want ErrNilImage", err)
	}

	src := image.NewNRGBA(image.Rect(0, 0, 0, 0))
	if _, err := ConvertImage(t.Context(), io.Discard, src, Options{}); !errors.Is(err, ErrEmptyImage) {
		t.Errorf("empty image error = %v, want ErrEmptyImage", err)
	}
}

// TestFullyTransparentImage checks an image with nothing to draw still
// produces a valid, empty document.
func TestFullyTransparentImage(t *testing.T) {
	var buf bytes.Buffer

	res, err := ConvertImage(t.Context(), &buf, image.NewNRGBA(image.Rect(0, 0, 4, 4)), Options{})
	if err != nil {
		t.Fatalf("convert: %v", err)
	}

	if res.Colors != 0 || res.Elements != 0 {
		t.Errorf("result = %+v, want no colours and no elements", res)
	}

	doc := parseSVG(t, buf.String())
	if len(doc.groups) != 0 {
		t.Errorf("document has %d groups, want none", len(doc.groups))
	}
}

// TestCancellation checks a cancelled context stops a conversion instead of
// running to completion.
func TestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	src := solidImage(512, 512, color.NRGBA{R: 0xff, A: 0xff})

	_, err := ConvertImage(ctx, io.Discard, src, Options{})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}

// failingWriter fails after letting a fixed number of bytes through.
type failingWriter struct {
	left int
}

// Write implements io.Writer.
func (f *failingWriter) Write(p []byte) (int, error) {
	if len(p) > f.left {
		n := f.left
		f.left = 0

		return n, errors.New("writer full")
	}

	f.left -= len(p)

	return len(p), nil
}

// TestWriteErrorSurfaces checks a destination failing mid-document is
// reported, not swallowed by the sticky-error writer.
func TestWriteErrorSurfaces(t *testing.T) {
	src := generatedImage(128, 128, 16, 4)

	_, err := ConvertImage(t.Context(), &failingWriter{left: 100}, src, Options{})
	if err == nil {
		t.Fatal("convert succeeded although the destination failed")
	}

	if !strings.Contains(err.Error(), "writer full") {
		t.Errorf("error = %v, want the destination's own error", err)
	}
}

// TestDecodeFormats checks the three formats the standard library registers,
// and that WebP is not silently claimed.
func TestDecodeFormats(t *testing.T) {
	src := generatedImage(16, 16, 4, 4)

	t.Run("png", func(t *testing.T) {
		if _, err := decode(bytes.NewReader(encodePNG(t, src))); err != nil {
			t.Errorf("decode png: %v", err)
		}
	})

	t.Run("gif", func(t *testing.T) {
		if _, err := decode(bytes.NewReader(encodeGIF(t, src))); err != nil {
			t.Errorf("decode gif: %v", err)
		}
	})

	t.Run("jpeg", func(t *testing.T) {
		if _, err := decode(bytes.NewReader(encodeJPEG(t, src))); err != nil {
			t.Errorf("decode jpeg: %v", err)
		}
	})

	t.Run("webp", func(t *testing.T) {
		// RIFF container with a VP8L chunk: a real WebP header. The package
		// does not register WebP, so this must fail as an unknown format
		// rather than be mistaken for something else.
		webp := []byte("RIFF\x24\x00\x00\x00WEBPVP8L\x18\x00\x00\x00")
		if _, err := decode(bytes.NewReader(webp)); err == nil {
			t.Error("decoded a WebP although the format is not registered")
		}
	})
}
