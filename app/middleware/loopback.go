// Package middleware holds the HTTP middleware that belongs to this app and
// not to the framework.
package middleware

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// LoopbackHost refuses a request whose Host is neither a loopback address nor
// one of extra, with 403.
//
// Local mode listens on loopback only, which stops other machines but not a
// web page: a page on another origin can rebind its own name to 127.0.0.1 and
// then talk to this server with the visitor's browser, and the browser sends
// that page's name as Host. Checking Host closes that. The session cookie and
// CSRF rules are the second line, not the first.
//
// extra holds host names without a port (the host of APP_URL); it is compared
// case-insensitively.
//
// workaround(vukem): VF-001 — the framework's middleware.LoopbackOnly is
// stricter (peer, proxy headers, exact port) but vukemtest.Client cannot send
// a loopback Host for a relative path, so the suites could not run behind it.
func LoopbackHost(extra ...string) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(extra))

	for _, h := range extra {
		if h = normaliseHost(h); h != "" {
			allowed[h] = struct{}{}
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host := normaliseHost(r.Host)

			if _, ok := allowed[host]; !ok && !isLoopback(host) {
				http.Error(w, "forbidden: this server answers on loopback only", http.StatusForbidden)

				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// normaliseHost drops the port and the brackets of an IPv6 literal and lower
// cases the name. A trailing dot (an absolute DNS name) is dropped too.
func normaliseHost(hostport string) string {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}

	host = strings.TrimSuffix(strings.Trim(host, "[]"), ".")

	return strings.ToLower(host)
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}

	ip, err := netip.ParseAddr(host)

	return err == nil && ip.IsLoopback()
}
