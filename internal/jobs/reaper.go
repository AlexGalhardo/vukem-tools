package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// Reaper closes conversions left running by nobody.
//
// It exists because the queue cannot tell a handler that its retries are
// spent (see framework feedback). Two cases end up here:
//
//   - A worker was killed — out of memory, a container replaced mid-deploy —
//     while it held a conversion. The queue reclaims the message after the
//     timeout and another worker finishes the job, so this is rare, but the
//     row says "running" in the meantime and nothing else would ever correct
//     it if the message is lost as well.
//   - A transient failure was retried until the attempts ran out. The queue
//     moves the message to failed_jobs and tells nobody, so the row would say
//     "running" forever and the user would watch a progress bar that never
//     moves again.
//
// It only ever closes a row that has not been touched for longer than any
// attempt could run, so it cannot race a worker that is simply slow.
//
// The second case above is handled before the reaper ever sees it: settle
// checks queue.AttemptFrom and closes the row on the last attempt. What is
// left is the first — a worker that dies between claiming a conversion and
// settling it — and no queue feature can fix that, because the process that
// would have reported it is gone. This is the backstop for exactly that.
type Reaper struct {
	store *Store
	after time.Duration
	batch int
	log   *slog.Logger
	now   func() time.Time
}

// ReaperOptions configures a Reaper. Store and After are required.
type ReaperOptions struct {
	Store *Store
	// After is how long a conversion may go without progress before it is
	// considered abandoned. It must be comfortably longer than the longest
	// tool timeout plus the queue's reclaim grace, or a slow conversion would
	// be failed while it is still running.
	After time.Duration
	// Batch caps one sweep, so a backlog cannot hold a transaction open or
	// starve the scheduler. The next sweep takes the next batch.
	Batch int
	Log   *slog.Logger
	Now   func() time.Time
}

// NewReaper returns a reaper.
func NewReaper(o ReaperOptions) (*Reaper, error) {
	switch {
	case o.Store == nil:
		return nil, errors.New("jobs: reaper needs a store")
	case o.After <= 0:
		return nil, fmt.Errorf("jobs: reaper needs a positive window, got %s", o.After)
	}

	if o.Batch <= 0 {
		o.Batch = defaultReapBatch
	}

	if o.Log == nil {
		o.Log = slog.Default()
	}

	if o.Now == nil {
		o.Now = time.Now
	}

	return &Reaper{store: o.Store, after: o.After, batch: o.Batch, log: o.Log, now: o.Now}, nil
}

// defaultReapBatch is how many abandoned conversions one sweep closes.
const defaultReapBatch = 50

// Sweep closes every abandoned conversion it finds, up to the batch size, and
// returns how many it closed.
//
// A row that another worker settles between the read and the write is skipped
// rather than overwritten: Failure is conditional on the state it leaves, so
// the race resolves in favour of whoever actually did the work.
func (r *Reaper) Sweep(ctx context.Context) (int, error) {
	cutoff := r.now().UTC().Add(-r.after)

	abandoned, err := r.store.Abandoned(ctx, cutoff, r.batch)
	if err != nil {
		return 0, err
	}

	var closed int

	for i := range abandoned {
		c := &abandoned[i]

		cause := Failf(Internal, "abandoned: no progress since %s", c.UpdatedAt.UTC().Format(time.RFC3339))

		settled, err := r.store.Failure(ctx, c.ID, cause)
		if err != nil {
			// One bad row must not stop the sweep: the rest are just as
			// stuck, and the next sweep retries this one.
			r.log.ErrorContext(ctx, "jobs: could not close an abandoned conversion",
				"conversion", c.ID, "err", err)

			continue
		}

		if !settled {
			continue
		}

		closed++

		r.log.WarnContext(ctx, "jobs: closed an abandoned conversion",
			"conversion", c.ID, "tool", c.Tool, "idle_since", c.UpdatedAt.UTC())
	}

	return closed, nil
}
