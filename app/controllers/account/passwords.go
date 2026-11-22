package account

import (
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
		return h.requestLocalReset(c, in.Email)
	}

	if err := h.a.SendResetLink(c.Context(), in.Email); err != nil {
		return err
	}

	inertia.Flash(c, "status", h.a.T(c.Context(), "auth.reset_link_sent"))

	return c.Redirect("/forgot-password")
}

// requestLocalReset is the local answer to "forgot password": there is no
// mailbox, so the code goes to a file in the data directory and never into the
// response. The answer is the same for an account and for an unknown email.
func (h *Handlers) requestLocalReset(c *web.Ctx, email string) error {
	if err := h.a.LocalRequestReset(c.Context(), email); err != nil {
		return err
	}

	inertia.Flash(c, "status", h.a.T(c.Context(), "auth.local_reset_requested"))

	return c.Redirect("/reset-password")
}

// ShowLocalReset renders the local reset page: email, the code from the file
// and the new password. The code is typed, never part of the URL.
func (h *Handlers) ShowLocalReset(c *web.Ctx) error {
	return inertia.Render(c, "Auth/ResetPassword", inertia.Props{
		"token": "", "email": "", "codeFile": h.a.LocalResetFile(),
	})
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
