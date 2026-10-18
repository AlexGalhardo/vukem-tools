package controllers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/AlexGalhardo/vukem/database"
	"github.com/AlexGalhardo/vukem/storage"
	"github.com/AlexGalhardo/vukem/web"

	"github.com/AlexGalhardo/vukem-tools/internal/engine"
)

// Health answers the two questions an operator and a load balancer ask, which
// are not the same question.
//
// The shallow check says "this process is up and serving". A load balancer
// uses it, so it must not touch the database, the bucket or the queue: a
// dependency being slow would otherwise take every instance out of rotation
// and turn a degraded service into no service at all.
//
// The deep check says "this process can actually do its job", and names what
// is wrong. It is for an operator, a deploy gate and an alert — never for a
// load balancer.
type Health struct {
	db      *database.DB
	disk    storage.Disk
	engines *engine.Registry
	// started is when the process came up, which is what makes a restart loop
	// visible in the response rather than only in a log.
	started time.Time
	now     func() time.Time
	version string
}

// HealthOptions configures the controller.
type HealthOptions struct {
	DB      *database.DB
	Disk    storage.Disk
	Engines *engine.Registry
	Now     func() time.Time
	// Version identifies the build, so a deep check tells an operator which
	// code answered.
	Version string
}

// NewHealth returns the controller.
func NewHealth(o HealthOptions) *Health {
	now := o.Now
	if now == nil {
		now = time.Now
	}

	return &Health{
		db:      o.DB,
		disk:    o.Disk,
		engines: o.Engines,
		started: now(),
		now:     now,
		version: o.Version,
	}
}

// checkTimeout bounds every dependency check. A deep check that hangs is a
// deep check nobody can use, and an alert needs an answer more than it needs
// a complete one.
const checkTimeout = 3 * time.Second

// Shallow reports that the process is serving. It touches nothing.
func (h *Health) Shallow(c *web.Ctx) error {
	noStore(c)

	return c.JSON(http.StatusOK, map[string]any{
		"status":  "ok",
		"uptime":  h.now().Sub(h.started).Round(time.Second).String(),
		"version": h.version,
	})
}

// Check is one dependency's result.
type Check struct {
	Status string `json:"status"`
	// Detail says what is wrong. It is an operator-facing string, and it is
	// written by this package rather than taken from an error that could
	// carry a connection string.
	Detail string `json:"detail,omitempty"`
	Took   string `json:"took"`
}

// Deep checks every dependency and reports 503 when any required one fails.
//
// The engines are reported but do not fail the check: an engine whose tools
// are all still planned is a feature that is not ready, not a broken deploy,
// and a required engine already failed the boot.
func (h *Health) Deep(c *web.Ctx) error {
	ctx, cancel := context.WithTimeout(c.Context(), checkTimeout)
	defer cancel()

	type named struct {
		name  string
		check func(context.Context) error
	}

	probes := []named{
		{"database", h.checkDatabase},
		{"storage", h.checkStorage},
	}

	var (
		mu      sync.Mutex
		results = make(map[string]Check, len(probes)+1)
		failed  bool
	)

	var wg sync.WaitGroup

	for _, p := range probes {
		wg.Go(func() {
			started := h.now()
			err := p.check(ctx)
			took := h.now().Sub(started)

			mu.Lock()
			defer mu.Unlock()

			if err != nil {
				failed = true
				results[p.name] = Check{Status: "fail", Detail: err.Error(), Took: took.String()}

				return
			}

			results[p.name] = Check{Status: "ok", Took: took.String()}
		})
	}

	wg.Wait()

	results["engines"] = h.engineCheck()

	status := http.StatusOK
	overall := "ok"

	if failed {
		status = http.StatusServiceUnavailable
		overall = "fail"
	}

	noStore(c)

	return c.JSON(status, map[string]any{
		"status":  overall,
		"version": h.version,
		"checks":  results,
	})
}

// checkDatabase runs the cheapest statement that proves a working connection
// and a usable schema. A ping only proves the socket is open, and a pool
// handing out a connection to a database whose migrations never ran would pass
// it.
func (h *Health) checkDatabase(ctx context.Context) error {
	if h.db == nil {
		return errors.New("not configured")
	}

	var count int

	if err := h.db.QueryRowContext(ctx, "SELECT count(*) FROM conversions WHERE 1 = 0").Scan(&count); err != nil {
		// The driver's error can carry the connection string, so it is
		// classified rather than forwarded.
		return errors.New("the conversions table cannot be read")
	}

	return nil
}

// checkStorage writes, reads back and deletes one small object.
//
// Not Exists on a known key: with the wrong credentials several S3
// implementations answer "false" for a missing object rather than failing, so
// an existence check passes while nothing can be stored. A round trip is the
// only check that proves the bucket actually works.
func (h *Health) checkStorage(ctx context.Context) error {
	if h.disk == nil {
		return errors.New("not configured")
	}

	key := fmt.Sprintf("health/%d", h.now().UnixNano())
	body := []byte("ok")

	if err := h.disk.Put(ctx, key, bytes.NewReader(body)); err != nil {
		return errors.New("the bucket cannot be written")
	}

	// Deleted whatever the read does, so a failing read does not leave a
	// probe object behind on every check.
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), checkTimeout)
		defer cancel()

		_ = h.disk.Delete(cleanup, key)
	}()

	f, err := h.disk.Open(ctx, key)
	if err != nil {
		return errors.New("the bucket cannot be read")
	}

	defer func() { _ = f.Close() }()

	got := make([]byte, len(body))
	if _, err := f.Read(got); err != nil {
		return errors.New("the bucket returned nothing")
	}

	if !bytes.Equal(got, body) {
		return errors.New("the bucket returned the wrong bytes")
	}

	return nil
}

// engineCheck reports which engines are present. It never fails the check;
// see Deep.
func (h *Health) engineCheck() Check {
	if h.engines == nil {
		return Check{Status: "ok", Detail: "not configured", Took: "0s"}
	}

	missing := h.engines.Missing()
	if len(missing) == 0 {
		return Check{Status: "ok", Took: "0s"}
	}

	return Check{
		Status: "ok",
		Detail: fmt.Sprintf("%d engine(s) unavailable, so their tools are off: %v", len(missing), missing),
		Took:   "0s",
	}
}

// noStore keeps a health response out of every cache.
//
// A cached health check is a health check that reports the past, and several
// proxies cache a 200 on a bare GET by default.
func noStore(c *web.Ctx) {
	h := c.Writer().Header()
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
}
