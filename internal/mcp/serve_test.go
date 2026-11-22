package mcp_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/AlexGalhardo/vukem-tools/internal/mcp"
	"github.com/AlexGalhardo/vukem-tools/internal/toolkit"
)

// stuckAPI never answers the first call a conversion makes, so a tools/call
// stays in flight until its request is cancelled.
func stuckAPI(t *testing.T) *mcp.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	t.Cleanup(srv.Close)

	return mcp.New(toolkit.NewRunner(toolkit.Config{BaseURL: srv.URL, Key: "vk_k.secret"}, nil), "test")
}

type session struct {
	in    *io.PipeWriter
	lines chan map[string]any
	done  chan int
}

func start(t *testing.T, srv *mcp.Server) *session {
	t.Helper()

	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	s := &session{in: inW, lines: make(chan map[string]any, 16), done: make(chan int, 1)}

	t.Cleanup(func() { _ = inW.Close() })

	go func() {
		s.done <- srv.Serve(t.Context(), inR, outW, io.Discard)
		_ = outW.Close()
	}()

	go func() {
		sc := bufio.NewScanner(outR)
		for sc.Scan() {
			var m map[string]any
			if json.Unmarshal(sc.Bytes(), &m) == nil {
				s.lines <- m
			}
		}
	}()

	return s
}

func (s *session) send(t *testing.T, msg string) {
	t.Helper()

	if _, err := io.WriteString(s.in, msg+"\n"); err != nil {
		t.Fatal(err)
	}
}

func (s *session) next(t *testing.T, within time.Duration) map[string]any {
	t.Helper()

	select {
	case m := <-s.lines:
		return m
	case <-time.After(within):
		t.Fatalf("no answer within %v", within)

		return nil
	}
}

// QA-C4: a long tools/call held the read loop, so a ping sent meanwhile was
// not answered until the conversion ended and a client that pings gave up.
func TestAPingIsAnsweredWhileAToolCallRuns(t *testing.T) {
	t.Parallel()

	in := filepath.Join(t.TempDir(), "in.png")
	if err := os.WriteFile(in, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}

	s := start(t, stuckAPI(t))
	s.send(t, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"pixels-to-svg","arguments":{"input":`+
		jsonString(in)+`,"output":`+jsonString(filepath.Join(t.TempDir(), "o.svg"))+`}}}`)
	s.send(t, `{"jsonrpc":"2.0","id":2,"method":"ping"}`)

	if got := s.next(t, 3*time.Second); got["id"] != float64(2) {
		t.Fatalf("first answer = %v, want the ping (id 2) while the call runs", got)
	}

	// notifications/cancelled ends the call, which then answers with a code.
	s.send(t, `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":1}}`)

	got := s.next(t, 5*time.Second)
	if got["id"] != float64(1) {
		t.Fatalf("answer = %v, want the cancelled call (id 1)", got)
	}
}

// QA-C5: SIGINT and SIGTERM cancel the context, and the loop sat in a read of
// stdin, so the process could not be stopped from a terminal.
func TestServeReturnsWhenItsContextIsCancelledWithStdinOpen(t *testing.T) {
	t.Parallel()

	inR, inW := io.Pipe()
	t.Cleanup(func() { _ = inW.Close() })

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan int, 1)

	go func() { done <- stuckAPI(t).Serve(ctx, inR, io.Discard, io.Discard) }()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Serve did not return after its context was cancelled")
	}
}

func jsonString(s string) string { return strconv.Quote(s) }

func TestMalformedAndBatchLinesGetProtocolErrors(t *testing.T) {
	t.Parallel()

	s := start(t, stuckAPI(t))

	for line, want := range map[string]float64{
		`{"jsonrpc":`: -32700,
		`[{"jsonrpc":"2.0","id":1,"method":"ping"}]`: -32600,
		`{"jsonrpc":"2.0","id":7,"method":"nope"}`:   -32601,
	} {
		s.send(t, line)

		got := s.next(t, 3*time.Second)

		errObj, _ := got["error"].(map[string]any)
		if errObj["code"] != want {
			t.Errorf("%s -> %v, want code %v", line, got, want)
		}
	}
}

// QA-C6: the adapter reads and writes local files for whoever reaches it, and
// its Host check is no defence against a peer that sends "Host: localhost".
// It therefore must not listen on anything but the loopback.
func TestTheLocalAdapterRefusesANonLoopbackAddress(t *testing.T) {
	t.Parallel()

	for _, addr := range []string{":0", "0.0.0.0:0", "[::]:0", "192.168.1.10:0", "example.com:0"} {
		err := stuckAPI(t).Listen(t.Context(), addr, func(net.Addr) { t.Errorf("%s: listening", addr) })
		if err == nil {
			t.Errorf("%s: Listen returned nil, want a refusal", addr)
		}
	}

	ctx, cancel := context.WithCancel(t.Context())
	ready := make(chan struct{})
	done := make(chan error, 1)

	go func() { done <- stuckAPI(t).Listen(ctx, "127.0.0.1:0", func(net.Addr) { close(ready) }) }()

	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("a loopback address was refused: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("never listened on the loopback")
	}

	cancel()
	<-done
}
