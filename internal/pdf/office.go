package pdf

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// officeTimeout bounds one document conversion. LibreOffice starts slowly
// and a long spreadsheet paginates slowly, so this is generous, but it is a
// bound: a document that hangs the layout engine must not hold a worker.
const officeTimeout = 5 * time.Minute

// OfficeRequest is a document to convert to PDF.
type OfficeRequest struct {
	// Dir is the job's working directory.
	Dir string
	// Input is a Word, Excel, PowerPoint or OpenDocument file inside Dir. Its
	// extension must be the real one, because LibreOffice picks its import
	// filter from it; the tool layer names the file from the detected type.
	Input string
	// Output is the PDF to write, inside Dir.
	Output string
}

// OfficeToPDF converts a document with LibreOffice.
//
// Three things make this safe to run for strangers, and each is deliberate:
//
//   - A user profile of its own inside the job directory. LibreOffice keeps a
//     lock and a settings database in its profile, so two conversions sharing
//     one profile queue behind each other or crash; one profile per job is
//     what lets conversions run side by side.
//   - Macros stay off. A document's macros never run in a headless
//     conversion by default; the profile is fresh, so no setting can turn
//     them on.
//   - No network. The engine rules forbid it for conversion subprocesses, and
//     the sandbox applies that. A document that links an image by URL
//     renders without it.
func (e *Engines) OfficeToPDF(ctx context.Context, req OfficeRequest) error {
	if !inside(req.Dir, req.Input) || !inside(req.Dir, req.Output) {
		return &OptionError{Field: "path", Reason: "The file is outside the working directory."}
	}

	out := filepath.Join(req.Dir, "office-out")
	if err := os.Mkdir(out, 0o700); err != nil {
		return fmt.Errorf("pdf: create the office output directory: %w", err)
	}

	profile := filepath.ToSlash(filepath.Join(req.Dir, "lo-profile"))

	_, err := e.run(ctx, command{
		engine: EngineLibreOffice,
		dir:    req.Dir,
		args: []string{
			"--headless", "--norestore", "--nolockcheck", "--nodefault", "--nofirststartwizard",
			"-env:UserInstallation=file://" + profile,
			"--convert-to", "pdf",
			"--outdir", out,
			req.Input,
		},
		timeout: officeTimeout,
	})
	if err != nil {
		return err
	}

	// LibreOffice exits 0 for a document it could not open and writes
	// nothing, so the exit status is not the answer: the file is.
	produced := filepath.Join(out, strings.TrimSuffix(filepath.Base(req.Input), filepath.Ext(req.Input))+".pdf")

	info, err := os.Stat(produced)
	if err != nil || info.Size() == 0 {
		return &EngineError{Engine: EngineLibreOffice, Code: 0, Stderr: "no PDF was produced"}
	}

	if err := ValidateHeader(produced); err != nil {
		return err
	}

	if err := os.Rename(produced, req.Output); err != nil {
		return fmt.Errorf("pdf: move the converted document: %w", err)
	}

	return nil
}
