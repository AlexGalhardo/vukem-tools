//go:build windows || purego

// Command desktop opens the web UI in a Wails v3 window.
//
// The window loads the same /tools/{name} page the browser uses. Pass the
// origin with VUKEM_TOOLS_URL (default http://127.0.0.1:8080) and the tool
// with VUKEM_TOOLS_TOOL (default pdf-crop).
package main

import (
	"fmt"
	"os"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/AlexGalhardo/vukem-tools/internal/desktop"
)

func main() {
	base := os.Getenv("VUKEM_TOOLS_URL")
	if base == "" {
		base = "http://127.0.0.1:8080"
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

	app := application.New(application.Options{
		Name:        "Vukem Tools",
		Description: "File conversion toolbox",
	})

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "Vukem Tools",
		Width:  1200,
		Height: 800,
		URL:    page,
	})

	if err := app.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
