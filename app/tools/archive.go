package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/AlexGalhardo/vukem/z"

	"github.com/AlexGalhardo/vukem-tools/app/abilities"
	"github.com/AlexGalhardo/vukem-tools/app/models"
	"github.com/AlexGalhardo/vukem-tools/internal/archive"
	"github.com/AlexGalhardo/vukem-tools/internal/detect"
	"github.com/AlexGalhardo/vukem-tools/internal/files"
	"github.com/AlexGalhardo/vukem-tools/internal/jobs"
)

// The archive domain: two tools, named for what a person wants.
//
// Two rather than one with a direction option, which is the same call
// internal/spritesheet's phase made and for the same reason: the two
// directions take different inputs. Creating takes many files of any type,
// extracting takes exactly one archive — so Accepted, which is what refuses a
// file at submission before a worker is spent on it, has to be a different
// list for each. One tool would have to accept the union, which is every type
// the service knows, and the gate would be gone.
//
// They share one ability, because they are one family.

// Tool names, which are the catalogue ids and the URL segments too.
const (
	NameArchiveCreate  = "archive-create"
	NameArchiveExtract = "archive-extract"
)

// archiveTools is the archives domain.
func archiveTools(f *files.Store) []jobs.Tool {
	return []jobs.Tool{NewArchiveCreate(f), NewArchiveExtract(f)}
}

// archiveTimeout bounds one attempt in either direction.
//
// Two minutes. Both directions are stdlib compression over at most the
// service's upload limit, and the recorded benchmark for the cap is seconds,
// so anything near this bound is a pathological input or a stuck worker and
// should be stopped rather than waited on.
const archiveTimeout = 2 * time.Minute

// ArchiveCreate packs several uploads into one archive.
type ArchiveCreate struct {
	files *files.Store
}

// NewArchiveCreate returns the tool.
func NewArchiveCreate(f *files.Store) *ArchiveCreate { return &ArchiveCreate{files: f} }

// Name implements jobs.Tool.
func (*ArchiveCreate) Name() string { return NameArchiveCreate }

// Ability implements jobs.Tool.
func (*ArchiveCreate) Ability() string { return abilities.Archive }

// Timeout implements jobs.Tool.
func (*ArchiveCreate) Timeout() time.Duration { return archiveTimeout }

// ArchiveCreate deliberately does not implement jobs.Accepter. Any file the
// upload endpoint accepted can go into an archive — that is what an archive
// is — so there is no narrower list to state, and stating the whole of
// detect.Types() here would be a gate that refuses nothing while looking like
// one that does.

// ArchiveCreateOptions is the validated input of an archive-create
// conversion.
type ArchiveCreateOptions struct {
	// Format is the archive to write.
	Format string `json:"format" validate:"trim,max=8" doc:"zip, tar.gz, tar or gz. Default zip."`
	// Level is the compression level.
	Level int `json:"level" validate:"min=0,max=9" doc:"Compression level, 1 to 9. 0 uses the format's default."`
	// Name is the archive's own filename, without an extension.
	Name string `json:"name" validate:"trim,max=64" doc:"The archive's name, without an extension. Default archive."`
}

// ArchiveFormats returns the formats a client may name, for the documentation
// and the interface.
func ArchiveFormats() []string {
	out := make([]string, 0, len(archive.Formats()))
	for _, f := range archive.Formats() {
		out = append(out, string(f))
	}

	return out
}

// Normalise fills in every omitted field and refuses a value that is not one
// of the documented ones.
func (in ArchiveCreateOptions) Normalise() (ArchiveCreateOptions, error) {
	if in.Format = strings.TrimSpace(in.Format); in.Format == "" {
		in.Format = string(archive.FormatZip)
	}

	if !slices.Contains(ArchiveFormats(), in.Format) {
		return ArchiveCreateOptions{}, z.FieldError("options.format",
			"Unknown archive format. Use one of: "+strings.Join(ArchiveFormats(), ", ")+".")
	}

	if in.Level < 0 || in.Level > archive.MaxLevel {
		return ArchiveCreateOptions{}, z.FieldError("options.level",
			fmt.Sprintf("The compression level must be between 1 and %d, or 0 for the default.", archive.MaxLevel))
	}

	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		in.Name = "archive"
	}

	// The name reaches a Content-Disposition header and a bucket key, so it is
	// reduced to a basename here rather than trusted. A name that is nothing
	// but separators falls back to the default rather than refusing a
	// conversion over a cosmetic field.
	if i := strings.LastIndexAny(in.Name, `/\`); i >= 0 {
		in.Name = in.Name[i+1:]
	}

	if in.Name = strings.TrimSpace(in.Name); in.Name == "" || in.Name == "." || in.Name == ".." {
		in.Name = "archive"
	}

	return in, nil
}

// CheckArchiveCreateFiles refuses an option set that cannot describe an
// archive holding count files.
//
// It is separate from Normalise because Normalise sees only the options, and
// the one rule that needs both is gz: it compresses a single stream, so it
// holds exactly one file. Without this the submission is accepted and the
// worker refuses it a moment later, which is a conversion spent to tell
// somebody something the request already said.
func CheckArchiveCreateFiles(opts ArchiveCreateOptions, count int) error {
	err := archive.CreateOptions{Format: archive.Format(opts.Format), Level: opts.Level}.Check(count)
	if err == nil {
		return nil
	}

	if option, ok := errors.AsType[*archive.OptionError](err); ok {
		// The library names its own field; the API's spelling for the same
		// field is what the client has to act on.
		return z.FieldError("options."+option.Field, option.Message())
	}

	return err
}

// ParseArchiveCreateOptions validates and decodes the options stored on a
// conversion.
func ParseArchiveCreateOptions(raw json.RawMessage) (ArchiveCreateOptions, error) {
	var in ArchiveCreateOptions

	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &in); err != nil {
			return ArchiveCreateOptions{}, z.FieldError("options", "The options are not a JSON object.")
		}
	}

	return in.Normalise()
}

// ValidateOptions implements jobs.OptionsValidator.
func (*ArchiveCreate) ValidateOptions(raw json.RawMessage) (json.RawMessage, error) {
	opts, err := ParseArchiveCreateOptions(raw)
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
func (t *ArchiveCreate) Run(ctx context.Context, w jobs.Work) (jobs.Outcome, error) {
	opts, err := ParseArchiveCreateOptions(w.Options)
	if err != nil {
		return jobs.Outcome{}, jobs.Fail(jobs.UnsupportedInput, err, "the stored options are not valid for this tool")
	}

	inputs, err := t.files.ForConversion(ctx, w.ConversionID, models.FileInput)
	if err != nil {
		return jobs.Outcome{}, jobs.Fail(jobs.Internal, err, "look up the conversion's inputs")
	}

	if len(inputs) == 0 {
		return jobs.Outcome{}, jobs.Failf(jobs.Internal, "conversion %s has no input files", w.ConversionID)
	}

	if err := w.Report(jobs.Step{Percent: 10, Phase: "reading"}); err != nil {
		return jobs.Outcome{}, err
	}

	members, err := t.localise(ctx, w, inputs)
	if err != nil {
		return jobs.Outcome{}, err
	}

	if err := w.Report(jobs.Step{Percent: 40, Phase: "packing"}); err != nil {
		return jobs.Outcome{}, err
	}

	format := archive.Format(opts.Format)
	out := filepath.Join(w.Dir, "archive"+archiveExtension(format))

	dst, err := os.Create(out)
	if err != nil {
		return jobs.Outcome{}, jobs.Fail(jobs.DiskFull, err, "create the archive")
	}

	result, packErr := archive.Create(ctx, dst, members, archive.CreateOptions{
		Format: format,
		Level:  opts.Level,
	})

	if err := dst.Close(); err != nil && packErr == nil {
		return jobs.Outcome{}, jobs.Fail(jobs.DiskFull, err, "close the archive")
	}

	if packErr != nil {
		return jobs.Outcome{}, classifyArchive(packErr)
	}

	if err := w.Report(jobs.Step{Percent: 85, Phase: "storing"}); err != nil {
		return jobs.Outcome{}, err
	}

	return t.store(ctx, w, out, opts, format, result)
}

// localise copies every input out of the bucket into the job's directory.
//
// Copied rather than streamed straight into the archive writer because tar
// has to declare each member's size in its header before its bytes, which
// means knowing the size before writing — and a bucket object's recorded size
// is not a thing to stake a malformed archive on.
func (t *ArchiveCreate) localise(
	ctx context.Context,
	w jobs.Work,
	inputs []models.File,
) ([]archive.Member, error) {
	members := make([]archive.Member, 0, len(inputs))

	for i := range inputs {
		in := &inputs[i]

		body, err := t.files.Open(ctx, *in)
		if err != nil {
			return nil, jobs.Fail(jobs.Internal, err, "open an input")
		}

		// A positional local name, never the upload's own: the upload's name
		// is what goes inside the archive, and letting it name a file on disk
		// too would be one more place a name has to be trusted.
		path := filepath.Join(w.Dir, fmt.Sprintf("member-%03d", i))

		dst, createErr := os.Create(path)
		if createErr != nil {
			_ = body.Close()

			return nil, jobs.Fail(jobs.DiskFull, createErr, "create a local input file")
		}

		_, copyErr := io.Copy(dst, body)

		_ = body.Close()

		if closeErr := dst.Close(); closeErr != nil && copyErr == nil {
			copyErr = closeErr
		}

		if copyErr != nil {
			return nil, jobs.Fail(jobs.DiskFull, copyErr, "write a local input file")
		}

		members = append(members, archive.Member{Name: in.Name, Path: path})
	}

	return members, nil
}

// store puts the archive in the bucket and records it against the conversion.
func (t *ArchiveCreate) store(
	ctx context.Context,
	w jobs.Work,
	path string,
	opts ArchiveCreateOptions,
	format archive.Format,
	result archive.CreateResult,
) (jobs.Outcome, error) {
	mime := archiveMIME(format)

	kind, ok := detect.KindOf(mime)
	if !ok {
		return jobs.Outcome{}, jobs.Failf(jobs.Internal, "%s is not a recognised type", mime)
	}

	body, err := os.Open(path)
	if err != nil {
		return jobs.Outcome{}, jobs.Fail(jobs.Internal, err, "open the archive to store it")
	}

	defer func() { _ = body.Close() }()

	stored, err := t.files.Put(ctx, files.Incoming{
		UserID:       w.UserID,
		ConversionID: w.ConversionID,
		Role:         models.FileOutput,
		Name:         opts.Name + archiveExtension(format),
		Kind:         kind,
	}, body)
	if err != nil {
		return jobs.Outcome{}, jobs.Fail(jobs.Internal, err, "store the archive")
	}

	_ = result // the figures are reported through the progress trail, not the outcome

	return jobs.Outcome{BytesOut: stored.SizeBytes}, nil
}

// ArchiveExtract unpacks one archive into its files.
type ArchiveExtract struct {
	files *files.Store
}

// NewArchiveExtract returns the tool.
func NewArchiveExtract(f *files.Store) *ArchiveExtract { return &ArchiveExtract{files: f} }

// Name implements jobs.Tool.
func (*ArchiveExtract) Name() string { return NameArchiveExtract }

// Ability implements jobs.Tool.
func (*ArchiveExtract) Ability() string { return abilities.Archive }

// Timeout implements jobs.Tool.
func (*ArchiveExtract) Timeout() time.Duration { return archiveTimeout }

// Accepted implements jobs.Accepter.
//
// TAR.GZ is not listed separately because it has no signature of its own: a
// gzipped tar and a bare gzip are the same bytes at the front, and which one
// an upload is can only be answered from inside the stream. Both arrive here
// as application/gzip and the library decides.
func (*ArchiveExtract) Accepted() []string {
	return []string{"application/zip", "application/gzip", "application/x-tar"}
}

// ArchiveExtractOptions is the input of an extraction. There is nothing to
// choose: an archive says what it is, and the only instruction left would be
// which members to take, which is a filter better applied to the result than
// to the extraction.
type ArchiveExtractOptions struct{}

// Normalise implements the option contract.
func (o ArchiveExtractOptions) Normalise() (ArchiveExtractOptions, error) { return o, nil }

// Run implements jobs.Tool.
func (t *ArchiveExtract) Run(ctx context.Context, w jobs.Work) (jobs.Outcome, error) {
	inputs, err := t.files.ForConversion(ctx, w.ConversionID, models.FileInput)
	if err != nil {
		return jobs.Outcome{}, jobs.Fail(jobs.Internal, err, "look up the conversion's input")
	}

	if len(inputs) == 0 {
		return jobs.Outcome{}, jobs.Failf(jobs.Internal, "conversion %s has no input file", w.ConversionID)
	}

	in := inputs[0]

	if !slices.Contains(t.Accepted(), in.MIME) {
		kind, _ := detect.KindOf(in.MIME)
		if kind.Label == "" {
			kind.Label = in.MIME
		}

		return jobs.Outcome{}, jobs.Failf(jobs.UnsupportedInput,
			"%s is not an archive archive-extract reads", kind.Label)
	}

	if err := w.Report(jobs.Step{Percent: 10, Phase: "reading"}); err != nil {
		return jobs.Outcome{}, err
	}

	src, err := t.fetch(ctx, w, in)
	if err != nil {
		return jobs.Outcome{}, err
	}

	if err := w.Report(jobs.Step{Percent: 35, Phase: "extracting"}); err != nil {
		return jobs.Outcome{}, err
	}

	// A directory of its own under the job directory, so an extracted member
	// can never collide with the local copy of the archive itself.
	dest := filepath.Join(w.Dir, "extracted")
	if err := os.MkdirAll(dest, 0o700); err != nil {
		return jobs.Outcome{}, jobs.Fail(jobs.DiskFull, err, "create the extraction directory")
	}

	result, err := archive.Extract(ctx, dest, src)
	if err != nil {
		return jobs.Outcome{}, classifyArchive(err)
	}

	if err := w.Report(jobs.Step{Percent: 80, Phase: "storing"}); err != nil {
		return jobs.Outcome{}, err
	}

	return t.store(ctx, w, result)
}

// fetch copies the archive out of the bucket into the job's directory.
//
// Local rather than streamed: a ZIP is read through its trailing directory,
// which needs a reader that can seek, and seeking a bucket object means
// paying for the object more than once.
func (t *ArchiveExtract) fetch(ctx context.Context, w jobs.Work, in models.File) (string, error) {
	body, err := t.files.Open(ctx, in)
	if err != nil {
		return "", jobs.Fail(jobs.Internal, err, "open the input")
	}

	defer func() { _ = body.Close() }()

	path := filepath.Join(w.Dir, "input"+filepath.Ext(in.Name))

	dst, err := os.Create(path)
	if err != nil {
		return "", jobs.Fail(jobs.DiskFull, err, "create the local input file")
	}

	_, copyErr := io.Copy(dst, body)

	if closeErr := dst.Close(); closeErr != nil && copyErr == nil {
		copyErr = closeErr
	}

	if copyErr != nil {
		return "", jobs.Fail(jobs.DiskFull, copyErr, "write the local input file")
	}

	return path, nil
}

// store puts every extracted member in the bucket.
//
// One result row per member, each with its own expiry, which is the mechanism
// pdf-split already uses: the result endpoint signs a link for each when it is
// asked for. There is no second delivery mechanism and no re-archiving step —
// an archive of twenty files extracts to twenty files that expire together.
func (t *ArchiveExtract) store(ctx context.Context, w jobs.Work, result archive.ExtractResult) (jobs.Outcome, error) {
	var total int64

	for _, entry := range result.Entries {
		kind, err := t.kindOf(entry)
		if err != nil {
			return jobs.Outcome{}, err
		}

		body, err := os.Open(entry.Path)
		if err != nil {
			return jobs.Outcome{}, jobs.Fail(jobs.Internal, err, "open an extracted file to store it")
		}

		stored, putErr := t.files.Put(ctx, files.Incoming{
			UserID:       w.UserID,
			ConversionID: w.ConversionID,
			Role:         models.FileOutput,
			Name:         entry.Name,
			Kind:         kind,
		}, body)

		if closeErr := body.Close(); closeErr != nil && putErr == nil {
			return jobs.Outcome{}, jobs.Fail(jobs.Internal, closeErr, "close a stored file")
		}

		if putErr != nil {
			return jobs.Outcome{}, jobs.Fail(jobs.Internal, putErr, "store an extracted file")
		}

		total += stored.SizeBytes
	}

	return jobs.Outcome{BytesOut: total}, nil
}

// kindOf identifies an extracted member from its own bytes.
//
// From the bytes rather than from the name the archive gave it, because the
// name came out of an attacker-controlled directory: an entry called
// "report.pdf" holding an executable must be stored and served as what it is.
//
// OfResult rather than Of, because a member is produced rather than received:
// Of is the upload gate's identifier and refuses a .txt, which is the single
// most common thing an archive holds.
func (t *ArchiveExtract) kindOf(entry archive.Entry) (detect.Kind, error) {
	f, err := os.Open(entry.Path)
	if err != nil {
		return detect.Kind{}, jobs.Fail(jobs.Internal, err, "open an extracted file to identify it")
	}

	defer func() { _ = f.Close() }()

	kind, err := detect.OfResult(f)
	if err != nil {
		return detect.Kind{}, jobs.Fail(jobs.Internal, err, "identify an extracted file")
	}

	return kind, nil
}

// archiveExtension is the filename extension for a format.
func archiveExtension(f archive.Format) string {
	switch f {
	case archive.FormatZip:
		return ".zip"
	case archive.FormatTar:
		return ".tar"
	case archive.FormatTarGz:
		return ".tar.gz"
	case archive.FormatGz:
		return ".gz"
	default:
		return ".bin"
	}
}

// archiveMIME is the media type a format is stored and served as.
//
// A gzipped tar is application/gzip, which is what its bytes are: the tar is
// inside the stream, and there is no signature that distinguishes the two.
func archiveMIME(f archive.Format) string {
	switch f {
	case archive.FormatZip:
		return "application/zip"
	case archive.FormatTar:
		return "application/x-tar"
	case archive.FormatTarGz, archive.FormatGz:
		return "application/gzip"
	default:
		return "application/octet-stream"
	}
}

// classifyArchive maps the library's errors onto the conversion error
// taxonomy.
//
// Almost all of them are the input's problem: a file that is not an archive, a
// member that is a symlink, a cap that was reached. None of those can succeed
// on a second attempt, so classifying one as transient would spend three
// workers to reach the same answer.
func classifyArchive(err error) error {
	if format, ok := errors.AsType[*archive.FormatError](err); ok {
		return jobs.Refuse(jobs.UnsupportedInput, err, format.Message(), "the file is not an archive this tool reads")
	}

	if limit, ok := errors.AsType[*archive.LimitError](err); ok {
		return jobs.Refuse(jobs.InputTooLarge, err, limit.Message(), "an archive limit was reached")
	}

	if member, ok := errors.AsType[*archive.MemberError](err); ok {
		return jobs.Refuse(jobs.UnsupportedInput, err, member.Message(), "a member cannot be extracted")
	}

	if option, ok := errors.AsType[*archive.OptionError](err); ok {
		return jobs.Refuse(
			jobs.UnsupportedInput,
			err,
			option.Message(),
			"the options do not describe a possible archive",
		)
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}

	return jobs.Fail(jobs.EngineFailure, err, "the archive operation failed")
}
