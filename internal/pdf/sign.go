package pdf

import (
	"bytes"
	"context"
	"crypto"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	pkcs12 "software.sslmate.com/src/go-pkcs12"
)

// MaxBundleBytes caps the .p12 file. A certificate, its chain and a key are a
// few kilobytes; a megabyte is far past any real one.
const MaxBundleBytes = 1 << 20

// maxFieldText bounds the free text a signature carries (reason, location,
// contact), which readers display.
const maxFieldText = 200

// byteRangePlaceholder is what the signature dictionary holds before the
// real offsets are known. Ten digits is wider than any offset in a file under
// 10 GB, so the real array always fits in the same bytes and nothing after it
// moves; a different width would shift the cross-reference table.
const byteRangePlaceholder = "9999999999 9999999999 9999999999"

// SignRequest is a document to sign with a certificate the caller owns.
type SignRequest struct {
	// Dir is the job's working directory.
	Dir string
	// Input and Output are PDFs inside Dir.
	Input, Output string
	// Bundle is the .p12 file, inside Dir.
	Bundle string
	// Password opens the bundle.
	Password string
	// Reason, Location and Contact are shown by a reader beside the signature.
	Reason, Location, Contact string
	// Now is the signing time. Zero means the current time.
	Now time.Time
}

// SignResult says who signed and how.
type SignResult struct {
	// Signer is the certificate's subject.
	Signer string
	// Algorithm names the key type.
	Algorithm string
	// SignedAt is the time recorded in the signature.
	SignedAt time.Time
	// SelfSigned reports a certificate that is its own issuer, which a reader
	// will not trust until the signer's certificate is installed there.
	SelfSigned bool
}

// ErrAlreadySigned means the document carries a signature.
//
// Adding a signature here rewrites the file, which would invalidate the one
// already there, and an invalid signature on a document is worse than none.
var ErrAlreadySigned = errors.New("pdf: the document is already signed")

// Sign adds an invisible, detached CMS signature (adbe.pkcs7.detached) made
// with the certificate and key in a PKCS#12 bundle.
//
// The private key lives in this process for the length of one call. It is
// read from a file in the job's directory, used to sign one SHA-256 digest and
// dropped; the directory is removed when the job ends. Nothing here logs it,
// returns it or writes it anywhere else.
//
// The signature is produced in three steps. qpdf adds the signature field with
// a placeholder for the signature and for the byte ranges it covers, and
// writes the file. Then the ranges are filled in. Then the document is hashed
// over everything except the signature's own bytes and the signature is put
// where the placeholder was. A placeholder of the same width means no offset
// in the file changes after qpdf wrote it.
func (e *Engines) Sign(ctx context.Context, req SignRequest) (SignResult, error) {
	for _, p := range []string{req.Input, req.Output, req.Bundle} {
		if !inside(req.Dir, p) {
			return SignResult{}, &OptionError{Field: "path", Reason: "The file is outside the working directory."}
		}
	}

	if _, err := e.Inspect(ctx, req.Dir, req.Input); err != nil {
		return SignResult{}, err
	}

	key, cert, chain, err := openBundle(req.Bundle, req.Password, req.Now)
	if err != nil {
		return SignResult{}, err
	}

	at := req.Now
	if at.IsZero() {
		at = time.Now()
	}

	at = at.UTC().Truncate(time.Second)

	// Measure the signature with a dummy digest, so the placeholder is as
	// big as the real one will be plus a margin. A bigger certificate chain
	// or key needs a bigger placeholder, and a fixed size would either waste
	// kilobytes on every document or fail for a long chain.
	probe, err := buildCMS(make([]byte, sha256.Size), key, cert, chain, at)
	if err != nil {
		return SignResult{}, err
	}

	capacity := (len(probe) + 256 + 255) / 256 * 256

	update, err := e.signUpdate(ctx, req, cert, capacity, at)
	if err != nil {
		return SignResult{}, err
	}

	if err := e.qpdf(ctx, req.Dir, []string{
		"--update-from-json=" + update, "--object-streams=disable", req.Input, req.Output,
	}); err != nil {
		return SignResult{}, err
	}

	if err := finishSignature(req.Output, capacity, key, cert, chain, at); err != nil {
		return SignResult{}, err
	}

	return SignResult{
		Signer:     cert.Subject.String(),
		Algorithm:  cert.PublicKeyAlgorithm.String() + " with SHA-256",
		SignedAt:   at,
		SelfSigned: bytes.Equal(cert.RawIssuer, cert.RawSubject),
	}, nil
}

// openBundle reads the key and certificates from a .p12 file and refuses a
// certificate that cannot honestly sign.
func openBundle(path, password string, now time.Time) (crypto.Signer, *x509.Certificate, []*x509.Certificate, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("pdf: stat the certificate bundle: %w", err)
	}

	if info.Size() > MaxBundleBytes {
		return nil, nil, nil, &OptionError{
			Field:  "certificate",
			Reason: "That certificate file is too large to be a .p12 bundle.",
		}
	}

	body, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("pdf: read the certificate bundle: %w", err)
	}

	priv, cert, chain, err := pkcs12.DecodeChain(body, password)
	if err != nil {
		if errors.Is(err, pkcs12.ErrIncorrectPassword) {
			return nil, nil, nil, ErrWrongPassword
		}

		return nil, nil, nil, &OptionError{
			Field:  "certificate",
			Reason: "That file is not a readable .p12 certificate bundle.",
		}
	}

	signer, ok := priv.(crypto.Signer)
	if !ok {
		return nil, nil, nil, &OptionError{Field: "certificate", Reason: "The key in that bundle cannot sign."}
	}

	switch cert.PublicKeyAlgorithm {
	case x509.RSA, x509.ECDSA:
	default:
		return nil, nil, nil, &OptionError{Field: "certificate", Reason: "Only RSA and ECDSA certificates can sign."}
	}

	if now.IsZero() {
		now = time.Now()
	}

	switch {
	case now.Before(cert.NotBefore):
		return nil, nil, nil, &OptionError{
			Field:  "certificate",
			Reason: "That certificate is not valid until " + cert.NotBefore.UTC().Format("2006-01-02") + ".",
		}
	case now.After(cert.NotAfter):
		return nil, nil, nil, &OptionError{
			Field:  "certificate",
			Reason: "That certificate expired on " + cert.NotAfter.UTC().Format("2006-01-02") + ".",
		}
	}

	return signer, cert, chain, nil
}

// signDoc is the part of qpdf's JSON signing reads.
type signDoc struct {
	Qpdf  []json.RawMessage `json:"qpdf"`
	Pages []struct {
		Object string `json:"object"`
		Pos    int    `json:"pageposfrom1"`
	} `json:"pages"`
}

// signUpdate writes the qpdf update that adds the signature field, and returns
// its path.
func (e *Engines) signUpdate(
	ctx context.Context,
	req SignRequest,
	cert *x509.Certificate,
	capacity int,
	at time.Time,
) (string, error) {
	var doc signDoc

	err := e.runJSON(ctx, command{
		engine:  EngineQPDF,
		args:    []string{"--json=latest", "--json-key=qpdf", "--json-key=pages", req.Input},
		dir:     req.Dir,
		timeout: inspectTimeout,
	}, &doc)
	if err != nil {
		return "", err
	}

	if len(doc.Qpdf) < 2 || len(doc.Pages) == 0 {
		return "", &DamagedError{Detail: "the document has no pages or no object table"}
	}

	if bytes.Contains(doc.Qpdf[1], []byte(`"/ByteRange"`)) {
		return "", ErrAlreadySigned
	}

	var header struct {
		MaxObject int `json:"maxobjectid"`
	}

	if err := json.Unmarshal(doc.Qpdf[0], &header); err != nil || header.MaxObject <= 0 {
		return "", &DamagedError{Detail: "could not read the highest object number"}
	}

	var table map[string]struct {
		Value json.RawMessage `json:"value"`
	}

	if err := json.Unmarshal(doc.Qpdf[1], &table); err != nil {
		return "", &DamagedError{Detail: "could not read the object table: " + err.Error()}
	}

	object := func(ref string) (map[string]any, bool) {
		entry, ok := table["obj:"+ref]
		if !ok {
			return nil, false
		}

		var d map[string]any
		if json.Unmarshal(entry.Value, &d) != nil {
			return nil, false
		}

		return d, true
	}

	var trailer struct {
		Value map[string]any `json:"value"`
	}

	if raw, ok := table["trailer"]; ok {
		_ = json.Unmarshal(raw.Value, &trailer.Value)
	}

	rootRef, _ := trailer.Value["/Root"].(string)

	root, ok := object(rootRef)
	if !ok {
		return "", &DamagedError{Detail: "the document has no catalogue"}
	}

	pageRef := doc.Pages[0].Object

	page, ok := object(pageRef)
	if !ok {
		return "", &DamagedError{Detail: "the first page's object is missing"}
	}

	sigRef := strconv.Itoa(header.MaxObject+1) + " 0 R"
	fieldRef := strconv.Itoa(header.MaxObject+2) + " 0 R"

	changes := map[string]any{}

	sig := map[string]any{
		"/Type":      "/Sig",
		"/Filter":    "/Adobe.PPKLite",
		"/SubFilter": "/adbe.pkcs7.detached",
		"/ByteRange": []any{0, 9999999999, 9999999999, 9999999999},
		"/Contents":  "b:" + strings.Repeat("00", capacity),
		"/M":         "u:" + pdfDate(at),
		"/Name":      "u:" + cert.Subject.CommonName,
	}

	for key, text := range map[string]string{"/Reason": req.Reason, "/Location": req.Location, "/ContactInfo": req.Contact} {
		if text = strings.TrimSpace(text); text != "" {
			if len([]rune(text)) > maxFieldText {
				return "", &OptionError{Field: strings.ToLower(key[1:]), Reason: "At most 200 characters."}
			}

			sig[key] = "u:" + text
		}
	}

	changes["obj:"+sigRef] = map[string]any{"value": sig}
	changes["obj:"+fieldRef] = map[string]any{"value": map[string]any{
		"/Type": "/Annot", "/Subtype": "/Widget", "/FT": "/Sig", "/T": "u:Signature1",
		"/V": sigRef, "/F": 132, "/Rect": []any{0, 0, 0, 0}, "/P": pageRef,
	}}

	// The page lists the widget, and the AcroForm lists the field. Each may be
	// a direct value or a reference to an object of its own.
	if err := appendRef(changes, page, "/Annots", fieldRef, table); err != nil {
		return "", err
	}

	changes["obj:"+pageRef] = map[string]any{"value": page}

	form, formRef := root["/AcroForm"], ""
	if ref, isRef := form.(string); isRef {
		formRef = ref
	}

	switch {
	case formRef != "":
		fd, ok := object(formRef)
		if !ok {
			return "", &DamagedError{Detail: "the AcroForm object is missing"}
		}

		if err := appendRef(changes, fd, "/Fields", fieldRef, table); err != nil {
			return "", err
		}

		fd["/SigFlags"] = 3
		changes["obj:"+formRef] = map[string]any{"value": fd}
	case form != nil:
		fd, _ := form.(map[string]any)
		if fd == nil {
			return "", &DamagedError{Detail: "the AcroForm is not a dictionary"}
		}

		if err := appendRef(changes, fd, "/Fields", fieldRef, table); err != nil {
			return "", err
		}

		fd["/SigFlags"] = 3
		root["/AcroForm"] = fd
		changes["obj:"+rootRef] = map[string]any{"value": root}
	default:
		root["/AcroForm"] = map[string]any{"/Fields": []any{fieldRef}, "/SigFlags": 3}
		changes["obj:"+rootRef] = map[string]any{"value": root}
	}

	body, err := json.Marshal(map[string]any{
		"version":    2,
		"parameters": map[string]any{"decodelevel": "generalized"},
		"qpdf":       []any{doc.Qpdf[0], changes},
	})
	if err != nil {
		return "", fmt.Errorf("pdf: encode the signature update: %w", err)
	}

	path := filepath.Join(req.Dir, "sign-update.json")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return "", fmt.Errorf("pdf: write the signature update: %w", err)
	}

	return path, nil
}

// appendRef adds ref to the array under key in dict. When the array is itself
// an indirect object, that object is the one that changes.
func appendRef(
	changes map[string]any,
	dict map[string]any,
	key, ref string,
	table map[string]struct {
		Value json.RawMessage `json:"value"`
	},
) error {
	switch cur := dict[key].(type) {
	case nil:
		dict[key] = []any{ref}
	case []any:
		dict[key] = append(cur, ref)
	case string:
		entry, ok := table["obj:"+cur]
		if !ok {
			return &DamagedError{Detail: "the " + key + " array's object is missing"}
		}

		var arr []any
		if json.Unmarshal(entry.Value, &arr) != nil {
			return &DamagedError{Detail: "the " + key + " entry is not an array"}
		}

		changes["obj:"+cur] = map[string]any{"value": append(arr, ref)}
	default:
		return &DamagedError{Detail: "the " + key + " entry is not an array"}
	}

	return nil
}

// signatureContents finds the signature's own /Contents key and the "<" that
// opens its hex string.
var signatureContents = regexp.MustCompile(`/Contents\s*<`)

// pdfDate formats a time as a PDF date string.
func pdfDate(t time.Time) string { return "D:" + t.UTC().Format("20060102150405") + "Z" }

// finishSignature fills the byte ranges, hashes the document and writes the
// signature into the placeholder.
func finishSignature(
	path string,
	capacity int,
	key crypto.Signer,
	cert *x509.Certificate,
	chain []*x509.Certificate,
	at time.Time,
) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("pdf: open the document to sign it: %w", err)
	}

	defer func() { _ = f.Close() }()

	size, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return fmt.Errorf("pdf: measure the document: %w", err)
	}

	at0, err := findPlaceholder(f, size, []byte(byteRangePlaceholder))
	if err != nil {
		return err
	}

	hexLen := int64(capacity) * 2

	// The dictionary is small, so the window around the placeholder holds the
	// whole of it: the byte ranges, and the signature bytes beside them.
	winStart := max(at0-hexLen-1024, 0)
	winEnd := min(at0+hexLen+1024, size)
	window := make([]byte, winEnd-winStart)

	if _, err := f.ReadAt(window, winStart); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("pdf: read around the signature: %w", err)
	}

	local := int(at0 - winStart)

	open := bytes.LastIndexByte(window[:local], '[')
	closeAt := bytes.IndexByte(window[local:], ']')

	if open < 0 || closeAt < 0 {
		return &DamagedError{Detail: "the signature's byte range is malformed"}
	}

	rangeStart, rangeEnd := winStart+int64(open), winStart+int64(local+closeAt+1)

	// A page has a /Contents of its own (a reference to its content stream),
	// so the key alone is not enough: the signature's is the one followed by
	// a hex string.
	loc := signatureContents.FindIndex(window)
	if loc == nil {
		return &DamagedError{Detail: "the signature has no contents"}
	}

	hexStart := winStart + int64(loc[1])
	if hexStart+hexLen >= size || !allZeros(window, loc[1], int(hexLen)) ||
		window[loc[1]+int(hexLen)] != '>' {
		return &DamagedError{Detail: "the signature placeholder is not the size that was reserved"}
	}

	lt0, gt := hexStart-1, hexStart+hexLen+1

	text := fmt.Sprintf("[ 0 %d %d %d ]", lt0, gt, size-gt)
	if int64(len(text)) > rangeEnd-rangeStart {
		return &DamagedError{Detail: "the byte range does not fit its placeholder"}
	}

	text += strings.Repeat(" ", int(rangeEnd-rangeStart)-len(text))

	if _, err := f.WriteAt([]byte(text), rangeStart); err != nil {
		return fmt.Errorf("pdf: write the byte range: %w", err)
	}

	h := sha256.New()

	for _, r := range [][2]int64{{0, lt0}, {gt, size}} {
		if _, err := io.Copy(h, io.NewSectionReader(f, r[0], r[1]-r[0])); err != nil {
			return fmt.Errorf("pdf: hash the document: %w", err)
		}
	}

	der, err := buildCMS(h.Sum(nil), key, cert, chain, at)
	if err != nil {
		return err
	}

	if int64(len(der))*2 > hexLen {
		return &DamagedError{Detail: "the signature is larger than the space reserved for it"}
	}

	out := []byte(strings.ToUpper(hex.EncodeToString(der)))
	out = append(out, bytes.Repeat([]byte{'0'}, int(hexLen)-len(out))...)

	if _, err := f.WriteAt(out, hexStart); err != nil {
		return fmt.Errorf("pdf: write the signature: %w", err)
	}

	return f.Sync()
}

func allZeros(b []byte, from, n int) bool {
	if from < 0 || from+n > len(b) {
		return false
	}

	for _, c := range b[from : from+n] {
		if c != '0' {
			return false
		}
	}

	return true
}

// findPlaceholder returns the offset of needle, which must occur exactly once.
// It reads in blocks so a large document is never held in memory.
func findPlaceholder(f *os.File, size int64, needle []byte) (int64, error) {
	const block = 1 << 20

	buf := make([]byte, block+len(needle))
	found := int64(-1)

	for off := int64(0); off < size; off += block {
		n, err := f.ReadAt(buf, off)
		if err != nil && !errors.Is(err, io.EOF) {
			return 0, fmt.Errorf("pdf: search the document: %w", err)
		}

		for from := 0; ; {
			i := bytes.Index(buf[from:n], needle)
			if i < 0 {
				break
			}

			pos := off + int64(from+i)
			// The overlap of one block with the next would report the same
			// match twice; only a match that starts in this block counts.
			if pos < off+block {
				if found >= 0 && found != pos {
					return 0, &DamagedError{Detail: "the signature placeholder appears twice"}
				}

				found = pos
			}

			from += i + 1
		}
	}

	if found < 0 {
		return 0, &DamagedError{Detail: "the signature placeholder was not written"}
	}

	return found, nil
}
