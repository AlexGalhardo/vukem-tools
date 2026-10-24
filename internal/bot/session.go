package bot

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/AlexGalhardo/vukem-tools/clients/apiclient"
)

// session is one signed-in person. It holds the calls made with their key, and
// no copy of the key itself: only the transport inside Session ever sees it.
type session struct {
	Session
	name, email string
	expires     time.Time
}

// login is a sign-in in progress, watched by a goroutine until the person
// answers or the link expires.
type login struct {
	cancel context.CancelFunc
}

// sessionOf returns a person's live session, dropping one that expired.
func (b *Bot) sessionOf(user int64) (*session, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	s, ok := b.sessions[user]
	if !ok {
		return nil, false
	}

	if !b.now().Before(s.expires) {
		delete(b.sessions, user)

		return nil, false
	}

	return s, true
}

func (b *Bot) storeSession(user int64, s *session) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, replacing := b.sessions[user]; !replacing && len(b.sessions) >= sessionLimit {
		// Full: forget the session that expires soonest. A bot that refused
		// new sign-ins at the limit would let anybody who filled it lock out
		// everybody else.
		var (
			victim int64
			soon   time.Time
			found  bool
		)

		for id, other := range b.sessions {
			if !found || other.expires.Before(soon) {
				victim, soon, found = id, other.expires, true
			}
		}

		delete(b.sessions, victim)
	}

	b.sessions[user] = s
}

func (b *Bot) dropSession(user int64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	delete(b.sessions, user)
	delete(b.flows, user)
}

// offerLogin shows the signed-out menu: one button.
func (b *Bot) offerLogin(ctx context.Context, id who, messageID int64, intro string) {
	text := "Tools: convert, compress and fix files from this chat.\n\nSign in to your account to start."
	if intro != "" {
		text = intro + "\n\n" + text
	}

	b.show(ctx, id.chat, messageID, text, &Keyboard{Rows: [][]Button{{{Text: "Sign in", Data: "login"}}}})
}

// startLogin asks the server for a sign-in link and shows it.
//
// The person never types a password or a key into the chat. They open a page
// of the web app, where they are already signed in or sign in as usual, check
// that the code on it matches the one here, and press Connect; the bot
// collects its key by itself.
func (b *Bot) startLogin(ctx context.Context, id who, messageID int64, label string) {
	b.cancelLogin(id.user)

	name := plain(label, 40)

	body := apiclient.DevicelinksStartBody{Client: apiclient.DevicelinksStartBodyClientTelegram}
	if name != "" {
		body.DeviceName = &name
	}

	started, err := b.links.DevicelinksStart(ctx, body)
	if err != nil {
		b.show(
			ctx,
			id.chat,
			messageID,
			apiMessage(err),
			&Keyboard{Rows: [][]Button{{{Text: "Try again", Data: "login"}}}},
		)

		return
	}

	link := startedLink{
		DeviceCode: valueOr(started.DeviceCode, ""),
		UserCode:   valueOr(started.UserCode, ""),
		URL:        valueOr(started.VerificationURL, ""),
		ExpiresIn:  valueOr(started.ExpiresIn, 0),
		Interval:   valueOr(started.Interval, 0),
	}

	if err := linkSchema.Check(link); err != nil || !sameHost(link.URL, b.base) {
		b.log.ErrorContext(ctx, "bot: the server sent a sign-in link the bot refuses", "err", err)
		b.show(ctx, id.chat, messageID, "The server sent something I did not expect. Try again later.", nil)

		return
	}

	text := "Sign in\n\n" +
		"1. Press the button below and sign in if asked.\n" +
		"2. Check that the page shows this code: " + link.UserCode + "\n" +
		"3. Press Connect.\n\n" +
		"I carry on here by myself. The link works for " + humanSeconds(link.ExpiresIn) + "."

	cancel := []Button{{Text: "Cancel", Data: "cancel"}}

	// Telegram refuses a button whose address is not a public https one, which
	// is what a local test server is. The link then goes in the text instead,
	// so signing in still works; only the convenience is lost.
	shown, err := b.showTry(ctx, id.chat, messageID, text, &Keyboard{Rows: [][]Button{
		{{Text: "Open the sign-in page", URL: link.URL}}, cancel,
	}})
	if err != nil {
		shown, _ = b.showTry(ctx, id.chat, messageID, text+"\n\n"+link.URL, &Keyboard{Rows: [][]Button{cancel}})
	}

	watch, stop := context.WithCancel(ctx)

	b.mu.Lock()
	if len(b.logins) >= loginLimit {
		for _, other := range b.logins {
			other.cancel()
		}

		clear(b.logins)
	}

	b.logins[id.user] = &login{cancel: stop}
	b.mu.Unlock()

	b.wg.Go(func() {
		defer stop()

		b.watchLogin(watch, id, shown, link)
	})
}

func (b *Bot) cancelLogin(user int64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if l, ok := b.logins[user]; ok {
		l.cancel()
		delete(b.logins, user)
	}
}

// watchLogin polls until the person decides or the link expires.
func (b *Bot) watchLogin(ctx context.Context, id who, messageID int64, link startedLink) {
	// The server says how often to ask; only a configured override (a test, a
	// deployment that knows better) goes faster.
	interval := time.Duration(link.Interval) * time.Second
	if b.loginEvery > 0 {
		interval = b.loginEvery
	}

	deadline := b.now().Add(time.Duration(link.ExpiresIn) * time.Second)
	failures := 0

	retry := []Button{{Text: "Sign in again", Data: "login"}}

	for {
		if !sleep(ctx, interval) {
			return // cancelled: a newer sign-in, a Cancel press, or shutdown
		}

		if !b.now().Before(deadline) {
			b.endLogin(id.user)
			b.show(ctx, id.chat, messageID, "That sign-in link expired.", &Keyboard{Rows: [][]Button{retry}})

			return
		}

		token, err := b.links.DevicelinksToken(ctx, apiclient.DevicelinksTokenBody{DeviceCode: link.DeviceCode})
		if err != nil {
			failures++
			if failures >= 5 {
				b.endLogin(id.user)
				b.show(ctx, id.chat, messageID, apiMessage(err), &Keyboard{Rows: [][]Button{retry}})

				return
			}

			continue
		}

		failures = 0

		switch valueOr(token.Status, "") {
		case "pending":
			continue
		case "approved":
			b.finishLogin(ctx, id, messageID, token)

			return
		case "denied":
			b.endLogin(id.user)
			b.show(
				ctx,
				id.chat,
				messageID,
				"You refused the connection, so nothing was connected.",
				&Keyboard{Rows: [][]Button{retry}},
			)

			return
		default:
			b.endLogin(id.user)
			b.show(ctx, id.chat, messageID, "That sign-in link expired.", &Keyboard{Rows: [][]Button{retry}})

			return
		}
	}
}

func (b *Bot) endLogin(user int64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	delete(b.logins, user)
}

// finishLogin turns an approved link's key into a session.
func (b *Bot) finishLogin(ctx context.Context, id who, messageID int64, token apiclient.LinkToken) {
	defer b.endLogin(id.user)

	fail := func(text string) {
		b.show(ctx, id.chat, messageID, text, &Keyboard{Rows: [][]Button{{{Text: "Sign in again", Data: "login"}}}})
	}

	key := issuedKey{Token: valueOr(token.Token, "")}
	if err := keySchema.Check(key); err != nil {
		b.log.ErrorContext(ctx, "bot: the server sent a key the bot refuses", "err", err)
		fail("The server sent something I did not expect. Try again later.")

		return
	}

	calls, err := b.connect(key.Token)
	if err != nil {
		b.log.ErrorContext(ctx, "bot: could not build the API calls", "err", err)
		fail("I could not use that key. Try again later.")

		return
	}

	// One call proves the key works and tells the menu whose it is.
	me, err := calls.API.Me(ctx)
	if err != nil {
		fail(apiMessage(err))

		return
	}

	who := account{Name: plain(valueOr(me.Name, ""), 100), Email: plain(valueOr(me.Email, ""), 200)}
	if err := accountSchema.Check(who); err != nil {
		b.log.ErrorContext(ctx, "bot: the server described the account strangely", "err", err)
		fail("The server sent something I did not expect. Try again later.")

		return
	}

	expires := b.now().Add(30 * 24 * time.Hour)
	if token.ExpiresAt != nil {
		expires = *token.ExpiresAt
	}

	b.storeSession(id.user, &session{Session: calls, name: who.Name, email: who.Email, expires: expires})

	b.showMain(ctx, id, messageID)
}

// logout revokes the key and forgets the session.
func (b *Bot) logout(ctx context.Context, id who, messageID int64) {
	if s, ok := b.sessionOf(id.user); ok {
		// The key is revoked on the server, not just forgotten here: a key
		// that lingered in the account would outlive the person's decision to
		// sign out.
		if err := s.API.TokensDestroy(ctx); err != nil && !isUnauthorised(err) {
			b.log.WarnContext(ctx, "bot: could not revoke a key on sign-out", "err", err)
		}
	}

	b.cancelLogin(id.user)
	b.dropSession(id.user)

	b.offerLogin(ctx, id, messageID, "You are signed out and the key was revoked.")
}

func isUnauthorised(err error) bool {
	var apiErr *apiclient.Error

	return errors.As(err, &apiErr) && apiErr.Status == http.StatusUnauthorized
}

// lostSession handles a 401 in the middle of anything: the key was revoked
// elsewhere or expired. The session goes and the person is offered the way back
// in.
func (b *Bot) lostSession(ctx context.Context, id who, messageID int64) {
	b.dropSession(id.user)
	b.offerLogin(ctx, id, messageID, "You were signed out, so I cannot do that.")
}

// label is what a signed-in person's key is called in the account's key list.
func label(from *Sender) string {
	switch {
	case from == nil:
		return ""
	case from.Username != "":
		return "@" + from.Username
	default:
		return strings.TrimSpace(from.FirstName)
	}
}
