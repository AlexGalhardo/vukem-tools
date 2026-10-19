package tools

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AlexGalhardo/vukem-tools/app/abilities"
	"github.com/AlexGalhardo/vukem-tools/app/models"
	"github.com/AlexGalhardo/vukem-tools/internal/detect"
	"github.com/AlexGalhardo/vukem-tools/internal/files"
	"github.com/AlexGalhardo/vukem-tools/internal/jobs"
	"github.com/AlexGalhardo/vukem-tools/internal/spritesheet"
)

// The sprite sheet domain: two tools, named for what a person wants.
//
// sprite-split takes a sheet and gives back frames with an atlas describing
// them; sprite-build takes frames and gives back a sheet with the same. One
// ability, sprite:sheet, because they are one family.
//
// Pure Go throughout: internal/spritesheet is image/png, archive/zip and
// arithmetic, so this domain adds no engine and takes no Engines.

// Tool names, which are the catalogue ids and the URL segments too.
const (
	NameSpriteSplit = "sprite-split"
	NameSpriteBuild = "sprite-build"
)

// spriteTools is the sprite sheets and frame atlases domain (S7).
func spriteTools(f *files.Store) []jobs.Tool {
	return []jobs.Tool{NewSpriteSplit(f), NewSpriteBuild(f)}
}

// spriteTimeout bounds one attempt in either direction.
//
// Two minutes. The recorded benchmark for the worst case the limits allow — a
// 4096 by 4096 sheet of a thousand frames — is under half a second, so
// anything near this bound is a pathological input or a stuck worker, and
// either should be stopped rather than waited on.
const spriteTimeout = 2 * time.Minute

// SpriteSplit cuts a sheet into frames.
type SpriteSplit struct {
	files *files.Store
}

// NewSpriteSplit returns the tool.
func NewSpriteSplit(f *files.Store) *SpriteSplit { return &SpriteSplit{files: f} }

// Name implements jobs.Tool.
func (*SpriteSplit) Name() string { return NameSpriteSplit }

// Ability implements jobs.Tool.
func (*SpriteSplit) Ability() string { return abilities.SpriteSheet }

// Timeout implements jobs.Tool.
func (*SpriteSplit) Timeout() time.Duration { return spriteTimeout }

// Accepted implements jobs.Accepter.
//
// The types image/png decodes from the standard library, plus GIF and JPEG
// because it already reads them and refusing would be gratuitous. WebP and
// AVIF are absent on purpose: they would need golang.org/x/image or libvips,
// and this phase adds neither.
func (*SpriteSplit) Accepted() []string {
	return []string{"image/png", "image/gif", "image/jpeg"}
}

// ValidateOptions implements jobs.OptionsValidator.
func (*SpriteSplit) ValidateOptions(raw json.RawMessage) (json.RawMessage, error) {
	opts, err := ParseSpriteSplitOptions(raw)
	if err != nil {
		return nil, err
	}

	out, err := json.Marshal(opts)
	if err != nil {
		return nil, fmt.Errorf("tools: encode the normalised options: %w", err)
	}

	return out, nil
}

// Run implements jobs.Tool.
func (t *SpriteSplit) Run(ctx context.Context, w jobs.Work) (jobs.Outcome, error) {
	opts, err := ParseSpriteSplitOptions(w.Options)
	if err != nil {
		return jobs.Outcome{}, jobs.Fail(jobs.UnsupportedInput, err,
			"the stored options are not valid for this tool")
	}

	slice, err := opts.library()
	if err != nil {
		return jobs.Outcome{}, jobs.Fail(jobs.UnsupportedInput, err, "the stored options are not valid")
	}

	input, err := inputFile(ctx, t.files, w, "the sprite splitter", t.Accepted())
	if err != nil {
		return jobs.Outcome{}, err
	}

	if err := w.Report(jobs.Step{Percent: 10, Phase: "reading"}); err != nil {
		return jobs.Outcome{}, err
	}

	local, err := localInput(ctx, t.files, w, input)
	if err != nil {
		return jobs.Outcome{}, err
	}

	if err := w.Report(jobs.Step{Percent: 30, Phase: "slicing"}); err != nil {
		return jobs.Outcome{}, err
	}

	frames := filepath.Join(w.Dir, "frames-raw.zip")

	result, err := splitInto(ctx, frames, local, slice)
	if err != nil {
		return jobs.Outcome{}, classifySprite(err)
	}

	if err := w.Report(jobs.Step{Percent: 60, Phase: "writing metadata"}); err != nil {
		return jobs.Outcome{}, err
	}

	atlas := spritesheet.Atlas{
		Image:  sheetNameFor(input.Name),
		Size:   result.Sheet,
		Frames: result.Atlas,
	}

	meta, err := writeMetadata(w.Dir, atlas, opts.Metadata,
		spriteMetaOptions(opts.Aseprite, opts.FrameMS))
	if err != nil {
		return jobs.Outcome{}, classifySprite(err)
	}

	// The archive carries the metadata too, so one download is
	// self-contained. The frames are copied across without being
	// recompressed, which is what makes a second archive cheap.
	bundle := filepath.Join(w.Dir, "frames.zip")
	if err := bundleWithMetadata(bundle, frames, meta); err != nil {
		return jobs.Outcome{}, err
	}

	report := splitReport(result, opts)

	if err := w.Report(jobs.Step{Percent: 85, Phase: "storing", Message: report}); err != nil {
		return jobs.Outcome{}, err
	}

	outputs := append([]spriteOutput{{
		path: bundle,
		name: spriteStem(input.Name) + "-frames.zip",
		mime: "application/zip",
	}}, meta...)

	out, err := storeSpriteOutputs(ctx, t.files, w, outputs)
	if err != nil {
		return jobs.Outcome{}, err
	}

	// On the outcome as well as the trail: a split of a small sheet finishes
	// before a browser's event stream connects, and the trail alone would be
	// a race the fast path always loses.
	out.Note = report

	return out, nil
}

// splitInto runs the split, writing the frames archive to path.
func splitInto(
	ctx context.Context,
	path, input string,
	opts spritesheet.SliceOptions,
) (spritesheet.SplitResult, error) {
	src, err := os.Open(input)
	if err != nil {
		return spritesheet.SplitResult{}, jobs.Fail(jobs.Internal, err, "reopen the local input")
	}

	defer func() { _ = src.Close() }()

	dst, err := os.Create(path)
	if err != nil {
		return spritesheet.SplitResult{}, jobs.Fail(jobs.DiskFull, err, "create the frames archive")
	}

	result, splitErr := spritesheet.Split(ctx, dst, src, opts)

	if err := dst.Close(); err != nil && splitErr == nil {
		return spritesheet.SplitResult{}, jobs.Fail(jobs.DiskFull, err, "close the frames archive")
	}

	return result, splitErr
}

// splitReport is the sentence shown on the progress trail.
func splitReport(result spritesheet.SplitResult, opts SpriteSplitOptions) string {
	report := fmt.Sprintf("Cut %d frames from a %dx%d sheet in %s mode",
		result.Frames, result.Sheet.X, result.Sheet.Y, opts.Mode)

	if result.Dropped > 0 {
		// Reported rather than swallowed: a frame count that does not match
		// what somebody expected needs an explanation, and "three components
		// were too small" is one.
		report += fmt.Sprintf(", dropping %d too small to be frames", result.Dropped)
	}

	if len(opts.Metadata) > 0 {
		report += ", with " + strings.Join(opts.Metadata, ", ") + " metadata"
	}

	return report + "."
}

// SpriteBuild packs frames into a sheet.
type SpriteBuild struct {
	files *files.Store
}

// NewSpriteBuild returns the tool.
func NewSpriteBuild(f *files.Store) *SpriteBuild { return &SpriteBuild{files: f} }

// Name implements jobs.Tool.
func (*SpriteBuild) Name() string { return NameSpriteBuild }

// Ability implements jobs.Tool.
func (*SpriteBuild) Ability() string { return abilities.SpriteSheet }

// Timeout implements jobs.Tool.
func (*SpriteBuild) Timeout() time.Duration { return spriteTimeout }

// Accepted implements jobs.Accepter: a ZIP of frames, and nothing else.
func (*SpriteBuild) Accepted() []string { return []string{"application/zip"} }

// ValidateOptions implements jobs.OptionsValidator.
func (*SpriteBuild) ValidateOptions(raw json.RawMessage) (json.RawMessage, error) {
	opts, err := ParseSpriteBuildOptions(raw)
	if err != nil {
		return nil, err
	}

	out, err := json.Marshal(opts)
	if err != nil {
		return nil, fmt.Errorf("tools: encode the normalised options: %w", err)
	}

	return out, nil
}

// Run implements jobs.Tool.
func (t *SpriteBuild) Run(ctx context.Context, w jobs.Work) (jobs.Outcome, error) {
	opts, err := ParseSpriteBuildOptions(w.Options)
	if err != nil {
		return jobs.Outcome{}, jobs.Fail(jobs.UnsupportedInput, err,
			"the stored options are not valid for this tool")
	}

	input, err := inputFile(ctx, t.files, w, "the sprite packer", t.Accepted())
	if err != nil {
		return jobs.Outcome{}, err
	}

	if err := w.Report(jobs.Step{Percent: 10, Phase: "reading"}); err != nil {
		return jobs.Outcome{}, err
	}

	local, err := localInput(ctx, t.files, w, input)
	if err != nil {
		return jobs.Outcome{}, err
	}

	if err := w.Report(jobs.Step{Percent: 30, Phase: "packing"}); err != nil {
		return jobs.Outcome{}, err
	}

	sheet := filepath.Join(w.Dir, "sheet.png")

	result, err := buildInto(ctx, sheet, local, opts.library())
	if err != nil {
		return jobs.Outcome{}, classifySprite(err)
	}

	if err := w.Report(jobs.Step{Percent: 65, Phase: "writing metadata"}); err != nil {
		return jobs.Outcome{}, err
	}

	sheetName := spriteStem(input.Name) + "-sheet.png"

	meta, err := writeMetadata(w.Dir, spritesheet.Atlas{
		Image:  sheetName,
		Size:   result.Sheet,
		Frames: result.Atlas,
	}, opts.Metadata, spriteMetaOptions(opts.Aseprite, opts.FrameMS))
	if err != nil {
		return jobs.Outcome{}, classifySprite(err)
	}

	report := buildReport(result, opts)

	if err := w.Report(jobs.Step{Percent: 85, Phase: "storing", Message: report}); err != nil {
		return jobs.Outcome{}, err
	}

	outputs := append([]spriteOutput{{
		path: sheet,
		name: sheetName,
		mime: "image/png",
	}}, meta...)

	out, err := storeSpriteOutputs(ctx, t.files, w, outputs)
	if err != nil {
		return jobs.Outcome{}, err
	}

	out.Note = report

	return out, nil
}

// buildInto runs the build, writing the sheet to path.
func buildInto(
	ctx context.Context,
	path, input string,
	opts spritesheet.BuildOptions,
) (spritesheet.BuildResult, error) {
	src, err := os.Open(input)
	if err != nil {
		return spritesheet.BuildResult{}, jobs.Fail(jobs.Internal, err, "reopen the local input")
	}

	defer func() { _ = src.Close() }()

	info, err := src.Stat()
	if err != nil {
		return spritesheet.BuildResult{}, jobs.Fail(jobs.Internal, err, "measure the local input")
	}

	dst, err := os.Create(path)
	if err != nil {
		return spritesheet.BuildResult{}, jobs.Fail(jobs.DiskFull, err, "create the sheet")
	}

	result, buildErr := spritesheet.Build(ctx, dst, src, info.Size(), opts)

	if err := dst.Close(); err != nil && buildErr == nil {
		return spritesheet.BuildResult{}, jobs.Fail(jobs.DiskFull, err, "close the sheet")
	}

	return result, buildErr
}

// buildReport is the sentence shown on the progress trail.
//
// The packing efficiency is in it because it is the number that decides
// whether the compact layout was worth choosing, and it is otherwise
// invisible.
func buildReport(result spritesheet.BuildResult, opts SpriteBuildOptions) string {
	report := fmt.Sprintf("Packed %d frames into a %dx%d sheet in %s layout, %.0f%% of it used",
		result.Frames, result.Sheet.X, result.Sheet.Y, opts.Layout, result.Efficiency*100)

	if len(opts.Metadata) > 0 {
		report += ", with " + strings.Join(opts.Metadata, ", ") + " metadata"
	}

	report += "."

	if opts.Layout == string(spritesheet.LayoutGrid) {
		// The one sentence that replaces a Godot and a Unity writer, per the
		// phase's own decision: a uniform grid needs no metadata file in
		// either, so the honest deliverable is the sheet and the instruction.
		report += " A uniform grid imports into Godot 4 and Unity with no metadata file: " +
			"give the frame size or the frame counts in the importer."
	}

	return report
}

// spriteOutput is one file a sprite conversion produced.
type spriteOutput struct {
	path string
	name string
	mime string
}

// writeMetadata writes one file per requested format into dir.
func writeMetadata(
	dir string,
	atlas spritesheet.Atlas,
	formats []string,
	opts spritesheet.MetaOptions,
) ([]spriteOutput, error) {
	out := make([]spriteOutput, 0, len(formats))

	for _, name := range formats {
		format := spritesheet.MetaFormat(name)

		info, ok := spritesheet.MetaFormatOf(format)
		if !ok {
			return nil, fmt.Errorf("tools: %q is not a metadata format", name)
		}

		// The format's name is in the filename, because two formats share an
		// extension: asking for both JSON forms would otherwise be one file
		// overwriting the other.
		path := filepath.Join(dir, "atlas-"+name+info.Extension)

		f, err := os.Create(path)
		if err != nil {
			return nil, jobs.Fail(jobs.DiskFull, err, "create the metadata file")
		}

		writeErr := spritesheet.WriteAtlas(f, atlas, format, opts)

		if err := f.Close(); err != nil && writeErr == nil {
			return nil, jobs.Fail(jobs.DiskFull, err, "close the metadata file")
		}

		if writeErr != nil {
			return nil, writeErr
		}

		out = append(out, spriteOutput{
			path: path,
			name: spriteStem(atlas.Image) + "-" + name + info.Extension,
			mime: info.MIME,
		})
	}

	return out, nil
}

// bundleWithMetadata writes a ZIP holding every entry of frames plus the
// metadata files.
//
// The frames are copied with (*zip.Writer).Copy, which moves the already
// compressed bytes across without inflating and re-deflating them: a thousand
// frames is a stream copy rather than a thousand re-encodes.
func bundleWithMetadata(path, frames string, meta []spriteOutput) error {
	src, err := zip.OpenReader(frames)
	if err != nil {
		return jobs.Fail(jobs.Internal, err, "reopen the frames archive")
	}

	defer func() { _ = src.Close() }()

	dst, err := os.Create(path)
	if err != nil {
		return jobs.Fail(jobs.DiskFull, err, "create the bundled archive")
	}

	defer func() { _ = dst.Close() }()

	zw := zip.NewWriter(dst)

	for _, entry := range src.File {
		if err := zw.Copy(entry); err != nil {
			return jobs.Fail(jobs.Internal, err, "copy a frame into the bundled archive")
		}
	}

	for _, m := range meta {
		if err := addFileToZip(zw, m); err != nil {
			return err
		}
	}

	if err := zw.Close(); err != nil {
		return jobs.Fail(jobs.DiskFull, err, "finish the bundled archive")
	}

	return nil
}

// addFileToZip writes one metadata file into the archive.
func addFileToZip(zw *zip.Writer, m spriteOutput) error {
	// A fixed modification time rather than the clock, for the same reason
	// the frames carry one: the same sheet and options must produce a
	// byte-identical archive.
	entry, err := zw.CreateHeader(&zip.FileHeader{Name: filepath.Base(m.name), Method: zip.Deflate})
	if err != nil {
		return jobs.Fail(jobs.Internal, err, "start a metadata entry")
	}

	body, err := os.Open(m.path)
	if err != nil {
		return jobs.Fail(jobs.Internal, err, "open a metadata file")
	}

	defer func() { _ = body.Close() }()

	if _, err := io.Copy(entry, body); err != nil {
		return jobs.Fail(jobs.DiskFull, err, "write a metadata entry")
	}

	return nil
}

// storeSpriteOutputs puts every result in the bucket.
//
// One row per file, each with its own expiry, which is the mechanism
// pdf-split already uses. A split is therefore two rows — the archive and the
// metadata — and the archive holds the metadata as well, so one download is
// enough for somebody who wants everything.
func storeSpriteOutputs(
	ctx context.Context,
	store *files.Store,
	w jobs.Work,
	outputs []spriteOutput,
) (jobs.Outcome, error) {
	var total int64

	for _, out := range outputs {
		kind, ok := detect.KindOf(out.mime)
		if !ok {
			return jobs.Outcome{}, jobs.Failf(jobs.Internal, "%s is not a recognised type", out.mime)
		}

		body, err := os.Open(out.path)
		if err != nil {
			return jobs.Outcome{}, jobs.Fail(jobs.Internal, err, "open a result to store it")
		}

		stored, putErr := store.Put(ctx, files.Incoming{
			UserID:       w.UserID,
			ConversionID: w.ConversionID,
			Role:         models.FileOutput,
			Name:         out.name,
			Kind:         kind,
		}, body)

		if closeErr := body.Close(); closeErr != nil && putErr == nil {
			return jobs.Outcome{}, jobs.Fail(jobs.Internal, closeErr, "close a stored result")
		}

		if putErr != nil {
			return jobs.Outcome{}, jobs.Fail(jobs.Internal, putErr, "store a result")
		}

		total += stored.SizeBytes
	}

	return jobs.Outcome{BytesOut: total}, nil
}

// sheetNameFor is the image name a split's metadata points at.
//
// The input's own name, because that is the file the person has: a metadata
// file naming "sheet.png" when the sheet on disk is "hero.png" is one more
// thing to fix by hand after downloading.
func sheetNameFor(name string) string { return filepath.Base(name) }

// spriteStem is a file name without its extension, reduced to something safe
// to build another name from.
//
// Its own helper rather than the download domain's stemOf, whose fallback for
// an unusable name is "video": that is right there and would produce
// "video-frames.zip" here.
func spriteStem(name string) string {
	stem := safeStem(strings.TrimSuffix(filepath.Base(name), filepath.Ext(name)))
	if stem == "" {
		return "sprites"
	}

	return stem
}

// classifySprite maps the library's errors onto the conversion taxonomy.
//
// Every one of them is about the input or the options — a grid that does not
// tile, a sheet with no detectable background, a sheet or a sheet-to-be that
// is too large, an archive member that is not an image — so none can succeed
// on a retry, and classifying one as transient would spend three workers to
// reach the same answer.
func classifySprite(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	}

	// Ordered from the most specific to the least. Each of these carries a
	// message written for the person who uploaded the file, and the taxonomy
	// code is what decides whether the queue retries.
	for _, c := range []struct {
		as   func(error) (publicMessage, bool)
		kind jobs.ErrorType
	}{
		{asPublic[*spritesheet.DimensionError], jobs.InputTooLarge},
		{asPublic[*spritesheet.InputSizeError], jobs.InputTooLarge},
		{asPublic[*spritesheet.SheetSizeError], jobs.InputTooLarge},
		{asPublic[*spritesheet.FrameCountError], jobs.InputTooLarge},
		{asPublic[*spritesheet.GridError], jobs.UnsupportedInput},
		{asPublic[*spritesheet.BackgroundError], jobs.UnsupportedInput},
		{asPublic[*spritesheet.ArchiveError], jobs.UnsupportedInput},
		{asPublic[*spritesheet.OptionError], jobs.UnsupportedInput},
		{asPublic[*spritesheet.TrimError], jobs.UnsupportedInput},
	} {
		if public, ok := c.as(err); ok {
			return jobs.Refuse(c.kind, err, public.Message(), public.Error())
		}
	}

	if strings.Contains(err.Error(), "image:") {
		// The standard library's decoders report a bad or unsupported image
		// this way; there is no sentinel to match on.
		return jobs.Refuse(jobs.UnsupportedInput, err,
			"That image could not be decoded. It may be damaged or only partly uploaded.",
			"the sheet could not be decoded")
	}

	return jobs.Fail(jobs.Internal, err, "the sprite conversion failed")
}

// publicMessage is an error that carries a sentence for the person who
// uploaded the file, which every error internal/spritesheet returns does.
type publicMessage interface {
	error
	Message() string
}

// asPublic adapts errors.AsType to the table above, which needs one function
// type rather than one per error type.
func asPublic[T publicMessage](err error) (publicMessage, bool) {
	return errors.AsType[T](err)
}
