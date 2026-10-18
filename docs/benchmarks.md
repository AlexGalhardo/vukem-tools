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

Each tool lands with its own row, carrying wall-clock and peak memory for a named fixture,
per `.claude/rules/go.md`.

Planned rows, in ROADMAP order:

- **Pixels → SVG** (S2.1): a 2048 × 2048 input in each of the three output modes — time, peak RSS,
  output bytes and element count. Also `go test -bench . -benchmem ./internal/pixelsvg/...`.
- **Trim** (S3.2): `-c copy` against a full re-encode on the same clip.
- **Image conversion** (S4.1): peak RSS for a 2K image, which must stay under the recorded ceiling.
- **Video encoding** (S4.4): wall-clock and output size per preset for one reference clip.
- **Transcription** (S4.5): wall-clock for a 30-minute input on the worker's CPU allocation.
- **Sprite sheet building** (S7.2): peak heap against frame count, which must not grow with it.

### Sprite sheet splitting (S7.1)

Host: AMD Ryzen 7 5700X3D, 16 logical CPUs, Windows, `go1.27.2`. Fixture: a square sheet of
128 × 128 cells, each a flat colour inside a one-pixel transparent border — flat regions, because
noise would make every pixel its own run and benchmark a shape no sprite sheet has.

```bash
go test -bench 'BenchmarkSplitGrid|BenchmarkDetectAuto' -benchmem -run '^$' \
  -benchtime 3x ./internal/spritesheet/
```

| Date | Path | Sheet | Frames | Wall-clock | Allocated | Allocs |
| --- | --- | --- | --- | --- | --- | --- |
| 2026-10-18 | Split, grid | 1024 × 1024 | 64 | 27.8 ms | 5.8 MB | 1 289 |
| 2026-10-18 | Split, grid | 2048 × 2048 | 256 | 110 ms | 19.3 MB | 4 951 |
| 2026-10-18 | Split, grid | 4096 × 4096 | 1024 | **397 ms** | 70.2 MB | 20 464 |
| 2026-10-18 | Detect, auto | 1024 × 1024 | 64 | 8.8 ms | 2.6 MB | 234 |
| 2026-10-18 | Detect, auto | 2048 × 2048 | 256 | 32.6 ms | 11.5 MB | 686 |
| 2026-10-18 | Detect, auto | 4096 × 4096 | 1024 | 153 ms | 48.2 MB | 3 125 |

Allocated is total bytes allocated per operation, not the live heap. The live heap is the figure
that decides whether this fits the container, and it is recorded separately below.

#### Peak live heap

```bash
go test -run 'TestPeakMemory' -v -count=1 ./internal/spritesheet/
```

| Date | Sheet | Frames | Peak heap in use | Decoded sheet alone |
| --- | --- | --- | --- | --- |
| 2026-10-18 | 2048 × 2048 | 64 | 19.4 MiB | 16.0 MiB |
| 2026-10-18 | 2048 × 2048 | 256 | 19.5 MiB | 16.0 MiB |
| 2026-10-18 | 2048 × 2048 | 1024 | 20.1 MiB | 16.0 MiB |
| 2026-10-18 | 4096 × 4096 (`MaxPixels`) | 1024 | **68.6 MiB** | 64.0 MiB |

Sixteen times the frames costs 0.7 MiB more live heap, which is what proves the design: frames are
encoded into the archive one at a time through crops that share the decoded sheet's own pixel
buffer, so the cost is one sheet plus one encoder rather than one sheet plus every frame. At the
area limit the overhead above the decoded sheet is 4.6 MiB.

**This is Go's live heap, not the container's RSS.** The two are not the same number and this host
is Windows, so an RSS figure measured here would not be comparable with the Linux container's.
The ROADMAP's S7.1 assert is recorded as met on the heap figure and the ratio; the container RSS
row is owed and will be taken on Linux when the tool is deployed. Saying so beats presenting a
host number as a container number.

Under `-race` the same 4096 × 4096 split peaks at 102 MiB rather than 68.6 MiB, because the
detector holds shadow state per allocation. The test widens its bound in a race build rather than
skipping, so a real regression is still caught either way.

#### What the first measurement found

The first run of this benchmark was **1 504 ms and 1.28 GB allocated over 16 823 313 allocations**
for the 4096 × 4096 case — close to one allocation per pixel. Two causes, both found by measuring
rather than reading:

- `image.Image.At` returns a `color.Color` interface, so every pixel read boxed a colour onto the
  heap. Resolving the concrete image type once and indexing `Pix` directly (`internal/spritesheet`
  `pixels`) removed it.
- The per-frame crop was an `image.Image` adapter, which is correct but defeats `png.Encode`'s
  type switch onto its fast path, sending every pixel back through the boxing `At` call. Re-basing
  the concrete `SubImage` to the origin instead keeps both the shared pixel buffer and the fast
  path.
- `png.Encoder` was then still allocating its compressor per frame; giving it a `BufferPool` cut
  the remaining 1.21 GB to 70 MB.

Net: **3.8 × faster, 18 × less allocated, 822 × fewer allocations.** Recorded because the ROADMAP
asks for a measurement rather than a claim, and because the first number was the one that would
have shipped.
