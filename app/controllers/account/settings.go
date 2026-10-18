package account

import (
	"fmt"
	"slices"
	"time"

	"github.com/AlexGalhardo/vukem/auth"
	"github.com/AlexGalhardo/vukem/inertia"
	"github.com/AlexGalhardo/vukem/orm"
	"github.com/AlexGalhardo/vukem/web"
	"github.com/AlexGalhardo/vukem/z"

	"github.com/AlexGalhardo/vukem-tools/app/abilities"
	"github.com/AlexGalhardo/vukem-tools/app/models"
	"github.com/AlexGalhardo/vukem-tools/app/requests"
	"github.com/AlexGalhardo/vukem-tools/internal/quota"
)

// Settings is the account area: the profile, the password, the API keys and
// what today's quota has left.
//
// One page rather than four, because these are four short forms and somebody
// who came to rotate a key should not have to go looking for it. Each section
// is its own landmark, so a screen reader and a keyboard still move between
// them in one step.
type Settings struct {
	h     *Handlers
	quota *quota.Counter
	// keyTTL is how long a new key lasts, matching the API's own default so a
	// key made here and a key made over the API behave the same.
	keyTTL time.Duration
}

// NewSettings returns the account-area handlers.
//
// The quota counter may be nil in a process that did not build the pipeline.
// The page then omits the usage section rather than failing: somebody who came
// to change their password should not be stopped by it.
func NewSettings(h *Handlers, counter *quota.Counter) *Settings {
	return &Settings{h: h, quota: counter, keyTTL: 30 * 24 * time.Hour}
}

// profileView is what the page shows about the account.
//
// Not models.User: that carries the password hash and the session version,
// and a page prop ends up in the HTML.
type profileView struct {
	Name     string    `json:"name"`
	Email    string    `json:"email"`
	Verified bool      `json:"verified"`
	JoinedAt time.Time `json:"joined_at"`
}

// keyView is one API key as the page shows it. The secret is absent, because
// it exists only in the response that created it.
type keyView struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Abilities []string   `json:"abilities"`
	LastUsed  *time.Time `json:"last_used_at"`
	ExpiresAt *time.Time `json:"expires_at"`
	RevokedAt *time.Time `json:"revoked_at"`
}

// usageView is today's quota.
type usageView struct {
	SpentSeconds  int64     `json:"spent_seconds"`
	BudgetSeconds int64     `json:"budget_seconds"`
	ResetsAt      time.Time `json:"resets_at"`
	Exempt        bool      `json:"exempt"`
}

// Show renders the account area: GET /account.
func (s *Settings) Show(c *web.Ctx) error {
	u, err := auth.User[models.User](c, "web")
	if err != nil {
		return err
	}

	keys, err := s.keys(c, u)
	if err != nil {
		return err
	}

	props := inertia.Props{
		"profile":   profileOf(u),
		"keys":      keys,
		"abilities": abilities.All(),
	}

	// Omitted rather than wrong when the pipeline was not built here.
	if s.quota != nil {
		allowance, err := s.quota.Check(c.Context(), u.AuthID())
		if err != nil {
			return fmt.Errorf("account: read the quota: %w", err)
		}

		props["usage"] = usageView{
			SpentSeconds:  int64(allowance.Spent.Seconds()),
			BudgetSeconds: int64(allowance.Budget.Seconds()),
			ResetsAt:      allowance.ResetsAt,
			Exempt:        allowance.Exempt,
		}
	}

	return inertia.Render(c, "Account/Index", props)
}

// UpdateProfile changes the display name: PATCH /account/profile.
func (s *Settings) UpdateProfile(c *web.Ctx) error {
	u, err := auth.User[models.User](c, "web")
	if err != nil {
		return err
	}

	in, err := web.Parse(c, requests.UpdateProfileSchema)
	if err != nil {
		return err
	}

	if _, err := orm.QueryOf[models.User](s.h.a.DB).Where("id", u.ID).
		Update(c.Context(), orm.Set{"name": in.Name}); err != nil {
		return fmt.Errorf("account: update the name: %w", err)
	}

	inertia.Flash(c, "status", "Your name has been updated.")

	return c.Redirect("/account")
}

// UpdatePassword changes the password: PUT /account/password.
//
// Getting the current password wrong is a field error, not a sign-out: the
// person is already authenticated, and logging them out would punish a typo.
func (s *Settings) UpdatePassword(c *web.Ctx) error {
	u, err := auth.User[models.User](c, "web")
	if err != nil {
		return err
	}

	in, err := web.Parse(c, requests.ChangePasswordSchema)
	if err != nil {
		return err
	}

	ok, err := s.h.a.Hasher.Verify(u.Password, in.CurrentPassword)
	if err != nil {
		return fmt.Errorf("account: verify the current password: %w", err)
	}

	if !ok {
		return z.FieldError("current_password", "That is not your current password.")
	}

	if err := s.h.a.ChangePassword(c.Context(), u, in.Password, c.Session().ID()); err != nil {
		return fmt.Errorf("account: change the password: %w", err)
	}

	inertia.Flash(c, "status",
		"Your password has been changed. Every other session has been signed out.")

	return c.Redirect("/account")
}

// CreateKey issues an API key: POST /account/keys.
func (s *Settings) CreateKey(c *web.Ctx) error {
	u, err := auth.User[models.User](c, "web")
	if err != nil {
		return err
	}

	in, err := web.Parse(c, requests.CreateKeySchema)
	if err != nil {
		return err
	}

	granted, err := checkAbilities(in.Abilities)
	if err != nil {
		return err
	}

	plain, _, err := s.h.a.Tokens.Create(c.Context(), u.AuthID(), in.DeviceName, granted, s.keyTTL)
	if err != nil {
		return fmt.Errorf("account: issue a key: %w", err)
	}

	// Flashed rather than rendered into the props: a prop survives in the
	// history entry, and the back button would put the secret back on screen.
	// The page says plainly that it is shown once, because somebody who
	// misses that loses the key and makes another one.
	inertia.Flash(c, "key", plain)
	inertia.Flash(c, "status", "Your key has been created. Copy it now — it is not shown again.")

	return c.Redirect("/account")
}

// checkAbilities validates the requested abilities against the closed set.
//
// Checked here as well as in the API because this form posts names from a
// browser: an unknown ability would mint a key that grants nothing and fails
// only when somebody tries to use it.
func checkAbilities(requested []string) ([]string, error) {
	known := abilities.All()
	seen := make(map[string]bool, len(requested))
	out := make([]string, 0, len(requested))

	for _, a := range requested {
		if seen[a] {
			continue
		}

		if !slices.Contains(known, a) {
			return nil, z.FieldError("abilities", "One of those permissions does not exist.")
		}

		seen[a] = true
		out = append(out, a)
	}

	if len(out) == 0 {
		return nil, z.FieldError("abilities", "Choose at least one thing the key may do.")
	}

	return out, nil
}

// RevokeKey revokes one key: DELETE /account/keys/{id}.
//
// Scoped to the caller, and another account's key id is answered exactly like
// an unknown one: telling the difference would make this an oracle for which
// key ids exist.
func (s *Settings) RevokeKey(c *web.Ctx) error {
	u, err := auth.User[models.User](c, "web")
	if err != nil {
		return err
	}

	revoked, err := s.h.a.Tokens.RevokeFor(c.Context(), u.AuthID(), c.Param("id"))
	if err != nil {
		return fmt.Errorf("account: revoke a key: %w", err)
	}

	if !revoked {
		return z.FieldError("key", "That key does not exist.")
	}

	inertia.Flash(c, "status", "The key has been revoked.")

	return c.Redirect("/account")
}

// keys lists the caller's keys for the page.
func (s *Settings) keys(c *web.Ctx, u models.User) ([]keyView, error) {
	list, err := s.h.a.Tokens.List(c.Context(), u.AuthID())
	if err != nil {
		return nil, fmt.Errorf("account: list the keys: %w", err)
	}

	out := make([]keyView, 0, len(list))

	for _, t := range list {
		out = append(out, keyView{
			ID:        t.ID,
			Name:      t.Name,
			Abilities: t.Abilities,
			LastUsed:  t.LastUsedAt,
			ExpiresAt: t.ExpiresAt,
			RevokedAt: t.RevokedAt,
		})
	}

	return out, nil
}

// profileOf builds the profile prop.
func profileOf(u models.User) profileView {
	return profileView{
		Name:     u.Name,
		Email:    u.Email,
		Verified: u.EmailVerifiedAt.Valid,
		JoinedAt: u.CreatedAt,
	}
}
