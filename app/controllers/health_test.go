package controllers_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AlexGalhardo/vukem/database"
	"github.com/AlexGalhardo/vukem/storage"
	"github.com/AlexGalhardo/vukem/web"

	"github.com/AlexGalhardo/vukem-tools/app/controllers"
	"github.com/AlexGalhardo/vukem-tools/internal/engine"
)

// brokenDisk fails the way a bucket with the wrong credentials fails, and
// carries a secret in its error so the test can prove the secret is not
// forwarded to the response.
type brokenDisk struct {
	storage.Disk
	err error
}

func (d brokenDisk) Put(context.Context, string, io.Reader) error { return d.err }

// memDisk is a bucket that works.
type memDisk struct {
	storage.Disk
	objects map[string][]byte
}

func (d *memDisk) Put(_ context.Context, p string, r io.Reader) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	d.objects[p] = b

	return nil
}

func (d *memDisk) Open(_ context.Context, p string) (io.ReadCloser, error) {
	b, ok := d.objects[p]
	if !ok {
		return nil, storage.ErrNotFound
	}

	return io.NopCloser(strings.NewReader(string(b))), nil
}

func (d *memDisk) Delete(_ context.Context, p string) error {
	delete(d.objects, p)

	return nil
}

// A load balancer's probe must not touch a dependency. A slow database would
// otherwise take every instance out of rotation and turn a degraded service
// into no service at all.
func TestTheShallowCheckTouchesNothing(t *testing.T) {
	t.Parallel()

	// No database and no disk: if the shallow check used either, it would
	// fail here rather than answer.
	h := controllers.NewHealth(controllers.HealthOptions{Version: "test"})

	rec, err := call(h.Shallow, "/health")
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var body struct {
		Status  string `json:"status"`
		Uptime  string `json:"uptime"`
		Version string `json:"version"`
	}

	decode(t, rec, &body)

	if body.Status != "ok" {
		t.Errorf("status = %q, want ok", body.Status)
	}

	if body.Uptime == "" {
		t.Error("the response carries no uptime, so a restart loop is invisible")
	}

	if body.Version != "test" {
		t.Errorf("version = %q, want the build", body.Version)
	}

	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store: a cached health check reports the past", got)
	}
}

// The ROADMAP's assert: the deep check fails when the bucket credentials are
// wrong, and the shallow one still answers. A deploy that cannot store a
// result is broken even though it serves pages, and only the deep check may
// say so.
func TestTheDeepCheckFailsOnABadBucketWhileShallowStillAnswers(t *testing.T) {
	t.Parallel()

	// The error carries a credential, as a real driver's would.
	secret := "AKIAIOSFODNN7EXAMPLE"
	h := controllers.NewHealth(controllers.HealthOptions{
		Disk: brokenDisk{err: errors.New("SignatureDoesNotMatch for key " + secret)},
	})

	rec, err := call(h.Deep, "/health/deep")
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}

	raw := rec.Body.String()
	if strings.Contains(raw, secret) {
		t.Fatalf("the response leaked a credential from the driver's error:\n%s", raw)
	}

	var body struct {
		Status string                    `json:"status"`
		Checks map[string]map[string]any `json:"checks"`
	}

	decode(t, rec, &body)

	if body.Status != "fail" {
		t.Errorf("status = %q, want fail", body.Status)
	}

	if got := body.Checks["storage"]["status"]; got != "fail" {
		t.Errorf("storage check = %v, want fail", got)
	}

	// The shallow check is unaffected.
	shallow, err := call(h.Shallow, "/health")
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	if shallow.Code != http.StatusOK {
		t.Errorf("the shallow check returned %d while a dependency was down; it must stay 200",
			shallow.Code)
	}
}

// A working bucket passes, and the probe object does not accumulate: a check
// that leaves a file behind on every run fills the bucket it is checking.
func TestTheDeepCheckLeavesNoProbeObject(t *testing.T) {
	t.Parallel()

	disk := &memDisk{objects: map[string][]byte{}}
	h := controllers.NewHealth(controllers.HealthOptions{Disk: disk})

	rec, err := call(h.Deep, "/health/deep")
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	var body struct {
		Checks map[string]map[string]any `json:"checks"`
	}

	decode(t, rec, &body)

	if got := body.Checks["storage"]["status"]; got != "ok" {
		t.Fatalf("storage check = %v on a working bucket, want ok (body: %s)", got, rec.Body.String())
	}

	if len(disk.objects) != 0 {
		t.Errorf("the check left %d object(s) behind: %v", len(disk.objects), disk.objects)
	}
}

// A missing optional engine is a feature that is not ready, not a broken
// deploy. Failing the health check for it would take a working service out of
// rotation because one planned tool has no binary yet.
func TestAMissingOptionalEngineDoesNotFailTheDeepCheck(t *testing.T) {
	t.Parallel()

	registry, err := engine.Resolve(context.Background(), nil, []engine.Binary{
		{Name: "no-such-program-exists-anywhere"},
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	disk := &memDisk{objects: map[string][]byte{}}
	h := controllers.NewHealth(controllers.HealthOptions{
		DB:      openDB(t),
		Disk:    disk,
		Engines: registry,
		Token:   deepToken,
	})

	rec, err := callWith(h.Deep, "/health/deep", "Bearer "+deepToken)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: a planned tool's engine must not fail the check (body: %s)",
			rec.Code, rec.Body.String())
	}

	var body struct {
		Checks map[string]map[string]any `json:"checks"`
	}

	decode(t, rec, &body)

	detail, _ := body.Checks["engines"]["detail"].(string)
	if !strings.Contains(detail, "no-such-program-exists-anywhere") {
		t.Errorf("the engines check does not name what is unavailable: %q", detail)
	}
}

// An unconfigured database is a failure, not a pass. A deep check that
// answers "ok" because it had nothing to check is worse than no check.
func TestAnUnconfiguredDependencyFails(t *testing.T) {
	t.Parallel()

	h := controllers.NewHealth(controllers.HealthOptions{Now: func() time.Time { return time.Unix(0, 0) }})

	rec, err := call(h.Deep, "/health/deep")
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

// openDB is a database with the one table the deep check reads. The check
// deliberately reads a table rather than pinging, so a database whose
// migrations never ran fails it.
func openDB(t *testing.T) *database.DB {
	t.Helper()

	db, err := database.Open(t.Context(), "sqlite:"+filepath.Join(t.TempDir(), "health.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })

	if _, err := db.ExecContext(context.Background(),
		"CREATE TABLE conversions (id TEXT PRIMARY KEY)"); err != nil {
		t.Fatalf("create: %v", err)
	}

	return db
}

// A database that is up but has no schema must fail: a pool handing out a
// connection to a database whose migrations never ran would pass a ping, and
// the service cannot do its job.
func TestADatabaseWithoutItsSchemaFails(t *testing.T) {
	t.Parallel()

	db, err := database.Open(t.Context(), "sqlite:"+filepath.Join(t.TempDir(), "empty.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })

	h := controllers.NewHealth(controllers.HealthOptions{
		DB:   db,
		Disk: &memDisk{objects: map[string][]byte{}},
	})

	rec, err := call(h.Deep, "/health/deep")
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 for a database with no schema", rec.Code)
	}
}

const deepToken = "operator-token-for-the-deep-check"

// An anonymous caller learns whether the service is up and which dependency
// is down, and nothing that fingerprints the deployment: no engine versions,
// no build, no detail text, no timings.
func TestTheDeepCheckHidesVersionsFromAnAnonymousCaller(t *testing.T) {
	t.Parallel()

	registry, err := engine.Resolve(t.Context(), nil, []engine.Binary{{Name: "no-such-program-exists-anywhere"}})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	h := controllers.NewHealth(controllers.HealthOptions{
		DB:      openDB(t),
		Disk:    &memDisk{objects: map[string][]byte{}},
		Engines: registry,
		Version: "9.9.9-secret-build",
		Token:   deepToken,
	})

	for name, header := range map[string]string{
		"no credentials": "",
		"wrong token":    "Bearer not-the-token",
		"wrong scheme":   "Basic " + deepToken,
	} {
		rec, err := callWith(h.Deep, "/health/deep", header)
		if err != nil {
			t.Fatalf("%s: handler: %v", name, err)
		}

		raw := rec.Body.String()
		for _, leak := range []string{"9.9.9-secret-build", "no-such-program-exists-anywhere", `"detail"`, `"took"`} {
			if strings.Contains(raw, leak) {
				t.Errorf("%s: the anonymous body leaks %q:\n%s", name, leak, raw)
			}
		}

		var body struct {
			Status string                    `json:"status"`
			Checks map[string]map[string]any `json:"checks"`
		}

		decode(t, rec, &body)

		if body.Status != "ok" || body.Checks["database"]["status"] != "ok" {
			t.Errorf("%s: the anonymous body lost the status: %s", name, raw)
		}
	}
}

func TestTheDeepCheckGivesTheOperatorTheVersions(t *testing.T) {
	t.Parallel()

	h := controllers.NewHealth(controllers.HealthOptions{
		DB:      openDB(t),
		Disk:    &memDisk{objects: map[string][]byte{}},
		Version: "1.2.3",
		Token:   deepToken,
	})

	rec, err := callWith(h.Deep, "/health/deep", "Bearer "+deepToken)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	if !strings.Contains(rec.Body.String(), `"version":"1.2.3"`) || !strings.Contains(rec.Body.String(), `"took"`) {
		t.Errorf("the operator's body is missing the build or the timings: %s", rec.Body.String())
	}
}

// With no token configured nobody is the operator: the endpoint must not
// treat an empty bearer as a match for an empty token.
func TestTheDeepCheckWithNoTokenIsAlwaysAnonymous(t *testing.T) {
	t.Parallel()

	h := controllers.NewHealth(controllers.HealthOptions{
		DB:      openDB(t),
		Disk:    &memDisk{objects: map[string][]byte{}},
		Version: "1.2.3",
	})

	rec, err := callWith(h.Deep, "/health/deep", "Bearer ")
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	if strings.Contains(rec.Body.String(), "1.2.3") {
		t.Errorf("an empty bearer unlocked the operator view: %s", rec.Body.String())
	}
}

// call drives a handler the way the router does: through a web.Ctx over a
// recorder, so the test exercises the same path as production rather than a
// net/http handler the app does not have.
func call(h func(*web.Ctx) error, target string) (*httptest.ResponseRecorder, error) {
	return callWith(h, target, "")
}

// callWith is call with an Authorization header, when one is given.
func callWith(h func(*web.Ctx) error, target, authorization string) (*httptest.ResponseRecorder, error) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, target, http.NoBody)

	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}

	return rec, h(web.NewCtx(rec, req, nil))
}

func decode(t *testing.T, rec *httptest.ResponseRecorder, into any) {
	t.Helper()

	if err := json.Unmarshal(rec.Body.Bytes(), into); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
}
