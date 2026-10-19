package jobs

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/AlexGalhardo/vukem/database"
	"github.com/AlexGalhardo/vukem/orm"
	"github.com/AlexGalhardo/vukem/ulid"

	"github.com/AlexGalhardo/vukem-tools/app/models"
)

// ErrNotFound is returned when no conversion matches. It is deliberately the
// ORM's own 404 error, so a controller that returns it answers 404 without
// mapping anything, and a conversion owned by somebody else is answered
// exactly like one that does not exist.
var ErrNotFound = orm.ErrNotFound

// Store reads and writes conversions and their progress trail.
//
// Every write that changes a conversion's state is conditional on the state
// it is leaving. Two workers can hold the same message — a reservation can be
// reclaimed while the first worker is still alive — and a cancellation can
// land at any moment, so "load, decide, write" would let the later write
// silently undo the earlier one. The condition is in the UPDATE instead, and
// the row count says whether this caller was the one that moved it.
type Store struct {
	db  *database.DB
	ids *ulid.Generator
	now func() time.Time
	ttl time.Duration
	pub Publisher
	log *slog.Logger
}

// StoreOptions configures a Store. DB, IDs and ResultTTL are required.
type StoreOptions struct {
	DB  *database.DB
	IDs *ulid.Generator
	// ResultTTL is how long a result stays downloadable once it exists.
	ResultTTL time.Duration
	// Publisher broadcasts progress. Optional: without one, progress is
	// still recorded, and only the live stream is missing.
	Publisher Publisher
	// Now is the clock. Optional (time.Now).
	Now func() time.Time
	Log *slog.Logger
}

// NewStore returns a store.
func NewStore(o StoreOptions) (*Store, error) {
	switch {
	case o.DB == nil:
		return nil, errors.New("jobs: store needs a database")
	case o.IDs == nil:
		return nil, errors.New("jobs: store needs an id generator")
	case o.ResultTTL <= 0:
		return nil, fmt.Errorf("jobs: result lifetime must be positive, got %s", o.ResultTTL)
	}

	if o.Now == nil {
		o.Now = time.Now
	}

	if o.Log == nil {
		o.Log = slog.Default()
	}

	return &Store{db: o.DB, ids: o.IDs, now: o.Now, ttl: o.ResultTTL, pub: o.Publisher, log: o.Log}, nil
}

// Submission is a request to convert something.
type Submission struct {
	// UserID is the owner. Every read is scoped to it.
	UserID string
	// Tool names the conversion, e.g. "pixels-to-svg".
	Tool string
	// Options is the already-validated tool input, stored as accepted.
	Options json.RawMessage
	// IdempotencyKey, when set, makes a repeated submission return the first
	// conversion instead of starting a second one.
	IdempotencyKey string
	// BytesIn is the size of the input, for the quota and for the UI.
	BytesIn int64
	// Attach runs inside the transaction that creates the conversion, after
	// the row exists and before the queue message is sent. It is how an
	// uploaded input is bound to the conversion atomically: a conversion
	// whose input failed to attach must not exist at all, because a worker
	// would find nothing to read and fail it for a reason that is not the
	// user's.
	Attach func(ctx context.Context, ex database.Executor, conversionID string) error
}

// Create inserts a pending conversion and returns it. ex is the executor to
// write through: Submit passes a transaction, so the insert and the queue
// message commit together.
//
// It does not look for an existing conversion with the same idempotency key:
// that check belongs to Submit, which also has to handle the race between the
// check and the insert. Create is the plain write.
func (s *Store) Create(ctx context.Context, ex database.Executor, in Submission) (models.Conversion, error) {
	options := in.Options
	if len(options) == 0 {
		options = json.RawMessage("{}")
	}

	at := s.now().UTC()

	c := models.Conversion{
		ID:        s.ids.At(at),
		UserID:    in.UserID,
		Tool:      in.Tool,
		Status:    string(Pending),
		BytesIn:   in.BytesIn,
		Options:   orm.JSON[json.RawMessage]{V: options},
		ExpiresAt: at.Add(s.ttl),
	}

	if in.IdempotencyKey != "" {
		c.IdempotencyKey = &in.IdempotencyKey
	}

	if err := orm.Create(ctx, ex, &c); err != nil {
		return models.Conversion{}, fmt.Errorf("jobs: create conversion: %w", err)
	}

	return c, nil
}

// Find returns the conversion with this id belonging to userID.
func (s *Store) Find(ctx context.Context, userID, id string) (models.Conversion, error) {
	// A value that cannot be an id does not need a database round trip, and
	// answering it the same way as a real miss keeps ids unguessable.
	if !ulid.Valid(id) {
		return models.Conversion{}, ErrNotFound
	}

	return orm.QueryOf[models.Conversion](s.db).
		Where("user_id", userID).
		Where("id", id).
		First(ctx)
}

// List returns userID's conversions, newest first.
func (s *Store) List(ctx context.Context, userID string, limit int) ([]models.Conversion, error) {
	if limit <= 0 || limit > maxListLimit {
		limit = maxListLimit
	}

	return orm.QueryOf[models.Conversion](s.db).
		Where("user_id", userID).
		OrderBy("created_at", orm.Desc).
		OrderBy("id", orm.Desc).
		Limit(limit).
		Get(ctx)
}

// maxListLimit caps a listing so a caller cannot ask for every row at once.
const maxListLimit = 100

// ByIdempotencyKey returns the conversion userID already created with this
// key, or ErrNotFound.
func (s *Store) ByIdempotencyKey(ctx context.Context, userID, key string) (models.Conversion, error) {
	if key == "" {
		return models.Conversion{}, ErrNotFound
	}

	return orm.QueryOf[models.Conversion](s.db).
		Where("user_id", userID).
		Where("idempotency_key", key).
		First(ctx)
}

// Claim moves a conversion to running and reports whether this caller was the
// one that moved it.
//
// A message can be delivered more than once, so this is the gate: the first
// claim wins, a redelivery of a conversion already running is told so, and a
// conversion the owner cancelled while it waited in the queue is never
// started. Only pending and running are accepted, which is what keeps a final
// state final.
func (s *Store) Claim(ctx context.Context, id string) (bool, error) {
	at := s.now().UTC()

	n, err := orm.QueryOf[models.Conversion](s.db).
		Where("id", id).
		WhereIn("status", string(Pending), string(Running)).
		Update(ctx, orm.Set{
			"status":     string(Running),
			"started_at": at,
			"updated_at": at,
		})
	if err != nil {
		return false, fmt.Errorf("jobs: claim %s: %w", id, err)
	}

	if n == 0 {
		return false, nil
	}

	return true, s.record(ctx, id, Event{Status: Running, Message: "Started."})
}

// Release puts a running conversion back to pending without using an attempt.
// A worker shutting down mid-job calls it so the next worker starts from a
// state that says "queued", not one that says "somebody is on it".
func (s *Store) Release(ctx context.Context, id string) error {
	at := s.now().UTC()

	_, err := orm.QueryOf[models.Conversion](s.db).
		Where("id", id).
		Where("status", string(Running)).
		Update(ctx, orm.Set{
			"status":     string(Pending),
			"progress":   0,
			"phase":      "",
			"updated_at": at,
		})
	if err != nil {
		return fmt.Errorf("jobs: release %s: %w", id, err)
	}

	return nil
}

// Step is one progress report from a worker.
type Step struct {
	// Percent is 0 to 100. Values outside the range are clamped rather than
	// rejected: an engine's own estimate occasionally overshoots, and losing
	// the whole report over it would be worse than rounding it.
	Percent int
	// Phase is a short label, e.g. "decoding", "encoding", "uploading".
	Phase string
	// Message is shown to the owner, so it holds no path and no engine
	// argument.
	Message string
}

// Advance records progress on a running conversion and appends it to the
// trail. It reports whether the conversion was still running: false means the
// owner cancelled it, which is the worker's signal to stop.
func (s *Store) Advance(ctx context.Context, id string, step Step) (bool, error) {
	percent := min(max(step.Percent, 0), 100)
	at := s.now().UTC()

	n, err := orm.QueryOf[models.Conversion](s.db).
		Where("id", id).
		Where("status", string(Running)).
		Update(ctx, orm.Set{
			"progress":   percent,
			"phase":      step.Phase,
			"updated_at": at,
		})
	if err != nil {
		return false, fmt.Errorf("jobs: advance %s: %w", id, err)
	}

	if n == 0 {
		return false, nil
	}

	return true, s.record(ctx, id, Event{
		Status:   Running,
		Progress: percent,
		Phase:    step.Phase,
		Message:  step.Message,
	})
}

// Outcome is what a finished conversion produced.
type Outcome struct {
	BytesOut int64
	CPUMs    int64
	// Note is the tool's own account of what it did, shown to the owner. It
	// is stored on the conversion rather than only appended to the progress
	// trail, because a conversion that finishes before a browser's event
	// stream connects is answered with a snapshot and never replays the
	// trail — and the fast conversions are exactly the ones whose result
	// most needs explaining, a compression that changed nothing above all.
	//
	// Client-safe by construction: it holds no path and no engine argument,
	// the same contract as Step.Message.
	Note string
}

// Finish settles a conversion as finished. It reports whether this caller
// settled it: false means it had already ended, cancelled being the case that
// matters, and the worker should then delete what it produced.
func (s *Store) Finish(ctx context.Context, id string, out Outcome) (bool, error) {
	at := s.now().UTC()

	n, err := orm.QueryOf[models.Conversion](s.db).
		Where("id", id).
		Where("status", string(Running)).
		Update(ctx, orm.Set{
			"status":      string(Finished),
			"progress":    100,
			"phase":       "",
			"bytes_out":   out.BytesOut,
			"cpu_ms":      out.CPUMs,
			"note":        truncate(out.Note, maxErrorMessage),
			"finished_at": at,
			// The clock on the result starts when the result exists, not when
			// the conversion was submitted: a job that queued for an hour
			// would otherwise hand over a result that is already expired.
			"expires_at": at.Add(s.ttl),
			"updated_at": at,
		})
	if err != nil {
		return false, fmt.Errorf("jobs: finish %s: %w", id, err)
	}

	if n == 0 {
		return false, nil
	}

	// The tool's own sentence where it had one, so a client watching the
	// stream reads the same thing a client that reloads the page reads.
	return true, s.record(ctx, id, Event{
		Status:   Finished,
		Progress: 100,
		Message:  cmp.Or(out.Note, "Done."),
	})
}

// Failure settles a conversion as failed, classifying cause through the
// taxonomy. The client-safe message goes on the trail; cause's owner-only
// detail goes in error_detail and nowhere else.
//
// It accepts a conversion that is pending as well as one that is running: a
// submission can fail before any worker claims it, for instance when the
// quota is already spent.
func (s *Store) Failure(ctx context.Context, id string, cause error) (bool, error) {
	kind := Classify(cause)
	message := MessageOf(cause)
	detail := DetailOf(cause)
	at := s.now().UTC()

	n, err := orm.QueryOf[models.Conversion](s.db).
		Where("id", id).
		WhereIn("status", string(Pending), string(Running)).
		Update(ctx, orm.Set{
			"status":     string(Errored),
			"phase":      "",
			"error_type": string(kind),
			// The specific sentence, stored rather than only appended to the
			// trail: a client that reloads the page reads the row, and
			// deriving the message from the type alone there threw away
			// whatever jobs.Refuse had been given to say.
			"error_message": truncate(message, maxErrorMessage),
			"error_detail":  truncate(detail, maxErrorDetail),
			"finished_at":   at,
			"updated_at":    at,
		})
	if err != nil {
		return false, fmt.Errorf("jobs: fail %s: %w", id, err)
	}

	if n == 0 {
		return false, nil
	}

	return true, s.record(ctx, id, Event{Status: Errored, ErrorType: kind, Message: message})
}

// maxErrorDetail bounds what is stored for the owner. An engine that prints a
// megabyte of diagnostics must not be able to grow the table without limit,
// and the first lines are the useful ones.
const maxErrorDetail = 4000

// maxErrorMessage bounds the client-safe sentence. The messages this app
// writes are one sentence; the bound is here so a future one that interpolates
// an engine's own words cannot grow the row without limit.
const maxErrorMessage = 500

// Cancel stops a conversion the owner still owns, and reports whether it was
// cancelled. A conversion that already finished is not cancellable, which is
// why false is a normal answer and not an error.
func (s *Store) Cancel(ctx context.Context, userID, id string) (bool, error) {
	if !ulid.Valid(id) {
		return false, ErrNotFound
	}

	at := s.now().UTC()

	n, err := orm.QueryOf[models.Conversion](s.db).
		Where("user_id", userID).
		Where("id", id).
		WhereIn("status", string(Pending), string(Running)).
		Update(ctx, orm.Set{
			"status":      string(Cancelled),
			"phase":       "",
			"error_type":  string(CancelledByUser),
			"finished_at": at,
			"updated_at":  at,
		})
	if err != nil {
		return false, fmt.Errorf("jobs: cancel %s: %w", id, err)
	}

	if n == 0 {
		// Either it is not theirs, or it had already ended. Telling the two
		// apart costs a second query, and the caller re-reads the conversion
		// to render it anyway.
		return false, nil
	}

	return true, s.record(ctx, id, Event{
		Status:    Cancelled,
		ErrorType: CancelledByUser,
		Message:   Describe(CancelledByUser).Message,
	})
}

// Abandoned returns conversions left running with no progress since before
// cutoff: a worker that was killed, or one whose retries ran out without the
// queue telling anybody. They are the reaper's input.
func (s *Store) Abandoned(ctx context.Context, cutoff time.Time, limit int) ([]models.Conversion, error) {
	if limit <= 0 || limit > maxListLimit {
		limit = maxListLimit
	}

	return orm.QueryOf[models.Conversion](s.db).
		Where("status", string(Running)).
		Where("updated_at", "<", cutoff.UTC()).
		OrderBy("updated_at", orm.Asc).
		Limit(limit).
		Get(ctx)
}

// Events returns a conversion's progress trail in order, from after the given
// event id. A client that reconnects to the stream replays from there, which
// is what makes Last-Event-ID resumable: the stream is the fast path, this
// table is the truth.
func (s *Store) Events(ctx context.Context, conversionID string, after int64) ([]models.ConversionEvent, error) {
	if !ulid.Valid(conversionID) {
		return nil, ErrNotFound
	}

	return orm.QueryOf[models.ConversionEvent](s.db).
		Where("conversion_id", conversionID).
		Where("id", ">", after).
		OrderBy("id", orm.Asc).
		Limit(maxTrailLength).
		Get(ctx)
}

// maxTrailLength bounds a replay. A conversion reporting more steps than this
// is a bug in the tool, and a client does not need every one of them to catch
// up — the latest state is on the conversion row.
const maxTrailLength = 500

// record appends one line to the trail and broadcasts it.
//
// A failure to append is returned, not swallowed: the trail is what a
// reconnecting client replays, so losing a line loses the end of a conversion
// for anybody who was not watching at that moment.
//
// A failure to broadcast is logged and otherwise ignored. The state is
// already recorded and the trail already has the line, so the client learns
// the same thing when it polls or reconnects — a few seconds later. Returning
// it would make the worker treat a delivered conversion as a failed one.
func (s *Store) record(ctx context.Context, conversionID string, e Event) error {
	row := models.ConversionEvent{
		ConversionID: conversionID,
		Status:       string(e.Status),
		Progress:     e.Progress,
		Phase:        e.Phase,
		Message:      e.Message,
		CreatedAt:    s.now().UTC(),
	}

	if err := orm.Create(ctx, s.db, &row); err != nil {
		return fmt.Errorf("jobs: append event for %s: %w", conversionID, err)
	}

	if s.pub == nil {
		return nil
	}

	e.ID, e.ConversionID, e.At = row.ID, conversionID, row.CreatedAt

	if err := s.pub.Publish(ctx, conversionID, e); err != nil {
		s.log.WarnContext(ctx, "jobs: progress event not broadcast",
			"conversion", conversionID, "status", e.Status, "err", err)
	}

	return nil
}

// truncate shortens s to at most n bytes, on a rune boundary so the result is
// still valid UTF-8 and the database does not reject it.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}

	cut := n
	for cut > 0 && !utf8StartsRune(s[cut]) {
		cut--
	}

	return s[:cut]
}

// utf8StartsRune reports whether b can begin a UTF-8 sequence.
func utf8StartsRune(b byte) bool { return b&0xC0 != 0x80 }
