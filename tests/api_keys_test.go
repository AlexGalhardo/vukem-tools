package tests

import (
	"net/http"
	"testing"

	vukem "github.com/AlexGalhardo/vukem"
	"github.com/AlexGalhardo/vukem/auth"
	"github.com/AlexGalhardo/vukem/router"
	"github.com/AlexGalhardo/vukem/vukemtest"
	"github.com/AlexGalhardo/vukem/web"

	"github.com/AlexGalhardo/vukem-tools/app/abilities"
	"github.com/AlexGalhardo/vukem-tools/app/accounts"
	"github.com/AlexGalhardo/vukem-tools/bootstrap"
	"github.com/AlexGalhardo/vukem-tools/internal/httpx"
)

// issue asks for a key with the given abilities and returns the plaintext.
func issueKey(t *testing.T, app *vukemtest.App, email string, grant ...string) string {
	t.Helper()

	createAPIUser(t, app, email, true)

	r := app.Client(t).PostJSON("/api/v1/tokens", map[string]any{
		"email": email, "password": "correct horse battery",
		"device_name": "cli", "abilities": grant,
	}).AssertCreated()

	tok, _ := r.JSONPath("token")
	s, _ := tok.(string)

	if s == "" {
		t.Fatal("no plaintext key in the response")
	}

	return s
}

// A key says what it may do. No abilities, an unknown ability and the wildcard
// are all refused, so a key can never be issued with more authority than the
// caller asked for.
func TestKeyAbilitiesAreRequiredAndClosed(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)
	createAPIUser(t, app, "scoped@example.com", true)

	cases := map[string]any{
		"missing":  nil,
		"empty":    []string{},
		"unknown":  []string{"image:convert", "video:delete-everything"},
		"wildcard": []string{"*"},
	}

	for name, abil := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			body := map[string]any{
				"email": "scoped@example.com", "password": "correct horse battery",
				"device_name": "cli",
			}
			if abil != nil {
				body["abilities"] = abil
			}

			app.Client(t).PostJSON("/api/v1/tokens", body).AssertJSONValidationErrors("abilities")
		})
	}

	vukemtest.AssertDatabaseCount(t, app, "access_tokens", 0)
}

// The issued key carries exactly the abilities requested, and listing keys
// never returns a secret.
func TestKeyCarriesRequestedAbilitiesAndListingHidesSecrets(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)
	key := issueKey(t, app, "dev@example.com", abilities.ImageConvert, abilities.PixelsSVG)

	api := app.Client(t).WithToken(key)

	r := api.GetJSON("/api/v1/tokens").AssertOK()
	r.AssertJSONPath("data.0.abilities.0", abilities.ImageConvert)
	r.AssertJSONPath("data.0.abilities.1", abilities.PixelsSVG)

	// Whatever the listing contains, it is not the key itself.
	r.AssertDontSee(key)
}

// The ability list is published, so a client does not hardcode it.
func TestAbilitiesAreDiscoverable(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)

	r := app.Client(t).GetJSON("/api/v1/abilities").AssertOK()
	r.AssertJSONCount("data", len(abilities.All()))
	r.AssertJSONPath("data.0", abilities.All()[0])
}

// X-API-Key authenticates like Authorization: Bearer, and Authorization wins
// when both are present so a stale key cannot override a deliberate one.
func TestAPIKeyHeaderIsAccepted(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)
	key := issueKey(t, app, "header@example.com", abilities.ImageConvert)

	app.Client(t).WithHeader(httpx.APIKeyHeader, key).
		GetJSON("/api/v1/me").AssertOK().AssertJSONPath("email", "header@example.com")

	// Garbage in X-API-Key is ignored while Authorization carries a real key.
	app.Client(t).WithToken(key).WithHeader(httpx.APIKeyHeader, "not-a-key").
		GetJSON("/api/v1/me").AssertOK()

	// Whitespace is not a token.
	app.Client(t).WithHeader(httpx.APIKeyHeader, "   ").GetJSON("/api/v1/me").AssertUnauthorized()
}

// Revoking by id is scoped to the caller: another user's id revokes nothing and
// is answered exactly like an id that does not exist.
func TestRevokeByIDIsScopedToTheOwner(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)

	mine := issueKey(t, app, "mine@example.com", abilities.ImageConvert)
	theirs := issueKey(t, app, "theirs@example.com", abilities.ImageConvert)

	acc, err := accounts.New(app.App)
	if err != nil {
		t.Fatal(err)
	}

	theirToken, err := acc.Tokens.Find(t.Context(), theirs)
	if err != nil {
		t.Fatal(err)
	}

	// Attacker holds a valid key and guesses the victim's token id.
	app.Client(t).WithToken(mine).Delete("/api/v1/tokens/" + theirToken.ID).AssertNotFound()

	// The victim's key still works.
	app.Client(t).WithToken(theirs).GetJSON("/api/v1/me").AssertOK()

	// Revoking an id that does not exist is answered the same way, so the
	// response does not say whether the id was real.
	app.Client(t).WithToken(mine).Delete("/api/v1/tokens/does-not-exist").AssertNotFound()

	// Revoking the caller's own key works, once.
	myToken, err := acc.Tokens.Find(t.Context(), mine)
	if err != nil {
		t.Fatal(err)
	}

	app.Client(t).WithToken(mine).Delete("/api/v1/tokens/" + myToken.ID).AssertNoContent()
	app.Client(t).WithToken(mine).GetJSON("/api/v1/me").AssertUnauthorized()
}

// A key is refused on a route whose ability it does not carry. The route is
// declared for this test only: no tool exists yet, and the gate has to be
// proven before one relies on it.
func TestKeyWithoutTheAbilityIsRefused(t *testing.T) {
	t.Parallel()

	guarded := func(r *router.Router, app *vukem.App) error {
		acc, err := accounts.New(app)
		if err != nil {
			return err
		}

		r.Group("/api/v1", func(g *router.Group) {
			g.Use(httpx.AcceptAPIKeyHeader, auth.Required(acc.API))
			g.Get("/test-only/convert", func(c *web.Ctx) error {
				return c.JSON(http.StatusOK, map[string]any{"ok": true})
			}).Use(auth.Ability(acc.API, abilities.ImageConvert))
		})

		return nil
	}

	app := vukemtest.New(t, bootstrap.New, vukemtest.Options{
		AppOptions: []vukem.Option{vukem.WithRoutes(guarded)},
	})

	allowed := issueKey(t, app, "allowed@example.com", abilities.ImageConvert)
	denied := issueKey(t, app, "denied@example.com", abilities.PixelsSVG)

	app.Client(t).WithToken(allowed).GetJSON("/api/v1/test-only/convert").AssertOK()
	app.Client(t).WithToken(denied).GetJSON("/api/v1/test-only/convert").AssertForbidden()
	app.Client(t).GetJSON("/api/v1/test-only/convert").AssertUnauthorized()
}
