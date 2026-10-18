// Package pdf is every PDF operation the service offers, as a library.
//
// Nothing here knows about jobs, HTTP or object storage: an operation takes
// local file paths and returns local file paths, so each one can be
// benchmarked, fuzzed and run against a real engine without a queue or a
// database. The tool layer in app/tools is what moves the bytes.
//
// Three engines do the work, and which one does what is a deliberate choice
// rather than a preference:
//
//   - qpdf (Apache-2.0) for everything structural — merge, split, select,
//     rotate, crop, encrypt, decrypt, repair, form appearances. It edits the
//     object graph rather than re-rendering, so a page it did not touch comes
//     out byte-for-byte as it went in.
//   - Ghostscript (AGPL-3.0-or-later, declared on /licenses) only where a
//     page genuinely has to be re-rendered: lossy compression and PDF/A.
//     Re-rendering is lossy by definition, which is why it is never the
//     engine for a structural operation.
//   - poppler's pdftoppm and pdftotext (GPL-2.0-or-later) for rasterising and
//     text, because they are the reference implementations and neither
//     rewrites the document.
//
// Every engine runs through internal/engine, which applies the timeout, the
// minimal environment, the prlimit resource caps and the process-group kill.
// There is no exec.Command here and no cgo anywhere.
//
// Two rules shape the shape of the code. Nothing buffers a whole document: a
// PDF can be hundreds of megabytes and the container is small, so a file
// arrives as a path and leaves as a path, and the only thing read into memory
// is the structural JSON qpdf prints, which is bounded by a page cap rather
// than by the file's size. And an input is validated before any engine sees
// it: a file without a PDF header, or one that is encrypted, is refused here
// rather than handed to Ghostscript to crash on.
package pdf

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/AlexGalhardo/vukem-tools/internal/engine"
)

// The engines this package runs, by the name a Registry resolves them under.
const (
	// EngineQPDF is qpdf: structural editing, encryption and repair.
	EngineQPDF = "qpdf"
	// EngineGhostscript is gs: lossy compression and PDF/A.
	EngineGhostscript = "gs"
	// EnginePDFToPPM is poppler's pdftoppm: page rasterising.
	EnginePDFToPPM = "pdftoppm"
	// EnginePDFToText is poppler's pdftotext: text extraction.
	EnginePDFToText = "pdftotext"
	// EngineVips is libvips: converting an input image to the one raster
	// format a PDF can carry without re-encoding it a second time.
	EngineVips = "vips"
)

// Registry resolves an engine's path. engine.Registry satisfies it, and a
// test can pass a stub to check an argument list without running anything.
type Registry interface {
	// Path returns the absolute path of an engine, and whether it resolved.
	Path(name string) (string, bool)
	// Has reports whether the engine is available.
	Has(name string) bool
}

// Subprocess runs one engine under limits. engine.Runner satisfies it.
type Subprocess interface {
	Run(ctx context.Context, cmd engine.Command) (engine.Result, error)
}

// Engines is the set of engines an operation may use, with the runner that
// bounds them.
//
// It is a value rather than a package global because the registry is resolved
// once at boot from configuration, and a library that reached for a global
// would be untestable and would hide which engine an operation needs.
type Engines struct {
	registry Registry
	runner   Subprocess
}

// NewEngines returns the engine set. Both arguments are required: an
// operation with no runner would be an operation with no limits.
func NewEngines(r Registry, run Subprocess) (*Engines, error) {
	switch {
	case r == nil:
		return nil, errors.New("pdf: needs an engine registry")
	case run == nil:
		return nil, errors.New("pdf: needs a subprocess runner")
	}

	return &Engines{registry: r, runner: run}, nil
}

// Has reports whether an engine is available, which is what a tool asks
// before offering itself. A tool whose engine is absent is hidden rather than
// broken.
func (e *Engines) Has(name string) bool { return e.registry.Has(name) }

// command is one engine invocation.
type command struct {
	// engine is one of the Engine* constants.
	engine string
	args   []string
	// dir is the job's own working directory. Every path in args must be
	// inside it.
	dir string
	// stdin feeds the program. It is how a password reaches qpdf without
	// appearing in argv, where anything running as the same user could read
	// it out of /proc.
	stdin io.Reader
	// stdout receives the program's output, for an engine that writes a
	// document to a pipe. Nil discards it.
	stdout io.Writer
	// progress is called for each line the engine writes to stdout. It is
	// mutually exclusive with stdout, which the runner enforces by giving
	// the progress reader the pipe.
	progress func(line string)
	// timeout bounds this invocation. Zero leaves the runner's own bound,
	// which is the longest any engine may run.
	timeout time.Duration
}

// run executes one engine and classifies what came back.
//
// Classification happens here rather than at each call site so that a timeout
// is Timeout everywhere, a failed exit is EngineFailure everywhere, and the
// engine's stderr reaches the owner-only detail everywhere — a tool deciding
// that for itself is how two tools end up wording the same failure
// differently.
func (e *Engines) run(ctx context.Context, cmd command) (engine.Result, error) {
	path, ok := e.registry.Path(cmd.engine)
	if !ok {
		return engine.Result{}, &EngineMissingError{Engine: cmd.engine}
	}

	if cmd.timeout > 0 {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeout(ctx, cmd.timeout)
		defer cancel()
	}

	result, err := e.runner.Run(ctx, engine.Command{
		Path:     path,
		Args:     cmd.args,
		Dir:      cmd.dir,
		Stdin:    cmd.stdin,
		Stdout:   cmd.stdout,
		Progress: cmd.progress,
	})
	if err == nil {
		return result, nil
	}

	return result, classifyRun(cmd.engine, result, err)
}

// classifyRun turns a runner error into one of this package's typed errors.
func classifyRun(name string, result engine.Result, err error) error {
	if errors.Is(err, engine.ErrTimeout) {
		return &TimeoutError{Engine: name}
	}

	if exit, ok := errors.AsType[*engine.ExitError](err); ok {
		return &EngineError{Engine: name, Code: exit.Code, Stderr: result.Stderr}
	}

	return fmt.Errorf("pdf: run %s: %w", name, err)
}

// exitCode runs an engine whose answer is its exit status rather than its
// output, which is how qpdf's predicates (--is-encrypted,
// --requires-password) report.
//
// A non-zero exit is the answer, not a failure, so it is returned instead of
// classified. Only a timeout or a runner fault is an error.
func (e *Engines) exitCode(ctx context.Context, cmd command) (int, error) {
	result, err := e.run(ctx, cmd)
	if err == nil {
		return result.ExitCode, nil
	}

	if engineErr, ok := errors.AsType[*EngineError](err); ok {
		return engineErr.Code, nil
	}

	return 0, err
}

// inside reports whether path is inside dir, which every engine argument must
// be.
//
// It is checked rather than assumed because a filename reaches this package
// from an upload: the name is sanitised before it is stored, and this is the
// second gate, so a traversal would have to get through both. filepath.Rel
// rather than a prefix test, because a prefix test says "/a/b" contains
// "/a/bc".
func inside(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}

	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
