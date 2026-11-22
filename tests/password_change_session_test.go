package tests

import (
	"testing"

	"github.com/AlexGalhardo/vukem/vukemtest"

	"github.com/AlexGalhardo/vukem-tools/bootstrap"
)

// The account page promises that changing the password signs out every other
// session and "this one stays signed in". The version bump that ends the
// others also ended the session that made the change, so the person landed on
// the login page with a password already changed (QA-A16).
func TestChangingThePasswordKeepsTheSessionThatChangedIt(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)
	const email = "keep@example.com"

	here := app.Client(t).Inertia("")
	here.PostJSON("/register", signUpForm("Keep Me", email, testPassword)).AssertRedirect("/register/check-email")
	here.PostJSON("/login", map[string]any{"email": email, "password": testPassword}).AssertRedirect("/dashboard")

	elsewhere := app.Client(t).Inertia("")
	elsewhere.PostJSON("/login", map[string]any{"email": email, "password": testPassword}).AssertRedirect("/dashboard")

	here.PutJSON("/account/password", map[string]any{
		"current_password":      testPassword,
		"password":              otherPassword,
		"password_confirmation": otherPassword,
	}).AssertRedirect("/account")

	here.Get("/account").AssertOK()
	elsewhere.Get("/account").AssertRedirect("/login")
}
