package bootstrap

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func lookup(env map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := env[k]

		return v, ok
	}
}

func TestOnlyAppModeLocalSelectsLocalMode(t *testing.T) {
	t.Parallel()

	for env, want := range map[string]bool{
		"local":      true,
		"":           false,
		"production": false,
		"LOCAL":      false,
	} {
		if got := IsLocal(lookup(map[string]string{"APP_MODE": env})); got != want {
			t.Errorf("APP_MODE=%q: IsLocal = %v, want %v", env, got, want)
		}
	}

	if IsLocal(lookup(nil)) {
		t.Error("an unset APP_MODE selected local mode")
	}
}

func TestLocalEnvKeepsEverythingUnderTheDataDirectory(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "data")
	env := LocalEnv(dir)

	if !strings.HasPrefix(env["DATABASE_URL"], "file:") || !strings.HasSuffix(env["DATABASE_URL"], "/vukem-tools.db") {
		t.Errorf("DATABASE_URL = %q, want a SQLite file URL", env["DATABASE_URL"])
	}

	for _, k := range []string{"STORAGE_DIR", "UPLOAD_DIR", "WORK_DIR"} {
		if !strings.HasPrefix(env[k], dir+string(filepath.Separator)) {
			t.Errorf("%s = %q is outside %q", k, env[k], dir)
		}
	}

	if env["S3_BUCKET"] != "" {
		t.Errorf("S3_BUCKET = %q, local mode has no bucket", env["S3_BUCKET"])
	}

	if env["QUEUE_CONNECTION"] != "database" || env["QUEUE_IN_SERVE"] != "true" {
		t.Errorf("the worker must run in the serve process on the database queue: %v", env)
	}
}

func TestTheDatabaseURLEscapesWhatAFileURIReads(t *testing.T) {
	t.Parallel()

	got := LocalEnv(`/home/a b/100%/x#y?z`)["DATABASE_URL"]
	if strings.ContainsAny(strings.TrimPrefix(got, "file:"), "?#") || !strings.Contains(got, "100%25") {
		t.Errorf("DATABASE_URL = %q, want %%, # and ? escaped", got)
	}
}

// A data directory is private to its owner and is never inside the working
// directory.
func TestDataDirIsPrivateAndOutsideTheWorkingDirectory(t *testing.T) {
	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	t.Setenv("APPDATA", config)
	t.Setenv("HOME", config)

	dir, err := DataDir()
	if err != nil {
		t.Fatal(err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	if strings.HasPrefix(dir, wd) {
		t.Errorf("data directory %q is inside the working directory %q", dir, wd)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}

	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o700 {
		t.Errorf("data directory mode = %v, want 0700", info.Mode().Perm())
	}
}

func TestTheLocalKeyIsGeneratedOnceAndKeptPrivate(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	first, err := localKey(dir)
	if err != nil {
		t.Fatal(err)
	}

	if first == "" {
		t.Fatal("empty key")
	}

	second, err := localKey(dir)
	if err != nil {
		t.Fatal(err)
	}

	if first != second {
		t.Error("the key changed between runs, which would end every session")
	}

	info, err := os.Stat(filepath.Join(dir, keyFile))
	if err != nil {
		t.Fatal(err)
	}

	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("key file mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestANonLocalRunGetsNoLocalOption(t *testing.T) {
	t.Parallel()

	_, local, err := localOption(lookup(map[string]string{"APP_MODE": "production"}))
	if err != nil || local {
		t.Fatalf("localOption = %v, %v, want not local", local, err)
	}
}
