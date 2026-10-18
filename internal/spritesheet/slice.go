package spritesheet

import (
	"archive/zip"
	"context"
	"fmt"
	"image"
	"image/png"
	"io"
	"sync"
	"time"
)

// Slice returns the frames of a decoded sheet, by grid or by detection, with
// trimming applied when asked for.
func Slice(ctx context.Context, img image.Image, opts SliceOptions) ([]Frame, error) {
	if img == nil {
		return nil, ErrNilImage
	}

	eff, err := opts.normalize()
	if err != nil {
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	b := img.Bounds()
	if err := checkSize(b.Dx(), b.Dy()); err != nil {
		return nil, err
	}

	var frames []Frame

	switch eff.Mode {
	case ModeAuto:
		res, err := Detect(ctx, img, eff)
		if err != nil {
			return nil, err
		}

		frames = res.Frames
	case ModeGrid:
		// The grid is resolved from the sheet's dimensions, not its pixels,
		// so the frame cap and the tiling refusal both land before any work.
		grid, err := gridFrames(b.Dx(), b.Dy(), eff.Grid, eff.Prefix)
		if err != nil {
			return nil, err
		}

		// A grid is described in sheet-relative coordinates; the sheet itself
		// may not start at the origin.
		for i := range grid {
			grid[i].Rect = grid[i].Rect.Add(b.Min)
		}

		frames = grid
	}

	if !eff.Trim {
		return frames, nil
	}

	return trimFrames(ctx, img, frames)
}

// trimFrames shrinks each frame to its opaque bounding box, recording where
// the content sat inside the untrimmed frame.
//
// A frame that is entirely transparent is left alone rather than collapsed to
// nothing: a zero-sized frame is not something any importer can use, and an
// empty cell in an animation is a deliberate gap often enough that dropping it
// would renumber everything after it.
func trimFrames(ctx context.Context, img image.Image, frames []Frame) ([]Frame, error) {
	for i := range frames {
		if i%ctxCheckFrames == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}

		f := &frames[i]

		box, ok := opaqueBounds(img, f.Rect)
		if !ok || box == f.Rect {
			continue
		}

		f.Offset = box.Min.Sub(f.Rect.Min)
		f.Source = image.Pt(f.Rect.Dx(), f.Rect.Dy())
		f.Rect = box
		f.Trimmed = true
	}

	return frames, nil
}

// ctxCheckFrames is how many frames are processed between cancellation checks.
const ctxCheckFrames = 32

// opaqueBounds returns the smallest rectangle inside r holding every pixel
// that is not fully transparent, and whether any such pixel exists.
func opaqueBounds(img image.Image, r image.Rectangle) (image.Rectangle, bool) {
	minX, minY := r.Max.X, r.Max.Y
	maxX, maxY := r.Min.X, r.Min.Y
	found := false
	src := newPixels(img)

	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if src.alphaAt(x, y) == 0 {
				continue
			}

			found = true

			minX = min(minX, x)
			minY = min(minY, y)
			maxX = max(maxX, x)
			maxY = max(maxY, y)
		}
	}

	if !found {
		return image.Rectangle{}, false
	}

	return image.Rect(minX, minY, maxX+1, maxY+1), true
}

// SplitResult reports what a split produced.
type SplitResult struct {
	// Frames is how many frames were written.
	Frames int
	// Dropped is how many components automatic detection discarded as noise.
	Dropped int
	// Sheet is the source sheet's size.
	Sheet image.Point
	// Atlas describes the frames, for a metadata writer.
	Atlas []Frame
}

// Split decodes the sheet in r, cuts it into frames and writes them to w as a
// ZIP archive.
//
// Each frame is encoded straight into the archive as it is cut, through
// [image.Image] views that share the decoded sheet's own pixels, so the peak
// memory is one decoded sheet and one PNG encoder rather than one decoded
// sheet plus every frame. That is what lets a 4096 by 4096 sheet of a thousand
// frames run inside a small container.
func Split(ctx context.Context, w io.Writer, r io.Reader, opts SliceOptions) (SplitResult, error) {
	img, err := decode(r)
	if err != nil {
		return SplitResult{}, err
	}

	frames, err := Slice(ctx, img, opts)
	if err != nil {
		return SplitResult{}, err
	}

	zw := zip.NewWriter(w)

	if err := writeFrames(ctx, zw, img, frames); err != nil {
		// The archive is abandoned rather than closed: a half-written ZIP with
		// a valid central directory looks complete and is worse than one that
		// is obviously truncated.
		return SplitResult{}, err
	}

	if err := zw.Close(); err != nil {
		return SplitResult{}, fmt.Errorf("spritesheet: finish the archive: %w", err)
	}

	b := img.Bounds()

	return SplitResult{
		Frames: len(frames),
		Sheet:  image.Pt(b.Dx(), b.Dy()),
		Atlas:  frames,
	}, nil
}

// fixedModTime is the modification time every archive entry carries.
//
// A fixed time rather than the clock is what makes a split reproducible: the
// same sheet and options must produce byte-identical archives, which a golden
// test can pin and a cache can rely on. The value is the Go release epoch,
// chosen only because it is a real date in range for the ZIP format's
// MS-DOS timestamp.
var fixedModTime = time.Date(2012, time.March, 28, 0, 0, 0, 0, time.UTC)

// writeFrames encodes one PNG per frame into the archive.
func writeFrames(ctx context.Context, zw *zip.Writer, img image.Image, frames []Frame) error {
	// One encoder with a buffer pool for every frame. Without the pool,
	// png.Encode allocates its compressor and its row buffers per call, which
	// the benchmark measured as 1.2 GB allocated across a 1024-frame split —
	// all of it short-lived, and all of it GC pressure the container pays for.
	// BestCompression is wrong here: a sprite frame is tiny and the default
	// gets within a few per cent of it for a fraction of the time.
	enc := &png.Encoder{
		CompressionLevel: png.DefaultCompression,
		BufferPool:       encoderPool{},
	}

	for i := range frames {
		if i%ctxCheckFrames == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}

		f := frames[i]

		entry, err := zw.CreateHeader(&zip.FileHeader{
			Name:     f.Name + ".png",
			Method:   zip.Deflate,
			Modified: fixedModTime,
		})
		if err != nil {
			return fmt.Errorf("spritesheet: add %s to the archive: %w", f.Name, err)
		}

		if err := enc.Encode(entry, cropToOrigin(img, f.Rect)); err != nil {
			return fmt.Errorf("spritesheet: encode %s: %w", f.Name, err)
		}
	}

	return nil
}

// encoderPool lends png.Encode its scratch buffers back between frames.
//
// png.Encoder keeps no state of its own across calls, so without a pool every
// frame allocates a fresh compressor and row buffer. A split writes up to
// MaxFrames of them, which is pure garbage for the collector to walk.
type encoderPool struct{}

// pngBuffers holds the buffers across frames. It is a package-level sync.Pool
// rather than a field because png.EncoderBufferPool is an interface with value
// semantics, and a pool copied by value would lend nothing.
var pngBuffers sync.Pool

// Get implements png.EncoderBufferPool.
func (encoderPool) Get() *png.EncoderBuffer {
	buf, _ := pngBuffers.Get().(*png.EncoderBuffer)

	return buf
}

// Put implements png.EncoderBufferPool.
func (encoderPool) Put(buf *png.EncoderBuffer) { pngBuffers.Put(buf) }
