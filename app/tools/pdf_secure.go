package tools

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/AlexGalhardo/vukem/z"

	"github.com/AlexGalhardo/vukem-tools/app/abilities"
	"github.com/AlexGalhardo/vukem-tools/internal/jobs"
	"github.com/AlexGalhardo/vukem-tools/internal/pdf"
)

// ---------------------------------------------------------------------------
// S6.4 Optimise, S6.5 Security
// ---------------------------------------------------------------------------

// PDFCompressOptions is the input of a compression.
type PDFCompressOptions struct {
	// Level is light, balanced or strong.
	Level string `json:"level" validate:"trim,max=16" doc:"light keeps every page's appearance, balanced is the default, strong is for email."`
}

// Normalise implements the option contract.
func (o PDFCompressOptions) Normalise() (PDFCompressOptions, error) {
	if o.Level = strings.ToLower(strings.TrimSpace(o.Level)); o.Level == "" {
		o.Level = string(pdf.CompressBalanced)
	}

	if !pdf.CompressLevel(o.Level).Valid() {
		return PDFCompressOptions{}, z.FieldError("options.level",
			"The level is one of: "+strings.Join(pdf.CompressLevels(), ", ")+".")
	}

	return o, nil
}

// PDFRepairOptions is the input of a repair. There is nothing to choose: a
// repair either works or it does not.
type PDFRepairOptions struct{}

// Normalise implements the option contract.
func (o PDFRepairOptions) Normalise() (PDFRepairOptions, error) { return o, nil }

// PDFProtectOptions is the input of an encryption.
type PDFProtectOptions struct {
	// UserPassword opens the document. Empty leaves it openable by anybody
	// and enforces only the permissions.
	UserPassword string `json:"user_password" validate:"max=256" doc:"The password needed to open the document. Leave empty to restrict permissions only."`
	// OwnerPassword lifts the restrictions. Required.
	OwnerPassword string `json:"owner_password" validate:"required,max=256" doc:"The password that lifts the restrictions. Required, and must differ from the opening password."`
	// Print is full, low or none.
	Print string `json:"print" validate:"trim,max=8" doc:"full, low or none. Default full."`
	// Modify is all, annotate, form, assembly or none.
	Modify string `json:"modify" validate:"trim,max=16" doc:"all, annotate, form, assembly or none. Default all."`
	// AllowExtract allows copying text out.
	AllowExtract bool `json:"allow_extract" doc:"Allow text to be copied out. Screen-reader access is always allowed."`
}

// Normalise implements the option contract.
//
// The passwords are not trimmed. A password is bytes the user chose, and
// silently removing a leading space would mean the document could not be
// opened with the password they think they set.
func (o PDFProtectOptions) Normalise() (PDFProtectOptions, error) {
	if o.OwnerPassword == "" {
		return PDFProtectOptions{}, z.FieldError("options.owner_password",
			"Set an owner password. Without one the restrictions can be removed by any PDF tool, "+
				"including this one.")
	}

	if strings.ContainsAny(o.OwnerPassword, "\r\n") || strings.ContainsAny(o.UserPassword, "\r\n") {
		return PDFProtectOptions{}, z.FieldError("options.owner_password",
			"A password may not contain a line break.")
	}

	if o.UserPassword != "" && o.UserPassword == o.OwnerPassword {
		return PDFProtectOptions{}, z.FieldError("options.owner_password",
			"The owner password must differ from the one that opens the document, or anybody who "+
				"can open it can also lift the restrictions.")
	}

	if o.Print = strings.ToLower(strings.TrimSpace(o.Print)); o.Print == "" {
		o.Print = string(pdf.PrintFull)
	}

	if !pdf.Printing(o.Print).Valid() {
		return PDFProtectOptions{}, z.FieldError("options.print",
			"Printing is one of: "+strings.Join(pdf.Printings(), ", ")+".")
	}

	if o.Modify = strings.ToLower(strings.TrimSpace(o.Modify)); o.Modify == "" {
		o.Modify = string(pdf.ModifyAll)
	}

	if !pdf.Modification(o.Modify).Valid() {
		return PDFProtectOptions{}, z.FieldError("options.modify",
			"Modification is one of: "+strings.Join(pdf.Modifications(), ", ")+".")
	}

	return o, nil
}

// PDFUnlockOptions is the input of a password removal.
type PDFUnlockOptions struct {
	// Password is the one that opens the document. There is no other source
	// for it: this tool removes a password you know and does not attempt to
	// recover one it was not given.
	Password string `json:"password" validate:"required,max=256" doc:"The password that opens the document. It is tried once; there is no recovery of a password you do not have."`
}

// Normalise implements the option contract.
func (o PDFUnlockOptions) Normalise() (PDFUnlockOptions, error) {
	if o.Password == "" {
		return PDFUnlockOptions{}, z.FieldError("options.password",
			"Enter the password that opens the document.")
	}

	if len(o.Password) > pdf.MaxPasswordBytes {
		return PDFUnlockOptions{}, z.FieldError("options.password",
			"A password may be at most "+strconv.Itoa(pdf.MaxPasswordBytes)+" characters.")
	}

	return o, nil
}

// PDFCompareOptions is the input of a comparison. There is nothing to choose:
// the two documents are the ones attached, in order.
type PDFCompareOptions struct{}

// Normalise implements the option contract.
func (o PDFCompareOptions) Normalise() (PDFCompareOptions, error) { return o, nil }

// pdfOptimiseTools make a document smaller or readable again (S6.4).
func pdfOptimiseTools(deps *pdfDeps) []jobs.Tool {
	return []jobs.Tool{
		&pdfTool[PDFCompressOptions]{
			deps: deps, name: NamePDFCompress, ability: abilities.PDFCompress,
			timeout: pdfGhostTimeout, accepted: pdfInputTypes,
			engines: []string{pdf.EngineQPDF, pdf.EngineGhostscript}, maxInputs: 1,
			work: compressPDF,
		},
		&pdfTool[PDFRepairOptions]{
			deps: deps, name: NamePDFRepair, ability: abilities.PDFCompress,
			timeout: pdfStructureTimeout, accepted: pdfInputTypes,
			engines: []string{pdf.EngineQPDF}, maxInputs: 1,
			work: repairPDF,
		},
	}
}

// pdfSecurityTools protect, unlock and compare (S6.5).
func pdfSecurityTools(deps *pdfDeps) []jobs.Tool {
	return []jobs.Tool{
		&pdfTool[PDFProtectOptions]{
			deps: deps, name: NamePDFProtect, ability: abilities.PDFSecurity,
			timeout: pdfStructureTimeout, accepted: pdfInputTypes,
			engines: []string{pdf.EngineQPDF}, maxInputs: 1,
			work: protectPDF,
		},
		&pdfTool[PDFUnlockOptions]{
			deps: deps, name: NamePDFUnlock, ability: abilities.PDFSecurity,
			timeout: pdfStructureTimeout, accepted: pdfInputTypes,
			engines: []string{pdf.EngineQPDF}, maxInputs: 1,
			work: unlockPDF,
		},
		&pdfTool[PDFCompareOptions]{
			deps: deps, name: NamePDFCompare, ability: abilities.PDFSecurity,
			timeout: pdfStampTimeout, accepted: pdfInputTypes,
			engines: []string{pdf.EngineQPDF, pdf.EnginePDFToText}, minInputs: 2, maxInputs: 2,
			work: comparePDFs,
		},
	}
}

// compressPDF makes a document smaller, or returns it unchanged.
func compressPDF(ctx context.Context, w pdfWork[PDFCompressOptions]) (pdfResult, error) {
	in := w.Inputs[0]

	doc, err := w.Engines.Inspect(ctx, w.Dir, in.Path)
	if err != nil {
		return pdfResult{}, err
	}

	out := filepath.Join(w.Dir, "compressed.pdf")

	result, err := w.Engines.Compress(ctx, pdf.CompressRequest{
		Dir:      w.Dir,
		Input:    in.Path,
		Output:   out,
		Level:    pdf.CompressLevel(w.Options.Level),
		Pages:    doc.Count,
		Progress: func(page, total int) { _ = w.Report(page*100/max(total, 1), "compressing") },
	})
	if err != nil {
		return pdfResult{}, err
	}

	message := "This document was already as small as this tool can make it, so it is returned " +
		"unchanged rather than re-encoded into something larger."

	if result.Returned {
		message = "Reduced from " + humanBytes(result.Before) + " to " + humanBytes(result.After) +
			", saving " + strconv.Itoa(percentSaved(result)) + " per cent."
	}

	return pdfResult{
		Outputs: []pdfOutput{{Path: out, Name: in.Stem() + "-compressed.pdf", MIME: mimePDF}},
		Message: message,
	}, nil
}

// percentSaved is how much of the file the compression removed, rounded down.
func percentSaved(result pdf.CompressResult) int {
	if result.Before <= 0 {
		return 0
	}

	return int(result.Saved() * 100 / result.Before)
}

// humanBytes renders a size the way a person reads one.
func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return strconv.FormatFloat(float64(n)/(1<<20), 'f', 1, 64) + " MB"
	case n >= 1<<10:
		return strconv.FormatInt(n/(1<<10), 10) + " kB"
	default:
		return strconv.FormatInt(n, 10) + " bytes"
	}
}

// repairPDF rebuilds a document's structure.
func repairPDF(ctx context.Context, w pdfWork[PDFRepairOptions]) (pdfResult, error) {
	in := w.Inputs[0]
	out := filepath.Join(w.Dir, "repaired.pdf")

	result, err := w.Engines.Repair(ctx, pdf.RepairRequest{Dir: w.Dir, Input: in.Path, Output: out})
	if err != nil {
		return pdfResult{}, err
	}

	message := "The document was readable already; it has been rewritten cleanly, with " +
		strconv.Itoa(result.Pages) + " pages."

	if result.Rebuilt {
		message = "Rebuilt the document's structure and recovered " + strconv.Itoa(result.Pages) +
			" pages. A repair recovers the structure, not content the file no longer holds: " +
			"pages whose data was lost cannot come back."
	}

	return pdfResult{
		Outputs: []pdfOutput{{Path: out, Name: in.Stem() + "-repaired.pdf", MIME: mimePDF}},
		Message: message,
	}, nil
}

// protectPDF encrypts a document.
func protectPDF(ctx context.Context, w pdfWork[PDFProtectOptions]) (pdfResult, error) {
	in := w.Inputs[0]
	out := filepath.Join(w.Dir, "protected.pdf")

	err := w.Engines.Protect(ctx, pdf.ProtectRequest{
		Dir:           w.Dir,
		Input:         in.Path,
		Output:        out,
		UserPassword:  w.Options.UserPassword,
		OwnerPassword: w.Options.OwnerPassword,
		Print:         pdf.Printing(w.Options.Print),
		Modify:        pdf.Modification(w.Options.Modify),
		AllowExtract:  w.Options.AllowExtract,
	})
	if err != nil {
		return pdfResult{}, err
	}

	message := "Encrypted with AES-256. The restrictions are enforced by the reader, not by the " +
		"file: a reader that chooses to ignore them can."

	if w.Options.UserPassword != "" {
		message = "Encrypted with AES-256. The document cannot be opened without the password, " +
			"and nobody — including this service — can recover it for you."
	}

	return pdfResult{
		Outputs: []pdfOutput{{Path: out, Name: in.Stem() + "-protected.pdf", MIME: mimePDF}},
		Message: message,
	}, nil
}

// unlockPDF removes a password the user supplied.
func unlockPDF(ctx context.Context, w pdfWork[PDFUnlockOptions]) (pdfResult, error) {
	in := w.Inputs[0]
	out := filepath.Join(w.Dir, "unlocked.pdf")

	err := w.Engines.Unlock(ctx, pdf.UnlockRequest{
		Dir: w.Dir, Input: in.Path, Output: out, Password: w.Options.Password,
	})
	if err != nil {
		return pdfResult{}, err
	}

	return pdfResult{
		Outputs: []pdfOutput{{Path: out, Name: in.Stem() + "-unlocked.pdf", MIME: mimePDF}},
		Message: "The password has been removed. Anybody who gets this file can open it.",
	}, nil
}

// comparePDFs writes a text difference between the two attached documents.
func comparePDFs(ctx context.Context, w pdfWork[PDFCompareOptions]) (pdfResult, error) {
	before, after := w.Inputs[0], w.Inputs[1]
	out := filepath.Join(w.Dir, "comparison.md")

	result, err := w.Engines.Compare(ctx, pdf.CompareRequest{
		Dir:       w.Dir,
		Left:      before.Path,
		Right:     after.Path,
		LeftName:  before.Name,
		RightName: after.Name,
		Output:    out,
	})
	if err != nil {
		return pdfResult{}, err
	}

	return pdfResult{
		Outputs: []pdfOutput{{Path: out, Name: "comparison.md", MIME: mimeMarkdown}},
		Message: result.ComparisonSummary() + " This compares the text, so a change to a font, " +
			"a margin or a picture does not appear.",
	}, nil
}
