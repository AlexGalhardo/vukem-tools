package tests

import (
	"testing"

	"github.com/AlexGalhardo/vukem/vukemtest"

	"github.com/AlexGalhardo/vukem-tools/bootstrap"
)

func TestHome(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)

	// First visit: HTML with the page object; Inertia visits: JSON.
	app.Client(t).Get("/").AssertOK().AssertInertia("Home").HasProp("appName")
	app.Client(t).Inertia("").Get("/").AssertOK().AssertInertia("Home")
	app.Client(t).Get("/up").AssertOK()
}

func TestErrorPage(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)

	// A missing page is a redirect home now, so the error page is exercised
	// through an error that is not a missing page. The API keeps its real
	// status, which is what makes it usable here.
	app.Client(t).Inertia("").Get("/api/v1/no-such-route").AssertStatus(404).
		AssertInertia("Error").
		PropEquals("status", 404).
		PropEquals("message", "Not Found")
	app.Client(t).Get("/api/v1/no-such-route").AssertStatus(404).AssertHeader("X-Inertia", "")

	// And the missing page itself goes home rather than rendering anything.
	app.Client(t).Get("/no-such-page").AssertRedirect("/")
}

// A health probe must not create a session. The framework stopped persisting
// one for read-only visits, and production confirms it (GET /up answers with no
// Set-Cookie), but nothing in this repository asserted it: a future change to
// the middleware stack could start writing a session row per probe, which is a
// cheap way to fill the store.
func TestHealthProbeCreatesNoSession(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)

	app.Client(t).Get("/up").AssertOK().AssertHeaderMissing("Set-Cookie")

	// A second probe must not start one either.
	app.Client(t).Get("/up").AssertOK().AssertHeaderMissing("Set-Cookie")
}
