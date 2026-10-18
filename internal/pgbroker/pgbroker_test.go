package pgbroker_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AlexGalhardo/vukem/database"
	"github.com/AlexGalhardo/vukem/drivers/postgres"
	"github.com/AlexGalhardo/vukem/drivers/postgres/pgtest"

	"github.com/AlexGalhardo/vukem-tools/internal/pgbroker"
)

// The broker is the one piece of the progress path that cannot be tested on
// SQLite: LISTEN/NOTIFY is the feature under test. CI always provides a
// server, so this never skips there.
func url(t *testing.T) string {
	t.Helper()

	if os.Getenv("TEST_DATABASE_URL") == "" && os.Getenv("POSTGRES_URL") == "" {
		t.Skip("no PostgreSQL: set TEST_DATABASE_URL (CI always does, so this is never skipped there)")
	}

	return pgtest.Database(t, "")
}

// open connects to dsn. Notifications are scoped to one database, so a test
// that needs two processes opens two connections to the SAME dsn: calling
// pgtest.Database twice would create two databases, and nothing published in
// one would ever arrive in the other.
func open(t *testing.T, dsn string) *database.DB {
	t.Helper()

	db, err := database.Open(t.Context(), dsn, database.WithDriver(postgres.Driver()))
	if err != nil {
		t.Fatalf("open the test database: %v", err)
	}

	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close the test database: %v", err)
		}
	})

	return db
}

func newBroker(t *testing.T) *pgbroker.Broker {
	t.Helper()

	b, err := pgbroker.New(open(t, url(t)), nil)
	if err != nil {
		t.Fatalf("new broker: %v", err)
	}

	return b
}

// receive waits for one payload, so a hung subscription fails the test
// instead of hanging the suite.
func receive(t *testing.T, ch <-chan []byte) string {
	t.Helper()

	select {
	case got, ok := <-ch:
		if !ok {
			t.Fatal("the subscription closed before delivering anything")
		}

		return string(got)
	case <-time.After(10 * time.Second):
		t.Fatal("no notification within 10s")

		return ""
	}
}

// Subscribe must have the LISTEN in place before it returns. If it did not,
// an event published immediately afterwards would be lost, which is exactly
// the race a progress stream hits: the browser subscribes, then the worker
// starts.
func TestSubscribeIsListeningBeforeItReturns(t *testing.T) {
	t.Parallel()

	b := newBroker(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	ch, err := b.Subscribe(ctx, "sse:conversions:01JABCDEF")
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	if err := b.Publish(ctx, "sse:conversions:01JABCDEF", []byte(`{"progress":10}`)); err != nil {
		t.Fatalf("publish: %v", err)
	}

	if got := receive(t, ch); got != `{"progress":10}` {
		t.Fatalf("got %q", got)
	}
}

// This is the property the whole design rests on: a publish from one
// connection reaches a subscriber on another. The web service and the worker
// are separate processes, so if this fails, progress never leaves the worker.
func TestAPublishOnOneConnectionReachesASubscriberOnAnother(t *testing.T) {
	t.Parallel()

	const channel = "sse:conversions:cross-process"

	dsn := url(t)

	listener, err := pgbroker.New(open(t, dsn), nil)
	if err != nil {
		t.Fatalf("new listener: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	ch, err := listener.Subscribe(ctx, channel)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	// A second Broker, on its own pool over the same database, stands in for
	// the worker process.
	publisher, err := pgbroker.New(open(t, dsn), nil)
	if err != nil {
		t.Fatalf("new publisher: %v", err)
	}

	if err := publisher.Publish(ctx, channel, []byte("from the worker")); err != nil {
		t.Fatalf("publish: %v", err)
	}

	if got := receive(t, ch); got != "from the worker" {
		t.Fatalf("got %q", got)
	}
}

// Channels must be isolated: one conversion's progress must never appear on
// another conversion's stream, which would leak one user's work to another.
func TestAChannelOnlyReceivesItsOwnEvents(t *testing.T) {
	t.Parallel()

	b := newBroker(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	mine, err := b.Subscribe(ctx, "sse:conversions:mine")
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	if err := b.Publish(ctx, "sse:conversions:theirs", []byte("not for me")); err != nil {
		t.Fatalf("publish: %v", err)
	}

	if err := b.Publish(ctx, "sse:conversions:mine", []byte("for me")); err != nil {
		t.Fatalf("publish: %v", err)
	}

	// The second publish arriving proves the first had its chance: both went
	// through the same connection in order.
	if got := receive(t, mine); got != "for me" {
		t.Fatalf("got %q, so another channel's event was delivered", got)
	}
}

// Every subscriber of a channel gets every event. Two browsers watching the
// same conversion must both see it finish.
func TestEverySubscriberOfAChannelReceivesTheEvent(t *testing.T) {
	t.Parallel()

	const channel = "sse:conversions:shared"

	b := newBroker(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	first, err := b.Subscribe(ctx, channel)
	if err != nil {
		t.Fatalf("subscribe first: %v", err)
	}

	second, err := b.Subscribe(ctx, channel)
	if err != nil {
		t.Fatalf("subscribe second: %v", err)
	}

	if err := b.Publish(ctx, channel, []byte("done")); err != nil {
		t.Fatalf("publish: %v", err)
	}

	var wg sync.WaitGroup
	for _, ch := range []<-chan []byte{first, second} {
		wg.Go(func() {
			if got := receive(t, ch); got != "done" {
				t.Errorf("got %q", got)
			}
		})
	}

	wg.Wait()
}

// A cancelled context must end the subscription and close the channel, which
// is what returns the listening connection to the pool. A leak here would
// exhaust the pool after enough browsers had watched a conversion.
func TestCancellingTheContextClosesTheSubscription(t *testing.T) {
	t.Parallel()

	b := newBroker(t)

	ctx, cancel := context.WithCancel(t.Context())

	ch, err := b.Subscribe(ctx, "sse:conversions:closing")
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	cancel()

	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("a payload arrived after the context was cancelled")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the subscription channel was not closed within 10s of cancelling")
	}
}

// PostgreSQL caps a NOTIFY payload at 8000 bytes and fails the statement
// above it. Refusing early, with a named error, beats a publish that fails
// deep inside the driver on the one event that mattered.
func TestPublishRefusesAnOversizedPayload(t *testing.T) {
	t.Parallel()

	b := newBroker(t)

	err := b.Publish(t.Context(), "sse:conversions:big", []byte(strings.Repeat("x", pgbroker.MaxPayload+1)))
	if !errors.Is(err, pgbroker.ErrPayloadTooLarge) {
		t.Fatalf("err = %v, want ErrPayloadTooLarge", err)
	}
}

// A payload exactly at the limit must still go through, so the limit is a
// limit and not an off-by-one.
func TestPublishAcceptsAPayloadAtTheLimit(t *testing.T) {
	t.Parallel()

	const channel = "sse:conversions:at-the-limit"

	b := newBroker(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	ch, err := b.Subscribe(ctx, channel)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	payload := strings.Repeat("x", pgbroker.MaxPayload)
	if err := b.Publish(ctx, channel, []byte(payload)); err != nil {
		t.Fatalf("publish: %v", err)
	}

	if got := receive(t, ch); got != payload {
		t.Fatalf("got %d bytes, want %d", len(got), len(payload))
	}
}

// A long channel name must work: PostgreSQL identifiers stop at 63 bytes, and
// the Hub's topic for a conversion is longer than it looks once prefixed.
// Hashing the name is what makes this pass, so this test is what stops anyone
// "simplifying" it back to interpolating the name.
func TestALongChannelNameWorks(t *testing.T) {
	t.Parallel()

	channel := "sse:conversions:" + strings.Repeat("a", 200)

	b := newBroker(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	ch, err := b.Subscribe(ctx, channel)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	if err := b.Publish(ctx, channel, []byte("ok")); err != nil {
		t.Fatalf("publish: %v", err)
	}

	if got := receive(t, ch); got != "ok" {
		t.Fatalf("got %q", got)
	}
}

// A channel name holding a quote must not be able to change the statement.
// The name never reaches SQL as text — it is hashed — and this test is the
// proof, not the comment above identifier.
func TestAHostileChannelNameCannotInjectSQL(t *testing.T) {
	t.Parallel()

	channel := `sse:"; DROP TABLE users; --`

	b := newBroker(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	ch, err := b.Subscribe(ctx, channel)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	if err := b.Publish(ctx, channel, []byte("still here")); err != nil {
		t.Fatalf("publish: %v", err)
	}

	if got := receive(t, ch); got != "still here" {
		t.Fatalf("got %q", got)
	}
}

func TestSubscribeNeedsAChannel(t *testing.T) {
	t.Parallel()

	b := newBroker(t)

	if _, err := b.Subscribe(t.Context()); err == nil {
		t.Fatal("Subscribe with no channel was accepted")
	}
}

// A broker on SQLite would accept every publish and deliver nothing. Failing
// at construction turns a silent production fault into a boot error.
func TestNewRefusesAnythingThatIsNotPostgres(t *testing.T) {
	t.Parallel()

	db, err := database.Open(t.Context(), "sqlite:"+filepath.Join(t.TempDir(), "not-postgres.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})

	if _, err := pgbroker.New(db, nil); !errors.Is(err, pgbroker.ErrNotPostgres) {
		t.Fatalf("err = %v, want ErrNotPostgres", err)
	}

	if _, err := pgbroker.New(nil, nil); !errors.Is(err, pgbroker.ErrNotPostgres) {
		t.Fatalf("err for a nil database = %v, want ErrNotPostgres", err)
	}
}
