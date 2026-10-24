// Package bot is the Telegram front end: sign in once, then pick a tool from a
// menu, send a file or a link, and receive the result.
//
// It is a client of this service's own HTTP API and nothing more. Each person
// signs in to their own account with a link the server approves (see
// app/devicelink), so every call the bot makes carries that person's own key,
// limited to what the bot's menus use. A compromised bot process can do what
// the keys it currently holds can do, no more, and a key is one revoke away
// under API keys in the account. Reaching into the pipeline directly would
// have been less code and would have given the bot the whole database.
//
// Three rules shape the code. Everything that comes in (a message, a button, a
// file) and everything that comes back (a conversion, a link, a usage figure)
// is checked with vukem's z before it is used; see validate.go. Everything the
// bot says is plain text, because file names and titles come from outside.
// And the bot only ever offers what fits a chat: one file or a short list of
// files and at most one choice made with a button; see tools.go.
package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AlexGalhardo/vukem/z"
)

// Options configures a Bot. Telegram, Links, Connect and APIBaseURL are
// required.
type Options struct {
	// Telegram is the Bot API.
	Telegram Telegram
	// Links starts and collects the sign-in links.
	Links Links
	// Connect builds the API calls for one person's key. It is a function so
	// a test can hand back a fake and so the key stays inside this package's
	// session store: nothing else holds it.
	Connect func(key string) (Session, error)
	// APIBaseURL is where the API lives. Links the server sends back (to sign
	// in, to download a result) must be on this host, or the bot refuses to
	// forward them.
	APIBaseURL string
	// AllowedUsers, when not empty, restricts who may use the bot at all.
	// Empty means anybody may try to sign in: the sign-in is the gate, and a
	// stranger who signs in reaches only their own account.
	AllowedUsers []int64
	// PollTimeout is how long a long-poll waits. Zero means 50 seconds.
	PollTimeout time.Duration
	// PollInterval is how often a conversion is polled for. Zero means two
	// seconds.
	PollInterval time.Duration
	// LoginInterval overrides how often a sign-in link is polled. Zero uses
	// what the server asks for.
	LoginInterval time.Duration
	// ConversionTimeout bounds how long the bot waits for one conversion
	// before telling the user to check back. Zero means 20 minutes.
	ConversionTimeout time.Duration
	// MaxConversions bounds how many conversions the bot watches at once. Zero
	// means 8.
	MaxConversions int
	Log            *slog.Logger
	Now            func() time.Time
}

// Bot is the Telegram front end.
type Bot struct {
	tg      Telegram
	links   Links
	connect func(key string) (Session, error)
	base    string
	allowed []int64

	pollTimeout  time.Duration
	pollInterval time.Duration
	loginEvery   time.Duration
	conversion   time.Duration

	log *slog.Logger
	now func() time.Time

	// watching bounds the conversions followed at once.
	watching chan struct{}
	// wg tracks the goroutines the bot starts, so a shutdown and a test can
	// wait for them.
	wg sync.WaitGroup

	// mu guards the three maps. They are in memory on purpose: a key is the
	// most sensitive thing the bot holds, a restart costs one click to sign
	// in again, and nothing secret is written to disk.
	mu       sync.Mutex
	sessions map[int64]*session
	logins   map[int64]*login
	flows    map[int64]*flow
}

// Limits on what the bot remembers, so a stranger pressing buttons cannot
// grow the heap. They are far above what the people using one bot need.
const (
	sessionLimit = 4096
	loginLimit   = 256
	flowLimit    = 1024
	flowTTL      = 30 * time.Minute
)

// New returns a bot.
func New(o Options) (*Bot, error) {
	switch {
	case o.Telegram == nil:
		return nil, errors.New("bot: a Telegram client is required")
	case o.Links == nil:
		return nil, errors.New("bot: a links client is required")
	case o.Connect == nil:
		return nil, errors.New("bot: a Connect function is required")
	case strings.TrimSpace(o.APIBaseURL) == "":
		return nil, errors.New("bot: the API base URL is required")
	}

	if o.Log == nil {
		o.Log = slog.Default()
	}

	if o.Now == nil {
		o.Now = time.Now
	}

	if o.PollTimeout <= 0 {
		o.PollTimeout = 50 * time.Second
	}

	if o.PollInterval <= 0 {
		o.PollInterval = 2 * time.Second
	}

	if o.ConversionTimeout <= 0 {
		o.ConversionTimeout = 20 * time.Minute
	}

	if o.MaxConversions <= 0 {
		o.MaxConversions = 8
	}

	return &Bot{
		tg:           o.Telegram,
		links:        o.Links,
		connect:      o.Connect,
		base:         strings.TrimRight(o.APIBaseURL, "/"),
		allowed:      slices.Clone(o.AllowedUsers),
		pollTimeout:  o.PollTimeout,
		pollInterval: o.PollInterval,
		loginEvery:   o.LoginInterval,
		conversion:   o.ConversionTimeout,
		log:          o.Log,
		now:          o.Now,
		watching:     make(chan struct{}, o.MaxConversions),
		sessions:     make(map[int64]*session),
		logins:       make(map[int64]*login),
		flows:        make(map[int64]*flow),
	}, nil
}

// Wait blocks until every goroutine the bot started has finished: the
// sign-in watchers and the conversions. Run calls it on shutdown, and tests
// call it before they read what was sent.
func (b *Bot) Wait() { b.wg.Wait() }

// Allows reports whether a sender may use the bot.
//
// A nil sender is an update Telegram sent without one, which happens for a
// channel post and for an anonymous administrator. It is refused rather than
// read as user 0, because user 0 compared against a list of real ids is a
// comparison that happens to fail today and would start succeeding the moment
// somebody put a 0 in the configuration.
func (b *Bot) Allows(from *Sender) bool {
	if from == nil || from.ID <= 0 || from.IsBot {
		return false
	}

	return len(b.allowed) == 0 || slices.Contains(b.allowed, from.ID)
}

// Run polls Telegram until ctx is cancelled.
func (b *Bot) Run(ctx context.Context) error {
	var offset int64

	b.log.InfoContext(ctx, "bot: polling", "restricted", len(b.allowed) > 0, "abilities", Abilities())

	defer b.Wait()

	for {
		updates, keepGoing := b.poll(ctx, offset)
		if !keepGoing {
			return nil
		}

		for _, update := range updates {
			// Acknowledged by advancing the offset whether it was handled or
			// not: an update that cannot be handled must not be redelivered
			// forever.
			offset = max(offset, update.ID+1)

			b.Handle(ctx, update)
		}
	}
}

// poll fetches one batch of updates. It reports false only when the bot
// should stop, which is only ever because the context ended: a failed poll is
// almost always Telegram being briefly unreachable, and backing off beats
// exiting and having the platform restart the process.
func (b *Bot) poll(ctx context.Context, offset int64) ([]Update, bool) {
	if ctx.Err() != nil {
		return nil, false
	}

	updates, err := b.tg.GetUpdates(ctx, offset, b.pollTimeout)

	// The context is checked before the error: a cancelled poll fails, and
	// reporting a shutdown as an outage would put a warning in the log of
	// every clean deploy.
	if ctx.Err() != nil {
		return nil, false
	}

	if err != nil {
		b.log.WarnContext(ctx, "bot: the poll failed, retrying", "err", err)

		return nil, sleep(ctx, b.pollInterval)
	}

	return updates, true
}

// sleep waits for d, reporting false when ctx ended first.
func sleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// Handle processes one update. It is exported so a test can drive a whole
// conversation without a poll loop.
func (b *Bot) Handle(ctx context.Context, update Update) {
	switch {
	case update.Callback != nil:
		b.handleButton(ctx, update.Callback)
	case update.Message != nil:
		b.handleMessage(ctx, update.Message)
	default:
		b.log.DebugContext(ctx, "bot: update with nothing to handle", "update", update.ID)
	}
}

// refused is what an unauthorised sender is told. It says the bot is private
// rather than "you are not on the list": the second confirms the bot is the
// one the sender was looking for.
const refused = "This bot is private."

// who is the flattened identity of an update, once it has passed the schema.
type who struct {
	chat, user int64
	// label names the person in their account's key list.
	label string
}

// admit checks that an update came from a person in a private chat and may use
// the bot. It answers the sender itself when it refuses, and reports whether
// to go on.
func (b *Bot) admit(ctx context.Context, chat *Chat, from *Sender, probe func() error) (who, bool) {
	if chat == nil {
		return who{}, false // nowhere to answer
	}

	if !b.Allows(from) {
		b.log.WarnContext(ctx, "bot: refused an update", "chat", chat.ID, "has_sender", from != nil)
		b.say(ctx, chat.ID, refused)

		return who{}, false
	}

	if err := probe(); err != nil {
		if issues, isIssues := z.AsIssues(err); isIssues && issues.First("chat_type") != "" {
			b.say(
				ctx,
				chat.ID,
				"Open a private chat with me to use the tools. In a group, anybody could press your buttons.",
			)

			return who{}, false
		}

		b.log.WarnContext(ctx, "bot: an update failed validation", "chat", chat.ID, "err", err)
		b.say(ctx, chat.ID, "I could not read that. Use the buttons.")

		return who{}, false
	}

	return who{chat: chat.ID, user: from.ID, label: label(from)}, true
}

// handleMessage handles a text message or a file.
func (b *Bot) handleMessage(ctx context.Context, msg *Message) {
	if msg.Chat == nil || msg.From == nil {
		if msg.Chat != nil {
			b.say(ctx, msg.Chat.ID, refused)
		}

		return
	}

	id, ok := b.admit(ctx, msg.Chat, msg.From, func() error {
		return inboundSchema.Check(inboundMessage{
			ChatID: msg.Chat.ID, ChatType: msg.Chat.Type, UserID: msg.From.ID, Text: msg.Text,
		})
	})
	if !ok {
		return
	}

	if file, has := attachmentOf(msg); has {
		b.handleFile(ctx, id, file)

		return
	}

	text := strings.TrimSpace(msg.Text)

	// A pasted link is the natural way to ask for a download, so it works
	// from anywhere: it starts that flow and carries the link along.
	if link, err := checkLink(text); err == nil {
		b.handleLink(ctx, id, link)

		return
	}

	// Everything else, including /start, gets the menu. The bot has no
	// commands to learn: the menu is the interface.
	b.showMain(ctx, id, 0)
}

// handleButton handles a press on any keyboard.
func (b *Bot) handleButton(ctx context.Context, cb *CallbackQuery) {
	if cb.Message == nil || cb.Message.Chat == nil {
		return // nowhere to answer
	}

	// Acknowledged first: Telegram shows a spinner on the button until this
	// returns, and the work can take minutes.
	b.ack(ctx, cb.ID, "")

	id, ok := b.admit(ctx, cb.Message.Chat, cb.From, func() error {
		return buttonSchema.Check(inboundButton{
			ChatID: cb.Message.Chat.ID, ChatType: cb.Message.Chat.Type, UserID: cb.From.ID, Data: cb.Data,
		})
	})
	if !ok {
		return
	}

	b.press(ctx, id, cb.Message.ID, cb.Data)
}

// ack acknowledges a button press.
func (b *Bot) ack(ctx context.Context, callbackID, text string) {
	if err := b.tg.AnswerCallback(ctx, callbackID, text); err != nil {
		b.log.WarnContext(ctx, "bot: could not acknowledge a button press", "err", err)
	}
}

// say sends plain text, logging a failure rather than returning it: there is
// nobody above Handle to tell, and a Telegram outage is not something the next
// update should inherit.
func (b *Bot) say(ctx context.Context, chatID int64, text string) {
	if _, err := b.tg.SendMessage(ctx, chatID, text, nil); err != nil {
		b.log.ErrorContext(ctx, "bot: could not send a message", "chat", chatID, "err", err)
	}
}

// show puts text and a keyboard on screen. When there is a menu message to
// reuse it edits that one, so navigating does not fill the chat with copies;
// when the edit fails (the message is too old, or unchanged) it sends a new
// one. It returns the id of the message now showing.
func (b *Bot) show(ctx context.Context, chatID, messageID int64, text string, kb *Keyboard) int64 {
	shown, err := b.showTry(ctx, chatID, messageID, text, kb)
	if err != nil {
		b.log.ErrorContext(ctx, "bot: could not show a menu", "chat", chatID, "err", err)
	}

	return shown
}

// showTry is show with the failure returned, for the one caller that has a
// fallback.
func (b *Bot) showTry(ctx context.Context, chatID, messageID int64, text string, kb *Keyboard) (int64, error) {
	if messageID != 0 {
		if err := b.tg.EditMessage(ctx, chatID, messageID, text, kb); err == nil {
			return messageID, nil
		}
	}

	sent, err := b.tg.SendMessage(ctx, chatID, text, kb)
	if err != nil {
		return messageID, err
	}

	return sent, nil
}

// attachmentOf extracts the file a message carries, whatever its kind.
func attachmentOf(msg *Message) (inboundFile, bool) {
	switch {
	case msg.Document != nil:
		return fromAttachment(msg.Document, ""), true
	case msg.Audio != nil:
		return fromAttachment(msg.Audio, "audio"), true
	case msg.Video != nil:
		return fromAttachment(msg.Video, "video.mp4"), true
	case msg.Voice != nil:
		return fromAttachment(msg.Voice, "voice.ogg"), true
	case len(msg.Photo) > 0:
		// Telegram sends the same picture at several sizes; the last is the
		// largest.
		p := msg.Photo[len(msg.Photo)-1]

		return inboundFile{FileID: p.FileID, FileName: "photo.jpg", MimeType: "image/jpeg", Size: p.FileSize}, true
	}

	return inboundFile{}, false
}

func fromAttachment(a *Attachment, fallbackName string) inboundFile {
	name := a.FileName
	if name == "" {
		name = fallbackName
	}

	return inboundFile{FileID: a.FileID, FileName: name, MimeType: a.MimeType, Size: a.FileSize}
}

// valueOr dereferences an optional field from the generated client, which
// makes every field a pointer so an absent one can be told from an empty one.
func valueOr[T any](p *T, fallback T) T {
	if p == nil {
		return fallback
	}

	return *p
}

// money-free helpers for the sentences the bot writes.

func humanSeconds(s int64) string {
	switch {
	case s < 60:
		return strconv.FormatInt(s, 10) + "s"
	case s < 3600:
		return fmt.Sprintf("%dm %02ds", s/60, s%60)
	default:
		return fmt.Sprintf("%dh %02dm", s/3600, (s%3600)/60)
	}
}
