package bot_test

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AlexGalhardo/vukem-tools/app/abilities"
	"github.com/AlexGalhardo/vukem-tools/clients/apiclient"
	"github.com/AlexGalhardo/vukem-tools/internal/bot"
)

const allowedUser = int64(4242)

// fakeTelegram records everything the bot said, which is what a scripted
// conversation is checked against.
type fakeTelegram struct {
	// mu guards everything below. The Run test reads these fields from the
	// test goroutine while the bot writes them from its own, which is a real
	// data race even though it is only in the harness.
	mu        sync.Mutex
	messages  []sentMessage
	videos    []sentFile
	audios    []sentFile
	documents []sentFile
	acks      []string
	// updates is what GetUpdates hands out, one batch per call, then an
	// empty batch forever so a poll loop under test terminates on its
	// context rather than on an error.
	updates [][]bot.Update
	polls   int
	// sendErr, when set, is returned by every file send, which is how
	// Telegram refusing a URL over its 20 MB limit is simulated.
	sendErr error
}

type sentMessage struct {
	chatID   int64
	text     string
	keyboard *bot.Keyboard
}

type sentFile struct {
	chatID  int64
	url     string
	caption string
}

func (f *fakeTelegram) GetUpdates(_ context.Context, _ int64, _ time.Duration) ([]bot.Update, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.polls++

	if len(f.updates) == 0 {
		return nil, nil
	}

	batch := f.updates[0]
	f.updates = f.updates[1:]

	return batch, nil
}

func (f *fakeTelegram) SendMessage(_ context.Context, chatID int64, text string, k *bot.Keyboard) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.messages = append(f.messages, sentMessage{chatID: chatID, text: text, keyboard: k})

	return nil
}

func (f *fakeTelegram) SendVideo(_ context.Context, chatID int64, url, caption string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.sendErr != nil {
		return f.sendErr
	}

	f.videos = append(f.videos, sentFile{chatID: chatID, url: url, caption: caption})

	return nil
}

func (f *fakeTelegram) SendAudio(_ context.Context, chatID int64, url, caption string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.sendErr != nil {
		return f.sendErr
	}

	f.audios = append(f.audios, sentFile{chatID: chatID, url: url, caption: caption})

	return nil
}

func (f *fakeTelegram) SendDocument(_ context.Context, chatID int64, url, caption string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.sendErr != nil {
		return f.sendErr
	}

	f.documents = append(f.documents, sentFile{chatID: chatID, url: url, caption: caption})

	return nil
}

func (f *fakeTelegram) AnswerCallback(_ context.Context, id, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.acks = append(f.acks, id)

	return nil
}

// said reports whether any message contains text.
func (f *fakeTelegram) said(text string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, m := range f.messages {
		if strings.Contains(m.text, text) {
			return true
		}
	}

	return false
}

// lastKeyboard returns the most recent inline keyboard, if any.
func (f *fakeTelegram) lastKeyboard() *bot.Keyboard {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, m := range slices.Backward(f.messages) {
		if m.keyboard != nil {
			return m.keyboard
		}
	}

	return nil
}

// fakeAPI is this service's API, scripted.
type fakeAPI struct {
	media    apiclient.MediaView
	mediaErr error

	submitted []apiclient.ToolsVideoDownloadBody
	submitErr error

	// statuses is returned by ConversionsShow in order, the last one
	// repeating, so a test can script "running, running, finished".
	statuses []string
	shown    int

	results    apiclient.ResultList
	resultsErr error

	caps    apiclient.CapabilitiesView
	capsErr error
}

func (a *fakeAPI) ToolsVideoDownloadMetadata(
	context.Context,
	apiclient.ToolsVideoDownloadMetadataBody,
) (apiclient.MediaView, error) {
	if a.mediaErr != nil {
		return apiclient.MediaView{}, a.mediaErr
	}

	return a.media, nil
}

func (a *fakeAPI) ToolsVideoDownload(
	_ context.Context,
	body apiclient.ToolsVideoDownloadBody,
) (apiclient.ConversionView, error) {
	a.submitted = append(a.submitted, body)

	if a.submitErr != nil {
		return apiclient.ConversionView{}, a.submitErr
	}

	id := "01JQ8Z5X0K7M3N9P2R4T6V8W0Y"
	status := "pending"

	return apiclient.ConversionView{ID: &id, Status: &status}, nil
}

func (a *fakeAPI) ConversionsShow(_ context.Context, id string) (apiclient.ConversionView, error) {
	status := "finished"

	if len(a.statuses) > 0 {
		status = a.statuses[min(a.shown, len(a.statuses)-1)]
	}

	a.shown++

	view := apiclient.ConversionView{ID: &id, Status: &status}

	if status == "error" {
		kind := "unsupported_input"
		message := "That video is private, so it cannot be downloaded."
		view.ErrorType = &kind
		view.ErrorMessage = &message
	}

	return view, nil
}

func (a *fakeAPI) ConversionsResult(context.Context, string) (apiclient.ResultList, error) {
	if a.resultsErr != nil {
		return apiclient.ResultList{}, a.resultsErr
	}

	return a.results, nil
}

func (a *fakeAPI) Capabilities(context.Context) (apiclient.CapabilitiesView, error) {
	if a.capsErr != nil {
		return apiclient.CapabilitiesView{}, a.capsErr
	}

	return a.caps, nil
}

// newBot builds a bot on the two fakes, with the poll interval collapsed so a
// scripted conversation does not wait.
func newBot(t *testing.T, tg bot.Telegram, api bot.API) *bot.Bot {
	t.Helper()

	b, err := bot.New(bot.Options{
		Telegram:        tg,
		API:             api,
		AllowedUsers:    []int64{allowedUser},
		DeployTarget:    "bot-test",
		APIDeployTarget: func() string { return "api-test" },
		PollInterval:    time.Millisecond,
		Log:             discardLogger(t),
	})
	if err != nil {
		t.Fatalf("new bot: %v", err)
	}

	return b
}

// message is an update carrying text from a sender.
func message(from int64, text string) bot.Update {
	return bot.Update{
		ID: 1,
		Message: &bot.Message{
			ID:   1,
			From: &bot.Sender{ID: from},
			Chat: &bot.Chat{ID: from, Type: "private"},
			Text: text,
		},
	}
}

// press is an update carrying a button press from a sender.
func press(from int64, data string) bot.Update {
	return bot.Update{
		ID: 2,
		Callback: &bot.CallbackQuery{
			ID:      "cb-1",
			From:    &bot.Sender{ID: from},
			Message: &bot.Message{ID: 1, Chat: &bot.Chat{ID: from, Type: "private"}},
			Data:    data,
		},
	}
}

// youtubeMedia is a probe answer for a video that can be fetched.
func youtubeMedia() apiclient.MediaView {
	return apiclient.MediaView{
		Platform:        new("youtube"),
		Title:           new("A video about Go"),
		Uploader:        new("Someone"),
		DurationSeconds: new(212.0),
		EstimatedBytes:  new(int64(36_438_016)),
		MaxBytes:        new(int64(524_288_000)),
		TooLarge:        new(false),
	}
}

func TestAScriptedConversationCoversADownloadEndToEnd(t *testing.T) {
	t.Parallel()

	tg := &fakeTelegram{}
	api := &fakeAPI{
		media:    youtubeMedia(),
		statuses: []string{"pending", "running", "finished"},
		results: apiclient.ResultList{Data: []apiclient.ResultFile{{
			Name:      new("A-video-about-Go.mp4"),
			Mime:      new("video/mp4"),
			SizeBytes: new(int64(36_438_016)),
			URL:       new("https://tools.example.com/files/abc?signature=x"),
		}}},
	}

	b := newBot(t, tg, api)

	// /start, then the link, then the format.
	b.Handle(t.Context(), message(allowedUser, "/start"))

	if !tg.said("Send me a link") {
		t.Error("/start did not explain what the bot does")
	}

	if !tg.said("terms of service") {
		t.Error("/start does not say the user is responsible for what they download")
	}

	b.Handle(t.Context(), message(allowedUser, "https://www.youtube.com/watch?v=abc"))

	keyboard := tg.lastKeyboard()
	if keyboard == nil {
		t.Fatal("no format keyboard was offered")
	}

	if !tg.said("A video about Go") || !tg.said("34.8 MB") {
		t.Errorf("the summary does not report the title and the size: %+v", tg.messages)
	}

	// Every button the keyboard offers has to be a choice the bot accepts,
	// or the user is shown a button that answers "I do not recognise that".
	var pressed int

	for _, row := range keyboard.Rows {
		for _, button := range row {
			if button.Data == "" || button.Text == "" {
				t.Errorf("button %+v is incomplete", button)
			}

			if len(button.Data) > 64 {
				t.Errorf("button %+v has callback_data past Telegram's 64-byte limit", button)
			}

			pressed++
		}
	}

	if pressed != 4 {
		t.Errorf("%d buttons, want 4 (best, smallest, MP3, clips)", pressed)
	}

	b.Handle(t.Context(), press(allowedUser, keyboard.Rows[0][0].Data))

	if len(tg.acks) != 1 {
		t.Errorf("%d button presses acknowledged, want 1: the button spins until they are", len(tg.acks))
	}

	if len(api.submitted) != 1 {
		t.Fatalf("%d downloads submitted, want 1", len(api.submitted))
	}

	options := api.submitted[0].Options
	if options == nil {
		t.Fatal("the submission carried no options")
	}

	if options.URL != "https://www.youtube.com/watch?v=abc" {
		t.Errorf("submitted URL %q", options.URL)
	}

	if *options.Kind != "video" || *options.Quality != "best" {
		t.Errorf("submitted kind %q quality %q, want video/best", *options.Kind, *options.Quality)
	}

	if len(tg.videos) != 1 {
		t.Fatalf("%d videos sent, want 1", len(tg.videos))
	}

	if tg.videos[0].url != "https://tools.example.com/files/abc?signature=x" {
		t.Errorf("sent %q, want the signed link", tg.videos[0].url)
	}

	if !strings.Contains(tg.videos[0].caption, "A-video-about-Go.mp4") {
		t.Errorf("caption %q does not name the file", tg.videos[0].caption)
	}
}

func TestAScriptedConversationDeliversClipsAsSeparateVideos(t *testing.T) {
	t.Parallel()

	tg := &fakeTelegram{}
	api := &fakeAPI{
		media:    youtubeMedia(),
		statuses: []string{"finished"},
		results: apiclient.ResultList{Data: []apiclient.ResultFile{
			{Name: new("v-clip-01.mp4"), Mime: new("video/mp4"), SizeBytes: new(int64(1 << 20)), URL: new("u1")},
			{Name: new("v-clip-02.mp4"), Mime: new("video/mp4"), SizeBytes: new(int64(1 << 20)), URL: new("u2")},
			{Name: new("v-clip-03.mp4"), Mime: new("video/mp4"), SizeBytes: new(int64(1 << 20)), URL: new("u3")},
			// The web interface also produces a zip. In a chat the separate
			// videos are what anybody wants, so the zip is skipped rather
			// than uploaded a second time.
			{Name: new("v-clips.zip"), Mime: new("application/zip"), SizeBytes: new(int64(3 << 20)), URL: new("z")},
		}},
	}

	b := newBot(t, tg, api)

	b.Handle(t.Context(), message(allowedUser, "https://www.youtube.com/watch?v=abc"))
	b.Handle(t.Context(), press(allowedUser, "c"))

	if len(api.submitted) != 1 || api.submitted[0].Options == nil {
		t.Fatal("the clips choice submitted nothing")
	}

	if clips := api.submitted[0].Options.Clips; clips == nil || !*clips {
		t.Error("the clips option was not set")
	}

	if len(tg.videos) != 3 {
		t.Fatalf("%d videos sent, want 3", len(tg.videos))
	}

	if len(tg.documents) != 0 {
		t.Errorf("the zip was sent as well: %+v", tg.documents)
	}

	if !tg.said("all 3 clips") {
		t.Errorf("the bot did not report the clip count: %+v", tg.messages)
	}
}

func TestAScriptedConversationShowsTheAPIsOwnSentenceForAFailure(t *testing.T) {
	t.Parallel()

	tg := &fakeTelegram{}
	api := &fakeAPI{media: youtubeMedia(), statuses: []string{"error"}}

	b := newBot(t, tg, api)

	b.Handle(t.Context(), message(allowedUser, "https://www.youtube.com/watch?v=abc"))
	b.Handle(t.Context(), press(allowedUser, "v:best"))

	// The same sentence the web interface shows, from the same taxonomy. A
	// second vocabulary for the same failures is the thing this asserts
	// against.
	if !tg.said("That video is private, so it cannot be downloaded.") {
		t.Errorf("the bot invented its own wording: %+v", tg.messages)
	}

	if len(tg.videos) != 0 {
		t.Error("a failed conversion sent a file")
	}
}

func TestAProbeRefusalIsShownWithTheAPIsFieldMessage(t *testing.T) {
	t.Parallel()

	tg := &fakeTelegram{}
	api := &fakeAPI{mediaErr: &apiclient.Error{
		Status: http.StatusUnprocessableEntity,
		Problem: apiclient.Problem{
			Status: http.StatusUnprocessableEntity,
			Detail: "1 field(s) are invalid.",
			Errors: map[string][]string{
				"url": {"That URL is not one this service can download from. " +
					"YouTube, Instagram, X and TikTok are supported."},
			},
		},
	}}

	b := newBot(t, tg, api)

	b.Handle(t.Context(), message(allowedUser, "https://vimeo.com/1"))

	if !tg.said("not one this service can download from") {
		t.Errorf("the field message was not shown: %+v", tg.messages)
	}

	if tg.lastKeyboard() != nil {
		t.Error("a format keyboard was offered for a URL that cannot be downloaded")
	}
}

func TestAVideoTooLargeForTelegramFallsBackToTheLink(t *testing.T) {
	t.Parallel()

	tg := &fakeTelegram{sendErr: errors.New("Request Entity Too Large")}
	api := &fakeAPI{
		media:    youtubeMedia(),
		statuses: []string{"finished"},
		results: apiclient.ResultList{Data: []apiclient.ResultFile{{
			Name:      new("big.mp4"),
			Mime:      new("video/mp4"),
			SizeBytes: new(int64(300 << 20)),
			URL:       new("https://tools.example.com/files/big?signature=x"),
		}}},
	}

	b := newBot(t, tg, api)

	b.Handle(t.Context(), message(allowedUser, "https://www.youtube.com/watch?v=abc"))
	b.Handle(t.Context(), press(allowedUser, "v:best"))

	// The result expires in five minutes, so the user needs the link now
	// rather than an apology.
	if !tg.said("https://tools.example.com/files/big?signature=x") {
		t.Errorf("no link was offered after the upload failed: %+v", tg.messages)
	}
}

func TestAUserOutsideTheAllowListIsRefused(t *testing.T) {
	t.Parallel()

	tg := &fakeTelegram{}
	api := &fakeAPI{media: youtubeMedia()}

	b := newBot(t, tg, api)

	const stranger = int64(9999)

	b.Handle(t.Context(), message(stranger, "https://www.youtube.com/watch?v=abc"))
	b.Handle(t.Context(), press(stranger, "v:best"))
	b.Handle(t.Context(), message(stranger, "/status"))

	if !tg.said("This bot is private.") {
		t.Errorf("the stranger was not refused: %+v", tg.messages)
	}

	if len(api.submitted) != 0 {
		t.Fatalf("a stranger started %d downloads", len(api.submitted))
	}

	if tg.lastKeyboard() != nil {
		t.Error("a stranger was offered the format keyboard")
	}

	// The refusal must not confirm the bot is the one they were looking for.
	for _, m := range tg.messages {
		if strings.Contains(strings.ToLower(m.text), "allow") ||
			strings.Contains(strings.ToLower(m.text), "not on the list") {
			t.Errorf("the refusal says too much: %q", m.text)
		}
	}
}

func TestAnUpdateWithNoSenderIsRefused(t *testing.T) {
	t.Parallel()

	tg := &fakeTelegram{}
	api := &fakeAPI{media: youtubeMedia()}
	b := newBot(t, tg, api)

	// Telegram omits `from` for a channel post and for an anonymous
	// administrator. Comparing that against the allow-list as user 0 is the
	// bug this guards: it fails today and would start succeeding the moment
	// somebody put a 0 in the configuration.
	updates := []bot.Update{
		{ID: 1, Message: &bot.Message{ID: 1, Chat: &bot.Chat{ID: 1, Type: "channel"}, Text: "https://youtu.be/a"}},
		{ID: 2, Callback: &bot.CallbackQuery{ID: "c", Message: &bot.Message{Chat: &bot.Chat{ID: 1}}, Data: "v:best"}},
		{ID: 3, Message: &bot.Message{ID: 3, From: &bot.Sender{ID: 0}, Chat: &bot.Chat{ID: 1}, Text: "/status"}},
	}

	for _, update := range updates {
		b.Handle(t.Context(), update)
	}

	if len(api.submitted) != 0 {
		t.Fatalf("an update with no sender started %d downloads", len(api.submitted))
	}

	if b.Allows(nil) {
		t.Error("a nil sender was allowed")
	}

	if b.Allows(&bot.Sender{ID: 0}) {
		t.Error("sender 0 was allowed")
	}

	if !b.Allows(&bot.Sender{ID: allowedUser}) {
		t.Error("the allowed user was refused")
	}
}

func TestAnEmptyAllowListRefusesEverybody(t *testing.T) {
	t.Parallel()

	// Fail closed: an allow-list that was not configured must not mean an
	// open bot.
	tg := &fakeTelegram{}
	api := &fakeAPI{media: youtubeMedia()}

	b, err := bot.New(bot.Options{
		Telegram:     tg,
		API:          api,
		AllowedUsers: nil,
		Log:          discardLogger(t),
	})
	if err != nil {
		t.Fatalf("new bot: %v", err)
	}

	b.Handle(t.Context(), message(allowedUser, "https://youtu.be/a"))

	if len(api.submitted) != 0 {
		t.Fatal("a bot with no allow-list accepted a download")
	}

	if b.Allows(&bot.Sender{ID: allowedUser}) {
		t.Error("a bot with no allow-list allowed somebody")
	}
}

func TestTheStatusCommandReportsBothDeployTargets(t *testing.T) {
	t.Parallel()

	tg := &fakeTelegram{}
	api := &fakeAPI{caps: apiclient.CapabilitiesView{
		Tools: []string{"video-clip", "video-download"},
		Engines: []apiclient.EngineView{
			{Name: new("yt-dlp"), Present: new(true), Version: new("2026.08.19")},
			{Name: new("ffmpeg"), Present: new(true), Version: new("ffmpeg version 7.1.5-0+deb13u1 Copyright")},
			{Name: new("gs"), Present: new(false)},
		},
	}}

	b := newBot(t, tg, api)

	b.Handle(t.Context(), message(allowedUser, "/status"))

	for _, want := range []string{"bot-test", "api-test", "video-download", "yt-dlp 2026.08.19", "gs missing"} {
		if !tg.said(want) {
			t.Errorf("/status does not report %q: %+v", want, tg.messages)
		}
	}

	// ffmpeg reports its version as a sentence with a copyright notice; a
	// status message is not the place for it.
	if tg.said("Copyright") {
		t.Errorf("/status printed ffmpeg's whole banner: %+v", tg.messages)
	}
}

func TestTheStatusCommandSaysSoWhenTheAPIIsDown(t *testing.T) {
	t.Parallel()

	tg := &fakeTelegram{}
	api := &fakeAPI{capsErr: errors.New("dial tcp: connection refused")}

	b := newBot(t, tg, api)

	b.Handle(t.Context(), message(allowedUser, "/status"))

	if !tg.said("not answering") {
		t.Errorf("/status hid an unreachable API: %+v", tg.messages)
	}

	// The bot is up, which is the most useful thing it can say when the API
	// is not.
	if !tg.said("bot-test") {
		t.Errorf("/status did not report the bot's own deploy: %+v", tg.messages)
	}
}

func TestAnUnknownCommandAndAnExpiredChoiceAreExplained(t *testing.T) {
	t.Parallel()

	tg := &fakeTelegram{}
	api := &fakeAPI{media: youtubeMedia()}
	b := newBot(t, tg, api)

	b.Handle(t.Context(), message(allowedUser, "/nonsense"))

	if !tg.said("do not know that command") {
		t.Errorf("an unknown command was not explained: %+v", tg.messages)
	}

	// A press with nothing pending: the bot restarted, or the user pressed
	// an old keyboard.
	b.Handle(t.Context(), press(allowedUser, "v:best"))

	if !tg.said("expired") {
		t.Errorf("a stale button press was not explained: %+v", tg.messages)
	}

	if len(api.submitted) != 0 {
		t.Error("a stale button press started a download")
	}

	// A choice value the bot does not define must never reach the API.
	b.Handle(t.Context(), message(allowedUser, "https://youtu.be/a"))
	b.Handle(t.Context(), press(allowedUser, "v:best; drop table"))

	if len(api.submitted) != 0 {
		t.Error("an unrecognised choice reached the API")
	}

	if !tg.said("do not recognise that choice") {
		t.Errorf("an unrecognised choice was not explained: %+v", tg.messages)
	}
}

func TestTheBotsKeyNeedsExactlyTheAbilitiesItUses(t *testing.T) {
	t.Parallel()

	got := bot.Abilities()

	if len(got) != 1 || got[0] != abilities.VideoDownload {
		t.Fatalf("abilities %v, want exactly [%s]", got, abilities.VideoDownload)
	}

	// Every ability it asks for has to be one the API defines, or the key
	// cannot be issued at all.
	for _, ability := range got {
		if !abilities.Valid(ability) {
			t.Errorf("%q is not an ability this API defines", ability)
		}
	}
}

func TestTheRunLoopStopsOnItsContextRatherThanOnAnError(t *testing.T) {
	t.Parallel()

	tg := &fakeTelegram{updates: [][]bot.Update{{message(allowedUser, "/help")}}}
	api := &fakeAPI{}
	b := newBot(t, tg, api)

	ctx, cancel := context.WithCancel(t.Context())

	done := make(chan error, 1)

	go func() { done <- b.Run(ctx) }()

	// The first batch is handled, then the fake reports nothing forever.
	deadline := time.After(5 * time.Second)

	for !tg.said("Send me a link") {
		select {
		case <-deadline:
			t.Fatal("the first update was never handled")
		case <-time.After(time.Millisecond):
		}
	}

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run returned %v, want nil on cancellation", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop when its context was cancelled")
	}
}

func TestABotNeedsATelegramClientAndAnAPI(t *testing.T) {
	t.Parallel()

	if _, err := bot.New(bot.Options{API: &fakeAPI{}}); err == nil {
		t.Error("a bot was built without a Telegram client")
	}

	if _, err := bot.New(bot.Options{Telegram: &fakeTelegram{}}); err == nil {
		t.Error("a bot was built without an API client")
	}
}

func TestNewTelegramNeedsAToken(t *testing.T) {
	t.Parallel()

	if _, err := bot.NewTelegram("", "", nil); err == nil {
		t.Error("a Telegram client was built without a token")
	}

	if _, err := bot.NewTelegram("   ", "", nil); err == nil {
		t.Error("a whitespace token was accepted")
	}
}

func TestNewAPINeedsABaseURLAndAKey(t *testing.T) {
	t.Parallel()

	if _, _, err := bot.NewAPI("", "key", nil); err == nil {
		t.Error("an API client was built without a base URL")
	}

	if _, _, err := bot.NewAPI("https://example.com", "", nil); err == nil {
		t.Error("an API client was built without a key")
	}
}
