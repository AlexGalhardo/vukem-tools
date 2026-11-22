package account

import (
	"net/url"
	"strings"

	"github.com/AlexGalhardo/vukem/errs"
	"github.com/AlexGalhardo/vukem/inertia"
	"github.com/AlexGalhardo/vukem/web"

	"github.com/AlexGalhardo/vukem-tools/app/requests"
)

// ShowForgot renders the reset link request page.
func (*Handlers) ShowForgot(c *web.Ctx) error { return inertia.Render(c, "Auth/ForgotPassword", nil) }

// SendResetLink emails a reset link; the answer is the same whether or not
// the email has an account.
func (h *Handlers) SendResetLink(c *web.Ctx) error {
	in, err := web.Parse(c, requests.ForgotPasswordSchema)
	if err != nil {
		return err
	}

	if h.a.Local {
		return h.showLocalResetCode(c, in.Email)
	}

	if err := h.a.SendResetLink(c.Context(), in.Email); err != nil {
		return err
	}

	inertia.Flash(c, "status", h.a.T(c.Context(), "auth.reset_link_sent"))

	return c.Redirect("/forgot-password")
}

// showLocalResetCode is the local answer to "forgot password": no mail, so the
// one-time code goes straight to the reset page in the redirect. The route's
// token parameter is marked secret, so the request log never records it.
func (h *Handlers) showLocalResetCode(c *web.Ctx, email string) error {
	code, found, err := h.a.LocalResetCode(c.Context(), email)
	if err != nil {
		return err
	}

	if !found {
		return errs.NewValidation().Add("email", h.a.T(c.Context(), "auth.local_no_account"))
	}

	return c.Redirect(
		"/reset-password/" + code + "?email=" + url.QueryEscape(strings.ToLower(strings.TrimSpace(email))),
	)
}

// ShowReset renders the new password page.
func (*Handlers) ShowReset(c *web.Ctx) error {
	return inertia.Render(c, "Auth/ResetPassword", inertia.Props{"token": c.Param("token"), "email": c.Query("email")})
}

// Reset sets the new password.
func (h *Handlers) Reset(c *web.Ctx) error {
	in, err := web.Parse(c, requests.ResetPasswordSchema)
	if err != nil {
		return err
	}

	if _, err := h.a.ResetPassword(c.Context(), in.Email, in.Token, in.Password); err != nil {
		return err
	}

	inertia.Flash(c, "status", h.a.T(c.Context(), "auth.password_reset"))

	return c.Redirect("/login")
}
