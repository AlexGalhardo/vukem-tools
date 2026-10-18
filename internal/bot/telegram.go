package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// The Telegram Bot API, narrowed to what this bot uses.
//
// It is an interface so the whole conversation can be tested without a
// network: a scripted conversation is the only way to check that the bot says
// the right thing in the right order, and a real bot token in a test would
// make that impossible.

// Update is one event from Telegram.
//
// Only the fields this bot reads are decoded. The real document has dozens
// more, several of them free text from whoever sent the message, and decoding
// only what is used means a new field cannot change anything here.
type Update struct {
	ID int64 `json:"update_id"`
	// Message is a plain message. Nil for every other kind of update.
	Message *Message `json:"message,omitzero"`
	// Callback is a button press. Nil for every other kind.
	Callback *CallbackQuery `json:"callback_query,omitzero"`
}

// Message is a message in a chat.
type Message struct {
	ID int64 `json:"message_id"`
	// From is who sent it. It is a pointer because Telegram genuinely omits
	// it: a message from a channel, or one posted by an anonymous
	// administrator, has no sender. That case is why the allow-list has to
	// fail closed rather than compare against a zero id.
	From *Sender `json:"from,omitzero"`
	Chat *Chat   `json:"chat,omitzero"`
	Text string  `json:"text,omitzero"`
}

// CallbackQuery is a press on an inline keyboard button.
type CallbackQuery struct {
	ID string `json:"id"`
	// From is who pressed it. Also a pointer, for the same reason.
	From *Sender `json:"from,omitzero"`
	// Message is the message the keyboard was attached to, which is where
	// the chat comes from.
	Message *Message `json:"message,omitzero"`
	// Data is the button's callback_data, which this bot sets itself.
	Data string `json:"data,omitzero"`
}

// Sender is a Telegram user.
type Sender struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot,omitzero"`
	FirstName string `json:"first_name,omitzero"`
	Username  string `json:"username,omitzero"`
}

// Chat is where a message was sent.
type Chat struct {
	ID int64 `json:"id"`
	// Type is "private", "group", "supergroup" or "channel".
	Type string `json:"type,omitzero"`
}

// Button is one inline keyboard button.
type Button struct {
	Text string `json:"text"`
	// Data is returned verbatim as CallbackQuery.Data. It is set by this bot
	// and never by a user, and Telegram caps it at 64 bytes.
	Data string `json:"callback_data"`
}

// Keyboard is an inline keyboard: a list of rows of buttons.
type Keyboard struct {
	Rows [][]Button `json:"inline_keyboard"`
}

// Telegram is the Bot API as this bot needs it.
type Telegram interface {
	// GetUpdates long-polls for events after offset. It returns an empty
	// slice when the poll timed out with nothing to report, which is the
	// normal case and not an error.
	GetUpdates(ctx context.Context, offset int64, timeout time.Duration) ([]Update, error)
	// SendMessage sends text, with an optional inline keyboard.
	SendMessage(ctx context.Context, chatID int64, text string, keyboard *Keyboard) error
	// SendVideo asks Telegram to fetch a URL and post it as a video.
	SendVideo(ctx context.Context, chatID int64, fileURL, caption string) error
	// SendAudio does the same for an audio file.
	SendAudio(ctx context.Context, chatID int64, fileURL, caption string) error
	// SendDocument does the same for anything else.
	SendDocument(ctx context.Context, chatID int64, fileURL, caption string) error
	// AnswerCallback acknowledges a button press. Telegram shows a spinner
	// on the button until this is called, so it is not optional.
	AnswerCallback(ctx context.Context, callbackID, text string) error
}

// httpTelegram talks to api.telegram.org.
type httpTelegram struct {
	token  string
	base   string
	client *http.Client
}

// NewTelegram returns a Bot API client.
//
// base is the API root, which exists so a test can point it at a local
// server; empty means the real one. The token is interpolated into the path
// rather than sent as a header, which is how the Bot API works, and is the
// reason no URL built here is ever logged.
func NewTelegram(token, base string, client *http.Client) (Telegram, error) {
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("bot: a Telegram bot token is required")
	}

	if base == "" {
		base = "https://api.telegram.org"
	}

	if client == nil {
		// A timeout well above the long-poll timeout: the request is
		// *supposed* to sit open for a minute, so the usual short client
		// timeout would cancel every successful poll.
		client = &http.Client{Timeout: 2 * time.Minute}
	}

	return &httpTelegram{token: token, base: strings.TrimRight(base, "/"), client: client}, nil
}

// response is the envelope every Bot API method answers with.
type response struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	Description string          `json:"description"`
	ErrorCode   int             `json:"error_code"`
}

// call invokes one Bot API method.
func (t *httpTelegram) call(ctx context.Context, method string, body, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("bot: encode %s: %w", method, err)
	}

	// The token is in the path, so the URL is a credential. It is built here
	// and never put in an error: every message below names the method only.
	endpoint := t.base + "/bot" + t.token + "/" + method

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("bot: build %s: %w", method, err)
	}

	req.Header.Set("Content-Type", "application/json")

	res, err := t.client.Do(req)
	if err != nil {
		// url.Error carries the URL, and the URL carries the token.
		if urlErr, ok := errors.AsType[*url.Error](err); ok {
			return fmt.Errorf("bot: %s: %w", method, urlErr.Err)
		}

		return fmt.Errorf("bot: %s failed", method)
	}

	defer func() { _ = res.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(res.Body, maxTelegramBody))
	if err != nil {
		return fmt.Errorf("bot: read the %s response: %w", method, err)
	}

	var envelope response
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("bot: decode the %s response (HTTP %d): %w", method, res.StatusCode, err)
	}

	if !envelope.OK {
		return fmt.Errorf("bot: %s: telegram returned %d: %s", method, envelope.ErrorCode, envelope.Description)
	}

	if out == nil {
		return nil
	}

	if err := json.Unmarshal(envelope.Result, out); err != nil {
		return fmt.Errorf("bot: decode the %s result: %w", method, err)
	}

	return nil
}

// maxTelegramBody bounds a Bot API response. A batch of 100 updates is tens
// of kilobytes; this is far above that and still stops a broken proxy from
// growing the heap without limit.
const maxTelegramBody = 4 << 20

// GetUpdates implements Telegram.
func (t *httpTelegram) GetUpdates(ctx context.Context, offset int64, timeout time.Duration) ([]Update, error) {
	var updates []Update

	err := t.call(ctx, "getUpdates", map[string]any{
		"offset":  offset,
		"timeout": int(timeout.Seconds()),
		// Only the two kinds this bot handles. Telegram then does not send
		// the rest at all, which keeps a chat full of joins and edits from
		// filling the poll.
		"allowed_updates": []string{"message", "callback_query"},
	}, &updates)
	if err != nil {
		return nil, err
	}

	return updates, nil
}

// SendMessage implements Telegram.
func (t *httpTelegram) SendMessage(ctx context.Context, chatID int64, text string, keyboard *Keyboard) error {
	body := map[string]any{
		"chat_id": chatID,
		"text":    truncate(text, maxMessageLength),
		// No parse mode: the text carries a video title from a remote page,
		// and sending that as HTML or Markdown would let the title close a
		// tag. Plain text has nothing to escape.
		"disable_web_page_preview": true,
	}

	if keyboard != nil {
		body["reply_markup"] = keyboard
	}

	return t.call(ctx, "sendMessage", body, nil)
}

// maxMessageLength is Telegram's own limit on a message.
const maxMessageLength = 4096

// SendVideo implements Telegram.
func (t *httpTelegram) SendVideo(ctx context.Context, chatID int64, fileURL, caption string) error {
	return t.sendFile(ctx, "sendVideo", "video", chatID, fileURL, caption)
}

// SendAudio implements Telegram.
func (t *httpTelegram) SendAudio(ctx context.Context, chatID int64, fileURL, caption string) error {
	return t.sendFile(ctx, "sendAudio", "audio", chatID, fileURL, caption)
}

// SendDocument implements Telegram.
func (t *httpTelegram) SendDocument(ctx context.Context, chatID int64, fileURL, caption string) error {
	return t.sendFile(ctx, "sendDocument", "document", chatID, fileURL, caption)
}

// sendFile hands Telegram a URL and lets it fetch the file.
//
// Telegram fetching the URL rather than the bot uploading it is deliberate:
// the alternative is streaming the whole file through this process, and a
// 500 MB download would then be paid for twice in bandwidth and once in the
// container's memory. The cost is Telegram's own 20 MB limit on a URL it
// fetches, which is why deliver falls back to sending the link.
func (t *httpTelegram) sendFile(
	ctx context.Context,
	method, field string,
	chatID int64,
	fileURL, caption string,
) error {
	return t.call(ctx, method, map[string]any{
		"chat_id": chatID,
		field:     fileURL,
		"caption": truncate(caption, maxCaptionLength),
	}, nil)
}

// maxCaptionLength is Telegram's own limit on a caption.
const maxCaptionLength = 1024

// AnswerCallback implements Telegram.
//
// Telegram shows a spinner on the pressed button until this is called, so it
// is not optional: without it the button looks stuck for the whole
// conversion. text is shown as a toast when it is not empty.
func (t *httpTelegram) AnswerCallback(ctx context.Context, callbackID, text string) error {
	body := map[string]any{"callback_query_id": callbackID}
	if text != "" {
		body["text"] = truncate(text, maxCallbackAnswerLength)
	}

	return t.call(ctx, "answerCallbackQuery", body, nil)
}

// maxCallbackAnswerLength is Telegram's own limit on a callback answer.
const maxCallbackAnswerLength = 200

// truncate cuts a string to at most n bytes, on a rune boundary.
//
// Telegram counts UTF-16 code units and this counts bytes, which is
// deliberately conservative: a message this cuts short is a message that
// would have been rejected, and a rejected message is worse than a clipped
// one.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}

	cut := s[:n]
	for cut != "" && !utf8Start(cut[len(cut)-1]) {
		cut = cut[:len(cut)-1]
	}

	return strings.TrimSpace(cut) + "…"
}

// utf8Start reports whether b can begin a UTF-8 rune.
func utf8Start(b byte) bool { return b&0xC0 != 0x80 }

// formatBytes renders a size the way a person reads it, for the messages the
// bot sends.
func formatBytes(n int64) string {
	const unit = 1 << 20

	if n <= 0 {
		return "unknown size"
	}

	if n < unit {
		return strconv.FormatInt(n, 10) + " bytes"
	}

	return strconv.FormatFloat(float64(n)/float64(unit), 'f', 1, 64) + " MB"
}

// formatDuration renders a duration as minutes and seconds.
func formatDuration(seconds float64) string {
	if seconds <= 0 {
		return "unknown length"
	}

	total := int(seconds)

	return strconv.Itoa(total/60) + "m" + strconv.Itoa(total%60) + "s"
}
