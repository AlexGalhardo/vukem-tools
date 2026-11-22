package desktop_test

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AlexGalhardo/vukem-tools/internal/desktop"
)

// The port is chosen at start, so every absolute URL the app prints (the API
// reference's server, llms.txt, the sitemap) has to name it. A fixed APP_URL
// sent API clients to http://localhost:3000, which can be another program.
func TestAbsoluteLinksNameTheRealPort(t *testing.T) {
	// An APP_URL left in the user's environment must not win either.
	t.Setenv("APP_URL", "https://tools.example.test")

	l, err := desktop.StartLocal(t.Context(), dataDir(t))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { stop(t, l) })

	for _, path := range []string{"/openapi.json", "/llms.txt", "/sitemap.xml"} {
		code, raw, _ := do(t, http.MethodGet, l.Origin+path, "", http.NoBody, nil)
		if code != http.StatusOK {
			t.Fatalf("%s = %d", path, code)
		}

		body := string(raw)

		if !strings.Contains(body, l.Origin) {
			t.Errorf("%s does not name the server's own origin %s", path, l.Origin)
		}

		for _, wrong := range []string{"localhost:3000", "tools.example.test"} {
			if strings.Contains(body, wrong) {
				t.Errorf("%s names %s instead of %s", path, wrong, l.Origin)
			}
		}
	}
}

// What makes the shell local (the database, the in-process worker, the disk
// for results, the password sign-in) is not up to the environment the window
// was launched from: a DATABASE_URL left in a user's shell must not point the
// desktop at a Postgres, and QUEUE_IN_SERVE=false must not leave every
// conversion waiting for a worker that never starts.
func TestTheLaunchEnvironmentCannotUndoLocalMode(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://nobody:nothing@127.0.0.1:1/none?connect_timeout=2")
	t.Setenv("QUEUE_IN_SERVE", "false")
	t.Setenv("QUEUE_CONNECTION", "sync")
	t.Setenv("S3_BUCKET", "a-bucket-that-does-not-exist")
	t.Setenv("PASSWORD_AUTH", "off")

	dir := dataDir(t)

	l, err := desktop.StartLocal(t.Context(), dir)
	if err != nil {
		t.Fatalf("an inherited setting broke the start: %v", err)
	}

	t.Cleanup(func() { stop(t, l) })

	key := apiKey(t, l.Origin)
	id := submitArchive(t, l.Origin, key, 64)
	waitStatus(t, l.Origin, key, id, "finished")
}

// A run that was killed mid-conversion leaves the engine's scratch directory,
// with a copy of the person's file in it, and nothing else ever deletes it.
// The instance lock makes the work directory this process's alone, so a start
// clears it.
func TestAStartClearsTheScratchADeadRunLeft(t *testing.T) {
	t.Parallel()

	dir := dataDir(t)
	orphan := filepath.Join(dir, "work", "01JABANDONEDCONVERSION")

	if err := os.MkdirAll(orphan, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(orphan, "member-000"), []byte("half a conversion"), 0o600); err != nil {
		t.Fatal(err)
	}

	l, err := desktop.StartLocal(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { stop(t, l) })

	if _, err := os.Stat(orphan); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the dead run's scratch directory survived the start: %v", err)
	}
}

// An empty data directory used to be joined onto the working directory: the
// key, the lock and the database would have been written wherever the app was
// launched from.
func TestStartLocalRefusesAnEmptyDataDirectory(t *testing.T) {
	t.Parallel()

	l, err := desktop.StartLocal(t.Context(), "")
	if err == nil {
		stop(t, l)
		t.Fatal("started with no data directory")
	}

	for _, name := range []string{"app.key", "desktop.lock", "vukem-tools.db"} {
		if _, statErr := os.Stat(name); statErr == nil {
			_ = os.Remove(name)

			t.Errorf("%s was written to the working directory", name)
		}
	}
}
