<h1 align="center">Vukem Tools</h1>

<p align="center">An open-source file conversion toolbox. Convert, compress and optimise images, video, audio and documents, download videos, and turn pixel art into SVG — self-hosted, on your own machine or server.</p>

<p align="center">
  <a href="https://github.com/AlexGalhardo/vukem-tools/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/AlexGalhardo/vukem-tools/ci.yml?branch=main&label=tests" alt="Tests"></a>
  <a href="https://github.com/AlexGalhardo/vukem-tools/releases"><img src="https://img.shields.io/github/v/release/AlexGalhardo/vukem-tools?include_prereleases&label=version" alt="Version"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/AlexGalhardo/vukem-tools" alt="License"></a>
</p>

## Introduction

Vukem Tools is a self-hosted alternative to the hosted file converters, built as a single Go
binary. The backend is written entirely on the [vukem](https://vukem.dev/docs/introduction)
framework; the frontend is Inertia v3 with React, Base UI and Tailwind CSS v4.

It exists for three reasons: to be a tool its author uses nearly every day, to produce assets for
game development and digital content, and to push the vukem framework by building something
demanding on top of it.

> **Not for commercial use.** Several conversion engines are licensed under the GPL or the AGPL.
> Every engine, with its exact licence, is listed on the `/licenses` page and in
> [docs/web-research.md](docs/web-research.md).

## Status

Version 0.6.0, phase S9 of the [ROADMAP](ROADMAP.md). Images, video, audio, PDF, archives, sprite
sheets, the developer tools and the Telegram bot are in. A tool whose engine is missing from the
image is not registered at all, so the tool index shows it as planned rather than offering a form
that cannot work — `GET /api/v1/capabilities` reports which engines a given deployment has.

## Tools

Every tool needs an account. The index at `/` lists them, `Ctrl+K` searches them, and the sidebar
groups them by family.

| Family | What it does | Engine |
| --- | --- | --- |
| Images | Convert, compress, pixel art to SVG, images to one PDF | libvips, own Go implementation |
| Video and audio | Convert, compress, cut and crop, video to GIF, download from YouTube, Instagram, X and TikTok | ffmpeg, yt-dlp |
| PDF | Merge, split, rotate, crop, compress, repair, protect, unlock, compare, watermark, page numbers, to text, to images, to PDF/A, OCR, fill forms, sign | qpdf, Ghostscript, poppler, OCRmyPDF with Tesseract |
| Documents to PDF | Word, PowerPoint and Excel to PDF | LibreOffice |
| Archives | Create and extract ZIP, TAR, TAR.GZ and GZ; sprite sheets and atlases | Own Go implementation |
| Developer tools | JSON, YAML, CSV and XML, JSON Schema and tool-definition checks, diff, hashes, passwords, UUID and ULID, Base64, JWT | Your browser |
| Brazil | CPF and CNPJ, CEP and CNPJ lookup, fake people for test data | Your browser, ViaCEP, BrasilAPI |
| LLM and agents | Token estimate, cost estimate, clean a model's reply, convert chat formats | Your browser |

The same tools are on the JSON API (`/docs`), and `/llms.txt` and `/llms-full.txt` describe them for
language models.

## Telegram bot

`cmd/bot` is a separate binary that serves the tools from a chat. Nobody types a password into the
chat: the bot shows a short code and a button, you sign in on the site, check the code and press
Connect, and the bot receives a key limited to what its menus use. The main menu shows who is
signed in and what is left of the day's budget, and signing out revokes the key.

```bash
TELEGRAM_BOT_TOKEN=... BOT_API_URL=https://tools.example.com go run ./cmd/bot
```

The settings are in `.env.example`, and the design is in
[docs/brainstorming.md § 9.8](docs/brainstorming.md).

## Stack

| Layer | Technology |
| --- | --- |
| Backend | [vukem](https://vukem.dev) on Go, `net/http`-native |
| Frontend | [Inertia](https://inertiajs.com) v3 + React 19, [Base UI](https://base-ui.com), [Tailwind CSS](https://tailwindcss.com) v4, Vite |
| Database | PostgreSQL |
| Jobs | `vukem/queue` on PostgreSQL, progress over Server-Sent Events |
| Uploads | `vukem/upload` with resumable [tus](https://tus.io) uploads and ClamAV scanning |
| Storage | S3-compatible object storage with signed URLs; results expire after five minutes |
| API docs | OpenAPI 3.1 with a [Scalar](https://scalar.com) reference at `/docs` |
| Engines | ffmpeg, yt-dlp, libvips, Ghostscript, qpdf, poppler, LibreOffice, OCRmyPDF with Tesseract — all as subprocesses, pinned by version |
| Quality | golangci-lint, `go test -race`, markdownlint, Conventional Commits, git-cliff |
| Deploy | [Railway](https://railway.com): a `web` service and a `worker` service from one image |

## Quick start

Requirements: Go (the version in `go.mod`), [Bun](https://bun.com), and the engines for the tools
you want to run.

```bash
git clone https://github.com/AlexGalhardo/vukem-tools.git
cd vukem-tools
bun install
cp .env.example .env
go run ./cmd/app key:generate
go run ./cmd/app migrate
bun run dev          # Vite, in one terminal
go run ./cmd/app serve   # the app, in another
```

The app listens on `http://localhost:3000` and the API reference on `http://localhost:3000/docs`.

## Documentation

- [ROADMAP.md](ROADMAP.md) — every task, subtask and quality assert, by phase
- [docs/brainstorming.md](docs/brainstorming.md) — the decisions behind the design, and why
- [docs/web-research.md](docs/web-research.md) — every conversion engine, its licence and its traps
- [CHANGELOG.md](CHANGELOG.md) — Keep a Changelog, generated by git-cliff

## Quality

Every commit holds the gates in [ROADMAP.md](ROADMAP.md#global-quality-gates):
`go build`, `go vet`, `golangci-lint run`, `go test -race`, `bun run typecheck`, `bun run build`
and `markdownlint-cli2`, all clean. Commits follow
[Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/), versions follow
[SemVer](https://semver.org), and the changelog follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## Licences

The project itself is MIT. The engines it drives are not all permissive: ffmpeg is built with
`--enable-gpl` (GPL-2.0-or-later), Ghostscript is AGPL-3.0, and several optimisers are GPL. They
run as separate processes, and this repository is public, which satisfies the GPL and AGPL
obligations — but the site declares no commercial use for that reason.

The Pixels to SVG tool was written from the published algorithms, not ported: both
[pixels2svg](https://github.com/ValentinFrancois/pixels2svg) and
[pixel2svg-rs](https://github.com/FluffyBucketSnake/pixel2svg-rs) are GPL-3.0, and their authors
deserve the credit for the approach.

The full list, engine by engine, is on the `/licenses` page and in
[docs/web-research.md](docs/web-research.md).

### Downloading from a platform

The video download tool fetches from YouTube, Instagram, X and TikTok with
[yt-dlp](https://github.com/yt-dlp/yt-dlp). **Downloading a video may breach the platform's terms
of service, and you are responsible for what you download and for what you do with it.** This
project provides a tool; it grants no right to the material the tool fetches. Copyright in a video
belongs to whoever made it, and neither the project nor whoever runs an instance of it can give you
permission you do not already have.

Downloading is restricted to those four platforms by design. yt-dlp's `generic` extractor will
fetch any URL at all, which would turn the endpoint into an open proxy inside whatever network it
runs in, so the URL is matched against a closed list of platforms and yt-dlp is restricted to that
platform's own extractors.

## Contributing

Read [CONTRIBUTING.md](.github/CONTRIBUTING.md) first: it covers the setup, the quality bar every
commit holds, and the two documents whose decisions a pull request is expected to respect. By
taking part you agree to the [code of conduct](.github/CODE_OF_CONDUCT.md).

## Security

This app accepts arbitrary files from the internet and feeds them to decoders written in C, so it
takes that seriously. The policy, the scope and how to report privately are in
[SECURITY.md](.github/SECURITY.md) — **do not open a public issue** for a vulnerability.

## License

[MIT](LICENSE) © Alex Galhardo
