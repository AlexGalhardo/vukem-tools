package spritesheet

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// framePNG returns a solid frame of the given size, encoded.
func framePNG(tb testing.TB, w, h int, c color.NRGBA) []byte {
	tb.Helper()

	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, c)
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		tb.Fatalf("encode a frame: %v", err)
	}

	return buf.Bytes()
}

// zipEntry is one member of a test archive.
type zipEntry struct {
	name string
	body []byte
	dir  bool
}

// makeZip builds an archive from entries.
func makeZip(tb testing.TB, entries []zipEntry) []byte {
	tb.Helper()

	var buf bytes.Buffer

	zw := zip.NewWriter(&buf)

	for _, e := range entries {
		name := e.name
		if e.dir && !strings.HasSuffix(name, "/") {
			name += "/"
		}

		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
		if err != nil {
			tb.Fatalf("create %q: %v", name, err)
		}

		if _, err := w.Write(e.body); err != nil {
			tb.Fatalf("write %q: %v", name, err)
		}
	}

	if err := zw.Close(); err != nil {
		tb.Fatalf("close the archive: %v", err)
	}

	return buf.Bytes()
}

// buildFrom runs Build over raw archive bytes.
func buildFrom(tb testing.TB, raw []byte, opts BuildOptions) (BuildResult, error) {
	tb.Helper()

	return Build(tb.Context(), io.Discard, bytes.NewReader(raw), int64(len(raw)), opts)
}

// gridBuild is the default build, which is grid layout with no gutters.
func gridBuild() BuildOptions {
	return BuildOptions{Layout: LayoutGrid}
}

// --- Case 1: too many entries ---------------------------------------------

// TestBuildRefusesTooManyEntries checks the entry cap is read from the
// archive's directory rather than discovered by decoding three thousand
// images.
func TestBuildRefusesTooManyEntries(t *testing.T) {
	frame := framePNG(t, 2, 2, color.NRGBA{R: 1, G: 2, B: 3, A: 0xff})

	entries := make([]zipEntry, 0, MaxFrames+1)
	for i := range MaxFrames + 1 {
		entries = append(entries, zipEntry{name: fmt.Sprintf("f%04d.png", i), body: frame})
	}

	_, err := buildFrom(t, makeZip(t, entries), gridBuild())
	if err == nil {
		t.Fatalf("an archive of %d entries was accepted, the cap is %d", MaxFrames+1, MaxFrames)
	}

	var fe *FrameCountError
	if !asError(err, &fe) {
		t.Fatalf("error is %T (%v), want *FrameCountError", err, err)
	}

	if fe.Limit != MaxFrames {
		t.Errorf("the error reports a limit of %d, want %d", fe.Limit, MaxFrames)
	}

	if !strings.Contains(fe.Message(), strconv.Itoa(MaxFrames)) {
		t.Errorf("the public message %q does not state the limit as a number", fe.Message())
	}
}

// --- Case 2: a forged uncompressed size ------------------------------------

// rawZip builds an archive by hand so a test can forge the declared
// uncompressed size, which archive/zip's writer will not do: it computes the
// sizes from what was actually written.
//
// Forging it is the whole point. A decompression bomb is a small archive
// whose directory claims an enormous expansion, and the defence has to read
// that claim and refuse rather than start inflating and find out.
func rawZip(name string, body []byte, declaredSize uint32) []byte {
	var out bytes.Buffer

	sum := crc32.ChecksumIEEE(body)
	nameLen := uint16(len(name))

	// Local file header, method 0 (stored).
	_ = binary.Write(&out, binary.LittleEndian, uint32(0x04034b50))
	_ = binary.Write(&out, binary.LittleEndian, uint16(20)) // version needed
	_ = binary.Write(&out, binary.LittleEndian, uint16(0))  // flags
	_ = binary.Write(&out, binary.LittleEndian, uint16(0))  // method: stored
	_ = binary.Write(&out, binary.LittleEndian, uint16(0))  // mod time
	_ = binary.Write(&out, binary.LittleEndian, uint16(0))  // mod date
	_ = binary.Write(&out, binary.LittleEndian, sum)
	_ = binary.Write(&out, binary.LittleEndian, uint32(len(body))) // compressed
	_ = binary.Write(&out, binary.LittleEndian, declaredSize)      // uncompressed, forged
	_ = binary.Write(&out, binary.LittleEndian, nameLen)
	_ = binary.Write(&out, binary.LittleEndian, uint16(0)) // extra len
	out.WriteString(name)
	out.Write(body)

	cdOffset := out.Len()

	// Central directory entry, carrying the same forged size: zip.Reader
	// reads the directory, so that is the number that has to be refused.
	_ = binary.Write(&out, binary.LittleEndian, uint32(0x02014b50))
	_ = binary.Write(&out, binary.LittleEndian, uint16(20)) // version made by
	_ = binary.Write(&out, binary.LittleEndian, uint16(20)) // version needed
	_ = binary.Write(&out, binary.LittleEndian, uint16(0))  // flags
	_ = binary.Write(&out, binary.LittleEndian, uint16(0))  // method
	_ = binary.Write(&out, binary.LittleEndian, uint16(0))  // mod time
	_ = binary.Write(&out, binary.LittleEndian, uint16(0))  // mod date
	_ = binary.Write(&out, binary.LittleEndian, sum)
	_ = binary.Write(&out, binary.LittleEndian, uint32(len(body)))
	_ = binary.Write(&out, binary.LittleEndian, declaredSize)
	_ = binary.Write(&out, binary.LittleEndian, nameLen)
	_ = binary.Write(&out, binary.LittleEndian, uint16(0)) // extra
	_ = binary.Write(&out, binary.LittleEndian, uint16(0)) // comment
	_ = binary.Write(&out, binary.LittleEndian, uint16(0)) // disk
	_ = binary.Write(&out, binary.LittleEndian, uint16(0)) // internal attrs
	_ = binary.Write(&out, binary.LittleEndian, uint32(0)) // external attrs
	_ = binary.Write(&out, binary.LittleEndian, uint32(0)) // offset of local header
	out.WriteString(name)

	cdSize := out.Len() - cdOffset

	// End of central directory.
	_ = binary.Write(&out, binary.LittleEndian, uint32(0x06054b50))
	_ = binary.Write(&out, binary.LittleEndian, uint16(0)) // disk
	_ = binary.Write(&out, binary.LittleEndian, uint16(0)) // cd disk
	_ = binary.Write(&out, binary.LittleEndian, uint16(1)) // entries on disk
	_ = binary.Write(&out, binary.LittleEndian, uint16(1)) // entries total
	_ = binary.Write(&out, binary.LittleEndian, uint32(cdSize))
	_ = binary.Write(&out, binary.LittleEndian, uint32(cdOffset))
	_ = binary.Write(&out, binary.LittleEndian, uint16(0)) // comment len

	return out.Bytes()
}

// TestBuildRefusesForgedUncompressedSize is the decompression-bomb case. The
// archive is a few hundred bytes and claims 2 GiB, and the refusal must come
// from the claim rather than from inflating it.
func TestBuildRefusesForgedUncompressedSize(t *testing.T) {
	const twoGiB = uint32(2 << 30)

	raw := rawZip("frame.png", framePNG(t, 2, 2, color.NRGBA{A: 0xff}), twoGiB)

	if len(raw) > 4096 {
		t.Fatalf("the fixture is %d bytes, which is not a small archive", len(raw))
	}

	// Sanity: archive/zip must actually report the forged size, or the test
	// would be asserting against a number nothing ever sees.
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("the hand-built archive does not parse: %v", err)
	}

	if got := zr.File[0].UncompressedSize64; got != uint64(twoGiB) {
		t.Fatalf("archive/zip reports %d uncompressed, want the forged %d", got, twoGiB)
	}

	_, err = buildFrom(t, raw, gridBuild())
	if err == nil {
		t.Fatal("an archive claiming 2 GiB uncompressed was accepted")
	}

	var ae *ArchiveError
	if !asError(err, &ae) {
		t.Fatalf("error is %T (%v), want *ArchiveError", err, err)
	}

	if !strings.Contains(ae.Message(), "MB") && !strings.Contains(ae.Message(), "GB") {
		t.Errorf("the public message %q does not state the size limit", ae.Message())
	}
}

// --- Case 3: path traversal, and the design that makes it moot -------------

// traversalNames are the names an archive uses to escape the directory it is
// unpacked into. They are tested as a group because the defence is one
// decision — never treat an entry name as a path — rather than a list of
// patterns to match.
var traversalNames = []string{
	"../../etc/passwd",
	"..\\..\\windows\\system32\\config\\sam",
	"/etc/shadow",
	"C:\\Windows\\System32\\drivers\\etc\\hosts",
	"....//....//escape",
	"a/../../../../../../tmp/escape",
	"..",
	".",
	"...",
	"./././x",
}

// TestBuildCreatesNoFileAnywhere is the assert that matters for traversal, and
// it proves the design rather than the sanitiser.
//
// Testing a sanitiser's output only ever proves the sanitiser handles the
// inputs somebody thought of. This instead proves there is nothing to
// sanitise: Build takes an io.ReaderAt and an io.Writer and never touches the
// filesystem, so an entry name cannot reach a path operation however it is
// spelled. The test watches the whole working tree and the temporary
// directory across a build whose every entry is an escape attempt, and
// asserts not one file appeared.
func TestBuildCreatesNoFileAnywhere(t *testing.T) {
	frame := framePNG(t, 4, 4, color.NRGBA{R: 9, G: 9, B: 9, A: 0xff})

	entries := make([]zipEntry, 0, len(traversalNames))
	for _, name := range traversalNames {
		entries = append(entries, zipEntry{name: name, body: frame})
	}

	// A sandbox the build has no reason to write into, watched for anything
	// appearing. TempDir is removed for us when the test ends.
	sandbox := t.TempDir()

	before := treeOf(t, sandbox)
	cwdBefore := treeOf(t, ".")

	// The result does not matter here — every name may legitimately be
	// refused. What matters is that no file was created either way.
	_, _ = buildFrom(t, makeZip(t, entries), gridBuild())

	if after := treeOf(t, sandbox); !equalTrees(before, after) {
		t.Errorf("the build created or removed files under %s: before %v, after %v",
			sandbox, before, after)
	}

	if after := treeOf(t, "."); !equalTrees(cwdBefore, after) {
		t.Errorf("the build created or removed files in the working directory: before %v, after %v",
			cwdBefore, after)
	}
}

// treeOf lists every path under root, relative to it.
func treeOf(tb testing.TB, root string) []string {
	tb.Helper()

	var out []string

	err := filepath.WalkDir(root, func(path string, _ os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		out = append(out, rel)

		return nil
	})
	if err != nil {
		tb.Fatalf("walk %s: %v", root, err)
	}

	return out
}

// equalTrees reports whether two listings hold the same paths.
func equalTrees(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	seen := make(map[string]int, len(a))
	for _, p := range a {
		seen[p]++
	}

	for _, p := range b {
		seen[p]--
		if seen[p] < 0 {
			return false
		}
	}

	return true
}

// TestBuildTakesNoPathFromTheCaller pins the signature itself, because that is
// where the traversal defence actually lives.
//
// Build reads from an io.ReaderAt and writes to an io.Writer. If somebody ever
// adds a path or a directory parameter to it, every entry name becomes
// attacker-controlled input to a path operation again, and this test is what
// says so out loud rather than leaving the property implicit.
func TestBuildTakesNoPathFromTheCaller(t *testing.T) {
	// The archive's entry names are the escape attempts; the build is given
	// nowhere to put anything.
	raw := makeZip(t, []zipEntry{
		{name: "../../escape.png", body: framePNG(t, 2, 2, color.NRGBA{A: 0xff})},
		{name: "ok.png", body: framePNG(t, 2, 2, color.NRGBA{A: 0xff})},
	})

	var sheet bytes.Buffer

	res, err := Build(t.Context(), &sheet, bytes.NewReader(raw), int64(len(raw)), gridBuild())
	if err != nil {
		// A refusal is acceptable. What is not acceptable is a path.
		return
	}

	for _, f := range res.Atlas {
		if strings.ContainsAny(f.Name, `/\`) {
			t.Errorf("frame name %q carries a path separator into the atlas", f.Name)
		}

		if strings.Contains(f.Name, "..") {
			t.Errorf("frame name %q carries a traversal into the atlas", f.Name)
		}
	}
}

// --- Case 4: directory entries ---------------------------------------------

// TestBuildRefusesDirectoryEntries checks a directory member is refused by
// name rather than decoded as an image.
func TestBuildRefusesDirectoryEntries(t *testing.T) {
	raw := makeZip(t, []zipEntry{
		{name: "frames", dir: true},
		{name: "frame.png", body: framePNG(t, 4, 4, color.NRGBA{A: 0xff})},
	})

	_, err := buildFrom(t, raw, gridBuild())
	if err == nil {
		t.Fatal("an archive holding a directory entry was accepted")
	}

	var ae *ArchiveError
	if !asError(err, &ae) {
		t.Fatalf("error is %T (%v), want *ArchiveError", err, err)
	}

	if !strings.Contains(ae.Error(), "frames") {
		t.Errorf("the error %q does not name the offending entry", ae.Error())
	}
}

// --- Case 5: entries that are not images -----------------------------------

// TestBuildRefusesNonImageEntries checks an entry whose bytes are not an image
// this package decodes is refused, naming it, rather than skipped silently: a
// person whose archive holds a stray readme needs to be told, not to receive a
// sheet quietly missing a frame.
func TestBuildRefusesNonImageEntries(t *testing.T) {
	cases := map[string][]byte{
		"readme.txt":    []byte("these are my frames"),
		"frame.png":     []byte("PNG-ish but not a PNG"),
		"empty.png":     {},
		"truncated.png": framePNG(t, 4, 4, color.NRGBA{A: 0xff})[:20],
	}

	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			raw := makeZip(t, []zipEntry{
				{name: "good.png", body: framePNG(t, 4, 4, color.NRGBA{A: 0xff})},
				{name: name, body: body},
			})

			_, err := buildFrom(t, raw, gridBuild())
			if err == nil {
				t.Fatalf("an archive holding %q was accepted", name)
			}

			var ae *ArchiveError
			if !asError(err, &ae) {
				t.Fatalf("error is %T (%v), want *ArchiveError", err, err)
			}

			if !strings.Contains(ae.Error(), name) {
				t.Errorf("the error %q does not name the offending entry", ae.Error())
			}
		})
	}
}

// --- Case 6: an over-long name ---------------------------------------------

// TestBuildRefusesOverlongName checks a name long enough to blow a path limit
// is refused on its length.
//
// It cannot reach a path on this design, but it can still reach a metadata
// file and an engine's own asset loader, and a 60000-character frame key is
// not something to pass on. The limit is this package's, stated in the error.
func TestBuildRefusesOverlongName(t *testing.T) {
	long := strings.Repeat("a", MaxFrameNameBytes+1) + ".png"

	raw := makeZip(t, []zipEntry{
		{name: long, body: framePNG(t, 4, 4, color.NRGBA{A: 0xff})},
	})

	_, err := buildFrom(t, raw, gridBuild())
	if err == nil {
		t.Fatalf("an entry name of %d characters was accepted", len(long))
	}

	var ae *ArchiveError
	if !asError(err, &ae) {
		t.Fatalf("error is %T (%v), want *ArchiveError", err, err)
	}

	if !strings.Contains(ae.Message(), strconv.Itoa(MaxFrameNameBytes)) {
		t.Errorf("the public message %q does not state the length limit", ae.Message())
	}
}

// --- Case 7: names that collide after the path is dropped ------------------

// TestBuildRefusesCollidingFrameNames is the case that silently corrupts a
// sheet rather than failing it.
//
// Two entries in different directories can share a basename. Since the path is
// deliberately dropped and only the basename is kept as the frame name, the
// second would overwrite the first in the atlas: the sheet would hold both
// frames while the metadata described one, and every frame after it would be
// off by one. That is a wrong sheet delivered as a correct one, so it is
// refused.
func TestBuildRefusesCollidingFrameNames(t *testing.T) {
	frame := framePNG(t, 4, 4, color.NRGBA{A: 0xff})

	cases := map[string][]string{
		"same basename in two directories": {"walk/frame.png", "jump/frame.png"},
		"path against bare name":           {"walk/frame.png", "frame.png"},
		"case only":                        {"Frame.png", "frame.png"},
	}

	for name, names := range cases {
		t.Run(name, func(t *testing.T) {
			entries := make([]zipEntry, 0, len(names))
			for _, n := range names {
				entries = append(entries, zipEntry{name: n, body: frame})
			}

			_, err := buildFrom(t, makeZip(t, entries), gridBuild())
			if err == nil {
				t.Fatalf("entries %v collide into one frame name and were accepted", names)
			}

			var ae *ArchiveError
			if !asError(err, &ae) {
				t.Fatalf("error is %T (%v), want *ArchiveError", err, err)
			}
		})
	}
}

// TestBuildAcceptsFramesInDirectories checks the flip side: entries in
// subdirectories are fine as long as their basenames are distinct, because a
// person exporting from Aseprite gets exactly that layout.
func TestBuildAcceptsFramesInDirectories(t *testing.T) {
	frame := framePNG(t, 8, 8, color.NRGBA{R: 7, G: 7, B: 7, A: 0xff})

	raw := makeZip(t, []zipEntry{
		{name: "walk/walk_0.png", body: frame},
		{name: "walk/walk_1.png", body: frame},
		{name: "jump/jump_0.png", body: frame},
	})

	res, err := buildFrom(t, raw, gridBuild())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if res.Frames != 3 {
		t.Fatalf("packed %d frames, want 3", res.Frames)
	}

	want := map[string]bool{"walk_0": true, "walk_1": true, "jump_0": true}
	for _, f := range res.Atlas {
		if !want[f.Name] {
			t.Errorf("frame name %q is not one of the three expected basenames", f.Name)
		}
	}
}
