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
	"github.com/AlexGalhardo/vukem/config"
	"github.com/AlexGalhardo/vukem/drivers/s3"
	"github.com/AlexGalhardo/vukem/signed"
	"github.com/AlexGalhardo/vukem/sse"
	"github.com/AlexGalhardo/vukem/storage"

	"github.com/AlexGalhardo/vukem-tools/app/tools"
	"github.com/AlexGalhardo/vukem-tools/internal/files"
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
	files  *files.Store
	signer *signed.Signer
	disk   storage.Disk
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

// Files returns the file store. Resolve must have succeeded.
func (c *Conversions) Files() *files.Store { return c.files }

// Signer signs and verifies download links. Resolve must have succeeded.
func (c *Conversions) Signer() *signed.Signer { return c.signer }

// Default settings, used when the matching variable is unset.
const (
	defaultResultTTLMinutes = 5
	defaultWorkRoot         = "storage/work"
	// defaultUploadRoot holds partial resumable uploads. It is scratch
	// space: a finished upload is moved to the bucket and the partial data
	// deleted, so nothing here has to survive a restart.
	defaultUploadRoot = "storage/uploads"
	// defaultMaxUploadMB is the largest file accepted, matching the figure
	// in .env.example and the one the interface advertises.
	defaultMaxUploadMB = 500
	// uploadExpiry is how long an unfinished resumable upload is kept before
	// the cleanup deletes it. Long enough to survive a bad connection and a
	// night's sleep, short enough that abandoned bytes do not accumulate.
	uploadExpiry = 24 * time.Hour
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
	if err != nil {
		return err
	}

	return c.buildFiles(app, ttl)
}

// buildFiles wires the file store: the bucket, the signer for download links,
// and the lifetime a result keeps.
func (c *Conversions) buildFiles(app *vukem.App, ttl time.Duration) error {
	db, err := app.DB()
	if err != nil {
		return fmt.Errorf("services: database: %w", err)
	}

	if c.disk, err = openDisk(app); err != nil {
		return err
	}

	// A separate key for signing links, derived from APP_KEY: a link
	// signature must not be made with the same key that encrypts cookies, so
	// that leaking one does not forge the other. The previous keys keep links
	// made before a key rotation working until they expire.
	key, err := app.DeriveKey("signed.urls", 32)
	if err != nil {
		return fmt.Errorf("services: derive the URL signing key: %w", err)
	}

	c.signer = signed.New(key).
		WithPreviousKeys(app.DerivePreviousKeys("signed.urls", 32)...).
		WithClock(app.Now)

	c.files, err = files.New(files.Options{
		DB:      db,
		Disk:    c.disk,
		IDs:     ids.NewGenerator(),
		Signer:  c.signer,
		BaseURL: app.Config.URL,
		TTL:     ttl,
		Now:     app.Now,
		Log:     app.Log,
	})

	return err
}

// openDisk returns the bucket results live in.
//
// Production must have a bucket: the web service and the worker are separate
// containers with separate filesystems, so a local disk there would mean a
// worker writing a result nobody can download. Outside production a local
// directory keeps `vukem serve` working with no cloud account, and the log
// says which one is in use so nobody is surprised by it.
func openDisk(app *vukem.App) (storage.Disk, error) {
	bucket := app.Source.Get("S3_BUCKET")
	if bucket == "" {
		if app.Config.IsProduction() {
			return nil, errors.New("services: S3_BUCKET is required in production: " +
				"the web and worker services share no filesystem, so results must live in a bucket")
		}

		dir := app.Source.Get("STORAGE_DIR")
		if dir == "" {
			dir = defaultStorageDir
		}

		app.Log.Warn("services: no S3_BUCKET, keeping results on local disk", "dir", dir)

		// No public base URL: every download goes through a signed app
		// route, so the disk must not hand out URLs of its own.
		return storage.NewLocal(dir, "")
	}

	cfg, err := config.Load[s3.Config](app.Source)
	if err != nil {
		return nil, fmt.Errorf("services: S3 configuration: %w", err)
	}

	disk, err := s3.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("services: object storage: %w", err)
	}

	return disk, nil
}

// defaultStorageDir is where results go without a bucket (development only).
const defaultStorageDir = "storage/app"

// UploadRoot is where partial resumable uploads are kept.
func UploadRoot(app *vukem.App) string {
	if dir := app.Source.Get("UPLOAD_DIR"); dir != "" {
		return dir
	}

	return defaultUploadRoot
}

// MaxUploadBytes is the largest file accepted.
func MaxUploadBytes(app *vukem.App) int64 {
	return int64(intFrom(app, "MAX_UPLOAD_SIZE_MB", defaultMaxUploadMB)) << 20
}

// UploadExpiry is how long an unfinished resumable upload is kept.
func UploadExpiry() time.Duration { return uploadExpiry }

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
