package pdf

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// MaxPages caps the documents this package will work on.
//
// Several operations hold one item per page: a page range, a stamp page, a
// structural edit. Ten thousand pages is far past any document a person
// converts by hand and still bounded, which is the difference between an
// operation whose cost the service controls and one a caller controls.
const MaxPages = 10_000

// headerWindow is how far into the file the PDF header may start.
//
// The specification says byte zero. Readers tolerate leading junk, and
// Ghostscript and qpdf both scan for the header, so a file they would accept
// must be accepted here too — otherwise the service refuses documents that
// work everywhere else. A kilobyte is the window every implementation agrees
// on, and it is bounded, which is the part that matters: the check reads a
// fixed kilobyte of a file that may be half a gigabyte.
const headerWindow = 1024

// inspectTimeout bounds reading a document's structure. Reading the page
// list is cheap even for a large document, so a probe that takes longer than
// this is a document qpdf is struggling to parse, and the operation that
// follows would struggle too.
const inspectTimeout = 2 * time.Minute

// Document is what a probe learned about a PDF, and the handle every
// operation takes.
//
// It carries the page objects' qpdf ids because the structural operations
// address pages by object: a crop has to rewrite the page dictionary, and the
// only name that dictionary has is the object number qpdf prints.
type Document struct {
	// Path is the file on local disk the probe read.
	Path string
	// Count is how many pages it has.
	Count int
	// objects[i] is the qpdf object id of page i+1, e.g. "4 0 R".
	objects []string
}

// Object returns the qpdf object id of a 1-based page number.
func (d Document) Object(page int) (string, bool) {
	if page < 1 || page > len(d.objects) {
		return "", false
	}

	return d.objects[page-1], true
}

// ValidateHeader refuses a file that is not a PDF, reading only its first
// kilobyte.
//
// This runs before any engine sees the file, and it is not redundant with the
// magic-byte detection the upload already did. The upload checked the bytes
// that arrived; this checks the bytes on local disk at the moment an engine is
// about to be pointed at them. Ghostscript parsing a malformed document is
// the single largest attack surface in the service, and the cheapest defence
// is not calling it.
func ValidateHeader(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("pdf: open %s: %w", path, err)
	}

	defer func() { _ = file.Close() }()

	head := make([]byte, headerWindow)

	n, err := io.ReadFull(file, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return fmt.Errorf("pdf: read %s: %w", path, err)
	}

	return checkHeader(head[:n])
}

// checkHeader looks for "%PDF-" followed by a version in head.
func checkHeader(head []byte) error {
	marker := []byte("%PDF-")

	for i := 0; i+len(marker)+1 <= len(head); i++ {
		if !bytes.Equal(head[i:i+len(marker)], marker) {
			continue
		}

		// A version digit has to follow, or "%PDF-" is a comment somebody
		// wrote in a text file rather than a header.
		if c := head[i+len(marker)]; c >= '0' && c <= '9' {
			return nil
		}
	}

	if len(head) == 0 {
		return &NotPDFError{Reason: "the file is empty"}
	}

	return &NotPDFError{Reason: "no %PDF- header in the first " + strconv.Itoa(headerWindow) + " bytes"}
}

// IsEncrypted reports whether a document is password-protected.
//
// It asks qpdf's --is-encrypted, whose whole answer is its exit status: 0
// encrypted, 2 not. That is deliberately all this function returns. qpdf can
// also say whether the password needed is the user's or the owner's, and this
// function does not expose it, because that is information about somebody
// else's document and nothing in this service has a use for it — ROADMAP S6.5
// requires that an unlock refusal never reveal which kind of password a file
// had, and the way to keep a promise like that is not to carry the
// information in the first place.
func (e *Engines) IsEncrypted(ctx context.Context, dir, path string) (bool, error) {
	code, err := e.exitCode(ctx, command{
		engine:  EngineQPDF,
		args:    []string{"--is-encrypted", path},
		dir:     dir,
		timeout: inspectTimeout,
	})
	if err != nil {
		return false, err
	}

	switch code {
	case qpdfOK:
		return true, nil
	case qpdfError:
		return false, nil
	default:
		// qpdf answers this predicate with 0 or 2 and nothing else. Anything
		// else means it could not read the file at all, which the caller has
		// to hear about rather than read as "not encrypted".
		return false, &DamagedError{Detail: "qpdf --is-encrypted exited " + strconv.Itoa(code)}
	}
}

// Inspect validates a document and reads its page list.
//
// It is the first call of every operation, and the order of its checks is the
// point: the header before the engine, the encryption before the work, the
// page cap before anything that allocates per page. An encrypted document
// comes back as ErrEncrypted rather than being handed to an engine, because
// several engines answer an encrypted input with an empty output, and an empty
// document that opens is the failure a user does not notice.
func (e *Engines) Inspect(ctx context.Context, dir, path string) (Document, error) {
	if !inside(dir, path) {
		return Document{}, fmt.Errorf("pdf: %s is outside the job directory", filepath.Base(path))
	}

	if err := ValidateHeader(path); err != nil {
		return Document{}, err
	}

	encrypted, err := e.IsEncrypted(ctx, dir, path)
	if err != nil {
		return Document{}, err
	}

	if encrypted {
		return Document{}, ErrEncrypted
	}

	objects, err := e.pageObjects(ctx, dir, path)
	if err != nil {
		return Document{}, err
	}

	if len(objects) == 0 {
		return Document{}, &DamagedError{Detail: "the document has no pages"}
	}

	if len(objects) > MaxPages {
		return Document{}, &TooManyPagesError{Pages: len(objects), Limit: MaxPages}
	}

	return Document{Path: path, Count: len(objects), objects: objects}, nil
}

// qpdfPages is the shape of `qpdf --json --json-key=pages`.
//
// Only the two fields this package uses are declared. The engine prints more
// per page — the content streams, the images, the label — and the decoder
// walks past them without keeping them, which is what keeps the memory cost
// proportional to the page count rather than to the document.
type qpdfPages struct {
	Pages []struct {
		Object       string `json:"object"`
		PagePosFrom1 int    `json:"pageposfrom1"`
	} `json:"pages"`
}

// pageObjects reads the document's page list.
func (e *Engines) pageObjects(ctx context.Context, dir, path string) ([]string, error) {
	var decoded qpdfPages

	err := e.runJSON(ctx, command{
		engine:  EngineQPDF,
		args:    []string{"--json=latest", "--json-key=pages", path},
		dir:     dir,
		timeout: inspectTimeout,
	}, &decoded)
	if err != nil {
		return nil, err
	}

	out := make([]string, 0, len(decoded.Pages))

	for i, page := range decoded.Pages {
		if page.Object == "" {
			return nil, &DamagedError{Detail: "page " + strconv.Itoa(i+1) + " has no object id"}
		}

		// The engine numbers the pages itself; a list that disagrees with its
		// own numbering is a document this package will not guess about.
		if page.PagePosFrom1 != i+1 {
			return nil, &DamagedError{
				Detail: "page " + strconv.Itoa(i+1) + " reports position " + strconv.Itoa(page.PagePosFrom1),
			}
		}

		out = append(out, page.Object)
	}

	return out, nil
}

// maxJSONBytes caps the structural JSON an engine may print.
//
// qpdf's JSON is proportional to the object count, not to the content, so a
// document whose structure is this large is a document built to exhaust a
// parser rather than one somebody wants converted.
const maxJSONBytes = 64 << 20

// runJSON runs an engine that prints JSON and decodes it into out.
//
// The output goes to a file in the job directory and is decoded from there,
// rather than through a pipe: a pipe would need a reader goroutine racing the
// subprocess, and the job directory is deleted when the conversion ends
// either way. The read is capped, and the decoder streams, so neither the
// engine nor the decoder can be made to allocate without limit.
func (e *Engines) runJSON(ctx context.Context, cmd command, out any) error {
	path, err := os.CreateTemp(cmd.dir, "probe-*.json")
	if err != nil {
		return fmt.Errorf("pdf: create a probe file: %w", err)
	}

	name := path.Name()

	defer func() {
		_ = path.Close()
		_ = os.Remove(name)
	}()

	cmd.stdout = path

	if _, err := e.run(ctx, cmd); err != nil {
		return err
	}

	if _, err := path.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("pdf: rewind the probe file: %w", err)
	}

	if err := json.NewDecoder(io.LimitReader(path, maxJSONBytes)).Decode(out); err != nil {
		return &DamagedError{Detail: "could not read the document's structure: " + err.Error()}
	}

	return nil
}
