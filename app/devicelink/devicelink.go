// Package devicelink lets a program that cannot show a login form, such as
// the Telegram bot, obtain an API key for an account without ever seeing the
// account's password.
//
// It is the device authorisation grant (RFC 8628) reduced to what this
// service needs. The program asks for a link and gets two codes: a long
// secret device code it keeps, and a short user code it shows. The person
// opens the verification page in a browser they are already signed in on,
// checks that the code on screen is the one in their chat, and approves.
// The program, polling with its secret, then receives a key made on the spot.
// The key is created at that moment rather than stored in advance, so no
// secret waits anywhere between approval and delivery.
//
// The state is short-lived by nature, so it lives in the cache with a time to
// live and needs no table.
package devicelink

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/AlexGalhardo/vukem/cache"

	"github.com/AlexGalhardo/vukem-tools/app/abilities"
)

// Client names a kind of program that may ask for a link. The abilities a
// link grants are fixed by the client kind here, on the server: a program
// that could name its own abilities could ask for all of them.
type Client string

// Telegram is the client kind of the Telegram bot.
const Telegram Client = "telegram"

// Clients returns every client kind.
func Clients() []string { return []string{string(Telegram)} }

// Abilities returns what a key made for the client may do.
func Abilities(c Client) []string {
	if c == Telegram {
		return abilities.Telegram()
	}

	return nil
}

// Valid reports whether c is a client kind this service knows.
func (c Client) Valid() bool { return slices.Contains(Clients(), string(c)) }

// Status is where a link stands.
type Status string

// The statuses a poll can see. Unknown covers a link that never existed and
// one that expired, which are deliberately indistinguishable.
const (
	Pending  Status = "pending"
	Approved Status = "approved"
	Denied   Status = "denied"
	Expired  Status = "expired"
)

const (
	// TTL is how long a link can be approved. Long enough to open a browser,
	// sign in and read a code; short enough that a code left in a chat is dead
	// by the time anybody scrolls to it.
	TTL = 10 * time.Minute
	// Interval is how often the program should poll, in seconds.
	Interval = 3

	// codeAlphabet leaves out the letters and digits that look alike (0 O 1 I
	// L) and the vowels, which also keeps a code from spelling a word.
	codeAlphabet = "BCDFGHJKMNPQRTVWXZ23456789"
	codeLength   = 8
	maxDevice    = 64
)

// record is what the cache holds for one link.
type record struct {
	Client     Client    `json:"client"`
	DeviceName string    `json:"device_name"`
	UserCode   string    `json:"user_code"`
	Status     Status    `json:"status"`
	UserID     string    `json:"user_id,omitempty"`
	ExpiresAt  time.Time `json:"expires_at"`
}

// Started is the answer to asking for a link.
type Started struct {
	// DeviceCode is the secret the program polls with. It is never shown to
	// the person.
	DeviceCode string
	// UserCode is what the person compares, formatted XXXX-XXXX.
	UserCode  string
	ExpiresAt time.Time
}

// Request is what the verification page shows the person.
type Request struct {
	Client     Client
	DeviceName string
	UserCode   string
	Abilities  []string
	ExpiresAt  time.Time
}

// Service holds links.
type Service struct {
	store cache.Store
	now   func() time.Time
}

// New returns a service on a cache store.
func New(store cache.Store, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}

	return &Service{store: store, now: now}
}

func hashOf(deviceCode string) string {
	sum := sha256.Sum256([]byte(deviceCode))

	return hex.EncodeToString(sum[:])
}

func deviceKey(hash string) string { return "devicelink:d:" + hash }
func userKey(code string) string   { return "devicelink:u:" + code }
func claimKey(hash string) string  { return "devicelink:c:" + hash }
func normalise(code string) string {
	return strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(code))
}
func formatCode(code string) string { return code[:codeLength/2] + "-" + code[codeLength/2:] }

// NormalisedCode returns the canonical spelling of a code a person typed or
// pasted: upper case, no dash or spaces. It is what Lookup and Decide expect.
func NormalisedCode(code string) string { return normalise(code) }

func randomCode() (string, error) {
	buf := make([]byte, codeLength)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("devicelink: random code: %w", err)
	}

	out := make([]byte, codeLength)
	// A modulo over 256 would favour the first letters of the alphabet, so
	// bytes past the largest whole multiple are skipped.
	limit := 256 - 256%len(codeAlphabet)

	for i := range out {
		for int(buf[i]) >= limit {
			if _, err := rand.Read(buf[i : i+1]); err != nil {
				return "", fmt.Errorf("devicelink: random code: %w", err)
			}
		}

		out[i] = codeAlphabet[int(buf[i])%len(codeAlphabet)]
	}

	return string(out), nil
}

// Start creates a link.
func (s *Service) Start(ctx context.Context, client Client, deviceName string) (Started, error) {
	if !client.Valid() {
		return Started{}, errors.New("devicelink: unknown client")
	}

	deviceName = strings.TrimSpace(deviceName)
	if deviceName == "" {
		deviceName = string(client)
	}

	if len([]rune(deviceName)) > maxDevice {
		deviceName = string([]rune(deviceName)[:maxDevice])
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return Started{}, fmt.Errorf("devicelink: random device code: %w", err)
	}

	device := base64.RawURLEncoding.EncodeToString(raw)

	// A collision is astronomically unlikely over a ten-minute window, but
	// Add makes it a retry rather than a stolen link.
	for range 5 {
		code, err := randomCode()
		if err != nil {
			return Started{}, err
		}

		rec := record{
			Client: client, DeviceName: deviceName, UserCode: code, Status: Pending,
			ExpiresAt: s.now().Add(TTL),
		}

		body, err := json.Marshal(rec)
		if err != nil {
			return Started{}, fmt.Errorf("devicelink: encode: %w", err)
		}

		fresh, err := s.store.Add(ctx, userKey(code), []byte(hashOf(device)), TTL)
		if err != nil {
			return Started{}, fmt.Errorf("devicelink: reserve the user code: %w", err)
		}

		if !fresh {
			continue
		}

		if err := s.store.Set(ctx, deviceKey(hashOf(device)), body, TTL); err != nil {
			return Started{}, fmt.Errorf("devicelink: store the link: %w", err)
		}

		return Started{DeviceCode: device, UserCode: formatCode(code), ExpiresAt: rec.ExpiresAt}, nil
	}

	return Started{}, errors.New("devicelink: could not find a free user code")
}

func (s *Service) load(ctx context.Context, hash string) (record, bool, error) {
	body, ok, err := s.store.Get(ctx, deviceKey(hash))
	if err != nil || !ok {
		return record{}, false, err
	}

	var rec record
	if err := json.Unmarshal(body, &rec); err != nil {
		return record{}, false, fmt.Errorf("devicelink: decode: %w", err)
	}

	if !rec.ExpiresAt.After(s.now()) {
		return record{}, false, nil
	}

	return rec, true, nil
}

func (s *Service) byUserCode(ctx context.Context, code string) (string, record, bool, error) {
	hash, ok, err := s.store.Get(ctx, userKey(normalise(code)))
	if err != nil || !ok {
		return "", record{}, false, err
	}

	rec, ok, err := s.load(ctx, string(hash))

	return string(hash), rec, ok, err
}

// Lookup finds the pending link a person is being asked to approve.
func (s *Service) Lookup(ctx context.Context, userCode string) (Request, bool, error) {
	_, rec, ok, err := s.byUserCode(ctx, userCode)
	if err != nil || !ok || rec.Status != Pending {
		return Request{}, false, err
	}

	return Request{
		Client: rec.Client, DeviceName: rec.DeviceName, UserCode: formatCode(rec.UserCode),
		Abilities: Abilities(rec.Client), ExpiresAt: rec.ExpiresAt,
	}, true, nil
}

// ErrUnknown means no pending link has that code: it never existed, it
// expired, or it was already decided.
var ErrUnknown = errors.New("devicelink: no pending link has that code")

// Decide records the person's answer. userID is the signed-in account.
func (s *Service) Decide(ctx context.Context, userCode, userID string, approve bool) error {
	hash, rec, ok, err := s.byUserCode(ctx, userCode)
	if err != nil {
		return err
	}

	if !ok || rec.Status != Pending {
		return ErrUnknown
	}

	if approve {
		rec.Status, rec.UserID = Approved, userID
	} else {
		rec.Status = Denied
	}

	body, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("devicelink: encode: %w", err)
	}

	ttl := rec.ExpiresAt.Sub(s.now())
	if ttl <= 0 {
		return ErrUnknown
	}

	if err := s.store.Set(ctx, deviceKey(hash), body, ttl); err != nil {
		return fmt.Errorf("devicelink: store the decision: %w", err)
	}

	return nil
}

// Outcome is what a poll sees.
type Outcome struct {
	Status Status
	// UserID and Abilities are set only when Status is Approved and this call
	// won the right to claim the link.
	UserID     string
	Abilities  []string
	DeviceName string
}

// Poll reports a link's status. When it is approved, exactly one caller gets
// the outcome with the account and the abilities to issue a key for; the link
// is then spent. Anyone else, including a second poll that raced the first,
// sees it as still pending, so two keys are never made for one approval.
func (s *Service) Poll(ctx context.Context, deviceCode string) (Outcome, error) {
	if deviceCode == "" || len(deviceCode) > 128 {
		return Outcome{Status: Expired}, nil
	}

	hash := hashOf(deviceCode)

	rec, ok, err := s.load(ctx, hash)
	if err != nil {
		return Outcome{}, err
	}

	if !ok {
		return Outcome{Status: Expired}, nil
	}

	switch rec.Status {
	case Pending:
		return Outcome{Status: Pending}, nil
	case Approved:
		// Claimed below: only one poller may take it.
	case Denied:
		s.spend(ctx, hash, rec.UserCode)

		return Outcome{Status: Denied}, nil
	case Expired:
		return Outcome{Status: Expired}, nil
	}

	won, err := s.store.Add(ctx, claimKey(hash), []byte("1"), TTL)
	if err != nil {
		return Outcome{}, fmt.Errorf("devicelink: claim the approval: %w", err)
	}

	if !won {
		return Outcome{Status: Pending}, nil
	}

	s.spend(ctx, hash, rec.UserCode)

	return Outcome{
		Status: Approved, UserID: rec.UserID, Abilities: Abilities(rec.Client), DeviceName: rec.DeviceName,
	}, nil
}

// spend removes a link that has been decided and delivered. A failed delete is
// harmless: the entries expire on their own.
func (s *Service) spend(ctx context.Context, hash, userCode string) {
	_ = s.store.Delete(ctx, deviceKey(hash), userKey(userCode))
}
