//go:build windows || purego

// Command desktop opens the web UI in a Wails v3 window.
//
// The window loads the same /tools/{name} page the browser uses. Pass the
// origin with VUKEM_TOOLS_URL (default http://127.0.0.1:3000) and the tool
// with VUKEM_TOOLS_TOOL (default pdf-crop).
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/AlexGalhardo/vukem-tools/internal/desktop"
)

// version is the release, set with -X main.version at build time.
var version = "dev"

func main() {
	base := os.Getenv("VUKEM_TOOLS_URL")
	if base == "" {
		base = desktop.DefaultOrigin
	}

	name := os.Getenv("VUKEM_TOOLS_TOOL")
	if name == "" {
		name = "pdf-crop"
	}

	page, err := desktop.ToolURL(base, name)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	open := page
	if !desktop.OriginUp(context.Background(), base) {
		status, err := desktop.StartStatus(version, page)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}

		defer func() { _ = status.Close(context.Background()) }()
		open = status.URL
	}

	label := desktop.ReleaseLabel(version)
	app := application.New(application.Options{
		Name:        "Vukem Tools",
		Description: "File conversion toolbox",
	})

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "Vukem Tools " + label,
		Width:  1200,
		Height: 800,
		URL:    open,
	})

	if err := app.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
