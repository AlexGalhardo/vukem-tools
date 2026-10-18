package routes

import (
	"context"
	"time"

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

		// Deleting expired results. This is what makes the five-minute
		// promise true rather than decorative, so it runs often: a user is
		// told their result lives five minutes, and a sweep every minute
		// means it is gone within six rather than within an hour.
		s.Func("files:sweep", func(ctx context.Context) error {
			if err := c.Resolve(app); err != nil {
				return err
			}

			n, err := c.Files().Sweep(ctx, 0)
			if err != nil {
				return err
			}

			if n > 0 {
				app.Log.InfoContext(ctx, "cleanup: expired files deleted", "count", n)
			}

			return nil
		}).Every(time.Minute).WithoutOverlapping()

		// Deleting resumable uploads nobody finished. Hourly is plenty: these
		// are bytes on a local disk with a 24-hour life, not a promise to
		// anybody.
		s.Func("uploads:sweep", func(ctx context.Context) error {
			if err := c.Resolve(app); err != nil {
				return err
			}

			n, err := c.SweepUploads(ctx)
			if err != nil {
				return err
			}

			if n > 0 {
				app.Log.InfoContext(ctx, "cleanup: abandoned uploads deleted", "count", n)
			}

			return nil
		}).Hourly().WithoutOverlapping()
	}
}
