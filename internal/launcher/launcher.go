// Package launcher decides whether an install may move forward.
//
// It never downloads. The caller supplies the feed and the artifact bytes.
// A checksum that does not match returns before the install hook runs.
package launcher

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// ErrNotNewer means the feed has no release newer than the one installed.
var ErrNotNewer = errors.New("launcher: no newer release")

// ErrChecksum means the artifact bytes do not match the feed.
var ErrChecksum = errors.New("launcher: checksum does not match")

// Release is one published build in a local fixture feed.
type Release struct {
	Tag       string `json:"tag"`
	Version   string `json:"version"`
	AssetName string `json:"asset_name"`
	SHA256    string `json:"sha256"`
	URL       string `json:"url"`
}

// Select returns the newest release that is strictly newer than current.
//
// List order does not matter: an older build listed last is not chosen.
func Select(current string, releases []Release) (Release, error) {
	currentN, err := parse(current)
	if err != nil {
		return Release{}, err
	}

	var (
		found bool
		best  Release
		bestN version
	)

	for _, rel := range releases {
		n, err := parse(rel.Version)
		if err != nil {
			return Release{}, fmt.Errorf("release %s: %w", rel.Tag, err)
		}

		if !n.newerThan(currentN) {
			continue
		}

		if !found || n.newerThan(bestN) {
			found = true
			best = rel
			bestN = n
		}
	}

	if !found {
		return Release{}, ErrNotNewer
	}

	return best, nil
}

// VerifyAndInstall hashes r while writing it to a temp file. The install hook
// runs only after the digest matches rel.SHA256. A mismatch deletes the file
// and does not call install.
func VerifyAndInstall(rel Release, r io.Reader, install func(path string) error) error {
	if install == nil {
		return errors.New("launcher: install hook is required")
	}

	file, err := os.CreateTemp("", "vukem-update-*")
	if err != nil {
		return err
	}

	name := file.Name()
	hash := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(file, hash), r)
	closeErr := file.Close()

	if copyErr != nil {
		_ = os.Remove(name)
		return copyErr
	}

	if closeErr != nil {
		_ = os.Remove(name)
		return closeErr
	}

	got := hex.EncodeToString(hash.Sum(nil))
	if !strings.EqualFold(got, strings.TrimSpace(rel.SHA256)) {
		_ = os.Remove(name)
		return ErrChecksum
	}

	if err := install(name); err != nil {
		_ = os.Remove(name)
		return err
	}

	return nil
}

type version [3]int

func parse(raw string) (version, error) {
	raw = strings.TrimPrefix(strings.TrimSpace(raw), "v")
	parts := strings.Split(raw, ".")

	if len(parts) != 3 {
		return version{}, fmt.Errorf("version %q is not major.minor.patch", raw)
	}

	var out version

	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return version{}, fmt.Errorf("version %q is not major.minor.patch", raw)
		}

		out[i] = n
	}

	return out, nil
}

func (v version) newerThan(other version) bool {
	for i := range v {
		if v[i] != other[i] {
			return v[i] > other[i]
		}
	}

	return false
}
