// Package apiv1 holds the JSON API handlers.
package apiv1

import (
	"net/http"
	"strings"
	"time"

	"github.com/AlexGalhardo/vukem/auth"
	"github.com/AlexGalhardo/vukem/errs"
	"github.com/AlexGalhardo/vukem/web"
	"github.com/AlexGalhardo/vukem/z"

	"github.com/AlexGalhardo/vukem-tools/app/abilities"
	"github.com/AlexGalhardo/vukem-tools/app/accounts"
	"github.com/AlexGalhardo/vukem-tools/app/models"
)

// Tokens issues, lists and revokes API keys.
type Tokens struct {
	a *accounts.Accounts
	// TTL is the token lifetime.
	TTL time.Duration
}

// NewTokens returns the token handlers.
func NewTokens(a *accounts.Accounts) *Tokens { return &Tokens{a: a, TTL: 30 * 24 * time.Hour} }

type issueInput struct {
	Email      string `json:"email"`
	Password   string `json:"password"`
	DeviceName string `json:"device_name"`
	// Abilities the key is granted; every one must be known to
	// app/abilities. There is no default and no wildcard: a key says what it
	// may do, so revoking one tool is never a surprise.
	Abilities []string `json:"abilities"`
	// Code or RecoveryCode is required for users with two-factor
	// authentication.
	Code         string `json:"code"`
	RecoveryCode string `json:"recovery_code"`
}

var issueSchema = z.Struct[issueInput](z.Shape{
	"email":         z.String().Trim().Email(),
	"password":      z.String().Min(1),
	"device_name":   z.String().Trim().Min(1).Max(100),
	"abilities":     z.Slice(z.String().Trim().Min(1).Max(64)).Min(1).Max(len(abilities.All())),
	"code":          z.String().Trim().Max(16).Optional(),
	"recovery_code": z.String().Trim().Max(32).Optional(),
}).Refine(func(in issueInput) error {
	if bad := abilities.Unknown(in.Abilities); len(bad) > 0 {
		return z.FieldError("abilities", "Unknown abilities: "+strings.Join(bad, ", ")+
			". Allowed: "+strings.Join(abilities.All(), ", ")+".")
	}

	return nil
})

// Issue exchanges email and password for a key: POST /api/v1/tokens.
//
// The plaintext key is returned once and never stored: only its SHA-256 hash
// is. A caller that loses it issues another.
func (h *Tokens) Issue(c *web.Ctx) error {
	in, err := web.Parse(c, issueSchema)
	if err != nil {
		return err
	}

	u, err := h.a.Web.Validate(c, map[string]string{"email": strings.ToLower(in.Email), "password": in.Password})
	if err != nil {
		return err
	}

	// A key is a login: an unverified address may belong to someone else
	// (anyone can sign up first with it).
	if !u.EmailVerified() {
		return errs.NewKey(http.StatusForbidden, "auth.unverified", "Your email address is not verified.", nil)
	}

	plain, tok, err := h.a.Tokens.Create(c.Context(), u.AuthID(), in.DeviceName, in.Abilities, h.TTL)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusCreated, map[string]any{
		"id":         tok.ID,
		"token":      plain,
		"type":       "Bearer",
		"abilities":  tok.Abilities,
		"expires_at": tok.ExpiresAt,
	})
}

// Me returns the authenticated user: GET /api/v1/me.
func (*Tokens) Me(c *web.Ctx) error {
	u, err := auth.User[models.User](c, "api")
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, u)
}

// Index lists the caller's keys, newest first, revoked ones included, never
// the secrets: GET /api/v1/tokens.
func (h *Tokens) Index(c *web.Ctx) error {
	u, err := auth.User[models.User](c, "api")
	if err != nil {
		return err
	}

	list, err := h.a.Tokens.List(c.Context(), u.AuthID())
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, map[string]any{"data": list})
}

// Revoke revokes the key used by this request (the row stays, marked
// revoked_at): DELETE /api/v1/tokens/current.
func (h *Tokens) Revoke(c *web.Ctx) error {
	tok, ok := auth.CurrentToken(c, "api")
	if !ok {
		return auth.ErrUnauthenticated
	}

	if err := h.a.Tokens.Revoke(c.Context(), tok.ID); err != nil {
		return err
	}

	return c.NoContent()
}

// RevokeOne revokes one of the caller's keys by id:
// DELETE /api/v1/tokens/{token}.
//
// RevokeFor scopes the delete to the caller, so guessing another user's id
// revokes nothing and is answered exactly like an id that does not exist.
func (h *Tokens) RevokeOne(c *web.Ctx) error {
	u, err := auth.User[models.User](c, "api")
	if err != nil {
		return err
	}

	revoked, err := h.a.Tokens.RevokeFor(c.Context(), u.AuthID(), c.Param("token"))
	if err != nil {
		return err
	}

	if !revoked {
		return errs.New(http.StatusNotFound, "No such key.")
	}

	return c.NoContent()
}

// Abilities lists the abilities a key may be granted, so a client does not
// have to hardcode them: GET /api/v1/abilities.
func (*Tokens) Abilities(c *web.Ctx) error {
	return c.JSON(http.StatusOK, map[string]any{"data": abilities.All()})
}
