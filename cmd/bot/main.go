// Command bot runs the Telegram front end: paste a URL, choose a format,
// receive the file.
//
// It is a separate binary from cmd/app on purpose. The bot is a client of the
// HTTP API with an API key that carries one ability, so it needs no database,
// no bucket, no engines and no signing key — and running it in the web
// process would have given it all five. It also means the bot can be
// restarted, scaled or switched off without touching the API.
//
// Every setting comes from the environment, because that is what the platform
// supplies and the bot has no configuration file of its own:
//
//	TELEGRAM_BOT_TOKEN       the bot's token from BotFather (required)
//	TELEGRAM_ALLOWED_USERS   comma-separated Telegram user ids (required)
//	BOT_API_URL              the base URL of the vukem-tools API (required)
//	BOT_API_KEY              an API key with only the video:download ability
//	DEPLOY_TARGET            what /status calls this deploy
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

	api, apiDeploy, err := bot.NewAPI(os.Getenv("BOT_API_URL"), os.Getenv("BOT_API_KEY"), nil)
	if err != nil {
		return err
	}

	b, err := bot.New(bot.Options{
		Telegram:        telegram,
		API:             api,
		AllowedUsers:    allowed,
		DeployTarget:    os.Getenv("DEPLOY_TARGET"),
		APIDeployTarget: apiDeploy,
		Log:             log,
	})
	if err != nil {
		return err
	}

	// SIGTERM is what a platform sends to stop a container, and SIGINT is
	// Ctrl-C. Both cancel the context, which ends the poll loop between
	// updates rather than in the middle of one.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Info("bot: starting", "allowed_users", len(allowed), "abilities", bot.Abilities())

	return b.Run(ctx)
}

// allowedUsers parses the allow-list.
//
// An empty list is an error here rather than a warning: bot.New tolerates it
// and refuses everybody, which is right for a library, but a deploy that
// forgot the list should fail loudly instead of running a bot nobody can use
// and nobody notices is broken.
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

	if len(ids) == 0 {
		return nil, errors.New("TELEGRAM_ALLOWED_USERS is required: a download bot with no allow-list is " +
			"a download bot for the internet")
	}

	return ids, nil
}
