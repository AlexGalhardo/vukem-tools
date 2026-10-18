package pixelsvg

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	// Registered so image.Decode and image.DecodeConfig handle the three
	// formats the standard library ships. WebP is deliberately absent:
	// golang.org/x/image is not in go.mod and adding a dependency is the
	// owner's decision, not this package's.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
)

// decode reads an encoded image from r.
//
// The header is parsed first with [image.DecodeConfig] and the dimensions are
// checked against [MaxDimension] before any pixel data is decoded, so an
// oversized input costs only the few bytes of its header. The bytes the header
// parse consumed are replayed into the full decode, so r is read once and is
// never required to be seekable.
func decode(r io.Reader) (image.Image, error) {
	return decodeLimited(r, MaxInputBytes)
}

// decodeLimited is decode with the input length limit as a parameter, so a
// test can reach the limit without feeding in 64 MiB.
func decodeLimited(r io.Reader, maxBytes int64) (image.Image, error) {
	limited := &io.LimitedReader{R: r, N: maxBytes + 1}

	var head bytes.Buffer

	cfg, _, err := image.DecodeConfig(io.TeeReader(limited, &head))
	if err != nil {
		if limited.N <= 0 {
			return nil, &InputSizeError{Limit: maxBytes}
		}

		return nil, fmt.Errorf("pixelsvg: read image header: %w", err)
	}

	if cfg.Width > MaxDimension || cfg.Height > MaxDimension {
		return nil, &DimensionError{Width: cfg.Width, Height: cfg.Height, Limit: MaxDimension}
	}

	if cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, ErrEmptyImage
	}

	img, _, err := image.Decode(io.MultiReader(bytes.NewReader(head.Bytes()), limited))
	if err != nil {
		if limited.N <= 0 {
			return nil, &InputSizeError{Limit: maxBytes}
		}

		return nil, fmt.Errorf("pixelsvg: decode image: %w", err)
	}

	// A decoder is free to disagree with the config it reported, and an
	// animated GIF reports the logical screen size rather than the frame's,
	// so the real bounds are checked again.
	b := img.Bounds()
	if b.Dx() > MaxDimension || b.Dy() > MaxDimension {
		return nil, &DimensionError{Width: b.Dx(), Height: b.Dy(), Limit: MaxDimension}
	}

	if b.Dx() <= 0 || b.Dy() <= 0 {
		return nil, ErrEmptyImage
	}

	return img, nil
}

// toNRGBA returns src as a non-premultiplied RGBA image anchored at the
// origin, reusing src when it already is one.
//
// Non-premultiplied is the right buffer for this job: a premultiplied
// [image.RGBA] cannot represent a half-transparent pure red exactly, and
// rounding it back for the fill colour would make two source pixels that
// differ become one, or the reverse.
func toNRGBA(src image.Image) *image.NRGBA {
	b := src.Bounds()
	if n, ok := src.(*image.NRGBA); ok && b.Min == (image.Point{}) {
		return n
	}

	dst := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Src)

	return dst
}
