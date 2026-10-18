package jobs_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AlexGalhardo/vukem/database"
	"github.com/AlexGalhardo/vukem/orm"
	"github.com/AlexGalhardo/vukem/queue"
	"github.com/AlexGalhardo/vukem/schema"

	"github.com/AlexGalhardo/vukem-tools/app/models"
	"github.com/AlexGalhardo/vukem-tools/database/migrations"
	"github.com/AlexGalhardo/vukem-tools/internal/ids"
	"github.com/AlexGalhardo/vukem-tools/internal/jobs"
)

const (
	owner      = "7"
	otherOwner = "8"
	resultTTL  = time.Hour
)

// clock is a hand-wound clock. Every retry and expiry decision in the
// pipeline reads it, so the tests move time instead of sleeping.
type clock struct {
	mu sync.Mutex
	at time.Time
}

func newClock() *clock {
	return &clock{at: time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)}
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.at
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.at = c.at.Add(d)
}

// openDB returns a migrated database on a file in the test's own directory.
// SQLite is enough here: every statement goes through the ORM, so nothing in
// these tests is dialect-specific. The one piece that is — the LISTEN/NOTIFY
// broker — has its own tests against PostgreSQL.
func openDB(t *testing.T) *database.DB {
	t.Helper()

	// The database gets a directory of its own inside the test's, which this
	// cleanup removes with a few retries: on Windows the SQLite file can
	// still be held for a moment after Close, and t.TempDir's own cleanup
	// fails the test when its directory is not empty yet. Cleanups run in
	// reverse order, so this one empties the subdirectory before t.TempDir
	// removes the parent.
	dir := filepath.Join(t.TempDir(), "db")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatalf("temp dir: %v", err)
	}

	db, err := database.Open(t.Context(), "sqlite:"+filepath.Join(dir, "jobs.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close: %v", err)
		}

		removeWithRetry(t, dir)
	})

	m, err := schema.NewMigrator(db, migrations.All())
	if err != nil {
		t.Fatalf("migrator: %v", err)
	}

	if _, err := m.Up(t.Context()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	return db
}

// removeWithRetry deletes dir, giving the operating system a few moments to
// release a file it has already been told to close.
func removeWithRetry(t *testing.T, dir string) {
	t.Helper()

	var err error
	for range 20 {
		if err = os.RemoveAll(dir); err == nil {
			return
		}

		time.Sleep(25 * time.Millisecond)
	}

	t.Errorf("could not remove %s: %v", dir, err)
}

// recorder is a Publisher that keeps what it was given, so the tests can
// assert what a watching browser would have seen.
type recorder struct {
	mu     sync.Mutex
	events []jobs.Event
	fail   error
}

func (r *recorder) Publish(_ context.Context, _ string, e jobs.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.events = append(r.events, e)

	return r.fail
}

func (r *recorder) all() []jobs.Event {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]jobs.Event(nil), r.events...)
}

// tool is a configurable Tool. Each test says how it behaves.
type tool struct {
	name    string
	timeout time.Duration
	run     func(ctx context.Context, w jobs.Work) (jobs.Outcome, error)
	calls   atomic.Int32
}

func (t *tool) Name() string           { return t.name }
func (t *tool) Ability() string        { return "test:run" }
func (t *tool) Timeout() time.Duration { return t.timeout }

func (t *tool) Run(ctx context.Context, w jobs.Work) (jobs.Outcome, error) {
	t.calls.Add(1)
	return t.run(ctx, w)
}

func newTool(run func(ctx context.Context, w jobs.Work) (jobs.Outcome, error)) *tool {
	return &tool{name: "test", timeout: time.Minute, run: run}
}

// rig is a store, a queue and a pipeline wired together over one database.
type rig struct {
	db    *database.DB
	store *jobs.Store
	queue *queue.Queue
	pipe  *jobs.Pipeline
	pub   *recorder
	clock *clock
	tool  *tool
	root  string
}

func newRig(t *testing.T, tl *tool) *rig {
	t.Helper()

	db := openDB(t)
	clk := newClock()
	pub := &recorder{}

	store, err := jobs.NewStore(jobs.StoreOptions{
		DB:        db,
		IDs:       ids.NewGenerator(),
		ResultTTL: resultTTL,
		Publisher: pub,
		Now:       clk.Now,
	})
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	q := queue.New(queue.NewMemory(), nil)
	q.SetClock(clk.Now)

	if d, ok := q.Driver().(queue.Clock); ok {
		d.SetClock(clk.Now)
	}

	root := filepath.Join(t.TempDir(), "work")

	pipe, err := jobs.NewPipeline(jobs.PipelineOptions{
		Store:    store,
		Queue:    q,
		Tools:    []jobs.Tool{tl},
		WorkRoot: root,
		Now:      clk.Now,
	})
	if err != nil {
		t.Fatalf("pipeline: %v", err)
	}

	if err := q.Register(pipe.Registration()); err != nil {
		t.Fatalf("register: %v", err)
	}

	return &rig{db: db, store: store, queue: q, pipe: pipe, pub: pub, clock: clk, tool: tl, root: root}
}

// work runs queued conversions until the queue is empty, the way a worker
// process does, but synchronously so a test can assert afterwards.
func (r *rig) work(t *testing.T) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	if err := r.queue.Work(ctx, queue.WorkOptions{StopWhenEmpty: true, Concurrency: 1}); err != nil {
		t.Fatalf("work: %v", err)
	}
}

func (r *rig) submit(t *testing.T, in jobs.Submission) models.Conversion {
	t.Helper()

	if in.UserID == "" {
		in.UserID = owner
	}

	if in.Tool == "" {
		in.Tool = r.tool.name
	}

	c, _, err := r.pipe.Submit(t.Context(), in)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}

	return c
}

func (r *rig) reload(t *testing.T, id string) models.Conversion {
	t.Helper()

	c, err := r.store.Find(t.Context(), owner, id)
	if err != nil {
		t.Fatalf("reload %s: %v", id, err)
	}

	return c
}

func ok(_ context.Context, _ jobs.Work) (jobs.Outcome, error) {
	return jobs.Outcome{BytesOut: 42, CPUMs: 7}, nil
}

// A submitted conversion starts pending, runs, and ends finished with the
// output it reported.
func TestASubmittedConversionRunsAndFinishes(t *testing.T) {
	t.Parallel()

	r := newRig(t, newTool(ok))

	c := r.submit(t, jobs.Submission{Options: json.RawMessage(`{"mode":"rect"}`), BytesIn: 100})
	if got := jobs.Status(c.Status); got != jobs.Pending {
		t.Fatalf("a new conversion is %q, want pending", got)
	}

	r.work(t)

	done := r.reload(t, c.ID)
	if got := jobs.Status(done.Status); got != jobs.Finished {
		t.Fatalf("status = %q, want finished", got)
	}

	if done.Progress != 100 {
		t.Errorf("progress = %d, want 100", done.Progress)
	}

	if done.BytesOut != 42 || done.CPUMs != 7 {
		t.Errorf("bytes_out = %d, cpu_ms = %d; want 42 and 7", done.BytesOut, done.CPUMs)
	}

	if !done.FinishedAt.Valid {
		t.Error("finished_at was not set")
	}

	if done.ErrorType != nil {
		t.Errorf("error_type = %q on a conversion that succeeded", *done.ErrorType)
	}
}

// The ROADMAP's assert: two identical submissions produce one conversion.
func TestTwoIdenticalSubmissionsProduceOneConversion(t *testing.T) {
	t.Parallel()

	r := newRig(t, newTool(ok))
	in := jobs.Submission{UserID: owner, Tool: "test", IdempotencyKey: "abc-123"}

	first, isNew, err := r.pipe.Submit(t.Context(), in)
	if err != nil {
		t.Fatalf("first submit: %v", err)
	}

	if !isNew {
		t.Fatal("the first submission was not reported as new")
	}

	second, isNew, err := r.pipe.Submit(t.Context(), in)
	if err != nil {
		t.Fatalf("second submit: %v", err)
	}

	if isNew {
		t.Fatal("the second submission was reported as new")
	}

	if second.ID != first.ID {
		t.Fatalf("the second submission made a new conversion: %s then %s", first.ID, second.ID)
	}

	count, err := orm.QueryOf[models.Conversion](r.db).Count(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if count != 1 {
		t.Fatalf("%d conversions in the table, want 1", count)
	}
}

// Concurrent submissions with the same key must still produce one
// conversion: the lookup and the insert are not atomic, so the unique index
// is what decides and the loser has to return the winner.
func TestConcurrentSubmissionsWithOneKeyProduceOneConversion(t *testing.T) {
	t.Parallel()

	r := newRig(t, newTool(ok))

	const attempts = 6

	var (
		mu  sync.Mutex
		got = map[string]int{}
		wg  sync.WaitGroup
	)

	for range attempts {
		wg.Go(func() {
			c, _, err := r.pipe.Submit(t.Context(), jobs.Submission{
				UserID: owner, Tool: "test", IdempotencyKey: "race",
			})
			if err != nil {
				t.Errorf("submit: %v", err)

				return
			}

			mu.Lock()
			defer mu.Unlock()

			got[c.ID]++
		})
	}

	wg.Wait()

	if len(got) != 1 {
		t.Fatalf("%d distinct conversions for one idempotency key: %v", len(got), got)
	}
}

// An idempotency key belongs to its owner. Two users using the same key must
// get their own conversion, or one user could read another's work by guessing
// a key.
func TestAnIdempotencyKeyIsScopedToItsOwner(t *testing.T) {
	t.Parallel()

	r := newRig(t, newTool(ok))

	mine, _, err := r.pipe.Submit(t.Context(), jobs.Submission{UserID: owner, Tool: "test", IdempotencyKey: "k"})
	if err != nil {
		t.Fatal(err)
	}

	theirs, _, err := r.pipe.Submit(t.Context(), jobs.Submission{UserID: otherOwner, Tool: "test", IdempotencyKey: "k"})
	if err != nil {
		t.Fatal(err)
	}

	if mine.ID == theirs.ID {
		t.Fatal("two users sharing an idempotency key got the same conversion")
	}
}

// A conversion naming an unknown tool is refused at submission, not queued
// and discovered by a worker.
func TestSubmitRefusesAnUnknownTool(t *testing.T) {
	t.Parallel()

	r := newRig(t, newTool(ok))

	_, _, err := r.pipe.Submit(t.Context(), jobs.Submission{UserID: owner, Tool: "no-such-tool"})
	if !errors.Is(err, jobs.ErrUnknownTool) {
		t.Fatalf("err = %v, want ErrUnknownTool", err)
	}
}

// A permanent failure must be recorded with its code and must not be retried:
// a file that cannot be read will not become readable on the third attempt.
func TestAPermanentFailureIsRecordedAndNotRetried(t *testing.T) {
	t.Parallel()

	tl := newTool(func(context.Context, jobs.Work) (jobs.Outcome, error) {
		return jobs.Outcome{}, jobs.Failf(jobs.UnsupportedInput, "magic bytes say image/gif")
	})
	r := newRig(t, tl)

	c := r.submit(t, jobs.Submission{})
	r.work(t)

	failed := r.reload(t, c.ID)
	if got := jobs.Status(failed.Status); got != jobs.Errored {
		t.Fatalf("status = %q, want error", got)
	}

	if failed.ErrorType == nil || jobs.ErrorType(*failed.ErrorType) != jobs.UnsupportedInput {
		t.Fatalf("error_type = %v, want unsupported_input", failed.ErrorType)
	}

	if n := tl.calls.Load(); n != 1 {
		t.Fatalf("the tool ran %d times, want 1: a permanent failure must not be retried", n)
	}
}

// The owner-only detail must reach the database for the owner to debug with,
// and must never reach the client-facing message.
func TestTheOwnerOnlyDetailIsStoredAndNeverPublished(t *testing.T) {
	t.Parallel()

	const secret = "/srv/work/01JABC/input.psd"

	tl := newTool(func(context.Context, jobs.Work) (jobs.Outcome, error) {
		return jobs.Outcome{}, jobs.Fail(jobs.EngineFailure, errors.New("exit status 1"), "vips "+secret)
	})
	r := newRig(t, tl)

	c := r.submit(t, jobs.Submission{})
	r.work(t)

	failed := r.reload(t, c.ID)
	if failed.ErrorDetail == nil {
		t.Fatal("error_detail was not stored, so the owner cannot debug the failure")
	}

	if !strings.Contains(*failed.ErrorDetail, secret) {
		t.Fatalf("error_detail = %q, want it to name the input", *failed.ErrorDetail)
	}

	for _, e := range r.pub.all() {
		if strings.Contains(e.Message, secret) {
			t.Fatalf("a published event leaked the owner-only detail: %q", e.Message)
		}
	}
}

// A transient failure is retried, and the attempt that succeeds settles the
// conversion. The clock is wound forward instead of slept on, so the backoff
// is exercised without the test taking as long as the backoff.
func TestATransientFailureIsRetriedUntilItSucceeds(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	tl := newTool(func(context.Context, jobs.Work) (jobs.Outcome, error) {
		if attempts.Add(1) == 1 {
			return jobs.Outcome{}, jobs.Failf(jobs.DiskFull, "no space left")
		}

		return jobs.Outcome{BytesOut: 1}, nil
	})
	r := newRig(t, tl)

	c := r.submit(t, jobs.Submission{})

	// The first attempt fails and the message is released with a backoff.
	r.work(t)

	if got := jobs.Status(r.reload(t, c.ID).Status); got != jobs.Running {
		t.Fatalf("after a retryable failure the status is %q, want running", got)
	}

	// Nothing is available until the backoff has passed.
	r.work(t)

	if n := attempts.Load(); n != 1 {
		t.Fatalf("the tool ran %d times before the backoff elapsed, want 1", n)
	}

	r.clock.Advance(time.Minute)
	r.work(t)

	done := r.reload(t, c.ID)
	if got := jobs.Status(done.Status); got != jobs.Finished {
		t.Fatalf("status = %q, want finished", got)
	}

	if n := attempts.Load(); n != 2 {
		t.Fatalf("the tool ran %d times, want 2", n)
	}
}

// A transient failure that never clears must not be retried forever, and the
// conversion must not be left saying "running" once the attempts are spent.
// The reaper is what closes it, because the queue cannot say "no more".
func TestTheReaperClosesAConversionWhoseAttemptsRanOut(t *testing.T) {
	t.Parallel()

	tl := newTool(func(context.Context, jobs.Work) (jobs.Outcome, error) {
		return jobs.Outcome{}, jobs.Failf(jobs.DiskFull, "no space left")
	})
	r := newRig(t, tl)

	c := r.submit(t, jobs.Submission{})

	for range 4 {
		r.work(t)
		r.clock.Advance(time.Minute)
	}

	if got := jobs.Status(r.reload(t, c.ID).Status); got != jobs.Running {
		t.Fatalf("status = %q; the queue is expected to leave it running", got)
	}

	reaper, err := jobs.NewReaper(jobs.ReaperOptions{Store: r.store, After: 30 * time.Minute, Now: r.clock.Now})
	if err != nil {
		t.Fatal(err)
	}

	// Not yet abandoned: a sweep must not close a conversion a worker could
	// still be running.
	closed, err := reaper.Sweep(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if closed != 0 {
		t.Fatalf("the reaper closed %d conversions that are not abandoned yet", closed)
	}

	r.clock.Advance(time.Hour)

	closed, err = reaper.Sweep(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if closed != 1 {
		t.Fatalf("the reaper closed %d conversions, want 1", closed)
	}

	reaped := r.reload(t, c.ID)
	if got := jobs.Status(reaped.Status); got != jobs.Errored {
		t.Fatalf("status = %q, want error", got)
	}

	if reaped.ErrorType == nil || jobs.ErrorType(*reaped.ErrorType) != jobs.Internal {
		t.Fatalf("error_type = %v, want internal", reaped.ErrorType)
	}
}

// Cancelling while the tool is running must stop it. The tool is told through
// Report and its context is cancelled, so a tool that ignores the error still
// loses its context — which is what kills an engine's process group.
func TestCancellingARunningConversionStopsTheTool(t *testing.T) {
	t.Parallel()

	var (
		started      = make(chan struct{})
		contextEnded = make(chan struct{})
		reportErr    error
	)

	tl := newTool(func(_ context.Context, w jobs.Work) (jobs.Outcome, error) {
		close(started)

		// Wait for the cancellation to land, then report, which is how a
		// real tool discovers it between two progress lines.
		<-contextEnded

		reportErr = w.Report(jobs.Step{Percent: 50, Phase: "encoding"})
		if reportErr != nil {
			return jobs.Outcome{}, reportErr
		}

		return jobs.Outcome{}, errors.New("the tool kept going after being cancelled")
	})
	r := newRig(t, tl)

	c := r.submit(t, jobs.Submission{})

	var wg sync.WaitGroup

	wg.Go(func() { r.work(t) })

	<-started

	cancelled, err := r.store.Cancel(t.Context(), owner, c.ID)
	if err != nil {
		t.Fatal(err)
	}

	if !cancelled {
		t.Fatal("Cancel reported that it did not cancel a running conversion")
	}

	close(contextEnded)
	wg.Wait()

	if reportErr == nil {
		t.Fatal("Report did not tell the tool to stop")
	}

	if got := jobs.Classify(reportErr); got != jobs.CancelledByUser {
		t.Fatalf("Report's error classifies as %q, want cancelled", got)
	}

	final := r.reload(t, c.ID)
	if got := jobs.Status(final.Status); got != jobs.Cancelled {
		t.Fatalf("status = %q, want cancelled: the worker overwrote a final state", got)
	}
}

// A conversion that already finished cannot be cancelled: a final state is
// final, whoever asks and in whatever order the requests arrive.
func TestAFinishedConversionCannotBeCancelled(t *testing.T) {
	t.Parallel()

	r := newRig(t, newTool(ok))

	c := r.submit(t, jobs.Submission{})
	r.work(t)

	cancelled, err := r.store.Cancel(t.Context(), owner, c.ID)
	if err != nil {
		t.Fatal(err)
	}

	if cancelled {
		t.Fatal("a finished conversion was cancelled")
	}

	if got := jobs.Status(r.reload(t, c.ID).Status); got != jobs.Finished {
		t.Fatalf("status = %q, want finished", got)
	}
}

// One user must not be able to cancel, read or even confirm the existence of
// another user's conversion.
func TestAConversionIsInvisibleToAnotherUser(t *testing.T) {
	t.Parallel()

	r := newRig(t, newTool(ok))
	c := r.submit(t, jobs.Submission{})

	if _, err := r.store.Find(t.Context(), otherOwner, c.ID); !errors.Is(err, jobs.ErrNotFound) {
		t.Fatalf("Find by another user = %v, want ErrNotFound", err)
	}

	cancelled, err := r.store.Cancel(t.Context(), otherOwner, c.ID)
	if err != nil {
		t.Fatal(err)
	}

	if cancelled {
		t.Fatal("another user cancelled a conversion that is not theirs")
	}
}

// An id that cannot be an id is refused without touching the database, and
// answered exactly like one that does not exist.
func TestAMalformedIDIsAPlainMiss(t *testing.T) {
	t.Parallel()

	r := newRig(t, newTool(ok))

	for _, id := range []string{"", "1", "../../etc/passwd", "' OR 1=1 --"} {
		if _, err := r.store.Find(t.Context(), owner, id); !errors.Is(err, jobs.ErrNotFound) {
			t.Errorf("Find(%q) = %v, want ErrNotFound", id, err)
		}
	}
}

// Progress is both recorded and broadcast, and a client that reconnects sees
// the same trail it would have streamed.
func TestProgressIsRecordedAndBroadcast(t *testing.T) {
	t.Parallel()

	tl := newTool(func(_ context.Context, w jobs.Work) (jobs.Outcome, error) {
		for _, step := range []jobs.Step{
			{Percent: 10, Phase: "decoding"},
			{Percent: 60, Phase: "encoding"},
		} {
			if err := w.Report(step); err != nil {
				return jobs.Outcome{}, err
			}
		}

		return jobs.Outcome{BytesOut: 3}, nil
	})
	r := newRig(t, tl)

	c := r.submit(t, jobs.Submission{})
	r.work(t)

	trail, err := r.store.Events(t.Context(), c.ID, 0)
	if err != nil {
		t.Fatal(err)
	}

	// Started, two progress steps, done.
	if len(trail) != 4 {
		t.Fatalf("the trail has %d lines, want 4: %+v", len(trail), trail)
	}

	wantStatuses := []jobs.Status{jobs.Running, jobs.Running, jobs.Running, jobs.Finished}
	for i, want := range wantStatuses {
		if got := jobs.Status(trail[i].Status); got != want {
			t.Errorf("trail[%d] status = %q, want %q", i, got, want)
		}
	}

	if got := len(r.pub.all()); got != len(trail) {
		t.Fatalf("%d events were broadcast but %d recorded; the two must agree", got, len(trail))
	}

	// Every broadcast event carries the id of its trail line, which is what
	// makes Last-Event-ID resumable.
	for i, e := range r.pub.all() {
		if e.ID != trail[i].ID {
			t.Errorf("event %d has id %d, want %d", i, e.ID, trail[i].ID)
		}

		if e.ConversionID != c.ID {
			t.Errorf("event %d names conversion %q, want %q", i, e.ConversionID, c.ID)
		}
	}

	// Replaying from the second line gives exactly the rest.
	rest, err := r.store.Events(t.Context(), c.ID, trail[1].ID)
	if err != nil {
		t.Fatal(err)
	}

	if len(rest) != 2 {
		t.Fatalf("replay after line 2 returned %d lines, want 2", len(rest))
	}
}

// A broker that is down must not fail a conversion. The state is in the
// database; the stream is only the fast path to it.
func TestAFailedBroadcastDoesNotFailTheConversion(t *testing.T) {
	t.Parallel()

	r := newRig(t, newTool(ok))
	r.pub.fail = errors.New("the broker is down")

	c := r.submit(t, jobs.Submission{})
	r.work(t)

	if got := jobs.Status(r.reload(t, c.ID).Status); got != jobs.Finished {
		t.Fatalf("status = %q, want finished", got)
	}
}

// Progress reported out of range is clamped rather than rejected: an engine's
// own estimate occasionally overshoots, and losing the report would be worse.
func TestProgressIsClamped(t *testing.T) {
	t.Parallel()

	tl := newTool(func(_ context.Context, w jobs.Work) (jobs.Outcome, error) {
		if err := w.Report(jobs.Step{Percent: 140}); err != nil {
			return jobs.Outcome{}, err
		}

		if err := w.Report(jobs.Step{Percent: -5}); err != nil {
			return jobs.Outcome{}, err
		}

		return jobs.Outcome{}, nil
	})
	r := newRig(t, tl)

	c := r.submit(t, jobs.Submission{})
	r.work(t)

	trail, err := r.store.Events(t.Context(), c.ID, 0)
	if err != nil {
		t.Fatal(err)
	}

	for _, line := range trail {
		if line.Progress < 0 || line.Progress > 100 {
			t.Fatalf("a trail line says %d%%", line.Progress)
		}
	}
}

// The tool gets a private, empty directory, and it is gone once the
// conversion ends — including after a panic, which is why the removal is
// deferred.
func TestTheWorkingDirectoryIsPrivateAndRemoved(t *testing.T) {
	t.Parallel()

	var seen string

	tl := newTool(func(_ context.Context, w jobs.Work) (jobs.Outcome, error) {
		seen = w.Dir

		entries, err := os.ReadDir(w.Dir)
		if err != nil {
			return jobs.Outcome{}, err
		}

		if len(entries) != 0 {
			return jobs.Outcome{}, errors.New("the working directory was not empty")
		}

		return jobs.Outcome{}, nil
	})
	r := newRig(t, tl)

	r.submit(t, jobs.Submission{})
	r.work(t)

	if seen == "" {
		t.Fatal("the tool was given no working directory")
	}

	if _, err := os.ReadDir(seen); err == nil {
		t.Fatalf("%s still exists after the conversion ended", seen)
	}
}

// A redelivery of a message whose conversion already ended must do nothing.
// The queue can deliver twice — a reservation is reclaimed after a timeout —
// so the row, not the message, is what decides.
func TestARedeliveryOfASettledConversionDoesNothing(t *testing.T) {
	t.Parallel()

	r := newRig(t, newTool(ok))

	c := r.submit(t, jobs.Submission{})
	r.work(t)

	before := r.reload(t, c.ID)

	// Enqueue the same conversion again, exactly as a reclaimed reservation
	// would.
	if _, _, err := r.pipe.Submit(t.Context(), jobs.Submission{UserID: owner, Tool: "test"}); err != nil {
		t.Fatal(err)
	}

	if err := r.queue.Dispatch(t.Context(), conversionMessage{ConversionID: c.ID}); err != nil {
		t.Fatalf("redispatch: %v", err)
	}

	r.work(t)

	after := r.reload(t, c.ID)
	if !after.FinishedAt.V.Equal(before.FinishedAt.V) {
		t.Fatal("a redelivery re-ran a conversion that had already finished")
	}

	if n := r.tool.calls.Load(); n != 2 {
		t.Fatalf("the tool ran %d times; want 2 (the new conversion, not the redelivery)", n)
	}
}

// conversionMessage mirrors the pipeline's own queue payload so a test can
// enqueue a redelivery. The kind must match, which is what makes a drift in
// the payload shape show up here.
type conversionMessage struct {
	ConversionID string `json:"conversion_id"`
}

func (conversionMessage) Kind() string { return "conversion" }

// A conversion whose tool has been removed between submission and running
// must be settled, not retried into existence.
func TestAConversionNamingAMissingToolIsSettled(t *testing.T) {
	t.Parallel()

	r := newRig(t, newTool(ok))

	c := r.submit(t, jobs.Submission{})

	// Simulate the tool disappearing in a deploy: the row still names it.
	if _, err := orm.QueryOf[models.Conversion](r.db).
		Where("id", c.ID).
		Update(t.Context(), orm.Set{"tool": "removed-in-a-deploy"}); err != nil {
		t.Fatal(err)
	}

	r.work(t)

	failed := r.reload(t, c.ID)
	if got := jobs.Status(failed.Status); got != jobs.Errored {
		t.Fatalf("status = %q, want error", got)
	}

	if failed.ErrorType == nil || jobs.ErrorType(*failed.ErrorType) != jobs.Internal {
		t.Fatalf("error_type = %v, want internal", failed.ErrorType)
	}
}

// The result's clock starts when the result exists. A conversion that queued
// for longer than the lifetime must not hand over something already expired.
func TestTheResultLifetimeStartsWhenTheResultExists(t *testing.T) {
	t.Parallel()

	r := newRig(t, newTool(ok))

	c := r.submit(t, jobs.Submission{})
	r.clock.Advance(2 * resultTTL)
	r.work(t)

	done := r.reload(t, c.ID)
	if !done.ExpiresAt.After(r.clock.Now()) {
		t.Fatalf("expires_at %s is not after now %s", done.ExpiresAt, r.clock.Now())
	}
}

// Store construction refuses a configuration that cannot work, rather than
// failing on the first conversion in production.
func TestNewStoreValidatesItsOptions(t *testing.T) {
	t.Parallel()

	db := openDB(t)
	gen := ids.NewGenerator()

	cases := map[string]jobs.StoreOptions{
		"no database":       {IDs: gen, ResultTTL: time.Hour},
		"no generator":      {DB: db, ResultTTL: time.Hour},
		"no result life":    {DB: db, IDs: gen},
		"negative lifetime": {DB: db, IDs: gen, ResultTTL: -time.Hour},
	}

	for name, o := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := jobs.NewStore(o); err == nil {
				t.Fatal("the options were accepted")
			}
		})
	}
}

func TestNewPipelineRefusesTwoToolsWithOneName(t *testing.T) {
	t.Parallel()

	r := newRig(t, newTool(ok))

	_, err := jobs.NewPipeline(jobs.PipelineOptions{
		Store:    r.store,
		Queue:    r.queue,
		Tools:    []jobs.Tool{newTool(ok), newTool(ok)},
		WorkRoot: r.root,
	})
	if err == nil {
		t.Fatal("two tools with the same name were accepted")
	}
}

// Concurrency must be usable as a pool size: at least one, whatever the host.
func TestConcurrencyIsAtLeastOne(t *testing.T) {
	t.Parallel()

	if got := jobs.Concurrency(); got < 1 {
		t.Fatalf("Concurrency = %d, want at least 1", got)
	}
}
