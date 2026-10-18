package pdf

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

// ImageInfo is what a raster file's header says about it.
type ImageInfo struct {
	Width, Height int
	// Gray is true for a single-component image, which a PDF has to name
	// /DeviceGray rather than /DeviceRGB. Getting it the wrong way round
	// produces a page of noise, not a slightly wrong colour.
	Gray bool
}

// ErrNotJPEG means the file is not a JPEG this package can embed.
var ErrNotJPEG = errors.New("pdf: not a JPEG")

// jpegHeaderLimit is how far into a JPEG the frame header is looked for.
//
// A JPEG's SOF marker comes after its tables and any EXIF, which is normally
// a few kilobytes and occasionally a few hundred when a camera has embedded a
// thumbnail and a colour profile. The bound is what stops a crafted file with
// a megabyte of comment markers turning a header read into a full-file scan.
const jpegHeaderLimit = 2 << 20

// JPEGInfo reads a JPEG's dimensions and component count from its header.
//
// It is read here rather than by decoding the image because the image is
// never decoded: Write embeds a JPEG's bytes untouched as a DCTDecode stream,
// so the only things it needs are the three numbers that go in the XObject
// dictionary. Decoding a 20-megapixel photograph to learn how wide it is
// would cost 80 MB of pixels per page.
func JPEGInfo(path string) (ImageInfo, error) {
	file, err := os.Open(path)
	if err != nil {
		return ImageInfo{}, fmt.Errorf("pdf: open %s: %w", path, err)
	}

	defer func() { _ = file.Close() }()

	r := bufio.NewReader(io.LimitReader(file, jpegHeaderLimit))

	var start [2]byte
	if _, err := io.ReadFull(r, start[:]); err != nil {
		return ImageInfo{}, ErrNotJPEG
	}

	if start[0] != 0xff || start[1] != 0xd8 {
		return ImageInfo{}, ErrNotJPEG
	}

	return scanJPEGMarkers(r)
}

// scanJPEGMarkers walks the segment chain to the frame header.
func scanJPEGMarkers(r *bufio.Reader) (ImageInfo, error) {
	for {
		marker, err := nextMarker(r)
		if err != nil {
			return ImageInfo{}, err
		}

		// The start-of-frame markers, every one of which carries the
		// dimensions in the same layout: baseline, extended, progressive and
		// lossless. The arithmetic-coded variants (0xc9 to 0xcf, less 0xcc)
		// are listed because a PDF reader accepts them as DCTDecode too.
		if isStartOfFrame(marker) {
			return readFrameHeader(r)
		}

		// Start of scan: the entropy-coded data begins and there is no frame
		// header to find after it.
		if marker == 0xda {
			return ImageInfo{}, ErrNotJPEG
		}

		if err := skipSegment(r); err != nil {
			return ImageInfo{}, err
		}
	}
}

// isStartOfFrame reports whether the marker carries a frame header.
func isStartOfFrame(marker byte) bool {
	switch marker {
	case 0xc0, 0xc1, 0xc2, 0xc3, 0xc5, 0xc6, 0xc7, 0xc9, 0xca, 0xcb, 0xcd, 0xce, 0xcf:
		return true
	default:
		return false
	}
}

// nextMarker reads forward to the next marker byte, skipping the fill bytes a
// JPEG is allowed to pad with.
func nextMarker(r *bufio.Reader) (byte, error) {
	for {
		b, err := r.ReadByte()
		if err != nil {
			return 0, ErrNotJPEG
		}

		if b != 0xff {
			continue
		}

		// A run of 0xff is legal padding before the marker itself.
		for {
			b, err = r.ReadByte()
			if err != nil {
				return 0, ErrNotJPEG
			}

			if b != 0xff {
				break
			}
		}

		// 0x00 after 0xff is a stuffed byte inside entropy data, not a
		// marker.
		if b != 0x00 {
			return b, nil
		}
	}
}

// readFrameHeader reads the five fields a start-of-frame segment begins with.
func readFrameHeader(r *bufio.Reader) (ImageInfo, error) {
	// length(2) precision(1) height(2) width(2) components(1)
	var head [8]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return ImageInfo{}, ErrNotJPEG
	}

	height := int(binary.BigEndian.Uint16(head[3:5]))
	width := int(binary.BigEndian.Uint16(head[5:7]))
	components := int(head[7])

	if width <= 0 || height <= 0 {
		return ImageInfo{}, ErrNotJPEG
	}

	// Four components is CMYK, which a PDF can carry but which needs a
	// /Decode array to come out right more often than not. Refusing is the
	// honest answer; the tool converts the image to RGB first.
	if components != 1 && components != 3 {
		return ImageInfo{}, ErrNotJPEG
	}

	return ImageInfo{Width: width, Height: height, Gray: components == 1}, nil
}

// skipSegment steps over one marker segment using its length.
func skipSegment(r *bufio.Reader) error {
	var length [2]byte
	if _, err := io.ReadFull(r, length[:]); err != nil {
		return ErrNotJPEG
	}

	// The length includes its own two bytes, so a segment shorter than that
	// is malformed and would make the skip go backwards.
	size := int(binary.BigEndian.Uint16(length[:]))
	if size < 2 {
		return ErrNotJPEG
	}

	if _, err := r.Discard(size - 2); err != nil {
		return ErrNotJPEG
	}

	return nil
}

// ErrNotPNG means the file is not a PNG.
var ErrNotPNG = errors.New("pdf: not a PNG")

// pngSignature is the eight bytes every PNG starts with.
var pngSignature = [...]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

// PNGSize reads a PNG's dimensions from its IHDR chunk.
//
// It exists so a test can check a rasterised page's pixel dimensions without
// decoding the image: ROADMAP S6.3 asks that a page of known size rasterise
// to the expected pixel dimensions, and the header is where that answer is.
func PNGSize(path string) (ImageInfo, error) {
	file, err := os.Open(path)
	if err != nil {
		return ImageInfo{}, fmt.Errorf("pdf: open %s: %w", path, err)
	}

	defer func() { _ = file.Close() }()

	// signature(8) length(4) "IHDR"(4) width(4) height(4)
	var head [24]byte
	if _, err := io.ReadFull(file, head[:]); err != nil {
		return ImageInfo{}, ErrNotPNG
	}

	if !bytes.Equal(head[:8], pngSignature[:]) || string(head[12:16]) != "IHDR" {
		return ImageInfo{}, ErrNotPNG
	}

	return ImageInfo{
		Width:  int(binary.BigEndian.Uint32(head[16:20])),
		Height: int(binary.BigEndian.Uint32(head[20:24])),
	}, nil
}
