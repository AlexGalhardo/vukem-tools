// Command bot runs the Telegram front end: sign in once, pick a tool from a
// menu, send a file or a link, receive the result.
//
// It is a separate binary from cmd/app on purpose. The bot is a client of the
// HTTP API and holds no key of its own: each person signs in to their own
// account through a link the server approves, and the bot keeps their key in
// memory for as long as they stay signed in. So it needs no database, no
// bucket, no engines and no signing key, and running it in the web process
// would have given it all four. It also means the bot can be restarted,
// scaled or switched off without touching the API; a restart signs everybody
// out, which costs one click each.
//
// Every setting comes from the environment, because that is what the platform
// supplies and the bot has no configuration file of its own:
//
//	TELEGRAM_BOT_TOKEN       the bot's token from BotFather (required)
//	BOT_API_URL              the base URL of the vukem-tools app (required)
//	TELEGRAM_ALLOWED_USERS   comma-separated Telegram user ids; empty lets
//	                         anybody try to sign in, which reaches only their
//	                         own account
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/AlexGalhardo/vukem-tools/internal/bot"
)

func main() {
	if err := run(); err != nil {
		// slog rather than the error being printed by the runtime: the
		// platform collects structured logs, and a bot that will not start
		// should say why in the same format as everything else it logs.
		slog.Error("bot: could not start", "err", err)
		os.Exit(1)
	}
}

func run() error {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	token := strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN"))
	if token == "" {
		return errors.New("TELEGRAM_BOT_TOKEN is required")
	}

	allowed, err := allowedUsers(os.Getenv("TELEGRAM_ALLOWED_USERS"))
	if err != nil {
		return err
	}

	telegram, err := bot.NewTelegram(token, os.Getenv("TELEGRAM_API_URL"), nil)
	if err != nil {
		return err
	}

	apiURL := strings.TrimSpace(os.Getenv("BOT_API_URL"))

	links, err := bot.NewLinks(apiURL, nil)
	if err != nil {
		return err
	}

	b, err := bot.New(bot.Options{
		Telegram:     telegram,
		Links:        links,
		Connect:      func(key string) (bot.Session, error) { return bot.NewSession(apiURL, key, nil) },
		APIBaseURL:   apiURL,
		AllowedUsers: allowed,
		Log:          log,
	})
	if err != nil {
		return err
	}

	// SIGTERM is what a platform sends to stop a container, and SIGINT is
	// Ctrl-C. Both cancel the context, which ends the poll loop between
	// updates rather than in the middle of one.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Info("bot: starting", "restricted", len(allowed) > 0)

	return b.Run(ctx)
}

// allowedUsers parses the optional allow-list.
//
// Empty is valid: the sign-in is what keeps a stranger out of anybody's
// account, so the list is a way to also keep strangers from using the bot's
// capacity, not the only gate. Garbage is not valid: a list that silently
// parsed to fewer ids than were written would let in people the owner meant
// to keep out, or lock out the people they meant to let in.
func allowedUsers(raw string) ([]int64, error) {
	var ids []int64

	for part := range strings.SplitSeq(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		id, err := strconv.ParseInt(part, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("TELEGRAM_ALLOWED_USERS: %q is not a Telegram user id", part)
		}

		if id == 0 {
			// Zero is what an update with no sender would compare as, so it
			// must never be allowed by configuration.
			return nil, errors.New("TELEGRAM_ALLOWED_USERS: 0 is not a Telegram user id")
		}

		ids = append(ids, id)
	}

	return ids, nil
}
