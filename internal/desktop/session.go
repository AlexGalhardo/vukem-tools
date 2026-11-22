package desktop

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Mode is how a window got the app it shows.
type Mode int

const (
	// ModeRemote shows the server named by VUKEM_TOOLS_URL. Nothing is started.
	ModeRemote Mode = iota
	// ModeLocal shows a server this process started and stops on exit.
	ModeLocal
	// ModeAttached shows the server of another instance on the same data
	// directory. This process owns nothing.
	ModeAttached
)

func (m Mode) String() string {
	switch m {
	case ModeRemote:
		return "remote"
	case ModeLocal:
		return "local"
	case ModeAttached:
		return "attached"
	default:
		return "unknown"
	}
}

// OpenOptions are the inputs of Open that are not environment variables.
type OpenOptions struct {
	// Version is the release printed on the offline page.
	Version string
	// DataDir is the local mode data directory, see bootstrap.DataDir.
	DataDir string
}

// Session is what the window opens and what must be released when it closes.
type Session struct {
	Mode Mode
	// URL is the first page of the window.
	URL string

	stop func(context.Context) error
	done <-chan struct{}
}

// Done is closed when the server this session depends on has stopped on its
// own. It is nil, and never fires, for a session that owns no server.
func (s *Session) Done() <-chan struct{} { return s.done }

// Stop releases everything the session owns. It is idempotent.
func (s *Session) Stop(ctx context.Context) error {
	if s.stop == nil {
		return nil
	}

	return s.stop(ctx)
}

// Open decides what the window shows.
//
// VUKEM_TOOLS_URL set means remote mode: no server is started, and an origin
// that does not answer gets the offline page. Otherwise the app starts in this
// process; when another instance already holds the data directory, the window
// attaches to that one instead. VUKEM_TOOLS_TOOL opens a tool page rather than
// the home page.
func Open(ctx context.Context, getenv func(string) string, opts OpenOptions) (*Session, error) {
	tool := strings.TrimSpace(getenv("VUKEM_TOOLS_TOOL"))

	if base := getenv("VUKEM_TOOLS_URL"); base != "" {
		return openRemote(ctx, base, tool, opts.Version)
	}

	l, err := StartLocal(ctx, opts.DataDir)

	if running, ok := errors.AsType[*RunningError](err); ok {
		url, err := pageURL(running.Origin, tool)
		if err != nil {
			return nil, err
		}

		return &Session{Mode: ModeAttached, URL: url}, nil
	}

	if err != nil {
		return nil, err
	}

	url, err := pageURL(l.Origin, tool)
	if err != nil {
		return nil, errors.Join(err, l.Stop(context.WithoutCancel(ctx)))
	}

	return &Session{Mode: ModeLocal, URL: url, stop: l.Stop, done: l.Done()}, nil
}

func openRemote(ctx context.Context, base, tool, version string) (*Session, error) {
	// A remote origin keeps the old default tool page.
	if tool == "" {
		tool = "pdf-crop"
	}

	page, err := ToolURL(base, tool)
	if err != nil {
		return nil, err
	}

	s := &Session{Mode: ModeRemote, URL: page}

	if !OriginUp(ctx, base) {
		status, err := StartStatus(version, page)
		if err != nil {
			return nil, fmt.Errorf("desktop: offline page: %w", err)
		}

		s.URL = status.URL
		s.stop = status.Close
	}

	return s, nil
}

// pageURL is the home page of origin, or the page of tool when one is named.
func pageURL(origin, tool string) (string, error) {
	if tool == "" {
		return origin + "/", nil
	}

	return ToolURL(origin, tool)
}
