package apiv1

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/AlexGalhardo/vukem/auth"
	"github.com/AlexGalhardo/vukem/database"
	"github.com/AlexGalhardo/vukem/errs"
	"github.com/AlexGalhardo/vukem/sse"
	"github.com/AlexGalhardo/vukem/web"
	"github.com/AlexGalhardo/vukem/z"

	"github.com/AlexGalhardo/vukem-tools/app/models"
	"github.com/AlexGalhardo/vukem-tools/app/tools"
	"github.com/AlexGalhardo/vukem-tools/internal/detect"
	"github.com/AlexGalhardo/vukem-tools/internal/files"
	"github.com/AlexGalhardo/vukem-tools/internal/jobs"
	"github.com/AlexGalhardo/vukem-tools/internal/quota"
)

// Conversions reads, cancels and streams conversions.
type Conversions struct {
	pipe  *jobs.Pipeline
	store *jobs.Store
	files *files.Store
	quota *quota.Counter
	hub   *sse.Hub
}

// NewConversions returns the conversion handlers.
func NewConversions(
	pipe *jobs.Pipeline,
	store *jobs.Store,
	f *files.Store,
	q *quota.Counter,
	hub *sse.Hub,
) *Conversions {
	return &Conversions{pipe: pipe, store: store, files: f, quota: q, hub: hub}
}

// ConversionView is one conversion as the API reports it.
//
// It is built by hand rather than serialised from the model, so adding a
// column cannot accidentally publish it. error_detail in particular is
// owner-only and must never appear here.
type ConversionView struct {
	ID     string `json:"id"     doc:"The conversion id, used to poll it, stream it or cancel it."`
	Tool   string `json:"tool"   doc:"Which conversion produced it."`
	Status string `json:"status" doc:"One of pending, running, finished, error, cancelled."`

	Progress int    `json:"progress" doc:"Percentage from 0 to 100."`
	Phase    string `json:"phase"    doc:"What it is doing now, empty once it has ended."`

	BytesIn  int64 `json:"bytes_in"  doc:"Size of the input in bytes."`
	BytesOut int64 `json:"bytes_out" doc:"Size of the result in bytes, 0 until it finishes."`

	// ErrorType is a stable code; ErrorMessage is the sentence to show. Both
	// are empty unless the conversion failed. GET /api/v1/conversion-errors
	// lists every code.
	ErrorType    string `json:"error_type"    doc:"A code from the error taxonomy, empty unless it failed."`
	ErrorMessage string `json:"error_message" doc:"What to tell the user, empty unless it failed."`

	StartedAt  *time.Time `json:"started_at"  doc:"When a worker picked it up, or null."`
	FinishedAt *time.Time `json:"finished_at" doc:"When it ended, or null."`
	ExpiresAt  time.Time  `json:"expires_at"  doc:"When the result stops being downloadable."`
	CreatedAt  time.Time  `json:"created_at"  doc:"When it was submitted."`
}

// viewOf renders a conversion for the API.
func viewOf(c models.Conversion) ConversionView {
	v := ConversionView{
		ID:        c.ID,
		Tool:      c.Tool,
		Status:    c.Status,
		Progress:  c.Progress,
		Phase:     c.Phase,
		BytesIn:   c.BytesIn,
		BytesOut:  c.BytesOut,
		ExpiresAt: c.ExpiresAt,
		CreatedAt: c.CreatedAt,
	}

	if c.ErrorType != nil {
		v.ErrorType = *c.ErrorType
		v.ErrorMessage = jobs.Describe(jobs.ErrorType(*c.ErrorType)).Message
	}

	if c.StartedAt.Valid {
		at := c.StartedAt.V
		v.StartedAt = &at
	}

	if c.FinishedAt.Valid {
		at := c.FinishedAt.V
		v.FinishedAt = &at
	}

	return v
}

// SubmitPixelSVG is the body of POST /api/v1/tools/pixels-to-svg.
//
// There is one submit route per tool rather than one generic route carrying
// opaque JSON. Two reasons, and the second is the one that settled it: the
// OpenAPI document then describes each tool's options exactly instead of
// saying "an object", and the typed-handler validator derives its schema from
// the Go type, so raw JSON cannot be an input field at all — neither
// json.RawMessage nor map[string]any is a type it can describe.
type SubmitPixelSVG struct {
	// FileID is an upload of this caller's that is not already attached to a
	// conversion.
	FileID string `json:"file_id" validate:"required,trim" doc:"The id of an upload of yours, from the resumable upload endpoint."`
	// IdempotencyKey makes a repeated submission return the first conversion
	// instead of starting a second one. Scoped to the caller.
	IdempotencyKey string `json:"idempotency_key" validate:"trim,max=128" doc:"Repeat a submission safely: the same key returns the same conversion."`
	// Options are the tool's own options. Every field is optional and has a
	// documented default.
	Options tools.PixelSVGOptions `json:"options" doc:"How to convert. Every field is optional."`
}

// PixelSVG submits a pixel-art to SVG conversion:
// POST /api/v1/tools/pixels-to-svg.
func (h *Conversions) PixelSVG(c *web.Ctx, in SubmitPixelSVG) (ConversionView, error) {
	opts, err := in.Options.Normalise()
	if err != nil {
		return ConversionView{}, err
	}

	encoded, err := json.Marshal(opts)
	if err != nil {
		return ConversionView{}, err
	}

	return h.submit(c, submission{
		Tool:           "pixels-to-svg",
		FileID:         in.FileID,
		IdempotencyKey: in.IdempotencyKey,
		Options:        encoded,
	})
}

// submission is what every per-tool handler reduces to.
type submission struct {
	Tool           string
	FileID         string
	IdempotencyKey string
	Options        json.RawMessage
}

// submit is the shared path behind every per-tool route: check the ability,
// check the file, then create the conversion and attach the input in one
// transaction.
//
// A conversion whose input failed to attach never exists, because a worker
// would otherwise find nothing to read and fail it for a reason that is not
// the caller's.
func (h *Conversions) submit(c *web.Ctx, in submission) (ConversionView, error) {
	userID, err := callerID(c)
	if err != nil {
		return ConversionView{}, err
	}

	tool, ok := h.pipe.Tool(in.Tool)
	if !ok {
		// A route exists for a tool the pipeline does not have: a wiring
		// mistake, not a client error.
		return ConversionView{}, errs.NewKey(http.StatusServiceUnavailable, "conversion.tool_unavailable",
			"That tool is not available right now.", nil)
	}

	if err := requireAbility(c, tool.Ability()); err != nil {
		return ConversionView{}, err
	}

	if err := h.withinBudget(c, userID); err != nil {
		return ConversionView{}, err
	}

	input, err := h.files.Find(c.Context(), userID, in.FileID)
	if err != nil {
		// Someone else's upload, or one that does not exist: the same answer
		// either way, so an id cannot be probed.
		return ConversionView{}, z.FieldError("file_id", "That upload does not exist.")
	}

	if accepter, ok := tool.(jobs.Accepter); ok && !slices.Contains(accepter.Accepted(), input.MIME) {
		// Refused here rather than in the worker: the caller is still on the
		// phone, and no worker has been spent on a file this tool cannot read.
		kind, _ := detect.KindOf(input.MIME)
		if kind.Label == "" {
			kind.Label = input.MIME
		}

		return ConversionView{}, z.FieldError("file_id",
			"That upload is "+kind.Label+", which this tool cannot read.")
	}

	created, _, err := h.pipe.Submit(c.Context(), jobs.Submission{
		UserID:         userID,
		Tool:           in.Tool,
		Options:        in.Options,
		IdempotencyKey: in.IdempotencyKey,
		BytesIn:        input.SizeBytes,
		Attach: func(ctx context.Context, ex database.Executor, conversionID string) error {
			attached, err := h.files.AttachTx(ctx, ex, userID, in.FileID, conversionID)
			if err != nil {
				return err
			}

			if !attached {
				// Already used by another conversion, or taken by a
				// concurrent submission. Either way this conversion must not
				// exist, so the transaction rolls back.
				return errInputAlreadyUsed
			}

			return nil
		},
	})
	if err != nil {
		if errors.Is(err, errInputAlreadyUsed) {
			return ConversionView{}, z.FieldError("file_id",
				"That upload has already been converted. Upload it again to convert it twice.")
		}

		return ConversionView{}, err
	}

	return viewOf(created), nil
}

// errInputAlreadyUsed is returned from inside the submit transaction when the
// upload is no longer available to attach, which rolls the transaction back.
var errInputAlreadyUsed = errors.New("apiv1: the upload is already attached to a conversion")

// withinBudget refuses a submission once the account has spent today's
// budget, with Retry-After naming when it resets.
//
// It is checked here as well as in the worker so the answer is immediate: a
// 429 the caller reads now beats a conversion that is accepted, queued and
// then fails for a reason the caller could have been told about at once.
func (h *Conversions) withinBudget(c *web.Ctx, userID string) error {
	if h.quota == nil {
		return nil
	}

	allowance, err := h.quota.Check(c.Context(), userID)
	if err != nil {
		return err
	}

	if allowance.Allowed {
		return nil
	}

	retry := allowance.RetryAfter(time.Now())
	c.Writer().Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())))

	return errs.NewKey(http.StatusTooManyRequests, "conversion.quota",
		"You have used today's conversion budget. It resets at "+
			allowance.ResetsAt.Format("15:04")+" UTC.", nil)
}

// requireAbility refuses a key that was not granted the tool's ability.
//
// A session has no abilities and is allowed everything the user is; an API key
// lists what it may do, so a key minted for one tool cannot start another.
func requireAbility(c *web.Ctx, ability string) error {
	tok, ok := auth.CurrentToken(c, "api")
	if !ok {
		return nil
	}

	if !tok.Can(ability) {
		return errs.NewKey(http.StatusForbidden, "conversion.ability",
			"This key may not use that tool. It needs the "+ability+" ability.", nil)
	}

	return nil
}

// ConversionList is the answer to GET /api/v1/conversions.
type ConversionList struct {
	Data []ConversionView `json:"data" doc:"The caller's conversions, newest first."`
}

// ConversionIDInput is the path input of the per-conversion routes.
//
// ID carries no `doc` tag because the generator reads it for query parameters
// only and drops it for path parameters; the description lives in
// routes/apidocs.go instead (workaround(vukem): path parameters ignore the
// doc tag).
type ConversionIDInput struct {
	ID string `json:"id" validate:"required"`
}

// ListInput is the query of GET /api/v1/conversions.
type ListInput struct {
	Limit int `json:"limit" validate:"min=0,max=100" doc:"How many to return, 1 to 100. Default 20."`
}

// defaultListLimit is what a caller gets without asking.
const defaultListLimit = 20

// Index lists the caller's conversions, newest first:
// GET /api/v1/conversions.
func (h *Conversions) Index(c *web.Ctx, in ListInput) (ConversionList, error) {
	userID, err := callerID(c)
	if err != nil {
		return ConversionList{}, err
	}

	limit := in.Limit
	if limit == 0 {
		limit = defaultListLimit
	}

	rows, err := h.store.List(c.Context(), userID, limit)
	if err != nil {
		return ConversionList{}, err
	}

	// An empty array, never null: a client should not have to handle both.
	out := ConversionList{Data: make([]ConversionView, 0, len(rows))}
	for i := range rows {
		out.Data = append(out.Data, viewOf(rows[i]))
	}

	return out, nil
}

// Show returns one conversion: GET /api/v1/conversions/{id}.
//
// This is also the polling endpoint. A client that cannot hold a stream open
// — the Telegram bot, a shell script — asks for this until status is final.
func (h *Conversions) Show(c *web.Ctx, in ConversionIDInput) (ConversionView, error) {
	userID, err := callerID(c)
	if err != nil {
		return ConversionView{}, err
	}

	row, err := h.store.Find(c.Context(), userID, in.ID)
	if err != nil {
		return ConversionView{}, err
	}

	return viewOf(row), nil
}

// Cancel stops a conversion: DELETE /api/v1/conversions/{id}.
//
// It returns the conversion as it now stands rather than an empty body, so a
// client learns in one round trip whether it was cancelled or had already
// finished. Cancelling something that already ended is answered 409, which is
// what the taxonomy says for that code.
func (h *Conversions) Cancel(c *web.Ctx, in ConversionIDInput) (ConversionView, error) {
	userID, err := callerID(c)
	if err != nil {
		return ConversionView{}, err
	}

	cancelled, err := h.store.Cancel(c.Context(), userID, in.ID)
	if err != nil {
		return ConversionView{}, err
	}

	// Re-read either way: on success it is the cancelled row, and on failure
	// the read is what tells a missing conversion from a finished one.
	row, err := h.store.Find(c.Context(), userID, in.ID)
	if err != nil {
		return ConversionView{}, err
	}

	if !cancelled {
		info := jobs.Describe(jobs.CancelledByUser)

		return ConversionView{}, errs.NewKey(info.Status, "conversion.not_cancellable",
			"That conversion has already ended.", nil)
	}

	return viewOf(row), nil
}

// ResultFile is one output of a finished conversion, with a link to it.
type ResultFile struct {
	Name      string    `json:"name"       doc:"The file name to save it as."`
	MIME      string    `json:"mime"       doc:"Its media type."`
	SizeBytes int64     `json:"size_bytes" doc:"Its size."`
	URL       string    `json:"url"        doc:"A signed link, valid until expires_at and no longer."`
	ExpiresAt time.Time `json:"expires_at" doc:"When the file is deleted and the link stops working."`
}

// ResultList is the answer to GET /api/v1/conversions/{id}/result.
type ResultList struct {
	Data []ResultFile `json:"data" doc:"The files this conversion produced."`
}

// Result returns signed links to a finished conversion's output:
// GET /api/v1/conversions/{id}/result.
//
// The link is made here rather than in the client because only the server can
// sign one, and it is signed on request rather than stored so that it is never
// older than the request that asked for it — a stored link would start
// expiring the moment the conversion finished, whether anybody had read it or
// not.
func (h *Conversions) Result(c *web.Ctx, in ConversionIDInput) (ResultList, error) {
	userID, err := callerID(c)
	if err != nil {
		return ResultList{}, err
	}

	// Scoped first: the conversion has to be the caller's before any of its
	// files are named, let alone signed.
	conversion, err := h.store.Find(c.Context(), userID, in.ID)
	if err != nil {
		return ResultList{}, err
	}

	if jobs.Status(conversion.Status) != jobs.Finished {
		return ResultList{}, errs.NewKey(http.StatusConflict, "conversion.unfinished",
			"That conversion has not produced a result.", nil)
	}

	outputs, err := h.files.ForConversion(c.Context(), conversion.ID, models.FileOutput)
	if err != nil {
		return ResultList{}, err
	}

	out := ResultList{Data: make([]ResultFile, 0, len(outputs))}

	for i := range outputs {
		f := outputs[i]

		link, err := h.files.DownloadURL(f, 0)
		if err != nil {
			if errors.Is(err, files.ErrExpired) {
				// The cleanup has reached it, or is about to. 410 rather than
				// 404: it existed, and saying so is what tells the user to
				// convert again rather than to look for a typo.
				return ResultList{}, errs.NewKey(http.StatusGone, "conversion.expired",
					"That result has expired. Convert the file again for a new one.", nil)
			}

			return ResultList{}, err
		}

		out.Data = append(out.Data, ResultFile{
			Name:      f.Name,
			MIME:      f.MIME,
			SizeBytes: f.SizeBytes,
			URL:       link,
			ExpiresAt: f.ExpiresAt,
		})
	}

	return out, nil
}

// ErrorCatalog is the answer to GET /api/v1/conversion-errors.
type ErrorCatalog struct {
	Data []ErrorCatalogEntry `json:"data" doc:"Every error code a conversion can end with."`
}

// ErrorCatalogEntry is one code of the taxonomy.
type ErrorCatalogEntry struct {
	Type    string `json:"type"    doc:"The stable code, as it appears on a conversion."`
	Message string `json:"message" doc:"What to tell the user."`
	Retry   bool   `json:"retry"   doc:"Whether submitting the same input again could succeed."`
}

// Errors lists the error taxonomy: GET /api/v1/conversion-errors.
//
// It is published so a client does not have to hardcode the codes, and so the
// interface and the API cannot drift apart about what a failure means.
func (*Conversions) Errors(_ *web.Ctx, _ struct{}) (ErrorCatalog, error) {
	catalog := jobs.Catalog()

	out := ErrorCatalog{Data: make([]ErrorCatalogEntry, 0, len(catalog))}
	for _, info := range catalog {
		out.Data = append(out.Data, ErrorCatalogEntry{
			Type:    string(info.Type),
			Message: info.Message,
			Retry:   info.Retry,
		})
	}

	return out, nil
}

// ToolList is the answer to GET /api/v1/tools.
type ToolList struct {
	Data []string `json:"data" doc:"Every tool that can be submitted, by name."`
}

// Tools lists the registered tools: GET /api/v1/tools.
func (h *Conversions) Tools(_ *web.Ctx, _ struct{}) (ToolList, error) {
	return ToolList{Data: h.pipe.Tools()}, nil
}

// Stream sends a conversion's progress as server-sent events:
// GET /api/v1/conversions/{id}/events.
//
// It is a plain handler, not a typed one, because a stream has no response
// body an OpenAPI schema can describe; it is hidden from the generated
// document and written up in docs/api.md instead.
//
// The order of what it sends matters. First the current state, so a client
// that connects after the work is done still learns the outcome instead of
// waiting for an event that will never arrive. Then the trail after
// Last-Event-ID, so a reconnecting client misses nothing. Only then live
// events. Subscribing before the replay — rather than after — is what closes
// the gap: an event published during the replay is already buffered.
func (h *Conversions) Stream(c *web.Ctx) error {
	userID, err := callerID(c)
	if err != nil {
		return err
	}

	id := c.Request().PathValue("id")

	// Authorise before opening the stream: a 404 must be a 404, not an empty
	// stream that looks like a conversion with nothing to report.
	current, err := h.store.Find(c.Context(), userID, id)
	if err != nil {
		return err
	}

	events, unsubscribe := h.hub.Subscribe(c.Context(), jobs.Topic(id))
	defer unsubscribe()

	return sse.Serve(c.Writer(), c.Request(), func(stream *sse.Stream) error {
		if err := stream.SendJSON(jobs.EventName, jobs.Snapshot(current)); err != nil {
			return err
		}

		last, err := h.replay(c, stream, id)
		if err != nil {
			return err
		}

		if jobs.Status(current.Status).Final() {
			// Nothing more will ever be published. Ending the stream lets the
			// client close instead of holding a connection open on a
			// heartbeat forever.
			return nil
		}

		return pump(stream, events, last)
	})
}

// replay sends the trail after the client's Last-Event-ID and returns the id
// of the last event sent, so the live pump can drop anything it has already
// seen.
func (h *Conversions) replay(c *web.Ctx, stream *sse.Stream, id string) (int64, error) {
	after := int64(0)
	if v, err := strconv.ParseInt(stream.LastEventID(), 10, 64); err == nil && v > 0 {
		after = v
	}

	trail, err := h.store.Events(c.Context(), id, after)
	if err != nil {
		return 0, err
	}

	last := after

	for i := range trail {
		e := jobs.EventOf(id, trail[i])
		if err := stream.SendEvent(sseEvent(e)); err != nil {
			return 0, err
		}

		last = e.ID
	}

	return last, nil
}

// pump forwards live events until the conversion ends, the client goes away
// or the server shuts down.
//
// An event at or below last was already replayed, so it is dropped: the
// subscription is opened before the replay on purpose, which means the
// overlap is expected rather than a bug.
func pump(stream *sse.Stream, events <-chan sse.Event, last int64) error {
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				// The hub dropped this subscriber, which it does when a
				// subscriber falls too far behind. Ending the stream makes the
				// client reconnect and replay, which is the recovery.
				return nil
			}

			if id, err := strconv.ParseInt(ev.ID, 10, 64); err == nil && id <= last {
				continue
			}

			if err := stream.SendEvent(ev); err != nil {
				return err
			}

			if finalEvent(ev) {
				return nil
			}
		case <-sse.ShutdownSignal(stream.Context()):
			return nil
		case <-stream.Context().Done():
			return nil
		}
	}
}

// sseEvent renders a progress event as a wire event. It mirrors
// jobs.HubPublisher so a replayed event and a live one are identical.
func sseEvent(e jobs.Event) sse.Event {
	body, err := json.Marshal(e)
	if err != nil {
		// Encoding a struct of strings and integers cannot fail; if it ever
		// does, an empty data frame beats a half-written stream.
		body = []byte("{}")
	}

	return sse.Event{ID: strconv.FormatInt(e.ID, 10), Event: jobs.EventName, Data: string(body)}
}

// finalEvent reports whether a wire event ends the conversion, by reading the
// status out of the payload the publisher wrote.
func finalEvent(ev sse.Event) bool {
	var e jobs.Event
	if err := json.Unmarshal([]byte(ev.Data), &e); err != nil {
		return false
	}

	return e.Final()
}

// caller returns the authenticated user, from whichever guard authenticated
// this request.
//
// The routes accept both: a programmatic client carries an API key, and the
// app's own pages call the same endpoints with the session they already have.
// Asking only the "api" guard is what made every tool answer 401 to a
// signed-in browser while the upload that produced the file_id succeeded --
// app/controllers/uploads.go already resolved its owner across guards, which
// is why the two halves of one flow disagreed.
func caller(c *web.Ctx) (models.User, error) {
	for _, guard := range []string{"web", "api"} {
		if u, err := auth.User[models.User](c, guard); err == nil {
			return u, nil
		}
	}

	return models.User{}, auth.ErrUnauthenticated
}

// callerID returns the authenticated caller's user id.
func callerID(c *web.Ctx) (string, error) {
	u, err := caller(c)
	if err != nil {
		return "", err
	}

	return u.AuthID(), nil
}
