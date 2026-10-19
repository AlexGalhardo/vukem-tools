package tests

import (
	"net/http"
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

// Every live tool must render its page. This is the test that was missing:
// the PDF pages were added to the map with no props function, Tool called it
// unconditionally, and all 18 answered 500 in production while the suite
// stayed green because nothing asked for them.
func TestEveryLiveToolRendersItsPage(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)
	user := verifiedUser(t, app)
	client := app.Client(t).ActingAs("web", user)

	// The home page is the tool index, so its props are exactly what a
	// visitor can click. Reading them keeps this test honest without
	// reaching into the pipeline's wiring.
	groups, ok := client.Get("/").AssertOK().AssertInertia("Home").Prop("groups")
	if !ok {
		t.Fatal("the home page does not list the tool groups")
	}

	live := 0

	for _, g := range groups.([]any) {
		group, _ := g.(map[string]any)

		tools, _ := group["tools"].([]any)
		for _, item := range tools {
			tool, _ := item.(map[string]any)
			if tool["status"] != "live" {
				continue
			}

			live++

			name, _ := tool["name"].(string)

			res := client.Get("/tools/" + name)
			if res.Status() != http.StatusOK {
				t.Errorf("GET /tools/%s = %d, want 200: a live tool must have a page that renders",
					name, res.Status())
			}
		}
	}

	if live == 0 {
		t.Fatal("no live tool in the catalogue, so this test proved nothing")
	}

	t.Logf("checked %d live tools", live)
}
