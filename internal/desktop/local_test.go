package desktop_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AlexGalhardo/vukem-tools/internal/desktop"
)

func dataDir(t *testing.T) string {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	return dir
}

func status(t *testing.T, url string) int {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}

	client := &http.Client{
		Timeout:       5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}

	_ = resp.Body.Close()

	return resp.StatusCode
}

func stop(t *testing.T, l *desktop.Local) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := l.Stop(ctx); err != nil {
		t.Fatalf("stop: %v", err)
	}
}

func TestStartLocalAnswersUpOnLoopback(t *testing.T) {
	t.Parallel()

	dir := dataDir(t)

	l, err := desktop.StartLocal(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = l.Stop(context.Background()) })

	host, _, err := net.SplitHostPort(l.Origin[len("http://"):])
	if err != nil {
		t.Fatal(err)
	}

	if host != "127.0.0.1" {
		t.Fatalf("origin %q is not on 127.0.0.1", l.Origin)
	}

	if code := status(t, l.Origin+"/up"); code != http.StatusOK {
		t.Fatalf("/up = %d, want 200", code)
	}

	// The sign-in page is what the window shows first, offline.
	if code := status(t, l.Origin+"/login"); code != http.StatusOK {
		t.Fatalf("/login = %d, want 200", code)
	}

	if _, err := os.Stat(filepath.Join(dir, "vukem-tools.db")); err != nil {
		t.Fatalf("the database is not in the data directory: %v", err)
	}
}

func TestStopReleasesThePortAndTheDatabaseFile(t *testing.T) {
	t.Parallel()

	dir := dataDir(t)

	l, err := desktop.StartLocal(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}

	addr := l.Origin[len("http://"):]

	stop(t, l)

	select {
	case <-l.Done():
	default:
		t.Fatal("Done is not closed after Stop")
	}

	if c, err := (&net.Dialer{Timeout: time.Second}).DialContext(t.Context(), "tcp", addr); err == nil {
		_ = c.Close()
		t.Fatalf("%s still accepts connections after Stop", addr)
	}

	// On Windows an open SQLite handle makes this fail.
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("the data directory is still locked after Stop: %v", err)
	}
}

func TestStopIsIdempotent(t *testing.T) {
	t.Parallel()

	l, err := desktop.StartLocal(t.Context(), dataDir(t))
	if err != nil {
		t.Fatal(err)
	}

	stop(t, l)
	stop(t, l)
}

func TestCancellingTheContextStopsTheServer(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())

	l, err := desktop.StartLocal(ctx, dataDir(t))
	if err != nil {
		t.Fatal(err)
	}

	cancel()

	select {
	case <-l.Done():
	case <-time.After(30 * time.Second):
		t.Fatal("the server did not stop after the context was cancelled")
	}
}

func TestASecondStartOnOneDataDirReportsTheFirst(t *testing.T) {
	t.Parallel()

	dir := dataDir(t)

	first, err := desktop.StartLocal(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = first.Stop(context.Background()) })

	second, err := desktop.StartLocal(t.Context(), dir)
	if second != nil {
		_ = second.Stop(context.Background())
		t.Fatal("a second server started on the same data directory")
	}

	var running *desktop.RunningError
	if !errors.As(err, &running) {
		t.Fatalf("err = %v, want *RunningError", err)
	}

	if running.Origin != first.Origin {
		t.Fatalf("RunningError.Origin = %q, want %q", running.Origin, first.Origin)
	}

	// The first keeps serving.
	if code := status(t, first.Origin+"/up"); code != http.StatusOK {
		t.Fatalf("/up = %d after the second start", code)
	}
}

func TestAStaleLockIsTakenOver(t *testing.T) {
	t.Parallel()

	dir := dataDir(t)

	stale := `{"pid":1,"origin":"http://127.0.0.1:1"}`
	if err := os.WriteFile(filepath.Join(dir, "desktop.lock"), []byte(stale), 0o600); err != nil {
		t.Fatal(err)
	}

	l, err := desktop.StartLocal(t.Context(), dir)
	if err != nil {
		t.Fatalf("a lock left by a crashed run blocked the start: %v", err)
	}

	stop(t, l)

	if _, err := os.Stat(filepath.Join(dir, "desktop.lock")); !os.IsNotExist(err) {
		t.Fatalf("the lock survives Stop: %v", err)
	}
}

func TestOpenRemoteModeStartsNoServer(t *testing.T) {
	t.Parallel()

	const url = "http://127.0.0.1:1"

	s, err := desktop.Open(t.Context(), func(k string) string {
		if k == "VUKEM_TOOLS_URL" {
			return url
		}

		return ""
	}, desktop.OpenOptions{Version: "dev", DataDir: dataDir(t)})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	if s.Mode != desktop.ModeRemote {
		t.Fatalf("mode = %v, want remote", s.Mode)
	}

	// Nothing answers there, so the window shows the offline page.
	if s.URL == url+"/tools/pdf-crop" || s.URL == "" {
		t.Fatalf("URL = %q, want the offline page", s.URL)
	}
}

func TestOpenLocalModeServesAndAttachesTheSecondInstance(t *testing.T) {
	t.Parallel()

	dir := dataDir(t)
	noEnv := func(string) string { return "" }
	opts := desktop.OpenOptions{Version: "dev", DataDir: dir}

	first, err := desktop.Open(t.Context(), noEnv, opts)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = first.Stop(context.Background()) })

	if first.Mode != desktop.ModeLocal {
		t.Fatalf("mode = %v, want local", first.Mode)
	}

	second, err := desktop.Open(t.Context(), noEnv, opts)
	if err != nil {
		t.Fatal(err)
	}

	if second.Mode != desktop.ModeAttached || second.URL != first.URL {
		t.Fatalf("second = %v %q, want attached to %q", second.Mode, second.URL, first.URL)
	}

	// Closing the second window must not stop the first's server.
	if err := second.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}

	if code := status(t, first.URL+"up"); code != http.StatusOK {
		t.Fatalf("/up = %d after the attached session stopped", code)
	}
}

// The port is random, so the origin the browser sends never matches APP_URL.
// A same-origin form post must still pass the CSRF origin check.
func TestARandomPortIsStillSameOriginForForms(t *testing.T) {
	t.Parallel()

	l, err := desktop.StartLocal(t.Context(), dataDir(t))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = l.Stop(context.Background()) })

	post := func(origin string) int {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, l.Origin+"/login",
			strings.NewReader("email=nobody@example.com&password=wrong"))
		if err != nil {
			t.Fatal(err)
		}

		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", origin)

		client := &http.Client{
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}

		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}

		_ = resp.Body.Close()

		return resp.StatusCode
	}

	if got := post(l.Origin); got != http.StatusSeeOther {
		t.Fatalf("same-origin POST /login = %d, want 303", got)
	}

	if got := post("http://evil.example"); got != http.StatusForbidden {
		t.Fatalf("cross-origin POST /login = %d, want 403", got)
	}
}
