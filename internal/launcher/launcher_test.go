package launcher_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"testing"

	"github.com/AlexGalhardo/vukem-tools/internal/launcher"
)

func TestSelectOnlyMovesForward(t *testing.T) {
	t.Parallel()

	feed := []launcher.Release{
		{Tag: "v1.0.0", Version: "1.0.0", AssetName: "old.exe", SHA256: "aa"},
		{Tag: "v1.2.0", Version: "1.2.0", AssetName: "new.exe", SHA256: "bb"},
		{Tag: "v0.9.0", Version: "0.9.0", AssetName: "older.exe", SHA256: "cc"},
	}

	got, err := launcher.Select("1.1.0", feed)
	if err != nil {
		t.Fatalf("Select = %v", err)
	}

	if got.Version != "1.2.0" {
		t.Fatalf("selected %s, want 1.2.0 (an older build is listed last)", got.Version)
	}
}

func TestSelectRefusesADowngrade(t *testing.T) {
	t.Parallel()

	_, err := launcher.Select("2.0.0", []launcher.Release{
		{Tag: "v1.9.0", Version: "1.9.0"},
		{Tag: "v2.0.0", Version: "2.0.0"},
	})
	if !errors.Is(err, launcher.ErrNotNewer) {
		t.Fatalf("Select = %v, want ErrNotNewer", err)
	}
}

func TestVerifyRefusesABadChecksumBeforeInstall(t *testing.T) {
	t.Parallel()

	called := false
	err := launcher.VerifyAndInstall(
		launcher.Release{SHA256: "deadbeef"},
		bytes.NewReader([]byte("artifact")),
		func(string) error {
			called = true
			return nil
		},
	)

	if !errors.Is(err, launcher.ErrChecksum) {
		t.Fatalf("VerifyAndInstall = %v, want ErrChecksum", err)
	}

	if called {
		t.Fatal("install hook ran after a bad checksum")
	}
}

func TestVerifyInstallsOnlyWhenTheChecksumMatches(t *testing.T) {
	t.Parallel()

	body := []byte("vukem-tools-binary")
	sum := sha256.Sum256(body)
	var got []byte

	err := launcher.VerifyAndInstall(
		launcher.Release{SHA256: hex.EncodeToString(sum[:]), Version: "1.2.0"},
		bytes.NewReader(body),
		func(path string) error {
			var readErr error
			got, readErr = os.ReadFile(path)

			return readErr
		},
	)
	if err != nil {
		t.Fatalf("VerifyAndInstall = %v", err)
	}

	if !bytes.Equal(got, body) {
		t.Fatalf("installed bytes = %q, want the artifact", got)
	}
}
