// Package pgbroker carries server-sent events between processes over
// PostgreSQL LISTEN/NOTIFY.
//
// workaround(vukem): sse.Hub has no broker for an app on PostgreSQL
//
// vukem/sse.Hub fans events out inside one process. To reach a browser
// connected to the web service with progress produced by the worker service
// it needs an sse.Broker, and the framework ships exactly one: drivers/redis.
// This app runs on PostgreSQL and nothing else, so adding Redis purely to
// move progress events would mean a second stateful service to pay for, to
// back up and to monitor. LISTEN/NOTIFY does the same job on a database that
// is already there.
//
// What it costs, and why each cost is acceptable here:
//
//   - A NOTIFY payload must be under 8000 bytes. Publish refuses a larger
//     one rather than truncating it, and progress events are tens of bytes.
//   - LISTEN occupies a connection for as long as the subscription lives, so
//     a subscription is taken out of the pool. The Hub holds one per topic
//     with local subscribers and releases it when the last one leaves, so
//     the count follows the number of watched conversions, not the number of
//     watching browsers.
//   - A notification is not persisted. A listener that is not connected when
//     NOTIFY runs never sees it, which is why progress is also written to
//     conversion_events: the stream is the fast path, the table is the truth.
package pgbroker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"

	"github.com/AlexGalhardo/vukem/database"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

// MaxPayload is the largest payload PostgreSQL accepts on NOTIFY. The
// documented limit is 8000 bytes; the margin leaves room for the quoting the
// driver adds.
const MaxPayload = 7900

// ErrPayloadTooLarge is returned by Publish for a payload over MaxPayload.
// It is a programming error, not a runtime condition: an event that large
// does not belong on a notification channel.
var ErrPayloadTooLarge = errors.New("pgbroker: payload over the NOTIFY limit")

// ErrNotPostgres is returned when the database behind the pool is not
// PostgreSQL, or is reached through a driver that is not pgx. Both are
// configuration mistakes, and failing loudly beats a broker that silently
// delivers nothing.
var ErrNotPostgres = errors.New("pgbroker: needs a PostgreSQL connection through pgx")

// Broker publishes and receives events over LISTEN/NOTIFY. It implements
// vukem/sse.Broker.
type Broker struct {
	db  *database.DB
	log *slog.Logger
}

// New returns a broker on db. It fails rather than degrade when db is not
// PostgreSQL: a broker that accepts every publish and delivers nothing would
// look like working code and break only in production, under load, in the
// one path nobody watches.
func New(db *database.DB, log *slog.Logger) (*Broker, error) {
	if db == nil {
		return nil, ErrNotPostgres
	}

	if name := db.Dialect().Name(); name != "postgres" {
		return nil, fmt.Errorf("%w: dialect is %q", ErrNotPostgres, name)
	}

	if log == nil {
		log = slog.Default()
	}

	return &Broker{db: db, log: log}, nil
}

// identifier maps a channel name to a PostgreSQL identifier.
//
// Channel names come from the Hub and look like "sse:conversions:<ulid>":
// they hold characters that would have to be quoted and can pass the 63-byte
// identifier limit. Hashing sidesteps both, and since LISTEN and NOTIFY go
// through this same function they always agree. The prefix keeps the name
// recognisable in pg_stat_activity.
func identifier(channel string) string {
	sum := sha256.Sum256([]byte(channel))
	return "vt_" + hex.EncodeToString(sum[:16])
}

// Publish sends payload to every listener of channel.
//
// pg_notify is used instead of the NOTIFY statement because NOTIFY takes a
// literal, not a parameter: building it would mean concatenating a payload
// into SQL. pg_notify is an ordinary function call with bound parameters.
func (b *Broker) Publish(ctx context.Context, channel string, payload []byte) error {
	if len(payload) > MaxPayload {
		return fmt.Errorf("%w: %d bytes", ErrPayloadTooLarge, len(payload))
	}

	if _, err := b.db.ExecContext(ctx, "SELECT pg_notify($1, $2)", identifier(channel), string(payload)); err != nil {
		return fmt.Errorf("pgbroker: notify %s: %w", channel, err)
	}

	return nil
}

// Subscribe listens on channels until ctx is done, then closes the returned
// channel. It returns once the LISTEN is in place, so an event published
// after it returns is delivered.
//
// A notification that nobody is ready to receive is dropped rather than
// allowed to block the listening connection: a stalled subscriber must not
// stop the one connection that serves every other subscriber of the same
// topic. The Hub already treats a gap as a reason for the client to
// reconnect, and the client then replays from conversion_events.
func (b *Broker) Subscribe(ctx context.Context, channels ...string) (<-chan []byte, error) {
	if len(channels) == 0 {
		return nil, errors.New("pgbroker: subscribe needs at least one channel")
	}

	conn, err := b.db.SQL().Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("pgbroker: take a connection: %w", err)
	}

	// listening is closed by the pump once every LISTEN has been issued, so
	// Subscribe can return only after the subscription is really in place.
	listening := make(chan error, 1)
	out := make(chan []byte, outBuffer)

	go func() {
		defer close(out)

		// The connection is returned to the pool only after the pump ends.
		// Closing it here, rather than on the caller's goroutine, is what
		// makes "ctx is done" the single way a subscription ends.
		defer func() {
			if cerr := conn.Close(); cerr != nil {
				b.log.DebugContext(ctx, "pgbroker: closing the listening connection", "err", cerr)
			}
		}()

		err := conn.Raw(func(driverConn any) error {
			return b.pump(ctx, driverConn, channels, listening, out)
		})
		// A context that is done is how a subscription is meant to end.
		if err != nil && ctx.Err() == nil {
			b.log.ErrorContext(ctx, "pgbroker: subscription ended", "channels", channels, "err", err)
		}
	}()

	if err := <-listening; err != nil {
		return nil, err
	}

	return out, nil
}

// outBuffer is how many notifications may be waiting for a subscriber before
// the next one is dropped. The Hub applies its own buffer per stream; this
// one only absorbs a burst between the listening connection and the Hub.
const outBuffer = 64

// pump issues the LISTENs and then forwards notifications until ctx is done.
// It runs inside sql.Conn.Raw, which is the only place the pgx connection may
// be touched, so the whole lifetime of the subscription lives in here.
func (b *Broker) pump(
	ctx context.Context,
	driverConn any,
	channels []string,
	listening chan<- error,
	out chan<- []byte,
) error {
	c, ok := driverConn.(*stdlib.Conn)
	if !ok {
		err := fmt.Errorf("%w: driver connection is %T", ErrNotPostgres, driverConn)
		listening <- err

		return err
	}

	pc := c.Conn()

	for _, channel := range channels {
		// identifier produces [a-z0-9_] only, so it cannot carry quoting out
		// of the string it is interpolated into. LISTEN takes no parameters.
		if _, err := pc.Exec(ctx, `LISTEN `+identifier(channel)); err != nil {
			err = fmt.Errorf("pgbroker: listen %s: %w", channel, err)
			listening <- err

			return err
		}
	}

	listening <- nil

	return forward(ctx, pc, out)
}

// forward moves notifications to out until ctx is done.
func forward(ctx context.Context, pc *pgx.Conn, out chan<- []byte) error {
	for {
		n, err := pc.WaitForNotification(ctx)
		if err != nil {
			return err
		}

		select {
		case out <- []byte(n.Payload):
		case <-ctx.Done():
			return ctx.Err()
		default:
			// Dropped on purpose: see Subscribe.
		}
	}
}
