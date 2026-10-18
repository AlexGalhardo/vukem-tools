# Deployment

The production image carries the Go binary, the built Vite assets and every conversion engine.
Railway builds it from [`Dockerfile`](../Dockerfile); so does plain `docker build`.

## The image

| | |
| --- | --- |
| Runtime base | `debian:trixie-slim`, pinned by digest |
| Size | 1.2 GB |
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
