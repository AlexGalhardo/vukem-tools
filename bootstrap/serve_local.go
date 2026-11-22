package bootstrap

import (
	"os"

	vukem "github.com/AlexGalhardo/vukem"
)

// LocalServeOptions is what a host process (the desktop shell) passes to New to
// serve local mode from dataDir on a loopback port the kernel chooses.
//
// It does not read APP_MODE: the caller already decided. HOST and PORT are
// fixed here and win over the process environment, because the shell must never
// listen beyond loopback or on a port something else holds. Every other name is
// the process environment first, then LocalEnv, as in local mode under serve.
// No .env file is read, so a checkout's Postgres settings cannot leak in.
func LocalServeOptions(dataDir string) ([]vukem.Option, error) {
	key, err := localKey(dataDir)
	if err != nil {
		return nil, err
	}

	forced := LocalEnv(dataDir)
	forced["APP_KEY"] = key

	fixed := map[string]string{"HOST": "127.0.0.1", "PORT": "0"}

	return []vukem.Option{vukem.WithEnv(os.DirFS(dataDir), func(name string) (string, bool) {
		if v, ok := fixed[name]; ok {
			return v, true
		}

		// Production refuses a plain-http, loopback APP_URL and an engine runner
		// with no sandbox, and a desktop install is both. APP_ENV=local is what
		// app/tools reads to relax its own runner; vukem knows only
		// development, test and production.
		if name == "APP_ENV" {
			if v, ok := os.LookupEnv(name); ok && v != "local" {
				return v, true
			}

			return "development", true
		}

		if v, ok := os.LookupEnv(name); ok {
			return v, true
		}

		v, ok := forced[name]

		return v, ok
	})}, nil
}
