package detect_test

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

	pkcs12 "software.sslmate.com/src/go-pkcs12"

	"github.com/AlexGalhardo/vukem-tools/internal/detect"
)

// Real headers, not invented ones: a test built from a made-up magic number
// proves the test author's guess, not the detector.
var samples = map[string]struct {
	head []byte
	mime string
}{
	"png": {
		[]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x08\x00\x00\x00\x08\x08\x06\x00\x00\x00"),
		"image/png",
	},
	"jpeg": {[]byte("\xff\xd8\xff\xe0\x00\x10JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00"), "image/jpeg"},
	"gif":  {[]byte("GIF89a\x08\x00\x08\x00\x80\x00\x00"), "image/gif"},
	"webp": {append([]byte("RIFF\x24\x00\x00\x00WEBPVP8 "), make([]byte, 16)...), "image/webp"},
	"pdf":  {[]byte("%PDF-1.7\n1 0 obj\n<< /Type /Catalog >>\n"), "application/pdf"},
	// An MP4 is identified by the ftyp box, which starts at byte 4.
	"mp4": {[]byte("\x00\x00\x00\x20ftypisom\x00\x00\x02\x00isomiso2avc1mp41"), "video/mp4"},
	"zip": {[]byte("PK\x03\x04\x14\x00\x00\x00\x08\x00"), "application/zip"},
	"gzip": {
		[]byte("\x1f\x8b\x08\x00\x00\x00\x00\x00\x00\x03"),
		"application/gzip",
	},
}

func TestOfIdentifiesRealHeaders(t *testing.T) {
	t.Parallel()

	for name, tc := range samples {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := detect.Of(bytes.NewReader(tc.head))
			if err != nil {
				t.Fatalf("Of: %v", err)
			}

			if got.MIME != tc.mime {
				t.Fatalf("MIME = %q, want %q", got.MIME, tc.mime)
			}

			if got.Extension == "" || got.Label == "" {
				t.Fatalf("%q has no canonical extension or label: %+v", got.MIME, got)
			}
		})
	}
}

// This is the ROADMAP's own assert: an .mp4 renamed to .png is rejected with
// a message naming the detected type.
func TestAnMP4RenamedToPNGIsRejectedByName(t *testing.T) {
	t.Parallel()

	_, err := detect.Check(bytes.NewReader(samples["mp4"].head), "holiday.png", []string{"image/png"})

	// Either error would refuse the file, but only the mismatch tells the
	// user what actually happened, and that distinction is the point.
	mismatch, ok := errors.AsType[*detect.MismatchError](err)
	if !ok {
		t.Fatalf("err = %#v, want a *detect.MismatchError", err)
	}

	msg := mismatch.Message()
	if !strings.Contains(msg, "MP4") {
		t.Fatalf("the message does not name the detected type: %q", msg)
	}

	if !strings.Contains(msg, ".png") {
		t.Fatalf("the message does not name the claimed extension: %q", msg)
	}
}

// A file whose name disagrees with its content is refused even when both
// types are accepted: the disagreement is itself the signal.
func TestAMismatchIsRefusedEvenWhenBothTypesAreAccepted(t *testing.T) {
	t.Parallel()

	accepted := []string{"image/png", "image/jpeg"}

	if _, err := detect.Check(bytes.NewReader(samples["jpeg"].head), "photo.png", accepted); err != nil {
		if _, ok := errors.AsType[*detect.MismatchError](err); !ok {
			t.Fatalf("err = %#v, want a *detect.MismatchError", err)
		}
	} else {
		t.Fatal("a JPEG named .png was accepted")
	}
}

// Aliases are the same bytes under a different spelling, so .jpeg must pass
// for a JPEG just as .jpg does.
func TestExtensionAliasesAreAccepted(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"photo.jpg", "photo.jpeg", "photo.JPEG", "photo.JPG"} {
		if _, err := detect.Check(bytes.NewReader(samples["jpeg"].head), name, []string{"image/jpeg"}); err != nil {
			t.Errorf("Check(%q) = %v, want it accepted", name, err)
		}
	}
}

// A name with no extension is not a disagreement: there is nothing to
// disagree with.
func TestANameWithoutAnExtensionIsAccepted(t *testing.T) {
	t.Parallel()

	if _, err := detect.Check(bytes.NewReader(samples["png"].head), "screenshot", []string{"image/png"}); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

func TestCheckRefusesATypeTheToolDoesNotAccept(t *testing.T) {
	t.Parallel()

	_, err := detect.Check(bytes.NewReader(samples["pdf"].head), "paper.pdf", []string{"image/png"})

	unsupported, ok := errors.AsType[*detect.UnsupportedError](err)
	if !ok {
		t.Fatalf("err = %#v, want an *detect.UnsupportedError", err)
	}

	if unsupported.Detected.MIME != "application/pdf" {
		t.Fatalf("the error does not carry the detected type: %+v", unsupported.Detected)
	}

	if msg := unsupported.Message(); !strings.Contains(msg, "PDF") {
		t.Fatalf("the message does not name the detected type: %q", msg)
	}
}

// Bytes that match nothing are refused as unrecognised, and the message must
// not pretend to know what the file was.
func TestUnrecognisedBytesAreRefused(t *testing.T) {
	t.Parallel()

	head := bytes.Repeat([]byte{0x7f, 0x13, 0xa4, 0x00}, 64)

	if _, err := detect.Of(bytes.NewReader(head)); !errors.Is(err, detect.ErrUnknown) {
		t.Fatalf("Of = %v, want ErrUnknown", err)
	}

	_, err := detect.Check(bytes.NewReader(head), "mystery.png", []string{"image/png"})

	unsupported, ok := errors.AsType[*detect.UnsupportedError](err)
	if !ok {
		t.Fatalf("err = %#v, want an *detect.UnsupportedError", err)
	}

	if unsupported.Detected.MIME != "" {
		t.Fatalf("an unrecognised file reported a type: %q", unsupported.Detected.MIME)
	}
}

// An empty file must be refused rather than identified as something.
func TestAnEmptyFileIsRefused(t *testing.T) {
	t.Parallel()

	if _, err := detect.Of(bytes.NewReader(nil)); err == nil {
		t.Fatal("an empty file was identified")
	}
}

// Detection reads only the header. A tool handling a half-gigabyte upload
// must not have the whole thing pulled into memory to learn its type.
func TestDetectionReadsOnlyTheHeader(t *testing.T) {
	t.Parallel()

	body := append(bytes.Clone(samples["png"].head), make([]byte, 1<<20)...)
	counter := &countingReader{r: bytes.NewReader(body)}

	if _, err := detect.Of(counter); err != nil {
		t.Fatalf("Of: %v", err)
	}

	if counter.n > detect.HeaderBytes {
		t.Fatalf("read %d bytes, want at most %d", counter.n, detect.HeaderBytes)
	}
}

type countingReader struct {
	r *bytes.Reader
	n int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n

	return n, err
}

// Every type the service recognises must have a canonical extension and a
// label: a type with no label produces a message that says nothing.
func TestEveryKnownTypeIsFullyDescribed(t *testing.T) {
	t.Parallel()

	types := detect.Types()
	if len(types) == 0 {
		t.Fatal("no types are recognised")
	}

	for _, mime := range types {
		k, ok := detect.KindOf(mime)
		if !ok {
			t.Fatalf("Types lists %q but KindOf does not know it", mime)
		}

		if !strings.HasPrefix(k.Extension, ".") {
			t.Errorf("%q has extension %q, want it to start with a dot", mime, k.Extension)
		}

		if k.Label == "" {
			t.Errorf("%q has no label", mime)
		}

		if k.MIME != mime {
			t.Errorf("KindOf(%q).MIME = %q", mime, k.MIME)
		}
	}
}

func TestKnown(t *testing.T) {
	t.Parallel()

	if !detect.Known("image/png") {
		t.Error("image/png is not known")
	}

	if detect.Known("application/x-made-up") {
		t.Error("an invented type is known")
	}
}

// The image carries OpenEXR because libvips depends on it, and OpenEXR has
// two critical CVEs with no fixed version released (CVE-2026-42216 and
// CVE-2026-42217: information disclosure and denial of service from a
// malformed EXR file). Neither is reachable while EXR is not a type this
// service recognises, because an unrecognised type is refused before any
// engine sees the bytes.
//
// This test is that mitigation, not a note about it: adding EXR turns two
// unreachable criticals into reachable ones, so it has to fail until somebody
// has read them and said why it is acceptable. docs/deployment.md records the
// same reasoning for an operator.
func TestEXRIsNotARecognisedType(t *testing.T) {
	t.Parallel()

	if _, err := detect.Check(bytes.NewReader(exrMagic), "frame.exr", nil); err == nil {
		t.Fatal("an EXR file was accepted; OpenEXR has unfixed critical CVEs")
	}

	for _, mime := range []string{"image/x-exr", "image/aces", "image/exr"} {
		if detect.Known(mime) {
			t.Errorf("%s is a recognised type; read this test's comment before adding it", mime)
		}
	}
}

// exrMagic is the OpenEXR file signature.
var exrMagic = []byte{0x76, 0x2f, 0x31, 0x01, 0x02, 0x00, 0x00, 0x00}

// office builds the smallest ZIP mimetype recognises as an Office document: the
// entries it looks at, in the order it looks, with ODF's stored mimetype first.
func office(t *testing.T, entries ...[2]string) []byte {
	t.Helper()

	var buf bytes.Buffer

	zw := zip.NewWriter(&buf)

	for _, e := range entries {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: e[0], Method: zip.Store})
		if err != nil {
			t.Fatalf("zip entry %s: %v", e[0], err)
		}

		if _, err := w.Write([]byte(e[1])); err != nil {
			t.Fatalf("write %s: %v", e[0], err)
		}
	}

	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}

	return buf.Bytes()
}

func TestOfficeDocumentsAreNotMistakenForZIPArchives(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		file []byte
		want string
	}{
		{
			"docx",
			office(t, [2]string{"[Content_Types].xml", "<Types/>"}, [2]string{"word/document.xml", "<w/>"}),
			".docx",
		},
		{
			"xlsx",
			office(t, [2]string{"[Content_Types].xml", "<Types/>"}, [2]string{"xl/workbook.xml", "<w/>"}),
			".xlsx",
		},
		{
			"pptx",
			office(t, [2]string{"[Content_Types].xml", "<Types/>"}, [2]string{"ppt/presentation.xml", "<p/>"}),
			".pptx",
		},
		{"odt", office(t, [2]string{"mimetype", "application/vnd.oasis.opendocument.text"}), ".odt"},
		{"ods", office(t, [2]string{"mimetype", "application/vnd.oasis.opendocument.spreadsheet"}), ".ods"},
		{"odp", office(t, [2]string{"mimetype", "application/vnd.oasis.opendocument.presentation"}), ".odp"},
		{"zip", office(t, [2]string{"a.txt", "hello"}), ".zip"},
	}

	for _, c := range cases {
		kind, err := detect.Of(bytes.NewReader(c.file))
		if err != nil {
			t.Errorf("%s: %v", c.name, err)

			continue
		}

		if kind.Extension != c.want {
			t.Errorf("%s detected as %s, want %s", c.name, kind.Extension, c.want)
		}
	}
}

func TestEachOfficeConverterReadsOnlyItsOwnFamily(t *testing.T) {
	t.Parallel()

	for _, list := range [][]string{detect.WordTypes(), detect.SpreadsheetTypes(), detect.PresentationTypes()} {
		for _, mime := range list {
			if !detect.Known(mime) {
				t.Errorf("%s is listed for a converter but detect does not know it", mime)
			}
		}
	}
}

// A real bundle, made by the encoder the signing tool's tests use, so the
// recogniser is checked against what a library writes rather than against
// bytes the test author typed.
func TestAPKCS12BundleIsRecognisedByItsHeader(t *testing.T) {
	t.Parallel()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("certificate: %v", err)
	}

	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	for name, enc := range map[string]*pkcs12.Encoder{"modern": pkcs12.Modern, "legacy": pkcs12.LegacyRC2} {
		body, err := enc.Encode(key, cert, nil, "pw")
		if err != nil {
			t.Fatalf("%s encode: %v", name, err)
		}

		kind, err := detect.OfBytes(body[:min(len(body), detect.HeaderBytes)])
		if err != nil || kind.MIME != detect.MIMEPKCS12 {
			t.Errorf("%s bundle detected as %+v, %v; want a PKCS#12 bundle", name, kind, err)
		}

		if _, err := detect.Check(bytes.NewReader(body), "cert.pfx", []string{detect.MIMEPKCS12}); err != nil {
			t.Errorf("%s bundle named .pfx: %v", name, err)
		}
	}

	if _, err := detect.OfBytes(
		[]byte{
			0x30,
			0x82,
			0x01,
			0x00,
			0x02,
			0x01,
			0x01,
			0x30,
			0x00,
			0x00,
			0x00,
			0x00,
			0x00,
			0x00,
			0x00,
			0x00,
			0x00,
			0x00,
			0x00,
			0x00,
			0x00,
			0x00,
			0x00,
			0x00,
		},
	); !errors.Is(
		err,
		detect.ErrUnknown,
	) {
		t.Errorf("a DER sequence that is not a PFX was classified: %v", err)
	}
}
