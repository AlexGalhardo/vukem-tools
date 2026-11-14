package httpx_test

import (
	"maps"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AlexGalhardo/vukem-tools/internal/httpx"
)

// seen records the Authorization header the wrapped handler received.
func serve(t *testing.T, mw func(http.Handler) http.Handler, header http.Header) (string, *httptest.ResponseRecorder) {
	t.Helper()

	var got string

	h := mw(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	maps.Copy(req.Header, header)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	return got, rec
}

func TestAcceptAPIKeyHeader(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		header http.Header
		want   string
	}{
		{"the key becomes a bearer token", http.Header{"X-Api-Key": {"abc"}}, "Bearer abc"},
		{"surrounding space is trimmed", http.Header{"X-Api-Key": {"  abc  "}}, "Bearer abc"},
		{
			"Authorization wins over a stale key",
			http.Header{"Authorization": {"Bearer real"}, "X-Api-Key": {"stale"}},
			"Bearer real",
		},
		{"an empty key is ignored", http.Header{"X-Api-Key": {""}}, ""},
		{"a blank key is ignored", http.Header{"X-Api-Key": {"   "}}, ""},
		{"no header, no token", http.Header{}, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, rec := serve(t, httpx.AcceptAPIKeyHeader, tc.header)
			if got != tc.want {
				t.Errorf("Authorization = %q, want %q", got, tc.want)
			}

			if rec.Header().Get("Vary") == "" {
				t.Error(
					"the response does not say it varies on the key header: a cache could serve one caller's answer to another",
				)
			}
		})
	}
}

func TestDeployTarget(t *testing.T) {
	t.Parallel()

	for target, want := range map[string]string{"web": "web", "": "unknown"} {
		_, rec := serve(t, httpx.DeployTarget(target), nil)
		if got := rec.Header().Get(httpx.DeployTargetHeader); got != want {
			t.Errorf("target %q stamped %q, want %q", target, got, want)
		}
	}
}

func TestBearerMatches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		authorization string
		want          string
		ok            bool
	}{
		{"exact", "Bearer secret", "secret", true},
		{"the scheme is case-insensitive", "bearer secret", "secret", true},
		{"wrong token", "Bearer other", "secret", false},
		{"a prefix of the token", "Bearer secre", "secret", false},
		{"wrong scheme", "Basic secret", "secret", false},
		{"no scheme", "secret", "secret", false},
		{"empty header", "", "secret", false},
		{"an empty bearer never matches an empty token", "Bearer ", "", false},
		{"no configured token matches nothing", "Bearer anything", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := httpx.BearerMatches(tc.authorization, tc.want); got != tc.ok {
				t.Errorf("BearerMatches(%q, %q) = %v, want %v", tc.authorization, tc.want, got, tc.ok)
			}
		})
	}
}
