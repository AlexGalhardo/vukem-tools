package bot_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AlexGalhardo/vukem-tools/clients/apiclient"
	"github.com/AlexGalhardo/vukem-tools/internal/bot"
)

const (
	apiBase = "https://tools.example"
	userID  = int64(4242)
	chatID  = int64(4242)
)

func discardLogger(t *testing.T) *slog.Logger {
	t.Helper()

	return slog.New(slog.DiscardHandler)
}

// shown is one message the bot put on screen, as it stands now: an edit
// replaces the text and keyboard of the message it names.
type shown struct {
	id       int64
	text     string
	keyboard *bot.Keyboard
}

// fakeTelegram records everything the bot says and does.
type fakeTelegram struct {
	mu       sync.Mutex
	nextID   int64
	messages []*shown
	// history keeps every text ever put on screen, because an edit replaces
	// the message a test would otherwise look at.
	history []shown
	sent    []string // "kind:name"
	acks    []string
	files   map[string][]byte
	// rejectURLButtons makes Telegram refuse a keyboard with a URL button, as
	// it does for a local address.
	rejectURLButtons bool
}

func newTelegram() *fakeTelegram {
	return &fakeTelegram{nextID: 100, files: map[string][]byte{}}
}

func (f *fakeTelegram) GetUpdates(context.Context, int64, time.Duration) ([]bot.Update, error) {
	return nil, nil
}

func hasURLButton(kb *bot.Keyboard) bool {
	if kb == nil {
		return false
	}

	for _, row := range kb.Rows {
		for _, b := range row {
			if b.URL != "" {
				return true
			}
		}
	}

	return false
}

func (f *fakeTelegram) SendMessage(_ context.Context, _ int64, text string, kb *bot.Keyboard) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.rejectURLButtons && hasURLButton(kb) {
		return 0, io.ErrUnexpectedEOF
	}

	f.nextID++
	f.messages = append(f.messages, &shown{id: f.nextID, text: text, keyboard: kb})
	f.history = append(f.history, shown{id: f.nextID, text: text, keyboard: kb})

	return f.nextID, nil
}

func (f *fakeTelegram) EditMessage(_ context.Context, _ int64, messageID int64, text string, kb *bot.Keyboard) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.rejectURLButtons && hasURLButton(kb) {
		return io.ErrUnexpectedEOF
	}

	for _, m := range f.messages {
		if m.id == messageID {
			m.text, m.keyboard = text, kb
			f.history = append(f.history, shown{id: messageID, text: text, keyboard: kb})

			return nil
		}
	}

	return io.ErrUnexpectedEOF
}

func (f *fakeTelegram) GetFile(_ context.Context, fileID string) (bot.File, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	body, ok := f.files[fileID]
	if !ok {
		return bot.File{}, io.ErrUnexpectedEOF
	}

	return bot.File{FileID: fileID, FilePath: "docs/" + fileID, FileSize: int64(len(body))}, nil
}

func (f *fakeTelegram) OpenFile(_ context.Context, path string) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	body, ok := f.files[strings.TrimPrefix(path, "docs/")]
	if !ok {
		return nil, io.ErrUnexpectedEOF
	}

	return io.NopCloser(strings.NewReader(string(body))), nil
}

func (f *fakeTelegram) record(kind, link, caption string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.sent = append(f.sent, kind+":"+link+"|"+caption)

	return nil
}

func (f *fakeTelegram) SendVideo(_ context.Context, _ int64, link, caption string) error {
	return f.record("video", link, caption)
}

func (f *fakeTelegram) SendAudio(_ context.Context, _ int64, link, caption string) error {
	return f.record("audio", link, caption)
}

func (f *fakeTelegram) SendDocument(_ context.Context, _ int64, link, caption string) error {
	return f.record("document", link, caption)
}

func (f *fakeTelegram) AnswerCallback(_ context.Context, id, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.acks = append(f.acks, id)

	return nil
}

// last returns the message that was most recently created or edited.
func (f *fakeTelegram) last() *shown {
	f.mu.Lock()
	defer f.mu.Unlock()

	if len(f.messages) == 0 {
		return &shown{}
	}

	return f.messages[len(f.messages)-1]
}

// find returns the newest message whose text contains s.
func (f *fakeTelegram) find(s string) *shown {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, m := range slices.Backward(f.messages) {
		if strings.Contains(m.text, s) {
			return m
		}
	}

	return nil
}

// texts is everything ever shown, oldest first.
func (f *fakeTelegram) texts() string {
	f.mu.Lock()
	defer f.mu.Unlock()

	var b strings.Builder
	for _, m := range f.history {
		b.WriteString(m.text + "\n---\n")
	}

	return b.String()
}

// everShown returns the newest message that ever had text containing s, even
// if it has since been edited into something else.
func (f *fakeTelegram) everShown(s string) *shown {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, m := range slices.Backward(f.history) {
		if strings.Contains(m.text, s) {
			return &m
		}
	}

	return nil
}

// buttons lists the labels and the callback data of a keyboard.
func (s *shown) buttons() map[string]string {
	out := map[string]string{}

	if s == nil || s.keyboard == nil {
		return out
	}

	for _, row := range s.keyboard.Rows {
		for _, b := range row {
			if b.Data != "" {
				out[b.Text] = b.Data
			} else {
				out[b.Text] = "url:" + b.URL
			}
		}
	}

	return out
}

// fakeLinks scripts the sign-in link.
type fakeLinks struct {
	mu       sync.Mutex
	statuses []string
	polls    int
	started  int
	token    string
}

func (l *fakeLinks) DevicelinksStart(context.Context, apiclient.DevicelinksStartBody) (apiclient.LinkStarted, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.started++

	return apiclient.LinkStarted{
		DeviceCode:      new("device-code-0123456789abcdef"),
		UserCode:        new("BCDF-2346"),
		VerificationURL: new(apiBase + "/account/link?code=BCDF-2346"),
		ExpiresIn:       new(int64(600)),
		Interval:        new(int64(3)),
	}, nil
}

func (l *fakeLinks) DevicelinksToken(context.Context, apiclient.DevicelinksTokenBody) (apiclient.LinkToken, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	status := "pending"
	if l.polls < len(l.statuses) {
		status = l.statuses[l.polls]
	}

	l.polls++

	out := apiclient.LinkToken{Status: &status}
	if status == "approved" {
		out.Token = new(l.token)
		out.ExpiresAt = new(time.Now().Add(30 * 24 * time.Hour))
	}

	return out, nil
}

// fakeUploader records what the bot uploads and registers it in the fake API,
// the way the real server's list of unclaimed uploads would show it.
type fakeUploader struct {
	api  *fakeAPI
	mime string
	fail error
}

func (u *fakeUploader) Upload(_ context.Context, name string, size int64, body io.Reader) error {
	if u.fail != nil {
		return u.fail
	}

	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}

	u.api.mu.Lock()
	defer u.api.mu.Unlock()

	u.api.nextUpload++
	id := "UP" + string(rune('0'+u.api.nextUpload))

	u.api.uploads = append([]apiclient.UploadView{{
		ID: &id, Name: &name, Mime: new(u.mime), SizeBytes: new(size), CreatedAt: new(time.Now()),
	}}, u.api.uploads...)
	u.api.uploaded = append(u.api.uploaded, string(data))

	return nil
}

// fakeAPI answers the calls the menus make. Methods the test does not script
// are left to the embedded nil interface, so an unexpected call panics.
type fakeAPI struct {
	bot.API

	mu         sync.Mutex
	nextUpload int
	uploads    []apiclient.UploadView
	uploaded   []string

	usage    apiclient.UsageView
	meErr    error
	revoked  bool
	status   []string // statuses ConversionsShow walks through
	statusN  int
	results  []apiclient.ResultFile
	failWith error

	calls []string
	body  any
}

func (a *fakeAPI) Me(context.Context) (apiclient.User, error) {
	return apiclient.User{Name: new("Ana Souza"), Email: new("ana@example.com")}, a.meErr
}

func (a *fakeAPI) Usage(context.Context) (apiclient.UsageView, error) {
	return a.usage, a.failWith
}

func (a *fakeAPI) TokensDestroy(context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.revoked = true

	return nil
}

func (a *fakeAPI) UploadsIndex(context.Context) (apiclient.UploadList, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	return apiclient.UploadList{Data: append([]apiclient.UploadView(nil), a.uploads...)}, nil
}

func (a *fakeAPI) submitted(name string, body any) (apiclient.ConversionView, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.calls = append(a.calls, name)
	a.body = body

	if a.failWith != nil {
		return apiclient.ConversionView{}, a.failWith
	}

	return apiclient.ConversionView{ID: new("CONV01"), Status: new("pending"), Progress: new(int64(0))}, nil
}

func (a *fakeAPI) ToolsImageConvert(
	_ context.Context,
	b apiclient.ToolsImageConvertBody,
) (apiclient.ConversionView, error) {
	return a.submitted("image-convert", b)
}

func (a *fakeAPI) ToolsPdfMerge(_ context.Context, b apiclient.ToolsPdfMergeBody) (apiclient.ConversionView, error) {
	return a.submitted("pdf-merge", b)
}

func (a *fakeAPI) ToolsPdfCompress(
	_ context.Context,
	b apiclient.ToolsPdfCompressBody,
) (apiclient.ConversionView, error) {
	return a.submitted("pdf-compress", b)
}

func (a *fakeAPI) ToolsVideoDownload(
	_ context.Context,
	b apiclient.ToolsVideoDownloadBody,
) (apiclient.ConversionView, error) {
	return a.submitted("video-download", b)
}

func (a *fakeAPI) ToolsVideoDownloadMetadata(
	context.Context,
	apiclient.ToolsVideoDownloadMetadataBody,
) (apiclient.MediaView, error) {
	return apiclient.MediaView{
		Title: new("A fine video"), Uploader: new("someone"),
		DurationSeconds: new(125.0), EstimatedBytes: new(int64(5 << 20)),
	}, nil
}

func (a *fakeAPI) ConversionsShow(context.Context, string) (apiclient.ConversionView, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	status := "finished"
	if a.statusN < len(a.status) {
		status = a.status[a.statusN]
	}

	a.statusN++

	out := apiclient.ConversionView{
		ID:       new("CONV01"),
		Status:   &status,
		Progress: new(int64(50)),
		Note:     new("Took 3 seconds."),
	}
	if status == "error" {
		out.ErrorMessage = new("That file is damaged.")
	}

	return out, nil
}

func (a *fakeAPI) ConversionsResult(context.Context, string) (apiclient.ResultList, error) {
	return apiclient.ResultList{Data: a.results}, nil
}

func (a *fakeAPI) ConversionsCancel(context.Context, string) (apiclient.ConversionView, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.calls = append(a.calls, "cancel")

	return apiclient.ConversionView{}, nil
}

func (a *fakeAPI) called(name string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	return slices.Contains(a.calls, name)
}

// harness wires a bot to the fakes.
type harness struct {
	t     *testing.T
	b     *bot.Bot
	tg    *fakeTelegram
	links *fakeLinks
	api   *fakeAPI
	up    *fakeUploader
}

func newHarness(t *testing.T, mutate ...func(*bot.Options)) *harness {
	t.Helper()

	h := &harness{
		t:     t,
		tg:    newTelegram(),
		links: &fakeLinks{token: "secret-key-0123456789abcdef", statuses: []string{"pending", "approved"}},
	}
	h.api = &fakeAPI{usage: apiclient.UsageView{
		BudgetSeconds: new(int64(1800)), SpentSeconds: new(int64(120)), RemainingSeconds: new(int64(1680)),
		Unlimited: new(false), ResetsAt: new(time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)),
	}}
	h.up = &fakeUploader{api: h.api, mime: "image/png"}

	opts := bot.Options{
		Telegram:      h.tg,
		Links:         h.links,
		Connect:       func(string) (bot.Session, error) { return bot.Session{API: h.api, Uploader: h.up}, nil },
		APIBaseURL:    apiBase,
		PollInterval:  time.Millisecond,
		LoginInterval: time.Millisecond,
		Log:           discardLogger(t),
	}

	for _, m := range mutate {
		m(&opts)
	}

	b, err := bot.New(opts)
	if err != nil {
		t.Fatalf("bot.New: %v", err)
	}

	h.b = b

	return h
}

func (h *harness) say(text string) {
	h.t.Helper()

	h.b.Handle(context.Background(), bot.Update{Message: &bot.Message{
		ID:   1,
		From: &bot.Sender{ID: userID, FirstName: "Ana", Username: "ana"},
		Chat: &bot.Chat{ID: chatID, Type: "private"},
		Text: text,
	}})
	h.b.Wait()
}

func (h *harness) press(data string) {
	h.t.Helper()

	menu := h.tg.last()

	h.b.Handle(context.Background(), bot.Update{Callback: &bot.CallbackQuery{
		ID: "cb", From: &bot.Sender{ID: userID, FirstName: "Ana"}, Data: data,
		Message: &bot.Message{ID: menu.id, Chat: &bot.Chat{ID: chatID, Type: "private"}},
	}})
	h.b.Wait()
}

func (h *harness) sendFile(fileID string, content []byte, mutate ...func(*bot.Message)) {
	h.t.Helper()

	h.tg.mu.Lock()
	h.tg.files[fileID] = content
	h.tg.mu.Unlock()

	msg := &bot.Message{
		ID:   2,
		From: &bot.Sender{ID: userID, FirstName: "Ana"},
		Chat: &bot.Chat{ID: chatID, Type: "private"},
		Document: &bot.Attachment{
			FileID:   fileID,
			FileName: "picture.png",
			MimeType: "image/png",
			FileSize: int64(len(content)),
		},
	}

	for _, m := range mutate {
		m(msg)
	}

	h.b.Handle(context.Background(), bot.Update{Message: msg})
	h.b.Wait()
}

// signIn runs the whole sign-in and leaves the main menu on screen.
func (h *harness) signIn() {
	h.t.Helper()

	h.say("/start")
	h.press("login")
	h.b.Wait()
}

var _ = http.StatusOK
