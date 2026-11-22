//go:build windows || purego

// Command desktop opens the web UI in a Wails v3 window.
//
// By default it starts the app in this process (local mode, SQLite, a random
// loopback port), shows it, and stops it when the window closes. Set
// VUKEM_TOOLS_URL to show a server that is already running instead; nothing is
// started then. VUKEM_TOOLS_TOOL opens a tool page rather than the home page.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/AlexGalhardo/vukem-tools/bootstrap"
	"github.com/AlexGalhardo/vukem-tools/internal/desktop"
)

// version is the release, set with -X main.version at build time.
var version = "dev"

// stopTimeout bounds the drain of the worker and the close of the database.
const stopTimeout = 30 * time.Second

func main() {
	// os.Exit lives here so the deferred stop of the server still runs.
	os.Exit(run())
}

// run returns the process status. The server is stopped when it returns.
func run() int {
	opts := desktop.OpenOptions{Version: version}

	if os.Getenv("VUKEM_TOOLS_URL") == "" {
		dir, err := bootstrap.DataDir()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}

		opts.DataDir = dir

		// The engine runner reads APP_ENV itself and refuses to start without a
		// sandbox in production; a desktop install is not production.
		if _, ok := os.LookupEnv("APP_ENV"); !ok {
			if err := os.Setenv("APP_ENV", "local"); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
		}
	}

	session, err := desktop.Open(context.Background(), os.Getenv, opts)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), stopTimeout)
		defer cancel()

		if err := session.Stop(ctx); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	}()

	app := application.New(application.Options{
		Name:        "Vukem Tools",
		Description: "File conversion toolbox",
	})

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "Vukem Tools " + desktop.ReleaseLabel(version),
		Width:  1200,
		Height: 800,
		URL:    session.URL,
	})

	// A server that dies on its own leaves a window that cannot work: close it.
	if done := session.Done(); done != nil {
		go func() {
			<-done
			app.Quit()
		}()
	}

	if err := app.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	return 0
}
