package tests

import (
	"net/http"
	"strings"
	"testing"

	"github.com/AlexGalhardo/vukem/vukemtest"

	"github.com/AlexGalhardo/vukem-tools/bootstrap"
)

// The app's own pages call /api/v1 with the session they already have. A
// token-only guard answered 401 to every signed-in browser, which is how
// every tool came to be unusable from the UI while the API worked.
func TestTheAppsOwnPagesCanCallTheAPIWithTheirSession(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)
	user := verifiedUser(t, app)

	res := app.Client(t).ActingAs("web", user).
		WithHeader("Accept", "application/json").
		Get("/api/v1/me")

	if res.Status() == http.StatusUnauthorized {
		t.Fatalf("a signed-in session was refused by the API:\n%.300s", res.Body())
	}

	res.AssertOK()

	if body := res.Body(); !strings.Contains(body, user.Email) {
		t.Errorf("GET /api/v1/me did not answer for the session's account:\n%.300s", body)
	}
}

// Signed out it is still 401, and still a problem document rather than a
// redirect to the login page: an API client parses the body.
func TestTheAPIStillRefusesAnAnonymousCaller(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)

	res := app.Client(t).WithHeader("Accept", "application/json").Get("/api/v1/me")

	res.AssertStatus(http.StatusUnauthorized)

	if body := res.Body(); !strings.Contains(body, "Unauthenticated") {
		t.Errorf("want a problem document, got:\n%.300s", body)
	}
}

// A key is still held to its abilities. Accepting the session must not turn
// every key into a wildcard: requireAbility only skips the check when there
// is no token, and this proves the token path still runs.
func TestAKeyIsStillHeldToItsAbilities(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)

	// A key for one tool must not start another.
	key := issueKey(t, app, "keyed@example.com", "pixels:svg")

	res := app.Client(t).
		WithHeader("Authorization", "Bearer "+key).
		WithHeader("Accept", "application/json").
		PostJSON("/api/v1/tools/image-convert", map[string]any{
			"file_id": "01M58X4WQHF2R9JTQW25VX3GE0",
			"options": map[string]any{"to": "jpeg"},
		})

	if res.Status() == http.StatusOK || res.Status() == http.StatusAccepted {
		t.Fatalf("a key without the ability started a conversion (status %d)", res.Status())
	}

	if res.Status() == http.StatusUnauthorized {
		t.Fatalf("the key was not authenticated at all (401), so the ability check never ran:\n%.300s", res.Body())
	}
}
