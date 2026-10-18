// Package bot is the Telegram front end: paste a URL, choose a format,
// receive the file.
//
// It is a client of this service's own HTTP API rather than of the conversion
// pipeline. That is the whole design. The bot runs as its own process, very
// likely on its own deploy, and it holds an API key that carries exactly one
// ability — so a compromised bot can start a download and do nothing else:
// not read another account's conversions, not issue a key, not touch an
// upload. Reaching into internal/jobs directly would have been less code and
// would have given the bot the whole database.
//
// Two things here are security decisions rather than features. The allow-list
// fails closed: no configured users means nobody is allowed, and an update
// with no sender — Telegram omits it for a channel post and for an anonymous
// administrator — is refused rather than compared against a zero id. And
// every message the bot sends is plain text, because the titles it quotes
// come from a remote page.
package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/AlexGalhardo/vukem-tools/app/abilities"
	"github.com/AlexGalhardo/vukem-tools/clients/apiclient"
	"github.com/AlexGalhardo/vukem-tools/internal/httpx"
)

// API is this service's HTTP API, narrowed to what the bot uses.
//
// *apiclient.Client satisfies it. Listing the five methods rather than taking
// the generated client whole is what makes the ability assertion checkable:
// the bot's key needs exactly the abilities these five routes require, and a
// sixth method would have to be added here first.
type API interface {
	ToolsVideoDownloadMetadata(
		ctx context.Context,
		body apiclient.ToolsVideoDownloadMetadataBody,
	) (apiclient.MediaView, error)
	ToolsVideoDownload(ctx context.Context, body apiclient.ToolsVideoDownloadBody) (apiclient.ConversionView, error)
	ConversionsShow(ctx context.Context, id string) (apiclient.ConversionView, error)
	ConversionsResult(ctx context.Context, id string) (apiclient.ResultList, error)
	Capabilities(ctx context.Context) (apiclient.CapabilitiesView, error)
}

// Abilities is every ability the bot's API key needs, and nothing more.
//
// Only the submit route carries an ability at all: reading a conversion and
// its result are about the caller's own work, and the metadata probe is
// guarded by the same ability as the download it precedes. A key minted with
// this list can start a download and cannot do anything else with the API.
//
// tests assert this against the live route table, so adding a route to the
// bot without widening the key — or widening the key without using it —
// fails.
func Abilities() []string { return []string{abilities.VideoDownload} }

// Options configures a Bot. Telegram, API and AllowedUsers are required.
type Options struct {
	// Telegram is the Bot API.
	Telegram Telegram
	// API is this service's HTTP API.
	API API
	// AllowedUsers are the Telegram user ids that may use the bot. An empty
	// list means nobody: the bot refuses every update and says why at
	// startup. That is the fail-closed default, and it is the right one —
	// a download bot with no allow-list is a download bot for the internet.
	AllowedUsers []int64
	// DeployTarget names this bot's own deploy, for the status command.
	DeployTarget string
	// APIDeployTarget reports what the API's last response said it was. It
	// is a function because the value arrives on a response header, so it is
	// not known until a call has been made.
	APIDeployTarget func() string
	// PollTimeout is how long a long-poll waits. Zero means 50 seconds.
	PollTimeout time.Duration
	// PollInterval is how often a finished conversion is polled for.
	// Zero means two seconds.
	PollInterval time.Duration
	// ConversionTimeout bounds how long the bot waits for one conversion
	// before telling the user to check back. Zero means 20 minutes, which
	// matches the tool's own timeout.
	ConversionTimeout time.Duration
	Log               *slog.Logger
	Now               func() time.Time
}

// Bot is the Telegram front end.
type Bot struct {
	tg      Telegram
	api     API
	allowed []int64

	deploy    string
	apiDeploy func() string

	pollTimeout  time.Duration
	pollInterval time.Duration
	conversion   time.Duration

	log *slog.Logger
	now func() time.Time

	// pending holds the URL each chat last pasted, until a format is chosen.
	//
	// In memory rather than in the database: it is a half-finished sentence,
	// not a record, and losing it on a restart costs the user one paste. It
	// is bounded, because a map keyed by anything a client controls is a
	// memory leak with extra steps — even behind an allow-list, where the
	// bound is small enough that it can only ever be reached by somebody
	// pasting URLs on purpose.
	mu      sync.Mutex
	pending map[int64]pendingURL
}

// pendingURL is a URL waiting for a format choice.
type pendingURL struct {
	url   string
	title string
	at    time.Time
}

// pendingLimit bounds the pending map, and pendingTTL is how long a choice
// stays open. A user who pastes a URL and wanders off should not hold an
// entry for the life of the process.
const (
	pendingLimit = 256
	pendingTTL   = 30 * time.Minute
)

// New returns a bot.
func New(o Options) (*Bot, error) {
	switch {
	case o.Telegram == nil:
		return nil, errors.New("bot: a Telegram client is required")
	case o.API == nil:
		return nil, errors.New("bot: an API client is required")
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

	if o.APIDeployTarget == nil {
		o.APIDeployTarget = func() string { return "unknown" }
	}

	if len(o.AllowedUsers) == 0 {
		// Loud, and not fatal: the bot runs and refuses everybody, which is
		// a configuration an operator can see and fix. Starting with an open
		// bot because the list was empty is the failure that cannot be
		// undone.
		o.Log.Warn("bot: no allowed users configured, every update will be refused")
	}

	return &Bot{
		tg:           o.Telegram,
		api:          o.API,
		allowed:      slices.Clone(o.AllowedUsers),
		deploy:       o.DeployTarget,
		apiDeploy:    o.APIDeployTarget,
		pollTimeout:  o.PollTimeout,
		pollInterval: o.PollInterval,
		conversion:   o.ConversionTimeout,
		log:          o.Log,
		now:          o.Now,
		pending:      make(map[int64]pendingURL),
	}, nil
}

// Allows reports whether a sender may use the bot.
//
// A nil sender is an update Telegram sent without one, which happens for a
// channel post and for an anonymous administrator. It is refused rather than
// read as user 0, because user 0 compared against an allow-list of real ids
// is a comparison that happens to fail today and would start succeeding the
// moment somebody put a 0 in the configuration.
func (b *Bot) Allows(from *Sender) bool {
	if from == nil || from.ID == 0 {
		return false
	}

	return slices.Contains(b.allowed, from.ID)
}

// Run polls Telegram until ctx is cancelled.
func (b *Bot) Run(ctx context.Context) error {
	var offset int64

	b.log.InfoContext(ctx, "bot: polling", "allowed_users", len(b.allowed), "deploy", b.deploy)

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

// poll fetches one batch of updates.
//
// It reports false only when the bot should stop, which is only ever because
// the context ended: a failed poll is almost always Telegram being briefly
// unreachable, and backing off and carrying on beats exiting and having the
// platform restart the process. Returning "keep going" rather than an error
// is what keeps that distinction in one place instead of in the loop.
func (b *Bot) poll(ctx context.Context, offset int64) ([]Update, bool) {
	if ctx.Err() != nil {
		return nil, false
	}

	updates, err := b.tg.GetUpdates(ctx, offset, b.pollTimeout)

	// The context is checked before the error: a cancelled poll fails, and
	// reporting a shutdown as a Telegram outage would put a warning in the
	// log of every clean deploy.
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
		b.handleCallback(ctx, update.Callback)
	case update.Message != nil:
		b.handleMessage(ctx, update.Message)
	default:
		// An update kind that was not asked for. Nothing to do, and nothing
		// worth logging at a level anybody reads.
		b.log.DebugContext(ctx, "bot: update with nothing to handle", "update", update.ID)
	}
}

// refused is what an unauthorised sender is told.
//
// It says the bot is private rather than "you are not on the list": the
// second confirms the bot is the one the sender was looking for, which is
// information worth not giving away.
const refused = "This bot is private."

// handleMessage handles a text message.
func (b *Bot) handleMessage(ctx context.Context, msg *Message) {
	if msg.Chat == nil {
		return // nowhere to answer
	}

	if !b.Allows(msg.From) {
		b.log.WarnContext(ctx, "bot: refused an update", "chat", msg.Chat.ID, "has_sender", msg.From != nil)
		b.say(ctx, msg.Chat.ID, refused)

		return
	}

	text := strings.TrimSpace(msg.Text)

	switch {
	case text == "":
		b.say(ctx, msg.Chat.ID, "Send me a link to a video, or /help.")
	case strings.HasPrefix(text, "/start"), strings.HasPrefix(text, "/help"):
		b.say(ctx, msg.Chat.ID, helpText)
	case strings.HasPrefix(text, "/status"):
		b.status(ctx, msg.Chat.ID)
	case strings.HasPrefix(text, "/"):
		b.say(ctx, msg.Chat.ID, "I do not know that command. Try /help.")
	default:
		b.offerFormats(ctx, msg.Chat.ID, text)
	}
}

// helpText is what /help and /start answer.
const helpText = "Send me a link to a video on YouTube, Instagram, X or TikTok and I will " +
	"offer you the formats I can fetch.\n\n" +
	"/status — what this bot and the API are running\n" +
	"/help — this message\n\n" +
	"Downloading may breach the platform's terms of service, and you are responsible for what " +
	"you download."

// say sends a message, logging a failure rather than returning it: there is
// nobody above Handle to tell, and a Telegram outage is not something the
// next update should inherit.
func (b *Bot) say(ctx context.Context, chatID int64, text string) {
	if err := b.tg.SendMessage(ctx, chatID, text, nil); err != nil {
		b.log.ErrorContext(ctx, "bot: could not send a message", "chat", chatID, "err", err)
	}
}

// status answers /status with what this bot and the API are running.
func (b *Bot) status(ctx context.Context, chatID int64) {
	deploy := b.deploy
	if deploy == "" {
		deploy = "unknown"
	}

	caps, err := b.api.Capabilities(ctx)
	if err != nil {
		// The bot is up, which is itself worth reporting: "the API is not
		// answering" is the single most useful thing /status can say.
		b.say(ctx, chatID, "Bot deploy: "+deploy+"\nAPI: not answering ("+apiMessage(err)+")")

		return
	}

	var out strings.Builder

	out.WriteString("Bot deploy: " + deploy + "\n")
	out.WriteString("API deploy: " + b.apiDeploy() + "\n")
	out.WriteString("Tools: " + strings.Join(caps.Tools, ", ") + "\n")

	var engines []string

	for _, e := range caps.Engines {
		name := valueOr(e.Name, "?")
		if e.Present != nil && *e.Present {
			engines = append(engines, name+" "+firstWord(valueOr(e.Version, "")))
		} else {
			engines = append(engines, name+" missing")
		}
	}

	out.WriteString("Engines: " + strings.Join(engines, ", "))

	b.say(ctx, chatID, out.String())
}

// firstWord is the first space-separated word of a version string.
//
// ffmpeg reports its version as a whole sentence with a copyright notice, and
// a status message is not the place for it.
func firstWord(s string) string {
	fields := strings.Fields(s)
	for _, f := range fields {
		// "ffmpeg version 7.1.5-..." — the word after "version" is the one.
		if f != "ffmpeg" && f != "ffprobe" && f != "version" {
			return f
		}
	}

	if len(fields) > 0 {
		return fields[0]
	}

	return "unknown"
}

// valueOr dereferences an optional field from the generated client, which
// makes every field a pointer so an absent one can be told from an empty one.
func valueOr[T any](p *T, fallback T) T {
	if p == nil {
		return fallback
	}

	return *p
}

// The callback_data values the format keyboard sends back. They are short
// because Telegram caps callback_data at 64 bytes, and they are matched
// exactly rather than parsed, so nothing a user can type reaches a request.
const (
	choiceVideoBest  = "v:best"
	choiceVideoSmall = "v:small"
	choiceAudio      = "a"
	choiceClips      = "c"
)

// offerFormats probes the URL and offers what can be fetched.
func (b *Bot) offerFormats(ctx context.Context, chatID int64, rawURL string) {
	media, err := b.api.ToolsVideoDownloadMetadata(ctx, apiclient.ToolsVideoDownloadMetadataBody{URL: rawURL})
	if err != nil {
		// The API already words this for a person — "that video is private",
		// "the platform refused this server" — so the bot repeats it rather
		// than inventing a second vocabulary for the same failures. That is
		// the ROADMAP's "errors in the same plain English as the web UI,
		// from the same taxonomy".
		b.say(ctx, chatID, apiMessage(err))

		return
	}

	if media.TooLarge != nil && *media.TooLarge {
		b.say(ctx, chatID, fmt.Sprintf(
			"That video is about %s, and this server downloads at most %s.",
			formatBytes(valueOr(media.EstimatedBytes, 0)),
			formatBytes(valueOr(media.MaxBytes, 0)),
		))

		return
	}

	title := valueOr(media.Title, "that video")

	b.remember(chatID, rawURL, title)

	summary := fmt.Sprintf("%s\nby %s · %s · about %s\n\nWhat would you like?",
		title,
		valueOr(media.Uploader, "unknown"),
		formatDuration(valueOr(media.DurationSeconds, 0)),
		formatBytes(valueOr(media.EstimatedBytes, 0)),
	)

	keyboard := &Keyboard{Rows: [][]Button{
		{
			{Text: "MP4, best", Data: choiceVideoBest},
			{Text: "MP4, smallest", Data: choiceVideoSmall},
		},
		{
			{Text: "MP3", Data: choiceAudio},
			{Text: "60-second clips", Data: choiceClips},
		},
	}}

	if err := b.tg.SendMessage(ctx, chatID, summary, keyboard); err != nil {
		b.log.ErrorContext(ctx, "bot: could not offer the formats", "chat", chatID, "err", err)
	}
}

// remember stores a chat's pending URL, evicting what has expired.
func (b *Bot) remember(chatID int64, rawURL, title string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := b.now()

	for id, p := range b.pending {
		if now.Sub(p.at) > pendingTTL {
			delete(b.pending, id)
		}
	}

	// Still full after the sweep: drop the oldest rather than grow. One user
	// pasting URLs in a loop must not be able to hold the heap.
	if len(b.pending) >= pendingLimit {
		oldest, found := int64(0), false

		for id, p := range b.pending {
			if !found || p.at.Before(b.pending[oldest].at) {
				oldest, found = id, true
			}
		}

		if found {
			delete(b.pending, oldest)
		}
	}

	b.pending[chatID] = pendingURL{url: rawURL, title: title, at: now}
}

// recall returns a chat's pending URL and removes it, so one choice starts
// one download.
func (b *Bot) recall(chatID int64) (pendingURL, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	p, ok := b.pending[chatID]
	if !ok {
		return pendingURL{}, false
	}

	delete(b.pending, chatID)

	if b.now().Sub(p.at) > pendingTTL {
		return pendingURL{}, false
	}

	return p, true
}

// handleCallback handles a press on the format keyboard.
func (b *Bot) handleCallback(ctx context.Context, cb *CallbackQuery) {
	if cb.Message == nil || cb.Message.Chat == nil {
		return // nowhere to answer
	}

	chatID := cb.Message.Chat.ID

	if !b.Allows(cb.From) {
		b.log.WarnContext(ctx, "bot: refused a button press", "chat", chatID, "has_sender", cb.From != nil)
		b.ack(ctx, cb.ID, refused)
		b.say(ctx, chatID, refused)

		return
	}

	// Acknowledged before the work starts: Telegram shows a spinner on the
	// button until this returns, and the work takes minutes.
	b.ack(ctx, cb.ID, "")

	kind, quality, clips, ok := choiceOf(cb.Data)
	if !ok {
		b.say(ctx, chatID, "I do not recognise that choice. Send the link again.")

		return
	}

	pending, ok := b.recall(chatID)
	if !ok {
		b.say(ctx, chatID, "That link has expired. Send it again.")

		return
	}

	b.download(ctx, chatID, pending, kind, quality, clips)
}

// ack acknowledges a button press.
func (b *Bot) ack(ctx context.Context, callbackID, text string) {
	if err := b.tg.AnswerCallback(ctx, callbackID, text); err != nil {
		b.log.WarnContext(ctx, "bot: could not acknowledge a button press", "err", err)
	}
}

// choiceOf maps a callback_data value onto the download options.
//
// The mapping is exhaustive and closed: an unrecognised value is refused
// rather than passed on, so the only strings that ever reach the API are the
// four this file defines.
func choiceOf(data string) (kind, quality string, clips, ok bool) {
	switch data {
	case choiceVideoBest:
		return "video", "best", false, true
	case choiceVideoSmall:
		return "video", "smallest", false, true
	case choiceAudio:
		return "audio", "best", false, true
	case choiceClips:
		return "video", "best", true, true
	default:
		return "", "", false, false
	}
}

// download submits the conversion, waits for it and delivers the result.
func (b *Bot) download(ctx context.Context, chatID int64, pending pendingURL, kind, quality string, clips bool) {
	b.say(ctx, chatID, "Working on it. This takes a minute or two.")

	conversion, err := b.api.ToolsVideoDownload(ctx, apiclient.ToolsVideoDownloadBody{
		Options: &apiclient.DownloadOptions{
			URL:     pending.url,
			Kind:    &kind,
			Quality: &quality,
			Clips:   &clips,
			// Compression is a separate choice the bot does not offer: a
			// chat wants the file now, and a second re-encode doubles the
			// wait for a saving nobody asked for.
			Compress: new(false),
		},
	})
	if err != nil {
		b.say(ctx, chatID, apiMessage(err))

		return
	}

	id := valueOr(conversion.ID, "")
	if id == "" {
		b.say(ctx, chatID, "The server accepted that but gave me nothing to follow. Try again.")

		return
	}

	final, err := b.wait(ctx, id)
	if err != nil {
		b.say(ctx, chatID, apiMessage(err))

		return
	}

	switch valueOr(final.Status, "") {
	case "finished":
		b.deliver(ctx, chatID, id, clips)
	case "cancelled":
		b.say(ctx, chatID, "That download was cancelled.")
	default:
		// The API words every failure for a person and puts it in
		// error_message, which is the same sentence the web interface shows.
		message := valueOr(final.ErrorMessage, "")
		if message == "" {
			message = "That did not work, and the server did not say why."
		}

		b.say(ctx, chatID, message)
	}
}

// wait polls the conversion until it ends.
//
// Polling rather than the event stream: the API publishes both, and a bot
// that cannot hold a connection open for twenty minutes across a deploy is
// exactly the client the polling endpoint exists for.
func (b *Bot) wait(ctx context.Context, id string) (apiclient.ConversionView, error) {
	deadline := b.now().Add(b.conversion)

	for {
		conversion, err := b.api.ConversionsShow(ctx, id)
		if err != nil {
			return apiclient.ConversionView{}, err
		}

		switch valueOr(conversion.Status, "") {
		case "finished", "error", "cancelled":
			return conversion, nil
		}

		if b.now().After(deadline) {
			return apiclient.ConversionView{}, errStillRunning
		}

		if !sleep(ctx, b.pollInterval) {
			return apiclient.ConversionView{}, ctx.Err()
		}
	}
}

// errStillRunning is reported when a conversion outlives the bot's patience.
// The work carries on; only the bot stops watching.
var errStillRunning = errors.New("the download is taking longer than I wait for")

// deliver sends the finished files.
//
// Clips go as separate videos, which is what the ROADMAP asks for and what
// anybody wanting to post one of them needs. The zip the web interface also
// produces is skipped: in a chat it is the least useful of the two, and
// sending both would upload everything twice.
func (b *Bot) deliver(ctx context.Context, chatID int64, id string, clips bool) {
	results, err := b.api.ConversionsResult(ctx, id)
	if err != nil {
		b.say(ctx, chatID, apiMessage(err))

		return
	}

	sent := 0

	for _, file := range results.Data {
		name := valueOr(file.Name, "file")
		link := valueOr(file.URL, "")
		mime := valueOr(file.Mime, "")

		if link == "" {
			continue
		}

		if clips && strings.HasSuffix(strings.ToLower(name), ".zip") {
			continue
		}

		if err := b.send(ctx, chatID, mime, link, name, valueOr(file.SizeBytes, 0)); err != nil {
			b.log.WarnContext(ctx, "bot: could not send a result", "chat", chatID, "err", err)

			// Telegram refuses a URL over its own 20 MB limit, and refuses
			// some types outright. The link is the honest fallback: the
			// result exists and expires in five minutes, so the user needs
			// it now rather than an apology.
			b.say(ctx, chatID, name+"\nToo large for Telegram to fetch, so here is the link. "+
				"It expires in a few minutes.\n"+link)
		}

		sent++
	}

	if sent == 0 {
		b.say(ctx, chatID, "That finished but produced nothing I can send.")

		return
	}

	if clips {
		b.say(ctx, chatID, fmt.Sprintf("That is all %d clips. The links expire in a few minutes.", sent))
	}
}

// send posts one file, choosing the method from its media type so a video
// arrives playable and an MP3 arrives as audio rather than as an attachment.
func (b *Bot) send(ctx context.Context, chatID int64, mime, link, name string, size int64) error {
	caption := name
	if size > 0 {
		caption = name + " · " + formatBytes(size)
	}

	switch {
	case strings.HasPrefix(mime, "video/"):
		return b.tg.SendVideo(ctx, chatID, link, caption)
	case strings.HasPrefix(mime, "audio/"):
		return b.tg.SendAudio(ctx, chatID, link, caption)
	default:
		return b.tg.SendDocument(ctx, chatID, link, caption)
	}
}

// apiMessage is the sentence to show for an API failure.
//
// The API's own problem detail is preferred, because it is already written
// for a person and comes from the same taxonomy the web interface renders —
// which is the point of the bot having no vocabulary of its own. A 422 names
// the field, so its first message is the useful one.
func apiMessage(err error) string {
	if err == nil {
		return ""
	}

	if errors.Is(err, errStillRunning) {
		return "That is taking longer than I wait for. It is still running: ask again in a few minutes."
	}

	var apiErr *apiclient.Error
	if !errors.As(err, &apiErr) {
		return "I could not reach the server. Try again shortly."
	}

	for _, messages := range apiErr.Problem.Errors {
		if len(messages) > 0 {
			return messages[0]
		}
	}

	if apiErr.Problem.Detail != "" {
		return apiErr.Problem.Detail
	}

	if apiErr.Status == http.StatusTooManyRequests {
		return "Too many requests for now. Try again in a minute."
	}

	return "That did not work. Try again shortly."
}

// NewAPI returns an API client that authenticates with key and records the
// deploy target of every answer.
//
// The deploy target arrives on a response header, which the generated client
// does not expose, so it is captured by a transport instead. That is also
// why Options.APIDeployTarget is a function: the value is not known until a
// call has been made.
func NewAPI(baseURL, key string, base http.RoundTripper) (API, func() string, error) {
	if strings.TrimSpace(baseURL) == "" {
		return nil, nil, errors.New("bot: the API base URL is required")
	}

	if strings.TrimSpace(key) == "" {
		return nil, nil, errors.New("bot: an API key is required")
	}

	if base == nil {
		base = http.DefaultTransport
	}

	tracker := &deployTracker{base: base}

	client := apiclient.New(baseURL)
	client.HTTP = &http.Client{Transport: tracker, Timeout: apiTimeout}
	client.Header = func(r *http.Request) { r.Header.Set(httpx.APIKeyHeader, key) }

	return client, tracker.target, nil
}

// apiTimeout bounds one API call. Generous because the metadata probe waits
// on a platform, and well under the conversion timeout because no single call
// should ever take that long.
const apiTimeout = 90 * time.Second

// deployTracker records the deploy target header of the last response.
type deployTracker struct {
	base http.RoundTripper

	mu   sync.Mutex
	last string
}

// RoundTrip implements http.RoundTripper.
func (t *deployTracker) RoundTrip(r *http.Request) (*http.Response, error) {
	res, err := t.base.RoundTrip(r)
	if err != nil {
		return nil, err
	}

	if target := res.Header.Get(httpx.DeployTargetHeader); target != "" {
		t.mu.Lock()
		t.last = target
		t.mu.Unlock()
	}

	return res, nil
}

// target is what the API last said it was.
func (t *deployTracker) target() string {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.last == "" {
		return "unknown"
	}

	return t.last
}
