package desktop

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"time"

	vukem "github.com/AlexGalhardo/vukem"

	"github.com/AlexGalhardo/vukem-tools/bootstrap"
)

// readyTimeout bounds boot, migrations and the first /up answer.
const readyTimeout = 60 * time.Second

// Local is the app running inside this process, on a loopback port.
type Local struct {
	// Origin is the base URL the window opens, for example http://127.0.0.1:51234.
	Origin string

	cancel context.CancelFunc
	done   chan struct{}
	err    error
}

// Done is closed when the server has stopped and released the database and the
// lock, whether Stop asked for it or the server failed.
func (l *Local) Done() <-chan struct{} { return l.done }

// Stop cancels the server and waits until it has drained the worker, closed the
// database and released the data directory, or ctx ends. It is idempotent.
func (l *Local) Stop(ctx context.Context) error {
	l.cancel()

	select {
	case <-l.done:
		return l.err
	case <-ctx.Done():
		return fmt.Errorf("desktop: waiting for the server to stop: %w", ctx.Err())
	}
}

// StartLocal runs the app in local mode in this process, on 127.0.0.1 with a
// port the kernel picks, and returns once /up answers.
//
// The server lives until Stop or until ctx is cancelled. A second start on the
// same dataDir returns a *RunningError and starts nothing.
func StartLocal(ctx context.Context, dataDir string) (*Local, error) {
	if strings.TrimSpace(dataDir) == "" {
		return nil, errors.New("desktop: no data directory given")
	}

	lock, err := acquireLock(ctx, dataDir)
	if err != nil {
		return nil, err
	}

	l, err := start(ctx, dataDir, lock)
	if err != nil {
		lock.release()

		return nil, err
	}

	return l, nil
}

// listenAttempts is how many ports start tries. The port is reserved and released
// before the app binds it (the app has to know it to print absolute URLs), so
// another process can take it in between; a fresh port fixes that.
const listenAttempts = 3

var errNotListening = errors.New("the server stopped before it listened")

func start(ctx context.Context, dataDir string, lock *instanceLock) (*Local, error) {
	// The lock is ours, so nothing else is converting into this directory: what is
	// in it belongs to a run that died.
	// A file an antivirus still holds open must not stop the app from starting.
	if err := os.RemoveAll(bootstrap.LocalEnv(dataDir)["WORK_DIR"]); err != nil {
		slog.WarnContext(ctx, "desktop: could not clear the scratch space of an earlier run", "err", err)
	}

	var err error

	for range listenAttempts {
		var l *Local

		if l, err = startOnce(ctx, dataDir, lock); err == nil || !errors.Is(err, errNotListening) {
			return l, err
		}
	}

	return nil, err
}

// freePort asks the kernel for an unused loopback port.
func freePort(ctx context.Context) (int, error) {
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("desktop: choose a port: %w", err)
	}

	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		_ = ln.Close()

		return 0, fmt.Errorf("desktop: choose a port: unexpected address %s", ln.Addr())
	}

	return addr.Port, ln.Close()
}

func startOnce(ctx context.Context, dataDir string, lock *instanceLock) (*Local, error) {
	port, err := freePort(ctx)
	if err != nil {
		return nil, err
	}

	opts, err := bootstrap.LocalServeOptions(dataDir, port)
	if err != nil {
		return nil, err
	}

	ready := make(chan string, 1)

	app, err := bootstrap.New(append(opts, vukem.WithReady(ready))...)
	if err != nil {
		return nil, fmt.Errorf("desktop: build the app: %w", err)
	}

	runCtx, cancel := context.WithCancel(ctx)

	if err := migrate(runCtx, app); err != nil {
		cancel()

		return nil, errors.Join(err, app.Shutdown(context.WithoutCancel(ctx)))
	}

	l := &Local{cancel: cancel, done: make(chan struct{})}

	go func() {
		defer close(l.done)

		err := app.Run(runCtx)
		// Run has shut the providers down, so the database is closed here.
		lock.release()

		l.err = err
	}()

	select {
	case addr := <-ready:
		l.Origin = "http://" + addr
	case <-l.done:
		return nil, errors.Join(fmt.Errorf("desktop: %w", errNotListening), l.err)
	case <-time.After(readyTimeout):
		return nil, abort(l, "the server did not listen in time")
	}

	if err := waitUp(runCtx, l.Origin); err != nil {
		return nil, abort(l, err.Error())
	}

	if err := lock.publish(l.Origin); err != nil {
		return nil, abort(l, err.Error())
	}

	return l, nil
}

// abort stops a server that never became ready and reports why.
func abort(l *Local, why string) error {
	stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	return errors.Join(fmt.Errorf("desktop: %s", why), l.Stop(stopCtx))
}

// migrate brings the schema up to date before the first request. serve does
// not, and a fresh data directory has no tables.
func migrate(ctx context.Context, app *vukem.App) error {
	if err := app.Boot(ctx); err != nil {
		return fmt.Errorf("desktop: boot: %w", err)
	}

	m, err := app.Migrator()
	if err != nil {
		return fmt.Errorf("desktop: open the database: %w", err)
	}

	if _, err := m.Up(ctx); err != nil {
		return fmt.Errorf("desktop: migrate: %w", err)
	}

	return nil
}

// waitUp polls /up until it answers 200.
func waitUp(ctx context.Context, origin string) error {
	ctx, cancel := context.WithTimeout(ctx, readyTimeout)
	defer cancel()

	for !probeUp(ctx, origin) {
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s/up did not answer: %w", origin, ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}

	return nil
}
