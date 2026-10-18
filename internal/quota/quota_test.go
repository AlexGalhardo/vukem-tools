package quota_test

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/AlexGalhardo/vukem/database"
	"github.com/AlexGalhardo/vukem/schema"

	"github.com/AlexGalhardo/vukem-tools/database/migrations"
	"github.com/AlexGalhardo/vukem-tools/internal/quota"
)

const (
	owner      = "7"
	otherOwner = "8"
	budget     = time.Hour
)

// clock is a hand-wound clock: the budget resets at midnight UTC, so the
// tests move the day rather than wait for one.
type clock struct {
	mu sync.Mutex
	at time.Time
}

func newClock() *clock {
	return &clock{at: time.Date(2026, 10, 18, 9, 30, 0, 0, time.UTC)}
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.at
}

func (c *clock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.at = t
}

// openDB returns a migrated database on a file that this test owns. Not
// t.TempDir directly: on Windows the SQLite file can still be held for a
// moment after Close, and t.TempDir's own cleanup then fails the test for a
// reason that has nothing to do with quotas.
func openDB(t *testing.T) *database.DB {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "db")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatalf("temp dir: %v", err)
	}

	db, err := database.Open(t.Context(), "sqlite:"+filepath.Join(dir, "quota.db"))
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

func newCounter(t *testing.T, db *database.DB, clk *clock, exempt ...string) *quota.Counter {
	t.Helper()

	c, err := quota.New(quota.Options{DB: db, Budget: budget, Exempt: exempt, Now: clk.Now})
	if err != nil {
		t.Fatalf("counter: %v", err)
	}

	return c
}

// An account that has converted nothing today has spent nothing, and that
// must not need a row to be true.
func TestAnAccountWithNoUsageHasSpentNothing(t *testing.T) {
	t.Parallel()

	c := newCounter(t, openDB(t), newClock())

	got, err := c.Check(t.Context(), owner)
	if err != nil {
		t.Fatal(err)
	}

	if !got.Allowed {
		t.Fatal("a fresh account is not allowed to convert")
	}

	if got.Spent != 0 {
		t.Fatalf("spent = %s, want 0", got.Spent)
	}

	if got.Remaining() != budget {
		t.Fatalf("remaining = %s, want %s", got.Remaining(), budget)
	}
}

// The ROADMAP's assert: the counter equals the sum of the measured durations.
func TestTheCounterEqualsTheSumOfWhatWasSpent(t *testing.T) {
	t.Parallel()

	c := newCounter(t, openDB(t), newClock())

	spends := []time.Duration{
		1500 * time.Millisecond,
		250 * time.Millisecond,
		10 * time.Second,
		time.Minute,
	}

	var want time.Duration

	for _, d := range spends {
		if err := c.Spend(t.Context(), owner, quota.Usage{Spent: d, BytesOut: 10}); err != nil {
			t.Fatal(err)
		}

		want += d
	}

	got, err := c.Check(t.Context(), owner)
	if err != nil {
		t.Fatal(err)
	}

	if got.Spent != want {
		t.Fatalf("spent = %s, want %s", got.Spent, want)
	}
}

// Concurrent finishes must not lose an increment: a read-modify-write would,
// and a conversion nobody was charged for is a hole in the budget.
func TestConcurrentSpendsAreAllCounted(t *testing.T) {
	t.Parallel()

	c := newCounter(t, openDB(t), newClock())

	const (
		workers = 8
		each    = 10
		unit    = 100 * time.Millisecond
	)

	var wg sync.WaitGroup

	for range workers {
		wg.Go(func() {
			for range each {
				if err := c.Spend(t.Context(), owner, quota.Usage{Spent: unit}); err != nil {
					t.Errorf("spend: %v", err)

					return
				}
			}
		})
	}

	wg.Wait()

	got, err := c.Check(t.Context(), owner)
	if err != nil {
		t.Fatal(err)
	}

	if want := workers * each * unit; got.Spent != want {
		t.Fatalf("spent = %s, want %s: an increment was lost", got.Spent, want)
	}
}

// Past the budget the account is refused, and told when it resets.
func TestAnAccountPastItsBudgetIsRefused(t *testing.T) {
	t.Parallel()

	clk := newClock()
	c := newCounter(t, openDB(t), clk)

	if err := c.Spend(t.Context(), owner, quota.Usage{Spent: budget}); err != nil {
		t.Fatal(err)
	}

	got, err := c.Check(t.Context(), owner)
	if err != nil {
		t.Fatal(err)
	}

	if got.Allowed {
		t.Fatal("an account that spent its whole budget is still allowed")
	}

	if got.Remaining() != 0 {
		t.Fatalf("remaining = %s, want 0", got.Remaining())
	}

	// Midnight UTC of the next day, which is what the user is told.
	want := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	if !got.ResetsAt.Equal(want) {
		t.Fatalf("resets at %s, want %s", got.ResetsAt, want)
	}

	// Retry-After is a whole number of seconds, rounded up, and positive.
	if retry := got.RetryAfter(clk.Now()); retry <= 0 || retry%time.Second != 0 {
		t.Fatalf("RetryAfter = %s, want a positive whole number of seconds", retry)
	}
}

// The budget is per day: crossing midnight UTC starts a fresh one.
func TestTheBudgetResetsAtMidnight(t *testing.T) {
	t.Parallel()

	clk := newClock()
	c := newCounter(t, openDB(t), clk)

	if err := c.Spend(t.Context(), owner, quota.Usage{Spent: budget}); err != nil {
		t.Fatal(err)
	}

	if got, err := c.Check(t.Context(), owner); err != nil || got.Allowed {
		t.Fatalf("allowed = %v, err = %v; want refused today", got.Allowed, err)
	}

	clk.Set(time.Date(2026, 10, 19, 0, 0, 1, 0, time.UTC))

	got, err := c.Check(t.Context(), owner)
	if err != nil {
		t.Fatal(err)
	}

	if !got.Allowed {
		t.Fatal("the budget did not reset after midnight")
	}

	if got.Spent != 0 {
		t.Fatalf("spent = %s on a new day, want 0", got.Spent)
	}
}

// One account's spending must not count against another's.
func TestTheBudgetIsPerAccount(t *testing.T) {
	t.Parallel()

	c := newCounter(t, openDB(t), newClock())

	if err := c.Spend(t.Context(), owner, quota.Usage{Spent: budget}); err != nil {
		t.Fatal(err)
	}

	got, err := c.Check(t.Context(), otherOwner)
	if err != nil {
		t.Fatal(err)
	}

	if !got.Allowed || got.Spent != 0 {
		t.Fatalf("another account is affected: allowed=%v spent=%s", got.Allowed, got.Spent)
	}
}

// An exempt account is never refused, and is still counted — otherwise the
// owner's own usage would be invisible on the very dashboard that exists to
// show it.
func TestAnExemptAccountIsCountedButNeverRefused(t *testing.T) {
	t.Parallel()

	c := newCounter(t, openDB(t), newClock(), owner)

	if err := c.Spend(t.Context(), owner, quota.Usage{Spent: 10 * budget}); err != nil {
		t.Fatal(err)
	}

	got, err := c.Check(t.Context(), owner)
	if err != nil {
		t.Fatal(err)
	}

	if !got.Allowed {
		t.Fatal("an exempt account was refused")
	}

	if !got.Exempt {
		t.Fatal("the allowance does not report the exemption")
	}

	if got.Spent != 10*budget {
		t.Fatalf("spent = %s, want %s: an exempt account must still be counted", got.Spent, 10*budget)
	}
}

// A zero budget means no limit, which is the right default for a
// single-user instance. It must not read as "no allowance".
func TestAZeroBudgetMeansNoLimit(t *testing.T) {
	t.Parallel()

	clk := newClock()

	c, err := quota.New(quota.Options{DB: openDB(t), Budget: 0, Now: clk.Now})
	if err != nil {
		t.Fatal(err)
	}

	if err := c.Spend(t.Context(), owner, quota.Usage{Spent: 1000 * time.Hour}); err != nil {
		t.Fatal(err)
	}

	got, err := c.Check(t.Context(), owner)
	if err != nil {
		t.Fatal(err)
	}

	if !got.Allowed {
		t.Fatal("a zero budget refused an account")
	}

	if got.Budget != 0 {
		t.Fatalf("budget = %s, want 0", got.Budget)
	}
}

// The ROADMAP's assert: the state survives a restart, because it lives in the
// database rather than in memory.
func TestUsageSurvivesARestart(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "db")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	dsn := "sqlite:" + filepath.Join(dir, "quota.db")
	clk := newClock()

	// First "process": migrate and spend.
	first, err := database.Open(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}

	m, err := schema.NewMigrator(first, migrations.All())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := m.Up(t.Context()); err != nil {
		t.Fatal(err)
	}

	before := newCounter(t, first, clk)
	if err := before.Spend(t.Context(), owner, quota.Usage{Spent: 42 * time.Minute}); err != nil {
		t.Fatal(err)
	}

	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	// Second "process": a new connection over the same file.
	second, err := database.Open(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := second.Close(); err != nil {
			t.Errorf("close: %v", err)
		}

		removeWithRetry(t, dir)
	})

	got, err := newCounter(t, second, clk).Check(t.Context(), owner)
	if err != nil {
		t.Fatal(err)
	}

	if got.Spent != 42*time.Minute {
		t.Fatalf("after a restart spent = %s, want 42m", got.Spent)
	}
}

// Sub-millisecond work is not free: a tool that finishes in 400 microseconds
// still counts, rounded down, so a flood of tiny conversions is bounded by
// the per-minute rate limit rather than silently costing nothing.
func TestSubMillisecondWorkRoundsDown(t *testing.T) {
	t.Parallel()

	c := newCounter(t, openDB(t), newClock())

	if err := c.Spend(t.Context(), owner, quota.Usage{Spent: 400 * time.Microsecond}); err != nil {
		t.Fatal(err)
	}

	got, err := c.Check(t.Context(), owner)
	if err != nil {
		t.Fatal(err)
	}

	if got.Spent != 0 {
		t.Fatalf("spent = %s, want 0 (milliseconds is the unit)", got.Spent)
	}
}

func TestNewValidatesItsOptions(t *testing.T) {
	t.Parallel()

	db := openDB(t)

	if _, err := quota.New(quota.Options{Budget: budget}); err == nil {
		t.Error("a counter with no database was accepted")
	}

	if _, err := quota.New(quota.Options{DB: db, Budget: -time.Hour}); err == nil {
		t.Error("a negative budget was accepted")
	}
}

// ErrExceeded is the sentinel the pipeline reports, so it must stay
// recognisable through wrapping.
func TestErrExceededIsRecognisable(t *testing.T) {
	t.Parallel()

	if !errors.Is(errors.Join(errors.New("convert: "), quota.ErrExceeded), quota.ErrExceeded) {
		t.Fatal("ErrExceeded is not recognisable through wrapping")
	}
}
