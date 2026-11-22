package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AlexGalhardo/vukem-tools/app/middleware"
)

func TestLoopbackHost(t *testing.T) {
	t.Parallel()

	h := middleware.LoopbackHost("Tools.Example.com")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	for host, want := range map[string]int{
		"localhost":             http.StatusNoContent,
		"LocalHost:3000":        http.StatusNoContent,
		"localhost.":            http.StatusNoContent,
		"127.0.0.1":             http.StatusNoContent,
		"127.0.0.1:8080":        http.StatusNoContent,
		"127.9.9.9":             http.StatusNoContent,
		"[::1]":                 http.StatusNoContent,
		"[::1]:8080":            http.StatusNoContent,
		"tools.example.com:443": http.StatusNoContent,
		"":                      http.StatusForbidden,
		"evil.test":             http.StatusForbidden,
		"localhost.evil.test":   http.StatusForbidden,
		"127.0.0.1.evil.test":   http.StatusForbidden,
		"0.0.0.0":               http.StatusForbidden,
		"192.168.1.10:3000":     http.StatusForbidden,
		"[::ffff:192.168.1.1]":  http.StatusForbidden,
		"localhost@evil.test":   http.StatusForbidden,
	} {
		r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
		r.Host = host

		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)

		if rec.Code != want {
			t.Errorf("Host %q: status %d, want %d", host, rec.Code, want)
		}
	}
}
