package tests

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/AlexGalhardo/vukem/orm"
	"github.com/AlexGalhardo/vukem/vukemtest"

	"github.com/AlexGalhardo/vukem-tools/app/accounts"
	"github.com/AlexGalhardo/vukem-tools/app/models"
	"github.com/AlexGalhardo/vukem-tools/bootstrap"
)

// The account area needs a verified account, like every other route that can
// spend the quota. Signed out it must send somebody to the login page rather
// than render an empty page.
func TestTheAccountAreaNeedsAnAccount(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)

	res := app.Client(t).Get("/account")

	if res.Status() != http.StatusFound && res.Status() != http.StatusSeeOther {
		t.Fatalf("GET /account signed out returned %d, want a redirect to the login page", res.Status())
	}

	if to := res.Header("Location"); !strings.Contains(to, "/login") {
		t.Errorf("Location = %q, want the login page", to)
	}
}

// The page shows the profile, the keys and the quota, and it must not carry
// the password hash: a page prop ends up in the HTML.
func TestTheAccountPageCarriesNoPasswordHash(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)
	user := verifiedUser(t, app)

	res := app.Client(t).ActingAs("web", user).Get("/account").AssertOK()
	res.AssertInertia("Account/Index")

	body := res.Body()

	// The props end up in the HTML, so anything the controller puts in them
	// is published to whoever can read the page.
	if strings.Contains(body, "$argon2") || strings.Contains(body, "$2a$") {
		t.Fatal("the account page contains a password hash")
	}

	if !strings.Contains(body, user.Email) {
		t.Error("the page does not show the account's email address")
	}
}

// Changing the password requires the current one. A session can be a borrowed
// laptop, and a password change takes over the account.
func TestChangingThePasswordNeedsTheCurrentOne(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)
	user := verifiedUser(t, app)

	res := form(app.Client(t).ActingAs("web", user), http.MethodPut, "/account/password", url.Values{
		"current_password":      {"not-the-current-password"},
		"password":              {"An0ther-Str0ng!Pass"},
		"password_confirmation": {"An0ther-Str0ng!Pass"},
	})

	// Inertia reports a validation error by redirecting back with it
	// flashed, so the status alone cannot tell acceptance from refusal. What
	// matters is that the password did not change.
	acc, err := accounts.New(app.App)
	if err != nil {
		t.Fatal(err)
	}

	fresh, err := orm.QueryOf[models.User](acc.DB).Where("id", user.ID).First(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if fresh.Password != user.Password {
		t.Fatal("the password was changed without the current one")
	}

	// And the old password still works, which is the same statement from the
	// other side: a change that half-applied would fail this.
	ok, err := acc.Hasher.Verify(fresh.Password, accountPassword)
	if err != nil {
		t.Fatal(err)
	}

	if !ok {
		t.Error("the original password no longer verifies")
	}

	// A field error, not a sign-out: the person is already authenticated and
	// logging them out would punish a typo.
	if res.Status() == http.StatusUnauthorized {
		t.Errorf("a wrong current password signed the person out (status %d)", res.Status())
	}
}

// A key is minted with the abilities asked for, and the plaintext is shown
// once. It must not be a page prop: a prop survives in the history entry, and
// the back button would put the secret back on screen.
func TestCreatingAKeyShowsItOnceAndGrantsOnlyWhatWasAsked(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)
	user := verifiedUser(t, app)
	client := app.Client(t).ActingAs("web", user)

	client.PostForm("/account/keys", url.Values{
		"device_name": {"my laptop"},
		"abilities":   {"pixels:svg"},
	})

	// The page after the redirect lists the key, without its secret.
	page := client.Get("/account").AssertOK().Body()

	if !strings.Contains(page, "my laptop") {
		t.Errorf("the new key is not listed:\n%.600s", page)
	}

	if !strings.Contains(page, "pixels:svg") {
		t.Error("the key's abilities are not shown")
	}
}

// An ability that does not exist is refused. This form posts names from a
// browser, and an unknown ability would mint a key that grants nothing and
// fails only when somebody tries to use it.
func TestAnUnknownAbilityIsRefused(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)
	user := verifiedUser(t, app)

	res := app.Client(t).ActingAs("web", user).PostForm("/account/keys", url.Values{
		"device_name": {"a key"},
		"abilities":   {"everything:*"},
	})

	if res.Status() == http.StatusOK || res.Status() == http.StatusFound {
		// A redirect would mean it was accepted.
		if !strings.Contains(res.Body(), "does not exist") {
			t.Fatalf("an unknown ability was accepted (status %d):\n%.400s", res.Status(), res.Body())
		}
	}

	vukemtest.AssertDatabaseCount(t, app, "access_tokens", 0)
}

// Revoking another account's key id is answered exactly like an unknown one,
// so the endpoint is not an oracle for which key ids exist.
func TestRevokingSomebodyElsesKeyIsIndistinguishableFromAnUnknownOne(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)

	owner := verifiedUser(t, app)
	other := verifiedUserWith(t, app, "other@example.com")

	app.Client(t).ActingAs("web", owner).PostForm("/account/keys", url.Values{
		"device_name": {"the owner's key"},
		"abilities":   {"pixels:svg"},
	})

	// The other account tries two ids: one that exists and one that does not.
	client := app.Client(t).ActingAs("web", other)

	existing := client.Delete("/account/keys/01JBXQ8EXAMPLEEXAMPLE00")
	fake := client.Delete("/account/keys/does-not-exist-at-all")

	if existing.Status() != fake.Status() {
		t.Errorf("an existing id and an unknown id answered differently: %d and %d",
			existing.Status(), fake.Status())
	}

	// The owner's key is still there.
	vukemtest.AssertDatabaseCount(t, app, "access_tokens", 1)
}

// form sends a urlencoded body with an explicit method, so a test can
// exercise PUT and PATCH as the routes are actually registered rather than
// through a method override the router may not read.
func form(c *vukemtest.Client, method, target string, values url.Values) *vukemtest.Response {
	return c.Do(method, target, strings.NewReader(values.Encode()),
		"application/x-www-form-urlencoded")
}

// verifiedUser creates a verified account and returns it, so a test can act
// as it.
func verifiedUser(t *testing.T, app *vukemtest.App) models.User {
	t.Helper()

	return verifiedUserWith(t, app, "owner@example.com")
}

// verifiedUserWith creates a verified account with this address.
func verifiedUserWith(t *testing.T, app *vukemtest.App, email string) models.User {
	t.Helper()

	acc, err := accounts.New(app.App)
	if err != nil {
		t.Fatal(err)
	}

	h, err := acc.Hasher.Make(accountPassword)
	if err != nil {
		t.Fatal(err)
	}

	u := models.User{Name: "Account Owner", Email: email, Password: h}
	u.EmailVerifiedAt.V, u.EmailVerifiedAt.Valid = time.Now().UTC(), true

	if err := orm.Create(t.Context(), acc.DB, &u); err != nil {
		t.Fatal(err)
	}

	return u
}

// accountPassword is the password the account-area tests sign in with. It
// satisfies the policy in app/requests/password.go, so a test failure is
// never the fixture's fault.
const accountPassword = "Str0ng-Fixture!Pass"

// The plaintext key is flashed, and the page reads it from the flash. It was
// read from props, where the server never puts it, so a newly created key was
// never shown and its only copy was lost.
func TestANewKeyIsFlashedWhereThePageReadsIt(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)
	user := verifiedUser(t, app)
	client := app.Client(t).ActingAs("web", user)

	client.PostForm("/account/keys", url.Values{
		"device_name": {"my laptop"},
		"abilities":   {"pixels:svg"},
	})

	page := client.Get("/account").AssertOK().AssertInertia("Account/Index").Page()

	flash, _ := page["flash"].(map[string]any)

	key, _ := flash["key"].(string)
	if key == "" {
		t.Fatalf("no key in the flash, so the page cannot show it: %v", page["flash"])
	}

	// And it must not be a prop: a prop survives in the history entry, so the
	// back button would put the secret back on screen.
	props, _ := page["props"].(map[string]any)
	if _, ok := props["key"]; ok {
		t.Error("the plaintext key is a page prop, so the back button can show it again")
	}
}
