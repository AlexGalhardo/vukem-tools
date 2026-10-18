// Package ffmpeg is the plumbing every video and audio conversion shares:
// the engine's capabilities, the media probe, the progress stream and the
// error taxonomy.
//
// It exists as its own package because internal/video and internal/audio
// need exactly the same four things and would otherwise each carry a copy of
// the ffprobe JSON shape. A second copy of that is a second place for the
// duration to be read a different way, and the duration is what every
// progress percentage is computed from.
//
// Nothing here knows about jobs, storage or HTTP. Every engine call goes
// through internal/engine, so each one carries a timeout, an address-space
// cap, a file-size cap and a process-group kill — the last of which matters
// more for ffmpeg than for anything else in this service, because ffmpeg
// spawns children and ignores a plain terminate in several configurations.
package ffmpeg

import (
	"context"
	"strings"

	"github.com/AlexGalhardo/vukem-tools/internal/cpu"
	"github.com/AlexGalhardo/vukem-tools/internal/engine"
)

// Runner runs one engine subprocess under limits. engine.Runner satisfies it.
//
// An interface rather than the concrete runner, so the argument building and
// the progress parsing can be tested against a recorded command line on a
// machine with no ffmpeg.
type Runner interface {
	Run(ctx context.Context, cmd engine.Command) (engine.Result, error)
}

// Engine is where the two programs are. Both paths come from the registry,
// which resolved them at boot and logged their versions.
type Engine struct {
	FFmpeg  string
	FFprobe string
}

// Base is the arguments every ffmpeg invocation starts with.
//
// Each one is here for a reason that has bitten somebody:
//
//   - -nostdin, because ffmpeg reads the terminal by default and a worker
//     has no terminal; without it ffmpeg can block on a read that never
//     returns.
//   - -hide_banner and -loglevel error, so the stderr tail the runner keeps
//     holds the reason for a failure rather than fifty lines of build
//     configuration.
//   - -nostats, because the stats go to stderr and progress is read from a
//     structured stream instead; scraping stderr for progress is the thing
//     the engine rules forbid.
//   - -y, because the output path is inside the job's own directory and an
//     interactive overwrite prompt would hang the worker.
//   - -protocol_whitelist file, because ffmpeg can open http, rtmp and
//     concat inputs. Every input here is a local file, and a conversion
//     subprocess gets no network: a crafted playlist that made ffmpeg fetch
//     a URL would be a server-side request forgery with the engine as the
//     client.
func Base() []string {
	return []string{
		"-nostdin",
		"-hide_banner",
		"-loglevel", "error",
		"-nostats",
		"-y",
		"-protocol_whitelist", "file",
		// The filter graph's thread pools, which are global options. Both
		// are sized from the container's quota rather than the host's core
		// count.
		"-filter_threads", itoa(cpu.Limit()),
		"-filter_complex_threads", itoa(cpu.Limit()),
		// Decoder threads. This is an *input* option, so it bounds the
		// decode and nothing else — see Threads for the other half, which is
		// the half that was a bug.
		"-threads", itoa(cpu.Limit()),
	}
}

// Threads is the encoder's thread count, and it must be appended after the
// last -i, not before it.
//
// This is a trap worth spelling out, because it cost a day. ffmpeg's -threads
// is positional: before an input it configures that input's *decoder*, and
// after the inputs it configures the *encoder*. An argument list with
// "-threads 2" only in the global position therefore bounds the decode and
// leaves the encoder on "auto", which is the host's core count.
//
// That is not merely wasteful here, it fails. The engine runs under
// RLIMIT_NPROC, Linux counts threads against it, and x264 on a 16-core host
// asks for about 24 encoder threads plus lookahead. The result is an ffmpeg
// that exits having written nothing, with "at least one of its streams
// received no packets" — a message that says nothing about threads at all.
//
// So every builder in internal/video and internal/audio appends this to its
// output options, and TestEveryBuilderBoundsTheEncodersThreads asserts that
// they all still do.
func Threads() []string { return []string{"-threads", itoa(cpu.Limit())} }

// Env is the environment ffmpeg runs in. It is deliberately almost empty:
// the runner already starts from a minimal environment, and ffmpeg needs
// nothing from the server's.
func Env() []string { return nil }

// itoa is strconv.Itoa without the import, kept local so Base reads as a
// list of flags.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}

	var digits []byte

	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}

	return string(digits)
}

// Capabilities is what this ffmpeg build can do.
//
// It is probed rather than assumed, and the GPL flag is the reason. x264 and
// x265 are GPL-only, so an ffmpeg built without --enable-gpl has no H.264 and
// no H.265 encoder at all. Debian's build is the GPL one and this project
// ships it, but a self-hosted deploy may not, and the difference has to
// surface as "this build cannot produce H.264" rather than as ffmpeg's
// "Unknown encoder 'libx264'".
type Capabilities struct {
	// Version is the first line of `ffmpeg -version`.
	Version string
	// GPL reports whether the build has --enable-gpl in its configuration.
	GPL bool
	// Encoders is every encoder name the build lists.
	Encoders map[string]bool
}

// Has reports whether the build has this encoder.
func (c Capabilities) Has(encoder string) bool { return c.Encoders[encoder] }

// ReadCapabilities asks ffmpeg what it is and what it can encode.
//
// Two processes, once, at boot. The alternative — finding out at the first
// conversion — means a tool that is offered, accepted, queued and then fails
// on a file that was never the problem.
func ReadCapabilities(ctx context.Context, e Engine, r Runner, dir string) (Capabilities, error) {
	var version strings.Builder

	if _, err := r.Run(ctx, engine.Command{
		Path:   e.FFmpeg,
		Args:   []string{"-hide_banner", "-version"},
		Dir:    dir,
		Stdout: &version,
		Env:    Env(),
	}); err != nil {
		return Capabilities{}, Classify(err, "read the engine's version")
	}

	var encoders strings.Builder

	if _, err := r.Run(ctx, engine.Command{
		Path:   e.FFmpeg,
		Args:   []string{"-hide_banner", "-encoders"},
		Dir:    dir,
		Stdout: &encoders,
		Env:    Env(),
	}); err != nil {
		return Capabilities{}, Classify(err, "list the engine's encoders")
	}

	return CapabilitiesFrom(version.String(), encoders.String()), nil
}

// CapabilitiesFrom reads what the two commands printed.
//
// Exported so a test can hand it the output of an LGPL build without one
// being installed, which is how the "GPL-only codec on an LGPL build" path is
// proven rather than asserted.
func CapabilitiesFrom(version, encoders string) Capabilities {
	c := Capabilities{Encoders: make(map[string]bool, 64)}

	for line := range strings.SplitSeq(version, "\n") {
		trimmed := strings.TrimSpace(line)

		if c.Version == "" && strings.HasPrefix(trimmed, "ffmpeg version") {
			c.Version = trimmed
		}

		if strings.HasPrefix(trimmed, "configuration:") && strings.Contains(trimmed, "--enable-gpl") {
			c.GPL = true
		}
	}

	// `ffmpeg -encoders` prints a legend, a line of dashes, and then one line
	// per encoder: six flag characters, the name, then a description.
	//
	// The dashes are what the parse keys on, because the legend lines look
	// exactly like encoder lines to anything simpler: " V..... = Video" has
	// a six-character first field and a second field, so a flag-shape check
	// alone reads "=" as an encoder name.
	listing := false

	for line := range strings.SplitSeq(encoders, "\n") {
		trimmed := strings.TrimSpace(line)

		if !listing {
			listing = len(trimmed) >= 3 && strings.Trim(trimmed, "-") == ""

			continue
		}

		fields := strings.Fields(trimmed)
		if len(fields) < 2 || len(fields[0]) != 6 {
			continue
		}

		c.Encoders[fields[1]] = true
	}

	return c
}
