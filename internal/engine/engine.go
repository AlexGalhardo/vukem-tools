// Package engine resolves the external programs a conversion runs, and runs
// them under limits.
//
// Every engine is a subprocess, never cgo: a decoder written in C that reads
// a file somebody uploaded is the most dangerous code in this service, and a
// subprocess is the only boundary that survives it crashing. The cost is that
// the limits have to be real — a timeout, a memory cap, a file-size cap and a
// process-group kill — because a hung or runaway engine in-process would take
// the whole worker with it.
//
// What is resolved and what is enforced are deliberately separate. Resolution
// happens once at boot, logs every engine's exact version, and refuses to
// start when a required one is missing: an engine that is absent should fail
// the deploy, not the first conversion that needs it. Enforcement happens per
// run.
package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

// Binary describes one external program.
type Binary struct {
	// Name is how the app refers to it, and the key in a Registry.
	Name string
	// EnvVar names the setting that gives an absolute path. Empty means the
	// program is only ever found on PATH.
	EnvVar string
	// VersionArgs print the version. Almost every engine uses a different
	// spelling, which is why this is per binary rather than a constant.
	VersionArgs []string
	// Required makes a missing program a boot error rather than a tool that
	// is quietly unavailable.
	Required bool
}

// Lookup reads a configuration value. config.Source satisfies it, and a test
// can pass a map.
type Lookup interface {
	Get(key string) string
}

// Resolved is a program that was found, with the version it reported.
type Resolved struct {
	Binary
	// Path is the absolute path that will be executed.
	Path string
	// Version is the first line the program printed, trimmed. It is logged
	// and shown on the health probe, so a deploy can be checked against what
	// the Dockerfile pinned.
	Version string
}

// ErrMissing is returned when a required program cannot be found.
var ErrMissing = errors.New("engine: required program not found")

// Registry holds the engines that were resolved.
type Registry struct {
	found   map[string]Resolved
	missing []string
}

// versionTimeout bounds asking a program for its version. A program that
// cannot answer in this long is broken, and waiting for it would hold the
// boot.
const versionTimeout = 5 * time.Second

// Resolve finds every binary and reads its version.
//
// A missing optional program is recorded rather than returned as an error:
// the tool that needs it is then unavailable, which the tools page can say,
// and the rest of the service still runs. A missing required one fails.
func Resolve(ctx context.Context, source Lookup, binaries []Binary) (*Registry, error) {
	r := &Registry{found: make(map[string]Resolved, len(binaries))}

	var required []string

	for _, b := range binaries {
		path, err := locate(source, b)
		if err != nil {
			if b.Required {
				required = append(required, b.Name)
			}

			r.missing = append(r.missing, b.Name)

			continue
		}

		r.found[b.Name] = Resolved{Binary: b, Path: path, Version: probe(ctx, path, b.VersionArgs)}
	}

	if len(required) > 0 {
		return nil, fmt.Errorf("%w: %s", ErrMissing, strings.Join(required, ", "))
	}

	return r, nil
}

// locate returns the absolute path of b, preferring the configured one.
//
// A configured path is checked rather than trusted: a typo in a deploy
// variable should fail at boot with the path in the message, not at the first
// conversion with "no such file or directory".
func locate(source Lookup, b Binary) (string, error) {
	if b.EnvVar != "" && source != nil {
		if configured := strings.TrimSpace(source.Get(b.EnvVar)); configured != "" {
			path, err := exec.LookPath(configured)
			if err != nil {
				return "", fmt.Errorf("engine: %s=%q: %w", b.EnvVar, configured, err)
			}

			return path, nil
		}
	}

	return exec.LookPath(b.Name)
}

// probe asks a program for its version, returning "unknown" rather than
// failing: the program exists and can be run, which is what matters, and a
// version string is for the log and the probe.
func probe(ctx context.Context, path string, args []string) string {
	if len(args) == 0 {
		return "unknown"
	}

	ctx, cancel := context.WithTimeout(ctx, versionTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, path, args...).Output()
	if err != nil && len(out) == 0 {
		return "unknown"
	}

	line, _, _ := bytes.Cut(out, []byte{'\n'})

	if v := strings.TrimSpace(string(line)); v != "" {
		return v
	}

	return "unknown"
}

// Path returns the absolute path of an engine, and whether it was resolved.
func (r *Registry) Path(name string) (string, bool) {
	found, ok := r.found[name]
	return found.Path, ok
}

// Version returns what the engine reported, or "" when it was not resolved.
func (r *Registry) Version(name string) string { return r.found[name].Version }

// Has reports whether the engine is available, which is what a tool asks
// before offering itself.
func (r *Registry) Has(name string) bool {
	_, ok := r.found[name]
	return ok
}

// Missing returns the engines that were not found, in the order they were
// declared.
func (r *Registry) Missing() []string { return append([]string(nil), r.missing...) }

// Versions returns each resolved engine's version, for the health probe.
func (r *Registry) Versions() map[string]string {
	out := make(map[string]string, len(r.found))
	for name, found := range r.found {
		out[name] = found.Version
	}

	return out
}

// Log records what was resolved and what was not.
//
// It logs at boot on purpose: the one question an operator asks after a deploy
// is "which versions am I actually running", and the answer should be in the
// log rather than require a shell in the container.
func (r *Registry) Log(ctx context.Context, log *slog.Logger) {
	if log == nil {
		log = slog.Default()
	}

	for name, found := range r.found {
		log.InfoContext(ctx, "engine resolved", "engine", name, "version", found.Version, "path", found.Path)
	}

	if len(r.missing) > 0 {
		// Warning, not info: every missing engine is a tool somebody cannot
		// use, and that should be visible without being fatal.
		log.WarnContext(ctx, "engines unavailable, their tools are hidden", "engines", r.missing)
	}
}
