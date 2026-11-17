// Command vukem-tools is the agent entry point: catalog, PDF crop, MCP and
// the local HTTP adapter.
package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/AlexGalhardo/vukem-tools/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := cli.Run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}
