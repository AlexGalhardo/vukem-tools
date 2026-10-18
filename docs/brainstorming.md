# Brainstorming: vukem-tools

Session date: 2026-10-18. Owner: Alex Galhardo. Classification: **architectural** — a new project
made of several independent subsystems, so it is decomposed below and each sub-project gets its own
spec, plan and implementation cycle.

Research that fed these decisions: [web-research.md](web-research.md).

## 1. Intent

**What this is.** An open-source, self-hosted file toolbox in the spirit of CloudConvert: upload a
file (or paste a video URL), pick an operation, get the result back. Backend entirely on the
[vukem](https://vukem.dev/docs/introduction) framework; frontend Inertia v3 + React with base-ui
and Tailwind CSS v4.

**Why it exists**, in the owner's own priority order:

1. Push the vukem framework forward by using it for something demanding (uploads, long jobs, a
   queue, streaming progress, API keys, OpenAPI).
2. Be a tool the owner actually uses almost daily, for years.
3. Help produce assets for game development and digital content.
4. Go deeper into Go and into the tooling involved.

**What success looks like.** The owner converts and compresses his own files on
`https://tools.vukem.dev` instead of reaching for a web service; the Telegram flow he already uses
keeps working; and every vukem rough edge found along the way lands in
`framework-feedback/vukem-tools-feedback.md` and gets fixed in the framework.

**What it is not.** Not a business, not a CloudConvert competitor on format count, and not
commercial — several engines are GPL or AGPL and the site says so.

## 2. Starting point

A fresh repository. The owner decided on 2026-10-18 to start from zero rather than evolve an
earlier video-downloader project: the scope, the language and the architecture all change, so
carrying its code, its documentation or its history would only constrain the new design. Nothing
from it remains, in the tree or in the history.

The foundation is scaffolded by the framework itself — `vukem new`, then `install:inertia --react`,
`install:auth` and `install:api` on vukem v0.28.0 — which is also the point: if the generators do
not produce something good enough to build on, that is the first piece of framework feedback.

## 3. Decisions

Each item below was asked and answered in the session. "Decided" means it is settled; the ROADMAP
treats it as given.

### 3.1 Scope of the first wave

Decided: **all four groups** ship in the first wave.

- Refactor what already exists onto vukem: video download (yt-dlp), Cut & Crop (ffmpeg), compress
  video/audio/PDF.
- Pixels → SVG, rewritten in Go.
- Images: convert, compress/optimise, resize/crop/rotate/watermark/thumbnail.
- Video and audio: convert (MP4 ↔ WebM ↔ MKV, video → GIF), audio formats, transcription to SRT.

Deferred to a later phase, in the ROADMAP but not now: sprite sheet / frame atlas, archives, PDF
structural operations and OCR, Office → PDF, data formats, fonts.

### 3.2 Architecture

Decided: **one Go monolith on vukem serving both the JSON API and the Inertia pages.** One binary,
one deployment unit, one place where a route is defined. The API is designed fresh, with no
compatibility constraint from any earlier interface.

### 3.3 Job execution

All decided:

- **Queue:** `vukem/queue` on Postgres. No Redis, no second queue concept.
- **Progress:** `vukem/sse` pushes job progress; no polling. The existing
  `GET /api/v1/downloads/{job_id}` polling endpoint stays for the Telegram bot's sake.
- **Uploads:** `vukem/upload` with TUS resumable uploads.
- **Type detection:** magic bytes via `gabriel-vasile/mimetype`. Never the file extension, never
  the client's `Content-Type`.
- **Malware:** ClamAV through `clamd`, wired from the start (`vukem/upload` integrates it).
- **Result lifetime:** 5 minutes, then deleted (revised 2026-10-18; it was 1 hour). The Railway volume is small and a full disk takes the
  service down.

### 3.4 Licensing

All decided:

- The repository stays **MIT**. A public `/licenses` page lists every binary and library with its
  exact licence and a link; the footer carries the "no commercial use" notice and explains that it
  comes from the GPL/AGPL engines.
- **Pixels → SVG is reimplemented from scratch.** `pixels2svg` and `pixel2svg-rs` are both
  GPL-3.0; porting their code would contaminate an MIT repository. Both get explicit credit in the
  README and on `/licenses`.
- **ffmpeg is built with `--enable-gpl`** (x264/x265), because H.264/H.265 output is what every
  destination expects. The GPL notice goes in `NOTICE`.
- **Ghostscript (AGPL-3.0) is included** for lossy PDF compression. It is the only good
  open-source option; the repository is public, so the AGPL network-service obligation is met.

### 3.5 Authentication and limits

- **Accounts:** email and password with email verification, plus Google OAuth (`vukem/social`).
- **API keys:** one or more per user via `auth.Tokens`, each carrying abilities per tool
  (`image:convert`, `video:download`, ...), with a management page. The `X-API-Key` header the
  Telegram bot already sends keeps working.
- **No anonymous use.** Every operation requires an account. Simpler to reason about, and abuse
  stops being a design problem.
- **Webhooks:** a signed `job.finished` callback (HMAC-SHA256, retries, delivery log) through
  `vukem/webhook`, following CloudConvert's shape.
- **Limits:** 500 MB per file. Quota measured in CPU minutes per day per user rather than file
  count, plus rate limiting per IP and per API key.

### 3.6 Frontend

- **One page per tool** (`/image/convert`, `/pixels-to-svg`, ...) plus a home with search. Good for
  SEO and for bookmarking, and it matches how CloudConvert is organised.
- **Drag and drop with a visible queue:** several files at once, each its own job with its own
  progress bar over SSE, downloadable individually or as a `.zip`.
- **Before/after preview** with size in MB and the percentage saved, extended to every tool. For
  Pixels → SVG, a side-by-side SVG preview with sliders for colour tolerance and output mode.
- **Dark mode** following the system preference.
- **English only. No i18n.** (Owner's correction during the session — it overrides the earlier
  pt-BR + en suggestion.)
- The `impeccable` skill is loaded before any UI work, together with `frontend-design`. The target
  is a modern, minimal, easy interface.

### 3.7 Deployment

All decided, and the owner authorised changing this project's production configuration:

- **`tools.vukem.dev`** added as a Railway custom domain, with the CNAME created in the Vercel DNS
  zone that holds `vukem.dev`.
- **Own Dockerfile** replacing RAILPACK: multi-stage, `oven/bun` for assets, `golang` for the
  binary, a Debian slim runtime with ffmpeg, yt-dlp, libvips and Ghostscript **pinned to exact
  versions**.
- **Postgres added** for users, API keys and the queue.
- **Two services, `web` and `worker`.** The web service answers HTTP; the worker drains the queue.
  Heavy conversion stops competing with response time.
- **Railway object storage** (S3-compatible) for uploads and results, through `vukem/drivers/s3`
  with signed URLs — required because a Railway volume belongs to a single service and the two
  services must share files.

### 3.8 Engines

- **libvips is the primary image engine**; ImageMagick is kept only for exotic inputs (PSD, ICO),
  pinned ≥ 7.1.2-34 with a restrictive `policy.xml` after the three 2026 policy bypasses.
- **Pixels → SVG offers three output modes:** `square` (one rect per pixel, faithful and huge),
  `rect` (greedy run merging, the default) and `contour` (one polygon per colour blob, smallest
  output).
- **Pixels → SVG caps input at 2K**, not 4K. (Owner's correction during the session — it overrides
  the "up to 4K" in the original brief.) Above the cap the API explains the limit instead of
  exhausting memory.
- **Engines run as subprocesses, not through cgo.** `exec.CommandContext` with timeouts and
  rlimits. This avoids cgo cross-compilation pain and keeps the GPL engines at arm's length from
  the Go binary.

### 3.9 Telegram bot

Decided: **a Telegram bot in Go**, on vukem's Telegram support, in the first wave. It talks to the
app's own HTTP API with an API key whose abilities are limited to what it uses, so the bot is just
another API client and gets no privileged path.

### 3.10 Versioning

The project starts at **0.1.0** and follows the same standards as the vukem repositories: SemVer
tags `vX.Y.Z`, Conventional Commits, a Keep a Changelog file generated by git-cliff with a link per
commit, `.editorconfig` with tab width 4, markdownlint, and a `release.yml` that publishes the
GitHub release from the matching CHANGELOG section. Below 1.0.0 a breaking change is a minor bump.

### 3.11 Performance

Owner's explicit instruction: **optimise the Go backend as hard as possible so it consumes the
least server resource it can.** Concretely, for the ROADMAP:

- Stream everything. Never load a whole file into memory: `io.Copy` to disk or object storage,
  `bufio` writers for SVG output, shrink-on-load for JPEG.
- Bound concurrency explicitly. A worker pool sized from the container's CPU quota, not
  `GOMAXPROCS` guessed from the host.
- Set `rlimits` per subprocess so one job cannot take the container down.
- Measure before claiming: `testing.B` with `-benchmem` for the Go paths, and a recorded
  wall-clock plus peak RSS per conversion kind.
- Prefer a pure-Go path when it is not materially slower than shelling out, to avoid process
  startup cost on small files.

## 4. Decomposition

The project is too large for one spec. Four sub-projects, in order:

| # | Sub-project | Contents |
| --- | --- | --- |
| **S1** | Foundation | vukem app skeleton, Postgres, auth + Google OAuth, API keys with abilities, Scalar API docs, `vukem/queue`, TUS uploads, ClamAV, object storage, SSE progress, rate limiting, Dockerfile with pinned engines, Railway `web` + `worker` + Postgres + bucket, `tools.vukem.dev`, repo rename to 0.1.0, quality tooling |
| **S2** | Pixels → SVG | The Go rewrite: quantisation, union-find labelling, three output modes, 2K cap, benchmarks, UI with live preview and sliders |
| **S3** | Download, edit and compress | yt-dlp downloads with clips, ffmpeg trim and crop, video/audio/PDF compression, and the Telegram bot in Go |
| **S4** | Image, video and audio conversion | libvips image pipeline, ffmpeg conversion pipeline, whisper.cpp transcription, plus the deferred categories when their turn comes |

S1 must land first: everything else plugs into its queue, storage and auth. S2 is independent of
S3 and S4 and is the smallest self-contained win, so it ships second.

## 5. Open items

Carried into the ROADMAP as explicit tasks rather than assumptions:

- Confirm every licence that the GitHub API could not classify (`NOASSERTION`) against the upstream
  `LICENSE` file before the `/licenses` page ships. See §11 of
  [web-research.md](web-research.md).
- Decide the exact CPU-minutes-per-day quota number once a real conversion has been measured;
  until then the quota is enforced but generous.
- Confirm whether a seccomp filter via `go-seccomp-bpf` actually loads inside a Railway container.
  `nsjail` and `bubblewrap` are already ruled out there (no user namespaces).
- LibreOffice's licence is reported differently by the GitHub API (GPL-3.0) and by the project
  (MPL-2.0). Resolve before Office → PDF ships, which is a later phase anyway.
