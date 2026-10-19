package tests

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/AlexGalhardo/vukem/vukemtest"

	"github.com/AlexGalhardo/vukem-tools/bootstrap"
)

// With PASSWORD_AUTH=off the password routes are not registered at all, so
// there is no endpoint to probe and no form that can post to one. Hiding the
// fields in the frontend protects nothing on its own, which is why this
// asserts the server and not the page.
func TestPasswordAuthOffLeavesNoPasswordEndpoint(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New, vukemtest.Options{
		Env: map[string]string{"PASSWORD_AUTH": "off"},
	})

	// The sign-in page stays: it is where the Google button lives.
	app.Client(t).Get("/login").AssertOK()

	for _, tc := range []struct {
		method, path string
	}{
		{http.MethodPost, "/login"},
		{http.MethodGet, "/register"},
		{http.MethodPost, "/register"},
		{http.MethodGet, "/register/check-email"},
		{http.MethodGet, "/forgot-password"},
		{http.MethodPost, "/forgot-password"},
		{http.MethodPost, "/reset-password"},
		{http.MethodGet, "/email/verify"},
	} {
		res := app.Client(t).Do(tc.method, tc.path, nil, "")

		// 404 or 405: the route is absent, not refused. Anything that reaches
		// a handler (200, 302, 403, 422) means the gate leaked.
		if s := res.Status(); s != http.StatusNotFound && s != http.StatusMethodNotAllowed {
			t.Errorf("%s %s = %d, want the route not to exist", tc.method, tc.path, s)
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

	app.Client(t).Get("/register").AssertOK()
}
