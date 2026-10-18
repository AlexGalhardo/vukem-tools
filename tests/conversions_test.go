package tests

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/AlexGalhardo/vukem/orm"
	"github.com/AlexGalhardo/vukem/ulid"
	"github.com/AlexGalhardo/vukem/vukemtest"

	"github.com/AlexGalhardo/vukem-tools/app/models"
	"github.com/AlexGalhardo/vukem-tools/bootstrap"
	"github.com/AlexGalhardo/vukem-tools/internal/jobs"
	"github.com/AlexGalhardo/vukem-tools/routes"
)

// seedConversion writes a conversion straight to the database. Submitting one
// through the pipeline needs an input file, which is the next phase; these
// tests are about reading, cancelling and streaming what already exists.
func seedConversion(t *testing.T, app *vukemtest.App, userID string, status jobs.Status) models.Conversion {
	t.Helper()

	db, err := app.DB()
	if err != nil {
		t.Fatal(err)
	}

	at := time.Now().UTC()

	c := models.Conversion{
		ID:        ulid.NewGenerator().At(at),
		UserID:    userID,
		Tool:      "pixels-to-svg",
		Status:    string(status),
		Options:   orm.JSON[json.RawMessage]{V: json.RawMessage(`{"mode":"rect"}`)},
		BytesIn:   1234,
		ExpiresAt: at.Add(time.Hour),
	}

	if status.Final() {
		c.Progress = 100
		c.FinishedAt.V, c.FinishedAt.Valid = at, true
	}

	if err := orm.Create(t.Context(), db, &c); err != nil {
		t.Fatalf("seed a conversion: %v", err)
	}

	return c
}

// userIDOf returns the id the auth layer uses for an email, which is what
// scopes every conversion query.
func userIDOf(t *testing.T, app *vukemtest.App, email string) string {
	t.Helper()

	db, err := app.DB()
	if err != nil {
		t.Fatal(err)
	}

	u, err := orm.QueryOf[models.User](db).Where("email", strings.ToLower(email)).First(t.Context())
	if err != nil {
		t.Fatalf("find %s: %v", email, err)
	}

	return u.AuthID()
}

// The catalogue is public on purpose: a client should be able to learn the
// tools and the error codes before it has a key, and neither says anything
// about an account.
func TestTheCatalogueIsPublic(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)

	app.Client(t).Get("/api/v1/tools").AssertOK()

	r := app.Client(t).Get("/api/v1/conversion-errors").AssertOK()

	data, _ := r.JSONPath("data")

	list, ok := data.([]any)
	if !ok {
		t.Fatalf("data is %T, want a list", data)
	}

	if len(list) != len(jobs.Catalog()) {
		t.Fatalf("the endpoint lists %d codes, the taxonomy has %d", len(list), len(jobs.Catalog()))
	}

	// Every code must carry the sentence a client is meant to show. A code
	// with no message would leave a client inventing its own wording.
	for i, entry := range list {
		e, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("entry %d is %T", i, entry)
		}

		if e["type"] == "" || e["message"] == "" {
			t.Errorf("entry %d has an empty type or message: %v", i, e)
		}
	}
}

// Reading, cancelling and streaming all need a key. Without one the answer is
// 401, not an empty list.
func TestTheConversionRoutesNeedAKey(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)
	id := ulid.NewGenerator().New()

	for _, path := range []string{
		"/api/v1/conversions",
		"/api/v1/conversions/" + id,
		"/api/v1/conversions/" + id + "/events",
	} {
		app.Client(t).Get(path).AssertStatus(http.StatusUnauthorized)
	}

	app.Client(t).Delete("/api/v1/conversions/" + id).AssertStatus(http.StatusUnauthorized)
}

// A listing is scoped to the caller and is an empty array, never null: a
// client should not have to handle both shapes.
func TestAListingIsScopedToTheCallerAndNeverNull(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)
	key := issueKey(t, app, "lister@example.com", "image:convert")
	mine := userIDOf(t, app, "lister@example.com")

	empty := app.Client(t).WithHeader("Authorization", "Bearer "+key).Get("/api/v1/conversions").AssertOK()

	data, _ := empty.JSONPath("data")
	if list, ok := data.([]any); !ok || len(list) != 0 {
		t.Fatalf("data = %#v, want an empty list", data)
	}

	seedConversion(t, app, mine, jobs.Finished)
	seedConversion(t, app, "99999", jobs.Finished) // somebody else's

	r := app.Client(t).WithHeader("Authorization", "Bearer "+key).Get("/api/v1/conversions").AssertOK()

	data, _ = r.JSONPath("data")

	list, ok := data.([]any)
	if !ok {
		t.Fatalf("data is %T", data)
	}

	if len(list) != 1 {
		t.Fatalf("the listing has %d conversions, want only the caller's 1", len(list))
	}
}

// Another account's conversion is answered exactly like one that does not
// exist, so an id cannot be probed for existence.
func TestAnotherAccountsConversionIsAPlainMiss(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)
	key := issueKey(t, app, "nosy@example.com", "image:convert")

	theirs := seedConversion(t, app, "99999", jobs.Running)
	missing := ulid.NewGenerator().New()

	for _, id := range []string{theirs.ID, missing} {
		app.Client(t).WithHeader("Authorization", "Bearer "+key).
			Get("/api/v1/conversions/" + id).
			AssertStatus(http.StatusNotFound)
	}
}

// The response must never carry error_detail: it can hold a path or an engine
// command line, and it is the owner's debugging aid, not the client's.
func TestTheResponseNeverCarriesTheOwnerOnlyDetail(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)
	key := issueKey(t, app, "detail@example.com", "image:convert")
	mine := userIDOf(t, app, "detail@example.com")

	c := seedConversion(t, app, mine, jobs.Running)

	db, err := app.DB()
	if err != nil {
		t.Fatal(err)
	}

	const secret = "/srv/work/01JABC/input.psd"

	if _, err := orm.QueryOf[models.Conversion](db).Where("id", c.ID).Update(t.Context(), orm.Set{
		"status":       string(jobs.Errored),
		"error_type":   string(jobs.EngineFailure),
		"error_detail": "vips " + secret + ": exit status 1",
	}); err != nil {
		t.Fatal(err)
	}

	r := app.Client(t).WithHeader("Authorization", "Bearer "+key).
		Get("/api/v1/conversions/" + c.ID).
		AssertOK()

	body := r.Body()
	if strings.Contains(body, secret) {
		t.Fatalf("the response leaked the owner-only detail:\n%s", body)
	}

	if !strings.Contains(body, string(jobs.EngineFailure)) {
		t.Fatalf("the response does not carry the error code:\n%s", body)
	}

	// The sentence to show the user comes from the taxonomy, so the API and
	// the interface cannot word the same failure differently.
	if !strings.Contains(body, jobs.Describe(jobs.EngineFailure).Message) {
		t.Fatalf("the response does not carry the taxonomy's message:\n%s", body)
	}
}

// Cancelling a running conversion returns it cancelled; cancelling one that
// already ended is a 409, not a silent success.
func TestCancelling(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)
	key := issueKey(t, app, "canceller@example.com", "image:convert")
	mine := userIDOf(t, app, "canceller@example.com")

	running := seedConversion(t, app, mine, jobs.Running)

	r := app.Client(t).WithHeader("Authorization", "Bearer "+key).
		Delete("/api/v1/conversions/" + running.ID).
		AssertOK()

	if status, _ := r.JSONPath("status"); status != string(jobs.Cancelled) {
		t.Fatalf("status = %v, want cancelled", status)
	}

	// Again: it has ended now, so it is no longer cancellable.
	app.Client(t).WithHeader("Authorization", "Bearer "+key).
		Delete("/api/v1/conversions/" + running.ID).
		AssertStatus(http.StatusConflict)

	finished := seedConversion(t, app, mine, jobs.Finished)

	app.Client(t).WithHeader("Authorization", "Bearer "+key).
		Delete("/api/v1/conversions/" + finished.ID).
		AssertStatus(http.StatusConflict)
}

// stream opens the progress stream and returns the frames it received. The
// request runs against the app's own handler so the whole middleware stack is
// in place; the context bounds it, because a stream for a live conversion
// never ends by itself.
func stream(t *testing.T, app *vukemtest.App, key, id, lastEventID string, wait time.Duration) []string {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), wait)
	defer cancel()

	req := httptest.NewRequestWithContext(ctx, http.MethodGet, routes.StreamPath, nil)
	req.URL.Path = "/api/v1/conversions/" + id + "/events"
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "text/event-stream")

	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}

	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("stream returned %d:\n%s", rec.Code, rec.Body.String())
	}

	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}

	var frames []string

	sc := bufio.NewScanner(strings.NewReader(rec.Body.String()))
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); strings.HasPrefix(line, "data:") {
			frames = append(frames, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}

	return frames
}

// A client that connects after the work is done must still learn the outcome.
// Sending the current state first, and then ending the stream, is what stops
// it waiting on an event that will never be published.
func TestTheStreamSendsTheCurrentStateAndEndsForAFinishedConversion(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)
	key := issueKey(t, app, "streamer@example.com", "image:convert")
	mine := userIDOf(t, app, "streamer@example.com")

	c := seedConversion(t, app, mine, jobs.Finished)

	frames := stream(t, app, key, c.ID, "", 10*time.Second)
	if len(frames) == 0 {
		t.Fatal("the stream sent nothing")
	}

	var first jobs.Event
	if err := json.Unmarshal([]byte(frames[0]), &first); err != nil {
		t.Fatalf("the first frame is not a progress event: %v\n%s", err, frames[0])
	}

	if first.Status != jobs.Finished {
		t.Fatalf("the first frame says %q, want finished", first.Status)
	}

	if first.ConversionID != c.ID {
		t.Fatalf("the first frame names %q, want %q", first.ConversionID, c.ID)
	}
}

// The stream is authorised before it is opened: a conversion that is not the
// caller's must be a 404, not an open stream with nothing in it. An empty
// stream looks like a conversion that simply has not started.
func TestTheStreamRefusesAnotherAccountsConversion(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)
	key := issueKey(t, app, "peeper@example.com", "image:convert")

	theirs := seedConversion(t, app, "99999", jobs.Running)

	app.Client(t).WithHeader("Authorization", "Bearer "+key).
		Get("/api/v1/conversions/" + theirs.ID + "/events").
		AssertStatus(http.StatusNotFound)
}

// A reconnecting client replays the trail after its Last-Event-ID, so it
// misses nothing while it was away.
func TestTheStreamReplaysTheTrailAfterLastEventID(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)
	key := issueKey(t, app, "resumer@example.com", "image:convert")
	mine := userIDOf(t, app, "resumer@example.com")

	c := seedConversion(t, app, mine, jobs.Finished)

	db, err := app.DB()
	if err != nil {
		t.Fatal(err)
	}

	var eventIDs []int64

	for i, step := range []struct {
		status   jobs.Status
		progress int
		phase    string
	}{
		{jobs.Running, 10, "decoding"},
		{jobs.Running, 70, "tracing"},
		{jobs.Finished, 100, ""},
	} {
		e := models.ConversionEvent{
			ConversionID: c.ID,
			Status:       string(step.status),
			Progress:     step.progress,
			Phase:        step.phase,
			CreatedAt:    time.Now().UTC().Add(time.Duration(i) * time.Second),
		}

		if err := orm.Create(t.Context(), db, &e); err != nil {
			t.Fatal(err)
		}

		eventIDs = append(eventIDs, e.ID)
	}

	// Without Last-Event-ID: the snapshot plus the whole trail.
	all := stream(t, app, key, c.ID, "", 10*time.Second)
	if len(all) != 1+len(eventIDs) {
		t.Fatalf("a fresh stream sent %d frames, want %d", len(all), 1+len(eventIDs))
	}

	// With it: the snapshot plus only what came after.
	resumed := stream(t, app, key, c.ID, strconv.FormatInt(eventIDs[0], 10), 10*time.Second)
	if len(resumed) != 1+len(eventIDs)-1 {
		t.Fatalf("a resumed stream sent %d frames, want %d", len(resumed), len(eventIDs))
	}

	var got jobs.Event
	if err := json.Unmarshal([]byte(resumed[1]), &got); err != nil {
		t.Fatal(err)
	}

	if got.ID != eventIDs[1] {
		t.Fatalf("the replay restarted at event %d, want %d", got.ID, eventIDs[1])
	}
}

// A malformed id is a 404, decided before any database work, and is answered
// exactly like an id that does not exist.
func TestAMalformedConversionIDIsNotFound(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)
	key := issueKey(t, app, "prober@example.com", "image:convert")

	for _, id := range []string{"1", "not-an-id", "01JQ8Z5X0K7M3N9P2R4T6V8W0"} {
		app.Client(t).WithHeader("Authorization", "Bearer "+key).
			Get("/api/v1/conversions/" + id).
			AssertStatus(http.StatusNotFound)
	}
}
