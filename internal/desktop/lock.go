package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const lockFile = "desktop.lock"

// startingGrace is how long a second start waits for a first one that holds the
// lock but has not published its address yet.
const startingGrace = 20 * time.Second

// RunningError means another instance already serves this data directory.
type RunningError struct {
	// Origin is where the running instance answers.
	Origin string
}

func (e *RunningError) Error() string {
	return "desktop: another instance already serves this data directory at " + e.Origin
}

type lockContent struct {
	PID    int    `json:"pid"`
	Origin string `json:"origin"`
}

// instanceLock marks a data directory as served by this process.
//
// It is a file created exclusively, not an OS file lock: that needs no cgo and
// no dependency on any platform. A crashed run leaves the file behind, so a
// holder counts only while its recorded origin answers /up.
type instanceLock struct {
	path string
	pid  int
}

func readLock(path string) (lockContent, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return lockContent{}, err
	}

	var c lockContent
	// A half-written file reads as "starting": the writer is mid-update.
	_ = json.Unmarshal(raw, &c)

	return c, nil
}

// acquireLock takes the lock for dir, or returns a *RunningError.
func acquireLock(ctx context.Context, dir string) (*instanceLock, error) {
	l := &instanceLock{path: filepath.Join(dir, lockFile), pid: os.Getpid()}
	deadline := time.Now().Add(startingGrace)

	for {
		f, err := os.OpenFile(l.path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			_, werr := f.Write(l.encode(""))

			if cerr := f.Close(); werr != nil || cerr != nil {
				_ = os.Remove(l.path)

				return nil, fmt.Errorf("desktop: write the instance lock: %w", errors.Join(werr, cerr))
			}

			return l, nil
		}

		if !errors.Is(err, fs.ErrExist) {
			return nil, fmt.Errorf("desktop: create the instance lock: %w", err)
		}

		held, err := readLock(l.path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}

		if err != nil {
			return nil, fmt.Errorf("desktop: read the instance lock: %w", err)
		}

		switch {
		case held.Origin != "" && probeUp(ctx, held.Origin):
			return nil, &RunningError{Origin: held.Origin}
		case held.Origin == "" && time.Now().Before(deadline):
			// The holder is still booting.
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(100 * time.Millisecond):
			}
		default:
			// Stale: the holder crashed. Remove only the file just read, so a
			// start that already replaced it is not undone.
			if again, err := readLock(l.path); err == nil && again == held {
				_ = os.Remove(l.path)
			}
		}
	}
}

// publish records the address the holder answers on.
func (l *instanceLock) publish(origin string) error {
	return os.WriteFile(l.path, l.encode(origin), 0o600)
}

func (l *instanceLock) encode(origin string) []byte {
	// Two scalar fields: Marshal cannot fail on them.
	raw, err := json.Marshal(lockContent{PID: l.pid, Origin: origin})
	if err != nil {
		panic(err)
	}

	return raw
}

func (l *instanceLock) release() {
	if held, err := readLock(l.path); err == nil && held.PID == l.pid {
		_ = os.Remove(l.path)
	}
}

// probeUp reports whether origin answers /up with 200 right now.
func probeUp(ctx context.Context, origin string) bool {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, origin+"/up", http.NoBody)
	if err != nil {
		return false
	}

	resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		return false
	}

	_ = resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}
