// Package bootstrap wires the application together. There is no hidden
// registration: everything the app uses is listed here.
package bootstrap

import (
	"context"

	vukem "github.com/AlexGalhardo/vukem"
	"github.com/AlexGalhardo/vukem/drivers/postgres"
	"github.com/AlexGalhardo/vukem/inertia"
	"github.com/AlexGalhardo/vukem/queue"

	"github.com/AlexGalhardo/vukem-tools/app/jobs"
	"github.com/AlexGalhardo/vukem-tools/app/listeners"
	"github.com/AlexGalhardo/vukem-tools/database/migrations"
	"github.com/AlexGalhardo/vukem-tools/database/seeders"
	"github.com/AlexGalhardo/vukem-tools/resources"
	"github.com/AlexGalhardo/vukem-tools/routes"
)

// New builds the application; cmd/app and the tests call it.
func New(opts ...vukem.Option) (*vukem.App, error) {
	base := []vukem.Option{
		// PostgreSQL in every environment: the queue, sessions, cache and the
		// conversion tables all live there, and SQLite would not survive the
		// web and worker services being separate processes.
		vukem.WithDatabaseDrivers(postgres.Driver()),
		vukem.WithProviders(Providers()...),
		vukem.WithMigrations(migrations.All()),
		vukem.WithSeeders(seeders.All()...),
		vukem.WithListeners(listeners.All()...),
		vukem.WithJobsFunc(jobs.All),
		vukem.WithSchedule(routes.Schedule),
		vukem.WithViews(resources.Views()),
		vukem.WithVite(resources.Public()),
		vukem.WithInertia(inertia.Config{}),
	}

	for _, fn := range routes.All() {
		base = append(base, vukem.WithRoutes(fn))
	}

	app, err := vukem.New(append(base, opts...)...)
	if err != nil {
		return nil, err
	}

	app.AddCommand(Commands()...)
	addHealthChecks(app)

	return app, nil
}

// addHealthChecks registers what GET /up?checks=1 and ReadyHandler report.
//
// Each one resolves its dependency inside the check, so a dependency that is
// down at boot shows up as an unhealthy check rather than a failed boot. The
// bucket and the conversion engines get theirs when they land (S1.7, S1.11).
func addHealthChecks(app *vukem.App) {
	app.AddHealthCheck("database", func(ctx context.Context) error {
		db, err := app.DB()
		if err != nil {
			return err
		}

		return db.Ping(ctx)
	})

	// The queue is the path every conversion takes: a reachable database with
	// unusable queue tables would otherwise look healthy.
	//
	// Pending lives on the database driver rather than on queue.Driver, so the
	// depth read needs the concrete type. A different driver simply reports
	// healthy once app.Queue() resolves.
	app.AddHealthCheck("queue", func(ctx context.Context) error {
		q, err := app.Queue()
		if err != nil {
			return err
		}

		if d, ok := q.Driver().(*queue.Database); ok {
			_, err = d.Pending(ctx)

			return err
		}

		return nil
	})
}
