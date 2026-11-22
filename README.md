<div align="center">

<h1><a href="https://tools.vukem.dev" target="_blank" rel="noopener noreferrer">Vukem Tools</a></h1>

<a href="https://github.com/AlexGalhardo/vukem-tools/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/AlexGalhardo/vukem-tools/ci.yml?branch=main&label=tests" alt="Tests"></a>
<a href="https://github.com/AlexGalhardo/vukem-tools/actions/workflows/release.yml"><img src="https://img.shields.io/github/actions/workflow/status/AlexGalhardo/vukem-tools/release.yml?label=ci%2Fcd" alt="CI/CD"></a>
<a href="https://github.com/AlexGalhardo/vukem-tools/releases"><img src="https://img.shields.io/github/v/release/AlexGalhardo/vukem-tools?include_prereleases&label=version" alt="Version"></a>
<a href="go.mod"><img src="https://img.shields.io/badge/go-1.27.4-00ADD8?logo=go&logoColor=white" alt="Go 1.27.4"></a>
<a href="LICENSE.md"><img src="https://img.shields.io/github/license/AlexGalhardo/vukem-tools" alt="License"></a>
<a href="https://scorecard.dev/viewer/?uri=github.com/AlexGalhardo/vukem-tools"><img src="https://img.shields.io/ossf-scorecard/github.com/AlexGalhardo/vukem-tools?label=openssf%20scorecard" alt="OpenSSF Scorecard"></a>

### Convert, compress and edit your files on your own machine, for you and for your AI agents

<br/>

</div>

<br/>

## Introduction

**Why upload your files to a different website for every conversion, when one tool you run yourself can do it all? This is why Vukem Tools exists.**

Vukem Tools is an open source (MIT), self-hosted file toolbox, on the web and on the desktop, built with Go and the [Vukem](https://vukem.dev) framework. It is a self-hosted alternative to the hosted file converters, and it is built around the problems that get in the way of working with files:

- **Your files are scattered across converter websites, and each one wants your upload.** One account on a server you run covers the whole [catalogue](https://tools.vukem.dev/docs/concepts/catalogue/):
  - Images: convert, compress, pixel art to SVG, and images to one PDF.
  - Video and audio: convert, compress, cut and crop, video to GIF, and download from YouTube, Instagram, X and TikTok.
  - PDF: merge, split, rotate, crop, compress, protect, unlock, OCR, fill forms and sign, with Word, PowerPoint and Excel to PDF. See the [PDF crop guide](https://tools.vukem.dev/docs/guide/pdf-crop/).
  - Archives and sprite sheets, and more than a dozen developer tools that run in your browser and never upload anything: JSON, YAML, CSV and XML, diff, hashes, JWT, UUID, Base64 and token estimates.
- **Your AI agent cannot use the tools you use.** Agents are first-class users: everything you do in the UI they can do through three surfaces:
  - [Agents overview](https://tools.vukem.dev/docs/agents/overview/).
  - The [CLI](https://tools.vukem.dev/docs/agents/cli/), the [HTTP API](https://tools.vukem.dev/docs/api/overview/) and the [MCP server](https://tools.vukem.dev/docs/mcp/overview/).
- **Your files and keys live in someone else's cloud.** Vukem Tools runs on your machine or on your own server:
  - [Installation](https://tools.vukem.dev/docs/getting-started/installation/) as a [desktop app](https://tools.vukem.dev/docs/guide/desktop/) or with [Docker](https://tools.vukem.dev/docs/self-hosting/docker/).
  - A result lives five minutes and is then deleted, and uploads are scanned for malware.
  - [Configuration](https://tools.vukem.dev/docs/reference/configuration/) is plain environment variables.
- **You want it in the chat you already use.** A Telegram bot serves the tools from a chat, and you sign in on the site, never in the chat.

> **Not for commercial use.** Several conversion engines are licensed under the GPL or the AGPL. Every engine, with its exact licence, is on the `/licenses` page and in the [licences guide](https://tools.vukem.dev/docs/self-hosting/licences/).

New here? Start with the [introduction](https://tools.vukem.dev/docs/getting-started/introduction/) and the [quick start](https://tools.vukem.dev/docs/getting-started/quick-start/).

## Stability

Versions follow SemVer. Vukem Tools is in `0.x`: a minor release may break. See the [changelog](CHANGELOG.md) and the [releases](https://github.com/AlexGalhardo/vukem-tools/releases).

## Contributing

To build from source you need Go (see `go.mod`) and [Bun](https://bun.com). Read the [contribution guide](CONTRIBUTING.md) to get started and [`AGENTS.md`](AGENTS.md) for how the team of agents works on this repository.

## Code of conduct

To keep the community welcoming to everyone, please review and follow the [Code of Conduct](CODE_OF_CONDUCT.md).

## Security vulnerabilities

If you discover a security vulnerability, please follow the [security policy](SECURITY.md) instead of opening a public issue.

## License

Vukem Tools is open-source software licensed under the [MIT license](LICENSE.md).
