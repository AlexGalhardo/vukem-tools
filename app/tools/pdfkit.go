package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AlexGalhardo/vukem/z"

	"github.com/AlexGalhardo/vukem-tools/app/models"
	"github.com/AlexGalhardo/vukem-tools/internal/detect"
	"github.com/AlexGalhardo/vukem-tools/internal/engine"
	"github.com/AlexGalhardo/vukem-tools/internal/files"
	"github.com/AlexGalhardo/vukem-tools/internal/jobs"
	"github.com/AlexGalhardo/vukem-tools/internal/pdf"
)

// pdfDeps is what every PDF tool shares: the file store and the engines.
//
// The engines are resolved on first use rather than at construction, and that
// is a workaround rather than a design. tools.All is handed a *files.Store and
// nothing else, so there is no way for the resolved engine registry — which
// the application builds at boot, logs the versions of, and fails the deploy
// over — to reach a tool. Resolving them here means the versions are not in
// the boot log and a missing engine is found at the first conversion instead
// of at startup.
//
// The fix is one line in app/tools/tools.go and one in app/services: pass the
// registry and the runner into All. That file belongs to the whole
// application rather than to this domain, so it is asked for rather than
// taken.
//
// workaround(vukem-tools): tools.All cannot reach the resolved engine registry.
type pdfDeps struct {
	files *files.Store

	once    sync.Once
	engines *pdf.Engines
	err     error
}

// newPDFDeps returns the shared dependencies for the PDF tools. Nothing is
// resolved until a conversion runs.
func newPDFDeps(f *files.Store) *pdfDeps { return &pdfDeps{files: f} }

// pdfBinaries are the engines the PDF tools use, with the same settings and
// version arguments app/services/engines.go declares. They are repeated here
// only because of the workaround above; when the registry is passed in, this
// goes.
var pdfBinaries = []engine.Binary{
	{Name: pdf.EngineQPDF, EnvVar: "QPDF_PATH", VersionArgs: []string{"--version"}},
	{Name: pdf.EngineGhostscript, EnvVar: "GHOSTSCRIPT_PATH", VersionArgs: []string{"--version"}},
	{Name: pdf.EnginePDFToPPM, EnvVar: "PDFTOPPM_PATH", VersionArgs: []string{"-v"}},
	{Name: pdf.EnginePDFToText, EnvVar: "PDFTOTEXT_PATH", VersionArgs: []string{"-v"}},
	{Name: pdf.EngineVips, EnvVar: "VIPS_PATH", VersionArgs: []string{"--version"}},
	{Name: pdf.EngineLibreOffice, EnvVar: "SOFFICE_PATH", VersionArgs: []string{"--version"}},
	{Name: pdf.EngineOCRmyPDF, EnvVar: "OCRMYPDF_PATH", VersionArgs: []string{"--version"}},
}

// The engine limits the PDF tools run under.
//
// They mirror app/services/engines.go for the same reason pdfBinaries does.
// The address space is what stops a malformed document from being a container
// outage: Ghostscript asks for its memory before it does the work, so a
// document claiming an impossible page size fails its allocation and exits.
const (
	pdfAddressSpace = 3 << 30
	pdfFileSize     = 2 << 30
	pdfProcesses    = 64
	pdfOpenFiles    = 512
	pdfEngineClock  = 20 * time.Minute
	pdfEngineCPU    = 15 * time.Minute
)

// resolve builds the engine set once.
func (d *pdfDeps) resolve(ctx context.Context) (*pdf.Engines, error) {
	d.once.Do(func() {
		registry, err := engine.Resolve(ctx, osLookup{}, pdfBinaries)
		if err != nil {
			d.err = fmt.Errorf("tools: resolve the PDF engines: %w", err)

			return
		}

		runner, err := engine.NewRunner(engine.RunnerOptions{
			Limits: engine.Limits{
				Timeout:      pdfEngineClock,
				AddressSpace: pdfAddressSpace,
				CPU:          pdfEngineCPU,
				FileSize:     pdfFileSize,
				Processes:    pdfProcesses,
				OpenFiles:    pdfOpenFiles,
			},
			// Production is read from the environment rather than from the
			// application's config, which this layer cannot see. It defaults
			// to on: a runner that silently skipped its limits because a
			// variable was unset would be the wrong failure.
			Production: os.Getenv("APP_ENV") != "local" && os.Getenv("APP_ENV") != "testing",
			Log:        slog.Default(),
		})
		if err != nil {
			d.err = fmt.Errorf("tools: the PDF engine runner: %w", err)

			return
		}

		d.engines, d.err = pdf.NewEngines(registry, runner)
	})

	return d.engines, d.err
}

// osLookup reads an engine path from the process environment.
type osLookup struct{}

// Get implements engine.Lookup.
func (osLookup) Get(key string) string { return os.Getenv(key) }

// pdfOptions is what a PDF tool's options must be able to do: normalise
// themselves, filling in defaults and refusing a value the tool cannot act
// on.
//
// It is a type parameter rather than an interface value so that Normalise
// returns the concrete option type: a tool's Run then reads its own options
// directly, with no assertion and no `any` in the path.
type pdfOptions[T any] interface {
	Normalise() (T, error)
}

// pdfOutput is one file a PDF tool produced.
type pdfOutput struct {
	// Path is the file in the job's working directory.
	Path string
	// Name is what the user downloads it as.
	Name string
	// MIME is the type to record, which must be one internal/detect knows.
	MIME string
}

// pdfResult is what a tool's own work returns.
type pdfResult struct {
	// Outputs are the files to store, in order.
	Outputs []pdfOutput
	// Message is one sentence for the conversion's progress trail, shown to
	// the owner. It holds no path and no engine argument.
	Message string
}

// pdfWork is everything a tool's own function is handed.
type pdfWork[O any] struct {
	// Engines is the resolved engine set.
	Engines *pdf.Engines
	// Dir is the job's private working directory.
	Dir string
	// Inputs are the local copies of the conversion's input files, in the
	// order they were attached.
	Inputs []pdfInput
	// Options are the validated, normalised options.
	Options O
	// Report sends progress, and returns an error when the conversion must
	// stop.
	Report func(percent int, phase string) error
}

// pdfInput is one input file, copied to local disk.
type pdfInput struct {
	// Path is the local copy.
	Path string
	// Name is the name the user uploaded it under.
	Name string
	// MIME is the type detected from its bytes at upload.
	MIME string
}

// Stem is the input's name without its extension, for naming a result after
// it.
func (i pdfInput) Stem() string {
	return strings.TrimSuffix(i.Name, filepath.Ext(i.Name))
}

// pdfTool is one PDF conversion.
//
// There is one implementation of jobs.Tool for all nineteen PDF tools rather
// than nineteen structs, because the parts that differ are the name, the
// ability, the bound, the types it reads and the work itself — and the parts
// that are the same are the parts worth getting right once: reading the
// inputs out of the bucket without buffering them, reporting progress,
// classifying a failure onto the error taxonomy, and storing the results.
// Nineteen copies of that would be nineteen chances to get the classification
// subtly different.
type pdfTool[O pdfOptions[O]] struct {
	deps *pdfDeps
	// name is the stable tool id, part of the public API.
	name string
	// ability is the API ability a key needs.
	ability string
	// timeout bounds one attempt.
	timeout time.Duration
	// accepted are the MIME types this tool reads.
	accepted []string
	// engines are the engines it needs. A tool whose engine is missing fails
	// as internal rather than blaming the input, because the tool should not
	// have been on offer.
	engines []string
	// minInputs and maxInputs bound how many files the conversion takes. Most
	// tools take one; merge takes several.
	minInputs, maxInputs int
	// work does the conversion.
	work func(ctx context.Context, w pdfWork[O]) (pdfResult, error)
}

// Name implements jobs.Tool.
func (t *pdfTool[O]) Name() string { return t.name }

// Ability implements jobs.Tool.
func (t *pdfTool[O]) Ability() string { return t.ability }

// Timeout implements jobs.Tool.
func (t *pdfTool[O]) Timeout() time.Duration { return t.timeout }

// Accepted implements jobs.Accepter.
func (t *pdfTool[O]) Accepted() []string { return slices.Clone(t.accepted) }

// ValidateOptions implements jobs.OptionsValidator. What it returns is what is
// stored, so the worker reads exactly the options that were validated.
func (t *pdfTool[O]) ValidateOptions(raw json.RawMessage) (json.RawMessage, error) {
	opts, err := parsePDFOptions[O](raw)
	if err != nil {
		return nil, err
	}

	out, err := json.Marshal(opts)
	if err != nil {
		return nil, fmt.Errorf("tools: encode the normalised options: %w", err)
	}

	return out, nil
}

// parsePDFOptions decodes and normalises a tool's options.
func parsePDFOptions[O pdfOptions[O]](raw json.RawMessage) (O, error) {
	var in O

	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &in); err != nil {
			var zero O

			return zero, z.FieldError("options", "The options are not a JSON object.")
		}
	}

	return in.Normalise()
}

// Run implements jobs.Tool.
func (t *pdfTool[O]) Run(ctx context.Context, w jobs.Work) (jobs.Outcome, error) {
	opts, err := parsePDFOptions[O](w.Options)
	if err != nil {
		// Stored options that no longer validate mean the conversion was
		// accepted by an older version of this tool. It will never succeed,
		// so it is the input's problem rather than a transient one.
		return jobs.Outcome{}, jobs.Fail(jobs.UnsupportedInput, err, "the stored options are not valid for this tool")
	}

	engines, err := t.deps.resolve(ctx)
	if err != nil {
		return jobs.Outcome{}, jobs.Fail(jobs.Internal, err, "resolve the PDF engines")
	}

	for _, name := range t.engines {
		if !engines.Has(name) {
			// Not the input's fault and not worth retrying on another
			// worker: every worker runs the same image.
			return jobs.Outcome{}, jobs.Refuse(jobs.EngineFailure, nil,
				"This tool is not available on this server.",
				"the "+name+" engine is not installed")
		}
	}

	if err := w.Report(jobs.Step{Percent: 5, Phase: "reading"}); err != nil {
		return jobs.Outcome{}, err
	}

	inputs, err := t.fetch(ctx, w)
	if err != nil {
		return jobs.Outcome{}, err
	}

	if err := w.Report(jobs.Step{Percent: 15, Phase: "working"}); err != nil {
		return jobs.Outcome{}, err
	}

	result, err := t.work(ctx, pdfWork[O]{
		Engines: engines,
		Dir:     w.Dir,
		Inputs:  inputs,
		Options: opts,
		Report: func(percent int, phase string) error {
			// The tool's own progress is mapped into the band between
			// reading and storing, so a tool reporting 0 to 100 does not
			// jump backwards from the 15 above.
			return w.Report(jobs.Step{Percent: 15 + percent*70/100, Phase: phase})
		},
	})
	if err != nil {
		return jobs.Outcome{}, classifyPDF(err)
	}

	if len(result.Outputs) == 0 {
		return jobs.Outcome{}, jobs.Failf(jobs.Internal, "%s produced no output", t.name)
	}

	if err := w.Report(jobs.Step{Percent: 90, Phase: "storing", Message: result.Message}); err != nil {
		return jobs.Outcome{}, err
	}

	return t.store(ctx, w, result)
}

// fetch copies the conversion's inputs into the job's own directory.
//
// Copied rather than streamed into the engine, because every engine here
// takes a file path: qpdf, Ghostscript and poppler all seek, and several of
// them read the file more than once. Streaming from the bucket would mean
// paying for the object twice and failing halfway through if the connection
// dropped. The copy itself is an io.Copy, so a half-gigabyte document never
// passes through memory.
func (t *pdfTool[O]) fetch(ctx context.Context, w jobs.Work) ([]pdfInput, error) {
	attached, err := t.deps.files.ForConversion(ctx, w.ConversionID, models.FileInput)
	if err != nil {
		return nil, jobs.Fail(jobs.Internal, err, "look up the conversion's inputs")
	}

	minimum := max(t.minInputs, 1)
	if len(attached) < minimum {
		return nil, jobs.Failf(jobs.Internal, "conversion %s has %d inputs, needs %d",
			w.ConversionID, len(attached), minimum)
	}

	if t.maxInputs > 0 && len(attached) > t.maxInputs {
		return nil, jobs.Refuse(jobs.InputTooLarge, nil,
			"This tool takes at most "+strconv.Itoa(t.maxInputs)+" files.",
			strconv.Itoa(len(attached))+" inputs were attached")
	}

	out := make([]pdfInput, 0, len(attached))

	for i := range attached {
		in := attached[i]

		if !slices.Contains(t.accepted, in.MIME) {
			kind, _ := detect.KindOf(in.MIME)
			if kind.Label == "" {
				kind.Label = in.MIME
			}

			return nil, jobs.Refuse(jobs.UnsupportedInput, nil,
				"That upload is "+kind.Label+", which this tool cannot read.",
				in.MIME+" is not accepted by "+t.name)
		}

		local, err := t.copyIn(ctx, w, in, i)
		if err != nil {
			return nil, err
		}

		out = append(out, pdfInput{Path: local, Name: in.Name, MIME: in.MIME})
	}

	return out, nil
}

// copyIn streams one input out of the bucket onto local disk.
func (t *pdfTool[O]) copyIn(ctx context.Context, w jobs.Work, in models.File, index int) (string, error) {
	body, err := t.deps.files.Open(ctx, in)
	if err != nil {
		return "", jobs.Fail(jobs.Internal, err, "open an input")
	}

	defer func() { _ = body.Close() }()

	// The name is generated, never taken from the upload: the upload's own
	// name is sanitised when it is stored, and generating this one means no
	// name from a client reaches a path at all.
	path := filepath.Join(w.Dir, "input-"+strconv.Itoa(index)+extensionFor(in.MIME))

	dst, err := os.Create(path)
	if err != nil {
		return "", jobs.Fail(jobs.DiskFull, err, "create a local input file")
	}

	if _, err := io.Copy(dst, body); err != nil {
		_ = dst.Close()

		return "", jobs.Fail(jobs.DiskFull, err, "write a local input file")
	}

	if err := dst.Close(); err != nil {
		return "", jobs.Fail(jobs.DiskFull, err, "close a local input file")
	}

	return path, nil
}

// extensionFor is the canonical extension for a type, or ".bin".
func extensionFor(mime string) string {
	if kind, ok := detect.KindOf(mime); ok && kind.Extension != "" {
		return kind.Extension
	}

	return ".bin"
}

// store puts a tool's results in the bucket and records them against the
// conversion.
//
// Each output gets its own row with its own expiry, which is what makes a
// multi-file result work inside the service's five-minute result lifetime:
// the clock on every part starts when that part is stored, and the result
// endpoint signs a link for each one when it is asked for. There is no second
// mechanism and no archive step — a split into twenty files is twenty files
// that expire together.
func (t *pdfTool[O]) store(ctx context.Context, w jobs.Work, result pdfResult) (jobs.Outcome, error) {
	var total int64

	for _, out := range result.Outputs {
		kind, ok := detect.KindOf(out.MIME)
		if !ok {
			return jobs.Outcome{}, jobs.Failf(jobs.Internal, "%s is not a recognised type", out.MIME)
		}

		body, err := os.Open(out.Path)
		if err != nil {
			return jobs.Outcome{}, jobs.Fail(jobs.Internal, err, "open a result to store it")
		}

		stored, err := t.deps.files.Put(ctx, files.Incoming{
			UserID:       w.UserID,
			ConversionID: w.ConversionID,
			Role:         models.FileOutput,
			Name:         out.Name,
			Kind:         kind,
		}, body)

		if closeErr := body.Close(); closeErr != nil && err == nil {
			return jobs.Outcome{}, jobs.Fail(jobs.Internal, closeErr, "close a stored result")
		}

		if err != nil {
			return jobs.Outcome{}, jobs.Fail(jobs.Internal, err, "store a result")
		}

		total += stored.SizeBytes
	}

	return jobs.Outcome{BytesOut: total}, nil
}

// classifyPDF maps the library's errors onto the conversion error taxonomy.
//
// This is the one place the mapping lives, which is the main reason the tools
// share an implementation: a password-protected document has to be a refusal
// rather than a crash in all nineteen of them, and nineteen copies of this
// switch would be nineteen chances for one of them to classify it as an
// internal error and retry it three times.
func classifyPDF(err error) error {
	var (
		notPDF    *pdf.NotPDFError
		damaged   *pdf.DamagedError
		tooMany   *pdf.TooManyPagesError
		rangeErr  *pdf.PageRangeError
		option    *pdf.OptionError
		timeout   *pdf.TimeoutError
		missing   *pdf.EngineMissingError
		engineErr *pdf.EngineError
	)

	switch {
	case errors.Is(err, context.Canceled):
		// Classified rather than returned bare, and the shape matters as much
		// as the code. The pipeline tells "the owner cancelled" from "the
		// worker is shutting down" by looking for exactly this — a
		// *ConversionError carrying CancelledByUser — so a bare
		// context.Canceled would read as a shutdown and be released back to
		// the queue instead of settled as cancelled.
		return jobs.Fail(jobs.CancelledByUser, err, "the owner cancelled the conversion")
	case errors.Is(err, context.DeadlineExceeded):
		// Returned unwrapped: jobs.Classify gives Timeout, which is
		// retryable, and an attempt that ran out of time genuinely may
		// succeed on a worker that is not as busy. Wrapping it as Internal —
		// which the default branch would — would report "something went wrong
		// on our side" for a document that is merely slow.
		return err
	case errors.Is(err, pdf.ErrEncrypted):
		// A refusal, not a crash: ROADMAP S6.1 asks for unsupported_input
		// rather than an empty file, and the sentence says what to do about
		// it.
		return jobs.Refuse(jobs.UnsupportedInput, err,
			"That PDF is password-protected. Remove the password with the unlock tool first, "+
				"then try again.",
			"the document is encrypted")
	case errors.Is(err, pdf.ErrWrongPassword):
		return jobs.Refuse(jobs.UnsupportedInput, err,
			"That password does not open this document.",
			"the supplied password was refused")
	case errors.Is(err, pdf.ErrNotEncrypted):
		return jobs.Refuse(jobs.UnsupportedInput, err,
			"That PDF has no password, so there is nothing to remove.",
			"the document is not encrypted")
	case errors.As(err, &notPDF):
		return jobs.Refuse(jobs.UnsupportedInput, err, notPDF.Message(), notPDF.Error())
	case errors.As(err, &damaged):
		return jobs.Refuse(jobs.UnsupportedInput, err, damaged.Message(), damaged.Error())
	case errors.As(err, &tooMany):
		return jobs.Refuse(jobs.InputTooLarge, err, tooMany.Message(), tooMany.Error())
	case errors.As(err, &rangeErr):
		return jobs.Refuse(jobs.UnsupportedInput, err, rangeErr.Message(), rangeErr.Error())
	case errors.As(err, &option):
		return jobs.Refuse(jobs.UnsupportedInput, err, option.Message(), option.Error())
	case errors.As(err, &timeout):
		return jobs.Fail(jobs.Timeout, err, "the engine exceeded its time limit")
	case errors.As(err, &missing):
		return jobs.Refuse(jobs.EngineFailure, err,
			"This tool is not available on this server.", missing.Error())
	case errors.As(err, &engineErr):
		// The engine ran and rejected the document. Not retryable: the same
		// file would reach the same answer.
		return jobs.Refuse(jobs.EngineFailure, err,
			"The converter could not process that PDF.", engineErr.Error())
	default:
		// Unclassified, so it may be a bug: Internal is retried and its
		// message says nothing about the input.
		return jobs.Fail(jobs.Internal, err, "the PDF conversion failed")
	}
}
