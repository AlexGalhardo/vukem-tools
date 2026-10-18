package spritesheet

import (
	"bytes"
	"fmt"
	"image"
	// Registered so image.Decode and image.DecodeConfig handle the formats
	// the standard library ships. WebP and AVIF are deliberately absent: they
	// would need golang.org/x/image or libvips, and adding a dependency is
	// the owner's decision, not this package's.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
)

// decode reads an encoded sheet from r.
//
// The header is parsed first and the limits are checked against it before any
// pixel data is decoded, which is the point of the whole function: a PNG
// header is twenty bytes and can claim 65535 by 65535, so a decoder that
// reaches the pixels first asks for 16 GiB and the container is killed rather
// than the request refused. The bytes the header parse consumed are replayed
// into the full decode, so r is read once and never has to be seekable.
func decode(r io.Reader) (image.Image, error) {
	return decodeLimited(r, MaxInputBytes)
}

// decodeLimited is decode with the byte limit as a parameter, so a test can
// reach the limit without feeding in 64 MiB.
func decodeLimited(r io.Reader, maxBytes int64) (image.Image, error) {
	limited := &io.LimitedReader{R: r, N: maxBytes + 1}

	var head bytes.Buffer

	cfg, _, err := image.DecodeConfig(io.TeeReader(limited, &head))
	if err != nil {
		if limited.N <= 0 {
			return nil, &InputSizeError{Limit: maxBytes}
		}

		return nil, fmt.Errorf("spritesheet: read the sheet's header: %w", err)
	}

	if err := checkSize(cfg.Width, cfg.Height); err != nil {
		return nil, err
	}

	img, _, err := image.Decode(io.MultiReader(bytes.NewReader(head.Bytes()), limited))
	if err != nil {
		if limited.N <= 0 {
			return nil, &InputSizeError{Limit: maxBytes}
		}

		return nil, fmt.Errorf("spritesheet: decode the sheet: %w", err)
	}

	// A decoder is free to disagree with the header it reported, and an
	// animated GIF reports the logical screen size rather than the frame's,
	// so the real bounds are checked again.
	b := img.Bounds()
	if err := checkSize(b.Dx(), b.Dy()); err != nil {
		return nil, err
	}

	return img, nil
}

// checkSize refuses a sheet that is too large on either side or in area.
//
// Both limits exist because they catch different shapes of absurd input: a
// 20000 by 1 sheet is a small allocation but still not a sprite sheet, and a
// 5000 by 5000 one is within every side limit while being 100 MB of RGBA.
func checkSize(w, h int) error {
	if w <= 0 || h <= 0 {
		return ErrEmptyImage
	}

	if w > MaxDimension || h > MaxDimension {
		return &DimensionError{Width: w, Height: h, Limit: MaxDimension}
	}

	// Multiplied as int64: on a 32-bit build w*h of two large sides would
	// overflow and the comparison would pass.
	if int64(w)*int64(h) > int64(MaxPixels) {
		return &DimensionError{Width: w, Height: h, Limit: MaxPixels, Area: true}
	}

	return nil
}
