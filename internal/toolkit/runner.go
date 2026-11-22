package toolkit

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/AlexGalhardo/vukem-tools/clients/apiclient"
)

// Environment variables the CLI and the MCP server read.
const (
	EnvURL = "VUKEM_TOOLS_URL"
	EnvKey = "VUKEM_TOOLS_API_KEY"
)

// DefaultURL is where a local `vukem-tools` server listens.
const DefaultURL = "http://localhost:3000"

const (
	tusPath    = "/uploads/tus/"
	tusVersion = "1.0.0"
	// maxBody bounds any JSON answer read from the server.
	maxBody = 1 << 20
	// DefaultTimeout is how long a call waits for a conversion to end.
	DefaultTimeout = 10 * time.Minute
)

// Config is where the API is and the key to call it with. The key lives in
// memory only: it is never printed, logged or written.
type Config struct {
	BaseURL string
	Key     string
}

// ConfigFromEnv reads the configuration through getenv.
func ConfigFromEnv(getenv func(string) string) Config {
	base := strings.TrimSpace(getenv(EnvURL))
	if base == "" {
		base = DefaultURL
	}

	return Config{BaseURL: strings.TrimRight(base, "/"), Key: strings.TrimSpace(getenv(EnvKey))}
}

// Runner calls tools through the HTTP API.
type Runner struct {
	cfg  Config
	http *http.Client
	api  *apiclient.Client
	// Poll is how often a running conversion is asked about.
	Poll time.Duration
}

// NewRunner returns a runner for cfg. hc may be nil.
func NewRunner(cfg Config, hc *http.Client) *Runner {
	if hc == nil {
		// No overall timeout: an upload or a video result can legitimately
		// take minutes, and every call is bound by its context instead.
		hc = &http.Client{}
	}

	wrapped := *hc

	base := wrapped.Transport
	if base == nil {
		base = http.DefaultTransport
	}

	wrapped.Transport = rateLimitRetry{base: base}
	hc = &wrapped

	api := apiclient.New(cfg.BaseURL)
	api.HTTP = hc
	api.Header = func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+cfg.Key) }

	return &Runner{cfg: cfg, http: hc, api: api, Poll: 500 * time.Millisecond}
}

// Call is one invocation of a tool.
type Call struct {
	// Files are local paths, in the order the tool reads them.
	Files   []string
	Options map[string]any
	// Output is a file path, or a directory when the tool yields several
	// files.
	Output         string
	IdempotencyKey string
	// Timeout bounds the wait for the conversion; DefaultTimeout when zero.
	Timeout time.Duration
}

// Output is one file written to disk.
type Output struct {
	Path      string `json:"path"`
	Name      string `json:"name"`
	Mime      string `json:"mime,omitempty"`
	SizeBytes int64  `json:"size_bytes"`
}

// Result is what a finished call reports.
type Result struct {
	Tool         string     `json:"tool"`
	ConversionID string     `json:"conversion_id"`
	Status       string     `json:"status"`
	Outputs      []Output   `json:"outputs"`
	BytesIn      int64      `json:"bytes_in,omitempty"`
	BytesOut     int64      `json:"bytes_out,omitempty"`
	Note         string     `json:"note,omitempty"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
}

// Run uploads the files, submits the conversion, waits for it and downloads
// the result at once, because a result lives only minutes.
func (r *Runner) Run(ctx context.Context, tool Tool, call Call) (Result, error) {
	if err := Validate(tool, call.Files, call.Options); err != nil {
		return Result{}, err
	}

	if strings.TrimSpace(call.Output) == "" {
		return Result{}, invalid("output is required: a file path, or a directory for a tool that yields several files")
	}

	if r.cfg.Key == "" {
		return Result{}, &Error{
			Code:    CodeMissingKey,
			Message: "No API key. Set " + EnvKey + " to a key that may run " + tool.Name + ".",
		}
	}

	sizes := make([]int64, len(call.Files))

	for i, path := range call.Files {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			return Result{}, invalid("input " + strconv.Quote(path) + " is not a readable file")
		}

		if info.Size() == 0 {
			return Result{}, invalid("input " + strconv.Quote(path) + " is empty")
		}

		sizes[i] = info.Size()
	}

	ids := make([]string, len(call.Files))

	for i, path := range call.Files {
		id, err := r.upload(ctx, path, sizes[i])
		if err != nil {
			return Result{}, r.scrub(err)
		}

		ids[i] = id
	}

	view, err := r.submit(ctx, tool, ids, call)
	if err != nil {
		return Result{}, r.scrub(err)
	}

	view, err = r.wait(ctx, tool, view, call.Timeout)
	if err != nil {
		return Result{}, r.scrub(err)
	}

	res := Result{
		Tool: tool.Name, ConversionID: deref(view.ID), Status: deref(view.Status),
		BytesIn: deref(view.BytesIn), BytesOut: deref(view.BytesOut), Note: deref(view.Note), ExpiresAt: view.ExpiresAt,
	}

	res.Outputs, err = r.download(ctx, res.ConversionID, call.Output)
	if err != nil {
		if te, ok := errors.AsType[*Error](err); ok {
			te.ConversionID = res.ConversionID
		}

		return Result{}, r.scrub(err)
	}

	return res, nil
}

// upload sends one file with the tus creation extension and returns the id
// the server gave it. The body is streamed from disk, never held whole.
func (r *Runner) upload(ctx context.Context, path string, size int64) (string, error) {
	before, err := r.api.UploadsIndex(ctx)
	if err != nil {
		return "", r.fromAPIError(err)
	}

	known := map[string]bool{}
	for _, u := range before.Data {
		known[deref(u.ID)] = true
	}

	started := time.Now().Add(-time.Minute)

	create, err := http.NewRequestWithContext(ctx, http.MethodPost, r.cfg.BaseURL+tusPath, http.NoBody)
	if err != nil {
		return "", &Error{Code: CodeUploadFailed, Message: "could not build the upload request"}
	}

	create.Header.Set("Tus-Resumable", tusVersion)
	create.Header.Set("Upload-Length", strconv.FormatInt(size, 10))
	create.Header.Set("Upload-Metadata", "filename "+base64.StdEncoding.EncodeToString([]byte(filepath.Base(path))))
	r.sign(create)

	res, err := r.http.Do(create)
	if err != nil {
		return "", networkError("start the upload", err)
	}

	body, _ := io.ReadAll(io.LimitReader(res.Body, maxBody))
	_ = res.Body.Close()

	if res.StatusCode != http.StatusCreated {
		return "", uploadRefusal(res.StatusCode, body)
	}

	// The location must stay on the API's host: sending the key anywhere else
	// would leak it.
	base, err := url.Parse(r.cfg.BaseURL)
	if err != nil {
		return "", invalid("VUKEM_TOOLS_URL is not a URL")
	}

	loc, err := url.Parse(res.Header.Get("Location"))
	if err != nil || loc.Path == "" {
		return "", &Error{Code: CodeUploadFailed, Message: "the server did not say where to send the file"}
	}

	target := base.ResolveReference(loc)
	if !strings.EqualFold(target.Host, base.Host) {
		return "", &Error{Code: CodeUploadFailed, Message: "the server pointed the upload at another host; refused"}
	}

	file, err := os.Open(path)
	if err != nil {
		return "", &Error{Code: CodeIO, Message: "could not open " + strconv.Quote(path)}
	}
	defer func() { _ = file.Close() }()

	patch, err := http.NewRequestWithContext(ctx, http.MethodPatch, target.String(), io.LimitReader(file, size))
	if err != nil {
		return "", &Error{Code: CodeUploadFailed, Message: "could not build the upload body"}
	}

	patch.ContentLength = size
	// A refused upload (429) is sent again from the start of the file.
	patch.GetBody = func() (io.ReadCloser, error) {
		again, err := os.Open(path)
		if err != nil {
			return nil, err
		}

		return &limitedFile{Reader: io.LimitReader(again, size), file: again}, nil
	}
	patch.Header.Set("Tus-Resumable", tusVersion)
	patch.Header.Set("Upload-Offset", "0")
	patch.Header.Set("Content-Type", "application/offset+octet-stream")
	r.sign(patch)

	res, err = r.http.Do(patch)
	if err != nil {
		return "", networkError("send the file", err)
	}

	body, _ = io.ReadAll(io.LimitReader(res.Body, maxBody))
	_ = res.Body.Close()

	if res.StatusCode != http.StatusNoContent && res.StatusCode != http.StatusOK {
		return "", uploadRefusal(res.StatusCode, body)
	}

	// tus has no body to carry the stored id, so the new row is read back.
	after, err := r.api.UploadsIndex(ctx)
	if err != nil {
		return "", r.fromAPIError(err)
	}

	for _, u := range after.Data {
		id := deref(u.ID)
		if id == "" || known[id] || deref(u.SizeBytes) != size {
			continue
		}

		if u.CreatedAt != nil && u.CreatedAt.Before(started) {
			continue
		}

		return id, nil
	}

	return "", &Error{
		Code:    CodeUploadFailed,
		Message: "the upload finished but the server lists no new file; it may have been refused or expired",
	}
}

func uploadRefusal(status int, body []byte) *Error {
	e := fromResponse(status, body)
	if e.Code == CodeServer || e.Code == CodeNotFound {
		e.Code = CodeUploadFailed
	}

	return e
}

// submit posts the conversion. The generated client has one method per tool,
// so the call is made generically here and a test checks that every table
// entry has its generated method.
func (r *Runner) submit(ctx context.Context, tool Tool, ids []string, call Call) (apiclient.ConversionView, error) {
	body := map[string]any{}

	if tool.Files != nil {
		if tool.Files.Field == "file_id" {
			body["file_id"] = ids[0]
		} else {
			body["file_ids"] = ids
		}
	}

	if len(call.Options) > 0 || tool.OptionsRequired {
		opts := call.Options
		if opts == nil {
			opts = map[string]any{}
		}

		body["options"] = opts
	}

	if call.IdempotencyKey != "" {
		body["idempotency_key"] = call.IdempotencyKey
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return apiclient.ConversionView{}, invalid("the options cannot be encoded as JSON")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		r.cfg.BaseURL+"/api/v1/tools/"+tool.Name, bytes.NewReader(raw))
	if err != nil {
		return apiclient.ConversionView{}, invalid("VUKEM_TOOLS_URL is not a URL")
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	r.sign(req)

	res, err := r.http.Do(req)
	if err != nil {
		return apiclient.ConversionView{}, networkError("submit the conversion", err)
	}

	defer func() { _ = res.Body.Close() }()

	answer, _ := io.ReadAll(io.LimitReader(res.Body, maxBody))

	if res.StatusCode != http.StatusCreated && res.StatusCode != http.StatusOK {
		return apiclient.ConversionView{}, fromResponse(res.StatusCode, answer)
	}

	var view apiclient.ConversionView
	if err := json.Unmarshal(answer, &view); err != nil || deref(view.ID) == "" {
		return apiclient.ConversionView{}, &Error{
			Code:    CodeServer,
			Message: "the server's answer to the submission was not a conversion",
		}
	}

	return view, nil
}

func (r *Runner) wait(
	ctx context.Context, tool Tool, view apiclient.ConversionView, timeout time.Duration,
) (apiclient.ConversionView, error) {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	id := deref(view.ID)
	deadline := time.NewTimer(timeout)

	defer deadline.Stop()

	// The API allows about two requests a second per client, so a long
	// conversion is polled less and less often instead of at a fixed rate.
	interval := r.Poll
	limit := r.Poll * 6

	tick := time.NewTimer(interval)
	defer tick.Stop()

	for {
		switch deref(view.Status) {
		case "finished":
			return view, nil
		case "error":
			return view, &Error{
				Code: CodeConversion, ConversionType: deref(view.ErrorType), ConversionID: id,
				Message: strings.TrimSpace(deref(view.ErrorMessage) + " (" + tool.Name + " failed)"),
			}
		case "cancelled":
			return view, &Error{Code: CodeCancelled, ConversionID: id, Message: "the conversion was cancelled"}
		}

		select {
		case <-ctx.Done():
			return view, r.stop(id, ctx.Err())
		case <-deadline.C:
			return view, r.stop(id, context.DeadlineExceeded)
		case <-tick.C:
		}

		interval = min(interval*3/2, limit)
		tick.Reset(interval)

		next, err := r.api.ConversionsShow(ctx, id)
		if err != nil {
			return view, r.fromAPIError(err)
		}

		view = next
	}
}

// stop cancels a conversion nobody is waiting for any more and reports why.
func (r *Runner) stop(id string, cause error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, _ = r.api.ConversionsCancel(ctx, id)

	if errors.Is(cause, context.DeadlineExceeded) {
		return &Error{
			Code: CodeTimeout, ConversionID: id, Retry: true,
			Message: "the conversion did not finish in time and was cancelled; try a smaller file or a longer timeout",
		}
	}

	return &Error{
		Code:         CodeCancelled,
		ConversionID: id,
		Message:      "the call was interrupted and the conversion cancelled",
	}
}

// download fetches every result file to disk. One file goes to output (or into
// it, when output is a directory); several go into the directory output.
func (r *Runner) download(ctx context.Context, id, output string) ([]Output, error) {
	list, err := r.api.ConversionsResult(ctx, id)
	if err != nil {
		return nil, r.fromAPIError(err)
	}

	if len(list.Data) == 0 {
		return nil, &Error{Code: CodeServer, Message: "the conversion finished with no result file"}
	}

	toDir := len(list.Data) > 1
	if info, err := os.Stat(output); err == nil && info.IsDir() {
		toDir = true
	}

	if toDir {
		if err := os.MkdirAll(output, 0o750); err != nil {
			return nil, &Error{Code: CodeIO, Message: "could not create the output directory " + strconv.Quote(output)}
		}
	} else if err := os.MkdirAll(filepath.Dir(output), 0o750); err != nil {
		return nil, &Error{Code: CodeIO, Message: "could not create the folder of " + strconv.Quote(output)}
	}

	outs := make([]Output, 0, len(list.Data))

	for i, f := range list.Data {
		name := filepath.Base(deref(f.Name))
		if name == "." || name == string(filepath.Separator) || name == "" {
			name = "result-" + strconv.Itoa(i+1)
		}

		path := output
		if toDir {
			path = filepath.Join(output, name)
		}

		n, err := r.fetch(ctx, deref(f.URL), path)
		if err != nil {
			return outs, err
		}

		outs = append(outs, Output{Path: path, Name: name, Mime: deref(f.Mime), SizeBytes: n})
	}

	return outs, nil
}

func (r *Runner) fetch(ctx context.Context, link, path string) (int64, error) {
	base, err := url.Parse(r.cfg.BaseURL)
	if err != nil {
		return 0, invalid("VUKEM_TOOLS_URL is not a URL")
	}

	target, err := url.Parse(link)
	if err != nil {
		return 0, &Error{Code: CodeServer, Message: "the server sent a result link that is not a URL"}
	}

	// Only the path and query of the link are used, on the host this runner
	// was configured with: the server builds links from its own public
	// address, which behind a proxy or a local port is not where this runner
	// reaches it, and following a host the server names is not needed.
	target = base.ResolveReference(&url.URL{Path: target.Path, RawPath: target.RawPath, RawQuery: target.RawQuery})

	// The link is signed, so it needs no key and none is sent.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), http.NoBody)
	if err != nil {
		return 0, &Error{Code: CodeServer, Message: "could not build the download request"}
	}

	res, err := r.http.Do(req)
	if err != nil {
		return 0, networkError("download the result", err)
	}

	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(res.Body, maxBody))
		if res.StatusCode == http.StatusNotFound || res.StatusCode == http.StatusGone ||
			res.StatusCode == http.StatusForbidden {
			return 0, &Error{
				Code: CodeResultExpired, Retry: true,
				Message: "the result link expired or was removed; run the tool again for a new one",
			}
		}

		return 0, fromResponse(res.StatusCode, body)
	}

	// The result goes to a temporary file beside the target and is renamed
	// into place only when it is whole, so a download that breaks half way
	// leaves an existing file at the target as it was.
	out, err := os.CreateTemp(filepath.Dir(path), ".vukem-*.part")
	if err != nil {
		return 0, &Error{Code: CodeIO, Message: "could not write " + strconv.Quote(path)}
	}

	n, copyErr := io.Copy(out, res.Body)
	closeErr := out.Close()

	if copyErr != nil || closeErr != nil {
		_ = os.Remove(out.Name())

		if copyErr != nil {
			return 0, networkError("download the result", copyErr)
		}

		return 0, &Error{Code: CodeIO, Message: "could not finish writing " + strconv.Quote(path)}
	}

	if err := os.Rename(out.Name(), path); err != nil {
		_ = os.Remove(out.Name())

		return 0, &Error{Code: CodeIO, Message: "could not write " + strconv.Quote(path)}
	}

	return n, nil
}

func (r *Runner) sign(req *http.Request) { req.Header.Set("Authorization", "Bearer "+r.cfg.Key) }

// fromAPIError maps an error of the generated client.
func (r *Runner) fromAPIError(err error) error {
	if ae, ok := errors.AsType[*apiclient.Error](err); ok {
		return fromProblem(ae.Status, ae.Problem)
	}

	return networkError("call the API", err)
}

func networkError(doing string, err error) *Error {
	if errors.Is(err, context.Canceled) {
		return &Error{Code: CodeCancelled, Message: "the call was interrupted while trying to " + doing}
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return &Error{Code: CodeTimeout, Retry: true, Message: "timed out while trying to " + doing}
	}

	return &Error{
		Code: CodeNetwork, Retry: true,
		Message: "could not " + doing + ": the API is unreachable (check " + EnvURL + ")",
	}
}

// scrub is the last line of defence for the key: whatever the error says, it
// does not say the key.
func (r *Runner) scrub(err error) error {
	e := asError(err)
	if r.cfg.Key != "" {
		e.Message = strings.ReplaceAll(e.Message, r.cfg.Key, "[redacted]")
	}

	return e
}

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}

	return *p
}
