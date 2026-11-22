package ingest_test

import (
	"archive/zip"
	"bytes"
	"os"
	"testing"

	"github.com/AlexGalhardo/vukem/upload"

	"github.com/AlexGalhardo/vukem-tools/internal/jobs"
	"github.com/AlexGalhardo/vukem-tools/internal/testenv"
)

// eicar is the standard anti-malware test string. It is harmless by design and
// every scanner reports it, so it proves the wiring without any real malware.
const eicar = `X5O!P%@AP[4\PZX54(P^)7CC)7}$EICAR-STANDARD-ANTIVIRUS-TEST-FILE!$H+H*`

func zipOf(t *testing.T, name, content string) []byte {
	t.Helper()

	var buf bytes.Buffer

	zw := zip.NewWriter(&buf)

	w, err := zw.Create(name)
	if err != nil {
		t.Fatalf("zip member: %v", err)
	}

	if _, err := w.Write([]byte(content)); err != nil {
		t.Fatalf("zip write: %v", err)
	}

	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}

	return buf.Bytes()
}

// The audit's H2: ClamAV must run on every upload. This talks to a real clamd,
// named by CLAMAV_ADDR, because a fake scanner proves only that Accept honours
// a refusal, not that the production scanner produces one.
//
// The test string rides in a ZIP member. The detector runs before the scanner
// and would refuse a bare text file as an unsupported type, and ClamAV's EICAR
// signature is anchored at the start of a file, so a PNG with the string
// appended is not reported. ClamAV opens archives and scans their members,
// which is also how real malware reaches an accepted type.
func TestARealClamdRefusesTheEICARTestString(t *testing.T) {
	addr := os.Getenv("CLAMAV_ADDR")
	if addr == "" {
		testenv.MissingEngine(
			t,
			"CLAMAV_ADDR is not set; run a clamd (compose.yaml has one) and point it at the host:port",
		)
	}

	scanner := upload.ClamAV(addr)
	s := store(t, 0)
	ing := ingester(t, s, scanner, 1<<20)

	infected := zipOf(t, "eicar.txt", eicar)

	_, err := ing.Accept(t.Context(), owner, info("a.zip", len(infected)), open(infected))
	if err == nil {
		t.Fatal("an upload carrying the EICAR test string was accepted")
	}

	if got := errType(t, err); got != jobs.Infected {
		t.Fatalf("refused as %s, want %s (%v)", got, jobs.Infected, err)
	}

	if stored, err := s.StoredBytes(t.Context(), owner); err != nil || stored != 0 {
		t.Errorf("the refused upload left %d bytes stored (%v)", stored, err)
	}

	// The same scanner must let a clean file through, or the refusal above
	// proves only that clamd refuses everything.
	clean := zipOf(t, "hello.txt", "hello")

	if _, err := ing.Accept(t.Context(), owner, info("b.zip", len(clean)), open(clean)); err != nil {
		t.Fatalf("a clean upload was refused by the real scanner: %v", err)
	}
}

// A clamd that cannot be reached is a refusal, not a pass: the way to get an
// infected file through must not be to knock the scanner over.
func TestAnUnreachableClamdRefusesTheUpload(t *testing.T) {
	t.Parallel()

	// Port 1 on loopback is closed everywhere the tests run.
	ing := ingester(t, store(t, 0), upload.ClamAV("127.0.0.1:1"), 1<<20)

	_, err := ing.Accept(t.Context(), owner, info("a.png", len(pngBytes)), open(pngBytes))
	if err == nil {
		t.Fatal("an upload was accepted while the scanner was unreachable")
	}

	if got := errType(t, err); got != jobs.Internal {
		t.Fatalf("refused as %s, want %s (%v)", got, jobs.Internal, err)
	}
}

// An upload past the size limit is refused before any byte is scanned or
// stored, so an oversized file cannot be used to keep clamd busy.
func TestAnOversizedUploadIsRefusedBeforeTheScanner(t *testing.T) {
	t.Parallel()

	sc := &scanner{}
	ing := ingester(t, store(t, 0), sc, int64(len(pngBytes))-1)

	_, err := ing.Accept(t.Context(), owner, info("a.png", len(pngBytes)), open(pngBytes))
	if err == nil || errType(t, err) != jobs.InputTooLarge {
		t.Fatalf("an oversized upload: %v", err)
	}

	if sc.calls != 0 {
		t.Errorf("the scanner ran %d times on an upload refused for its size", sc.calls)
	}
}
