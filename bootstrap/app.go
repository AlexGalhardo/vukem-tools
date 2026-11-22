// Package bootstrap wires the application together. There is no hidden
// registration: everything the app uses is listed here.
package bootstrap

import (
	"context"
	"os"

	vukem "github.com/AlexGalhardo/vukem"
	"github.com/AlexGalhardo/vukem/drivers/postgres"
	"github.com/AlexGalhardo/vukem/inertia"
	"github.com/AlexGalhardo/vukem/queue"

	"github.com/AlexGalhardo/vukem-tools/app/jobs"
	"github.com/AlexGalhardo/vukem-tools/app/listeners"
	"github.com/AlexGalhardo/vukem-tools/app/services"
	"github.com/AlexGalhardo/vukem-tools/database/migrations"
	"github.com/AlexGalhardo/vukem-tools/database/seeders"
	"github.com/AlexGalhardo/vukem-tools/resources"
	"github.com/AlexGalhardo/vukem-tools/routes"
)

// New builds the application; cmd/app and the tests call it.
func New(opts ...vukem.Option) (*vukem.App, error) {
	// One conversion pipeline per process, resolved at boot by
	// services.Provider and shared by the routes, the worker and the
	// scheduler. Two would mean two event hubs, and each hub holds a
	// listening database connection per watched conversion.
	conversions := services.NewConversions()

	// Telemetry is its own service because it is registered before the
	// pipeline, and the pipeline records into its instruments.
	telemetry := services.NewTelemetry()
	conversions.SetTelemetry(telemetry)

	base := []vukem.Option{
		// PostgreSQL is the production database: the web and worker services are
		// separate processes, and only a server database serves both. SQLite is
		// built into the framework and is chosen by DATABASE_URL, which local
		// mode (APP_MODE=local, see local.go) points at a file because one
		// process runs the web server and the worker together there.
		vukem.WithDatabaseDrivers(postgres.Driver()),
		vukem.WithProviders(Providers(telemetry, conversions)...),
		vukem.WithMigrations(migrations.All()),
		vukem.WithSeeders(seeders.All()...),
		vukem.WithListeners(listeners.All()...),
		vukem.WithJobsFunc(jobs.All(conversions)),
		vukem.WithSchedule(routes.Schedule(conversions)),
		vukem.WithViews(resources.Views()),
		vukem.WithVite(resources.Public()),
		vukem.WithInertia(inertia.Config{}),
	}

	local, isLocal, err := localOption(os.LookupEnv)
	if err != nil {
		return nil, err
	}

	for _, fn := range routes.All(telemetry, conversions) {
		base = append(base, vukem.WithRoutes(fn))
	}

	base = append(base, opts...)

	// Last, so it replaces the WithEnv that vukem.Main passes in opts: that
	// one reads the process environment and .env, and a .env written for the
	// Postgres setup would otherwise win.
	if isLocal {
		base = append(base, local)
	}

	app, err := vukem.New(base...)
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
