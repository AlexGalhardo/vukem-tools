package bot_test

import (
	"log/slog"
	"testing"
)

// discardLogger keeps the bot's own logging out of the test output. The bot
// logs a warning for every refused update, and the refusal tests produce
// several on purpose.
func discardLogger(t *testing.T) *slog.Logger {
	t.Helper()

	return slog.New(slog.DiscardHandler)
}
