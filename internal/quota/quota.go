// Package quota records what each account spends and refuses work past its
// daily budget.
//
// The budget is measured in processing time, not in a file count, because a
// 2 KB sprite and a 500 MB video cost nothing alike and a per-file limit
// would either be useless for the first or punishing for the second.
//
// What is measured is honest about what it is. For a tool that runs in this
// process the figure is wall-clock duration: Go gives no per-goroutine CPU
// time, so pretending otherwise would be a lie in a column named cpu_ms. Once
// a conversion is a subprocess the runner reports its real user plus system
// time, which is strictly smaller, so the budget gets more generous rather
// than less — the safe direction for a limit nobody has tuned yet.
package quota

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/AlexGalhardo/vukem/database"
)

// Counter reads and writes the per-day usage of each account.
type Counter struct {
	db     *database.DB
	budget time.Duration
	exempt map[string]struct{}
	now    func() time.Time
	log    *slog.Logger
	table  string
}

// Options configures a Counter. DB is required.
type Options struct {
	DB *database.DB
	// Budget is how much processing time an account may spend per day. Zero
	// means no limit, which is the right default for a self-hosted instance
	// with one user and the wrong one for a public service — so it is a
	// setting, not a constant.
	Budget time.Duration
	// Exempt lists user ids the budget does not apply to, normally just the
	// owner's. An exempt account is still counted, so the figures stay true.
	Exempt []string
	Now    func() time.Time
	Log    *slog.Logger
}

// New returns a counter.
func New(o Options) (*Counter, error) {
	if o.DB == nil {
		return nil, errors.New("quota: needs a database")
	}

	if o.Budget < 0 {
		return nil, fmt.Errorf("quota: the budget cannot be negative, got %s", o.Budget)
	}

	if o.Now == nil {
		o.Now = time.Now
	}

	if o.Log == nil {
		o.Log = slog.Default()
	}

	exempt := make(map[string]struct{}, len(o.Exempt))
	for _, id := range o.Exempt {
		if id != "" {
			exempt[id] = struct{}{}
		}
	}

	return &Counter{db: o.DB, budget: o.Budget, exempt: exempt, now: o.Now, log: o.Log, table: "usage_counters"}, nil
}

// Usage is what one conversion consumed.
type Usage struct {
	// Spent is the measured processing time.
	Spent time.Duration
	// BytesIn and BytesOut are the sizes moved, recorded for the account page
	// rather than for the limit.
	BytesIn, BytesOut int64
	// Conversions is how many conversions this records, normally one.
	Conversions int
}

// Allowance is what an account may still do today.
type Allowance struct {
	// Allowed is false when the budget is spent.
	Allowed bool
	// Spent and Budget are today's figures. Budget is zero when there is no
	// limit.
	Spent, Budget time.Duration
	// ResetsAt is midnight UTC, when the next day's row starts.
	ResetsAt time.Time
	// Exempt reports that the budget does not apply to this account.
	Exempt bool
}

// Remaining is how much of the budget is left, never negative. It is zero
// when there is no limit, so a caller must read Budget to tell "nothing left"
// from "no limit".
func (a Allowance) Remaining() time.Duration {
	if a.Budget == 0 {
		return 0
	}

	return max(a.Budget-a.Spent, 0)
}

// RetryAfter is how long until the budget resets, rounded up to a whole
// second, which is what an HTTP Retry-After header takes.
func (a Allowance) RetryAfter(now time.Time) time.Duration {
	if d := a.ResetsAt.Sub(now); d > 0 {
		return d.Truncate(time.Second) + time.Second
	}

	return time.Second
}

// ErrExceeded means the account has spent today's budget.
var ErrExceeded = errors.New("quota: today's budget is spent")

// Check reports what the account may still do today.
//
// A missing row is not an error: an account that has converted nothing today
// has spent nothing, which is the common case and does not deserve a write.
func (c *Counter) Check(ctx context.Context, userID string) (Allowance, error) {
	day := c.day()

	a := Allowance{
		Allowed:  true,
		Budget:   c.budget,
		ResetsAt: day.AddDate(0, 0, 1),
	}

	if _, ok := c.exempt[userID]; ok {
		a.Exempt = true
	}

	spent, err := c.spentOn(ctx, userID, day)
	if err != nil {
		return Allowance{}, err
	}

	a.Spent = spent

	if c.budget > 0 && !a.Exempt && spent >= c.budget {
		a.Allowed = false
	}

	return a, nil
}

// spentOn reads today's measured time, or zero when there is no row.
func (c *Counter) spentOn(ctx context.Context, userID string, day time.Time) (time.Duration, error) {
	dl := c.db.Dialect()
	query := "SELECT cpu_ms FROM " + dl.Quote(c.table) +
		" WHERE user_id = " + dl.Placeholder(1) + " AND day = " + dl.Placeholder(2)

	var ms int64

	switch err := c.db.QueryRowContext(ctx, query, userID, day).Scan(&ms); {
	case err == nil:
		return time.Duration(ms) * time.Millisecond, nil
	case isNoRows(err):
		return 0, nil
	default:
		return 0, fmt.Errorf("quota: read today's usage: %w", err)
	}
}

// Spend records what a conversion consumed, creating today's row or adding to
// it.
//
// It is an upsert rather than a read-modify-write: two workers finish at the
// same moment often enough, and a lost increment is a conversion nobody was
// charged for. Both PostgreSQL and SQLite take this form; the app runs on
// PostgreSQL and the tests on SQLite, and MySQL would need its own statement.
func (c *Counter) Spend(ctx context.Context, userID string, u Usage) error {
	if u.Conversions == 0 {
		u.Conversions = 1
	}

	ms := u.Spent.Milliseconds()
	day := c.day()
	at := c.now().UTC()
	dl := c.db.Dialect()
	t := dl.Quote(c.table)

	// The placeholders are built from the dialect rather than written as "?",
	// because PostgreSQL numbers them and SQLite does not; the ORM's own
	// rebinding is not reachable for a statement this package writes by hand.
	ph := make([]string, 13)
	for i := range ph {
		ph[i] = dl.Placeholder(i + 1)
	}

	query := "INSERT INTO " + t +
		" (user_id, day, cpu_ms, bytes_in, bytes_out, conversions, created_at, updated_at) VALUES (" +
		strings.Join(ph[:8], ", ") + ") ON CONFLICT (user_id, day) DO UPDATE SET " +
		"cpu_ms = " + t + ".cpu_ms + " + ph[8] + ", " +
		"bytes_in = " + t + ".bytes_in + " + ph[9] + ", " +
		"bytes_out = " + t + ".bytes_out + " + ph[10] + ", " +
		"conversions = " + t + ".conversions + " + ph[11] + ", " +
		"updated_at = " + ph[12]

	if _, err := c.db.ExecContext(ctx, query,
		userID, day, ms, u.BytesIn, u.BytesOut, u.Conversions, at, at,
		ms, u.BytesIn, u.BytesOut, u.Conversions, at,
	); err != nil {
		return fmt.Errorf("quota: record usage for %s: %w", userID, err)
	}

	return nil
}

// day is today in UTC, truncated to midnight.
//
// UTC rather than the user's zone: the row is keyed by date, and a budget that
// reset at a different moment for each account would make "today" ambiguous
// in every query an operator writes. The reset time is reported to the user,
// so it is visible rather than guessed.
func (c *Counter) day() time.Time {
	t := c.now().UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// isNoRows reports whether err is the empty-result error, by message, because
// database/sql's own sentinel is not reachable without importing it here for
// one comparison.
func isNoRows(err error) bool {
	return err != nil && err.Error() == "sql: no rows in result set"
}
