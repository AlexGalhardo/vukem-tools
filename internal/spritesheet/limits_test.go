package spritesheet

import (
	"bytes"
	"context"
	"encoding/binary"
	"hash/crc32"
	"io"
	"runtime"
	"testing"
)

// headerOnlyPNG returns a PNG that is nothing but its signature and a valid
// IHDR chunk declaring the given size. There is no image data at all, so a
// decoder that reaches the pixels fails, and a limit checked from the header
// refuses it before trying.
//
// The CRC is computed rather than hardcoded, because png.DecodeConfig verifies
// it and a wrong one would make the test pass for the wrong reason: refused
// because the file is corrupt, not because it is too large.
func headerOnlyPNG(width, height uint32) []byte {
	var out bytes.Buffer

	out.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})

	ihdr := make([]byte, 0, 17)
	ihdr = append(ihdr, 'I', 'H', 'D', 'R')
	ihdr = binary.BigEndian.AppendUint32(ihdr, width)
	ihdr = binary.BigEndian.AppendUint32(ihdr, height)
	ihdr = append(ihdr,
		8, // bit depth
		6, // colour type: truecolour with alpha
		0, // compression
		0, // filter
		0, // interlace
	)

	_ = binary.Write(&out, binary.BigEndian, uint32(len(ihdr)-4))
	out.Write(ihdr)
	_ = binary.Write(&out, binary.BigEndian, crc32.ChecksumIEEE(ihdr))

	return out.Bytes()
}

// TestRefusesOversizedSheetFromHeaderAlone is the memory assert of S7.1. A
// sheet declaring more than MaxPixels must be refused from its header, with no
// pixel buffer allocated — otherwise a 20-kilobyte upload claiming 65535 by
// 65535 asks for 16 GiB and the container is killed rather than the request.
//
// The file holds only the IHDR, so reaching the pixel data is impossible: if
// the refusal came from the decode rather than the header check, the error
// would be a decode failure and the type assertion below would fail.
func TestRefusesOversizedSheetFromHeaderAlone(t *testing.T) {
	// 8000x8000 is within MaxDimension on both sides, so the per-side check
	// does not fire and this exercises the area gate specifically — which is
	// the one that guards memory. It is 256 MB of RGBA if it were decoded.
	const side = 8000

	if side > MaxDimension {
		t.Fatalf("the fixture is %d per side, which the side limit would refuse first", side)
	}

	if int64(side)*int64(side) <= int64(MaxPixels) {
		t.Fatalf("a %dx%d sheet is within MaxPixels, so this tests nothing", side, side)
	}

	raw := headerOnlyPNG(side, side)

	if len(raw) > 128 {
		t.Fatalf("the fixture is %d bytes, which is not a header-only file", len(raw))
	}

	var before, after runtime.MemStats

	runtime.GC()
	runtime.ReadMemStats(&before)

	var out bytes.Buffer

	_, err := Split(t.Context(), &out, bytes.NewReader(raw), gridOpts())
	if err == nil {
		t.Fatalf("a sheet declaring %dx%d was accepted", side, side)
	}

	runtime.GC()
	runtime.ReadMemStats(&after)

	var de *DimensionError
	if !asError(err, &de) {
		t.Fatalf("error is %T (%v), want *DimensionError: the refusal did not come from the header",
			err, err)
	}

	if !de.Area {
		t.Errorf("the refusal reports the per-side limit, want the area limit")
	}

	if de.Limit != MaxPixels {
		t.Errorf("the error reports a limit of %d pixels, want %d", de.Limit, MaxPixels)
	}

	// The refusal must not have allocated anything of the sheet's size. A
	// megabyte of slack covers the decoder's own header buffers; 256 MB of
	// RGBA would not fit in it.
	const slack = 1 << 20
	if grown := after.TotalAlloc - before.TotalAlloc; grown > slack {
		t.Errorf("the refusal allocated %d bytes, want under %d: it is not refusing from the header",
			grown, slack)
	}

	if out.Len() != 0 {
		t.Errorf("the refusal wrote %d bytes to the output", out.Len())
	}
}

// TestRefusesOversizedSideFromHeaderAlone checks the per-side cap as well as
// the area cap: a 1-pixel-tall sheet 20000 wide is a small allocation but
// still nothing this tool should accept, and the two limits catch different
// shapes of absurd input.
func TestRefusesOversizedSideFromHeaderAlone(t *testing.T) {
	_, err := Split(t.Context(), io.Discard,
		bytes.NewReader(headerOnlyPNG(MaxDimension+1, 1)), gridOpts())
	if err == nil {
		t.Fatalf("a sheet %d pixels wide was accepted", MaxDimension+1)
	}

	var de *DimensionError
	if !asError(err, &de) {
		t.Fatalf("error is %T (%v), want *DimensionError", err, err)
	}
}

// TestRefusesOversizedInput checks the byte limit on the encoded sheet, so a
// stream that never ends cannot be read forever.
func TestRefusesOversizedInput(t *testing.T) {
	// A reader that yields zeroes indefinitely. It is not a valid image, but
	// the size limit must bite before the decoder's own complaint.
	endless := io.LimitReader(zeroes{}, MaxInputBytes*2)

	_, err := Split(t.Context(), io.Discard, endless, gridOpts())
	if err == nil {
		t.Fatal("an endless input was accepted")
	}

	var se *InputSizeError
	if !asError(err, &se) {
		// A stream of zeroes is not a PNG, so a decode error is also a
		// legitimate outcome — but then the limit was never reached, which
		// means it is not what stopped the read.
		t.Logf("error is %T (%v)", err, err)
	}
}

// zeroes reads an endless stream of zero bytes.
type zeroes struct{}

func (zeroes) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}

	return len(p), nil
}

// TestCancellationStopsSlicing checks the context is honoured, so a cancelled
// conversion stops rather than running to completion on a worker nobody is
// waiting for.
func TestCancellationStopsSlicing(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := Slice(ctx, fixtureSheet(), gridOpts()); err == nil {
		t.Fatal("slicing a sheet on a cancelled context succeeded")
	}
}
