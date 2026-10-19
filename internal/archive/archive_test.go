package archive_test

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AlexGalhardo/vukem-tools/internal/archive"
)

// write puts content at dir/name and returns the path, so a test reads as the
// archive it is describing rather than as file plumbing.
func write(t *testing.T, dir, name, content string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

func members(t *testing.T, dir string, pairs ...string) []archive.Member {
	t.Helper()

	out := make([]archive.Member, 0, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		out = append(out, archive.Member{Name: pairs[i], Path: write(t, dir, pairs[i], pairs[i+1])})
	}

	return out
}

func TestCreateAZipHoldsEveryMemberWithItsBytes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	in := members(t, dir, "one.txt", "first", "two.txt", "second")

	var buf bytes.Buffer

	got, err := archive.Create(context.Background(), &buf, in, archive.CreateOptions{Format: archive.FormatZip})
	if err != nil {
		t.Fatal(err)
	}

	if got.Members != 2 {
		t.Errorf("Members = %d, want 2", got.Members)
	}

	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]string{"one.txt": "first", "two.txt": "second"}
	if len(zr.File) != len(want) {
		t.Fatalf("the archive holds %d entries, want %d", len(zr.File), len(want))
	}

	for _, f := range zr.File {
		body, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}

		var out bytes.Buffer
		if _, err := out.ReadFrom(body); err != nil {
			t.Fatal(err)
		}

		_ = body.Close()

		if out.String() != want[f.Name] {
			t.Errorf("%s = %q, want %q", f.Name, out.String(), want[f.Name])
		}
	}
}

func TestCreateATarGzRoundTripsThroughExtract(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	in := members(t, dir, "a.txt", "alpha", "b.txt", "beta")

	var buf bytes.Buffer
	if _, err := archive.Create(
		context.Background(), &buf, in, archive.CreateOptions{Format: archive.FormatTarGz},
	); err != nil {
		t.Fatal(err)
	}

	src := filepath.Join(dir, "out.tar.gz")
	if err := os.WriteFile(src, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	dest := t.TempDir()

	got, err := archive.Extract(context.Background(), dest, src)
	if err != nil {
		t.Fatal(err)
	}

	if got.Format != archive.FormatTarGz {
		t.Errorf("Format = %q, want %q: a gzipped tar must not be read as a single file",
			got.Format, archive.FormatTarGz)
	}

	if len(got.Entries) != 2 {
		t.Fatalf("Entries = %d, want 2", len(got.Entries))
	}

	for _, e := range got.Entries {
		body, err := os.ReadFile(e.Path)
		if err != nil {
			t.Fatal(err)
		}

		want := map[string]string{"a.txt": "alpha", "b.txt": "beta"}[e.Name]
		if string(body) != want {
			t.Errorf("%s = %q, want %q", e.Name, body, want)
		}
	}
}

// gzip holds one stream, so a single file is the only thing it can hold. The
// refusal names the format rather than producing an archive holding whichever
// member happened to be first.
func TestCreateRefusesGzipForMoreThanOneMember(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	in := members(t, dir, "a.txt", "alpha", "b.txt", "beta")

	var buf bytes.Buffer

	_, err := archive.Create(context.Background(), &buf, in, archive.CreateOptions{Format: archive.FormatGz})

	opt, ok := errors.AsType[*archive.OptionError](err)
	if !ok {
		t.Fatalf("err = %v, want an *OptionError", err)
	}

	if !strings.Contains(opt.Message(), "gz") {
		t.Errorf("the message does not name the format: %q", opt.Message())
	}
}

func TestExtractAPlainGzipProducesTheOneFileWithoutItsSuffix(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Name = "notes.txt"

	if _, err := zw.Write([]byte("the body")); err != nil {
		t.Fatal(err)
	}

	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	src := filepath.Join(dir, "notes.txt.gz")
	if err := os.WriteFile(src, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := archive.Extract(context.Background(), t.TempDir(), src)
	if err != nil {
		t.Fatal(err)
	}

	if got.Format != archive.FormatGz {
		t.Errorf("Format = %q, want %q", got.Format, archive.FormatGz)
	}

	if len(got.Entries) != 1 {
		t.Fatalf("Entries = %d, want 1", len(got.Entries))
	}

	if got.Entries[0].Name != "notes.txt" {
		t.Errorf("Name = %q, want notes.txt: the .gz suffix is dropped", got.Entries[0].Name)
	}
}

// Zip slip. The entry name is never used as a path, so a traversing name
// becomes a plain basename inside the destination and nothing is written
// above it.
func TestExtractNeverWritesOutsideTheDestination(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	for _, name := range []string{"../../etc/passwd", "/absolute", `..\..\windows\system32`, "sub/dir/ok.txt"} {
		f, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := f.Write([]byte("x")); err != nil {
			t.Fatal(err)
		}
	}

	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	src := filepath.Join(dir, "evil.zip")
	if err := os.WriteFile(src, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	dest := t.TempDir()

	got, err := archive.Extract(context.Background(), dest, src)
	if err != nil {
		t.Fatal(err)
	}

	for _, e := range got.Entries {
		if strings.ContainsAny(e.Name, `/\`) {
			t.Errorf("entry name %q still holds a separator", e.Name)
		}

		abs, err := filepath.Abs(e.Path)
		if err != nil {
			t.Fatal(err)
		}

		if !strings.HasPrefix(abs, dest+string(os.PathSeparator)) {
			t.Errorf("entry %q was written to %q, outside %q", e.Name, abs, dest)
		}
	}

	// Nothing was created next to the destination either, which is what a
	// traversal would have produced.
	if _, err := os.Stat(filepath.Join(filepath.Dir(dest), "etc")); err == nil {
		t.Error("a sibling of the destination was created")
	}
}

func TestExtractRefusesMoreEntriesThanTheCap(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	for i := range archive.MaxEntries + 1 {
		f, err := zw.Create(string(rune('a'+i%26)) + string(rune('a'+i/26)) + ".txt")
		if err != nil {
			t.Fatal(err)
		}

		if _, err := f.Write([]byte("x")); err != nil {
			t.Fatal(err)
		}
	}

	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	src := filepath.Join(dir, "many.zip")
	if err := os.WriteFile(src, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := archive.Extract(context.Background(), t.TempDir(), src)

	limit, ok := errors.AsType[*archive.LimitError](err)
	if !ok {
		t.Fatalf("err = %v, want a *LimitError", err)
	}

	if !strings.Contains(limit.Message(), "64") {
		t.Errorf("the message does not state the cap as a number: %q", limit.Message())
	}
}

// A declared expansion past the total cap is refused from the directory,
// before a single byte is inflated.
func TestExtractRefusesADeclaredBombFromTheDirectoryAlone(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	src := filepath.Join(dir, "bomb.zip")

	if err := os.WriteFile(src, declaredBomb(t, archive.MaxTotalBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := archive.Extract(context.Background(), t.TempDir(), src)

	_, ok := errors.AsType[*archive.LimitError](err)
	if !ok {
		t.Fatalf("err = %v, want a *LimitError", err)
	}
}

// A tar declares no directory, so the total is enforced while reading and the
// refusal must still come before the cap is exceeded on disk.
func TestExtractRefusesATarThatGrowsPastTheTotalCap(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	var raw bytes.Buffer
	tw := tar.NewWriter(&raw)

	if err := tw.WriteHeader(&tar.Header{
		Name: "big", Size: archive.MaxTotalBytes + 1, Mode: 0o644, Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatal(err)
	}

	// The header's declared size is what is checked, so the body need not be
	// written in full for the refusal to be the right one.
	if _, err := tw.Write(make([]byte, 1024)); err != nil && !errors.Is(err, tar.ErrWriteTooLong) {
		t.Fatal(err)
	}

	src := filepath.Join(dir, "big.tar")
	if err := os.WriteFile(src, raw.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := archive.Extract(context.Background(), t.TempDir(), src)

	_, ok := errors.AsType[*archive.LimitError](err)
	if !ok {
		t.Fatalf("err = %v, want a *LimitError", err)
	}
}

// A symlink in a tar is the classic way out of an extraction root even when
// the names are sanitised: the link is created, and the next entry writes
// through it.
func TestExtractRefusesASymlinkEntry(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	var raw bytes.Buffer
	tw := tar.NewWriter(&raw)

	if err := tw.WriteHeader(&tar.Header{
		Name: "link", Linkname: "/etc/passwd", Mode: 0o777, Typeflag: tar.TypeSymlink,
	}); err != nil {
		t.Fatal(err)
	}

	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	src := filepath.Join(dir, "link.tar")
	if err := os.WriteFile(src, raw.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := archive.Extract(context.Background(), t.TempDir(), src)

	member, ok := errors.AsType[*archive.MemberError](err)
	if !ok {
		t.Fatalf("err = %v, want a *MemberError", err)
	}

	if !strings.Contains(member.Message(), "link") {
		t.Errorf("the message does not name the entry: %q", member.Message())
	}
}

// Two entries whose sanitised names collide must both survive, because
// dropping one silently loses a file the archive really held.
func TestExtractKeepsBothOfTwoCollidingNames(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	for _, name := range []string{"one/report.txt", "two/report.txt"} {
		f, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := f.Write([]byte(name)); err != nil {
			t.Fatal(err)
		}
	}

	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	src := filepath.Join(dir, "dup.zip")
	if err := os.WriteFile(src, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := archive.Extract(context.Background(), t.TempDir(), src)
	if err != nil {
		t.Fatal(err)
	}

	if len(got.Entries) != 2 {
		t.Fatalf("Entries = %d, want 2", len(got.Entries))
	}

	if got.Entries[0].Name == got.Entries[1].Name {
		t.Errorf("both entries are called %q; one would overwrite the other", got.Entries[0].Name)
	}
}

func TestExtractRefusesAFileThatIsNotAnArchive(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	src := write(t, dir, "notes.txt", "this is just text")

	_, err := archive.Extract(context.Background(), t.TempDir(), src)

	_, ok := errors.AsType[*archive.FormatError](err)
	if !ok {
		t.Fatalf("err = %v, want a *FormatError", err)
	}
}

// A directory entry carries no bytes and is not a file anybody downloads, so
// it is skipped rather than refused: a ZIP made by any desktop tool holds
// them.
func TestExtractSkipsDirectoryEntries(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	if _, err := zw.Create("folder/"); err != nil {
		t.Fatal(err)
	}

	f, err := zw.Create("folder/file.txt")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := f.Write([]byte("body")); err != nil {
		t.Fatal(err)
	}

	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	src := filepath.Join(dir, "dirs.zip")
	if err := os.WriteFile(src, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := archive.Extract(context.Background(), t.TempDir(), src)
	if err != nil {
		t.Fatal(err)
	}

	if len(got.Entries) != 1 || got.Entries[0].Name != "file.txt" {
		t.Fatalf("Entries = %+v, want just file.txt", got.Entries)
	}
}

// An archive holding nothing worth extracting is refused rather than
// producing a conversion with no result at all.
func TestExtractRefusesAnArchiveWithNoUsableEntry(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	if _, err := zw.Create("folder/"); err != nil {
		t.Fatal(err)
	}

	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	src := filepath.Join(dir, "empty.zip")
	if err := os.WriteFile(src, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := archive.Extract(context.Background(), t.TempDir(), src)

	_, ok := errors.AsType[*archive.MemberError](err)
	if !ok {
		t.Fatalf("err = %v, want a *MemberError", err)
	}
}

// declaredBomb builds a ZIP whose directory claims an enormous expansion for
// a few compressed bytes. The claim is what gets refused, so the test does
// not have to hold the expansion anywhere.
func declaredBomb(t *testing.T, declared int64) []byte {
	t.Helper()

	var raw bytes.Buffer
	zw := zip.NewWriter(&raw)

	f, err := zw.CreateRaw(&zip.FileHeader{
		Name:               "bomb",
		Method:             zip.Deflate,
		CompressedSize64:   uint64(len(deflatedZeros)),
		UncompressedSize64: uint64(declared),
		CRC32:              0,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := f.Write(deflatedZeros); err != nil {
		t.Fatal(err)
	}

	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	return raw.Bytes()
}

// deflatedZeros is a tiny valid deflate stream. Its contents do not matter:
// the refusal comes from the declared size above, not from inflating it.
var deflatedZeros = []byte{0x03, 0x00}

// A bare gzip declares no uncompressed size, so the ratio is the only thing
// that can stop a bomb early. Without it the only bound is the total cap, and
// reaching that means writing half a gigabyte to find out.
func TestExtractRefusesABareGzipThatExpandsTooFar(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)

	// Zeros compress to almost nothing, so this is a few kilobytes of gzip
	// claiming to be well past the floor at a ratio far above the cap.
	zeros := make([]byte, 1<<20)
	for range 8 {
		if _, err := zw.Write(zeros); err != nil {
			t.Fatal(err)
		}
	}

	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	src := filepath.Join(dir, "bomb.gz")
	if err := os.WriteFile(src, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	dest := t.TempDir()

	_, err := archive.Extract(context.Background(), dest, src)

	limit, ok := errors.AsType[*archive.LimitError](err)
	if !ok {
		t.Fatalf("err = %v, want a *LimitError", err)
	}

	if !strings.Contains(limit.Message(), "bomb") {
		t.Errorf("the message does not say what was refused: %q", limit.Message())
	}

	// The refusal has to come early. Anything approaching the total cap means
	// the ratio guard did nothing and the size cap caught it instead.
	written := int64(0)

	entries, err := os.ReadDir(dest)
	if err != nil {
		t.Fatal(err)
	}

	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			t.Fatal(err)
		}

		written += info.Size()
	}

	if written > 4*ratioFloorForTest {
		t.Errorf("%d bytes were written before the refusal, want it stopped near the %d-byte floor",
			written, ratioFloorForTest)
	}
}

// ratioFloorForTest mirrors the package's unexported ratio floor. Declared
// here rather than exported from the package: it is an implementation
// detail of when the ratio starts being judged, not part of the contract,
// and the test only needs an order of magnitude.
const ratioFloorForTest = 1 << 20
