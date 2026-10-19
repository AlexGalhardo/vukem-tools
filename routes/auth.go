package routes

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/AlexGalhardo/vukem"
	"github.com/AlexGalhardo/vukem/auth"
	"github.com/AlexGalhardo/vukem/inertia"
	"github.com/AlexGalhardo/vukem/ratelimit"
	"github.com/AlexGalhardo/vukem/router"
	"github.com/AlexGalhardo/vukem/web"

	"github.com/AlexGalhardo/vukem-tools/app/accounts"
	"github.com/AlexGalhardo/vukem-tools/app/controllers/account"
)

// Auth registers the sign-in routes (vukem install:auth). Password sign-up,
// sign-in, reset and email verification are behind PasswordAuthEnabled.
func Auth(r *router.Router, app *vukem.App) error {
	acc, err := accounts.New(app)
	if err != nil {
		return err
	}

	// RATE_LIMIT_STORE=cache shares the limits between servers (CACHE_STORE=database or Redis).
	limiters, err := app.RateLimiters()
	if err != nil {
		return err
	}

	h := account.New(acc)
	guest := auth.Guest(acc.Web)
	required := auth.Required(acc.Web)
	throttle := ratelimit.Middleware(limiters("login", 5, time.Minute), account.LoginThrottleKey)
	perAccount := ratelimit.Middleware(limiters("login.account", 20, 10*time.Minute), account.LoginAccountThrottleKey)
	mails := ratelimit.Middleware(limiters("mails", 3, time.Minute), ratelimit.ByIP)
	// Sign-up still notifies the owner of an existing address, so limit it per
	// IP and per address. The limits count requests, not accounts, so they
	// answer alike for a new and an existing email.
	signupIP := ratelimit.Middleware(limiters("register", 10, 10*time.Minute), ratelimit.ByIP)
	signupEmail := ratelimit.Middleware(limiters("register.email", 5, time.Hour), account.RegisterThrottleKey)

	r.Use(shareAuth(acc))

	r.Get("/login", h.ShowLogin).Name("login").Use(guest)
	r.Post("/logout", h.Logout).Name("logout").Use(required)

	// Password sign-in and sign-up are a unit with password reset and email
	// verification: all four need mail that actually reaches a stranger's
	// inbox. On the Mailtrap sandbox nothing is delivered, so an account
	// created with a password could never be recovered and an address could
	// never be confirmed. Until a real sending domain is verified, the whole
	// set is off and Google is the only way in.
	//
	// The routes are not registered rather than answering 403: a disabled
	// feature should have no endpoint to probe, and hiding the form in the
	// frontend protects nothing on its own.
	if PasswordAuthEnabled(app) {
		r.Post("/login", h.Login).Use(guest, throttle, perAccount)
		r.Get("/register", h.ShowRegister).Name("register").Use(guest)
		r.Post("/register", h.Register).Use(guest, signupIP, signupEmail)
		r.Get("/register/check-email", h.CheckEmail).Name("register.check").Use(guest)

		r.Get("/forgot-password", h.ShowForgot).Name("password.request").Use(guest)
		r.Post("/forgot-password", h.SendResetLink).Name("password.email").Use(guest, mails)
		r.Get("/reset-password/{token}", h.ShowReset).Name("password.reset").Use(guest)
		r.Post("/reset-password", h.Reset).Name("password.update").Use(guest)

		r.Get("/email/verify", h.Notice).Name("verification.notice").Use(required)
		// The signed link needs the account's login: signed out, Required
		// keeps the link as the intended URL and the login comes back to it.
		// Verify checks the signature after the login, so an expired link can
		// point to the resend page.
		r.Get("/email/verify/{id}", h.Verify).Name("verification.verify").Use(required)
		r.Post("/email/verification-notification", h.Resend).Name("verification.send").Use(required, mails)
	}

	r.Get("/dashboard", h.Dashboard).Name("dashboard").Use(required)

	return nil
}

// PasswordAuthEnabled reports whether this deployment offers email and
// password sign-in. PASSWORD_AUTH=off turns it off, leaving the social
// providers as the only way in.
//
// It defaults to **on**, so a fresh checkout and the test suite behave the
// way the scaffold does. A deployment without a verified sending domain sets
// it off, because password reset and email confirmation both need mail that
// reaches an address the project does not own.
func PasswordAuthEnabled(app *vukem.App) bool {
	switch strings.ToLower(strings.TrimSpace(app.Source.Get("PASSWORD_AUTH"))) {
	case "off", "false", "0", "no":
		return false
	default:
		return true
	}
}

// shareAuth shares the signed-in user (or null) with every page as
// auth.user; it is loaded only when a page is rendered.
func shareAuth(acc *accounts.Accounts) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c := web.NewCtx(w, r, web.ServicesFrom(r.Context()))

			inertia.Share(r.Context(), "auth", func(context.Context) (any, error) {
				u, err := acc.Web.User(c)
				if err != nil {
					return map[string]any{"user": nil}, nil //nolint:nilerr // signed out
				}

				return map[string]any{"user": u}, nil
			})

			next.ServeHTTP(w, r)
		})
	}
}
