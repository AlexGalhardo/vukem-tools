// Package mcp is a Model Context Protocol server for every tool of the
// catalogue.
//
// It speaks newline-delimited JSON-RPC on stdin and stdout, and offers the
// same over local HTTP. tools/list is read from internal/toolkit, the table
// the CLI reads too, and tools/call runs a tool through the HTTP API: the
// server never converts a file itself.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/AlexGalhardo/vukem-tools/internal/toolkit"
)

// defaultProtocol is answered when a client names no version.
const defaultProtocol = "2025-06-18"

// Request is one JSON-RPC message.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response is one JSON-RPC result.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *CallError      `json:"error,omitempty"`
}

// CallError is a JSON-RPC error.
type CallError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Tool is one entry of tools/list.
type Tool struct {
	Name        string         `json:"name"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Meta        map[string]any `json:"_meta"`
}

// Server answers MCP requests by calling the API through a runner.
type Server struct {
	runner  *toolkit.Runner
	version string
}

// New returns a server. version is reported to clients in initialize.
func New(runner *toolkit.Runner, version string) *Server {
	return &Server{runner: runner, version: version}
}

// Tools is what tools/list returns: every tool of the table.
func Tools() []Tool {
	all := toolkit.All()
	out := make([]Tool, 0, len(all))

	for i := range all {
		out = append(out, describe(&all[i]))
	}

	return out
}

func describe(t *toolkit.Tool) Tool {
	meta := map[string]any{"kind": string(t.Kind), "group": t.Group}
	desc := t.Summary

	if t.Kind == toolkit.Browser {
		desc += " This tool runs in the browser on the site and cannot be called here."

		return Tool{
			Name: t.Name, Title: t.Title, Description: desc, Meta: meta,
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		}
	}

	meta["area"], meta["command"] = t.Area, t.Command

	if t.Description != "" {
		desc += " " + t.Description
	}

	return Tool{Name: t.Name, Title: t.Title, Description: desc, Meta: meta, InputSchema: inputSchema(*t)}
}

// inputSchema is the arguments of tools/call: where to read, where to write
// and the options the API publishes for the tool.
func inputSchema(t toolkit.Tool) map[string]any {
	props := map[string]any{
		"output": map[string]any{
			"type": "string",
			"description": "Where to write the result: a file path, or a directory for a tool that " +
				"yields several files. The result is downloaded at once because it expires in minutes.",
		},
	}
	required := []any{"output"}

	switch {
	case t.Files == nil:
	case t.Files.Max == 1:
		props["input"] = map[string]any{"type": "string", "description": "Path of the file to read."}
		required = append(required, "input")
	default:
		props["inputs"] = map[string]any{
			"type": "array", "items": map[string]any{"type": "string"},
			"minItems": t.Files.Min, "maxItems": t.Files.Max,
			"description": "Paths of the files to read, in the order the tool uses them.",
		}
		required = append(required, "inputs")
	}

	if t.Options != nil {
		var opts map[string]any
		if err := json.Unmarshal(t.Options, &opts); err == nil {
			props["options"] = opts

			if t.OptionsRequired {
				required = append(required, "options")
			}
		}
	}

	props["idempotency_key"] = map[string]any{
		"type": "string", "maxLength": 128,
		"description": "Repeat a call safely: the same key returns the same conversion.",
	}

	return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
}

// Handle runs one message. The bool is false for a notification, which gets
// no answer.
func (s *Server) Handle(ctx context.Context, req Request) (Response, bool) {
	reply := func(result any) (Response, bool) {
		return Response{JSONRPC: "2.0", ID: req.ID, Result: result}, len(req.ID) > 0
	}

	fail := func(code int, msg string) (Response, bool) {
		return Response{JSONRPC: "2.0", ID: req.ID, Error: &CallError{Code: code, Message: msg}}, len(req.ID) > 0
	}

	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}

		_ = json.Unmarshal(req.Params, &p)

		if p.ProtocolVersion == "" {
			p.ProtocolVersion = defaultProtocol
		}

		return reply(map[string]any{
			"protocolVersion": p.ProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "vukem-tools", "version": s.version},
		})
	case "ping":
		return reply(map[string]any{})
	case "tools/list":
		return reply(map[string]any{"tools": Tools()})
	case "tools/call":
		result, rpcErr := s.call(ctx, req.Params)
		if rpcErr != nil {
			return fail(rpcErr.Code, rpcErr.Message)
		}

		return reply(result)
	}

	if strings.HasPrefix(req.Method, "notifications/") {
		return Response{}, false
	}

	return fail(-32601, "method not found")
}

type callParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type arguments struct {
	Input          string         `json:"input"`
	Inputs         []string       `json:"inputs"`
	Output         string         `json:"output"`
	Options        map[string]any `json:"options"`
	IdempotencyKey string         `json:"idempotency_key"`
}

func (s *Server) call(ctx context.Context, raw json.RawMessage) (any, *CallError) {
	var params callParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, &CallError{Code: -32602, Message: "invalid params: " + err.Error()}
	}

	tool, ok := toolkit.Find(params.Name)
	if !ok {
		return nil, &CallError{Code: -32602, Message: "unknown tool " + params.Name}
	}

	if tool.Kind == toolkit.Browser {
		return failure(&toolkit.Error{
			Code:    toolkit.CodeBrowserOnly,
			Message: tool.Name + " runs in the browser on the site; it has no call here",
		}), nil
	}

	var args arguments

	dec := json.NewDecoder(bytes.NewReader(params.Arguments))
	dec.DisallowUnknownFields()

	if len(params.Arguments) > 0 {
		if err := dec.Decode(&args); err != nil {
			return failure(&toolkit.Error{Code: toolkit.CodeInvalidInput, Message: "arguments: " + err.Error()}), nil
		}
	}

	files := args.Inputs
	if args.Input != "" {
		files = append([]string{args.Input}, files...)
	}

	res, err := s.runner.Run(ctx, tool, toolkit.Call{
		Files: files, Options: args.Options, Output: args.Output, IdempotencyKey: args.IdempotencyKey,
	})
	if err != nil {
		return failure(toolkit.AsError(err)), nil
	}

	text := encode(res)

	return map[string]any{
		"content":           []any{map[string]any{"type": "text", "text": text}},
		"structuredContent": res,
		"isError":           false,
	}, nil
}

// failure is a tool error, which MCP reports in the result so the model sees
// it, rather than as a protocol error.
func failure(e *toolkit.Error) map[string]any {
	text := encode(e)

	return map[string]any{
		"content":           []any{map[string]any{"type": "text", "text": text}},
		"structuredContent": map[string]any{"error": e},
		"isError":           true,
	}
}

// Serve reads newline-delimited requests until stdin ends or ctx is cancelled.
func (s *Server) Serve(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer) int {
	scanner := bufio.NewScanner(stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)

	enc := json.NewEncoder(stdout)

	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			writeErr(stderr, err)

			return 1
		}

		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}

		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			bad := Response{
				JSONRPC: "2.0",
				ID:      json.RawMessage("null"),
				Error:   &CallError{Code: -32700, Message: err.Error()},
			}
			if encodeErr := enc.Encode(bad); encodeErr != nil {
				writeErr(stderr, encodeErr)

				return 1
			}

			continue
		}

		resp, send := s.Handle(ctx, req)
		if !send {
			continue
		}

		if err := enc.Encode(resp); err != nil {
			writeErr(stderr, err)

			return 1
		}
	}

	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		writeErr(stderr, err)

		return 1
	}

	return 0
}

func writeErr(w io.Writer, err error) {
	_, _ = w.Write([]byte(encode(map[string]any{"error": map[string]any{
		"code": toolkit.CodeServer, "message": err.Error(),
	}}) + "\n"))
}

// encode renders v as JSON for a text block. Everything passed here is built
// from strings and numbers, so the fallback is a guard, not a path.
func encode(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return `{"code":"server_error","message":"the result could not be encoded"}`
	}

	return string(raw)
}

// Handler is the local HTTP adapter: GET /tools lists the tools and
// POST /mcp takes one JSON-RPC message.
//
// It is for the machine it runs on. A web page could otherwise make this
// process read local files and spend the API key through the visitor's
// browser, so it answers only requests addressed to a loopback host, refuses
// any request that carries an Origin header, and wants a JSON body.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /tools", func(w http.ResponseWriter, r *http.Request) {
		if !local(w, r) {
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"tools": Tools()})
	})

	mux.HandleFunc("POST /mcp", func(w http.ResponseWriter, r *http.Request) {
		if !local(w, r) {
			return
		}

		if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			http.Error(w, "send application/json", http.StatusUnsupportedMediaType)

			return
		}

		var req Request
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
			http.Error(w, "not a JSON-RPC message", http.StatusBadRequest)

			return
		}

		resp, send := s.Handle(r.Context(), req)
		if !send {
			w.WriteHeader(http.StatusAccepted)

			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	return mux
}

func local(w http.ResponseWriter, r *http.Request) bool {
	host := r.Host
	if h, _, err := net.SplitHostPort(r.Host); err == nil {
		host = h
	}

	if r.Header.Get("Origin") != "" || !slices.Contains([]string{"127.0.0.1", "localhost", "::1", "[::1]"}, host) {
		http.Error(w, "this adapter answers local programs only", http.StatusForbidden)

		return false
	}

	return true
}

// Listen serves Handler on addr until ctx is cancelled.
func (s *Server) Listen(ctx context.Context, addr string, ready func(net.Addr)) error {
	srv := &http.Server{Addr: addr, Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second}

	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", addr)
	if err != nil {
		return err
	}

	if ready != nil {
		ready(ln.Addr())
	}

	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()

	err = srv.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}

	return fmt.Errorf("mcp http: %w", err)
}
