# Benchmarks

Recorded numbers, so a claim about speed, size or memory can be checked instead of believed.
`.claude/rules/go.md` requires a measurement before any performance claim, and `ROADMAP.md` makes
several of these numbers quality asserts.

How to reproduce a row is part of the row. A number without its command is not a benchmark.

## Container image

| Date | Image | Size | Command |
| --- | --- | --- | --- |
| 2026-10-18 | `vukem-tools:local`, foundation only (no conversion engines yet) | **40.8 MB** | `docker build -t vukem-tools:local . && docker image inspect vukem-tools:local --format '{{.Size}}'` |

The foundation image is the Go binary plus the embedded Inertia assets on `alpine`, with no
conversion engines. Phase S1.11 adds ffmpeg, yt-dlp, libvips and Ghostscript, which will dominate
the size from then on; the ROADMAP treats a later regression above 20 % as a review failure, and
this row is the baseline that makes that measurable.

## Startup

| Date | What | Result | Command |
| --- | --- | --- | --- |
| 2026-10-18 | `about` in the production image: binary links, config loads, routes resolve | exit 0, 19 routes | `docker run --rm -e APP_KEY=... -e MAIL_MAILER=log vukem-tools:local about` |

## Conversion paths

Nothing to record yet: no tool is implemented. Each one lands with its own row, carrying wall-clock
and peak RSS for a named fixture, per `.claude/rules/go.md`.

Planned rows, in ROADMAP order:

- **Pixels → SVG** (S2.1): a 2048 × 2048 input in each of the three output modes — time, peak RSS,
  output bytes and element count. Also `go test -bench . -benchmem ./internal/pixelsvg/...`.
- **Trim** (S3.2): `-c copy` against a full re-encode on the same clip.
- **Image conversion** (S4.1): peak RSS for a 2K image, which must stay under the recorded ceiling.
- **Video encoding** (S4.4): wall-clock and output size per preset for one reference clip.
- **Transcription** (S4.5): wall-clock for a 30-minute input on the worker's CPU allocation.
