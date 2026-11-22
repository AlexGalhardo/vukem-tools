// Package cli is the vukem-tools command line.
//
// Every pipeline tool of the catalogue is a subcommand, `vukem-tools <area>
// <tool> --input FILE --output PATH`, generated from the same table the MCP
// server lists (internal/toolkit). The CLI never converts: it uploads to the
// HTTP API at VUKEM_TOOLS_URL with VUKEM_TOOLS_API_KEY, waits, and downloads
// the result.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/AlexGalhardo/vukem-tools/internal/mcp"
	"github.com/AlexGalhardo/vukem-tools/internal/toolkit"
)

// Options are the process's surroundings, so tests can run a command without
// touching the real environment.
type Options struct {
	Getenv  func(string) string
	Stdin   io.Reader
	HTTP    *http.Client
	Version string
	// Poll overrides how often a conversion is polled; zero keeps the default.
	Poll time.Duration
}

// Run executes one invocation against the real environment. args does not
// include the program name.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return Execute(ctx, args, stdout, stderr, Options{})
}

// Execute is Run with its surroundings given.
func Execute(ctx context.Context, args []string, stdout, stderr io.Writer, opt Options) int {
	if opt.Getenv == nil {
		opt.Getenv = os.Getenv
	}

	if opt.Stdin == nil {
		opt.Stdin = os.Stdin
	}

	if opt.Version == "" {
		opt.Version = "dev"
	}

	c := &command{ctx: ctx, stdout: stdout, stderr: stderr, opt: opt, json: slices.Contains(args, "--json")}

	if len(args) == 0 {
		return c.usage()
	}

	switch args[0] {
	case "version", "--version", "-version":
		_, _ = fmt.Fprintln(stdout, opt.Version)

		return 0
	case "help", "--help", "-h":
		c.help(stdout)

		return 0
	case "tools":
		return c.tools()
	case "mcp":
		return mcp.New(c.runner(), opt.Version).Serve(ctx, opt.Stdin, stdout, stderr)
	case "serve":
		return c.serve(args[1:])
	case "run":
		if len(args) < 2 {
			return c.fail(
				&toolkit.Error{Code: toolkit.CodeInvalidInput, Message: "usage: vukem-tools run <tool> [flags]"},
				2,
			)
		}

		tool, ok := toolkit.Find(args[1])
		if !ok {
			return c.fail(
				&toolkit.Error{Code: toolkit.CodeUnknownTool, Message: "unknown tool " + strconv.Quote(args[1])},
				2,
			)
		}

		return c.runTool(tool, args[2:])
	}

	if !slices.Contains(toolkit.Areas(), args[0]) {
		return c.fail(&toolkit.Error{
			Code:    toolkit.CodeUnknownTool,
			Message: fmt.Sprintf("unknown command %q; run `vukem-tools help`", args[0]),
		}, 2)
	}

	if len(args) == 1 || strings.HasPrefix(args[1], "-") {
		return c.area(args[0])
	}

	tool, ok := toolkit.ByCommand(args[0], args[1])
	if !ok {
		return c.fail(&toolkit.Error{
			Code:    toolkit.CodeUnknownTool,
			Message: fmt.Sprintf("unknown tool %q in %s; run `vukem-tools %s`", args[1], args[0], args[0]),
		}, 2)
	}

	return c.runTool(tool, args[2:])
}

type command struct {
	ctx            context.Context
	stdout, stderr io.Writer
	opt            Options
	json           bool
}

func (c *command) runner() *toolkit.Runner {
	r := toolkit.NewRunner(toolkit.ConfigFromEnv(c.opt.Getenv), c.opt.HTTP)
	if c.opt.Poll > 0 {
		r.Poll = c.opt.Poll
	}

	return r
}

func (c *command) usage() int {
	c.help(c.stderr)

	return 2
}

func (c *command) help(w io.Writer) {
	_, _ = fmt.Fprintf(w, `vukem-tools %s

Usage:
  vukem-tools <area> <tool> --input FILE --output PATH [flags]
  vukem-tools run <tool> ...      the same, by tool name
  vukem-tools <area>              list the tools of an area
  vukem-tools tools               list every tool as JSON
  vukem-tools mcp                 serve every tool over MCP on stdin and stdout
  vukem-tools serve [--addr A]    serve MCP over local HTTP
  vukem-tools version

Areas: %s

Set %s to the server (default %s) and %s to an API key. The tool runs on the
server; this command uploads the files and downloads the result. Add --json for
machine-readable output; errors then carry a stable code.
`, c.opt.Version, strings.Join(toolkit.Areas(), ", "), toolkit.EnvURL, toolkit.DefaultURL, toolkit.EnvKey)
}

func (c *command) area(area string) int {
	all := toolkit.All()

	for i := range all {
		if t := &all[i]; t.Kind == toolkit.Pipeline && t.Area == area {
			_, _ = fmt.Fprintf(c.stdout, "%-18s %s\n", t.Command, t.Summary)
		}
	}

	return 0
}

type toolEntry struct {
	Name    string         `json:"name"`
	Title   string         `json:"title"`
	Summary string         `json:"summary"`
	Group   string         `json:"group"`
	Kind    toolkit.Kind   `json:"kind"`
	Area    string         `json:"area,omitempty"`
	Command string         `json:"command,omitempty"`
	Files   *toolkit.Files `json:"files,omitempty"`
}

func (c *command) tools() int {
	all := toolkit.All()
	entries := make([]toolEntry, 0, len(all))

	for i := range all {
		t := &all[i]
		entries = append(entries, toolEntry{
			Name: t.Name, Title: t.Title, Summary: t.Summary, Group: t.Group, Kind: t.Kind,
			Area: t.Area, Command: t.Command, Files: t.Files,
		})
	}

	if err := json.NewEncoder(c.stdout).Encode(map[string]any{"tools": entries}); err != nil {
		return c.fail(toolkit.AsError(err), 1)
	}

	return 0
}

func (c *command) serve(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	addr := fs.String("addr", "127.0.0.1:8787", "listen address")

	if err := fs.Parse(args); err != nil {
		return c.fail(&toolkit.Error{Code: toolkit.CodeInvalidInput, Message: err.Error()}, 2)
	}

	err := mcp.New(c.runner(), c.opt.Version).Listen(c.ctx, *addr, func(a net.Addr) {
		_, _ = fmt.Fprintln(c.stderr, "listening on", a.String())
	})
	if err != nil {
		return c.fail(toolkit.AsError(err), 1)
	}

	return 0
}

// reserved are the flags every tool has; an option of the same name is
// reachable only through --options.
var reserved = []string{"input", "output", "options", "json", "timeout", "idempotency-key", "help"}

type optionFlag struct {
	name string
	kind string
	val  any
}

// optionFlags defines one flag per scalar option of the tool, and per list of
// strings. The rest (objects, lists of objects) go through --options.
func optionFlags(fs *flag.FlagSet, tool toolkit.Tool) []optionFlag {
	if tool.Options == nil {
		return nil
	}

	var schema struct {
		Properties map[string]struct {
			Type        string `json:"type"`
			Description string `json:"description"`
			Items       struct {
				Type string `json:"type"`
			} `json:"items"`
		} `json:"properties"`
	}

	if err := json.Unmarshal(tool.Options, &schema); err != nil {
		return nil
	}

	names := make([]string, 0, len(schema.Properties))
	for k := range schema.Properties {
		names = append(names, k)
	}

	slices.Sort(names)

	var out []optionFlag

	for _, key := range names {
		p := schema.Properties[key]
		name := strings.ReplaceAll(key, "_", "-")

		if slices.Contains(reserved, name) {
			continue
		}

		usage := p.Description

		switch {
		case p.Type == "integer":
			out = append(out, optionFlag{key, "integer", fs.Int64(name, 0, usage)})
		case p.Type == "number":
			out = append(out, optionFlag{key, "number", fs.Float64(name, 0, usage)})
		case p.Type == "boolean":
			out = append(out, optionFlag{key, "boolean", fs.Bool(name, false, usage)})
		case p.Type == "string":
			out = append(out, optionFlag{key, "string", fs.String(name, "", usage)})
		case p.Type == "array" && p.Items.Type == "string":
			list := new([]string)

			fs.Func(name, usage+" (repeat the flag for several values)", func(v string) error {
				*list = append(*list, v)

				return nil
			})

			out = append(out, optionFlag{key, "strings", list})
		}
	}

	return out
}

type listFlag []string

func (l *listFlag) String() string     { return strings.Join(*l, ",") }
func (l *listFlag) Set(v string) error { *l = append(*l, v); return nil }

func (c *command) runTool(tool toolkit.Tool, args []string) int {
	fs := flag.NewFlagSet(tool.Name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	var inputs listFlag

	fs.Var(&inputs, "input", "file to read; repeat it for a tool that takes several")

	output := fs.String("output", "", "where to write the result: a file, or a directory when there are several files")
	optionsJSON := fs.String("options", "", "options as a JSON object, for what the flags cannot express")
	fs.Bool("json", false, "print the result as JSON")

	timeout := fs.Duration("timeout", toolkit.DefaultTimeout, "how long to wait for the conversion")
	idem := fs.String("idempotency-key", "", "make a retry return the same conversion")
	extra := optionFlags(fs, tool)

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			c.toolHelp(tool, fs)

			return 0
		}

		return c.fail(&toolkit.Error{Code: toolkit.CodeInvalidInput, Message: err.Error()}, 2)
	}

	if fs.NArg() > 0 {
		return c.fail(&toolkit.Error{
			Code:    toolkit.CodeInvalidInput,
			Message: "unexpected argument " + strconv.Quote(fs.Arg(0)) + "; pass files with --input",
		}, 2)
	}

	options := map[string]any{}

	if *optionsJSON != "" {
		if err := json.Unmarshal([]byte(*optionsJSON), &options); err != nil {
			return c.fail(
				&toolkit.Error{
					Code:    toolkit.CodeInvalidInput,
					Message: "--options is not a JSON object: " + err.Error(),
				},
				2,
			)
		}
	}

	setFlags := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { setFlags[f.Name] = true })

	for _, o := range extra {
		if !setFlags[strings.ReplaceAll(o.name, "_", "-")] {
			continue
		}

		switch v := o.val.(type) {
		case *int64:
			options[o.name] = float64(*v)
		case *float64:
			options[o.name] = *v
		case *bool:
			options[o.name] = *v
		case *string:
			options[o.name] = *v
		case *[]string:
			options[o.name] = slices.Clone(*v)
		}
	}

	// A list is decoded from JSON as []any; the flag's is []string. Both are
	// normalised so the table's schema check sees one shape.
	for k, v := range options {
		if list, ok := v.([]string); ok {
			generic := make([]any, len(list))
			for i, s := range list {
				generic[i] = s
			}

			options[k] = generic
		}
	}

	if len(options) == 0 {
		options = nil
	}

	res, err := c.runner().Run(c.ctx, tool, toolkit.Call{
		Files: inputs, Options: options, Output: *output, IdempotencyKey: *idem, Timeout: *timeout,
	})
	if err != nil {
		te := toolkit.AsError(err)
		if te.Code == toolkit.CodeInvalidInput || te.Code == toolkit.CodeBrowserOnly {
			return c.fail(te, 2)
		}

		return c.fail(te, 1)
	}

	if c.json {
		if err := json.NewEncoder(c.stdout).Encode(res); err != nil {
			return c.fail(toolkit.AsError(err), 1)
		}

		return 0
	}

	for _, o := range res.Outputs {
		_, _ = fmt.Fprintf(c.stdout, "%s (%d bytes)\n", o.Path, o.SizeBytes)
	}

	if res.Note != "" {
		_, _ = fmt.Fprintln(c.stdout, res.Note)
	}

	return 0
}

func (c *command) toolHelp(tool toolkit.Tool, fs *flag.FlagSet) {
	_, _ = fmt.Fprintf(c.stdout, "vukem-tools %s %s: %s\n\n", tool.Area, tool.Command, tool.Summary)

	if tool.Description != "" {
		_, _ = fmt.Fprintf(c.stdout, "%s\n\n", tool.Description)
	}

	if tool.Files != nil {
		_, _ = fmt.Fprintf(c.stdout, "Reads %d to %d files (--input, in order).\n\n", tool.Files.Min, tool.Files.Max)
	}

	fs.SetOutput(c.stdout)
	fs.PrintDefaults()
}

// fail prints the error and returns the exit code.
func (c *command) fail(e *toolkit.Error, code int) int {
	if c.json {
		payload, err := json.Marshal(struct {
			Error *toolkit.Error `json:"error"`
		}{e})
		if err != nil {
			payload = []byte(`{"error":{"code":"server_error","message":"the error could not be encoded"}}`)
		}

		_, _ = fmt.Fprintln(c.stderr, string(payload))

		return code
	}

	_, _ = fmt.Fprintf(c.stderr, "error: %s: %s\n", e.Code, e.Message)

	return code
}
