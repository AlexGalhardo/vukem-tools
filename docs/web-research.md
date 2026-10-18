# Web research: conversion engines for vukem-tools

Research date: 2026-10-18. Target: an open-source, self-hosted file toolbox in the spirit of
[CloudConvert](https://cloudconvert.com), with a Go backend built on the
[vukem](https://vukem.dev/docs/introduction) framework.

Only open-source tools are considered. Every engine the site ships must appear on the public
`/licenses` page with its exact licence, and the site carries a "no commercial use" notice because
several engines are GPL or AGPL.

## Method and how to read the tables

- **Licence column.** `spdx` values were read from the GitHub REST API
  (`GET /repos/{owner}/{repo}` → `license.spdx_id`) on 2026-10-18 for 136 repositories. Where the
  API answered `NOASSERTION` it means GitHub could not classify the `LICENSE` file; those rows say
  **not classified** and, when a primary licensing page exists, link it. **Every "not classified"
  row must be confirmed against the upstream `LICENSE` file before it is printed on `/licenses`.**
- **Release column.** `tag_name` and `published_at` of the latest GitHub release. A `-` means the
  project publishes no GitHub releases (it may still be active — see the `pushed` date).
- **Go access.** `pure` = pure Go, `cgo` = cgo/purego binding, `exec` = external binary driven
  through `os/exec`.
- Claims that could not be verified are labelled **unverified** instead of being stated. They are
  not a basis for a decision.

## 1. What CloudConvert actually runs

CloudConvert publishes far less than its 212-format marketing page suggests.

- The only engines named in their API docs are `libreoffice` and `office` for document conversion
  ([convert task](https://cloudconvert.com/api/v2/convert)) and `ffmpeg`, `imagemagick`,
  `graphicsmagick` for command tasks
  ([command task](https://cloudconvert.com/api/v2/command), whose example response shows
  ffmpeg 6.1.4). GPU video uses `ffmpeg_nvidia`
  ([blog](https://cloudconvert.com/blog/gpu-video-encoding-for-h264-hevc-av1)).
- Pandoc, Calibre, Ghostscript and Tesseract are **not** named as CloudConvert engines anywhere we
  could find. The complete per-task engine list is only exposed through the per-task API endpoint,
  not through the docs.
- Architecture, from their own pages:
  - A job is a DAG of tasks: import → processing → export; "typically, each job has at least one
    import and one export task"; statuses `waiting`, `processing`, `finished`, `error`; by default
    the job fails if one task fails ([jobs](https://cloudconvert.com/api/v2/jobs)).
  - Job creation is async or sync; the same page admits "there might be cases in which we need to
    queue your job".
  - Default task timeout is **5 hours**, configurable; the docs explicitly discourage sync mode for
    long jobs such as video encoding.
  - "Each conversion runs in a separate, isolated container" and resources are strictly separated
    per customer ([security](https://cloudconvert.com/security)).
  - Files are deleted "at the latest after 24 hours" and jobs 24 hours after they end
    ([privacy](https://cloudconvert.com/privacy)).
  - Webhooks: `job.finished` event, per-webhook signing secret, `CloudConvert-Signature` header
    carrying an HMAC-SHA256 of the raw body, 3 retries on network failure or HTTP ≥ 500, and a
    `410` disables the webhook ([webhooks](https://cloudconvert.com/api/v2/webhooks)).
  - Rate limiting answers `429` with `Retry-After`.
- Input format defaults to **the file extension** ("if not set, the extension of the input file is
  used as input format", [convert](https://cloudconvert.com/api/v2/convert)). No content-based
  sniffing is documented, and no malware scanning is documented on their privacy, security or API
  pages. vukem-tools does better on both counts (see §9).

Self-hosted projects worth reading as prior art:

| Project | What it is | Licence | Release | URL |
| --- | --- | --- | --- | --- |
| ConvertX | Multi-engine web converter, the closest open-source analogue | AGPL-3.0 | v0.19.0 (2026-09-26) | <https://github.com/C4illin/ConvertX> |
| Stirling-PDF | PDF operations suite; its Dockerfile is a shopping list of engines | not classified (open-core: MIT with proprietary carve-out directories) | v3.2.0 (2026-10-15) | <https://github.com/Stirling-Tools/Stirling-PDF> |
| Gotenberg | Stateless HTTP API wrapping LibreOffice + Chromium | MIT | v8.38.1 (2026-10-15) | <https://github.com/gotenberg/gotenberg> |
| imgproxy | Image processing proxy; checks real type and dimensions before decoding | Apache-2.0 | v4.0.18 (2026-10-14) | <https://github.com/imgproxy/imgproxy> |
| thumbor | Image resize service | MIT | 7.8.0 (2026-05-30) | <https://github.com/thumbor/thumbor> |

Stirling-PDF's Dockerfile installs LibreOffice, `ocrmypdf`, `poppler-utils`, Tesseract, Calibre
`ebook-convert`, WeasyPrint, `unoserver`, ffmpeg and a JRE — Ghostscript arrives indirectly as an
`ocrmypdf` dependency. That is the practical engine set for a document converter.

## 2. Chosen stack

Decisions taken in [brainstorming.md](brainstorming.md); the reasoning lives here.

| Area | Choice | Why |
| --- | --- | --- |
| Images | **libvips** primary, ImageMagick only for exotic inputs | libvips streams and keeps a small working set; ImageMagick loads a full pixel cache and had three policy bypasses in 2026 (§9) |
| Video and audio | **ffmpeg** GPL build (x264/x265) + **yt-dlp** | Only practical answer; the GPL build is required for H.264/H.265 output |
| Subtitles | **whisper.cpp** on CPU | MIT, no Python runtime, no GPU |
| PDF structure | **pdfcpu** (Go) with **qpdf** as fallback | Apache-2.0, no AGPL, and pdfcpu is a library so no subprocess for the common cases |
| PDF compression | **Ghostscript** | The only good open-source lossy PDF compressor; AGPL, declared on `/licenses` |
| Archives | **mholt/archives** + **klauspost/compress**, `7zz` for 7z | Pure Go for zip/tar/gz/zst; 7-Zip only where Go cannot write the format |
| Pixel art → SVG | **Own Go implementation** | Both prior projects are GPL-3.0 and this repo is MIT (§7) |
| Queue | **vukem/queue** on Postgres | Already in the framework; no new infrastructure |
| Progress | **vukem/sse** | Push instead of polling |
| Uploads | **vukem/upload** (TUS) + **gabriel-vasile/mimetype** | Resumable uploads and magic-byte detection |
| Malware | **ClamAV** through `clamd` | Already integrated in `vukem/upload` |
| Storage | **Railway object storage** (S3-compatible) via `vukem/drivers/s3` | Shared between the `web` and `worker` services; the Railway volume is one per service |

## 3. Images

### 3.1 External engines

| Tool | Role | Licence | Release | Go access | URL |
| --- | --- | --- | --- | --- | --- |
| libvips | Streaming convert, resize, crop, watermark, thumbnail | LGPL-2.1 | v8.18.7 (2026-09-26) | `exec` (`vips`) or `cgo` | <https://github.com/libvips/libvips> |
| ImageMagick 7 | Widest codec coverage (PSD, ICO, exotic) | not classified — [ImageMagick Licence](https://imagemagick.org/license/), permissive, Apache-2.0-like | 7.1.2-34 (2026-10-14) | `exec` | <https://github.com/ImageMagick/ImageMagick> |
| libjpeg-turbo | JPEG decode/encode, `jpegtran` lossless transforms | not classified (IJG + BSD-3-Clause + zlib triple) | 3.2.0 (2026-06-30) | `exec` | <https://github.com/libjpeg-turbo/libjpeg-turbo> |
| mozjpeg | Smaller JPEG at equal quality | not classified (libjpeg-turbo derived) | v4.1.1 (2022-08-15) — stale | `exec` (`cjpeg`) | <https://github.com/mozilla/mozjpeg> |
| libjxl | JPEG XL, plus `cjpegli` | BSD-3-Clause | v0.12.0 (2026-07-01) | `exec` | <https://github.com/libjxl/libjxl> |
| libavif | AVIF still images | not classified (BSD-2-Clause per repo) | v1.4.2 (2026-05-26) | `exec` | <https://github.com/AOMediaCodec/libavif> |
| libwebp | WebP, `cwebp`/`dwebp`/`img2webp`/`gif2webp` | BSD-3-Clause | no GitHub releases (pushed 2026-10-16) | `exec` or `cgo` | <https://github.com/webmproject/libwebp> |
| libheif | HEIC/HEIF container | not classified (LGPL per README) | v1.23.6 (2026-10-05) | `exec` | <https://github.com/strukturag/libheif> |
| oxipng | Lossless PNG optimiser | MIT | v10.2.1 (2026-09-02) | `exec` | <https://github.com/shssoichiro/oxipng> |
| pngquant | Lossy PNG (palette) | not classified — repo states GPL-3.0-or-later **or** commercial | no GitHub releases (pushed 2026-06-21) | `exec` | <https://github.com/kornelski/pngquant> |
| jpegoptim | Lossless/lossy JPEG optimiser | **GPL-3.0** | v1.5.6 (2025-09-15) | `exec` | <https://github.com/tjko/jpegoptim> |
| gifsicle | GIF optimise and edit | **GPL-2.0** | no GitHub releases (pushed 2026-10-15) | `exec` | <https://github.com/kohler/gifsicle> |
| svgo | SVG optimiser (Node) | MIT | v4.1.0 (2026-08-24) | `exec` | <https://github.com/svg/svgo> |
| exiftool | Metadata read/write/strip | **GPL-3.0** | no GitHub releases (pushed 2026-05-27) | `exec` | <https://github.com/exiftool/exiftool> |
| ECT | Zopfli-class PNG/JPEG/ZIP recompression | Apache-2.0 | v0.9.5 (2024-01-08) | `exec` | <https://github.com/fhanau/Efficient-Compression-Tool> |
| zopfli | Maximum-deflate PNG/gzip | Apache-2.0 | zopfli-1.0.3 (2019-11-27) — stale | `exec` | <https://github.com/google/zopfli> |
| rimage | Rust CLI bundling mozjpeg/avif/jxl/oxipng | Apache-2.0 | v0.14.0 (2026-09-27) | `exec` | <https://github.com/SalOne22/rimage> |
| caesium-clt | Batch compression CLI | not classified — repo states GPLv3 **or** commercial | v1.5.0 (2026-09-16) | `exec` | <https://github.com/Lymphatus/caesium-clt> |
| RawTherapee | Camera RAW development | **GPL-3.0** | 5.13 (2026-07-26) | `exec` | <https://github.com/Beep6581/RawTherapee> |

Not adopted: `guetzli` (Apache-2.0, last release 2017, superseded by jpegli), `scour`
(Apache-2.0, last push 2024), `imageoptim-cli` (MIT, macOS only), GraphicsMagick (release and
licence data could not be fetched — **unverified**).

### 3.2 Go libraries

| Library | Role | Licence | Release | Access | URL |
| --- | --- | --- | --- | --- | --- |
| golang.org/x/image | Decoders for WebP (decode only), TIFF, BMP; `font/sfnt` | BSD-3-Clause | module, no tags | pure | <https://pkg.go.dev/golang.org/x/image> |
| davidbyttow/govips | libvips binding | MIT | v2.20.0 (2026-10-16) | cgo | <https://github.com/davidbyttow/govips> |
| h2non/bimg | libvips binding | MIT | v1.1.5 (2020-11-21) — effectively unmaintained | cgo | <https://github.com/h2non/bimg> |
| anthonynsimon/bild | Filters, resize, pure Go | MIT | v0.17.1 (2026-09-07) | pure | <https://github.com/anthonynsimon/bild> |
| disintegration/imaging | Resize, crop, filters | MIT | v1.6.2 (2019-11-16) — stale | pure | <https://github.com/disintegration/imaging> |
| HugoSmits86/nativewebp | WebP **encoder**, lossy + lossless + animation | MIT | v1.3.0 (2026-05-10) | pure | <https://github.com/HugoSmits86/nativewebp> |
| kolesa-team/go-webp | WebP encode/decode | MIT | v1.0.5 (2025-03-24) | cgo | <https://github.com/kolesa-team/go-webp> |
| gen2brain/avif | AVIF; purego + WASM fallback, no cgo | MIT | module (pushed 2026-08-14) | purego | <https://github.com/gen2brain/avif> |
| gen2brain/jpegxl | JPEG XL; purego + WASM | MIT | module (pushed 2026-08-14) | purego | <https://github.com/gen2brain/jpegxl> |
| gen2brain/heic | HEIC/AVIF; purego + WASM | MIT | module (pushed 2026-09-15) | purego | <https://github.com/gen2brain/heic> |
| soniakeys/quant | Median-cut quantisation to `image.Paletted` | MIT | module (pushed 2018-03-04) | pure | <https://github.com/soniakeys/quant> |
| delthas/octreequant | Octree quantisation, faster | MIT | module (pushed 2024-07-24) | pure | <https://github.com/delthas/octreequant> |

`golang.org/x/image/webp` decodes but does not encode; `nativewebp` covers encoding without cgo.

### 3.3 Recommended path per operation

- **Resize, crop, rotate, thumbnail, watermark, format conversion between JPEG/PNG/WebP/TIFF/GIF:**
  libvips, with shrink-on-load for JPEG and sequential access mode.
- **JPEG output:** `cjpeg` (mozjpeg) or `cjpegli` (libjxl) for best quality per byte; `jpegtran`
  for lossless rotate/crop.
- **PNG output:** `oxipng` for lossless, `pngquant` when a palette is acceptable, `ECT` when the
  last few per cent matter and time does not.
- **WebP:** `cwebp`, `img2webp` for animation; `nativewebp` when staying inside Go matters more
  than encode speed.
- **AVIF:** `avifenc`. **JPEG XL:** `cjxl`, with `--lossless_jpeg=1` to repack an existing JPEG
  without generation loss.
- **HEIC:** `heif-convert` / `heif-enc` (LGPL).
- **GIF:** `gifsicle -O3` to optimise, `gif2webp` to modernise.
- **SVG:** `svgo` to optimise; rasterise through libvips (librsvg loader) rather than ImageMagick's
  MSVG.
- **PSD, ICO, exotic:** ImageMagick, flattening first.
- **RAW:** RawTherapee CLI (GPL-3.0). Not benchmarked — treat as a later phase.

## 4. Video and audio

| Tool | Role | Licence | Release | Go access | URL |
| --- | --- | --- | --- | --- | --- |
| ffmpeg / ffprobe | Everything: transcode, mux, filter, thumbnail, loudness | not classified — LGPL-2.1-or-later by default, **GPL-2.0-or-later when built with `--enable-gpl`** ([legal](https://ffmpeg.org/legal.html)) | rolling (pushed 2026-10-18) | `exec` | <https://github.com/FFmpeg/FFmpeg> |
| x264 | H.264 encoder | **GPL-2.0** | mirror, no releases | via ffmpeg | <https://github.com/mirror/x264> |
| x265 | HEVC encoder | **GPL-2.0** | mirror, no releases | via ffmpeg | <https://github.com/videolan/x265> |
| SVT-AV1 | Fastest practical AV1 encoder | **unverified** (hosted on GitLab, not covered by the GitHub sweep) | v4.2.0 (2026-07-14, per release page) | via ffmpeg | <https://gitlab.com/AOMediaCodec/SVT-AV1> |
| rav1e | AV1 encoder in Rust | BSD-2-Clause | v0.8.1 (2025-06-16) | via ffmpeg | <https://github.com/xiph/rav1e> |
| libvpx | VP8/VP9 | BSD-3-Clause | mirror (pushed 2026-10-18) | via ffmpeg | <https://github.com/webmproject/libvpx> |
| yt-dlp | Downloader for YouTube/Instagram/X/TikTok and ~1000 more | Unlicense | 2026.08.19 | `exec` | <https://github.com/yt-dlp/yt-dlp> |
| bgutil-ytdlp-pot-provider | PO token provider for YouTube | **GPL-3.0** | 2.0.2 (2026-10-07) | sidecar service | <https://github.com/Brainicism/bgutil-ytdlp-pot-provider> |
| whisper.cpp | Speech-to-text → SRT/VTT, CPU viable | MIT | v1.9.5 (2026-10-06) | `exec` | <https://github.com/ggml-org/whisper.cpp> |
| GPAC / MP4Box | MP4 muxing, DASH/HLS packaging | LGPL-2.1 | v26.07.0 (2026-07-24) | `exec` | <https://github.com/gpac/gpac> |
| shaka-packager | DASH/HLS/CENC packaging | not classified | v3.9.3 (2026-07-27) | `exec` | <https://github.com/shaka-project/shaka-packager> |
| Bento4 | MP4 tooling | not classified (repo states GPL **or** commercial) | no releases (pushed 2026-06-27) | `exec` | <https://github.com/axiomatic-systems/Bento4> |
| FLAC | Lossless audio codec and CLI | API reports GFDL-1.3 for the docs tree; the codec is BSD-3-Clause and the `flac` CLI GPL-2.0 — **confirm before publishing** | 1.5.0 (2025-02-11) | `exec` | <https://github.com/xiph/flac> |
| opus-tools | `opusenc`/`opusdec` | not classified (BSD-3-Clause expected — **unverified**) | no releases (pushed 2026-03-25) | `exec` | <https://github.com/xiph/opus-tools> |
| Rubber Band | Time-stretch and pitch-shift | **GPL-2.0** | no releases (pushed 2025-03-03) | `exec` | <https://github.com/breakfastquay/rubberband> |
| audiowaveform | Waveform JSON/PNG for players | **GPL-3.0** | 1.10.3 (2025-08-20) | `exec` | <https://github.com/bbc/audiowaveform> |
| gifski | High-quality video → GIF | not classified (AGPL-3.0 or commercial per project — **confirm**) | 1.34.0 (2025-07-13) | `exec` | <https://github.com/ImageOptim/gifski> |
| libvmaf | Perceptual video quality score | not classified (BSD-2-Clause-Patent expected — **unverified**) | v3.2.1 (2026-09-14) | `exec` | <https://github.com/Netflix/vmaf> |
| ssimulacra2 | Perceptual image/frame metric | BSD-3-Clause | v2.1 (2023-04-20) | `exec` | <https://github.com/cloudinary/ssimulacra2> |
| u2takey/ffmpeg-go | Fluent builder that shells out to ffmpeg | Apache-2.0 | v0.5.0 (2023-08-08) — stale | pure (wraps `exec`) | <https://github.com/u2takey/ffmpeg-go> |
| asticode/go-astiav | cgo binding to libav* | MIT | module (pushed 2026-09-23) | cgo | <https://github.com/asticode/go-astiav> |

SoX is not adopted: last release 14.4.2 from 2015-02-22 on SourceForge, and ffmpeg filters cover
the same ground. MKVToolNix could not be fetched (**unverified**); `mkvmerge` remains a candidate
if MKV remuxing becomes a requirement.

### 4.1 ffmpeg recipes

Flags below are starting points, to be pinned and measured against the build we ship.

- **MP4 → WebM:** `-c:v libvpx-vp9 -crf 31 -b:v 0 -row-mt 1 -c:a libopus -b:a 96k`, or
  `-c:v libsvtav1 -preset 6 -crf 32`. Neither needs the GPL build.
- **Video → GIF (two-pass palette):**
  `-vf "fps=12,scale=480:-1:flags=lanczos,split[a][b];[a]palettegen[p];[b][p]paletteuse"`.
  `gifski` produces better results at a higher cost.
- **Compress video (1080p starting CRF):** x264 18–23, x265 22–28, SVT-AV1 `-preset 6` CRF 28–35,
  libvpx-vp9 CRF 31–33 with `-b:v 0`. x264/x265 require the **GPL build**.
- **Trim without re-encode:** `-ss <t> -i in -t <d> -c copy`. Cuts land on keyframes; re-encode
  when frame accuracy matters.
- **Crop/scale:** `-vf crop=w:h:x:y`, `-vf scale=-2:720` (keep dimensions even).
- **Thumbnail / sprite sheet:** `-vf "fps=1/10,scale=320:-1,tile=5x5"`.
- **Audio:** `-c:a libmp3lame -q:a 2`, `-c:a pcm_s16le`, `-c:a flac -compression_level 8`,
  `-c:a libopus -b:a 96k`.
- **Loudness (EBU R128):** `-af loudnorm=I=-16:TP=-1.5:LRA=11`; two passes for accuracy.
- **Burn subtitles:** `-vf subtitles=file.srt` (needs libass, ISC).
- **Progress:** parse `-progress pipe:1` key/value output (`out_time_us`, `progress=end`) rather
  than scraping stderr.

### 4.2 Hardware acceleration

NVENC, QSV, VAAPI, Vulkan and D3D12 encoders are pointless on Railway: no GPU and no device nodes.
Plan for CPU encoders with conservative presets and a worker queue. ffmpeg 8.1 added Vulkan and
D3D12 encoders, which only matters on self-hosted machines with a GPU.

### 4.3 yt-dlp operational reality

- Releases ship roughly weekly; a stale yt-dlp is the single most common cause of YouTube failures.
  The 2026.07.04 notes raise the minimum Python to 3.11 and fix CVE-2026-55404.
- Some clients require a PO token and answer HTTP 403 without one
  ([PO Token Guide](https://github.com/yt-dlp/yt-dlp/wiki/PO-Token-Guide)). `bgutil` is the usual
  provider, but its own page warns PO tokens no longer bypass the bot check in most cases.
- Cookies, a residential proxy and browser-based token fallbacks are the remaining levers. The
  current Python deployment already carries `YTDLP_POT_PROVIDER_*`, `YOUTUBE_COOKIES*` and
  `YTDLP_SLEEP_*` settings — that behaviour must be preserved by the Go rewrite.
- The README must state that downloading may breach a platform's terms of service and that the
  user is responsible.

## 5. Documents, PDF, OCR, ebooks

| Tool | Role | Licence | Release | Go access | URL |
| --- | --- | --- | --- | --- | --- |
| Gotenberg | HTTP service: Office and HTML → PDF; bundles LibreOffice 26.8.1 + Chromium 155 | MIT | v8.38.1 (2026-10-15) | HTTP | <https://github.com/gotenberg/gotenberg> |
| LibreOffice | Office → PDF and friends | **GPL-3.0** (project states MPL-2.0 for core; API reports GPL-3.0 — **confirm**) | rolling | `exec` (`soffice`) | <https://github.com/LibreOffice/core> |
| unoserver | Keeps a LibreOffice instance warm | MIT | module (pushed 2026-06-10) | `exec` | <https://github.com/unoconv/unoserver> |
| pdfcpu | Merge, split, rotate, watermark, encrypt, optimise — in Go | Apache-2.0 | v0.16.2 (2026-10-11) | pure | <https://github.com/pdfcpu/pdfcpu> |
| qpdf | Structural PDF repair and lossless optimisation | Apache-2.0 | v12.4.2 (2026-09-27) | `exec` | <https://github.com/qpdf/qpdf> |
| Ghostscript (ghostpdl) | Lossy PDF compression, PostScript | not classified — **AGPL-3.0** or commercial ([licensing](https://ghostscript.com/licensing)) | rolling | `exec` | <https://github.com/ArtifexSoftware/ghostpdl> |
| MuPDF / mutool | Fast PDF render and extraction | **AGPL-3.0** | rolling | `exec` | <https://github.com/ArtifexSoftware/mupdf> |
| poppler | `pdftotext`, `pdftoppm`, `pdfimages` | **unverified** (fetch failed; GPL-2.0 expected) | 26.10.0 (2026-10-04, per project site) | `exec` | <https://poppler.freedesktop.org/> |
| OCRmyPDF | Scanned PDF → searchable PDF | MPL-2.0 | v17.13.0 (2026-09-28) | `exec` | <https://github.com/ocrmypdf/OCRmyPDF> |
| Tesseract | OCR engine | Apache-2.0 | 5.5.3 (2026-07-24) | `exec` | <https://github.com/tesseract-ocr/tesseract> |
| PaddleOCR | OCR, higher accuracy, heavier | Apache-2.0 | v3.7.0 (2026-06-11) | `exec` | <https://github.com/PaddlePaddle/PaddleOCR> |
| Surya | OCR and layout | Apache-2.0 code; **weights under a modified AI Pubs Open RAIL-M licence** (free below US$5M revenue/funding) | v0.22.1 (2026-07-20) | `exec` | <https://github.com/datalab-to/surya> |
| Pandoc | Markup and document conversion | **GPL-2.0** | 3.12.1 (2026-10-08) | `exec` | <https://github.com/jgm/pandoc> |
| WeasyPrint | HTML/CSS → PDF without a browser | BSD-3-Clause | v70.0 (2026-09-08) | `exec` | <https://github.com/Kozea/WeasyPrint> |
| Typst | Modern typesetting → PDF | Apache-2.0 | v0.15.1 (2026-07-17) | `exec` | <https://github.com/typst/typst> |
| tectonic | Self-contained LaTeX | not classified (MIT expected — **unverified**) | 0.17.0 (2026-07-27) | `exec` | <https://github.com/tectonic-typesetting/tectonic> |
| Calibre | `ebook-convert` for EPUB/MOBI/AZW3 | **GPL-3.0** | v9.16.0 (2026-10-09) | `exec` | <https://github.com/kovidgoyal/calibre> |
| markitdown | Anything → Markdown | MIT | v0.1.8 (2026-09-21) | `exec` | <https://github.com/microsoft/markitdown> |
| docling | Document → structured Markdown/JSON | MIT | v2.140.0 (2026-10-16) | `exec` | <https://github.com/docling-project/docling> |
| img2pdf | Images → PDF without recompression | LGPL-3.0 | module (pushed 2026-07-01) | `exec` | <https://github.com/josch/img2pdf> |
| klippa-app/go-pdfium | PDFium binding | MIT | v1.21.3 (2026-10-09) | cgo + binary | <https://github.com/klippa-app/go-pdfium> |
| gen2brain/go-fitz | MuPDF binding | **AGPL-3.0** | module (pushed 2026-07-06) | cgo | <https://github.com/gen2brain/go-fitz> |
| ledongthuc/pdf | Simple PDF text extraction | BSD-3-Clause | module (pushed 2026-09-07) | pure | <https://github.com/ledongthuc/pdf> |
| go-pdf/fpdf | PDF generation (successor of the archived `jung-kurt/gofpdf`) | MIT | v0.9.0 (2023-09-13) | pure | <https://github.com/go-pdf/fpdf> |

**Excluded:** `unidoc/unipdf` is not open source — it requires a commercial licence code
(<https://unidoc.io/eula/>). `wkhtmltopdf` is archived (LGPL-3.0, last push 2022-11-22); use
Gotenberg's Chromium or WeasyPrint instead.

### 5.1 Engine per conversion

| Conversion | First choice | Fallback |
| --- | --- | --- |
| DOCX/XLSX/PPTX/ODT → PDF | Gotenberg | `unoserver` + a pool of `soffice` processes |
| HTML → PDF | Gotenberg (Chromium) | WeasyPrint |
| Markdown → PDF/DOCX | Pandoc | Typst (PDF only) |
| PDF → images | `pdftoppm` (poppler) | `mutool` (AGPL) |
| Images → PDF | `img2pdf` | pdfcpu |
| PDF merge/split/rotate/watermark/encrypt | pdfcpu | qpdf |
| PDF compress (lossy) | Ghostscript (AGPL) | — (pdfcpu/qpdf are lossless only) |
| PDF → text | `pdftotext` | ledongthuc/pdf |
| PDF → Markdown | markitdown | docling |
| Scanned PDF → searchable | OCRmyPDF (Tesseract) | PaddleOCR |
| EPUB/MOBI ↔ PDF | Calibre `ebook-convert` | Pandoc (EPUB only) |
| PDF → DOCX | LibreOffice Draw — poor quality | — |

### 5.2 Running LibreOffice safely

- A shared user profile makes a second `soffice` invocation hand the job to the already running
  instance instead of converting. Give every worker its own `-env:UserInstallation`
  ([report](https://ask.libreoffice.org/t/convert-to-commands-in-parallel-possible/90182)).
- A fresh profile per job costs startup time — one report measures 1 s → 5 s (same thread).
- Headless `--convert-to` hanging at 100 % CPU is a known failure
  ([report](https://ask.libreoffice.org/t/headless-convert-to-pdf-dies-with-100-cpu/76193)).
  Always wrap in `exec.CommandContext` with a timeout and kill the process group.
- `unoserver` has no load balancing; scale with one instance per port.
- CJK and emoji output needs the matching fonts installed in the image, or glyphs come out as
  boxes.
- Because of the image size (~1 GB) and the operational cost, Office → PDF is a later phase.

### 5.3 OCR on CPU

Weak evidence; treat as indicative, not decisive. One invoice test reports PaddleOCR 100 % in
4.85 s, Tesseract 87.5 % in 0.16 s, Surya 95.8 % in 2.1 s
([source](https://codesota.com/ocr/best-for-python)). No rigorous 2026 benchmark was found.
Tesseract is the pragmatic default: fast, Apache-2.0, no model-licence question.

## 6. Archives, fonts, data, 3D

### 6.1 Archives

| Tool | Role | Licence | Release | Go access | URL |
| --- | --- | --- | --- | --- | --- |
| mholt/archives | zip, tar, gz, bz2, xz, zst, 7z/RAR read | MIT | v0.1.5 (2025-10-13) | pure | <https://github.com/mholt/archives> |
| klauspost/compress | Fast zstd, gzip, flate, zip | not classified (BSD-3-Clause, Go-style) | v1.20.3 (2026-10-12) | pure | <https://github.com/klauspost/compress> |
| bodgit/sevenzip | 7z **read** | BSD-3-Clause | v1.6.5 (2026-07-10) | pure | <https://github.com/bodgit/sevenzip> |
| nwaples/rardecode | RAR4/RAR5 **read** | BSD-2-Clause | module (pushed 2026-10-13) | pure | <https://github.com/nwaples/rardecode> |
| andybalholm/brotli | Brotli | MIT | module (pushed 2026-10-16) | pure | <https://github.com/andybalholm/brotli> |
| 7-Zip (`7zz`) | 7z **write**, broad format support | not classified — LGPL-2.1-or-later with the unRAR restriction ([licence](https://7-zip.opensource.jp/license.txt)) | 26.04 (2026-10-06) | `exec` | <https://github.com/ip7z/7zip> |
| libarchive (`bsdtar`) | Universal archive reader/writer | not classified (BSD-2-Clause expected — **unverified**) | v3.8.10 (2026-10-14) | `exec` | <https://github.com/libarchive/libarchive> |
| zstd | Zstandard CLI | not classified (BSD-3-Clause **and** GPL-2.0 dual) | v1.5.7 (2025-02-19) | `exec` | <https://github.com/facebook/zstd> |
| brotli | Brotli CLI | MIT | v1.2.0 (2025-10-27) | `exec` | <https://github.com/google/brotli> |
| xz | XZ/LZMA CLI | not classified (0BSD + public domain parts) | v5.8.4 (2026-09-09) | `exec` | <https://github.com/tukaani-project/xz> |

RAR **creation** is not available under any open-source licence: the unRAR licence forbids using
the code to recreate RAR compression. Offer RAR as read-only and say so in the UI.

### 6.2 Fonts

| Tool | Role | Licence | Release | Go access | URL |
| --- | --- | --- | --- | --- | --- |
| fonttools | TTF/OTF/WOFF/WOFF2 conversion and subsetting | MIT | 4.67.0 (2026-10-14) | `exec` | <https://github.com/fonttools/fonttools> |
| woff2 | WOFF2 encode/decode | MIT | v1.0.2 (2017-11-13) | `exec` | <https://github.com/google/woff2> |
| sfnt2woff-zopfli | WOFF with Zopfli compression | not classified | module (pushed 2022-11-29) | `exec` | <https://github.com/bramstein/sfnt2woff-zopfli> |
| FontForge | Font editor and converter | not classified (GPL-3.0 expected — **unverified**) | 20251009 | `exec` (`-script`) | <https://github.com/fontforge/fontforge> |
| golang.org/x/image/font/sfnt | Parse TTF/OTF in Go | BSD-3-Clause | module | pure | <https://pkg.go.dev/golang.org/x/image/font/sfnt> |
| ConradIrwin/font | Go font tooling | MIT | module (pushed 2026-08-21) | pure | <https://github.com/ConradIrwin/font> |
| benoitkugler/textlayout | Go text shaping and font parsing | MIT | module (pushed 2026-03-04) | pure | <https://github.com/benoitkugler/textlayout> |

`fonttools` is the single dependency that covers the whole TTF/OTF/WOFF/WOFF2 matrix; everything
else is optional.

### 6.3 Data and spreadsheets

| Tool | Role | Licence | Release | Go access | URL |
| --- | --- | --- | --- | --- | --- |
| qax-os/excelize | XLSX read/write with streaming | BSD-3-Clause | v2.11.0 (2026-07-06) | pure | <https://github.com/qax-os/excelize> |
| gocarina/gocsv | CSV ↔ struct | MIT | module (pushed 2026-10-12) | pure | <https://github.com/gocarina/gocsv> |
| parquet-go/parquet-go | Parquet | Apache-2.0 | v0.32.0 (2026-08-12) | pure | <https://github.com/parquet-go/parquet-go> |
| DuckDB | CSV ↔ Parquet ↔ JSON with SQL | MIT | v1.5.6 (2026-09-28) | `exec` or driver | <https://github.com/duckdb/duckdb> |
| Miller (`mlr`) | CSV/TSV/JSON stream processing | not classified (BSD-2-Clause expected — **unverified**) | v6.22.0 (2026-09-25) | `exec` | <https://github.com/johnkerl/miller> |
| jq | JSON transformation | not classified (MIT expected — **unverified**) | jq-1.8.2 (2026-06-20) | `exec` | <https://github.com/jqlang/jq> |
| yq | YAML/JSON/XML transformation | MIT | v4.54.1 (2026-09-29) | `exec` | <https://github.com/mikefarah/yq> |
| dasel | Query and transform JSON/YAML/TOML/XML/CSV | MIT | v3.11.2 (2026-06-27) | `exec` | <https://github.com/TomWright/dasel> |
| qsv | Very fast CSV toolkit | not classified | 25.0.0 (2026-10-14) | `exec` | <https://github.com/dathere/qsv> |

CSV ↔ JSON ↔ XML is entirely feasible in pure Go with the standard library plus `excelize` for
XLSX; DuckDB only earns its place once Parquet is on the list.

### 6.4 Vector and pixel art

| Tool | Role | Licence | Release | Go access | URL |
| --- | --- | --- | --- | --- | --- |
| resvg | SVG → PNG rendering, strict SVG support | Apache-2.0 | v0.48.1 (2026-08-02) | `exec` | <https://github.com/RazrFalcon/resvg> |
| xo/resvg | Go binding for resvg | MIT | v0.12.1 (2026-10-13) | cgo | <https://github.com/xo/resvg> |
| vtracer | Raster → SVG with curve fitting | MIT | 1.0.0-alpha.4 (2026-08-29) | `exec` | <https://github.com/visioncortex/vtracer> |
| potrace | Bitmap → vector, the classic | **GPL-2.0** | — (SourceForge) | `exec` | <https://potrace.sourceforge.net> |
| gotranspile/gotrace | Machine translation of potrace to Go | **GPL-2.0** | module (pushed 2023-07-26) | pure | <https://github.com/gotranspile/gotrace> |
| axseem/pixv | Pixel art → SVG in Go (path/rect/square modes) | MIT | module (pushed 2025-07-02) | pure | <https://github.com/axseem/pixv> |
| ajstarks/svgo | SVG writer, streams to an `io.Writer` | not classified | module (pushed 2022-12-09) | pure | <https://github.com/ajstarks/svgo> |
| paulmach/orb | `simplify` package: Douglas-Peucker and Visvalingam | MIT | v0.13.0 (2026-03-30) | pure | <https://github.com/paulmach/orb> |
| Inkscape | SVG → PDF/EPS, CLI | not classified (GPL family — **unverified**) | — | `exec` | <https://github.com/inkscape/inkscape> |

### 6.5 3D, CAD, misc

| Tool | Role | Licence | Release | URL |
| --- | --- | --- | --- | --- |
| Assimp | 40+ 3D mesh formats | not classified (BSD-3-Clause expected — **unverified**) | v6.0.5 (2026-04-30) | <https://github.com/assimp/assimp> |
| Blender (headless) | 3D, render, heavy | not classified (**GPL-2.0-or-later**) | rolling | <https://github.com/blender/blender> |
| MeshLab | Mesh processing | **GPL-3.0** | 2025.07 | <https://github.com/cnr-isti-vclab/meshlab> |
| OpenSCAD | Script → STL | not classified (GPL-2.0 family) | 2021.01 | <https://github.com/openscad/openscad> |
| LibreCAD | 2D CAD | not classified (GPL-2.0 family) | v2.2.1.6 (2026-10-17) | <https://github.com/LibreCAD/LibreCAD> |
| LibreDWG (`dwg2dxf`) | DWG → DXF | **GPL-3.0** | 0.14 (2026-06-27) | <https://github.com/LibreDWG/libredwg> |
| libqrencode | QR code generation | LGPL-2.1 | v4.1.1 (2020-09-28) | <https://github.com/fukuchi/libqrencode> |

3D and CAD are out of scope for now: every usable engine is GPL and most are very large. Revisit
only if the game-asset workflow needs it.

## 7. Pixel art → SVG: designing the Go rewrite

### 7.1 What the prior art does

- **`pixel2svg-rs`** (Rust, GPL-3.0, `src/main.rs`, 155 lines) emits **one `<rect>` per
  non-transparent pixel**, with `--squaresize` (default 40), an optional 1 px overlap, hex or
  `rgb()` colours and alpha as `opacity`. There is no merging at all. A 2048×2048 sprite would
  produce up to 4.2 million elements — no browser renders that comfortably.
- **`pixels2svg`** (Python, GPL-3.0) is smarter: it reduces colours with a luminosity-based
  tolerance (`apply_color_tolerance`), maps RGBA to integer labels, finds connected blobs, traces
  each blob's contour (`calculate_blob_contours`, `_get_pixel_contour_segments`), reduces the
  contour to a minimal polygon (`minimal_polygon`) and groups paths by colour in `<g>`. Its own
  README reports 60 s for a 150×150 sprite without tolerance, 4 s at tolerance 64 and 0.8 s at 128.
- The original `pixel2svg` by Florian Berger (Python 2, GPL) is one shape per pixel with no
  merging.

Both inputs are **GPL-3.0** and this repository is MIT. The algorithms themselves are not
copyrightable, but their code is: the Go version is written from the algorithm description below,
not translated from either source, and both projects get explicit credit in the README and on
`/licenses`.

### 7.2 Algorithm for the Go version

1. **Decode** with `image` from the standard library into an RGBA buffer.
2. **Quantise** optionally: median cut (`soniakeys/quant`) or octree (`delthas/octreequant`),
   capped at 256 colours. A colour-tolerance pass merges near-identical neighbours, which is what
   makes the output usable at all.
3. **Label** connected regions per colour with union-find over horizontal runs — one pass over the
   rows, merging a run with the runs it touches on the previous row.
4. **Emit**, in one of three modes:
   - `square`: one `<rect>` per pixel. Faithful, huge; matches `pixel2svg-rs`.
   - `rect` (default): greedy merge of horizontal runs, then a vertical merge of identical
     adjacent runs into maximal rectangles. Big size win, pixel-exact output.
   - `contour`: trace each region's boundary (Moore-neighbour tracing, with a separate pass for
     holes), simplify with Douglas-Peucker (`paulmach/orb/simplify`), emit one `<path>` per region.
     Smallest files; matches `pixels2svg`.
5. **Write** by streaming into a `bufio.Writer`, grouping by colour in `<g fill="...">` so the
   fill is written once per colour rather than per shape, and setting
   `shape-rendering="crispEdges"` with a `viewBox` so scaling stays sharp.
6. **Limit**: reject inputs above **2048×2048** (the agreed cap) with a clear message, instead of
   trying and exhausting memory.

Memory at the 2K cap: an RGBA buffer is ~16 MB and an `int32` label grid ~16 MB, so the whole job
fits comfortably in a small container. Row bands processed by `runtime.NumCPU()` workers, each
emitting per-colour fragments stitched in order, keep it parallel without holding every path in
memory.

No published benchmark of pixel-art vectorisation at 1080p or 4K was found, so the target numbers
have to come from our own `testing.B` runs with `-benchmem`.

### 7.3 When to use a real vectoriser instead

`potrace` (GPL-2.0) and `vtracer` (MIT) fit curves and produce far smaller files, but they change
the pixels. They are the right answer for logos and scans, the wrong answer for pixel art where
every pixel edge matters. If curve fitting is ever offered, `vtracer` is the one to shell out to —
MIT, actively developed, and it supports clustering plus polygon/spline fitting with `--simplify`.

## 8. Go infrastructure

| Library | Role | Licence | Release | URL |
| --- | --- | --- | --- | --- |
| gabriel-vasile/mimetype | Magic-byte type detection, reads only the header | MIT | v1.4.16 (2026-10-17) | <https://github.com/gabriel-vasile/mimetype> |
| h2non/filetype | Magic-byte detection | MIT | v1.1.1 (2021-01-21) | <https://github.com/h2non/filetype> |
| riverqueue/river | Postgres job queue, retries, unique jobs | MPL-2.0 | active (pushed 2026-10-18) | <https://github.com/riverqueue/river> |
| hibiken/asynq | Redis job queue | MIT | v0.26.0 (2026-02-03) | <https://github.com/hibiken/asynq> |
| google/nsjail | Namespace/cgroup/rlimit jail | Apache-2.0 | 3.6 (2026-03-18) | <https://github.com/google/nsjail> |
| containers/bubblewrap | Unprivileged sandbox | not classified (LGPL-2.0-or-later expected) | v0.13.1 (2026-10-14) | <https://github.com/containers/bubblewrap> |
| google/minijail | Chromium's sandbox | BSD-3-Clause | linux-v18 (2023-05-23) | <https://github.com/google/minijail> |
| elastic/go-seccomp-bpf | seccomp filters in pure Go | Apache-2.0 | v1.6.0 (2025-06-20) | <https://github.com/elastic/go-seccomp-bpf> |
| Cisco-Talos/clamav | Malware scanning | **GPL-2.0** | 1.5.4 (2026-08-07) | <https://github.com/Cisco-Talos/clamav> |
| PyYoshi/go-clamav | `clamd` INSTREAM client in Go | MIT | v0.4.0 (2026-10-13) | <https://github.com/PyYoshi/go-clamav> |

`vukem/queue` (Postgres and memory drivers) replaces river/asynq here — it is already in the
framework, already tested, and avoids a second queue concept in the codebase. River stays on this
list as the escape hatch if `vukem/queue` turns out to lack something the workload needs; that
finding would be framework feedback, not a reason to fork.

**Sandboxing reality check.** `nsjail` and `bubblewrap` both need user namespaces, and Docker's
default seccomp profile blocks nested namespace creation
([Docker security](https://docs.docker.com/engine/security/non-events/)); `--privileged` would
disable seccomp entirely, which is not available on Railway and would be worse than the problem.
The achievable defence in depth is therefore:

- Run as a non-root user.
- One working directory per job, removed when the job ends.
- `exec.CommandContext` with a timeout, killing the whole process group.
- `setrlimit` before `exec` (`RLIMIT_AS`, `RLIMIT_CPU`, `RLIMIT_FSIZE`, `RLIMIT_NPROC`).
- No network for conversion subprocesses (only `yt-dlp` needs it).
- `no_new_privs`, plus a seccomp filter through `go-seccomp-bpf` where the environment allows it.

## 9. Operational traps

- **ImageMagick had three policy bypasses in 2026.** CVE-2026-25965 (path traversal: the policy was
  checked before `..` resolution, fixed in 7.1.2-15 / 6.9.13-40), JLSEC-2026-953 (`fd:<n>` bypasses
  the default policy's `-` block), CVE-2026-61858 (APNG encoder plus an external delegate bypasses
  the policy, CWE-59, fixed in 7.1.2-26). Pin ≥ 7.1.2-34, write an allowlist `policy.xml`
  (including `<policy domain="path" rights="none" pattern="fd:*"/>`), and disable unused delegates.
  Sources: [CVE-2026-25965](https://nvd.nist.gov/vuln/detail/CVE-2026-25965),
  [JLSEC-2026-953](https://security.julialang.org/advisories/JLSEC-2026-953/),
  [CVE-2026-61858](https://www.sentinelone.com/vulnerability-database/cve-2026-61858/).
- **Ghostscript CVE history** is long; GLSA 202508-02 covers CVE-2025-27830/27834/27835, fixed in
  10.05.1 ([GLSA](https://glsa.gentoo.org/glsa/202508-02)). Pin it and keep it current.
- **Zip bombs and zip slip.** Cap the compression ratio, total uncompressed size, entry count and
  nesting depth; sanitise destination paths and refuse symlinks pointing outside the extraction
  root.
- **Never trust the extension or the client's `Content-Type`.** Sniff magic bytes. Note that Office
  formats keep their signature near the end of the file, so `mimetype.SetLimit` must allow for it.
- **Large images.** libvips streams; ImageMagick materialises a pixel cache. Decode JPEG with
  shrink-on-load when a smaller output is wanted. Set an explicit pixel cap — `vukem/upload`
  already has one.
- **EXIF and ICC.** Stripping metadata protects privacy but also drops the colour profile; convert
  to sRGB before stripping.
- **Railway volumes** are one per service, 0.5 GB on free, 5 GB on Hobby and 50 GB on Pro
  ([docs](https://docs.railway.com/reference/volumes)). A full disk takes the service down, which
  is why results expire in 1 hour and why uploads go to object storage rather than the volume.
- **Licensing when distributing.** An ffmpeg build with `--enable-gpl` makes the combined binary
  GPL. We ship engines as separate processes in a container and the repository is public, which
  satisfies both GPL and AGPL obligations, but the `/licenses` page and a `NOTICE` file must say so
  explicitly. Do not link `fdk-aac` — it is not redistributable.
- **Patents.** H.264, H.265 and AAC are patent-encumbered; AV1, VP9 and Opus are royalty-free.
  Worth a line on `/licenses`.
- **Long jobs.** Always a context timeout and always kill the process group; ffmpeg in particular
  ignores a plain terminate in some configurations.

## 10. Licence summary for the `/licenses` page

Engines whose licence imposes obligations on us, as verified above:

- **AGPL-3.0:** Ghostscript (PDF compression), MuPDF (if used), `gen2brain/go-fitz` (not used).
- **GPL-3.0:** `jpegoptim`, `exiftool`, LibreOffice (per the API; confirm against MPL-2.0 claim),
  Calibre, `audiowaveform`, LibreDWG, MeshLab, RawTherapee, `bgutil-ytdlp-pot-provider`.
- **GPL-2.0:** x264, x265, `gifsicle`, Pandoc, ClamAV, Rubber Band, potrace, poppler (expected),
  Blender, OpenSCAD, LibreCAD.
- **GPL-2.0-or-later via build flag:** ffmpeg when built with `--enable-gpl` — which is what we
  ship.
- **Dual GPL/commercial:** `pngquant`, `caesium-clt`, Bento4, `gifski` (confirm each).
- **LGPL:** libvips (LGPL-2.1), libheif, GPAC, `img2pdf` (LGPL-3.0), `libqrencode`, 7-Zip
  (LGPL-2.1 with the unRAR restriction).
- **Weights with a non-OSI licence:** Surya (AI Pubs Open RAIL-M) — avoid, or state the revenue
  threshold.
- **Not open source, excluded:** `unidoc/unipdf`.

Everything else in this document is MIT, BSD, Apache-2.0, MPL-2.0 or the permissive ImageMagick
licence.

## 11. Open questions

- Confirm every **not classified** licence against the upstream `LICENSE` file before the
  `/licenses` page ships. The API's `NOASSERTION` is not an answer.
- LibreOffice: the GitHub API reports GPL-3.0 while the project's own page says MPL-2.0. Resolve
  before shipping Office conversion.
- FLAC: separate the codec licence (BSD-3-Clause) from the `flac` CLI licence (GPL-2.0).
- SVT-AV1 lives on GitLab and was not covered by the licence sweep.
- No rigorous 2026 benchmark was found for AV1 encoders, OCR engines or image optimiser chains.
  Measure on our own container before publishing any quality or speed claim.

## 12. Sources

Primary pages used for the architecture and engine claims:

- <https://cloudconvert.com/api/v2/jobs>, `/api/v2/convert`, `/api/v2/command`,
  `/api/v2/webhooks`, <https://cloudconvert.com/security>, <https://cloudconvert.com/privacy>
- <https://cloudconvert.com/blog/gpu-video-encoding-for-h264-hevc-av1>
- <https://ffmpeg.org/legal.html>
- <https://ghostscript.com/licensing>, <https://mupdf.readthedocs.io/en/1.27.0/license.html>
- <https://imagemagick.org/license/>, <https://7-zip.opensource.jp/license.txt>
- <https://github.com/yt-dlp/yt-dlp/wiki/PO-Token-Guide>
- <https://docs.railway.com/reference/volumes>,
  <https://docs.docker.com/engine/security/non-events/>
- <https://ask.libreoffice.org/t/convert-to-commands-in-parallel-possible/90182>,
  <https://ask.libreoffice.org/t/headless-convert-to-pdf-dies-with-100-cpu/76193>
- <https://nvd.nist.gov/vuln/detail/CVE-2026-25965>,
  <https://security.julialang.org/advisories/JLSEC-2026-953/>,
  <https://www.sentinelone.com/vulnerability-database/cve-2026-61858/>,
  <https://glsa.gentoo.org/glsa/202508-02>
- <https://github.com/Stirling-Tools/Stirling-PDF/blob/master/Dockerfile>,
  <https://github.com/C4illin/ConvertX>, <https://github.com/imgproxy/imgproxy>
- Licence and release data: GitHub REST API `GET /repos/{owner}/{repo}` and
  `GET /repos/{owner}/{repo}/releases/latest`, 136 repositories, 2026-10-18.
- Prior art read locally: `pixel2svg-rs/src/main.rs` (GPL-3.0),
  `pixels2svg-py/pixels2svg/**` (GPL-3.0).
