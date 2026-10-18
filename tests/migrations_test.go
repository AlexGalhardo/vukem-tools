package tests

import (
	"os"
	"testing"

	"github.com/AlexGalhardo/vukem/drivers/postgres/pgtest"
	"github.com/AlexGalhardo/vukem/vukemtest"

	"github.com/AlexGalhardo/vukem-tools/bootstrap"
	"github.com/AlexGalhardo/vukem-tools/database/migrations"
)

// Production runs on PostgreSQL, so the schema is asserted there and not on
// the in-memory SQLite the other tests use. pgtest needs a server: CI provides
// one, and locally the test says what to set instead of failing obscurely.
func postgresURL(t *testing.T) string {
	t.Helper()

	if os.Getenv("TEST_DATABASE_URL") == "" && os.Getenv("POSTGRES_URL") == "" {
		t.Skip("no PostgreSQL: set TEST_DATABASE_URL (CI always does, so this is never skipped there)")
	}

	return pgtest.Database(t, "")
}

// Every table and index this app owns exists after the migrations run, with the
// names the queries rely on. The framework's own tables (sessions, cache, jobs,
// failed_jobs, the webhook delivery log) are deliberately absent: it creates
// those at boot, not through a migration.
func TestMigrationsCreateTheAppSchema(t *testing.T) {
	t.Parallel()

	url := postgresURL(t)
	app := vukemtest.New(t, bootstrap.New, vukemtest.Options{Env: map[string]string{"DATABASE_URL": url}})

	db, err := app.DB()
	if err != nil {
		t.Fatal(err)
	}

	tables := []string{
		"users", "remember_tokens", "access_tokens", "password_resets",
		"conversions", "conversion_events", "files", "usage_counters",
	}

	for _, name := range tables {
		var found bool

		err := db.QueryRowContext(t.Context(),
			`SELECT EXISTS (SELECT 1 FROM information_schema.tables
			  WHERE table_schema = current_schema() AND table_name = $1)`, name).Scan(&found)
		if err != nil {
			t.Fatalf("looking up table %q: %v", name, err)
		}

		if !found {
			t.Errorf("table %q is missing after the migrations ran", name)
		}
	}

	// Each one is justified by a query in a comment on its migration. A
	// missing index here is a sequential scan on a table that only grows.
	indexes := map[string][]string{
		"conversions": {
			"conversions_user_id_created_at_index",
			"conversions_user_id_idempotency_key_unique",
			"conversions_expires_at_index",
		},
		"conversion_events": {"conversion_events_conversion_id_created_at_index"},
		"files": {
			"files_conversion_id_role_index",
			"files_user_id_created_at_index",
			"files_expires_at_index",
		},
		"usage_counters": {"usage_counters_user_id_day_unique"},
	}

	for table, names := range indexes {
		for _, name := range names {
			var found bool

			err := db.QueryRowContext(t.Context(),
				`SELECT EXISTS (SELECT 1 FROM pg_indexes
				  WHERE schemaname = current_schema() AND tablename = $1 AND indexname = $2)`,
				table, name).Scan(&found)
			if err != nil {
				t.Fatalf("looking up index %q: %v", name, err)
			}

			if !found {
				t.Errorf("index %q on %q is missing after the migrations ran", name, table)
			}
		}
	}
}

// Booting twice against the same database runs the migrations twice. The second
// run must find nothing to apply rather than fail on an existing table, which
// is what every deploy does when it restarts a container.
func TestMigrationsAreIdempotent(t *testing.T) {
	t.Parallel()

	url := postgresURL(t)
	env := vukemtest.Options{Env: map[string]string{"DATABASE_URL": url}}

	vukemtest.New(t, bootstrap.New, env)

	app, err := vukemtest.Build(t, bootstrap.New, env)
	if err != nil {
		t.Fatalf("booting a second time against the same database: %v", err)
	}

	db, err := app.DB()
	if err != nil {
		t.Fatal(err)
	}

	// The ledger holds exactly the registered migrations: the second boot
	// applied nothing new, and nothing was applied twice. (`migrations.id` is a
	// primary key, so counting one id would prove nothing on its own.)
	var applied int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM migrations`).Scan(&applied); err != nil {
		t.Fatalf("counting applied migrations: %v", err)
	}

	if want := len(migrations.All()); applied != want {
		t.Errorf("migrations ledger has %d rows after two boots, want %d", applied, want)
	}

	// A second boot must not have opened a new batch.
	var batches int
	if err := db.QueryRowContext(t.Context(), `SELECT count(DISTINCT batch) FROM migrations`).
		Scan(&batches); err != nil {
		t.Fatalf("counting migration batches: %v", err)
	}

	if batches != 1 {
		t.Errorf("migrations span %d batches after two boots, want 1", batches)
	}
}
