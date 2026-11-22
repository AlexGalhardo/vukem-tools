package bootstrap

import (
	"net"
	"os"
	"strconv"

	vukem "github.com/AlexGalhardo/vukem"
)

// LocalServeOptions is what a host process (the desktop shell) passes to New to
// serve local mode from dataDir on loopback port.
//
// It does not read APP_MODE: the caller already decided. What makes the shell
// local wins over the process environment, because a window is launched from
// whatever environment the user happens to have:
//
//   - HOST and PORT, so the shell never listens beyond loopback;
//   - APP_URL, the origin the window opens, so the API reference, llms.txt and
//     the sitemap name the port that is really in use;
//   - every name LocalEnv sets (the SQLite file, the in-process worker, the
//     disk for results), so a DATABASE_URL or QUEUE_IN_SERVE left in a shell
//     cannot point the app at a Postgres or leave conversions without a worker.
//
// Every other name is the process environment first, then nothing. No .env
// file is read, so a checkout's Postgres settings cannot leak in.
func LocalServeOptions(dataDir string, port int) ([]vukem.Option, error) {
	key, err := localKey(dataDir)
	if err != nil {
		return nil, err
	}

	forced := LocalEnv(dataDir)
	forced["APP_KEY"] = key
	forced["HOST"] = "127.0.0.1"
	forced["PORT"] = strconv.Itoa(port)
	forced["APP_URL"] = "http://" + net.JoinHostPort("127.0.0.1", forced["PORT"])

	return []vukem.Option{vukem.WithEnv(os.DirFS(dataDir), func(name string) (string, bool) {
		if v, ok := forced[name]; ok {
			return v, true
		}

		// Production refuses a plain-http, loopback APP_URL and an engine runner
		// with no sandbox, and a desktop install is both. APP_ENV=local is what
		// app/tools reads to relax its own runner; vukem knows only
		// development, test and production. A production value left in the
		// launch environment is ignored for the same reason.
		if name == "APP_ENV" {
			if v, ok := os.LookupEnv(name); ok && v != "local" && v != "production" {
				return v, true
			}

			return "development", true
		}

		return os.LookupEnv(name)
	})}, nil
}
