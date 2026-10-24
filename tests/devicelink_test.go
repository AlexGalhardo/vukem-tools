package tests

import (
	"reflect"
	"slices"
	"testing"

	"github.com/AlexGalhardo/vukem/vukemtest"

	"github.com/AlexGalhardo/vukem-tools/app/abilities"
	"github.com/AlexGalhardo/vukem-tools/bootstrap"
	"github.com/AlexGalhardo/vukem-tools/internal/bot"
)

// startLink asks for a link the way the bot does and returns the two codes.
func startLink(t *testing.T, app *vukemtest.App) (device, user string) {
	t.Helper()

	r := app.Client(t).PostJSON("/api/v1/device-links", map[string]any{"client": "telegram", "device_name": "@ana"})
	r.AssertStatus(201)

	d, _ := r.JSONPath("device_code")
	u, _ := r.JSONPath("user_code")

	device, _ = d.(string)
	user, _ = u.(string)

	if device == "" || user == "" {
		t.Fatalf("the link has no codes: %v %v", d, u)
	}

	return device, user
}

func pollLink(t *testing.T, app *vukemtest.App, device string) *vukemtest.Response {
	t.Helper()

	return app.Client(t).PostJSON("/api/v1/device-links/token", map[string]any{"device_code": device})
}

// signedIn returns a browser session for a verified account.
func signedIn(t *testing.T, app *vukemtest.App, email string) *vukemtest.Client {
	t.Helper()

	createAPIUser(t, app, email, true)

	web := app.Client(t).Inertia("").WithHeader("Referer", "/login")
	web.PostJSON("/login", map[string]any{"email": email, "password": "correct horse battery"}).
		AssertRedirect("/dashboard")

	return web
}

func TestAProgramGetsAKeyOnlyAfterASignedInPersonApproves(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)
	web := signedIn(t, app, "ana@example.com")

	device, user := startLink(t, app)

	// Until somebody decides, the program has nothing.
	pollLink(t, app, device).AssertJSONPath("status", "pending")

	// The page shows what is being asked, and the abilities are the server's.
	web.Get("/account/link?code="+user).AssertInertia("Account/Link").PropEquals("found", true).HasProp("abilities")

	web.PostJSON("/account/link", map[string]any{"code": user, "decision": "approve"}).AssertRedirect("/account")

	approved := pollLink(t, app, device)
	approved.AssertJSONPath("status", "approved")

	raw, _ := approved.JSONPath("token")
	token, _ := raw.(string)

	if token == "" {
		t.Fatal("an approved link handed over no key")
	}

	// The key is the person's own, and carries exactly what the bot's menus use.
	key := app.Client(t).WithToken(token)
	key.GetJSON("/api/v1/me").AssertOK().AssertJSONPath("email", "ana@example.com")
	key.GetJSON("/api/v1/usage").AssertOK().AssertJSONPath("spent_seconds", float64(0))

	listed := key.GetJSON("/api/v1/tokens").AssertOK()

	var body struct {
		Data []struct {
			Name      string   `json:"name"`
			Abilities []string `json:"abilities"`
		} `json:"data"`
	}

	listed.JSON(&body)

	found := false

	for _, k := range body.Data {
		if k.Name == "Telegram: @ana" {
			found = true

			if !slices.Equal(
				slices.Sorted(slices.Values(k.Abilities)),
				slices.Sorted(slices.Values(abilities.Telegram())),
			) {
				t.Errorf("the key carries %v, want the Telegram set %v", k.Abilities, abilities.Telegram())
			}

			if slices.Contains(k.Abilities, abilities.PDFSecurity) {
				t.Error("the bot's key can decrypt documents")
			}
		}
	}

	if !found {
		t.Errorf("the key is not in the account's key list under the bot's label: %+v", body.Data)
	}

	// Spent: the same codes give nothing again, so one approval is one key.
	pollLink(t, app, device).AssertJSONPath("status", "expired")
	web.Get("/account/link?code="+user).AssertInertia("Account/Link").PropEquals("found", false)
}

func TestRefusingALinkGivesNoKey(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)
	web := signedIn(t, app, "ana@example.com")

	device, user := startLink(t, app)

	web.PostJSON("/account/link", map[string]any{"code": user, "decision": "deny"}).AssertRedirect("/account")

	pollLink(t, app, device).AssertJSONPath("status", "denied")
	vukemtest.AssertDatabaseCount(t, app, "access_tokens", 0)
}

func TestAnAnonymousVisitorCannotApproveALink(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)
	_, user := startLink(t, app)

	anon := app.Client(t).Inertia("")
	anon.Get("/account/link?code=" + user).AssertRedirect("/login")
	anon.PostJSON("/account/link", map[string]any{"code": user, "decision": "approve"}).AssertRedirect("/login")
}

func TestAnUnverifiedAccountCannotConnectAProgram(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)
	createAPIUser(t, app, "squatter@example.com", false)

	web := app.Client(t).Inertia("").WithHeader("Referer", "/login")
	web.PostJSON("/login", map[string]any{"email": "squatter@example.com", "password": "correct horse battery"}).
		AssertRedirect("/dashboard")

	device, user := startLink(t, app)

	web.PostJSON("/account/link", map[string]any{"code": user, "decision": "approve"}).AssertRedirect("/login")
	pollLink(t, app, device).AssertJSONPath("status", "pending")
	vukemtest.AssertDatabaseCount(t, app, "access_tokens", 0)
}

func TestGuessedCodesAndUnknownClientsGetNothing(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)
	web := signedIn(t, app, "ana@example.com")

	pollLink(t, app, "not-a-real-device-code").AssertJSONPath("status", "expired")

	web.PostJSON("/account/link", map[string]any{"code": "BCDF-2346", "decision": "approve"}).AssertStatus(303)
	vukemtest.AssertDatabaseCount(t, app, "access_tokens", 0)

	app.Client(t).PostJSON("/api/v1/device-links", map[string]any{"client": "mainframe"}).AssertStatus(422)
}

func TestUsageReportsTheBudget(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)
	token := apiUser(t, app, "ana@example.com")

	app.Client(t).GetJSON("/api/v1/usage").AssertUnauthorized()
	usage := app.Client(t).
		WithToken(token).
		GetJSON("/api/v1/usage").
		AssertOK().
		AssertJSONPath("spent_seconds", float64(0))

	if resets, _ := usage.JSONPath("resets_at"); resets == nil {
		t.Error("usage does not say when the budget resets")
	}
}

// The link operations are the only public calls the bot makes, and the test
// says so: a method added to bot.Links must be an operation that really is
// public, and anything else the bot calls must be behind the guard.
func TestTheBotsPublicCallsAreExactlyTheLinkOperations(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)
	operations := apiOperations(t, app)

	for method := range reflect.TypeFor[bot.Links]().Methods() {
		op, ok := operations[method.Name]
		if !ok {
			t.Errorf("the bot calls %s, which no route serves", method.Name)

			continue
		}

		if op.secured {
			t.Errorf("%s is guarded, so the bot could never call it before it has a key", method.Name)
		}
	}
}
