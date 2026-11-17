//go:build !windows && !purego

// The Linux and macOS release builds pass -tags purego. Without that tag the
// Wails package wants GTK or WebKit headers, which `go build ./...` on a CI
// runner does not have. This file keeps that command compiling there.
package main

import (
	"fmt"
	"os"

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

	if _, err := desktop.ToolURL(base, name); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Fprintln(os.Stderr, "this desktop build needs -tags purego")
	os.Exit(1)
}
