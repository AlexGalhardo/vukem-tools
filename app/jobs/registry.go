// Package jobs holds the queued jobs.
package jobs

import (
	vukem "github.com/AlexGalhardo/vukem"
	"github.com/AlexGalhardo/vukem/queue"

	"github.com/AlexGalhardo/vukem-tools/app/services"
)

// All lists the job handlers. It takes the resolved conversion pipeline
// rather than building one, so the worker and the web routes share a single
// event hub; vukem make:job appends here.
//
// vukem.WithJobsFunc cannot report an error, so services.Provider resolves
// the pipeline during boot and the app does not start when that fails. The
// check here is the belt to that braces: returning no handler is better than
// a nil dereference, and the log line says which it was.
func All(c *services.Conversions) func(app *vukem.App) []queue.Registration {
	return func(app *vukem.App) []queue.Registration {
		if err := c.Resolve(app); err != nil {
			app.Log.Error("jobs: the conversion pipeline is unavailable, no handler registered", "err", err)

			return nil
		}

		return []queue.Registration{c.Pipeline().Registration()}
	}
}
