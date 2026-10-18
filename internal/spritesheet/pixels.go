package spritesheet

import (
	"image"
	"image/color"
)

// pixels reads a sheet's pixels without allocating per pixel.
//
// [image.Image.At] returns a [image/color.Color] interface, so every call boxes
// a colour value onto the heap. Over a 4096 by 4096 sheet that is 16.7 million
// allocations for one pass, which the benchmark measured at 1.27 GB allocated
// and 1.5 s per split before this type existed. Resolving the concrete type
// once and indexing Pix directly removes the whole cost: the methods below
// return a value type, so nothing escapes.
//
// The fallback keeps correctness for a decoder that returns something else —
// a paletted GIF or a 16-bit PNG — at the old cost, which is the right trade
// for the uncommon case.
type pixels struct {
	nrgba *image.NRGBA
	rgba  *image.RGBA
	img   image.Image
	rect  image.Rectangle
}

// newPixels resolves img's concrete type once.
func newPixels(img image.Image) pixels {
	p := pixels{img: img, rect: img.Bounds()}

	switch concrete := img.(type) {
	case *image.NRGBA:
		p.nrgba = concrete
	case *image.RGBA:
		p.rgba = concrete
	}

	return p
}

// at returns the pixel at (x, y) in the image's own coordinates, as a
// non-premultiplied colour.
func (p pixels) at(x, y int) color.NRGBA {
	switch {
	case p.nrgba != nil:
		i := p.nrgba.PixOffset(x, y)
		s := p.nrgba.Pix[i : i+4 : i+4]

		return color.NRGBA{R: s[0], G: s[1], B: s[2], A: s[3]}
	case p.rgba != nil:
		i := p.rgba.PixOffset(x, y)
		s := p.rgba.Pix[i : i+4 : i+4]

		return unpremultiply(s[0], s[1], s[2], s[3])
	default:
		return nrgbaAt(p.img, x, y)
	}
}

// alphaAt returns only the alpha at (x, y), which is all the trim pass needs.
// Reading one byte instead of four is most of the work saved on the commonest
// path.
func (p pixels) alphaAt(x, y int) uint8 {
	switch {
	case p.nrgba != nil:
		return p.nrgba.Pix[p.nrgba.PixOffset(x, y)+3]
	case p.rgba != nil:
		return p.rgba.Pix[p.rgba.PixOffset(x, y)+3]
	default:
		return nrgbaAt(p.img, x, y).A
	}
}

// unpremultiply converts a premultiplied RGBA pixel to straight alpha, which
// is what every comparison in this package is written against.
func unpremultiply(r, g, b, a uint8) color.NRGBA {
	switch a {
	case 0:
		return color.NRGBA{}
	case 0xff:
		return color.NRGBA{R: r, G: g, B: b, A: 0xff}
	}

	return color.NRGBA{
		R: unpremultiplyChannel(r, a),
		G: unpremultiplyChannel(g, a),
		B: unpremultiplyChannel(b, a),
		A: a,
	}
}

// unpremultiplyChannel divides one channel back out by its alpha, clamping the
// result.
//
// The clamp is not defensive padding. A premultiplied pixel is only valid when
// each channel is at most its alpha, and these bytes come from an uploaded
// file: a malformed or hostile image can carry a channel above its alpha, and
// then the division exceeds 255 and a plain conversion would wrap a bright
// pixel round to a dark one. Saturating is the right reading of a broken
// pixel, and it is what keeps the conversion total over every possible input.
func unpremultiplyChannel(v, a uint8) uint8 {
	if scaled := uint32(v) * 0xff / uint32(a); scaled < 0xff {
		return uint8(scaled)
	}

	return 0xff
}

// cropToOrigin returns an image over r's pixels whose bounds start at the
// origin, sharing the source's pixel buffer rather than copying it.
//
// Two things have to be true at once, which is why this is not just
// SubImage. The frame must start at the origin, because png.Encode writes an
// image's bounds as its size. And the result must keep its concrete type,
// because png.Encode type-switches on *image.NRGBA and friends to take a fast
// path; wrapping a SubImage in an image.Image adapter is correct but sends
// every pixel back through the boxing At call, which is what made a 1024-frame
// split allocate 1.27 GB.
//
// SubImage already returns a value whose Pix begins at r's first pixel, so
// re-basing is a matter of rewriting Rect and keeping Pix and Stride: offset
// (0,0) of the new header then lands exactly on r's first pixel.
func cropToOrigin(img image.Image, r image.Rectangle) image.Image {
	size := image.Rect(0, 0, r.Dx(), r.Dy())

	switch concrete := img.(type) {
	case *image.NRGBA:
		sub, ok := concrete.SubImage(r).(*image.NRGBA)
		if !ok {
			break
		}

		return &image.NRGBA{Pix: sub.Pix, Stride: sub.Stride, Rect: size}
	case *image.RGBA:
		sub, ok := concrete.SubImage(r).(*image.RGBA)
		if !ok {
			break
		}

		return &image.RGBA{Pix: sub.Pix, Stride: sub.Stride, Rect: size}
	case *image.NRGBA64:
		sub, ok := concrete.SubImage(r).(*image.NRGBA64)
		if !ok {
			break
		}

		return &image.NRGBA64{Pix: sub.Pix, Stride: sub.Stride, Rect: size}
	case *image.Gray:
		sub, ok := concrete.SubImage(r).(*image.Gray)
		if !ok {
			break
		}

		return &image.Gray{Pix: sub.Pix, Stride: sub.Stride, Rect: size}
	case *image.Paletted:
		sub, ok := concrete.SubImage(r).(*image.Paletted)
		if !ok {
			break
		}

		return &image.Paletted{Pix: sub.Pix, Stride: sub.Stride, Rect: size, Palette: sub.Palette}
	}

	// An image type this package does not know. Copying one frame is
	// acceptable; copying the sheet would not be.
	dst := image.NewNRGBA(size)
	src := newPixels(img)

	for y := range r.Dy() {
		for x := range r.Dx() {
			dst.SetNRGBA(x, y, src.at(r.Min.X+x, r.Min.Y+y))
		}
	}

	return dst
}
