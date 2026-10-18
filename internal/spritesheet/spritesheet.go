// Package spritesheet splits a sprite sheet into frames and builds a sheet
// from frames, with the metadata a game engine imports.
//
// It is pure Go over the standard library: slicing and packing PNGs is
// [image/png], [archive/zip] and arithmetic, so there is no engine, no cgo and
// no dependency to add. That is deliberate rather than incidental — an engine
// cannot promise byte-identical output across its own versions, and every
// golden test here depends on exactly that.
//
// # Modes
//
// [ModeGrid] cuts the sheet on a grid the caller describes, by cell size or by
// column and row counts, with a margin outside the cells and a spacing between
// them. It is exact, and it is what the overwhelming majority of sheets are.
//
// [ModeAuto] labels 8-connected components of the non-background mask and
// takes each one's bounding box, which is the answer for a hand-drawn sheet
// whose frames are not a uniform grid. The background is transparency by
// default, or a colour the caller names. A sheet with neither is refused with
// a [*BackgroundError] rather than guessed at: frames that are subtly wrong
// are worse than frames that were never produced.
//
// Labelling is 8-connected, not 4-connected, because pixel art joins at
// corners constantly — a diagonal line is a staircase of corner-touching
// pixels, and 4-connected labelling would shatter one frame into a cloud of
// fragments.
//
// # Trimming
//
// With [SliceOptions.Trim] each frame shrinks to its opaque bounding box, and
// the offset and the untrimmed size travel with it in [Frame.Offset] and
// [Frame.Source]. Those two fields are the whole reason trimming is safe: an
// importer re-inflates the frame to its original box, and without them a
// trimmed animation jitters as each frame lands in a different place.
//
// # Determinism
//
// The same sheet and the same options always produce byte-identical frames,
// archive entries and metadata. Every ordering is total: grid frames are
// row-major, detected frames are grouped into rows by vertical overlap and
// then sorted by x, and archive entries carry a fixed modification time rather
// than the clock.
//
// # Limits
//
// A sheet is refused above [MaxDimension] on either side or [MaxPixels] in
// area, both checked from the image header before any pixel buffer is
// allocated, and above [MaxInputBytes] of encoded data. No more than
// [MaxFrames] frames are produced, checked from the grid before a single frame
// is cut. The area limit is the one that matters on a small container: a PNG
// header is twenty bytes and can claim 65535 by 65535, which is 16 GiB of
// RGBA, so the refusal has to happen before the allocation and not after it.
//
// # Metadata formats
//
// The field names of every writer were taken from the consumer's own parser or
// documentation, not from memory:
//
//   - TexturePacker JSON, hash and array: read by Phaser 3's load.atlas and by
//     PixiJS. CodeAndWeb's own JSON reference could not be reached, so the
//     field set rests on PixiJS's sprite-sheet guide and the golden tests parse
//     the output back rather than trusting the key names.
//     https://pixijs.com/7.x/guides/components/sprite-sheets
//   - libGDX .atlas: read out of libGDX's own parser, TextureAtlas.java, in the
//     modern bounds:/offsets: spelling rather than the deprecated
//     xy:/size:/orig:/offset: one.
//     https://github.com/libgdx/libgdx/blob/master/gdx/src/com/badlogic/gdx/graphics/g2d/TextureAtlas.java
//   - Sparrow/Starling XML: read out of Phaser 3.88.2's AtlasXML.js, with the
//     frameX/frameY/frameWidth/frameHeight attributes written only for a
//     trimmed frame.
//   - Aseprite: shipped as the TexturePacker writers plus Aseprite's
//     extensions, per-frame duration and meta.frameTags. Aseprite's CLI flags
//     are documented but its frame structure is not, so it is described as
//     what it is rather than as a claim about what Aseprite emits.
//     https://www.aseprite.org/docs/cli/
//
// Godot and Unity get no writer, on purpose. Godot 4 needs no metadata for a
// uniform grid — AtlasTexture.region, or the frame counts on an
// AnimatedSprite2D — and Unity's slicing is editor-only, with no documented
// text form of SpriteMetaData. Hand-writing a .tres would be undocumented and
// version-fragile, so the honest deliverable for both is a grid-packed sheet
// and an import instruction.
package spritesheet

import (
	"image"
	"image/color"
	"math"
	"strconv"
)

// The hard limits of a conversion.
const (
	// MaxDimension is the largest accepted width and height, in pixels.
	MaxDimension = 8192
	// MaxPixels is the largest accepted area, in pixels. At four bytes each
	// this is 64 MiB of decoded RGBA, which is the real memory cost of
	// holding a sheet, and it is the limit that keeps this tool inside a
	// small container. 16777216 is 4096 by 4096.
	MaxPixels = 16777216
	// MaxFrames is the most frames one conversion produces. It bounds both
	// the archive a split writes and the canvas a build packs.
	MaxFrames = 2048
	// MaxInputBytes bounds how much encoded sheet data is read, so a
	// malformed or hostile file cannot be streamed indefinitely.
	MaxInputBytes int64 = 64 << 20
	// MaxGutter bounds a margin, a spacing and a padding. Larger than this is
	// not a sheet layout, it is a mistake with a zero too many.
	MaxGutter = 256
)

// Mode selects how the frames of a sheet are found.
type Mode string

// The supported modes.
const (
	// ModeGrid cuts the sheet on a grid the caller describes. It is the
	// default.
	ModeGrid Mode = "grid"
	// ModeAuto finds each frame's bounding box from the sheet's own content.
	ModeAuto Mode = "auto"
)

// Grid describes a uniform layout.
//
// Either the cell size or the column and row counts is given, never both: they
// are two ways of saying one thing, and accepting both would let a caller
// state a contradiction nobody could resolve. The margin is outside the cells,
// at every edge; the spacing is only between cells, never after the last one.
type Grid struct {
	// CellW and CellH are the cell size in pixels.
	CellW, CellH int
	// Cols and Rows are the counts, as an alternative to the cell size.
	Cols, Rows int
	// Margin is the gutter between the sheet's edge and the first cell.
	Margin int
	// Spacing is the gutter between two neighbouring cells.
	Spacing int
}

// Frame is one frame of a sheet.
type Frame struct {
	// Name is the frame's name, which becomes its file name in a split and
	// its key in the metadata.
	Name string
	// Rect is the frame's rectangle in the sheet. With trimming on, this is
	// the trimmed rectangle: the pixels that are actually there.
	Rect image.Rectangle
	// Offset is where Rect's content sits inside the untrimmed frame. It is
	// the zero point for an untrimmed frame.
	Offset image.Point
	// Source is the untrimmed frame's size. It equals Rect's size for an
	// untrimmed frame.
	Source image.Point
	// Trimmed says whether Rect is smaller than Source.
	Trimmed bool
}

// SliceOptions configures finding the frames of a sheet. The zero value is
// valid only for [ModeAuto]; [ModeGrid] needs a [Grid].
type SliceOptions struct {
	// Mode selects how frames are found. Empty means [ModeGrid].
	Mode Mode
	// Grid describes the layout in [ModeGrid].
	Grid Grid
	// Trim shrinks each frame to its opaque bounding box, recording the
	// offset and the untrimmed size.
	Trim bool
	// Background is the colour [ModeAuto] treats as empty. Nil means
	// transparency, and a fully opaque sheet with no colour named is refused.
	Background *color.NRGBA
	// BackgroundTolerance is how far a pixel may be from Background and still
	// count as background, on the same 0 to 255 scale as each channel.
	BackgroundTolerance int
	// MinFrame drops a detected component smaller than this many pixels, as
	// noise. Zero keeps everything.
	MinFrame int
	// Prefix names the frames: the nth frame is Prefix + a zero-padded index.
	// Empty means "frame".
	Prefix string
}

// defaultPrefix names frames when the caller does not.
const defaultPrefix = "frame"

// normalize returns opts with every omitted field replaced by its default, and
// an error for any value the conversion cannot accept.
func (o SliceOptions) normalize() (SliceOptions, error) {
	n := o

	if n.Mode == "" {
		n.Mode = ModeGrid
	}

	switch n.Mode {
	case ModeGrid, ModeAuto:
	default:
		return SliceOptions{}, &OptionError{
			Field: "mode",
			Value: strconv.Quote(string(n.Mode)),
			Want:  `either "grid" or "auto"`,
		}
	}

	if n.Prefix == "" {
		n.Prefix = defaultPrefix
	}

	if n.MinFrame < 0 {
		return SliceOptions{}, &OptionError{
			Field: "min_frame",
			Value: strconv.Itoa(n.MinFrame),
			Want:  "0 or more pixels",
		}
	}

	if n.BackgroundTolerance < 0 || n.BackgroundTolerance > 255 {
		return SliceOptions{}, &OptionError{
			Field: "background_tolerance",
			Value: strconv.Itoa(n.BackgroundTolerance),
			Want:  "0 to 255",
		}
	}

	return n, nil
}

// nrgbaAt returns the pixel at (x, y) as a non-premultiplied colour.
//
// Sheets arrive as NRGBA, paletted, RGBA or grey depending on the encoder, and
// every pixel test in this package is written against straight alpha. The
// common case is handled without going through the model at all; the discarded
// assertion in the fallback cannot fail, because color.NRGBAModel is defined
// to return a color.NRGBA, and a zero value would be a transparent pixel
// rather than a wrong opaque one.
func nrgbaAt(img image.Image, x, y int) color.NRGBA {
	if c, ok := img.At(x, y).(color.NRGBA); ok {
		return c
	}

	c, _ := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)

	return c
}

// squareSideOf returns the side of the largest square whose area is at most
// pixels, for a message that states a limit the person can picture.
func squareSideOf(pixels int) int { return int(math.Sqrt(float64(pixels))) }
