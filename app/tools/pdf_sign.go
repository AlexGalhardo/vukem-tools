package tools

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/AlexGalhardo/vukem/z"

	"github.com/AlexGalhardo/vukem-tools/app/abilities"
	"github.com/AlexGalhardo/vukem-tools/internal/detect"
	"github.com/AlexGalhardo/vukem-tools/internal/jobs"
	"github.com/AlexGalhardo/vukem-tools/internal/pdf"
)

const pdfSignTimeout = 5 * time.Minute

// PDFSignOptions is the input of a signature.
type PDFSignOptions struct {
	// Password opens the .p12 bundle. It is the only copy of the secret this
	// service holds, and only until the conversion's records expire.
	Password string `json:"password" validate:"required,max=256" doc:"The password of the .p12 certificate bundle. The key is used for this one signature and is not kept."`
	// Reason, Location and Contact are shown by a PDF reader beside the signature.
	Reason   string `json:"reason"   validate:"trim,max=200" doc:"Why you are signing. Shown beside the signature. Optional."`
	Location string `json:"location" validate:"trim,max=200" doc:"Where you are signing. Optional."`
	Contact  string `json:"contact"  validate:"trim,max=200" doc:"How to reach the signer. Optional."`
}

// Normalise implements the option contract.
//
// The password is not trimmed: it is bytes the owner chose, and a trimmed one
// would fail to open the bundle for no reason they could see.
func (o PDFSignOptions) Normalise() (PDFSignOptions, error) {
	if o.Password == "" {
		return PDFSignOptions{}, z.FieldError("options.password", "Enter the password of the .p12 file.")
	}

	if strings.ContainsAny(o.Password, "\r\n") {
		return PDFSignOptions{}, z.FieldError("options.password", "A password may not contain a line break.")
	}

	o.Reason = strings.TrimSpace(o.Reason)
	o.Location = strings.TrimSpace(o.Location)
	o.Contact = strings.TrimSpace(o.Contact)

	return o, nil
}

// pdfSignTools is the signing tool (S6.5).
//
// It takes two files, the document and the owner's own .p12 bundle, and tells
// them apart by their detected type rather than by their order. The ability is
// the security one: a key that can sign can be made to sign anything its owner
// has a certificate for.
func pdfSignTools(deps *pdfDeps) []jobs.Tool {
	return []jobs.Tool{&pdfTool[PDFSignOptions]{
		deps: deps, name: NamePDFSign, ability: abilities.PDFSecurity,
		timeout: pdfSignTimeout, accepted: []string{mimePDF, detect.MIMEPKCS12},
		engines: []string{pdf.EngineQPDF}, minInputs: 2, maxInputs: 2,
		work: signPDF,
	}}
}

// signPDF signs a document with the certificate in the other upload.
func signPDF(ctx context.Context, w pdfWork[PDFSignOptions]) (pdfResult, error) {
	var document, bundle *pdfInput

	for i := range w.Inputs {
		switch w.Inputs[i].MIME {
		case mimePDF:
			document = &w.Inputs[i]
		case detect.MIMEPKCS12:
			bundle = &w.Inputs[i]
		}
	}

	if document == nil || bundle == nil || len(w.Inputs) != 2 {
		return pdfResult{}, &pdf.OptionError{
			Field: "file_ids", Reason: "Give one PDF and one .p12 certificate file.",
		}
	}

	out := filepath.Join(w.Dir, "signed.pdf")

	res, err := w.Engines.Sign(ctx, pdf.SignRequest{
		Dir: w.Dir, Input: document.Path, Output: out, Bundle: bundle.Path,
		Password: w.Options.Password, Reason: w.Options.Reason,
		Location: w.Options.Location, Contact: w.Options.Contact,
	})
	if err != nil {
		return pdfResult{}, err
	}

	message := "Signed by " + res.Signer + " with " + res.Algorithm + "."
	if res.SelfSigned {
		message += " The certificate is self-signed, so a PDF reader will say the signer's identity " +
			"is unknown until it is told to trust that certificate. The signature still proves the " +
			"document has not changed since."
	}

	return pdfResult{
		Outputs: []pdfOutput{{Path: out, Name: document.Stem() + "-signed.pdf", MIME: mimePDF}},
		Message: message,
	}, nil
}
