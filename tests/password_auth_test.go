package tests

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/AlexGalhardo/vukem/vukemtest"

	"github.com/AlexGalhardo/vukem-tools/bootstrap"
)

// With PASSWORD_AUTH=off nothing that accepts a password exists. That is the
// property, and it is asserted on the server: hiding the fields in the
// frontend protects nothing on its own.
//
// The two sign-in PAGES stay (they are where the Google button lives), and
// the pages whose routes are gone answer with the redirect every missing page
// gets. What must not exist is anywhere to post a password.
func TestPasswordAuthOffLeavesNothingToPostAPasswordTo(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New, vukemtest.Options{
		Env: map[string]string{"PASSWORD_AUTH": "off"},
	})

	// The pages a visitor actually lands on still render.
	app.Client(t).Get("/login").AssertOK()
	app.Client(t).Get("/register").AssertOK()

	for _, path := range []string{"/login", "/register", "/forgot-password", "/reset-password"} {
		res := app.Client(t).PostForm(path, url.Values{
			"name":                  {"Mallory"},
			"email":                 {"mallory@example.com"},
			"password":              {testPassword},
			"password_confirmation": {testPassword},
			"token":                 {"whatever"},
		})

		// 404 or 405: the route is absent, not refused. Anything that reaches
		// a handler -- 200, 302, 403, 422 -- means the gate leaked.
		if s := res.Status(); s != http.StatusNotFound && s != http.StatusMethodNotAllowed {
			t.Errorf("POST %s = %d, want the route not to exist", path, s)
		}
	}

	// And the pages behind the flag are gone, which the missing-page redirect
	// then turns into a trip home.
	for _, path := range []string{"/register/check-email", "/forgot-password", "/email/verify"} {
		if got := app.Client(t).Get(path).Status(); got != http.StatusFound {
			t.Errorf("GET %s = %d, want the missing-page redirect", path, got)
		}
	}
}

// Nobody can be created with a password while it is off: the endpoint is
// gone, so the database stays empty rather than gaining an account that
// could never sign in.
func TestPasswordAuthOffCreatesNoAccount(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New, vukemtest.Options{
		Env: map[string]string{"PASSWORD_AUTH": "off"},
	})

	app.Client(t).PostForm("/register", url.Values{
		"name":                  {"Mallory"},
		"email":                 {"mallory@example.com"},
		"password":              {testPassword},
		"password_confirmation": {testPassword},
	})

	vukemtest.AssertDatabaseCount(t, app, "users", 0)
}

// The default is on, so a fresh checkout and every other test behave the way
// the scaffold does. Without this, turning the flag's default around would
// silently disable password sign-in everywhere and only this file would care.
func TestPasswordAuthDefaultsToOn(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)

	// The page renders either way; what the default controls is whether it
	// carries a form. A 404 here would mean the page itself went missing.
	app.Client(t).Get("/register").AssertOK().AssertInertia("Auth/Register").
		PropEquals("passwordAuth", true)
}

// With password sign-in off and no provider credentials there is no way in at
// all. The page must say so: a heading with nothing to click reads as a
// broken deploy and gives nobody a clue what broke. This is the state
// tools.vukem.dev was in after PASSWORD_AUTH went off while
// SOCIAL_GOOGLE_CLIENT_ID was still only in the owner's .env.
func TestNoWayInSaysSo(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New, vukemtest.Options{
		Env: map[string]string{"PASSWORD_AUTH": "off"},
	})

	// The props carry both facts the page branches on, so it can say
	// "unavailable" rather than render an empty form area.
	page := app.Client(t).Get("/login").AssertOK().AssertInertia("Auth/Login")

	page.PropEquals("passwordAuth", false)

	providers, ok := page.Prop("socialProviders")
	if !ok {
		t.Fatal("socialProviders is not shared, so the page cannot tell " +
			`"no provider configured" from "still loading"`)
	}

	if list, _ := providers.([]any); len(list) != 0 {
		t.Errorf("socialProviders = %v, want empty with no credentials configured", list)
	}
}
