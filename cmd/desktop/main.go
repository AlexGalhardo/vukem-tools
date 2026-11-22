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
			return failed(err, "")
		}

		opts.DataDir = dir

		// The engine runner reads APP_ENV itself and refuses to start without a
		// sandbox in production; a desktop install is not production, whatever
		// the environment the app was launched from says.
		if os.Getenv("APP_ENV") != "testing" {
			if err := os.Setenv("APP_ENV", "local"); err != nil {
				return failed(err, dir)
			}
		}
	}

	session, err := desktop.Open(context.Background(), os.Getenv, opts)
	if err != nil {
		return failed(err, opts.DataDir)
	}

	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), stopTimeout)
		defer cancel()

		if err := session.Stop(ctx); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	}()

	if err := showWindow(session.URL, session.Done()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	return 0
}

// failed tells the person why the app could not start. The Windows build has no
// console, so printing to stderr alone would close the app with nothing on
// screen: the reason goes in a window as well.
func failed(cause error, dataDir string) int {
	fmt.Fprintln(os.Stderr, cause)

	page, err := desktop.StartFailure(version, cause, dataDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	defer func() { _ = page.Close(context.Background()) }()

	if err := showWindow(page.URL, nil); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}

	return 1
}

// showWindow opens url in a window and returns when it closes. A server that
// dies on its own leaves a window that cannot work, so done closes it.
func showWindow(url string, done <-chan struct{}) error {
	app := application.New(application.Options{
		Name:        "Vukem Tools",
		Description: "File conversion toolbox",
	})

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "Vukem Tools " + desktop.ReleaseLabel(version),
		Width:  1200,
		Height: 800,
		URL:    url,
	})

	if done != nil {
		go func() {
			<-done
			app.Quit()
		}()
	}

	return app.Run()
}
