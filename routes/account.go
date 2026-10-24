package routes

import (
	"net/http"
	"time"

	"github.com/AlexGalhardo/vukem"
	"github.com/AlexGalhardo/vukem/auth"
	"github.com/AlexGalhardo/vukem/ratelimit"
	"github.com/AlexGalhardo/vukem/router"

	"github.com/AlexGalhardo/vukem-tools/app/accounts"
	"github.com/AlexGalhardo/vukem-tools/app/controllers/account"
	"github.com/AlexGalhardo/vukem-tools/app/devicelink"
	"github.com/AlexGalhardo/vukem-tools/app/models"
	"github.com/AlexGalhardo/vukem-tools/app/services"
)

// Account registers the account area: the profile, the password, the API keys
// and today's quota.
//
// Its own registrar rather than part of Auth, because it needs the quota
// counter and Auth has no reason to know the conversion pipeline exists.
func Account(c *services.Conversions) func(r *router.Router, app *vukem.App) error {
	return func(r *router.Router, app *vukem.App) error {
		return registerAccount(c, r, app)
	}
}

func registerAccount(conversions *services.Conversions, r *router.Router, app *vukem.App) error {
	acc, err := accounts.New(app)
	if err != nil {
		return err
	}

	limiters, err := app.RateLimiters()
	if err != nil {
		return err
	}

	if err := conversions.Resolve(app); err != nil {
		return err
	}

	settings := account.NewSettings(account.New(acc), conversions.Quota())

	appCache, err := app.Cache()
	if err != nil {
		return err
	}

	link := account.NewLink(devicelink.New(appCache.Store(), nil))

	required := auth.Required(acc.Web)

	// Issuing a key is rate limited per account. It is cheap for the server
	// and valuable to an attacker who has a session: without a limit, a
	// stolen session could mint keys faster than the owner could revoke
	// them, and a key outlives the session it was made from.
	minting := ratelimit.Middleware(limiters("account.keys", 10, time.Hour), byUser)

	r.Get("/account", settings.Show).Name("account").Use(required)
	r.Patch("/account/profile", settings.UpdateProfile).Name("account.profile").Use(required)
	// Same unit as the sign-in routes: a Google-only account never set a
	// password, so there is no current password to ask for.
	if PasswordAuthEnabled(app) {
		r.Put("/account/password", settings.UpdatePassword).Name("account.password").Use(required)
	}

	// Where a signed-in person approves a program that asked for a key. The
	// code in the query is the program's, so the page is read-only until the
	// person presses a button, and the decision itself is a POST.
	r.Get("/account/link", link.Show).Name("account.link").Use(required)
	r.Post("/account/link", link.Decide).Name("account.link.decide").Use(required, minting)

	r.Post("/account/keys", settings.CreateKey).Name("account.keys.store").Use(required, minting)
	r.Delete("/account/keys/{id}", settings.RevokeKey).Name("account.keys.revoke").Use(required)

	return nil
}

// byUser keys a rate limit on the signed-in account.
//
// Per account rather than per IP: the limit protects the account, and
// somebody holding a stolen session can change address between requests.
func byUser(r *http.Request) string {
	u, err := auth.UserFrom[models.User](r.Context(), "web")
	if err != nil {
		// The route is behind Required, so this should not happen. Falling
		// back to the address keeps the limit bounded rather than unlimited
		// if it ever does.
		return "account-keys:" + ratelimit.ByIP(r)
	}

	return "account-keys:" + u.AuthID()
}
