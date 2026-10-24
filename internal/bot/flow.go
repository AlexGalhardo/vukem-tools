package bot

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/AlexGalhardo/vukem-tools/clients/apiclient"
)

// flow is what one person is in the middle of: a tool picked and the files
// sent for it so far, or a link waiting for a format.
//
// In memory and bounded, like the sessions: it is a half-finished sentence,
// not a record, and losing it on a restart costs the person one more click.
type flow struct {
	tool   *Tool
	files  []stagedFile
	choice string
	// download is set for the link-based flow, which has no tool in the table.
	download bool
	url      string
	title    string
	// menu is the message to keep editing as the person moves along.
	menu int64
	at   time.Time
}

// stagedFile is a file already uploaded for the running flow.
type stagedFile struct {
	id, name, mime string
	size           int64
}

func (b *Bot) flowOf(user int64) *flow {
	b.mu.Lock()
	defer b.mu.Unlock()

	f, ok := b.flows[user]
	if !ok {
		return nil
	}

	if b.now().Sub(f.at) > flowTTL {
		delete(b.flows, user)

		return nil
	}

	return f
}

func (b *Bot) setFlow(user int64, f *flow) {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := b.now()
	f.at = now

	if _, replacing := b.flows[user]; !replacing && len(b.flows) >= flowLimit {
		for id, other := range b.flows {
			if now.Sub(other.at) > flowTTL {
				delete(b.flows, id)
			}
		}

		if len(b.flows) >= flowLimit {
			// Still full: drop the oldest rather than grow.
			var (
				oldest int64
				at     time.Time
				found  bool
			)

			for id, other := range b.flows {
				if !found || other.at.Before(at) {
					oldest, at, found = id, other.at, true
				}
			}

			delete(b.flows, oldest)
		}
	}

	b.flows[user] = f
}

func (b *Bot) clearFlow(user int64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	delete(b.flows, user)
}

// ---------------------------------------------------------------------------
// Menus.
// ---------------------------------------------------------------------------

// showMain is the main menu: who is signed in, what is left of today's
// budget, and the buttons for everything the bot can do.
func (b *Bot) showMain(ctx context.Context, id who, messageID int64) {
	s, ok := b.sessionOf(id.user)
	if !ok {
		b.offerLogin(ctx, id, messageID, "")

		return
	}

	b.clearFlow(id.user)

	var text strings.Builder

	text.WriteString("Tools\n")
	text.WriteString("Signed in as " + s.name)

	if s.email != "" {
		text.WriteString(" (" + s.email + ")")
	}

	text.WriteString("\n")

	usage, err := s.API.Usage(ctx)

	switch {
	case isUnauthorised(err):
		b.lostSession(ctx, id, messageID)

		return
	case err == nil:
		if line, ok := b.usageLine(usage); ok {
			text.WriteString(line + "\n")
		}
	}

	text.WriteString("\nWhat would you like to do?")

	rows := make([][]Button, 0, len(groups)/2+3)

	var pair []Button

	for _, g := range groups {
		pair = append(pair, Button{Text: g.Label, Data: "g:" + g.ID})
		if len(pair) == 2 {
			rows = append(rows, pair)
			pair = nil
		}
	}

	pair = append(pair, Button{Text: "Download a video", Data: "dl"})

	rows = append(rows, pair, []Button{{Text: "Refresh", Data: "m"}, {Text: "Sign out", Data: "logout"}})

	b.show(ctx, id.chat, messageID, text.String(), &Keyboard{Rows: rows})
}

// usageLine is the sentence about today's budget, checked before it is shown.
func (b *Bot) usageLine(v apiclient.UsageView) (string, bool) {
	u := usageState{
		Unlimited: valueOr(v.Unlimited, false),
		Budget:    valueOr(v.BudgetSeconds, 0),
		Spent:     valueOr(v.SpentSeconds, 0),
		Remaining: valueOr(v.RemainingSeconds, 0),
	}

	if err := usageSchema.Check(u); err != nil {
		b.log.Warn("bot: the usage figures failed validation", "err", err)

		return "", false
	}

	if u.Unlimited {
		return "Today: " + humanSeconds(u.Spent) + " used, no daily limit", true
	}

	line := fmt.Sprintf(
		"Today: %s of %s used, %s left",
		humanSeconds(u.Spent),
		humanSeconds(u.Budget),
		humanSeconds(u.Remaining),
	)

	if v.ResetsAt != nil {
		line += ", resets at " + v.ResetsAt.UTC().Format("15:04") + " UTC"
	}

	return line, true
}

// showGroup lists a group's tools.
func (b *Bot) showGroup(ctx context.Context, id who, messageID int64, group string) {
	g, ok := groupByID(group)
	if !ok {
		b.showMain(ctx, id, messageID)

		return
	}

	rows := make([][]Button, 0, len(tools)+1)
	for _, t := range toolsIn(g.ID) { //nolint:gocritic // rangeValCopy: a handful of entries, once per menu press
		rows = append(rows, []Button{{Text: t.Label, Data: "t:" + t.ID}})
	}

	rows = append(rows, []Button{{Text: "Back", Data: "m"}})

	b.show(ctx, id.chat, messageID, g.Label+"\n\nPick one.", &Keyboard{Rows: rows})
}

// press routes a button.
func (b *Bot) press(ctx context.Context, id who, messageID int64, data string) {
	switch data {
	case "login":
		b.startLogin(ctx, id, messageID, id.label)

		return
	case "cancel":
		b.cancelLogin(id.user)
		b.clearFlow(id.user)
		b.showMain(ctx, id, messageID)

		return
	}

	s, ok := b.sessionOf(id.user)
	if !ok {
		b.offerLogin(ctx, id, messageID, "Sign in first.")

		return
	}

	kind, arg, _ := strings.Cut(data, ":")

	switch kind {
	case "m":
		b.showMain(ctx, id, messageID)
	case "logout":
		b.logout(ctx, id, messageID)
	case "g":
		b.showGroup(ctx, id, messageID, arg)
	case "t":
		b.selectTool(ctx, id, messageID, arg)
	case "dl":
		b.setFlow(id.user, &flow{download: true, menu: messageID})
		b.show(ctx, id.chat, messageID,
			"Send me the link to a video on YouTube, Instagram, X or TikTok.\n\n"+
				"Downloading may breach the platform's terms of service, and you are responsible for what you download.",
			&Keyboard{Rows: [][]Button{{{Text: "Cancel", Data: "cancel"}}}})
	case "c":
		b.choose(ctx, id, s, messageID, arg)
	case "done":
		b.finishCollecting(ctx, id, s, messageID)
	case "dv":
		b.chooseDownload(ctx, id, s, messageID, arg)
	case "x":
		b.cancelConversion(ctx, id, s, messageID, arg)
	default:
		b.show(ctx, id.chat, messageID, "I do not recognise that button. Here is the menu.", nil)
		b.showMain(ctx, id, 0)
	}
}

// selectTool starts a tool's flow.
func (b *Bot) selectTool(ctx context.Context, id who, messageID int64, toolID string) {
	t, ok := toolByID(toolID)
	if !ok {
		b.showMain(ctx, id, messageID)

		return
	}

	b.setFlow(id.user, &flow{tool: &t, menu: messageID})

	b.show(ctx, id.chat, messageID, t.Label+"\n\n"+t.Prompt+"\n\nAt most "+formatBytes(MaxFileBytes)+" per file.",
		&Keyboard{Rows: [][]Button{{{Text: "Back", Data: "g:" + t.Group}, {Text: "Cancel", Data: "cancel"}}}})
}

// ---------------------------------------------------------------------------
// Files.
// ---------------------------------------------------------------------------

// fileTimeout bounds moving one file from Telegram to the server.
const fileTimeout = 3 * time.Minute

// handleFile takes a file the person sent for the tool they picked.
func (b *Bot) handleFile(ctx context.Context, id who, file inboundFile) {
	s, ok := b.sessionOf(id.user)
	if !ok {
		b.offerLogin(ctx, id, 0, "Sign in first.")

		return
	}

	f := b.flowOf(id.user)
	if f == nil || f.tool == nil {
		b.say(ctx, id.chat, "Pick what to do first, then send the file.")
		b.showMain(ctx, id, 0)

		return
	}

	if err := fileSchema.Check(file); err != nil {
		b.say(
			ctx,
			id.chat,
			"I cannot take that file: it is larger than "+formatBytes(MaxFileBytes)+" or its details are not valid.",
		)

		return
	}

	if len(f.files) >= f.tool.Max {
		b.say(ctx, id.chat, "That is all the files this tool takes. Press Done.")

		return
	}

	b.say(ctx, id.chat, "Receiving "+plain(file.FileName, 60)+"…")

	staged, err := b.stage(ctx, s, file, f)
	if err != nil {
		if isUnauthorised(err) {
			b.lostSession(ctx, id, 0)

			return
		}

		b.say(ctx, id.chat, apiMessage(err))

		return
	}

	if !f.tool.accepts(staged.mime) {
		b.say(
			ctx,
			id.chat,
			fmt.Sprintf("That is %s, and %s does not read it. %s", plain(staged.mime, 60), f.tool.Label, f.tool.Prompt),
		)

		return
	}

	f.files = append(f.files, staged)
	b.setFlow(id.user, f)

	if f.tool.multi() && len(f.files) < f.tool.Max {
		b.show(ctx, id.chat, 0, fmt.Sprintf("Got %s (%d of at most %d). Send the next one, or press Done.",
			plain(staged.name, 60), len(f.files), f.tool.Max),
			&Keyboard{Rows: [][]Button{{{Text: "Done", Data: "done"}, {Text: "Cancel", Data: "cancel"}}}})

		return
	}

	b.afterFiles(ctx, id, s, f)
}

// stage moves a file from Telegram to the server and finds the id the server
// gave it.
//
// The tus protocol answers with no id, so the stored upload is found by
// listing the person's unclaimed uploads, newest first, and taking the newest
// that is the right size and was not there before.
func (b *Bot) stage(ctx context.Context, s *session, file inboundFile, f *flow) (stagedFile, error) {
	ctx, cancel := context.WithTimeout(ctx, fileTimeout)
	defer cancel()

	started := b.now().Add(-5 * time.Second)

	info, err := b.tg.GetFile(ctx, file.FileID)
	if err != nil {
		return stagedFile{}, fmt.Errorf("bot: ask Telegram for the file: %w", err)
	}

	size := info.FileSize
	if size <= 0 {
		size = file.Size
	}

	if size <= 0 || size > MaxFileBytes {
		return stagedFile{}, &UploadError{Status: 413}
	}

	body, err := b.tg.OpenFile(ctx, info.FilePath)
	if err != nil {
		return stagedFile{}, fmt.Errorf("bot: fetch the file from Telegram: %w", err)
	}

	defer func() { _ = body.Close() }()

	name := plain(file.FileName, 120)
	if name == "" {
		name = "file"
	}

	if err := s.Uploader.Upload(ctx, name, size, body); err != nil {
		return stagedFile{}, err
	}

	list, err := s.API.UploadsIndex(ctx)
	if err != nil {
		return stagedFile{}, err
	}

	known := make(map[string]bool, len(f.files))
	for _, have := range f.files {
		known[have.id] = true
	}

	for _, u := range list.Data {
		uid := valueOr(u.ID, "")
		if uid == "" || known[uid] || valueOr(u.SizeBytes, 0) != size {
			continue
		}

		if u.CreatedAt != nil && u.CreatedAt.Before(started) {
			continue
		}

		if err := fileSchema.Check(
			inboundFile{FileID: uid, FileName: valueOr(u.Name, "file"), MimeType: valueOr(u.Mime, ""), Size: size},
		); err != nil {
			b.log.WarnContext(ctx, "bot: an upload description failed validation", "err", err)

			continue
		}

		return stagedFile{id: uid, name: valueOr(u.Name, name), mime: valueOr(u.Mime, ""), size: size}, nil
	}

	return stagedFile{}, &UploadError{Status: 502}
}

// finishCollecting is the Done button of a multi-file tool.
func (b *Bot) finishCollecting(ctx context.Context, id who, s *session, messageID int64) {
	f := b.flowOf(id.user)
	if f == nil || f.tool == nil {
		b.showMain(ctx, id, messageID)

		return
	}

	if len(f.files) < f.tool.Min {
		b.show(
			ctx,
			id.chat,
			messageID,
			fmt.Sprintf("%s needs at least %d files and you have sent %d.", f.tool.Label, f.tool.Min, len(f.files)),
			&Keyboard{Rows: [][]Button{{{Text: "Cancel", Data: "cancel"}}}},
		)

		return
	}

	f.menu = messageID
	b.afterFiles(ctx, id, s, f)
}

// afterFiles asks the tool's question, or runs it when it has none.
func (b *Bot) afterFiles(ctx context.Context, id who, s *session, f *flow) {
	if f.tool.Choice == nil {
		b.launch(ctx, id, s, f)

		return
	}

	rows := make([][]Button, 0, len(f.tool.Choice.Options)/2+2)

	var pair []Button

	for _, o := range f.tool.Choice.Options {
		pair = append(pair, Button{Text: o.Label, Data: "c:" + o.Value})
		if len(pair) == 2 {
			rows = append(rows, pair)
			pair = nil
		}
	}

	if len(pair) > 0 {
		rows = append(rows, pair)
	}

	rows = append(rows, []Button{{Text: "Cancel", Data: "cancel"}})

	f.menu = b.show(ctx, id.chat, f.menu, f.tool.Choice.Question, &Keyboard{Rows: rows})
	b.setFlow(id.user, f)
}

// choose records the answer to a tool's question and runs it.
func (b *Bot) choose(ctx context.Context, id who, s *session, messageID int64, value string) {
	f := b.flowOf(id.user)
	if f == nil || f.tool == nil || len(f.files) == 0 {
		b.show(ctx, id.chat, messageID, "That choice has expired.", nil)
		b.showMain(ctx, id, 0)

		return
	}

	// Only a value the tool itself put in a button is accepted: what reaches
	// the API is from the table, never from the press.
	if !f.tool.choiceAllows(value) {
		b.show(ctx, id.chat, messageID, "I do not recognise that choice.", nil)
		b.showMain(ctx, id, 0)

		return
	}

	f.choice, f.menu = value, messageID
	b.launch(ctx, id, s, f)
}

// launch submits the conversion and hands it to a watcher.
func (b *Bot) launch(ctx context.Context, id who, s *session, f *flow) {
	tool, choice := *f.tool, f.choice

	ids := make([]string, 0, len(f.files))
	for _, have := range f.files {
		ids = append(ids, have.id)
	}

	menu := f.menu
	b.clearFlow(id.user)

	conversion, err := tool.Submit(ctx, s.API, ids, choice)
	if err != nil {
		if isUnauthorised(err) {
			b.lostSession(ctx, id, menu)

			return
		}

		b.show(
			ctx,
			id.chat,
			menu,
			apiMessage(err),
			&Keyboard{Rows: [][]Button{{{Text: "Back to the menu", Data: "m"}}}},
		)

		return
	}

	b.watch(ctx, id, s, menu, conversion, tool.Label, false)
}

// ---------------------------------------------------------------------------
// Downloads by link.
// ---------------------------------------------------------------------------

// handleLink is a pasted link: it starts the download flow from anywhere.
func (b *Bot) handleLink(ctx context.Context, id who, link string) {
	s, ok := b.sessionOf(id.user)
	if !ok {
		b.offerLogin(ctx, id, 0, "Sign in first.")

		return
	}

	media, err := s.API.ToolsVideoDownloadMetadata(ctx, apiclient.ToolsVideoDownloadMetadataBody{URL: link})
	if err != nil {
		if isUnauthorised(err) {
			b.lostSession(ctx, id, 0)

			return
		}

		b.say(ctx, id.chat, apiMessage(err))

		return
	}

	if valueOr(media.TooLarge, false) {
		b.say(ctx, id.chat, fmt.Sprintf("That video is about %s, and this server downloads at most %s.",
			formatBytes(valueOr(media.EstimatedBytes, 0)), formatBytes(valueOr(media.MaxBytes, 0))))

		return
	}

	title := plain(valueOr(media.Title, "that video"), 120)

	summary := fmt.Sprintf("%s\nby %s · %s · about %s\n\nWhat would you like?",
		title, plain(valueOr(media.Uploader, "unknown"), 60),
		formatDuration(valueOr(media.DurationSeconds, 0)), formatBytes(valueOr(media.EstimatedBytes, 0)))

	menu := b.show(ctx, id.chat, 0, summary, &Keyboard{Rows: [][]Button{
		{{Text: "MP4, best", Data: "dv:best"}, {Text: "MP4, smallest", Data: "dv:small"}},
		{{Text: "MP3", Data: "dv:audio"}, {Text: "60-second clips", Data: "dv:clips"}},
		{{Text: "Cancel", Data: "cancel"}},
	}})

	b.setFlow(id.user, &flow{download: true, url: link, title: title, menu: menu})
}

// chooseDownload runs the download with the format the person pressed.
func (b *Bot) chooseDownload(ctx context.Context, id who, s *session, messageID int64, choice string) {
	f := b.flowOf(id.user)
	if f == nil || !f.download || f.url == "" {
		b.show(ctx, id.chat, messageID, "That link has expired. Send it again.", nil)
		b.showMain(ctx, id, 0)

		return
	}

	// A closed set: only these four strings ever select options.
	var kind, quality string

	var clips bool

	switch choice {
	case "best":
		kind, quality = "video", "best"
	case "small":
		kind, quality = "video", "smallest"
	case "audio":
		kind, quality = "audio", "best"
	case "clips":
		kind, quality, clips = "video", "best", true
	default:
		b.show(ctx, id.chat, messageID, "I do not recognise that choice.", nil)
		b.showMain(ctx, id, 0)

		return
	}

	url := f.url
	b.clearFlow(id.user)

	conversion, err := s.API.ToolsVideoDownload(ctx, apiclient.ToolsVideoDownloadBody{
		Options: &apiclient.DownloadOptions{
			URL: url, Kind: &kind, Quality: &quality, Clips: &clips,
			// Compression is a separate choice the bot does not offer: a chat
			// wants the file now, and a second re-encode doubles the wait.
			Compress: new(false),
		},
	})
	if err != nil {
		if isUnauthorised(err) {
			b.lostSession(ctx, id, messageID)

			return
		}

		b.show(
			ctx,
			id.chat,
			messageID,
			apiMessage(err),
			&Keyboard{Rows: [][]Button{{{Text: "Back to the menu", Data: "m"}}}},
		)

		return
	}

	b.watch(ctx, id, s, messageID, conversion, "Download", clips)
}
