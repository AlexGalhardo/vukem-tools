// Package services holds the application's own wiring: the pieces that
// several entry points share and that must exist exactly once per process.
//
// The framework has no service container, and the project has no globals, so
// a shared service is a value created in bootstrap and handed to whoever
// needs it. It is resolved lazily because what it needs — the database, the
// queue — does not exist until the app has been built.
package services

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	vukem "github.com/AlexGalhardo/vukem"
	"github.com/AlexGalhardo/vukem/sse"

	"github.com/AlexGalhardo/vukem-tools/app/tools"
	"github.com/AlexGalhardo/vukem-tools/internal/ids"
	"github.com/AlexGalhardo/vukem-tools/internal/jobs"
	"github.com/AlexGalhardo/vukem-tools/internal/pgbroker"
)

// Conversions is the conversion pipeline and everything it needs, built once
// per process.
//
// One hub per process matters: each hub holds a listening database connection
// per watched conversion, so two hubs in one process would double that for no
// benefit. The web routes and the queue worker therefore share this value
// rather than each building their own.
type Conversions struct {
	once sync.Once
	err  error

	hub    *sse.Hub
	store  *jobs.Store
	pipe   *jobs.Pipeline
	reaper *jobs.Reaper
}

// NewConversions returns an unresolved Conversions. Nothing is built until
// Resolve is called.
func NewConversions() *Conversions { return &Conversions{} }

// Resolve builds the pipeline on first call and returns the same error or the
// same value on every later one.
func (c *Conversions) Resolve(app *vukem.App) error {
	c.once.Do(func() { c.err = c.build(app) })
	return c.err
}

// Hub returns the event hub. Resolve must have succeeded.
func (c *Conversions) Hub() *sse.Hub { return c.hub }

// Store returns the conversion store. Resolve must have succeeded.
func (c *Conversions) Store() *jobs.Store { return c.store }

// Pipeline returns the pipeline. Resolve must have succeeded.
func (c *Conversions) Pipeline() *jobs.Pipeline { return c.pipe }

// Reaper returns the abandoned-conversion reaper. Resolve must have
// succeeded.
func (c *Conversions) Reaper() *jobs.Reaper { return c.reaper }

// Default settings, used when the matching variable is unset.
const (
	defaultResultTTLMinutes = 60
	defaultWorkRoot         = "storage/work"
	// reapAfter is how long a conversion may go without progress before it is
	// treated as abandoned. It must stay comfortably above the longest tool
	// timeout plus the queue's reclaim grace, or a slow conversion would be
	// failed while it is still running.
	reapAfter = 30 * time.Minute
	// reapEvery is how often the sweep runs. A user watching a dead
	// conversion waits at most reapAfter plus this.
	reapEvery = 5 * time.Minute
)

// build wires the pipeline from the app's configuration.
func (c *Conversions) build(app *vukem.App) error {
	db, err := app.DB()
	if err != nil {
		return fmt.Errorf("services: database: %w", err)
	}

	// Without a broker the hub reaches only the process it lives in, and the
	// worker's progress would never reach a browser. In production that is a
	// boot error rather than a silently broken feature; in development, where
	// serve runs the worker in process, a single-process hub is correct.
	var broker sse.Broker

	if app.Config.Env == "production" || db.Dialect().Name() == "postgres" {
		broker, err = pgbroker.New(db, app.Log)
		if err != nil {
			return fmt.Errorf("services: progress broker: %w", err)
		}
	}

	c.hub = sse.NewHub(broker)
	app.OnShutdown(func(context.Context) error {
		c.hub.Close()

		return nil
	})

	ttl := time.Duration(intFrom(app, "RESULT_TTL_MINUTES", defaultResultTTLMinutes)) * time.Minute

	c.store, err = jobs.NewStore(jobs.StoreOptions{
		DB:        db,
		IDs:       ids.NewGenerator(),
		ResultTTL: ttl,
		Publisher: jobs.NewHubPublisher(c.hub),
		Now:       app.Now,
		Log:       app.Log,
	})
	if err != nil {
		return err
	}

	root := app.Source.Get("WORK_DIR")
	if root == "" {
		root = defaultWorkRoot
	}

	c.pipe, err = jobs.NewPipeline(jobs.PipelineOptions{
		Store: c.store,
		// Resolved on demand: app.Queue() asks for the job registrations,
		// which ask for this pipeline, so resolving it here would be a
		// circular build.
		Queue:    app.Queue,
		Tools:    tools.All(),
		WorkRoot: root,
		Log:      app.Log,
		Now:      app.Now,
	})
	if err != nil {
		return err
	}

	c.reaper, err = jobs.NewReaper(jobs.ReaperOptions{
		Store: c.store,
		After: reapAfter,
		Log:   app.Log,
		Now:   app.Now,
	})

	return err
}

// ReapEvery is how often the scheduled sweep should run.
func ReapEvery() time.Duration { return reapEvery }

// intFrom reads a positive integer setting, falling back to def for an unset
// or unusable value.
//
// A nonsense value falls back rather than failing the boot, because every
// setting here has a safe default and a service that will not start is worse
// than one running on the default. A misconfiguration that weakens security
// would be a boot error; a result lifetime would not.
func intFrom(app *vukem.App, key string, def int) int {
	raw := app.Source.Get(key)
	if raw == "" {
		return def
	}

	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		app.Log.Warn("services: ignoring an unusable setting", "key", key, "value", raw, "using", def)

		return def
	}

	return n
}

// ErrUnresolved is returned by a caller that asks for a piece of the pipeline
// before Resolve has succeeded.
var ErrUnresolved = errors.New("services: the conversion pipeline is not resolved")
