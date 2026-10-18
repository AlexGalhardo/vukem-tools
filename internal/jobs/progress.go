package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/AlexGalhardo/vukem/sse"

	"github.com/AlexGalhardo/vukem-tools/app/models"
)

// Topic is the event topic of one conversion. It is also the broker channel,
// so it must identify the conversion and nothing else: a client is allowed to
// subscribe only after the conversion has been shown to be theirs.
func Topic(conversionID string) string { return "conversions:" + conversionID }

// EventName is the server-sent event name every progress message uses. One
// name keeps the client to a single handler; the payload carries the state.
const EventName = "progress"

// Event is one progress message, as a client receives it.
//
// It is deliberately small. The broker's payload limit is 8000 bytes, and a
// message that needs more than this has no business on a progress stream.
type Event struct {
	// ID is the event's place in the trail. A client sends the last one it
	// saw as Last-Event-ID and is given everything after it.
	ID int64 `json:"id"`
	// ConversionID lets one client multiplex several conversions over one
	// connection later without changing the payload.
	ConversionID string `json:"conversion_id"`
	Status       Status `json:"status"`
	Progress     int    `json:"progress"`
	Phase        string `json:"phase"`
	// Message is client-safe text: either the taxonomy's message or a phase
	// label. It never holds a path or an engine argument.
	Message string `json:"message"`
	// ErrorType is set only on a failure, and is a code from the taxonomy.
	ErrorType ErrorType `json:"error_type,omitzero"`
	At        time.Time `json:"at"`
}

// Final reports whether no further event will follow, so the stream can be
// closed instead of held open for a conversion that has ended.
func (e Event) Final() bool { return e.Status.Final() }

// Publisher broadcasts an event to whoever is watching a conversion.
//
// The store depends on this interface rather than on vukem/sse so that the
// write path can be tested without a hub, and so a publish failure can never
// be confused with a failure to record the conversion's state.
type Publisher interface {
	Publish(ctx context.Context, conversionID string, e Event) error
}

// HubPublisher publishes through a vukem/sse hub. With a broker behind the
// hub, an event published by the worker process reaches a stream held open by
// the web process.
type HubPublisher struct{ hub *sse.Hub }

// NewHubPublisher returns a publisher on hub.
func NewHubPublisher(hub *sse.Hub) *HubPublisher { return &HubPublisher{hub: hub} }

// Publish implements Publisher.
func (p *HubPublisher) Publish(ctx context.Context, conversionID string, e Event) error {
	body, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("jobs: encode progress event: %w", err)
	}

	return p.hub.Publish(ctx, Topic(conversionID), sse.Event{
		ID:    strconv.FormatInt(e.ID, 10),
		Event: EventName,
		Data:  string(body),
	})
}

// EventOf renders a stored trail line as a client event, so a replay and a
// live event are byte-identical.
func EventOf(conversionID string, row models.ConversionEvent) Event {
	return Event{
		ID:           row.ID,
		ConversionID: conversionID,
		Status:       Status(row.Status),
		Progress:     row.Progress,
		Phase:        row.Phase,
		Message:      row.Message,
		At:           row.CreatedAt,
	}
}

// Snapshot renders a conversion as the event a client would have received for
// its current state. A stream starts with it, so a client that connects after
// the work is done still learns the outcome instead of waiting for an event
// that will never come.
func Snapshot(c models.Conversion) Event {
	e := Event{
		ConversionID: c.ID,
		Status:       Status(c.Status),
		Progress:     c.Progress,
		Phase:        c.Phase,
		At:           c.UpdatedAt,
	}

	if c.ErrorType != nil {
		e.ErrorType = ErrorType(*c.ErrorType)
		e.Message = Describe(e.ErrorType).Message
	}

	return e
}
