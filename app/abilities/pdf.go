package abilities

// The PDF abilities (S6), one per group of tools on the tools page rather
// than one per tool.
//
// Nineteen abilities for nineteen PDF tools would be a key-minting form
// nobody reads, and the grouping is not arbitrary: it is the line along which
// somebody would actually want to limit a key. A script that splits invoices
// has no business encrypting anything, and the reverse is also true — so
// structure, conversion, security and editing are separate, while the two
// optimisation tools share the ability the roadmap already named.
const (
	// PDFStructure is merge, split, extract, remove, organise, rotate and
	// crop: operations that rearrange pages without changing them.
	PDFStructure = "pdf:structure"
	// PDFConvert is converting to and from PDF: images in, and images, text
	// or PDF/A out.
	PDFConvert = "pdf:convert"
	// PDFSecurity is encryption, password removal and comparison. It is the
	// one worth withholding: a key with it can decrypt any document its
	// owner has the password for.
	PDFSecurity = "pdf:security"
	// PDFEdit is stamping: watermarks and page numbers.
	PDFEdit = "pdf:edit"
)

// pdfAbilities is this domain's contribution to the closed set.
//
// It is a slice of its own so the PDF domain can be built without editing the
// list every other domain is also adding to; `all` concatenates them.
// PDFCompress is not here: it is declared with the original set and covers
// both optimisation tools, compression and repair. They are the same group on
// the tools page and nobody would grant one without the other.
var pdfAbilities = []string{
	PDFStructure,
	PDFConvert,
	PDFSecurity,
	PDFEdit,
}
