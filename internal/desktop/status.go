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
	origin := strings.TrimRight(target, "/")
	if i := strings.Index(origin, "/tools/"); i >= 0 {
		origin = origin[:i]
	}

	data := statusData{Release: ReleaseLabel(version), Target: target, Origin: origin}

	return servePage(func(w io.Writer) error { return statusPage.Execute(w, data) })
}

// servePage serves one HTML page on a loopback port until the Status is closed.
func servePage(render func(io.Writer) error) (Status, error) {
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		return Status{}, err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = render(w)
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

type failureData struct {
	Release string
	Reason  string
	DataDir string
}

var failurePage = template.Must(template.New("failure").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Vukem Tools {{.Release}}</title>
<style>
  body { margin: 0; min-height: 100vh; background: #161616; color: #f4f4f5;
    font: 16px/1.5 system-ui, sans-serif; display: grid; place-items: center; }
  main { max-width: 40rem; padding: 2rem; }
  h1 { font-size: 1.5rem; font-weight: 600; }
  pre { white-space: pre-wrap; overflow-wrap: anywhere; background: #262626; padding: 1rem; border-radius: 6px; }
  .release { position: fixed; left: 12px; bottom: 12px; margin: 0;
    font-size: 12px; color: #a1a1aa; }
</style>
</head>
<body>
<main>
  <h1>Vukem Tools could not start</h1>
  <p>The app starts its own server on this computer, and that did not work:</p>
  <pre>{{.Reason}}</pre>
  {{if .DataDir}}<p>Your files and settings are in <code>{{.DataDir}}</code>. Nothing there was changed or
  deleted. Check that the folder exists and that you can write to it, then open the app again.</p>{{end}}
  <p>Close this window to quit.</p>
</main>
<p class="release">{{.Release}}</p>
</body>
</html>
`))

// StartFailure serves a page that says why the app could not start, until
// Close. The Windows build has no console to print the error to, so the window
// is the only place the person can read it. dataDir may be empty.
func StartFailure(version string, cause error, dataDir string) (Status, error) {
	reason := "unknown error"
	if cause != nil {
		reason = cause.Error()
	}

	data := failureData{Release: ReleaseLabel(version), Reason: reason, DataDir: dataDir}

	return servePage(func(w io.Writer) error { return failurePage.Execute(w, data) })
}
