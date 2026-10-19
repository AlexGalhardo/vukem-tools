// Package apiv1 holds the JSON API handlers.
//
// Every handler is a typed handler (vukem/api): its input and output types
// are what the OpenAPI document is generated from, so a route that is not
// describable cannot be registered. See routes/apidocs.go and docs/api.md.
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

// MaxAbilitiesPerKey caps how many abilities one request may ask for.
//
// `validate` tags are static, so the cap cannot be written as
// len(abilities.All()); TestIssueDocumentsEveryAbility asserts the two stay
// equal, which is what keeps the tag honest when a tool is added.
const MaxAbilitiesPerKey = 16

// IssueInput is the body of POST /api/v1/tokens.
type IssueInput struct {
	Email    string `json:"email"    validate:"required,trim,lowercase,email" doc:"The account's email address."`
	Password string `json:"password" validate:"required,min=1"                doc:"The account's password."`
	// DeviceName labels the key in GET /api/v1/tokens, so a key can be
	// recognised before it is revoked.
	DeviceName string `json:"device_name" validate:"required,trim,min=1,max=100" doc:"A label for this key, shown when listing keys."`
	// Abilities the key is granted; every one must be known to
	// app/abilities. There is no default and no wildcard: a key says what it
	// may do, so revoking one tool is never a surprise.
	Abilities []string `json:"abilities" validate:"required,min=1,max=16,dive,trim,min=1,max=64" doc:"What the key may do; GET /api/v1/abilities lists the accepted values."`
	// Code or RecoveryCode is required for users with two-factor
	// authentication.
	Code         string `json:"code"          validate:"trim,max=16" doc:"A two-factor code, for accounts with two-factor authentication."`
	RecoveryCode string `json:"recovery_code" validate:"trim,max=32" doc:"A two-factor recovery code, instead of code."`
}

// TokenIssued is the answer to POST /api/v1/tokens.
type TokenIssued struct {
	ID string `json:"id" doc:"The key id, used to revoke it."`
	// Token is the plaintext key, returned once and never stored.
	Token     string     `json:"token"      doc:"The secret key. It is shown once; store it now."`
	Type      string     `json:"type"       doc:"The authorization scheme to use, always \"Bearer\"."`
	Abilities []string   `json:"abilities"  doc:"What this key may do."`
	ExpiresAt *time.Time `json:"expires_at" doc:"When the key stops working, or null when it never expires."`
}

// TokenList is the answer to GET /api/v1/tokens.
type TokenList struct {
	Data []auth.Token `json:"data" doc:"The caller's keys, newest first, revoked ones included."`
}

// AbilityList is the answer to GET /api/v1/abilities.
type AbilityList struct {
	Data []string `json:"data" doc:"Every ability a key may be granted."`
}

// RevokeInput is the path input of DELETE /api/v1/tokens/{token}.
type RevokeInput struct {
	Token string `json:"token" validate:"required" doc:"The id of the key to revoke, as returned in the id field when it was issued." example:"k_7f3c1b9e"`
}

// Issue exchanges email and password for a key: POST /api/v1/tokens.
//
// The plaintext key is returned once and never stored: only its SHA-256 hash
// is. A caller that loses it issues another.
func (h *Tokens) Issue(c *web.Ctx, in IssueInput) (TokenIssued, error) {
	var out TokenIssued

	// Before the credentials: an unknown ability is the caller's typo, and
	// answering it as a validation error rather than as a login failure is
	// both kinder and the behaviour the endpoint has always had (the check
	// used to be a z.Refine on the request schema).
	if bad := abilities.Unknown(in.Abilities); len(bad) > 0 {
		return out, z.FieldError("abilities", "Unknown abilities: "+strings.Join(bad, ", ")+
			". Allowed: "+strings.Join(abilities.All(), ", ")+".")
	}

	u, err := h.a.Web.Validate(c, map[string]string{"email": in.Email, "password": in.Password})
	if err != nil {
		return out, err
	}

	// A key is a login: an unverified address may belong to someone else
	// (anyone can sign up first with it).
	if !u.EmailVerified() {
		return out, errs.NewKey(http.StatusForbidden, "auth.unverified", "Your email address is not verified.", nil)
	}

	plain, tok, err := h.a.Tokens.Create(c.Context(), u.AuthID(), in.DeviceName, in.Abilities, h.TTL)
	if err != nil {
		return out, err
	}

	return TokenIssued{
		ID:        tok.ID,
		Token:     plain,
		Type:      "Bearer",
		Abilities: tok.Abilities,
		ExpiresAt: tok.ExpiresAt,
	}, nil
}

// Me returns the authenticated user: GET /api/v1/me.
func (*Tokens) Me(c *web.Ctx, _ struct{}) (models.User, error) {
	return caller(c)
}

// Index lists the caller's keys, newest first, revoked ones included, never
// the secrets: GET /api/v1/tokens.
func (h *Tokens) Index(c *web.Ctx, _ struct{}) (TokenList, error) {
	u, err := caller(c)
	if err != nil {
		return TokenList{}, err
	}

	list, err := h.a.Tokens.List(c.Context(), u.AuthID())
	if err != nil {
		return TokenList{}, err
	}

	return TokenList{Data: list}, nil
}

// Revoke revokes the key used by this request (the row stays, marked
// revoked_at): DELETE /api/v1/tokens/current.
func (h *Tokens) Revoke(c *web.Ctx, _ struct{}) (struct{}, error) {
	var out struct{}

	tok, ok := auth.CurrentToken(c, "api")
	if !ok {
		return out, auth.ErrUnauthenticated
	}

	return out, h.a.Tokens.Revoke(c.Context(), tok.ID)
}

// RevokeOne revokes one of the caller's keys by id:
// DELETE /api/v1/tokens/{token}.
//
// RevokeFor scopes the delete to the caller, so guessing another user's id
// revokes nothing and is answered exactly like an id that does not exist.
func (h *Tokens) RevokeOne(c *web.Ctx, in RevokeInput) (struct{}, error) {
	var out struct{}

	u, err := caller(c)
	if err != nil {
		return out, err
	}

	revoked, err := h.a.Tokens.RevokeFor(c.Context(), u.AuthID(), in.Token)
	if err != nil {
		return out, err
	}

	if !revoked {
		return out, errs.New(http.StatusNotFound, "No such key.")
	}

	return out, nil
}

// Abilities lists the abilities a key may be granted, so a client does not
// have to hardcode them: GET /api/v1/abilities.
func (*Tokens) Abilities(_ *web.Ctx, _ struct{}) (AbilityList, error) {
	return AbilityList{Data: abilities.All()}, nil
}
