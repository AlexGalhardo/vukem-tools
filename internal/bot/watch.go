package bot

import (
	"context"
	"fmt"
	"strings"

	"github.com/AlexGalhardo/vukem-tools/clients/apiclient"
)

// watch follows a conversion from its own goroutine, so one person's long
// conversion never holds up everybody else's menus, and delivers the result.
//
// The number of conversions followed at once is bounded: past the bound the
// next one waits for a slot, which is the bot being polite to the API, not a
// refusal.
func (b *Bot) watch(
	ctx context.Context,
	id who,
	s *session,
	menu int64,
	view apiclient.ConversionView,
	label string,
	clips bool,
) {
	state, err := b.readConversion(view)
	if err != nil {
		b.log.ErrorContext(ctx, "bot: the server described a conversion strangely", "err", err)
		b.show(ctx, id.chat, menu, "The server accepted that but sent something I did not expect. Try again.",
			&Keyboard{Rows: [][]Button{{{Text: "Back to the menu", Data: "m"}}}})

		return
	}

	cancel := &Keyboard{Rows: [][]Button{{{Text: "Cancel", Data: "x:" + state.ID}}}}
	progress := b.show(ctx, id.chat, menu, label+": working…", cancel)

	b.wg.Go(func() {
		select {
		case b.watching <- struct{}{}:
			defer func() { <-b.watching }()
		case <-ctx.Done():
			return
		}

		b.follow(ctx, id, s, progress, state.ID, label, clips, cancel)
	})
}

// readConversion validates what the server said about a conversion.
func (b *Bot) readConversion(v apiclient.ConversionView) (conversionState, error) {
	state := conversionState{
		ID:       valueOr(v.ID, ""),
		Status:   valueOr(v.Status, ""),
		Progress: valueOr(v.Progress, 0),
		Message:  valueOr(v.ErrorMessage, ""),
		Note:     valueOr(v.Note, ""),
	}

	if err := conversionSchema.Check(state); err != nil {
		return conversionState{}, err
	}

	return state, nil
}

// follow polls until the conversion ends, keeping one message up to date, then
// delivers.
func (b *Bot) follow(
	ctx context.Context,
	id who,
	s *session,
	progress int64,
	convID, label string,
	clips bool,
	cancel *Keyboard,
) {
	deadline := b.now().Add(b.conversion)
	lastShown := int64(-1)

	for {
		view, err := s.API.ConversionsShow(ctx, convID)
		if err != nil {
			if isUnauthorised(err) {
				b.lostSession(ctx, id, progress)

				return
			}

			b.show(ctx, id.chat, progress, apiMessage(err), backToMenu())

			return
		}

		state, err := b.readConversion(view)
		if err != nil {
			b.log.ErrorContext(ctx, "bot: the server described a conversion strangely", "err", err)
			b.show(ctx, id.chat, progress, "The server sent something I did not expect. Try again.", backToMenu())

			return
		}

		switch state.Status {
		case "finished":
			b.deliver(ctx, id, s, progress, convID, label, state, clips)

			return
		case "cancelled":
			b.show(ctx, id.chat, progress, label+": cancelled.", backToMenu())

			return
		case "error":
			message := plain(state.Message, 600)
			if message == "" {
				message = "That did not work, and the server did not say why."
			}

			b.show(ctx, id.chat, progress, message, backToMenu())

			return
		}

		if state.Progress != lastShown {
			lastShown = state.Progress
			b.show(ctx, id.chat, progress, fmt.Sprintf("%s: working… %d%%", label, state.Progress), cancel)
		}

		if b.now().After(deadline) {
			b.show(ctx, id.chat, progress, apiMessage(errStillRunning), backToMenu())

			return
		}

		if !sleep(ctx, b.pollInterval) {
			return
		}
	}
}

func backToMenu() *Keyboard {
	return &Keyboard{Rows: [][]Button{{{Text: "Back to the menu", Data: "m"}}}}
}

// cancelConversion is the Cancel button of a running conversion.
func (b *Bot) cancelConversion(ctx context.Context, id who, s *session, messageID int64, convID string) {
	// The API only lets a person cancel their own conversions, so a forged id
	// reaches nothing; the format check keeps a malformed one off the wire.
	if err := conversionSchema.Check(conversionState{ID: convID, Status: "running"}); err != nil {
		b.show(ctx, id.chat, messageID, "I do not recognise that.", backToMenu())

		return
	}

	if _, err := s.API.ConversionsCancel(ctx, convID); err != nil {
		if isUnauthorised(err) {
			b.lostSession(ctx, id, messageID)

			return
		}

		b.show(ctx, id.chat, messageID, apiMessage(err), backToMenu())

		return
	}

	b.show(ctx, id.chat, messageID, "Cancelling…", nil)
}

// deliver sends the finished files and then the menu.
//
// Telegram fetches each file from its signed link instead of the bot passing
// the bytes along: streaming a result through this process would cost the
// bandwidth twice. The cost is Telegram's own 20 MB limit on a URL it fetches,
// which is why a refused file is followed by the link itself.
func (b *Bot) deliver(
	ctx context.Context,
	id who,
	s *session,
	progress int64,
	convID, label string,
	state conversionState,
	clips bool,
) {
	results, err := s.API.ConversionsResult(ctx, convID)
	if err != nil {
		if isUnauthorised(err) {
			b.lostSession(ctx, id, progress)

			return
		}

		b.show(ctx, id.chat, progress, apiMessage(err), backToMenu())

		return
	}

	done := label + ": done."
	if note := plain(state.Note, 500); note != "" {
		done += "\n" + note
	}

	b.show(ctx, id.chat, progress, done, nil)

	sent := 0

	for _, raw := range results.Data {
		file := resultFile{
			Name: valueOr(raw.Name, "file"), Mime: valueOr(raw.Mime, ""),
			Size: valueOr(raw.SizeBytes, 0), URL: valueOr(raw.URL, ""),
		}

		// A result is repeated into a chat and its link is followed by
		// Telegram's servers, so both must be what this service makes.
		if err := resultSchema.Check(file); err != nil || !sameHost(file.URL, b.base) {
			b.log.WarnContext(ctx, "bot: a result failed validation and was not sent", "err", err)

			continue
		}

		if clips && strings.HasSuffix(strings.ToLower(file.Name), ".zip") {
			continue
		}

		name := plain(file.Name, 100)

		if err := b.send(ctx, id.chat, file.Mime, file.URL, name, file.Size); err != nil {
			b.log.WarnContext(ctx, "bot: could not send a result", "chat", id.chat, "err", err)

			b.say(
				ctx,
				id.chat,
				name+"\nTelegram could not fetch that itself, so here is the link. It expires in a few minutes.\n"+file.URL,
			)
		}

		sent++
	}

	switch {
	case sent == 0:
		b.say(ctx, id.chat, "That finished but produced nothing I can send.")
	case clips:
		b.say(ctx, id.chat, fmt.Sprintf("That is all %d clips. The links expire in a few minutes.", sent))
	}

	b.showMain(ctx, id, 0)
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
