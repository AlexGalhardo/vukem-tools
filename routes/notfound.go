package routes

import (
	"net/http"
	"strings"
)

// redirectNotFound sends a browser that asked for a page which does not exist
// to the home page, instead of showing it a 404 (owner's decision,
// 2026-10-18).
//
// It applies only to a GET or HEAD for a page. Three kinds of request keep
// their real 404, because a redirect would be a worse answer:
//
//   - the JSON API and the OpenAPI document: a client needs the status code.
//     Turning 404 into "302 to an HTML page" makes a missing conversion look
//     like a successful request returning unparseable markup.
//   - the operational endpoints: a probe that follows a redirect to a page
//     that always works would report every instance healthy for ever.
//   - built assets: a missing bundle must fail loudly as a missing bundle,
//     not arrive as HTML that the browser then refuses to execute.
//
// Worth knowing, because it is the cost of the decision: Search Console
// reports a redirect from a missing page as a soft 404, so a broken inbound
// link stops being visible as a broken link. That is the trade.
func redirectNotFound(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if (r.Method != http.MethodGet && r.Method != http.MethodHead) || keepsItsStatus(r.URL.Path) {
			next.ServeHTTP(w, r)

			return
		}

		rw := &notFoundRedirector{ResponseWriter: w, req: r}
		next.ServeHTTP(rw, r)
	})
}

// keepsItsStatus reports whether path must answer with its real status.
func keepsItsStatus(path string) bool {
	for _, prefix := range []string{
		"/api/", "/openapi.json", "/docs", "/metrics", "/health", "/up", "/build/", "/assets/",
	} {
		if path == prefix || strings.HasPrefix(path, prefix) {
			return true
		}
	}

	return false
}

// notFoundRedirector turns a 404 into a redirect to the home page. It writes
// the redirect itself rather than buffering the error page, so nothing of the
// discarded body reaches the client.
type notFoundRedirector struct {
	http.ResponseWriter

	req      *http.Request
	redirect bool
	wrote    bool
}

func (w *notFoundRedirector) WriteHeader(status int) {
	if w.wrote {
		return
	}

	w.wrote = true

	if status == http.StatusNotFound {
		w.redirect = true

		// The error page's headers are not this response's: drop anything the
		// handler set for a body that is now never sent.
		h := w.Header()
		h.Del("Content-Type")
		h.Del("Content-Length")

		http.Redirect(w.ResponseWriter, w.req, "/", http.StatusFound)

		return
	}

	w.ResponseWriter.WriteHeader(status)
}

func (w *notFoundRedirector) Write(b []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}

	// Pretend the error page was written: the handler must not see a short
	// write and treat it as a broken connection.
	if w.redirect {
		return len(b), nil
	}

	return w.ResponseWriter.Write(b)
}
