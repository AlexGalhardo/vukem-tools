package apiv1

import (
	"fmt"
	"strings"
	"time"

	"github.com/AlexGalhardo/vukem/web"
	"github.com/AlexGalhardo/vukem/z"

	"github.com/AlexGalhardo/vukem-tools/app/accounts"
	"github.com/AlexGalhardo/vukem-tools/app/devicelink"
)

// DeviceLinks lets a program that cannot show a login form, such as the
// Telegram bot, get an API key for an account without handling its password.
// The flow is described in app/devicelink.
type DeviceLinks struct {
	svc *devicelink.Service
	a   *accounts.Accounts
	// base is APP_URL without a trailing slash, where the person is sent to
	// approve. Never taken from the request: its Host is attacker-controlled.
	base string
	// TTL is the key lifetime, the same as a key made on the account page.
	TTL time.Duration
}

// NewDeviceLinks returns the handlers.
func NewDeviceLinks(svc *devicelink.Service, a *accounts.Accounts, baseURL string) *DeviceLinks {
	return &DeviceLinks{svc: svc, a: a, base: strings.TrimRight(baseURL, "/"), TTL: 30 * 24 * time.Hour}
}

// LinkStartInput is the body of POST /api/v1/device-links.
type LinkStartInput struct {
	Client     string `json:"client"      validate:"required,trim,oneof=telegram" doc:"The kind of program asking. It decides what the key may do: the server fixes the abilities, the program cannot choose them."`
	DeviceName string `json:"device_name" validate:"trim,max=64"                  doc:"A label for the key in the account's key list, such as the Telegram name of the person. Optional."`
}

// LinkStarted is the answer to POST /api/v1/device-links.
type LinkStarted struct {
	// DeviceCode is a secret: keep it, send it only when polling, never show it.
	DeviceCode string `json:"device_code" doc:"Secret to poll with. Never show it to the person."`
	// UserCode is what the person compares with what the page shows.
	UserCode        string `json:"user_code"        doc:"The short code to show the person, formatted XXXX-XXXX."`
	VerificationURL string `json:"verification_url" doc:"Where the person approves. It already contains the user code."`
	ExpiresIn       int    `json:"expires_in"       doc:"Seconds until the link can no longer be approved."`
	Interval        int    `json:"interval"         doc:"Seconds to wait between polls."`
}

// Start creates a link: POST /api/v1/device-links.
func (h *DeviceLinks) Start(c *web.Ctx, in LinkStartInput) (LinkStarted, error) {
	client := devicelink.Client(in.Client)
	if !client.Valid() {
		return LinkStarted{}, z.FieldError(
			"client",
			"Unknown client. Allowed: "+strings.Join(devicelink.Clients(), ", ")+".",
		)
	}

	started, err := h.svc.Start(c.Context(), client, in.DeviceName)
	if err != nil {
		return LinkStarted{}, err
	}

	return LinkStarted{
		DeviceCode:      started.DeviceCode,
		UserCode:        started.UserCode,
		VerificationURL: h.base + "/account/link?code=" + started.UserCode,
		ExpiresIn:       int(devicelink.TTL.Seconds()),
		Interval:        devicelink.Interval,
	}, nil
}

// LinkPollInput is the body of POST /api/v1/device-links/token.
type LinkPollInput struct {
	DeviceCode string `json:"device_code" validate:"required,trim,max=128" doc:"The device_code from the call that started the link."`
}

// LinkToken is the answer to polling a link.
type LinkToken struct {
	// Status is pending, approved, denied or expired. Only approved carries a key.
	Status string `json:"status" doc:"pending, approved, denied or expired. A link that never existed reads as expired."`
	// ID, Token, Type, Abilities and ExpiresAt are present only when approved.
	ID        string     `json:"id,omitempty"         doc:"The key id, used to revoke it. Only when approved."`
	Token     string     `json:"token,omitempty"      doc:"The secret key, shown once. Only when approved."`
	Type      string     `json:"type,omitempty"       doc:"The authorization scheme, always Bearer. Only when approved."`
	Abilities []string   `json:"abilities,omitempty"  doc:"What the key may do. Only when approved."`
	ExpiresAt *time.Time `json:"expires_at,omitempty" doc:"When the key stops working. Only when approved."`
}

// Token reports a link's status and, once, hands over the key:
// POST /api/v1/device-links/token.
//
// The key is made here, at the moment of delivery, so nothing secret waits in
// storage between the person's approval and the program's poll.
func (h *DeviceLinks) Token(c *web.Ctx, in LinkPollInput) (LinkToken, error) {
	out, err := h.svc.Poll(c.Context(), strings.TrimSpace(in.DeviceCode))
	if err != nil {
		return LinkToken{}, err
	}

	if out.Status != devicelink.Approved {
		return LinkToken{Status: string(out.Status)}, nil
	}

	plain, tok, err := h.a.Tokens.Create(c.Context(), out.UserID, "Telegram: "+out.DeviceName, out.Abilities, h.TTL)
	if err != nil {
		return LinkToken{}, fmt.Errorf("devicelink: the link was approved but the key could not be made: %w", err)
	}

	return LinkToken{
		Status: string(devicelink.Approved), ID: tok.ID, Token: plain, Type: "Bearer",
		Abilities: tok.Abilities, ExpiresAt: tok.ExpiresAt,
	}, nil
}
