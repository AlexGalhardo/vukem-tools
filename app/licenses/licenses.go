// Package licenses is what this service runs and under what terms.
//
// It exists because of an obligation, not as a courtesy. Several conversion
// engines are copyleft — Ghostscript is AGPL-3.0, Poppler and Calibre are GPL,
// and ffmpeg is built with --enable-gpl — and the AGPL in particular reaches
// anyone who makes the software available over a network. Listing the terms
// where a visitor can read them is part of complying with them.
//
// Every entry's SPDX identifier was verified against the project's own
// repository rather than taken from memory; docs/web-research.md records how
// and when. An entry whose licence could not be verified says so instead of
// guessing, because a wrong licence claim is worse than an admitted gap.
package licenses

// Component is one thing this service runs or links.
type Component struct {
	// Name is what the project calls itself.
	Name string `json:"name"`
	// SPDX is the licence identifier, or "unverified" when it could not be
	// confirmed from the source.
	SPDX string `json:"spdx"`
	// URL is the project's home, so a visitor can read the licence itself
	// rather than trust this page.
	URL string `json:"url"`
	// Role is what it does here, in a few words.
	Role string `json:"role"`
	// Copyleft marks a licence with obligations that reach whoever runs this.
	// It drives the notice at the top of the page, so it must not be set
	// decoratively.
	Copyleft bool `json:"copyleft"`
}

// Project describes this service's own licence.
func Project() Component {
	return Component{
		Name: "Vukem Tools",
		SPDX: "MIT",
		URL:  "https://github.com/AlexGalhardo/vukem-tools",
		Role: "This service",
	}
}

// components is everything shipped or planned, grouped loosely by what it is
// for. A component appears here before the feature that uses it ships: the
// ROADMAP's global gate requires it, and it is the only way the page can be
// true on the day a tool lands.
var components = []Component{
	{
		Name: "vukem",
		SPDX: "MIT",
		URL:  "https://github.com/AlexGalhardo/vukem",
		Role: "The web framework the whole backend runs on",
	},
	{
		Name: "Go",
		SPDX: "BSD-3-Clause",
		URL:  "https://go.dev",
		Role: "The language and standard library",
	},
	{
		Name: "React",
		SPDX: "MIT",
		URL:  "https://react.dev",
		Role: "The interface",
	},
	{
		Name: "Inertia.js",
		SPDX: "MIT",
		URL:  "https://inertiajs.com",
		Role: "Pages rendered by the server, navigated without a reload",
	},
	{
		Name: "Tailwind CSS",
		SPDX: "MIT",
		URL:  "https://tailwindcss.com",
		Role: "The styling",
	},
	{
		Name: "Base UI",
		SPDX: "MIT",
		URL:  "https://base-ui.com",
		Role: "Accessible interface primitives",
	},
	{
		Name: "mimetype",
		SPDX: "MIT",
		URL:  "https://github.com/gabriel-vasile/mimetype",
		Role: "Identifying an uploaded file from its bytes rather than its name",
	},
	{
		Name: "pgx",
		SPDX: "MIT",
		URL:  "https://github.com/jackc/pgx",
		Role: "The PostgreSQL driver, and the progress stream between processes",
	},
	{
		Name: "ClamAV",
		SPDX: "GPL-2.0-only",
		URL:  "https://www.clamav.net",
		Role: "Scanning every upload before it is stored",
	},
	{
		Name:     "FFmpeg",
		SPDX:     "GPL-2.0-or-later",
		URL:      "https://ffmpeg.org",
		Role:     "Video and audio conversion. Built with --enable-gpl, which makes the build GPL rather than LGPL",
		Copyleft: true,
	},
	{
		Name: "yt-dlp",
		SPDX: "Unlicense",
		URL:  "https://github.com/yt-dlp/yt-dlp",
		Role: "Fetching a video from a URL",
	},
	{
		Name: "libvips",
		SPDX: "LGPL-2.1-or-later",
		URL:  "https://www.libvips.org",
		Role: "Image conversion and resizing",
	},
	{
		Name:     "Ghostscript",
		SPDX:     "AGPL-3.0-or-later",
		URL:      "https://www.ghostscript.com",
		Role:     "PDF compression, rasterisation and PDF/A",
		Copyleft: true,
	},
	{
		Name: "pdfcpu",
		SPDX: "Apache-2.0",
		URL:  "https://github.com/pdfcpu/pdfcpu",
		Role: "PDF structure: merge, split, rotate, stamp",
	},
	{
		Name: "qpdf",
		SPDX: "Apache-2.0",
		URL:  "https://github.com/qpdf/qpdf",
		// It is the PDF domain's primary engine, not only its encryption one:
		// every structural operation goes through it, because it edits the
		// object graph instead of re-rendering the pages.
		Role: "PDF structure, encryption and repair: merge, split, reorder, rotate, crop, stamp",
	},
	{
		Name:     "Poppler",
		SPDX:     "GPL-2.0-or-later",
		URL:      "https://poppler.freedesktop.org",
		Role:     "Rendering PDF pages and extracting their text",
		Copyleft: true,
	},
	{
		Name: "OCRmyPDF",
		SPDX: "MPL-2.0",
		URL:  "https://github.com/ocrmypdf/OCRmyPDF",
		Role: "Adding a searchable text layer to a scanned PDF",
	},
	{
		Name: "Tesseract",
		SPDX: "Apache-2.0",
		URL:  "https://github.com/tesseract-ocr/tesseract",
		Role: "The OCR engine behind it",
	},
	{
		Name: "LibreOffice",
		SPDX: "MPL-2.0",
		URL:  "https://www.libreoffice.org",
		Role: "Office documents to PDF",
	},
}

// Components returns everything this service runs or links.
func Components() []Component { return append([]Component(nil), components...) }

// Copyleft returns only the components whose licence places obligations on
// whoever runs this service. It drives the notice the page leads with, so a
// visitor reads the consequence before the table.
func Copyleft() []Component {
	var out []Component

	for _, c := range components {
		if c.Copyleft {
			out = append(out, c)
		}
	}

	return out
}
