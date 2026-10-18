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

## PDF (S6)

Measured inside the production image, which is the only place the engines exist: the host is
Windows and the subprocess sandbox is Linux-only, so a figure taken on the host would be a figure
for engines that are not the ones that ship. See [deployment.md](deployment.md), "Verifying the
image", for the commands.

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go test -c -o /tmp/pdf.test ./internal/pdf/
docker run --rm -v /tmp:/t:ro --entrypoint sh vukem-tools:local \
  -c 'mkdir -p /app/work/t && cd /app/work/t && cp /t/pdf.test . \
      && HOME=/app/work/t TMPDIR=/app/work/t ./pdf.test -test.run XXX -test.bench . -test.benchmem'
```

Engines: Ghostscript 10.05.1, qpdf 12.2.0, poppler 25.03.0, libvips 8.16.1. Host CPU: AMD Ryzen 7
5700X3D, `GOMAXPROCS=16`. Fixtures are A4 pages of text written by `internal/pdf`'s own writer,
except where a row says otherwise.

| Operation | Fixture | Wall clock | Peak RSS | Go allocations |
| --- | --- | --- | --- | --- |
| Watermark | 10 pages | 26.6 ms | 11.9 MB | 282 kB / 1202 allocs |
| Watermark | 50 pages | 55.8 ms | 11.8 MB | 506 kB / 4304 allocs |
| Watermark | 500 pages | 280 ms | 12.1 MB | 3.42 MB / 40921 allocs |
| Merge | 10 documents of 10 pages | 145 ms | — | 892 kB / 1915 allocs |
| Split into single pages | 20 pages | 122 ms | — | 755 kB / 1574 allocs |
| Rasterise at 150 DPI | 10 pages | 226 ms | — | 401 kB / 929 allocs |
| Compress, balanced | 20 pages | 119 ms | — | 46 kB / 155 allocs |
| PDF to Markdown | 100 pages | 29.3 ms | — | 115 kB / 392 allocs |
| Write a PDF (no engine) | 10 pages | 44.2 µs | — | 77 kB / 433 allocs |
| Write a PDF (no engine) | 500 pages | 1.23 ms | — | 600 kB / 20873 allocs |

### Peak memory against page count

ROADMAP S6.6 asks that a watermark on a 500-page document be applied in one pass with peak memory
independent of the page count. Both halves hold, and the distinction between the two memory
columns above is why the figures are reported separately:

- **Peak RSS is flat.** 11.9 MB at 10 pages, 11.8 MB at 50, 12.1 MB at 500 — a 50-fold increase in
  pages moves the high-water mark by about 2 %, which is noise against the Go runtime's own
  baseline. Each size is measured in its own process, because `VmHWM` never falls and a single
  process would report the largest run's figure for all three.
- **Cumulative allocation is not flat**, and should not be read as if it were: `B/op` is bytes
  allocated over the whole operation, not bytes live. It grows by about 6.3 kB per page, which is
  the page dictionary qpdf prints as JSON while the page geometry is read. That memory is freed as
  the decoder walks past it, which is exactly why the peak does not move.

One pass means one engine invocation: the stamp document is written page by page and composited by
a single `qpdf --overlay`, so a 500-page watermark is one subprocess rather than 500.

### What the figures say about the engine choices

- **Structure is cheap because nothing is re-rendered.** A 10-document merge and a 20-way split are
  both about an eighth of a second, because qpdf edits the object graph. Compression is in the same
  range only because the fixture is text; Ghostscript re-renders every page, and on a document of
  photographs it is an order of magnitude slower — the appearance test's fixture, four pages of
  2000 × 1400 photographs, compresses 698 kB to 74.5 kB in about 1.1 s.
- **Rasterising is the expensive one**, and it is the one that parallelises: each page is its own
  `pdftoppm`, bounded by `internal/cpu.Limit()` rather than by the host's core count. The 226 ms
  for 10 pages at 150 DPI is with 16 CPUs available; on a 1-vCPU container expect it to be
  sequential.
- **The PDF writer is not the cost of anything.** 1.23 ms for 500 pages means a watermark's stamp
  document is a rounding error next to the composite, which is what justified writing it in Go
  rather than reaching for another engine.
