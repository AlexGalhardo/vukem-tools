// Package archive creates and extracts archives.
//
// Four formats, in both directions: ZIP, TAR, TAR.GZ and GZ. All four are the
// standard library — `archive/zip`, `archive/tar` and `compress/gzip` — which
// is why this package adds no dependency and no `/licenses` entry.
//
// XZ and ZSTD are deliberately absent. Neither is in the standard library, and
// both would mean a new module for a format the four above already cover for
// anyone sending files to a web service. `internal/spritesheet` set the
// precedent this follows: pure Go, benchmarkable without a queue, and a
// `git diff go.mod` that is empty over the whole feature.
//
// An archive handed to this package is attacker-controlled, so extraction is
// the hostile direction and is written that way:
//
//   - An entry's name is never used as a path. Only its basename survives, so
//     `../../etc/passwd` becomes `passwd` inside the destination and the zip
//     slip has nowhere to go. Two names that collide after sanitising are both
//     kept, numbered, because dropping one silently loses a file the archive
//     really held.
//   - A symlink, hard link, device or FIFO entry is refused. Sanitising names
//     is not enough on its own: a symlink that is created and then written
//     through is the classic way out of an extraction root.
//   - The entry count and the total uncompressed size are capped. A ZIP
//     declares its sizes in its own directory, so a declared bomb is refused
//     from the directory alone, before a byte is inflated; a TAR declares
//     each entry's size in that entry's header, so the same check runs per
//     entry as the stream is read.
//   - Everything streams. `io.Copy` from the archive to disk and back, never
//     a member held whole in memory, because this service runs on a small
//     container and a member can be as large as the cap.
package archive

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/flate"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gabriel-vasile/mimetype"
)

// Format is one of the four archive formats.
type Format string

// The formats, in the spelling the API uses.
const (
	// FormatZip is a ZIP archive: many members, each deflated on its own.
	FormatZip Format = "zip"
	// FormatTar is an uncompressed TAR: many members, no compression.
	FormatTar Format = "tar"
	// FormatTarGz is a gzipped TAR, which is what "tar.gz" and "tgz" mean.
	FormatTarGz Format = "tar.gz"
	// FormatGz is a bare gzip stream. It holds exactly one file, because
	// gzip compresses a stream rather than wrapping a directory.
	FormatGz Format = "gz"
)

// Formats returns every format this package writes, in the order the
// interface offers them.
func Formats() []Format { return []Format{FormatZip, FormatTarGz, FormatTar, FormatGz} }

// The limits. Each one is refused with a message naming it as a number.
const (
	// MaxEntries bounds how many members an archive may hold, in either
	// direction. Extraction stores one result file per member, and the
	// service signs a download URL for each: a thousand members would be a
	// thousand bucket objects and a browser nobody can download them from.
	// Sixty-four is a project's worth of files rather than a filesystem's.
	MaxEntries = 64
	// MaxTotalBytes bounds the total uncompressed size of an extraction. A
	// decompression bomb is a small file claiming an enormous expansion, so
	// the claim is what gets refused.
	MaxTotalBytes int64 = 512 << 20
	// MaxEntryNameBytes bounds a member's name. The name cannot reach a path
	// — only its basename is used, and only inside the job directory — but it
	// does reach the result row, a Content-Disposition header and a log line.
	MaxEntryNameBytes = 128
	// MaxLevel is the highest compression level. It is deflate's scale, which
	// gzip shares, so one number covers ZIP, TAR.GZ and GZ.
	MaxLevel = 9
	// MaxRatio caps how far one compressed stream may expand.
	//
	// It exists for the bare gzip case and only for it. A ZIP declares every
	// member's uncompressed size in its own directory and a TAR declares it
	// in each member's header, so a bomb in either is refused from the
	// declaration before anything is inflated. A bare gzip declares nothing,
	// so without this the only bound is MaxTotalBytes — and a kilobyte of
	// gzip reaching that means writing half a gigabyte and spending a
	// minute of CPU to discover what the ratio made obvious in the first
	// megabyte.
	//
	// 500 is well clear of real files. Prose and source gzip to roughly 3:1,
	// and even a pathological-but-legitimate case — a log of one repeated
	// line — lands around 100:1.
	MaxRatio = 500
	// ratioFloor is how much output is written before the ratio is judged. A
	// gzip header is a few dozen bytes, so a small file legitimately starts
	// out at a wild ratio; by a mebibyte the figure means something.
	ratioFloor = 1 << 20
)

// Member is one file going into an archive.
type Member struct {
	// Name is what the member is called inside the archive. It is sanitised
	// to a basename, so a name carrying a path writes no path.
	Name string
	// Path is the local file to read the bytes from.
	Path string
}

// CreateOptions configures Create.
type CreateOptions struct {
	// Format is the archive to write.
	Format Format
	// Level is the compression level, 1 to MaxLevel. 0 means the format's own
	// default, which is what a caller that has no opinion should send.
	Level int
}

// CreateResult is what Create wrote.
type CreateResult struct {
	Format Format
	// Members is how many files went in.
	Members int
	// BytesIn is the total size of those files, BytesOut the archive's size.
	// Both are reported so the interface can show what the compression
	// actually bought rather than claiming a ratio.
	BytesIn  int64
	BytesOut int64
}

// Entry is one file that came out of an archive.
type Entry struct {
	// Name is the sanitised, de-duplicated name, which is what the result is
	// stored and downloaded as.
	Name string
	// Path is where it was written, always inside the destination directory.
	Path string
	// Size is its uncompressed size in bytes.
	Size int64
}

// ExtractResult is what Extract produced.
type ExtractResult struct {
	// Format is what the archive turned out to be, read from its bytes rather
	// than from its name.
	Format Format
	// Entries are the files written, in the order the archive held them.
	Entries []Entry
	// Skipped is how many members were directories, which carry no bytes and
	// are not files anybody downloads. Reported rather than swallowed, so a
	// count that does not match the archive has an explanation.
	Skipped int
}

// Create writes an archive of members to w.
//
// Members stream straight from disk into the archive writer: nothing is held
// whole in memory, which is what lets the total be as large as the service's
// upload limit allows.
func Create(ctx context.Context, w io.Writer, members []Member, opts CreateOptions) (CreateResult, error) {
	if err := opts.Check(len(members)); err != nil {
		return CreateResult{}, err
	}

	counted := &countingWriter{w: w}
	named := uniqueNames(members)

	var (
		in  int64
		err error
	)

	switch opts.Format {
	case FormatZip:
		in, err = writeZip(ctx, counted, named, opts.Level)
	case FormatTar:
		in, err = writeTar(ctx, counted, named)
	case FormatTarGz:
		in, err = writeTarGz(ctx, counted, named, opts.Level)
	case FormatGz:
		in, err = writeGz(ctx, counted, named[0], opts.Level)
	default:
		// Unreachable: opts.Check refuses an unknown format above. Kept so
		// adding a format to the constants without adding a writer is a
		// compile-time switch a reader can see rather than a silent nil
		// archive.
		return CreateResult{}, &OptionError{Field: "format", Public: "That archive format is not one this tool writes."}
	}

	if err != nil {
		return CreateResult{}, err
	}

	return CreateResult{
		Format:   opts.Format,
		Members:  len(named),
		BytesIn:  in,
		BytesOut: counted.n,
	}, nil
}

// Check validates the options against the number of members.
//
// Exported so the submit endpoint can apply the same rule before a conversion
// is accepted. The format and the file count only make sense together — gz
// holds one stream — and neither the options nor the file list can see the
// other on its own, so the check has to be callable from where both are
// known. Create calls it too, so a conversion that reached a worker by some
// other path is still refused rather than producing a wrong archive.
func (o CreateOptions) Check(members int) error {
	if !slices.Contains(Formats(), o.Format) {
		return &OptionError{Field: "format", Public: "That archive format is not one this tool writes."}
	}

	if o.Level < 0 || o.Level > MaxLevel {
		return &OptionError{
			Field:  "level",
			Public: fmt.Sprintf("The compression level must be between 1 and %d, or 0 for the default.", MaxLevel),
		}
	}

	switch {
	case members == 0:
		return &OptionError{Field: "file_ids", Public: "An archive needs at least one file."}
	case members > MaxEntries:
		return &OptionError{
			Field:  "file_ids",
			Public: fmt.Sprintf("An archive may hold at most %d files.", MaxEntries),
		}
	case o.Format == FormatGz && members > 1:
		// gzip compresses one stream; it has no directory to put a second
		// file in. Saying so beats writing an archive that silently holds
		// whichever member came first.
		return &OptionError{
			Field: "format",
			Public: "A gz archive holds exactly one file, because gzip compresses a single stream. " +
				"Use zip or tar.gz for several files.",
		}
	}

	return nil
}

// writeZip deflates each member into a ZIP.
func writeZip(ctx context.Context, w io.Writer, members []Member, level int) (int64, error) {
	zw := zip.NewWriter(w)

	if level > 0 {
		zw.RegisterCompressor(zip.Deflate, func(out io.Writer) (io.WriteCloser, error) {
			return flate.NewWriter(out, level)
		})
	}

	var total int64

	for _, m := range members {
		if err := ctx.Err(); err != nil {
			return 0, err
		}

		// Modified is left at its zero value rather than set to now, so the
		// same members always produce byte-identical bytes. The uploads carry
		// no modification time of their own, so there is none to preserve.
		entry, err := zw.CreateHeader(&zip.FileHeader{Name: m.Name, Method: zip.Deflate})
		if err != nil {
			return 0, fmt.Errorf("archive: start the zip entry %q: %w", m.Name, err)
		}

		n, err := copyFile(entry, m.Path)
		if err != nil {
			return 0, err
		}

		total += n
	}

	if err := zw.Close(); err != nil {
		return 0, fmt.Errorf("archive: finish the zip: %w", err)
	}

	return total, nil
}

// writeTar writes an uncompressed TAR.
func writeTar(ctx context.Context, w io.Writer, members []Member) (int64, error) {
	tw := tar.NewWriter(w)

	total, err := tarMembers(ctx, tw, members)
	if err != nil {
		return 0, err
	}

	if err := tw.Close(); err != nil {
		return 0, fmt.Errorf("archive: finish the tar: %w", err)
	}

	return total, nil
}

// writeTarGz writes a TAR through a gzip writer.
func writeTarGz(ctx context.Context, w io.Writer, members []Member, level int) (int64, error) {
	zw, err := gzipWriter(w, level)
	if err != nil {
		return 0, err
	}

	tw := tar.NewWriter(zw)

	total, err := tarMembers(ctx, tw, members)
	if err != nil {
		return 0, err
	}

	if err := tw.Close(); err != nil {
		return 0, fmt.Errorf("archive: finish the tar: %w", err)
	}

	if err := zw.Close(); err != nil {
		return 0, fmt.Errorf("archive: finish the gzip: %w", err)
	}

	return total, nil
}

// tarMembers writes every member into tw.
func tarMembers(ctx context.Context, tw *tar.Writer, members []Member) (int64, error) {
	var total int64

	for _, m := range members {
		if err := ctx.Err(); err != nil {
			return 0, err
		}

		info, err := os.Stat(m.Path)
		if err != nil {
			return 0, fmt.Errorf("archive: measure %q: %w", m.Name, err)
		}

		// The size has to be declared before the bytes are written, which is
		// why tar needs the stat that zip does not. Mode is a fixed 0o644
		// rather than the local file's: the local file is a temporary copy in
		// a job directory, and its mode says nothing about the upload.
		if err := tw.WriteHeader(&tar.Header{
			Name:     m.Name,
			Size:     info.Size(),
			Mode:     0o644,
			Typeflag: tar.TypeReg,
		}); err != nil {
			return 0, fmt.Errorf("archive: start the tar entry %q: %w", m.Name, err)
		}

		n, err := copyFile(tw, m.Path)
		if err != nil {
			return 0, err
		}

		total += n
	}

	return total, nil
}

// writeGz compresses the one member into a bare gzip stream.
func writeGz(ctx context.Context, w io.Writer, member Member, level int) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	zw, err := gzipWriter(w, level)
	if err != nil {
		return 0, err
	}

	// The name goes in the gzip header, which is the only place a bare gzip
	// stream can carry it, so extracting the result gives the file its name
	// back rather than "output".
	zw.Name = member.Name

	n, err := copyFile(zw, member.Path)
	if err != nil {
		return 0, err
	}

	if err := zw.Close(); err != nil {
		return 0, fmt.Errorf("archive: finish the gzip: %w", err)
	}

	return n, nil
}

// gzipWriter returns a gzip writer at this level.
func gzipWriter(w io.Writer, level int) (*gzip.Writer, error) {
	if level == 0 {
		return gzip.NewWriter(w), nil
	}

	zw, err := gzip.NewWriterLevel(w, level)
	if err != nil {
		return nil, &OptionError{
			Field:  "level",
			Public: fmt.Sprintf("The compression level must be between 1 and %d, or 0 for the default.", MaxLevel),
		}
	}

	return zw, nil
}

// copyFile streams one local file into the archive writer.
func copyFile(dst io.Writer, path string) (int64, error) {
	src, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("archive: open %s: %w", filepath.Base(path), err)
	}

	defer func() { _ = src.Close() }()

	n, err := io.Copy(dst, src)
	if err != nil {
		return 0, fmt.Errorf("archive: write %s into the archive: %w", filepath.Base(path), err)
	}

	return n, nil
}

// countingReader counts what it has read, which is what the ratio guard
// measures the expansion against.
type countingReader struct {
	r io.Reader
	n int64
}

// Read implements io.Reader.
func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)

	return n, err
}

// ratioGuard stops a compressed stream that is expanding further than
// MaxRatio.
//
// It sits above the decompressor and below everything that reads it, so the
// refusal happens as the bytes come out rather than after they are all on
// disk. A bomb is therefore refused in its first mebibyte instead of its
// five-hundredth.
type ratioGuard struct {
	r        io.Reader
	consumed *countingReader
	produced int64
}

// Read implements io.Reader.
func (g *ratioGuard) Read(p []byte) (int, error) {
	n, err := g.r.Read(p)
	g.produced += int64(n)

	if g.produced < ratioFloor {
		return n, err
	}

	// A stream that has produced output without the reader having consumed
	// anything is not something to divide by.
	if g.consumed.n <= 0 {
		return n, &LimitError{
			Limit: "compression ratio", Cap: MaxRatio, Actual: g.produced, Unit: "to one",
		}
	}

	if g.produced/g.consumed.n > MaxRatio {
		return n, &LimitError{
			Limit: "compression ratio", Cap: MaxRatio, Actual: g.produced / g.consumed.n, Unit: "to one",
		}
	}

	return n, err
}

// countingWriter counts what passes through it, so the archive's size is
// measured as it is written rather than by stat-ing a file that may not be
// one.
type countingWriter struct {
	w io.Writer
	n int64
}

// Write implements io.Writer.
func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)

	return n, err
}

// Extract unpacks the archive at srcPath into destDir.
//
// The format is read from the archive's own bytes, never from its name: a
// `.zip` holding a tar is extracted as the tar it is, and a file named
// `.tar.gz` that is neither is refused. destDir must exist and nothing is
// ever written outside it.
func Extract(ctx context.Context, destDir, srcPath string) (ExtractResult, error) {
	f, err := os.Open(srcPath)
	if err != nil {
		return ExtractResult{}, fmt.Errorf("archive: open the archive: %w", err)
	}

	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return ExtractResult{}, fmt.Errorf("archive: measure the archive: %w", err)
	}

	head := make([]byte, sniffBytes)

	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return ExtractResult{}, fmt.Errorf("archive: read the archive's header: %w", err)
	}

	kind := mimetype.Detect(head[:n])

	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return ExtractResult{}, fmt.Errorf("archive: rewind the archive: %w", err)
	}

	switch {
	case kind.Is("application/zip"):
		return extractZip(ctx, destDir, f, info.Size())
	case kind.Is("application/x-tar"):
		return extractTar(ctx, destDir, f, FormatTar)
	case kind.Is("application/gzip"):
		// A bare gzip and a gzipped tar have the same header, so which one
		// this is can only be answered from inside the stream.
		return extractGzip(ctx, destDir, f, srcPath)
	default:
		return ExtractResult{}, &FormatError{Detected: kind.String()}
	}
}

// sniffBytes is how much of a stream is read to identify it. It matches
// internal/detect's own header window, and comfortably covers tar's `ustar`
// magic, which sits at offset 257 rather than at the start.
const sniffBytes = 3072

// extractZip writes every file member of a ZIP into destDir.
//
// A ZIP carries its own directory, so both caps are checked against what the
// archive declares before anything is inflated. That is the only way to
// refuse a declared bomb without paying for it.
func extractZip(ctx context.Context, destDir string, r io.ReaderAt, size int64) (ExtractResult, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return ExtractResult{}, &FormatError{Detected: "application/zip"}
	}

	var (
		files []*zip.File
		// Summed as uint64, which is the width the ZIP directory declares it
		// in. Converting each entry to int64 first would be the overflow this
		// is checking for: an entry claiming more than 2^63 bytes would come
		// out negative and pass the cap.
		declared uint64
		skipped  int
	)

	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			skipped++

			continue
		}

		files = append(files, f)

		declared += f.UncompressedSize64
		if declared > uint64(MaxTotalBytes) || declared < f.UncompressedSize64 {
			// The second test catches the sum wrapping, which a directory
			// full of entries each claiming most of the range would do.
			return ExtractResult{}, &LimitError{
				Limit: "total size", Cap: MaxTotalBytes, Actual: MaxTotalBytes + 1, Unit: "bytes",
			}
		}
	}

	if len(files) > MaxEntries {
		return ExtractResult{}, &LimitError{
			Limit: "entries", Cap: MaxEntries, Actual: int64(len(files)), Unit: "entries",
		}
	}

	if len(files) == 0 {
		return ExtractResult{}, &MemberError{
			Reason: "it holds no files",
			Public: "That archive holds no files — only folders, or nothing at all.",
		}
	}

	w := &extractor{dir: destDir, taken: map[string]bool{}}

	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return ExtractResult{}, err
		}

		body, err := f.Open()
		if err != nil {
			return ExtractResult{}, &MemberError{Entry: f.Name, Reason: "it cannot be decompressed"}
		}

		// Clamped in uint64 before narrowing, so the conversion only ever
		// runs on a value already proven to be in range. The loop above
		// refused the archive unless every entry's declared size summed to
		// at most MaxTotalBytes, so the clamp never fires; it is here so the
		// narrowing is correct by construction rather than by that argument.
		declaredEntry := MaxTotalBytes
		if f.UncompressedSize64 <= uint64(MaxTotalBytes) {
			declaredEntry = int64(f.UncompressedSize64)
		}

		err = w.write(f.Name, body, declaredEntry)

		_ = body.Close()

		if err != nil {
			return ExtractResult{}, err
		}
	}

	return ExtractResult{Format: FormatZip, Entries: w.entries, Skipped: skipped}, nil
}

// extractGzip decides whether the gzip stream holds a tar, and extracts it
// either way.
func extractGzip(ctx context.Context, destDir string, r io.Reader, srcPath string) (ExtractResult, error) {
	// Counted underneath the decompressor, so the ratio can be judged from
	// the bytes actually consumed rather than from the file's size: a
	// truncated or padded archive would make the latter a lie.
	consumed := &countingReader{r: r}

	zr, err := gzip.NewReader(consumed)
	if err != nil {
		return ExtractResult{}, &FormatError{Detected: "application/gzip"}
	}

	defer func() { _ = zr.Close() }()

	guarded := &ratioGuard{r: zr, consumed: consumed}

	// Buffered so the first few kilobytes can be identified and then read
	// again by whichever reader turns out to be the right one.
	buf := bufio.NewReaderSize(guarded, sniffBytes)

	head, err := buf.Peek(sniffBytes)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, bufio.ErrBufferFull) {
		return ExtractResult{}, &MemberError{
			Reason: "the gzip stream is truncated",
			Public: "That archive is truncated: the gzip stream ends before its contents do.",
		}
	}

	if mimetype.Detect(head).Is("application/x-tar") {
		return extractTar(ctx, destDir, buf, FormatTarGz)
	}

	// One file. Its name comes from the gzip header when it has one, and from
	// the archive's own name with the suffix removed when it does not.
	name := zr.Name
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(srcPath), ".gz")
	}

	w := &extractor{dir: destDir, taken: map[string]bool{}}

	if err := w.write(name, buf, -1); err != nil {
		return ExtractResult{}, err
	}

	return ExtractResult{Format: FormatGz, Entries: w.entries}, nil
}

// extractTar writes every regular-file member of a TAR into destDir.
//
// A TAR has no central directory, so the total is enforced entry by entry
// from each header's declared size as the stream is read. Declared rather
// than counted, so an entry claiming more than the cap is refused before its
// body is touched.
func extractTar(ctx context.Context, destDir string, r io.Reader, format Format) (ExtractResult, error) {
	tr := tar.NewReader(r)
	w := &extractor{dir: destDir, taken: map[string]bool{}}

	var (
		declared int64
		skipped  int
	)

	for {
		if err := ctx.Err(); err != nil {
			return ExtractResult{}, err
		}

		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return ExtractResult{}, &MemberError{
				Reason: "the tar stream is malformed",
				Public: "That archive is damaged: its entry headers cannot be read.",
			}
		}

		switch header.Typeflag {
		case tar.TypeReg:
			// The one kind that is a file.
		case tar.TypeDir:
			skipped++

			continue
		default:
			// A symlink, hard link, device or FIFO. Sanitised names alone do
			// not make these safe: a link that is created and then written
			// through is the way out of an extraction root, and a device node
			// is not something to materialise from an upload.
			return ExtractResult{}, &MemberError{
				Entry:  header.Name,
				Reason: "it is a link or a device rather than a file",
				Public: fmt.Sprintf(
					"The entry %q is a link or a device rather than a file. This tool extracts files only.",
					truncate(header.Name)),
			}
		}

		declared += header.Size
		if declared > MaxTotalBytes || declared < 0 {
			return ExtractResult{}, &LimitError{
				Limit: "total size", Cap: MaxTotalBytes, Actual: declared, Unit: "bytes",
			}
		}

		if len(w.entries) == MaxEntries {
			return ExtractResult{}, &LimitError{
				Limit: "entries", Cap: MaxEntries, Actual: int64(MaxEntries + 1), Unit: "entries",
			}
		}

		if err := w.write(header.Name, tr, header.Size); err != nil {
			return ExtractResult{}, err
		}
	}

	if len(w.entries) == 0 {
		return ExtractResult{}, &MemberError{
			Reason: "it holds no files",
			Public: "That archive holds no files — only folders, or nothing at all.",
		}
	}

	return ExtractResult{Format: format, Entries: w.entries, Skipped: skipped}, nil
}

// extractor writes members into one directory, keeping the names unique and
// the writes inside it.
type extractor struct {
	dir     string
	taken   map[string]bool
	entries []Entry
}

// write puts one member on disk under a sanitised, unique name.
//
// declared is the size the archive claims, or -1 when it claims none. It is
// used as the copy's ceiling, so a member whose body is longer than its header
// says cannot write past the cap: the limit is enforced on the bytes as well
// as on the claim.
func (e *extractor) write(raw string, body io.Reader, declared int64) error {
	name, ok := safeName(raw)
	if !ok {
		return &MemberError{
			Entry:  raw,
			Reason: "its name is empty, too long or not text",
			Public: fmt.Sprintf("The entry %q has a name this tool cannot use.", truncate(raw)),
		}
	}

	name = e.unique(name)

	// filepath.Join of a directory and a name holding no separator cannot
	// leave that directory, and safeName has already removed every separator.
	path := filepath.Join(e.dir, name)

	// O_EXCL rather than O_TRUNC: unique() has already made the name free, so
	// a name that exists means the two disagree, and overwriting would be the
	// data loss this is all here to prevent.
	dst, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("archive: create %s: %w", name, err)
	}

	ceiling := MaxTotalBytes
	if declared >= 0 && declared < ceiling {
		ceiling = declared
	}

	// One byte past the ceiling, so a body that is longer than it claimed is
	// detected rather than silently truncated to the limit.
	n, copyErr := io.Copy(dst, io.LimitReader(body, ceiling+1))

	if err := dst.Close(); err != nil && copyErr == nil {
		return fmt.Errorf("archive: close %s: %w", name, err)
	}

	if copyErr != nil {
		// A limit the guard above the decompressor reached is already the
		// right answer and the right message; reporting it as a damaged
		// entry would blame the file for the cap.
		if limit, ok := errors.AsType[*LimitError](copyErr); ok {
			return limit
		}

		return &MemberError{
			Entry:  raw,
			Reason: "its contents cannot be read",
			Public: fmt.Sprintf("The entry %q cannot be read: the archive is damaged.", truncate(raw)),
		}
	}

	if n > ceiling {
		return &LimitError{Limit: "total size", Cap: MaxTotalBytes, Actual: n, Unit: "bytes"}
	}

	e.entries = append(e.entries, Entry{Name: name, Path: path, Size: n})

	return nil
}

// unique returns a free name, numbering a collision rather than overwriting.
//
// Two members in different folders very often share a basename — `one/a.txt`
// and `two/a.txt` — and since only the basename survives sanitising, that
// collision is the common case rather than the odd one.
func (e *extractor) unique(name string) string {
	if !e.taken[name] {
		e.taken[name] = true

		return name
	}

	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)

	for i := 2; ; i++ {
		candidate := stem + "-" + strconv.Itoa(i) + ext
		if !e.taken[candidate] {
			e.taken[candidate] = true

			return candidate
		}
	}
}

// safeName reduces an archive entry's name to something that can be a
// filename, and reports whether anything usable was left.
//
// Only the basename survives. That is what makes the zip slip structurally
// impossible rather than checked for: there is no path to validate, because
// no path is ever built from the archive's bytes.
func safeName(raw string) (string, bool) {
	name := raw

	// Both separators, whatever this platform's is. A ZIP written on Windows
	// spells its paths with backslashes, and path.Base would keep one as part
	// of the name.
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}

	name = strings.TrimSpace(name)

	switch {
	case name == "", name == ".", name == "..":
		return "", false
	case len(name) > MaxEntryNameBytes:
		return "", false
	case !utf8.ValidString(name):
		return "", false
	case strings.ContainsRune(name, 0):
		return "", false
	}

	return name, true
}

// uniqueNames sanitises and de-duplicates the names going into an archive.
//
// The members come from uploads, whose names the upload endpoint has already
// sanitised, so this is the second line rather than the first. It matters
// anyway: two uploads may legitimately be called the same thing, and an
// archive with two identically named entries is one that most tools extract
// as one file.
func uniqueNames(members []Member) []Member {
	out := make([]Member, 0, len(members))
	e := &extractor{taken: map[string]bool{}}

	for i, m := range members {
		name, ok := safeName(m.Name)
		if !ok {
			// A member with no usable name still has to go in: refusing the
			// whole archive because one upload was oddly named would be
			// worse than giving it a positional name.
			name = "file-" + strconv.Itoa(i+1)
		}

		out = append(out, Member{Name: e.unique(name), Path: m.Path})
	}

	return out
}
