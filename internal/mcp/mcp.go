// Package mcp is a newline-delimited JSON-RPC server for the tool catalog.
//
// One JSON object per line on stdin, one JSON object per line on stdout.
// tools/list returns the conversion catalog. tools/call of pdf-crop crops
// every page through internal/pdf.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/AlexGalhardo/vukem-tools/internal/agent"
)

// Request is one JSON-RPC call.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
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

// Handle runs one request against the shipped catalog and crop function.
func Handle(ctx context.Context, req Request) Response {
	switch req.Method {
	case "tools/list":
		return Response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"tools": agent.Tools()}}
	case "tools/call":
		return call(ctx, req)
	default:
		return Response{JSONRPC: "2.0", ID: req.ID, Error: &CallError{Code: -32601, Message: "method not found"}}
	}
}

type callParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type cropArgs struct {
	Input  string  `json:"input"`
	Output string  `json:"output"`
	Left   float64 `json:"left"`
	Bottom float64 `json:"bottom"`
	Right  float64 `json:"right"`
	Top    float64 `json:"top"`
}

func call(ctx context.Context, req Request) Response {
	var params callParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return Response{JSONRPC: "2.0", ID: req.ID, Error: &CallError{Code: -32602, Message: err.Error()}}
	}

	if params.Name != "pdf-crop" {
		return Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &CallError{Code: -32602, Message: "unknown tool " + params.Name},
		}
	}

	var args cropArgs
	if err := json.Unmarshal(params.Arguments, &args); err != nil {
		return Response{JSONRPC: "2.0", ID: req.ID, Error: &CallError{Code: -32602, Message: err.Error()}}
	}

	engines, err := agent.OpenPDFEngines(ctx)
	if err != nil {
		return Response{JSONRPC: "2.0", ID: req.ID, Error: &CallError{Code: -32000, Message: err.Error()}}
	}

	outcome, err := agent.CropPDF(ctx, engines, args.Input, args.Output, agent.CropMargins{
		Left: args.Left, Bottom: args.Bottom, Right: args.Right, Top: args.Top,
	})
	if err != nil {
		return Response{JSONRPC: "2.0", ID: req.ID, Error: &CallError{Code: -32000, Message: err.Error()}}
	}

	return Response{JSONRPC: "2.0", ID: req.ID, Result: outcome}
}

// Serve reads newline-delimited requests until stdin ends or ctx is cancelled.
func Serve(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer) int {
	scanner := bufio.NewScanner(stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	enc := json.NewEncoder(stdout)

	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			agent.WriteError(stderr, err)
			return 1
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			if encodeErr := enc.Encode(
				Response{JSONRPC: "2.0", Error: &CallError{Code: -32700, Message: err.Error()}},
			); encodeErr != nil {
				agent.WriteError(stderr, encodeErr)
				return 1
			}
			continue
		}

		if err := enc.Encode(Handle(ctx, req)); err != nil {
			agent.WriteError(stderr, err)
			return 1
		}
	}

	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		agent.WriteError(stderr, err)
		return 1
	}

	return 0
}

// Handler is the local HTTP adapter. GET /tools lists the catalog.
// POST /pdf/crop crops every page and returns the CropOutcome JSON.
func Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /tools", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(struct {
			Tools []agent.Tool `json:"tools"`
		}{Tools: agent.Tools()}); err != nil {
			return
		}
	})
	mux.HandleFunc("POST /pdf/crop", cropHTTP)

	return mux
}

func cropHTTP(w http.ResponseWriter, r *http.Request) {
	var args cropArgs
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&args); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	engines, err := agent.OpenPDFEngines(r.Context())
	if err != nil {
		writeHTTPError(w, err)
		return
	}

	outcome, err := agent.CropPDF(r.Context(), engines, args.Input, args.Output, agent.CropMargins{
		Left: args.Left, Bottom: args.Bottom, Right: args.Right, Top: args.Top,
	})
	if err != nil {
		writeHTTPError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(outcome); err != nil {
		return
	}
}

func writeHTTPError(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnprocessableEntity)
	agent.WriteError(w, err)
}

// Listen serves Handler on addr until ctx is cancelled.
func Listen(ctx context.Context, addr string) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", addr)
	if err != nil {
		return err
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
