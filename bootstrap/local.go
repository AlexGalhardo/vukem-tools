package bootstrap

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	vukem "github.com/AlexGalhardo/vukem"
	"github.com/AlexGalhardo/vukem/config"
)

// LocalMode is the APP_MODE value that runs the whole app in one process on
// SQLite: no Postgres, no object storage, no second worker process.
const LocalMode = "local"

const (
	dataDirName  = "vukem-tools"
	databaseFile = "vukem-tools.db"
	keyFile      = "app.key"
)

// IsLocal reports whether the process environment asks for local mode.
func IsLocal(getenv func(string) (string, bool)) bool {
	v, _ := getenv("APP_MODE")

	return strings.TrimSpace(v) == LocalMode
}

// DataDir returns the per-user directory local mode keeps everything in,
// creating it with mode 0700. It is under the OS configuration directory and
// never inside the working directory, so a stray `git add .` cannot pick up a
// database or a key.
func DataDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("bootstrap: no user configuration directory for local mode: %w", err)
	}

	dir := filepath.Join(base, dataDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("bootstrap: create the data directory: %w", err)
	}

	return dir, nil
}

// LocalEnv is the configuration local mode forces, rooted at dataDir:
//
//   - the SQLite database, with the queue, sessions and cache in it;
//   - the queue worker and the scheduler inside the serve process;
//   - uploads, scratch space and results on local disk under dataDir, with no
//     bucket (an empty S3_BUCKET overrides one left in a .env file);
//   - email and password sign-in on, because Google needs the network and a
//     .env written for the hosted service may have switched passwords off.
//
// The database URL escapes the characters a file: URI treats specially, so a
// profile directory containing them still opens.
func LocalEnv(dataDir string) map[string]string {
	return map[string]string{
		"APP_MODE":          LocalMode,
		"DATABASE_URL":      "file:" + escapeURIPath(filepath.ToSlash(filepath.Join(dataDir, databaseFile))),
		"QUEUE_CONNECTION":  "database",
		"QUEUE_IN_SERVE":    "true",
		"SCHEDULE_IN_SERVE": "true",
		"S3_BUCKET":         "",
		"PASSWORD_AUTH":     "on",
		"DATA_DIR":          dataDir,
		"STORAGE_DIR":       filepath.Join(dataDir, "files"),
		"UPLOAD_DIR":        filepath.Join(dataDir, "uploads"),
		"WORK_DIR":          filepath.Join(dataDir, "work"),
	}
}

func escapeURIPath(p string) string {
	return strings.NewReplacer("%", "%25", "?", "%3F", "#", "%23").Replace(p)
}

// localOption returns the vukem option that applies local mode, and whether
// the process environment asked for it.
//
// A variable set in the process environment still wins, so one can point a
// local run at another file; anything else comes from LocalEnv, ahead of a
// .env file written for the Postgres setup.
func localOption(getenv func(string) (string, bool)) (opt vukem.Option, local bool, err error) {
	if !IsLocal(getenv) {
		return nil, false, nil
	}

	dir, err := DataDir()
	if err != nil {
		return nil, true, err
	}

	key, err := localKey(dir)
	if err != nil {
		return nil, true, err
	}

	forced := LocalEnv(dir)
	forced["APP_KEY"] = key

	return vukem.WithEnv(os.DirFS("."), func(name string) (string, bool) {
		if v, ok := getenv(name); ok {
			return v, true
		}

		v, ok := forced[name]

		return v, ok
	}), true, nil
}

// localKey returns the application key kept in the data directory,
// generating it on first run so a fresh laptop needs no .env file. The file
// is readable by its owner only.
func localKey(dir string) (string, error) {
	path := filepath.Join(dir, keyFile)

	raw, err := os.ReadFile(path)
	if err == nil {
		if key := strings.TrimSpace(string(raw)); key != "" {
			return key, nil
		}

		return "", fmt.Errorf("bootstrap: %s is empty; delete it to generate a new key", path)
	}

	if !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("bootstrap: read the local key: %w", err)
	}

	key := config.GenerateKey()

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		// Another process started at the same moment and won the race.
		if errors.Is(err, fs.ErrExist) {
			return localKey(dir)
		}

		return "", fmt.Errorf("bootstrap: create the local key: %w", err)
	}

	if _, err := f.WriteString(key + "\n"); err != nil {
		_ = f.Close()

		return "", fmt.Errorf("bootstrap: write the local key: %w", err)
	}

	if err := f.Close(); err != nil {
		return "", fmt.Errorf("bootstrap: write the local key: %w", err)
	}

	return key, nil
}
