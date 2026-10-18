package routes

import (
	"context"

	"github.com/AlexGalhardo/vukem"
	"github.com/AlexGalhardo/vukem/schedule"

	"github.com/AlexGalhardo/vukem-tools/app/services"
)

// Schedule declares the scheduled tasks. "vukem serve" runs them in process;
// with SCHEDULE_IN_SERVE=false run "vukem schedule:work" instead. List them
// with "vukem schedule:list". Tasks must honor ctx: on shutdown it is
// cancelled after SHUTDOWN_TIMEOUT.
func Schedule(c *services.Conversions) func(s *schedule.Schedule, app *vukem.App) {
	return func(s *schedule.Schedule, app *vukem.App) {
		// Closing conversions nobody is working on any more. The queue cannot
		// tell a handler that its retries are spent, and a killed worker tells
		// nobody at all, so without this a user would watch a progress bar
		// that never moves again.
		//
		// WithoutOverlapping because two sweeps would read the same rows. The
		// conditional writes make that harmless, but the work would be wasted.
		s.Func("conversions:reap", func(ctx context.Context) error {
			if err := c.Resolve(app); err != nil {
				return err
			}

			_, err := c.Reaper().Sweep(ctx)

			return err
		}).Every(services.ReapEvery()).WithoutOverlapping()
	}
}
