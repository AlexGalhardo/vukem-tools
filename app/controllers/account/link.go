package account

import (
	"errors"
	"fmt"

	"github.com/AlexGalhardo/vukem/auth"
	"github.com/AlexGalhardo/vukem/inertia"
	"github.com/AlexGalhardo/vukem/web"
	"github.com/AlexGalhardo/vukem/z"

	"github.com/AlexGalhardo/vukem-tools/app/devicelink"
	"github.com/AlexGalhardo/vukem-tools/app/models"
)

// Link is the page where a signed-in person approves a program that asked for
// an API key, such as the Telegram bot (see app/devicelink).
//
// It is the only place the approval can happen, and it needs the person's own
// session: the program that started the link holds a secret that is worth
// nothing until somebody who is signed in says yes here.
type Link struct {
	svc *devicelink.Service
}

// NewLink returns the handlers.
func NewLink(svc *devicelink.Service) *Link { return &Link{svc: svc} }

// linkDecision is the form this page posts.
type linkDecision struct {
	Code     string `json:"code"`
	Decision string `json:"decision"`
}

var linkDecisionSchema = z.Struct[linkDecision](z.Shape{
	"code":     z.String().Trim().Min(8).Max(16),
	"decision": z.String().Trim().OneOf("approve", "deny"),
})

// Show renders the request: GET /account/link?code=XXXX-XXXX.
//
// A code that is unknown, expired or already decided is shown as one message,
// so the page cannot be used to learn which codes exist.
func (l *Link) Show(c *web.Ctx) error {
	code := devicelink.NormalisedCode(c.Query("code"))

	props := inertia.Props{"found": false, "code": ""}

	if len(code) >= 8 && len(code) <= 16 {
		req, ok, err := l.svc.Lookup(c.Context(), code)
		if err != nil {
			return fmt.Errorf("account: look up a link: %w", err)
		}

		if ok {
			props = inertia.Props{
				"found":       true,
				"code":        req.UserCode,
				"client":      string(req.Client),
				"device_name": req.DeviceName,
				"abilities":   req.Abilities,
				"expires_at":  req.ExpiresAt,
			}
		}
	}

	return inertia.Render(c, "Account/Link", props)
}

// Decide records the answer: POST /account/link.
func (l *Link) Decide(c *web.Ctx) error {
	u, err := auth.User[models.User](c, "web")
	if err != nil {
		return err
	}

	in, err := web.Parse(c, linkDecisionSchema)
	if err != nil {
		return err
	}

	approve := in.Decision == "approve"

	// A key is a login, and an unverified address may belong to somebody else
	// (anyone can sign up first with it): the API refuses to issue one to an
	// unverified account, and so does this.
	if approve && !u.EmailVerified() {
		return z.FieldError("code", "Verify your email address before connecting a program to your account.")
	}

	switch err := l.svc.Decide(c.Context(), devicelink.NormalisedCode(in.Code), u.AuthID(), approve); {
	case errors.Is(err, devicelink.ErrUnknown):
		return z.FieldError("code", "That code is not valid, has expired, or was already used.")
	case err != nil:
		return fmt.Errorf("account: record a link decision: %w", err)
	}

	if approve {
		inertia.Flash(c, "status", "Connected. Go back to the chat: it signs you in within a few seconds. "+
			"You can revoke the key under API keys at any time.")
	} else {
		inertia.Flash(c, "status", "Refused. Nothing was connected.")
	}

	return c.Redirect("/account")
}
