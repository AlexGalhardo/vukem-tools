// Package httpx holds the app's own HTTP middleware.
package httpx

import (
	"net/http"
	"strings"
)

// APIKeyHeader is the header a client may send instead of
// "Authorization: Bearer <token>".
//
//nolint:gosec // G101 false positive: an HTTP header name, not a credential.
const APIKeyHeader = "X-API-Key"

// AcceptAPIKeyHeader lets a client authenticate with X-API-Key.
//
// vukem's token guard reads the token only from Authorization (auth.BearerToken
// is called directly by TokenGuard.User, with no hook for another header), so
// this middleware rewrites the request before the guard sees it. It runs on the
// API routes only.
//
// Authorization wins when both are present: a client that sends a real
// Authorization header gets the behaviour it asked for, and a stale X-API-Key
// in a shell profile or a proxy cannot silently override it.
//
// The value is copied verbatim. It is not logged, and an empty or
// whitespace-only header is ignored rather than turned into an empty Bearer
// token, which the guard would reject with the same error either way but which
// would read as "a token was sent" in a trace.
func AcceptAPIKeyHeader(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Tell caches that the response depends on both headers.
		w.Header().Add("Vary", APIKeyHeader)

		if r.Header.Get("Authorization") == "" {
			if key := strings.TrimSpace(r.Header.Get(APIKeyHeader)); key != "" {
				r.Header.Set("Authorization", "Bearer "+key)
			}
		}

		next.ServeHTTP(w, r)
	})
}

// DeployTargetHeader names the deploy that answered a request.
const DeployTargetHeader = "X-Deploy-Target"

// DeployTarget stamps every response with which deploy served it.
//
// With a web service and a worker built from one image, and Railway replacing
// containers on deploy, "which deploy answered this?" is otherwise guesswork.
// It is a plain value, never a secret: set DEPLOY_TARGET to something a human
// recognises.
func DeployTarget(target string) func(http.Handler) http.Handler {
	if target == "" {
		target = "unknown"
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set(DeployTargetHeader, target)
			next.ServeHTTP(w, r)
		})
	}
}
