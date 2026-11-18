// Package cli is the vukem-tools command line.
//
// Run is the entry point tests call. It writes JSON to stdout and a JSON
// error to stderr.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/AlexGalhardo/vukem-tools/internal/agent"
	"github.com/AlexGalhardo/vukem-tools/internal/mcp"
)

// Run executes one invocation. args does not include the program name.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		agent.WriteError(stderr, errors.New("usage: vukem-tools tools | pdf crop | mcp | serve"))
		return 2
	}

	switch args[0] {
	case "tools":
		return tools(stdout, stderr)
	case "pdf":
		return pdfCmd(ctx, args[1:], stdout, stderr)
	case "mcp":
		return mcp.Serve(ctx, os.Stdin, stdout, stderr)
	case "serve":
		return serve(ctx, args[1:], stderr)
	default:
		agent.WriteError(stderr, fmt.Errorf("unknown command %q", args[0]))
		return 2
	}
}

func tools(stdout, stderr io.Writer) int {
	if err := json.NewEncoder(stdout).Encode(struct {
		Tools []agent.Tool `json:"tools"`
	}{Tools: agent.Tools()}); err != nil {
		agent.WriteError(stderr, err)
		return 1
	}

	return 0
}

func pdfCmd(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	const usage = "usage: vukem-tools pdf crop --input file --output file"

	if len(args) == 0 || args[0] != "crop" {
		agent.WriteError(stderr, errors.New(usage))
		return 2
	}

	fs := flag.NewFlagSet("pdf crop", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	input := fs.String("input", "", "PDF to crop")
	output := fs.String("output", "", "where to write the cropped PDF")
	left := fs.Float64("left", 0, "points to trim from the left")
	bottom := fs.Float64("bottom", 0, "points to trim from the bottom")
	right := fs.Float64("right", 0, "points to trim from the right")
	top := fs.Float64("top", 0, "points to trim from the top")

	if err := fs.Parse(args[1:]); err != nil {
		agent.WriteError(stderr, err)
		return 2
	}

	if *input == "" || *output == "" {
		agent.WriteError(stderr, errors.New(usage))
		return 2
	}

	margins := agent.CropMargins{Left: *left, Bottom: *bottom, Right: *right, Top: *top}

	engines, err := agent.OpenPDFEngines(ctx)
	if err != nil {
		agent.WriteError(stderr, err)
		return 1
	}

	outcome, err := agent.CropPDF(ctx, engines, *input, *output, margins)
	if err != nil {
		agent.WriteError(stderr, err)
		return 1
	}

	if err := json.NewEncoder(stdout).Encode(outcome); err != nil {
		agent.WriteError(stderr, err)
		return 1
	}

	return 0
}

func serve(ctx context.Context, args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	addr := fs.String("addr", "127.0.0.1:8787", "listen address")

	if err := fs.Parse(args); err != nil {
		agent.WriteError(stderr, err)
		return 2
	}

	if err := mcp.Listen(ctx, *addr); err != nil {
		agent.WriteError(stderr, err)
		return 1
	}

	fmt.Fprintln(stderr, "listening", strconv.Quote(*addr))

	return 0
}
