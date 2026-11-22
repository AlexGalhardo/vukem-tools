// Command vukem-tools is the agent entry point: every pipeline tool of the
// catalogue as a subcommand, an MCP server and a local HTTP adapter. It calls
// the Vukem Tools API and never converts a file itself.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/AlexGalhardo/vukem-tools/internal/cli"
)

// version is set at build time: -ldflags "-X main.version=v1.2.3".
var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := cli.Execute(ctx, os.Args[1:], os.Stdout, os.Stderr, cli.Options{Version: version})
	stop()
	os.Exit(code)
}
