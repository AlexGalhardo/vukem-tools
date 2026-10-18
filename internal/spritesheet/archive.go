package spritesheet

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"path"
	"strings"
	"unicode/utf8"
)

// Archive limits. An uploaded archive is attacker-controlled, so each of these
// is read from the archive's own directory and refused before anything is
// inflated or decoded.
const (
	// MaxArchiveUncompressed bounds the total declared uncompressed size of
	// an archive. A decompression bomb is a small file whose directory claims
	// an enormous expansion, so the claim is what gets refused: starting to
	// inflate and finding out is the failure mode this prevents.
	MaxArchiveUncompressed int64 = 512 << 20
	// MaxFrameNameBytes bounds a frame's name. The name cannot reach a path
	// in this package, but it does reach the metadata and from there an
	// engine's asset loader, and a 60000-character frame key is not something
	// to pass on.
	MaxFrameNameBytes = 128
)

// ArchiveError reports an archive this package will not read, or a member of
// one it will not accept.
type ArchiveError struct {
	// Entry is the offending member's name as the archive spelled it, empty
	// when the fault is the archive as a whole.
	Entry string
	// Reason is what is wrong, for the log.
	Reason string
	// Public is the sentence to show the person. It states any limit as a
	// number, because somebody who hit a cap needs to know what it is.
	Public string
}

// Error implements error.
func (e *ArchiveError) Error() string {
	if e.Entry == "" {
		return "spritesheet: " + e.Reason
	}

	return fmt.Sprintf("spritesheet: %q: %s", e.Entry, e.Reason)
}

// Message is the sentence to show the person.
func (e *ArchiveError) Message() string {
	if e.Public != "" {
		return e.Public
	}

	if e.Entry == "" {
		return "That archive cannot be read: " + e.Reason + "."
	}

	return fmt.Sprintf("The archive entry %q cannot be used: %s.", e.Entry, e.Reason)
}

// member is one frame-bearing entry of an archive, after its header has been
// read and before its pixels have been.
type member struct {
	// name is the frame name: the entry's basename with its extension
	// removed. The directory is deliberately dropped, which is why a
	// collision between two members is refused rather than resolved.
	name string
	// entry is the archive member, for the second pass.
	entry *zip.File
	// size is the frame's dimensions, read from its image header alone.
	size image.Point
}

// readMembers is pass one: it reads every entry's header and nothing else.
//
// This is the half of the two-pass design that makes the memory bound hold.
// Packing needs only each frame's dimensions, and a PNG's dimensions are in
// its first few dozen bytes, so the layout is decided for the cost of headers
// rather than for the cost of every frame's pixels. Pass two then decodes one
// frame at a time into the canvas.
//
// Every refusal in here happens before a single frame is inflated.
func readMembers(ctx context.Context, r io.ReaderAt, size int64, prefix string) ([]member, error) {
	if size <= 0 {
		return nil, &ArchiveError{Reason: "the archive is empty"}
	}

	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, &ArchiveError{
			Reason: "it is not a readable ZIP archive: " + err.Error(),
			Public: "That file is not a ZIP archive this service can read.",
		}
	}

	// The entry count comes from the central directory, so an archive of
	// three thousand members is refused without reading three thousand
	// headers.
	if n := len(zr.File); n > MaxFrames {
		return nil, &FrameCountError{Count: n, Limit: MaxFrames}
	}

	if len(zr.File) == 0 {
		return nil, &ArchiveError{
			Reason: "it holds no entries",
			Public: "That archive is empty. It needs at least one frame.",
		}
	}

	if err := checkDeclaredSize(zr); err != nil {
		return nil, err
	}

	out := make([]member, 0, len(zr.File))
	seen := make(map[string]string, len(zr.File))

	for _, entry := range zr.File {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		name, err := frameNameOf(entry, prefix)
		if err != nil {
			return nil, err
		}

		// Two entries whose basenames collide would silently become one
		// frame in the atlas while both sat in the sheet, putting every
		// later frame off by one. A wrong sheet delivered as a correct one
		// is worse than a refusal.
		if first, dup := seen[strings.ToLower(name)]; dup {
			return nil, &ArchiveError{
				Entry:  entry.Name,
				Reason: fmt.Sprintf("its frame name %q is already taken by %q", name, first),
				Public: fmt.Sprintf("Two frames in that archive are both named %q (%q and %q). "+
					"Frame names come from the file name without its folder, so these would "+
					"collide; rename one.", name, first, entry.Name),
			}
		}

		seen[strings.ToLower(name)] = entry.Name

		dims, err := memberSize(entry)
		if err != nil {
			return nil, err
		}

		out = append(out, member{name: name, entry: entry, size: dims})
	}

	return out, nil
}

// checkDeclaredSize refuses an archive whose directory claims more expansion
// than this package will do.
func checkDeclaredSize(zr *zip.Reader) error {
	var total int64

	for _, entry := range zr.File {
		declared := entry.UncompressedSize64

		// A single entry over the whole budget is refused on its own, so the
		// message names the entry rather than the archive.
		if declared > uint64(MaxArchiveUncompressed) {
			return &ArchiveError{
				Entry: entry.Name,
				Reason: fmt.Sprintf(
					"it declares %d uncompressed bytes, over the %d byte limit",
					declared,
					MaxArchiveUncompressed,
				),
				Public: fmt.Sprintf("One entry in that archive expands to %d MB. "+
					"The limit for the whole archive is %d MB.",
					declared>>20, MaxArchiveUncompressed>>20),
			}
		}

		total += int64(declared)

		if total > MaxArchiveUncompressed {
			return &ArchiveError{
				Reason: fmt.Sprintf("its entries declare more than %d uncompressed bytes", MaxArchiveUncompressed),
				Public: fmt.Sprintf("That archive expands to more than %d MB, which is the limit.",
					MaxArchiveUncompressed>>20),
			}
		}
	}

	return nil
}

// frameNameOf returns the frame name for an archive entry, refusing an entry
// that cannot carry a frame.
//
// The entry name is never used as a path. Only its basename, with the
// extension removed, becomes the frame name, so nothing it contains can reach
// a filesystem operation however it is spelled — there is no filesystem
// operation to reach. What is still checked here is what the name is for: a
// frame name goes into the metadata and from there into an engine's asset
// loader.
func frameNameOf(entry *zip.File, prefix string) (string, error) {
	raw := entry.Name

	if entry.FileInfo().IsDir() || strings.HasSuffix(raw, "/") {
		return "", &ArchiveError{
			Entry:  raw,
			Reason: "it is a directory, not a frame",
			Public: "That archive holds folders. Put the frames in it directly, with no folders, " +
				"or make sure every frame has a different file name.",
		}
	}

	if len(raw) > MaxFrameNameBytes {
		return "", &ArchiveError{
			Entry:  truncateForMessage(raw),
			Reason: fmt.Sprintf("its name is %d bytes, over the %d byte limit", len(raw), MaxFrameNameBytes),
			Public: fmt.Sprintf("One entry in that archive has a name %d characters long. "+
				"The limit is %d.", len(raw), MaxFrameNameBytes),
		}
	}

	if !utf8.ValidString(raw) {
		return "", &ArchiveError{
			Entry:  truncateForMessage(raw),
			Reason: "its name is not valid UTF-8",
			Public: "One entry in that archive has a name that is not valid text.",
		}
	}

	// path.Base, not filepath.Base: the separator in a ZIP entry is always
	// "/" by the format's own specification, whatever platform this runs on,
	// and filepath.Base on Windows would additionally split on "\" and turn a
	// backslash that is a legitimate part of a name into a separator.
	base := path.Base(path.Clean("/" + strings.ReplaceAll(raw, "\\", "/")))
	name := strings.TrimSuffix(base, path.Ext(base))

	// Everything that is left after dropping the directory: "..", ".", "/" and
	// names that are only dots all reduce to nothing usable.
	if name == "" || name == "." || name == ".." || strings.Trim(name, ".") == "" {
		return "", &ArchiveError{
			Entry:  raw,
			Reason: "its name leaves nothing to call the frame",
			Public: fmt.Sprintf("The archive entry %q has no usable file name.", raw),
		}
	}

	_ = prefix // frames keep their own names; the prefix only names generated ones

	return name, nil
}

// truncateForMessage shortens a name that is itself the problem, so an error
// about a 60000-character name is not 60000 characters long.
func truncateForMessage(s string) string {
	const keep = 64

	if len(s) <= keep {
		return s
	}

	// Cut on a rune boundary so the result is still valid text.
	cut := keep
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}

	return s[:cut] + "…"
}

// memberSize reads one entry's image header and returns its dimensions.
//
// Only the header is read: the entry is opened, DecodeConfig takes what it
// needs from the front of the stream, and the rest is never inflated. That is
// what keeps pass one cheap.
func memberSize(entry *zip.File) (image.Point, error) {
	rc, err := entry.Open()
	if err != nil {
		return image.Point{}, &ArchiveError{
			Entry:  entry.Name,
			Reason: "it cannot be read: " + err.Error(),
			Public: fmt.Sprintf("The archive entry %q is damaged.", entry.Name),
		}
	}

	defer func() { _ = rc.Close() }()

	// Bounded independently of the entry's declared size, because the
	// declaration is the archive's claim and this is the real read.
	cfg, _, err := image.DecodeConfig(io.LimitReader(rc, maxFrameHeaderBytes))
	if err != nil {
		return image.Point{}, &ArchiveError{
			Entry: entry.Name,
			Reason: "it is not an image this service reads: " +
				strings.TrimPrefix(err.Error(), "image: "),
			Public: fmt.Sprintf("The archive entry %q is not a PNG, GIF or JPEG image. "+
				"Every entry has to be a frame.", entry.Name),
		}
	}

	if err := checkFrameSize(entry.Name, cfg.Width, cfg.Height); err != nil {
		return image.Point{}, err
	}

	return image.Pt(cfg.Width, cfg.Height), nil
}

// maxFrameHeaderBytes is how much of a frame is read to learn its dimensions.
// A PNG's IHDR is in the first 33 bytes and a JPEG's SOF can sit behind
// several kilobytes of metadata; this is generous for both and still refuses
// to inflate a frame to read its header.
const maxFrameHeaderBytes int64 = 64 << 10

// checkFrameSize refuses a frame that is too large to be one.
func checkFrameSize(name string, w, h int) error {
	if w <= 0 || h <= 0 {
		return &ArchiveError{
			Entry:  name,
			Reason: fmt.Sprintf("it is %dx%d, which has no pixels", w, h),
			Public: fmt.Sprintf("The archive entry %q has no pixels.", name),
		}
	}

	if w > MaxDimension || h > MaxDimension {
		return &ArchiveError{
			Entry:  name,
			Reason: fmt.Sprintf("it is %dx%d, over the %d pixel limit on either side", w, h, MaxDimension),
			Public: fmt.Sprintf("The frame %q is %d×%d. Neither side may be over %d.",
				name, w, h, MaxDimension),
		}
	}

	// One frame may not on its own exceed what a whole sheet may be.
	if int64(w)*int64(h) > int64(MaxPixels) {
		return &ArchiveError{
			Entry: name,
			Reason: fmt.Sprintf(
				"it is %dx%d, which is %d pixels, over the limit of %d",
				w,
				h,
				int64(w)*int64(h),
				MaxPixels,
			),
			Public: fmt.Sprintf("The frame %q is %d×%d, which is more than the %d pixel limit.",
				name, w, h, MaxPixels),
		}
	}

	return nil
}

// decodeMember is pass two for one frame: it inflates and decodes the entry,
// checking the real image against the header it declared.
func decodeMember(m member) (image.Image, error) {
	rc, err := m.entry.Open()
	if err != nil {
		return nil, &ArchiveError{
			Entry:  m.entry.Name,
			Reason: "it cannot be read: " + err.Error(),
			Public: fmt.Sprintf("The archive entry %q is damaged.", m.entry.Name),
		}
	}

	defer func() { _ = rc.Close() }()

	img, err := decodeLimited(rc, MaxInputBytes)
	if err != nil {
		// An ArchiveError already names the entry and carries a public
		// message, so it passes through rather than being wrapped again.
		if _, ok := errors.AsType[*ArchiveError](err); ok {
			return nil, err
		}

		return nil, &ArchiveError{
			Entry:  m.entry.Name,
			Reason: "it could not be decoded: " + err.Error(),
			Public: fmt.Sprintf("The archive entry %q could not be read as an image.", m.entry.Name),
		}
	}

	// A decoder may disagree with the header it reported, and the layout was
	// already decided from that header. A frame that changed size between the
	// two passes would be drawn into a cell that is the wrong shape, so it is
	// refused rather than scaled or clipped.
	if b := img.Bounds(); b.Dx() != m.size.X || b.Dy() != m.size.Y {
		return nil, &ArchiveError{
			Entry: m.entry.Name,
			Reason: fmt.Sprintf("its header declared %dx%d but it decoded as %dx%d",
				m.size.X, m.size.Y, b.Dx(), b.Dy()),
			Public: fmt.Sprintf("The frame %q does not match the size its own header declares.",
				m.name),
		}
	}

	return img, nil
}
