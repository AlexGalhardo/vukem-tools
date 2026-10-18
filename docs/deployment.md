# Deployment

The production image carries the Go binary, the built Vite assets and every conversion engine.
Railway builds it from [`Dockerfile`](../Dockerfile); so does plain `docker build`.

## The image

| | |
| --- | --- |
| Runtime base | `debian:trixie-slim`, pinned by digest |
| Size | 1.2 GB |
| Cold start | 1.3 s, process start to the first 200 on `/health` |
| User | `app`, uid 10001, non-root |
| Working directory | `/app`, with `storage/` and `work/` owned by `app` |

### Why Debian and not Alpine

The vukem scaffold uses Alpine, and this project started there. Three things about the engines
forced the change:

- yt-dlp ships one official Linux binary, built against glibc. On musl it does not start.
- Debian's `ffmpeg` is the GPL build, with encoders Alpine's package leaves out.
- `prlimit` comes from util-linux, and it is how every engine gets its memory, CPU and file-size
  limits. [`internal/engine/sandbox_linux.go`](../internal/engine/sandbox_linux.go) explains why Go
  cannot apply them itself.

### Why 1.2 GB is accepted

ffmpeg with its codec libraries, libvips, Ghostscript, poppler and ClamAV are most of it. The
alternatives were worse: building each engine from source would trade a one-time pull for a long
build on every deploy, and splitting the engines across services would put a network hop in the
middle of every conversion. The figure is recorded here so a future increase is visible rather
than gradual.

## Pinned versions

Every engine is pinned to an exact version, never a distro default and never `latest`
([`.claude/rules/engines.md`](../.claude/rules/engines.md)). The pins are build arguments at the top
of the runtime stage.

| Engine | Version | Licence | Used by |
| --- | --- | --- | --- |
| ffmpeg / ffprobe | `7:7.1.5-0+deb13u1` | GPL-3.0 build | video, audio |
| libvips-tools | `8.16.1-1+deb13u2` | LGPL-2.1-or-later | raster images |
| ghostscript | `10.05.1~dfsg-1+deb13u3` | AGPL-3.0-or-later | PDF |
| poppler-utils | `25.03.0-5+deb13u4` | GPL-2.0-or-later | PDF text and pages |
| qpdf | `12.2.0-1` | Apache-2.0 | PDF structure |
| clamav | `1.4.3+dfsg-1` | GPL-2.0 | upload scanning |
| util-linux | `2.41.5-0+deb13u1` | GPL-2.0-or-later | `prlimit` |
| yt-dlp | `2026.08.19`, SHA-256 checked | Unlicense | downloads |

Each licence is also on the site's `/licenses` page, which is what users see.

### Refreshing a pin

Debian removes a package version from the mirror when it supersedes it, so a pin that worked last
month can fail with `Version '...' for '...' was not found`. To refresh one:

```bash
docker run --rm debian:trixie-slim sh -c \
  'apt-get update -qq >/dev/null 2>&1; apt-cache policy ffmpeg | awk "/Candidate:/ {print \$2}"'
```

Put the printed version in the matching `ARG`, rebuild, and run the engine tests in the image (see
below). To build against the old version meanwhile, point apt at `snapshot.debian.org` rather than
unpinning: an unpinned engine is a silently different engine.

yt-dlp is fetched from its GitHub release in its own build stage, with its checksum verified, so a
bad download fails the build. Its checksum comes from the release's `SHA2-256SUMS`.

## Verifying the image

The engine sandbox is Linux-only: the process-group kill and the resource limits skip on a Windows
or macOS development machine. Run those tests where they actually apply — inside the image:

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go test -c -o /tmp/engine.test ./internal/engine/
docker run --rm -v /tmp:/t:ro --entrypoint sh vukem-tools:local \
  -c 'cd /tmp && cp /t/engine.test . && ./engine.test'
```

Both Linux-only assertions must pass there: `TestKillingTheEngineKillsItsChildren` (a timeout kills
what the engine spawned, not only the engine) and `TestAFileSizeLimitStopsARunawayWrite`
(`RLIMIT_FSIZE` stops a conversion that would fill the disk).

Check the engines are present and are the pinned versions:

```bash
docker run --rm --entrypoint sh vukem-tools:local -c 'id; ffmpeg -version | head -1; yt-dlp --version'
```

## Known vulnerabilities

Scanned with `trivy image` at the version in the table above. Three critical findings are present
and **none has a fixed version released**, so they cannot be patched away. They are recorded here
with why they are not reachable, rather than left for someone to rediscover:

| CVE | Package | Reachable? |
| --- | --- | --- |
| CVE-2026-42216 | `libopenexr-3-1-30` | No. EXR is not a type this service recognises. |
| CVE-2026-42217 | `libopenexr-3-1-30` | No. Same. |
| CVE-2026-6653 | `libxml2` | Only through XML input, which no live tool accepts. |

OpenEXR is in the image because libvips depends on it, not because anything uses it. An
unrecognised type is refused by magic-byte detection before any engine sees the bytes, so the two
OpenEXR findings are unreachable. That mitigation is enforced by a test rather than by this
paragraph: `TestEXRIsNotARecognisedType` in
[`internal/detect`](../internal/detect/detect_test.go) fails if EXR is ever added, so the decision
has to be made deliberately.

libxml2's denial of service needs crafted XML. No live tool accepts XML or SVG **input**. A tool
that does — an SVG rasteriser, or PDF handling that parses embedded XMP — makes this finding
reachable, and that tool's task must re-check it first.

One package was patched ahead of the base image: `liblzma5` is pinned to `5.8.1-1+deb13u2`, the
security update Debian published after the pinned digest was built. It is installed by name so
every other package stays at the digest's version; a blanket `apt-get upgrade` would unpin the
whole image.

Re-run the scan on every engine bump and on every base digest change:

```bash
docker save vukem-tools:local -o /tmp/img.tar
docker run --rm -v /tmp:/s aquasec/trivy:0.69.1 image --input /s/img.tar   --severity CRITICAL --scanners vuln --quiet
```

A new critical with a fix available is a blocker. A new critical without one needs the same
reachability argument as the three above, written down here.

## Measurements

Both figures come from the image built from this `Dockerfile`, and both are here so a regression is
visible rather than gradual. The ROADMAP treats a size increase above 20 % as a review blocker.

| Measure | Value | How |
| --- | --- | --- |
| Image size | 1.2 GB | `docker images --format '{{.Size}}'` |
| Cold start | 1.3 s | container start to the first `200` on `/health`, SQLite and local disk |

Cold start is dominated by engine resolution: the app runs each engine's version command at boot
(nine programs), which is the cost of knowing at startup rather than at the first conversion.

```bash
cid=$(docker run -d -p 18080:3000 -e APP_ENV=development -e APP_KEY=...   -e DATABASE_URL=sqlite:/app/storage/app.db -e MAIL_MAILER=log vukem-tools:local serve)
start=$(date +%s%3N)
until [ "$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:18080/health)" = 200 ]; do :; done
echo "$(( $(date +%s%3N) - start )) ms"
```

## Observability

Nothing is exported unless it is configured, deliberately: an app that exports nothing should cost
nothing, and running `serve` on a laptop should not require a collector.

| Setting | Turns on |
| --- | --- |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | traces and metrics over OTLP/HTTP to a collector |
| `METRICS_TOKEN` | `GET /metrics`, the Prometheus scrape target |
| `APP_VERSION` | the `service.version` attribute, so a trace says which build produced it |

The two are independent. Push suits a collector; a scrape target is what an operator points at when
there is no collector. Both read the same instruments, so nothing is counted twice.

`/metrics` is guarded by a bearer token compared in constant time, and it answers **404** when
`METRICS_TOKEN` is unset — an endpoint that exists but refuses tells a scanner the service has
metrics worth coming back for, and one that was never enabled should look like one that does not
exist. It serves its own Prometheus registry rather than the default one, so the body carries this
service's instruments and not the Go runtime collectors anything linked into the binary could add.

### What is recorded

The vukem otel driver already records HTTP, database and queue signals.
[`internal/telemetry`](../internal/telemetry/telemetry.go) adds the conversion ones:

| Instrument | Unit | Attributes |
| --- | --- | --- |
| `vukem_tools.conversion.duration` | s | tool, status, error_type |
| `vukem_tools.conversion.ended` | 1 | tool, status, error_type |
| `vukem_tools.conversion.bytes.in` | By | tool |
| `vukem_tools.conversion.bytes.out` | By | tool |
| `vukem_tools.engine.failures` | 1 | engine, error_type |
| `vukem_tools.queue.depth` | 1 | status |

Every attribute is low-cardinality on purpose. A tool name, a status and an error type come from
fixed sets in the code; a conversion id, a file name, a URL or an account would each give a metric
unbounded cardinality, which is how a metrics backend is brought down by its own data. An error's
**type** is recorded and its **message** is not, for the same reason — a message can contain a file
name, and a label value is kept for every series indefinitely.

### What is never recorded

A trace is read by anybody with access to the tracing backend, which is a wider audience than the
owner of the file being converted. An engine's command line is where secrets leak from: it carries
the working directory, both file names, a token for anything authenticated, and for the downloader
the URL somebody asked for. `telemetry.ScrubArgs` reduces it to its shape before it reaches a span.

It is an **allowlist**, and the difference matters. The first version kept any argument that did not
look like a path or a URL, which let `--token vkt_live_…` through untouched: a secret is a bare
word, and so is a codec name. A bare argument now survives only when it matches something a trace
is actually read for — a number, a dimension, a short lowercase identifier. Everything else becomes
`?`. The SQL text is kept off database spans too; a parameterised query carries no values, but it
does carry the shape of every table.

`TestAnEngineCommandCarriesNoSecretAndNoIdentity` is that guarantee, not a note about it: it asserts
a signed URL, an API key and an email address are all absent from a scrubbed command, **and** that
the flags survive, because an attribute that keeps nothing is worth nothing.

### Alerting

These are the thresholds to set wherever the metrics land; the service exports the signals and does
not page anybody itself.

| Alert | Condition | Why this number |
| --- | --- | --- |
| Queue backing up | `vukem_tools.queue.depth{status="pending"}` above the worker's concurrency × 10 for 5 min | Below that it is a burst the workers will clear; above it, arrivals are outpacing the workers and every user is now waiting |
| Error rate | `ended{status="error"}` over `ended` above 5 % for 10 min | Conversions fail legitimately — a corrupt upload, a password-protected PDF — so a non-zero rate is normal. A tenfold jump is an engine or a deploy |
| An engine failing | any increase in `engine.failures{error_type="engine_failed"}` | This is the engine itself breaking, not a bad input: a bad input gets its own error type |
| Conversions timing out | `ended{error_type="timeout"}` above 1 % for 15 min | A timeout is the user's work thrown away after the longest wait; it should be rare |
| Nothing arriving | `ended` flat for 30 min during a period that normally has traffic | Catches the failure the others cannot: a submission path that is broken produces no metrics at all |

The last one is the one usually missing. Every other alert fires on bad data; a broken upload path
produces no data, and an alerting rule that only watches for bad values is silent exactly then.

## Startup behaviour

The app resolves every engine before it wires anything else, logs each one's path and version, and
refuses to start when an engine some live tool needs is missing
([`app/services/engines.go`](../app/services/engines.go)). A broken image therefore fails the
deploy rather than the first conversion that needs it.

An engine whose tools are all still planned is logged as missing and its tools stay unavailable.
That is deliberate: "this feature is not ready" and "the image is broken" are different problems
and should not look the same.

`ENGINE_WORKDIR` defaults to `/app/work`, on the image's own filesystem rather than `/tmp`.
Conversions write large intermediate files, and `/tmp` is a tmpfs on several platforms, which would
put them in RAM.
