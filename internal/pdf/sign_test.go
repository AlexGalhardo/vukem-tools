package pdf_test

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	pkcs12 "software.sslmate.com/src/go-pkcs12"

	"github.com/AlexGalhardo/vukem-tools/internal/pdf"
)

// writeBundle makes a self-signed certificate and writes it, with its key, as
// a .p12 under password. notBefore and notAfter let a test make an expired one.
func writeBundle(t *testing.T, dir string, key crypto.Signer, password string, notBefore, notAfter time.Time) string {
	t.Helper()

	template := &x509.Certificate{
		SerialNumber: big.NewInt(42),
		Subject:      pkix.Name{CommonName: "Ana Souza", Organization: []string{"Vukem Test"}},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageContentCommitment,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, key.Public(), key)
	if err != nil {
		t.Fatalf("create the certificate: %v", err)
	}

	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse the certificate: %v", err)
	}

	body, err := pkcs12.Modern.Encode(key, cert, nil, password)
	if err != nil {
		t.Fatalf("encode the bundle: %v", err)
	}

	path := filepath.Join(dir, "cert.p12")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("write the bundle: %v", err)
	}

	return path
}

func newKeys(t *testing.T) map[string]crypto.Signer {
	t.Helper()

	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa key: %v", err)
	}

	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa key: %v", err)
	}

	return map[string]crypto.Signer{"RSA": rsaKey, "ECDSA": ecKey}
}

// pdfsig is poppler's independent verifier. It is what proves the signature
// is real: a test that only read the bytes back would prove this code agrees
// with itself.
func pdfsig(t *testing.T, path string) string {
	t.Helper()

	bin, err := exec.LookPath("pdfsig")
	if err != nil {
		t.Skip("pdfsig is not installed. This assertion runs inside the production image.")
	}

	out, _ := exec.CommandContext(context.Background(), bin, path).CombinedOutput()

	return string(out)
}

func TestASignedDocumentVerifiesWithAnIndependentReader(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineQPDF, pdf.EnginePDFToText)

	for name, key := range newKeys(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			in := writeNamedFixture(t, dir, "in.pdf", []string{"Contract", "Annex"})
			bundle := writeBundle(t, dir, key, "s3cret", time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour))
			out := filepath.Join(dir, "signed.pdf")

			res, err := engines.Sign(context.Background(), pdf.SignRequest{
				Dir: dir, Input: in, Output: out, Bundle: bundle, Password: "s3cret",
				Reason: "I approve", Location: "Sao Paulo",
			})
			if err != nil {
				t.Fatalf("Sign: %v", err)
			}

			if !res.SelfSigned || !strings.Contains(res.Signer, "Ana Souza") {
				t.Errorf("result = %+v, want the self-signed certificate's subject", res)
			}

			report := pdfsig(t, out)
			if !strings.Contains(report, "Signature is Valid") {
				t.Fatalf("pdfsig does not accept the signature:\n%s", report)
			}

			if !strings.Contains(report, "Ana Souza") {
				t.Errorf("pdfsig does not show the signer:\n%s", report)
			}

			if text := pdfText(
				t,
				engines,
				dir,
				out,
			); !strings.Contains(text, "Contract") ||
				!strings.Contains(text, "Annex") {
				t.Errorf("the signed document lost its text: %q", text)
			}

			// Any change after signing must be caught, which is the whole
			// point of a signature. The version digit in the header is inside
			// the signed range and changing it leaves a document that still
			// opens.
			raw, _ := os.ReadFile(out)
			if !strings.HasPrefix(string(raw), "%PDF-1.") {
				t.Fatalf("unexpected header %q", raw[:8])
			}

			raw[7] ^= 1
			tampered := filepath.Join(dir, "tampered.pdf")
			_ = os.WriteFile(tampered, raw, 0o600)

			if strings.Contains(pdfsig(t, tampered), "Signature is Valid") {
				t.Error("pdfsig accepts a document that was changed after signing")
			}
		})
	}
}

func TestSigningRefusesWhatWouldMakeAWorthlessSignature(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineQPDF)
	key := newKeys(t)["RSA"]
	dir := t.TempDir()
	in := writeNamedFixture(t, dir, "in.pdf", []string{"Contract"})
	ctx := context.Background()
	out := filepath.Join(dir, "out.pdf")

	good := writeBundle(t, dir, key, "pw", time.Now().Add(-time.Hour), time.Now().Add(time.Hour))

	if _, err := engines.Sign(
		ctx,
		pdf.SignRequest{Dir: dir, Input: in, Output: out, Bundle: good, Password: "wrong"},
	); !errors.Is(
		err,
		pdf.ErrWrongPassword,
	) {
		t.Errorf("wrong password: err = %v, want ErrWrongPassword", err)
	}

	expired := writeBundle(t, dir, key, "pw", time.Now().Add(-48*time.Hour), time.Now().Add(-24*time.Hour))
	if _, err := engines.Sign(
		ctx,
		pdf.SignRequest{Dir: dir, Input: in, Output: out, Bundle: expired, Password: "pw"},
	); err == nil ||
		!strings.Contains(err.Error(), "expired") {
		t.Errorf("expired certificate: err = %v, want a refusal that says so", err)
	}

	notP12 := filepath.Join(dir, "junk.p12")
	_ = os.WriteFile(notP12, []byte("not a bundle"), 0o600)

	if _, err := engines.Sign(
		ctx,
		pdf.SignRequest{Dir: dir, Input: in, Output: out, Bundle: notP12, Password: "pw"},
	); err == nil {
		t.Error("a file that is not a .p12 was accepted")
	}
}

func TestSigningTwiceIsRefusedRatherThanBreakingTheFirstSignature(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineQPDF)
	dir := t.TempDir()
	in := writeNamedFixture(t, dir, "in.pdf", []string{"Contract"})
	bundle := writeBundle(t, dir, newKeys(t)["RSA"], "pw", time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	once := filepath.Join(dir, "once.pdf")

	if _, err := engines.Sign(
		context.Background(),
		pdf.SignRequest{Dir: dir, Input: in, Output: once, Bundle: bundle, Password: "pw"},
	); err != nil {
		t.Fatalf("first signature: %v", err)
	}

	_, err := engines.Sign(context.Background(), pdf.SignRequest{
		Dir: dir, Input: once, Output: filepath.Join(dir, "twice.pdf"), Bundle: bundle, Password: "pw",
	})
	if !errors.Is(err, pdf.ErrAlreadySigned) {
		t.Fatalf("err = %v, want ErrAlreadySigned", err)
	}
}

func TestAFormSurvivesBeingSigned(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineQPDF)
	dir := t.TempDir()
	bundle := writeBundle(t, dir, newKeys(t)["ECDSA"], "pw", time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	out := filepath.Join(dir, "signed-form.pdf")

	if _, err := engines.Sign(context.Background(), pdf.SignRequest{
		Dir: dir, Input: writeFormFixture(t, dir), Output: out, Bundle: bundle, Password: "pw",
	}); err != nil {
		t.Fatalf("Sign: %v", err)
	}

	if report := pdfsig(t, out); !strings.Contains(report, "Signature is Valid") {
		t.Fatalf("pdfsig:\n%s", report)
	}

	fields, err := engines.FormFields(context.Background(), dir, out)
	if err != nil {
		t.Fatalf("FormFields: %v", err)
	}

	if _, ok := fieldsByName(fields)["name"]; !ok {
		t.Errorf("the form's own fields were lost: %+v", fields)
	}
}
