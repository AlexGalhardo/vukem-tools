package desktop

import (
	"context"
	"html/template"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// ReleaseLabel is the version printed on the window. An empty build is "dev".
func ReleaseLabel(version string) string {
	version = strings.TrimSpace(version)
	if version == "" || version == "dev" {
		return "dev"
	}

	if strings.HasPrefix(version, "v") {
		return version
	}

	return "v" + version
}

// OriginUp reports whether base answers an HTTP request before the deadline.
// Any status counts: a login redirect still means the app is there.
func OriginUp(ctx context.Context, base string) bool {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/"), http.NoBody)
	if err != nil {
		return false
	}

	client := &http.Client{
		Timeout: 2 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return false
	}

	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<10))

	return true
}

// Status is a page on 127.0.0.1 that explains the web app is not running.
type Status struct {
	URL   string
	close func(context.Context) error
}

// Close stops the status server.
func (s Status) Close(ctx context.Context) error {
	if s.close == nil {
		return nil
	}

	return s.close(ctx)
}

type statusData struct {
	Release string
	Target  string
	Origin  string
}

var statusPage = template.Must(template.New("status").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Vukem Tools {{.Release}}</title>
<style>
  body { margin: 0; min-height: 100vh; background: #161616; color: #f4f4f5;
    font: 16px/1.5 system-ui, sans-serif; display: grid; place-items: center; }
  main { max-width: 36rem; padding: 2rem; }
  h1 { font-size: 1.5rem; font-weight: 600; }
  a { color: #c4b5fd; }
  .release { position: fixed; left: 12px; bottom: 12px; margin: 0;
    font-size: 12px; color: #a1a1aa; }
</style>
</head>
<body>
<main>
  <h1>The web app is not running</h1>
  <p>This window opens the same pages as the browser. Nothing is listening at
  <a href="{{.Origin}}">{{.Origin}}</a>, so the tool cannot load.</p>
  <p>Start the server, then open the tool. From a checkout of this repository:</p>
  <pre>docker compose up -d --build --wait</pre>
  <p><a href="{{.Target}}">Open the tool</a></p>
</main>
<p class="release">{{.Release}}</p>
</body>
</html>
`))

// StartStatus serves the offline page until Close. target is the tool URL the
// link opens. The release is fixed at the bottom left of the page.
func StartStatus(version, target string) (Status, error) {
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		return Status{}, err
	}

	origin := strings.TrimRight(target, "/")
	if i := strings.Index(origin, "/tools/"); i >= 0 {
		origin = origin[:i]
	}

	data := statusData{Release: ReleaseLabel(version), Target: target, Origin: origin}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = statusPage.Execute(w, data)
	})

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() { _ = srv.Serve(ln) }()

	return Status{
		URL: "http://" + ln.Addr().String() + "/",
		close: func(ctx context.Context) error {
			return srv.Shutdown(ctx)
		},
	}, nil
}
