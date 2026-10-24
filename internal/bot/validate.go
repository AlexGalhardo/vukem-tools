package bot

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
	"unicode"

	"github.com/AlexGalhardo/vukem/z"
)

// Everything that crosses the bot's edge is checked with vukem's z before it
// is used: what a person sends (text, a pressed button, a file) and what the
// server sends back (a conversion, a result link, a usage figure). The first
// is untrusted because anybody can message a bot; the second is checked
// because the bot repeats it into a chat and follows its links, and a surprise
// there should be refused here rather than shown to a user.

// MaxFileBytes is the largest file the bot takes from a chat. Telegram's own
// getFile limit is 20 MB, so anything larger could not be fetched anyway; the
// check turns that into a sentence instead of an error from Telegram.
const MaxFileBytes = 20 << 20

var (
	// identifier is a Telegram file id or a server-made id: letters, digits,
	// dash and underscore.
	identifier = regexp.MustCompile(`^[A-Za-z0-9_-]{1,256}$`)
	// mediaType is type/subtype, as a Content-Type value without parameters.
	mediaType = regexp.MustCompile(`^[a-z0-9][a-z0-9!#$&^_.+-]{0,63}/[a-z0-9][a-z0-9!#$&^_.+-]{0,126}$`)
	// callbackData is what this bot puts in a button: short, with no spaces. An
	// id may be upper case, so the class is wider than the table's own keys.
	callbackData = regexp.MustCompile(`^[A-Za-z0-9:+._-]{1,48}$`)
)

// inboundMessage is a message as the bot needs it, flattened so one schema can
// check it.
type inboundMessage struct {
	ChatID   int64  `json:"chat_id"`
	ChatType string `json:"chat_type"`
	UserID   int64  `json:"user_id"`
	Text     string `json:"text"`
}

var inboundSchema = z.Struct[inboundMessage](z.Shape{
	"chat_id": z.Int64().Min(1),
	// Private chats only. In a group, anybody present could press the buttons
	// of a signed-in person, and the files they sent would be converted under
	// that person's key.
	"chat_type": z.String().OneOf("private"),
	"user_id":   z.Int64().Min(1),
	"text":      z.String().Max(4096),
})

// inboundButton is a press, flattened.
type inboundButton struct {
	ChatID   int64  `json:"chat_id"`
	ChatType string `json:"chat_type"`
	UserID   int64  `json:"user_id"`
	Data     string `json:"data"`
}

var buttonSchema = z.Struct[inboundButton](z.Shape{
	"chat_id":   z.Int64().Min(1),
	"chat_type": z.String().OneOf("private"),
	"user_id":   z.Int64().Min(1),
	"data":      z.String().Regex(callbackData),
})

// inboundFile is the metadata of a file a user sent.
type inboundFile struct {
	FileID   string `json:"file_id"`
	FileName string `json:"file_name"`
	MimeType string `json:"mime_type"`
	Size     int64  `json:"size"`
}

var fileSchema = z.Struct[inboundFile](z.Shape{
	"file_id":   z.String().Regex(identifier),
	"file_name": z.String().Max(255),
	// Empty is allowed: a photo carries no name or type, and the server
	// detects the real type from the bytes anyway.
	"mime_type": z.String().Max(127).Refine(func(s string) error {
		if s != "" && !mediaType.MatchString(strings.ToLower(s)) {
			return errors.New("is not a media type")
		}

		return nil
	}),
	// Telegram omits the size for some files; 0 is let through and the real
	// size is counted as the bytes arrive.
	"size": z.Int64().Min(0).Max(MaxFileBytes),
})

// webURL accepts what a person pastes as a link to a video: http or https,
// with a host, and nothing a chat client would break across a line.
var webURL = z.String().Min(8).Max(2048).URL().Refine(func(s string) error {
	u, err := url.Parse(s)
	if err != nil {
		return errors.New("is not a URL")
	}

	if (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return errors.New("must be an http or https link")
	}

	if u.User != nil {
		return errors.New("must not carry a username or password")
	}

	for _, r := range s {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return errors.New("must be one link with no spaces")
		}
	}

	return nil
})

// checkLink validates a pasted link and returns it cleaned.
func checkLink(raw string) (string, error) {
	raw = strings.TrimSpace(raw)

	if _, err := webURL.Parse(raw); err != nil {
		return "", err
	}

	return raw, nil
}

// ---------------------------------------------------------------------------
// What the server sends back.
// ---------------------------------------------------------------------------

// conversionState is a conversion as the bot reads it.
type conversionState struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Progress int64  `json:"progress"`
	Message  string `json:"message"`
	Note     string `json:"note"`
}

var conversionSchema = z.Struct[conversionState](z.Shape{
	"id":       z.String().Regex(identifier),
	"status":   z.String().OneOf("pending", "running", "finished", "error", "cancelled"),
	"progress": z.Int64().Min(0).Max(100),
	"message":  z.String().Max(2000),
	"note":     z.String().Max(2000),
})

// resultFile is one downloadable result.
type resultFile struct {
	Name string `json:"name"`
	Mime string `json:"mime"`
	Size int64  `json:"size"`
	URL  string `json:"url"`
}

var resultSchema = z.Struct[resultFile](z.Shape{
	"name": z.String().Min(1).Max(255),
	"mime": z.String().Max(127).Refine(func(s string) error {
		if s != "" && !mediaType.MatchString(strings.ToLower(s)) {
			return errors.New("is not a media type")
		}

		return nil
	}),
	"size": z.Int64().Min(0).Max(8 << 30),
	"url":  z.String().Min(8).Max(4096).URL(),
})

// usageState is today's budget.
type usageState struct {
	Unlimited bool  `json:"unlimited"`
	Budget    int64 `json:"budget"`
	Spent     int64 `json:"spent"`
	Remaining int64 `json:"remaining"`
}

var usageSchema = z.Struct[usageState](z.Shape{
	"unlimited": z.Bool(),
	"budget":    z.Int64().Min(0),
	"spent":     z.Int64().Min(0),
	"remaining": z.Int64().Min(0),
})

// account is who a key belongs to.
type account struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

var accountSchema = z.Struct[account](z.Shape{
	"name":  z.String().Max(200),
	"email": z.String().Min(3).Max(320),
})

// issuedKey is what an approved link hands over.
type issuedKey struct {
	Token string `json:"token"`
}

var keySchema = z.Struct[issuedKey](z.Shape{
	"token": z.String().Min(16).Max(512).Refine(func(s string) error {
		for _, r := range s {
			if unicode.IsSpace(r) || unicode.IsControl(r) {
				return errors.New("must not contain whitespace")
			}
		}

		return nil
	}),
})

// startedLink is the answer to asking for a sign-in link.
type startedLink struct {
	DeviceCode string `json:"device_code"`
	UserCode   string `json:"user_code"`
	URL        string `json:"url"`
	ExpiresIn  int64  `json:"expires_in"`
	Interval   int64  `json:"interval"`
}

var linkSchema = z.Struct[startedLink](z.Shape{
	"device_code": z.String().Min(16).Max(128).Regex(identifier),
	"user_code":   z.String().Regex(regexp.MustCompile(`^[A-Z0-9]{4}-[A-Z0-9]{4}$`)),
	"url":         z.String().Min(8).Max(2048).URL(),
	"expires_in":  z.Int64().Min(30).Max(3600),
	"interval":    z.Int64().Min(1).Max(30),
})

// sameHost reports whether a link points at the API's own host. A result or
// sign-in link that leads anywhere else is not something this service made,
// and the bot does not forward it.
func sameHost(raw, base string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}

	b, err := url.Parse(base)
	if err != nil {
		return false
	}

	return strings.EqualFold(u.Host, b.Host) && (u.Scheme == "https" || b.Scheme == "http")
}

// plain makes a string safe to put in a chat: control characters and line
// breaks become spaces, runs of space collapse, and it is cut to max runes.
// The bot sends plain text, so there is nothing to escape; this is about not
// letting a file name or a title reshape a message. limit is in runes.
func plain(s string, limit int) string {
	var b strings.Builder

	space := true

	for _, r := range s {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			if !space {
				b.WriteRune(' ')
			}

			space = true

			continue
		}

		b.WriteRune(r)

		space = false
	}

	out := strings.TrimSpace(b.String())

	if r := []rune(out); len(r) > limit {
		return string(r[:limit-1]) + "…"
	}

	return out
}
