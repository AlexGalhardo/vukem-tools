// Package account holds the login, registration, password reset and
// email verification handlers (vukem install:auth), rendered with Inertia
// pages in resources/js/pages/Auth.
package account

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/AlexGalhardo/vukem/auth"
	"github.com/AlexGalhardo/vukem/inertia"
	"github.com/AlexGalhardo/vukem/ratelimit"
	"github.com/AlexGalhardo/vukem/web"

	"github.com/AlexGalhardo/vukem-tools/app/accounts"
	"github.com/AlexGalhardo/vukem-tools/app/requests"
)

// Handlers serves the account pages.
type Handlers struct {
	a *accounts.Accounts
}

// New returns the handlers.
func New(a *accounts.Accounts) *Handlers { return &Handlers{a: a} }

// LoginThrottleKey limits login attempts per email and IP.
func LoginThrottleKey(r *http.Request) string {
	if r.Method != http.MethodPost {
		return ""
	}

	email, ok := loginEmail(r)
	if !ok {
		return "login-malformed:" + ratelimit.ByIP(r)
	}

	return "login:" + email + ":" + ratelimit.ByIP(r)
}

// LoginAccountThrottleKey limits login attempts per email from any IP, so
// many addresses cannot share the guessing budget of one account.
func LoginAccountThrottleKey(r *http.Request) string {
	if r.Method != http.MethodPost {
		return ""
	}

	email, ok := loginEmail(r)
	if !ok {
		return "login-account-malformed:" + ratelimit.ByIP(r)
	}

	return "login-account:" + email
}

// RegisterThrottleKey limits sign-ups per address. It keys on a hash of
// the normalized email, so the limiter store holds no addresses.
func RegisterThrottleKey(r *http.Request) string {
	if r.Method != http.MethodPost {
		return ""
	}

	email, ok := loginEmail(r)
	if !ok {
		return "register-malformed:" + ratelimit.ByIP(r)
	}

	sum := sha256.Sum256([]byte(email))

	return "register:" + hex.EncodeToString(sum[:])
}

// loginEmail normalizes the email like LoginSchema (trimmed, lower case),
// so " a@b.co" and "A@b.co " share a bucket. Inertia posts JSON, so the
// email is read from a JSON body as well as from a form.
//
// ok is false when the body claims to be JSON and is not. There is no email
// to key on then, and an empty one would put every malformed request from
// everywhere in one shared bucket, so the callers key on the address alone.
func loginEmail(r *http.Request) (email string, ok bool) {
	email = r.PostFormValue("email")

	if email == "" && strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
		if err == nil {
			var in struct {
				Email string `json:"email"`
			}

			if err := json.Unmarshal(body, &in); err != nil {
				r.Body = io.NopCloser(bytes.NewReader(body))

				return "", false
			}

			email = in.Email
		}

		r.Body = io.NopCloser(bytes.NewReader(body))
	}

	return strings.ToLower(strings.TrimSpace(email)), true
}

// ShowLogin renders the login page.
func (*Handlers) ShowLogin(c *web.Ctx) error { return inertia.Render(c, "Auth/Login", nil) }

// Login signs the user in and goes to the page they wanted.
func (h *Handlers) Login(c *web.Ctx) error {
	in, err := web.Parse(c, requests.LoginSchema)
	if err != nil {
		return err
	}

	creds := map[string]string{"email": strings.ToLower(in.Email), "password": in.Password}
	if _, err := h.a.Web.Attempt(c, creds, auth.Remember(in.Remember)); err != nil {
		return err
	}

	target, _ := c.Session().Pull(auth.IntendedKey).(string)

	return c.Redirect(web.SafeRedirect(c.Request(), target, "/dashboard"))
}

// ShowRegister renders the registration page.
func (*Handlers) ShowRegister(c *web.Ctx) error { return inertia.Render(c, "Auth/Register", nil) }

// Register answers the same for new and existing emails (no account
// enumeration) and does not log in: the user is asked to check the inbox,
// where the verification link (or an "account exists" notice) waits.
func (h *Handlers) Register(c *web.Ctx) error {
	in, err := web.Parse(c, requests.RegisterSchema)
	if err != nil {
		return err
	}

	if _, err := h.a.Register(c.Context(), in); err != nil {
		return err
	}

	return c.Redirect("/register/check-email")
}

// CheckEmail is the page shown after every sign-up attempt.
func (*Handlers) CheckEmail(c *web.Ctx) error { return inertia.Render(c, "Auth/CheckEmail", nil) }

// Logout signs the user out and rotates the history encryption key.
func (h *Handlers) Logout(c *web.Ctx) error {
	if err := h.a.Web.Logout(c); err != nil {
		return err
	}

	inertia.ClearHistory(c)

	return c.Redirect("/")
}

// Dashboard is the first page after login; the user arrives as the shared
// auth.user prop.
func (*Handlers) Dashboard(c *web.Ctx) error { return inertia.Render(c, "Dashboard", nil) }
