package catalog

// pdfCatalog is the PDF domain (S6).
//
// It is iLovePDF's published catalogue, tool for tool, with a decision
// against each one. The skipped entries are here on purpose and carry their
// reason: a visitor comparing this service with theirs should find the answer
// on the page rather than search for a tool, find nothing and assume it was
// forgotten. Every reason below is the one recorded in ROADMAP § Phase S6.
//
// A catalogue entry is written when the tool is planned, not when it ships:
// Status says which, and catalog.Grouped demotes a Live entry whose tool is
// not actually registered, so this file cannot advertise something that does
// not run.
var pdfCatalog = []Tool{
	// --- S6.1 Structure: take all ---
	{
		Name:     "pdf-merge",
		Title:    "Merge PDFs",
		Summary:  "Joins several PDFs into one, in the order you arrange them. Keeps the first document's bookmarks and every document's links.",
		In:       []string{"PDF"},
		Out:      []string{"PDF"},
		Group:    GroupDocuments,
		Icon:     "document",
		Status:   Live,
		Keywords: []string{"join", "combine", "append", "ilovepdf"},
	},
	{
		Name:     "pdf-split",
		Title:    "Split a PDF",
		Summary:  "Breaks a PDF into separate files, either every n pages or by the ranges you name. Each part has to be downloaded before the result expires.",
		In:       []string{"PDF"},
		Out:      []string{"PDF"},
		Group:    GroupDocuments,
		Icon:     "document",
		Status:   Live,
		Keywords: []string{"divide", "separate", "pages", "ilovepdf"},
	},
	{
		Name:     "pdf-extract-pages",
		Title:    "Extract pages",
		Summary:  "Pulls the pages you name into one new PDF. The bookmarks are dropped, because some would point at pages that are no longer there.",
		In:       []string{"PDF"},
		Out:      []string{"PDF"},
		Group:    GroupDocuments,
		Icon:     "document",
		Status:   Live,
		Keywords: []string{"subset", "pick", "select", "ilovepdf"},
	},
	{
		Name:     "pdf-remove-pages",
		Title:    "Remove pages",
		Summary:  "Deletes the pages you name and keeps the rest, in their original order.",
		In:       []string{"PDF"},
		Out:      []string{"PDF"},
		Group:    GroupDocuments,
		Icon:     "document",
		Status:   Live,
		Keywords: []string{"delete", "drop", "pages", "ilovepdf"},
	},
	{
		Name:     "pdf-organise",
		Title:    "Organise a PDF",
		Summary:  "Reorders and duplicates pages from one page list. A pure reordering keeps the bookmarks, because every page is still there.",
		In:       []string{"PDF"},
		Out:      []string{"PDF"},
		Group:    GroupDocuments,
		Icon:     "document",
		Status:   Live,
		Keywords: []string{"reorder", "rearrange", "sort", "organize", "ilovepdf"},
	},
	{
		Name:     "pdf-rotate",
		Title:    "Rotate a PDF",
		Summary:  "Turns pages by 90, 180 or 270 degrees, all of them or the ones you name. Nothing else about the document changes.",
		In:       []string{"PDF"},
		Out:      []string{"PDF"},
		Group:    GroupDocuments,
		Icon:     "document",
		Status:   Live,
		Keywords: []string{"turn", "sideways", "landscape", "ilovepdf"},
	},
	{
		Name:     "pdf-crop",
		Title:    "Crop a PDF",
		Summary:  "Trims the margins by setting the crop box. The content is hidden rather than deleted, so the crop can be undone.",
		In:       []string{"PDF"},
		Out:      []string{"PDF"},
		Group:    GroupDocuments,
		Icon:     "document",
		Status:   Live,
		Keywords: []string{"margins", "trim", "crop box", "ilovepdf"},
	},

	// --- S6.2 Convert to PDF: take all but HTML ---
	{
		Name:     "images-to-pdf",
		Title:    "Images to PDF",
		Summary:  "Binds JPEG, PNG, WebP, TIFF and AVIF into one PDF, one image per page. A JPEG is embedded without being re-encoded.",
		In:       []string{"JPEG", "PNG", "WebP", "TIFF", "AVIF", "GIF", "BMP"},
		Out:      []string{"PDF"},
		Group:    GroupDocuments,
		Icon:     "image",
		Status:   Live,
		Keywords: []string{"jpg to pdf", "png to pdf", "scan", "photos", "ilovepdf"},
	},
	{
		Name:     "word-to-pdf",
		Title:    "Word to PDF",
		Summary:  "Converting .docx and .doc to PDF.",
		In:       []string{"DOCX", "DOC"},
		Out:      []string{"PDF"},
		Group:    GroupDocuments,
		Icon:     "document",
		Status:   Planned,
		Keywords: []string{"docx", "doc", "office", "ilovepdf"},
	},
	{
		Name:     "powerpoint-to-pdf",
		Title:    "PowerPoint to PDF",
		Summary:  "Converting .pptx and .ppt to PDF.",
		In:       []string{"PPTX", "PPT"},
		Out:      []string{"PDF"},
		Group:    GroupDocuments,
		Icon:     "document",
		Status:   Planned,
		Keywords: []string{"pptx", "ppt", "slides", "office", "ilovepdf"},
	},
	{
		Name:     "excel-to-pdf",
		Title:    "Excel to PDF",
		Summary:  "Converting .xlsx and .xls to PDF.",
		In:       []string{"XLSX", "XLS"},
		Out:      []string{"PDF"},
		Group:    GroupDocuments,
		Icon:     "document",
		Status:   Planned,
		Keywords: []string{"xlsx", "xls", "spreadsheet", "office", "ilovepdf"},
	},
	{
		Name:    "html-to-pdf",
		Title:   "HTML to PDF",
		Summary: "Rendering a web page to PDF.",
		In:      []string{"URL", "HTML"},
		Out:     []string{"PDF"},
		Group:   GroupDocuments,
		Icon:    "document",
		Status:  Skipped,
		Reason: "It needs a headless Chromium in the image, which roughly doubles its size, plus a " +
			"sandbox and an egress policy for a tool whose whole job is fetching URLs a user " +
			"supplies. That is a server-side request forgery surface wrapped in a 400 MB " +
			"dependency. It will be reconsidered only with a dedicated, network-isolated worker.",
		Keywords: []string{"url", "web page", "chromium", "ilovepdf"},
	},

	// --- S6.3 Convert from PDF: take the faithful ones ---
	{
		Name:     "pdf-to-image",
		Title:    "PDF to JPG or PNG",
		Summary:  "Renders each page to an image at the resolution you choose, from 36 to 600 DPI.",
		In:       []string{"PDF"},
		Out:      []string{"PNG", "JPEG"},
		Group:    GroupDocuments,
		Icon:     "image",
		Status:   Live,
		Keywords: []string{"pdf to jpg", "pdf to png", "render", "dpi", "ilovepdf"},
	},
	{
		Name:     "pdf-to-pdfa",
		Title:    "PDF to PDF/A",
		Summary:  "Converts to the archival format, PDF/A-1b, 2b or 3b, and reports the converter's own conformance verdict rather than assuming it.",
		In:       []string{"PDF"},
		Out:      []string{"PDF/A"},
		Group:    GroupDocuments,
		Icon:     "document",
		Status:   Live,
		Keywords: []string{"archive", "pdfa", "pdf/a", "long term", "ilovepdf"},
	},
	{
		Name:     "pdf-to-text",
		Title:    "PDF to text or Markdown",
		Summary:  "Extracts the text, optionally as Markdown with headings and lists recognised. It is a text conversion, not a layout reconstruction.",
		In:       []string{"PDF"},
		Out:      []string{"TXT", "MD"},
		Group:    GroupDocuments,
		Icon:     "document",
		Status:   Live,
		Keywords: []string{"markdown", "txt", "extract text", "ilovepdf"},
	},
	{
		Name:    "pdf-to-word",
		Title:   "PDF to Word",
		Summary: "Converting a PDF into an editable document.",
		In:      []string{"PDF"},
		Out:     []string{"DOCX"},
		Group:   GroupDocuments,
		Icon:    "document",
		Status:  Skipped,
		Reason: "No open-source converter produces a .docx that is both faithful and editable. " +
			"LibreOffice imports a PDF as absolutely positioned text boxes, and saving that as " +
			"Word gives a file that opens and cannot be edited. Shipping it would create support, " +
			"not value. Use PDF to text or Markdown instead.",
		Keywords: []string{"docx", "editable", "ilovepdf"},
	},
	{
		Name:    "pdf-to-powerpoint",
		Title:   "PDF to PowerPoint",
		Summary: "Converting a PDF into editable slides.",
		In:      []string{"PDF"},
		Out:     []string{"PPTX"},
		Group:   GroupDocuments,
		Icon:    "document",
		Status:  Skipped,
		Reason: "The same problem as PDF to Word, and worse: slide semantics — a title, a bullet " +
			"list, a layout — cannot be recovered from page images. The result would be one " +
			"picture per slide, which is not editable slides.",
		Keywords: []string{"pptx", "slides", "ilovepdf"},
	},
	{
		Name:    "pdf-to-excel",
		Title:   "PDF to Excel",
		Summary: "Extracting tables from a PDF into a spreadsheet.",
		In:      []string{"PDF"},
		Out:     []string{"XLSX"},
		Group:   GroupDocuments,
		Icon:    "document",
		Status:  Skipped,
		Reason: "Table extraction good enough to trust needs Camelot or Tabula, which mean Python " +
			"and a JVM, and both still fail on merged cells and borderless tables. A spreadsheet " +
			"that is subtly wrong is worse than no spreadsheet, because nobody checks the cells " +
			"they did not expect to be wrong.",
		Keywords: []string{"xlsx", "tables", "spreadsheet", "ilovepdf"},
	},

	// --- S6.4 Optimise: take all ---
	{
		Name:     "pdf-compress",
		Title:    "Compress a PDF",
		Summary:  "Makes a PDF smaller at one of three levels, and returns it unchanged rather than larger when it is already optimised.",
		In:       []string{"PDF"},
		Out:      []string{"PDF"},
		Group:    GroupDocuments,
		Icon:     "compress",
		Status:   Live,
		Keywords: []string{"smaller", "reduce", "ghostscript", "ilovepdf"},
	},
	{
		Name:     "pdf-repair",
		Title:    "Repair a PDF",
		Summary:  "Rebuilds a damaged PDF's structure. It recovers the structure, not content the file no longer holds, and it says so rather than returning a document that opens empty.",
		In:       []string{"PDF"},
		Out:      []string{"PDF"},
		Group:    GroupDocuments,
		Icon:     "document",
		Status:   Live,
		Keywords: []string{"fix", "corrupt", "damaged", "recover", "ilovepdf"},
	},
	{
		Name:     "pdf-ocr",
		Title:    "OCR a PDF",
		Summary:  "Adding a searchable text layer to a scanned PDF.",
		In:       []string{"PDF"},
		Out:      []string{"PDF"},
		Group:    GroupDocuments,
		Icon:     "document",
		Status:   Planned,
		Keywords: []string{"ocr", "scanned", "searchable", "tesseract", "ilovepdf"},
	},

	// --- S6.5 Security: take most ---
	{
		Name:     "pdf-protect",
		Title:    "Protect a PDF",
		Summary:  "Encrypts with AES-256 and sets what a reader may print, change and copy. Nobody, including this service, can recover a password you lose.",
		In:       []string{"PDF"},
		Out:      []string{"PDF"},
		Group:    GroupDocuments,
		Icon:     "key",
		Status:   Live,
		Keywords: []string{"password", "encrypt", "permissions", "aes", "ilovepdf"},
	},
	{
		Name:     "pdf-unlock",
		Title:    "Unlock a PDF",
		Summary:  "Removes a password you already know. It tries that password once; there is no recovery of a password you do not have, and no code path that guesses.",
		In:       []string{"PDF"},
		Out:      []string{"PDF"},
		Group:    GroupDocuments,
		Icon:     "key",
		Status:   Live,
		Keywords: []string{"password", "decrypt", "remove password", "ilovepdf"},
	},
	{
		Name:     "pdf-compare",
		Title:    "Compare two PDFs",
		Summary:  "A page-aligned difference of the two documents' text. A change to a font, a margin or a picture does not change the text, so it does not appear.",
		In:       []string{"PDF"},
		Out:      []string{"MD"},
		Group:    GroupDocuments,
		Icon:     "document",
		Status:   Live,
		Keywords: []string{"diff", "difference", "changes", "versions", "ilovepdf"},
	},
	{
		Name:    "pdf-sign",
		Title:   "Sign a PDF",
		Summary: "Applying a cryptographic signature to a document.",
		In:      []string{"PDF"},
		Out:     []string{"PDF"},
		Group:   GroupDocuments,
		Icon:    "key",
		Status:  Skipped,
		Reason: "A real signature means holding your private key, or proxying a certificate " +
			"authority. Key custody for other people is a liability this project will not take " +
			"on, and a signature that is not legally meaningful is theatre. Use the watermark " +
			"tool for a visible stamp.",
		Keywords: []string{"signature", "certificate", "esign", "ilovepdf"},
	},
	{
		Name:    "pdf-redact",
		Title:   "Redact a PDF",
		Summary: "Permanently removing text and images from a document.",
		In:      []string{"PDF"},
		Out:     []string{"PDF"},
		Group:   GroupDocuments,
		Icon:    "key",
		Status:  Skipped,
		Reason: "True redaction removes the content, not just covers it. Drawing a black rectangle " +
			"over text that can still be copied out is a security failure dressed as a feature, " +
			"and it is the most dangerous thing on this list to get wrong. It will only ship with " +
			"content-stream editing and a test that searches the output for the redacted text.",
		Keywords: []string{"redact", "black out", "censor", "ilovepdf"},
	},

	// --- S6.6 Edit: mostly skipped, deliberately ---
	{
		Name:     "pdf-page-numbers",
		Title:    "Add page numbers",
		Summary:  "Stamps page numbers in the position and format you choose, over the whole document or a range, starting at any number.",
		In:       []string{"PDF"},
		Out:      []string{"PDF"},
		Group:    GroupDocuments,
		Icon:     "document",
		Status:   Live,
		Keywords: []string{"numbering", "folio", "page x of y", "ilovepdf"},
	},
	{
		Name:     "pdf-watermark",
		Title:    "Add a watermark",
		Summary:  "Stamps text across pages, at the angle, size and opacity you choose. A watermark is a visible mark, not protection.",
		In:       []string{"PDF"},
		Out:      []string{"PDF"},
		Group:    GroupDocuments,
		Icon:     "document",
		Status:   Live,
		Keywords: []string{"draft", "confidential", "stamp", "ilovepdf"},
	},
	{
		Name:     "pdf-forms",
		Title:    "PDF forms",
		Summary:  "Listing a form's fields, filling them from JSON and flattening the result.",
		In:       []string{"PDF"},
		Out:      []string{"PDF", "JSON"},
		Group:    GroupDocuments,
		Icon:     "document",
		Status:   Planned,
		Keywords: []string{"form", "acroform", "fill", "flatten", "ilovepdf"},
	},
	{
		Name:    "pdf-edit",
		Title:   "Edit a PDF",
		Summary: "Adding text, images and shapes to a page on a canvas.",
		In:      []string{"PDF"},
		Out:     []string{"PDF"},
		Group:   GroupDocuments,
		Icon:    "document",
		Status:  Skipped,
		Reason: "Adding text, images and shapes on a canvas is an application, not a conversion: " +
			"it needs a client-side PDF renderer, a content-stream editor and a document model " +
			"with undo. It does not belong in a pipeline whose unit of work is one file in, one " +
			"file out, and half of it would be worse than none.",
		Keywords: []string{"editor", "annotate", "draw", "ilovepdf"},
	},
	{
		Name:    "scan-to-pdf",
		Title:   "Scan to PDF",
		Summary: "Capturing a document with a phone camera.",
		In:      []string{"Camera"},
		Out:     []string{"PDF"},
		Group:   GroupDocuments,
		Icon:    "image",
		Status:  Skipped,
		Reason: "It is a phone camera feature: capture, edge detection and perspective correction " +
			"all happen on the device. The useful half is already here as Images to PDF — " +
			"photograph the pages and upload them.",
		Keywords: []string{"scan", "camera", "phone", "ilovepdf"},
	},

	// --- S6.7 Not in scope: AI tools ---
	{
		Name:    "pdf-summarise",
		Title:   "Summarise a PDF with AI",
		Summary: "Producing a summary of a document with a language model.",
		In:      []string{"PDF"},
		Out:     []string{"TXT"},
		Group:   GroupDocuments,
		Icon:    "document",
		Status:  Skipped,
		Reason: "It needs a language model. A self-hosted toolbox cannot ship one of a useful " +
			"size, and routing your document to somebody else's API contradicts the point of " +
			"self-hosting. If it is ever added it will be behind an explicit, per-user opt-in " +
			"with the provider named on this page.",
		Keywords: []string{"ai", "summary", "llm", "ilovepdf"},
	},
	{
		Name:    "pdf-translate",
		Title:   "Translate a PDF",
		Summary: "Translating a document into another language.",
		In:      []string{"PDF"},
		Out:     []string{"PDF"},
		Group:   GroupDocuments,
		Icon:    "document",
		Status:  Skipped,
		Reason: "The same reason as summarising with AI: it needs a language model this service " +
			"cannot host and will not send your document to without asking.",
		Keywords: []string{"ai", "translate", "language", "llm", "ilovepdf"},
	},
}
