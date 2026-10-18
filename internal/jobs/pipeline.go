package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/AlexGalhardo/vukem/database"
	"github.com/AlexGalhardo/vukem/orm"
	"github.com/AlexGalhardo/vukem/queue"

	"github.com/AlexGalhardo/vukem-tools/app/models"
	"github.com/AlexGalhardo/vukem-tools/internal/cpu"
)

// Work is what a tool is handed to do one conversion.
type Work struct {
	ConversionID string
	UserID       string
	// Options is the validated tool input, exactly as it was accepted.
	Options json.RawMessage
	// Dir is a private working directory, empty when the tool starts and
	// deleted when the conversion ends, including on a panic. Nothing outside
	// it may be written.
	Dir string
	// Report sends progress. It returns an error when the conversion must
	// stop — the owner cancelled it — and a tool must return that error
	// rather than swallow it. The context given to Run is also cancelled, so
	// a tool that forgets still has its subprocess killed.
	Report func(Step) error
}

// Tool converts one kind of input. One per entry on the tools page.
type Tool interface {
	// Name is the stable tool id stored on every conversion, e.g.
	// "pixels-to-svg". It is part of the public API.
	Name() string
	// Ability is the API ability a key needs to use it.
	Ability() string
	// Timeout bounds one attempt. It is per tool because the useful bound
	// differs by orders of magnitude: a sprite is milliseconds, a video is
	// minutes.
	Timeout() time.Duration
	// Run does the work. Anything the error taxonomy covers must come back
	// as a *ConversionError, so the failure is classified at the point that
	// knows what went wrong rather than guessed at later.
	Run(ctx context.Context, w Work) (Outcome, error)
}

// message is the queue payload. It carries the conversion id and nothing
// else: the conversion row is the state, so a payload that also held options
// could disagree with it, and a payload holding user data would end up in the
// jobs table and in every queue dump.
type message struct {
	ConversionID string `json:"conversion_id"`
}

// Kind implements queue.Job.
func (message) Kind() string { return "conversion" }

// Pipeline accepts conversions, queues them and runs them.
type Pipeline struct {
	store *Store
	queue *queue.Queue
	tools map[string]Tool
	root  string
	log   *slog.Logger
	now   func() time.Time
}

// PipelineOptions configures a Pipeline. Store, Queue and WorkRoot are
// required.
type PipelineOptions struct {
	Store *Store
	Queue *queue.Queue
	// Tools are the conversions on offer. A conversion naming a tool that is
	// not here fails as internal rather than being retried forever.
	Tools []Tool
	// WorkRoot is the directory private job directories are made under. It
	// must be on a filesystem with room for the largest input plus its
	// output.
	WorkRoot string
	Log      *slog.Logger
	Now      func() time.Time
}

// NewPipeline returns a pipeline.
func NewPipeline(o PipelineOptions) (*Pipeline, error) {
	switch {
	case o.Store == nil:
		return nil, errors.New("jobs: pipeline needs a store")
	case o.Queue == nil:
		return nil, errors.New("jobs: pipeline needs a queue")
	case o.WorkRoot == "":
		return nil, errors.New("jobs: pipeline needs a working directory")
	}

	tools := make(map[string]Tool, len(o.Tools))

	for _, t := range o.Tools {
		if _, dup := tools[t.Name()]; dup {
			return nil, fmt.Errorf("jobs: two tools are called %q", t.Name())
		}

		tools[t.Name()] = t
	}

	if o.Log == nil {
		o.Log = slog.Default()
	}

	if o.Now == nil {
		o.Now = time.Now
	}

	return &Pipeline{store: o.Store, queue: o.Queue, tools: tools, root: o.WorkRoot, log: o.Log, now: o.Now}, nil
}

// Tools returns the registered tool names, sorted, for the API and the tools
// page.
func (p *Pipeline) Tools() []string { return slices.Sorted(maps.Keys(p.tools)) }

// Tool returns the tool with this name.
func (p *Pipeline) Tool(name string) (Tool, bool) {
	t, ok := p.tools[name]
	return t, ok
}

// ErrUnknownTool is returned by Submit for a tool that is not registered.
var ErrUnknownTool = errors.New("jobs: unknown tool")

// Submit records a conversion and queues it. It reports whether the
// conversion is new: false means an earlier submission with the same
// idempotency key is being returned.
//
// The insert and the enqueue share one transaction, which is the whole point
// of using DispatchTx. Without it, a crash between them leaves a conversion
// that is pending forever and no message to run it, and a rollback after a
// successful enqueue leaves a message naming a conversion that does not
// exist.
func (p *Pipeline) Submit(ctx context.Context, in Submission) (models.Conversion, bool, error) {
	tool, ok := p.tools[in.Tool]
	if !ok {
		return models.Conversion{}, false, fmt.Errorf("%w: %q", ErrUnknownTool, in.Tool)
	}

	if in.IdempotencyKey != "" {
		existing, err := p.store.ByIdempotencyKey(ctx, in.UserID, in.IdempotencyKey)
		if err == nil {
			return existing, false, nil
		}

		if !errors.Is(err, orm.ErrNotFound) {
			return models.Conversion{}, false, err
		}
	}

	var created models.Conversion

	err := p.store.db.Tx(ctx, func(tx *database.Tx) error {
		c, err := p.store.Create(ctx, tx, in)
		if err != nil {
			return err
		}

		created = c

		return p.queue.DispatchTx(ctx, tx, message{ConversionID: c.ID},
			queue.Retries(maxAttempts-1),
			queue.Backoff(queue.Exponential(retryBase)),
			queue.Timeout(tool.Timeout()),
		)
	})
	// Two submissions with the same idempotency key raced and this one lost
	// the unique index. The winner is the answer, which is exactly what the
	// key promised.
	if err != nil && errors.Is(err, orm.ErrConflict) && in.IdempotencyKey != "" {
		existing, findErr := p.store.ByIdempotencyKey(ctx, in.UserID, in.IdempotencyKey)
		if findErr == nil {
			return existing, false, nil
		}
	}

	if err != nil {
		return models.Conversion{}, false, err
	}

	return created, true, nil
}

// How hard a transient failure is retried. Three attempts with exponential
// backoff from a second covers a restarting engine and a momentary disk
// shortage without making a user wait minutes to be told a file is broken.
const (
	maxAttempts = 3
	retryBase   = time.Second
)

// Registration binds the conversion job to its handler.
func (p *Pipeline) Registration() queue.Registration {
	return queue.Handle(p.run,
		queue.Retries(maxAttempts-1),
		queue.Backoff(queue.Exponential(retryBase)),
		queue.Timeout(defaultTimeout),
	)
}

// defaultTimeout bounds a conversion whose message carries no timeout, which
// only happens for a message enqueued by an older version.
const defaultTimeout = 10 * time.Minute

// Concurrency is how many conversions one worker process runs at once: one
// per CPU it may actually use. A conversion is CPU-bound almost end to end,
// so oversubscribing buys no throughput and spends the quota on context
// switches — every conversion gets slower at the same time.
func Concurrency() int { return cpu.Limit() }

// run is the queue handler.
//
// It returns an error only when the attempt should be retried. A permanent
// failure settles the conversion and returns nil, because the queue has no way
// to be told "do not retry this" (see framework feedback): returning the error
// would retry a file that can never convert, three times, with backoff.
//
// workaround(vukem): a queue handler cannot report a permanent failure.
func (p *Pipeline) run(ctx context.Context, m message) error {
	c, err := p.load(ctx, m.ConversionID)
	if err != nil {
		if errors.Is(err, orm.ErrNotFound) {
			// The row is gone: expired and cleaned up, or deleted with the
			// account. There is nothing to run and nothing to report.
			p.log.WarnContext(ctx, "jobs: conversion no longer exists", "conversion", m.ConversionID)

			return nil
		}

		return err
	}

	// Cancelled while it waited, or a duplicate delivery of something already
	// settled. Either way there is no work to do.
	if Status(c.Status).Final() {
		return nil
	}

	tool, ok := p.tools[c.Tool]
	if !ok {
		// Retrying cannot make a tool exist. Settle it so the row does not
		// sit running until the reaper finds it.
		_, err := p.store.Failure(ctx, c.ID, Failf(Internal, "no tool named %q is registered", c.Tool))

		return err
	}

	claimed, err := p.store.Claim(ctx, c.ID)
	if err != nil {
		return err
	}

	if !claimed {
		// Cancelled between the read and the claim, or another worker holds
		// it. Both are fine, and neither is this attempt's work.
		return nil
	}

	return p.execute(ctx, c, tool)
}

// load reads a conversion without scoping it to a user: the worker runs on
// nobody's behalf. Every path that serves a request goes through Store.Find,
// which does scope.
func (p *Pipeline) load(ctx context.Context, id string) (models.Conversion, error) {
	return orm.QueryOf[models.Conversion](p.store.db).Where("id", id).First(ctx)
}

// execute runs the tool and settles the conversion.
func (p *Pipeline) execute(ctx context.Context, c models.Conversion, tool Tool) error {
	dir, err := p.workdir(c.ID)
	if err != nil {
		// No working directory is a disk problem, which is worth retrying on
		// another worker.
		return Fail(DiskFull, err, "create the working directory")
	}

	defer p.removeWorkdir(ctx, dir)

	// A cancellation found while reporting progress cancels the tool's
	// context too, so a tool that ignores Report's error still has its
	// subprocess killed with its process group.
	runCtx, stopRun := context.WithCancel(ctx)
	defer stopRun()

	out, runErr := tool.Run(runCtx, Work{
		ConversionID: c.ID,
		UserID:       c.UserID,
		Options:      c.Options.V,
		Dir:          dir,
		Report:       p.reporter(ctx, c.ID, stopRun),
	})

	return p.settle(ctx, c.ID, out, runErr)
}

// reporter returns the Report function a tool is given. It carries the
// cancellation back twice: as an error to the caller, and by cancelling the
// tool's context.
func (p *Pipeline) reporter(ctx context.Context, id string, stopRun context.CancelFunc) func(Step) error {
	return func(step Step) error {
		alive, err := p.store.Advance(ctx, id, step)
		if err != nil {
			return err
		}

		if !alive {
			stopRun()

			return Fail(CancelledByUser, context.Canceled, "the owner cancelled the conversion")
		}

		return nil
	}
}

// settle records how the conversion ended and decides whether the queue
// should try again.
func (p *Pipeline) settle(ctx context.Context, id string, out Outcome, runErr error) error {
	// The writes below must happen even when ctx is already done, which is
	// the normal case for a timeout or a shutdown: with a dead context every
	// one of them would fail and the conversion would be left running.
	settleCtx := ctx
	if ctx.Err() != nil {
		var cancel context.CancelFunc

		settleCtx, cancel = context.WithTimeout(context.WithoutCancel(ctx), settleTimeout)
		defer cancel()
	}

	if runErr == nil {
		settled, err := p.store.Finish(settleCtx, id, out)
		if err != nil {
			return err
		}

		if !settled {
			// It was cancelled while the tool was finishing. The result is
			// not wanted, and the working directory is about to be deleted
			// with it.
			p.log.InfoContext(ctx, "jobs: result discarded, the conversion had ended", "conversion", id)
		}

		return nil
	}

	kind := Classify(runErr)
	log := p.log.With("conversion", id, "error_type", kind)

	// A shutdown is not a failure. Put the conversion back to pending and
	// hand the cancellation to the queue, which releases the message without
	// spending an attempt.
	if interrupted(ctx, runErr) {
		if err := p.store.Release(settleCtx, id); err != nil {
			log.ErrorContext(ctx, "jobs: could not release an interrupted conversion", "err", err)
		}

		log.InfoContext(ctx, "jobs: conversion interrupted, released")

		return runErr
	}

	if Describe(kind).Retry {
		// Leave the row running and let the queue retry. The next attempt
		// claims it again; the reaper closes it if no attempt ever does.
		log.WarnContext(ctx, "jobs: attempt failed, will be retried", "err", DetailOf(runErr))

		return runErr
	}

	if _, err := p.store.Failure(settleCtx, id, runErr); err != nil {
		return err
	}

	log.InfoContext(ctx, "jobs: conversion failed", "err", DetailOf(runErr))

	return nil
}

// settleTimeout bounds the writes that record how a conversion ended, when
// they run on a context that is already done.
const settleTimeout = 10 * time.Second

// interrupted reports whether the attempt ended because the process is
// shutting down rather than because the work failed.
//
// The owner cancelling also cancels the context, so the error's own
// classification decides: a cancellation the owner asked for is a final
// state, a cancellation nobody asked for is a shutdown.
func interrupted(ctx context.Context, runErr error) bool {
	if ctx.Err() == nil {
		return false
	}

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return false // the attempt timed out; that is a failure, not a shutdown
	}

	var ce *ConversionError
	if errors.As(runErr, &ce) && ce.Type == CancelledByUser {
		return false
	}

	return true
}

// workdir makes a private directory for one conversion.
//
// 0o700 because the engines run as this user and nothing else has any
// business reading an input file: another process on the host must not be
// able to read a user's upload off the disk while it is being converted.
func (p *Pipeline) workdir(conversionID string) (string, error) {
	if err := os.MkdirAll(p.root, 0o700); err != nil {
		return "", fmt.Errorf("jobs: prepare %s: %w", p.root, err)
	}

	// The id is a ULID, so it holds no separator and cannot escape the root.
	dir := filepath.Join(p.root, conversionID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("jobs: prepare %s: %w", dir, err)
	}

	return dir, nil
}

// removeWorkdir deletes a job's directory. It runs from a defer, so it also
// runs on a panic, which is what keeps a crashed conversion from leaving its
// input on disk. A failure is logged rather than returned: the conversion's
// own outcome is the answer, and the cleanup job sweeps what is left.
func (p *Pipeline) removeWorkdir(ctx context.Context, dir string) {
	if err := os.RemoveAll(dir); err != nil {
		p.log.ErrorContext(ctx, "jobs: working directory not removed", "dir", dir, "err", err)
	}
}
