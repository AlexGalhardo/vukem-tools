package services

import (
	"context"

	vukem "github.com/AlexGalhardo/vukem"
)

// Provider resolves the conversion pipeline during boot.
//
// Neither vukem.WithJobsFunc nor vukem.WithSchedule can report an error, so
// without this a wiring mistake — an unreachable database, a broker that
// refuses the dialect — would surface as a worker failing every message with
// "unknown kind" rather than as a service that does not start. Failing at
// boot is the loud version.
func Provider(c *Conversions) vukem.Provider { return &provider{c: c} }

type provider struct{ c *Conversions }

// Register implements vukem.Provider. Nothing is built here: the database and
// the queue are not available until boot.
func (*provider) Register(*vukem.App) error { return nil }

// Boot implements vukem.Booter.
func (p *provider) Boot(_ context.Context, app *vukem.App) error { return p.c.Resolve(app) }
