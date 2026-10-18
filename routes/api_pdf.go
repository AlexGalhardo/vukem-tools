package routes

import (
	"time"

	vukem "github.com/AlexGalhardo/vukem"
	"github.com/AlexGalhardo/vukem/api"
	"github.com/AlexGalhardo/vukem/auth"
	"github.com/AlexGalhardo/vukem/middleware"
	"github.com/AlexGalhardo/vukem/ratelimit"
	"github.com/AlexGalhardo/vukem/router"

	"github.com/AlexGalhardo/vukem-tools/app/accounts"
	"github.com/AlexGalhardo/vukem-tools/app/controllers/apiv1"
	"github.com/AlexGalhardo/vukem-tools/app/services"
	"github.com/AlexGalhardo/vukem-tools/internal/httpx"
)

// tagPDF groups the PDF operations in the OpenAPI document.
//
// Its own tag rather than Conversions: nineteen operations under one heading
// with the pixel and video tools would make the document unreadable, and
// somebody looking for "split a PDF" should find a section rather than scroll.
const tagPDF = "PDF"

// PDF registers the PDF submit routes (S6).
//
// It is a route file of its own rather than more lines in routes/conversions.go
// because the domains are built in parallel and one file of submit routes
// would make every tool a conflict with every other tool being added at the
// same time. It takes the same resolved pipeline: Resolve is idempotent, and
// the event hub must exist exactly once per process.
func PDF(c *services.Conversions) func(r *router.Router, app *vukem.App) error {
	return func(r *router.Router, app *vukem.App) error {
		if err := c.Resolve(app); err != nil {
			return err
		}

		acc, err := accounts.New(app)
		if err != nil {
			return err
		}

		limiters, err := app.RateLimiters()
		if err != nil {
			return err
		}

		h := apiv1.NewConversions(c.Pipeline(), c.Store(), c.Files(), c.Quota(), c.Hub())

		perIP := ratelimit.Middleware(limiters("api.ip", 120, time.Minute), ratelimit.ByIP)
		perToken := ratelimit.Middleware(limiters("api.token", 60, time.Minute), ratelimit.ByToken)
		// The same submit limiter name as the other conversion routes, so the
		// budget is "thirty conversions a minute" rather than thirty per
		// domain: a caller should not get more throughput by spreading work
		// across tools.
		submit := ratelimit.Middleware(limiters("api.submit", 30, time.Minute), ratelimit.ByToken)

		r.Group("/api/v1", func(g *router.Group) {
			g.Use(middleware.JSONOnly(), httpx.AcceptAPIKeyHeader, perIP)

			g.Group("", func(p *router.Group) {
				p.Use(auth.Required(acc.API), perToken)

				// One Register call per tool rather than a loop over a
				// table. api.Register is generic over the handler's input
				// and output types and infers both from the function, so a
				// handler cannot be carried through a slice as an `any`
				// without losing the types the OpenAPI schema is generated
				// from. The prose stays in pdfOp, which is where it is worth
				// reading; this block is the wiring.
				api.Register(p, "POST /tools/pdf-merge", pdfOp("pdf-merge"), h.PDFMerge).Use(submit)
				api.Register(p, "POST /tools/pdf-split", pdfOp("pdf-split"), h.PDFSplit).Use(submit)
				api.Register(p, "POST /tools/pdf-extract-pages",
					pdfOp("pdf-extract-pages"), h.PDFExtractPages).Use(submit)
				api.Register(p, "POST /tools/pdf-remove-pages",
					pdfOp("pdf-remove-pages"), h.PDFRemovePages).Use(submit)
				api.Register(p, "POST /tools/pdf-organise", pdfOp("pdf-organise"), h.PDFOrganise).Use(submit)
				api.Register(p, "POST /tools/pdf-rotate", pdfOp("pdf-rotate"), h.PDFRotate).Use(submit)
				api.Register(p, "POST /tools/pdf-crop", pdfOp("pdf-crop"), h.PDFCrop).Use(submit)
				api.Register(p, "POST /tools/images-to-pdf", pdfOp("images-to-pdf"), h.ImagesToPDF).Use(submit)
				api.Register(p, "POST /tools/pdf-to-image", pdfOp("pdf-to-image"), h.PDFToImage).Use(submit)
				api.Register(p, "POST /tools/pdf-to-pdfa", pdfOp("pdf-to-pdfa"), h.PDFToPDFA).Use(submit)
				api.Register(p, "POST /tools/pdf-to-text", pdfOp("pdf-to-text"), h.PDFToText).Use(submit)
				api.Register(p, "POST /tools/pdf-compress", pdfOp("pdf-compress"), h.PDFCompress).Use(submit)
				api.Register(p, "POST /tools/pdf-repair", pdfOp("pdf-repair"), h.PDFRepair).Use(submit)
				api.Register(p, "POST /tools/pdf-protect", pdfOp("pdf-protect"), h.PDFProtect).Use(submit)
				api.Register(p, "POST /tools/pdf-unlock", pdfOp("pdf-unlock"), h.PDFUnlock).Use(submit)
				api.Register(p, "POST /tools/pdf-compare", pdfOp("pdf-compare"), h.PDFCompare).Use(submit)
				api.Register(p, "POST /tools/pdf-watermark", pdfOp("pdf-watermark"), h.PDFWatermark).Use(submit)
				api.Register(p, "POST /tools/pdf-page-numbers",
					pdfOp("pdf-page-numbers"), h.PDFPageNumbers).Use(submit)
			})
		}).Name("api.v1.")

		return nil
	}
}

// pdfOp is the OpenAPI operation for a PDF tool.
//
// It panics on an unknown path, which is right for a wiring mistake: the
// paths come from the registrations above, in the same file, and a typo
// should fail the boot rather than publish an operation with no summary.
func pdfOp(path string) api.Op {
	for _, route := range pdfOperations() {
		if route.path == path {
			return api.Op{
				Name:        "tools." + route.path,
				Summary:     route.summary,
				Description: route.description,
				Tags:        []string{tagPDF},
				Security:    []string{SchemeBearer},
			}
		}
	}

	panic("routes: no PDF operation described for " + path)
}

// pdfRoute is one tool's documentation.
type pdfRoute struct {
	path        string
	summary     string
	description string
}

// pdfOperations is every PDF tool's documentation, in the order the generated
// document lists them:
// structure, then conversion, then optimisation, then security, then
// stamping — which is the order of ROADMAP § Phase S6 and the order of the
// tools page.
//
// The descriptions are the place the honest limits are written down, because
// this is what a client reads before deciding to use a tool. A merge that
// keeps only the first document's bookmarks, a Markdown conversion that is
// heuristics rather than layout reconstruction, an unlock that makes exactly
// one attempt: each of those is a thing somebody would otherwise discover
// afterwards.
func pdfOperations() []pdfRoute {
	return []pdfRoute{
		{
			path:    "pdf-merge",
			summary: "Join several PDFs into one",
			description: "The order of file_ids is the order of the pages. The first document's " +
				"bookmarks and every document's links are kept; the later documents' bookmarks " +
				"cannot be, because there is nowhere to graft them on. An encrypted input is " +
				"refused.",
		},
		{
			path:    "pdf-split",
			summary: "Split a PDF into several files",
			description: "Either every n pages or by the ranges you name. Every part is a result " +
				"of its own with its own expiry, so all of them have to be downloaded within the " +
				"result lifetime. The parts do not carry the original's bookmarks, because a " +
				"part holds a subset of the pages.",
		},
		{
			path:    "pdf-extract-pages",
			summary: "Keep only the pages you name",
			description: "Pages are named as 1-3,5,8-z, where z is the last page. A page the " +
				"document does not have is a validation error rather than a page quietly " +
				"skipped. The bookmarks are dropped, because some would point at pages that are " +
				"no longer there.",
		},
		{
			path:    "pdf-remove-pages",
			summary: "Remove the pages you name",
			description: "The inverse of extracting: the pages named are removed and the rest " +
				"keep their order. Removing every page is refused.",
		},
		{
			path:    "pdf-organise",
			summary: "Reorder or duplicate pages",
			description: "The page list is the new order, and a page may appear twice. A list " +
				"that is a pure reordering keeps the bookmarks, because every page is still " +
				"there; one that drops a page does not.",
		},
		{
			path:    "pdf-rotate",
			summary: "Rotate pages by 90, 180 or 270 degrees",
			description: "By default the rotation is added to each page's own; set absolute to " +
				"replace it. Nothing else about the document changes: the bookmarks, the links " +
				"and the page content are untouched.",
		},
		{
			path:    "pdf-crop",
			summary: "Trim a PDF's margins",
			description: "Sets the crop box, which is what a reader displays and prints. The " +
				"trimmed content is hidden rather than deleted, so the crop can be undone, and " +
				"everything else about the document is left alone.",
		},
		{
			path:    "images-to-pdf",
			summary: "Bind images into a PDF, one per page",
			description: "A JPEG input is embedded exactly as uploaded, re-encoded zero times. " +
				"Anything else is converted to JPEG once, with transparency flattened onto " +
				"white. The order of file_ids is the page order.",
		},
		{
			path:    "pdf-to-image",
			summary: "Render pages to PNG or JPEG",
			description: "One image per page, at 36 to 600 DPI. Pages are rendered from the crop " +
				"box, so the image matches what a reader shows rather than the untrimmed sheet.",
		},
		{
			path:    "pdf-to-pdfa",
			summary: "Convert to the archival PDF/A format",
			description: "Targets PDF/A-1b, 2b or 3b. Content is kept in preference to " +
				"conformance: a document that cannot be made conformant comes back with its " +
				"pages intact and the converter's own list of what is wrong, rather than " +
				"conformant with content silently removed.",
		},
		{
			path:    "pdf-to-text",
			summary: "Extract a PDF's text, as text or Markdown",
			description: "The Markdown form recognises numbered and capitalised headings, " +
				"bulleted and numbered lists, and joins the extractor's hard-wrapped lines back " +
				"into paragraphs. It is a text conversion with heuristics, not a layout " +
				"reconstruction: a document whose headings are marked only by font size comes " +
				"out as paragraphs, and a table comes out as spaced text. A scanned document " +
				"has no text to extract.",
		},
		{
			path:    "pdf-compress",
			summary: "Make a PDF smaller",
			description: "Three levels, which differ in the resolution images are downsampled " +
				"to: 300, 150 or 96 DPI. A document that is already smaller than the compressed " +
				"version is returned unchanged rather than re-encoded into something larger, and " +
				"the conversion says which happened.",
		},
		{
			path:    "pdf-repair",
			summary: "Rebuild a damaged PDF's structure",
			description: "Best effort, and honest about the limit: it rebuilds the " +
				"cross-reference table and the object graph, not content the file no longer " +
				"holds. A file it cannot rebuild into a readable document is refused rather than " +
				"returned as a document that opens empty.",
		},
		{
			path:    "pdf-protect",
			summary: "Encrypt a PDF with a password",
			description: "AES-256, with the printing, modification and extraction permissions " +
				"you choose. The owner password is required, because permissions without one can " +
				"be removed by any PDF tool. Nobody, including this service, can recover a " +
				"password you lose.",
		},
		{
			path:    "pdf-unlock",
			summary: "Remove a password you know",
			description: "The password you supply is tried exactly once. There is no recovery of " +
				"a password you do not have and no brute-force path in the code. A wrong " +
				"password is refused with one message that does not say whether the document had " +
				"a user password, an owner password or both.",
		},
		{
			path:    "pdf-compare",
			summary: "Compare two PDFs' text, page by page",
			description: "Exactly two uploads, the earlier document first. The result is a " +
				"Markdown report of the lines that differ on each page. It compares text: a " +
				"change to a font, a margin or a picture does not change the text, so it does " +
				"not appear.",
		},
		{
			path:    "pdf-watermark",
			summary: "Stamp text across a PDF's pages",
			description: "Position, size, angle, opacity and ink are all yours to set. The text " +
				"is limited to unaccented Latin letters, digits and punctuation, because the " +
				"standard PDF fonts are the only ones stamped without embedding a font file. A " +
				"watermark is a visible mark, not protection.",
		},
		{
			path:    "pdf-page-numbers",
			summary: "Stamp page numbers onto a PDF",
			description: "The label is a template: {n} is the number and {total} the count, so " +
				"\"Page {n} of {total}\" and \"- {n} -\" both work. Numbering can start at any " +
				"number and cover a page range, which is what a document with a cover page " +
				"needs.",
		},
	}
}
