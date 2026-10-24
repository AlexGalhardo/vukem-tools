package bot

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/AlexGalhardo/vukem-tools/app/abilities"
	"github.com/AlexGalhardo/vukem-tools/clients/apiclient"
	"github.com/AlexGalhardo/vukem-tools/internal/httpx"
)

// API is this service's HTTP API as the bot uses it for one signed-in person,
// narrowed to the operations its menus need.
//
// *apiclient.Client satisfies it, and the method set is the contract: the
// bot's key can reach exactly these operations, and a test checks each against
// the live route table and against the abilities the server grants the bot. A
// tool added to a menu has to appear here first.
type API interface {
	Me(ctx context.Context) (apiclient.User, error)
	Usage(ctx context.Context) (apiclient.UsageView, error)
	// TokensDestroy revokes the key making the request, which is what signing
	// out means.
	TokensDestroy(ctx context.Context) error

	UploadsIndex(ctx context.Context) (apiclient.UploadList, error)
	ConversionsShow(ctx context.Context, id string) (apiclient.ConversionView, error)
	ConversionsResult(ctx context.Context, id string) (apiclient.ResultList, error)
	ConversionsCancel(ctx context.Context, id string) (apiclient.ConversionView, error)

	ToolsImageConvert(ctx context.Context, body apiclient.ToolsImageConvertBody) (apiclient.ConversionView, error)
	ToolsImageCompress(ctx context.Context, body apiclient.ToolsImageCompressBody) (apiclient.ConversionView, error)
	ToolsPixelsToSvg(ctx context.Context, body apiclient.ToolsPixelsToSvgBody) (apiclient.ConversionView, error)
	ToolsImagesToPdf(ctx context.Context, body apiclient.ToolsImagesToPdfBody) (apiclient.ConversionView, error)
	ToolsAudioConvert(ctx context.Context, body apiclient.ToolsAudioConvertBody) (apiclient.ConversionView, error)
	ToolsVideoConvert(ctx context.Context, body apiclient.ToolsVideoConvertBody) (apiclient.ConversionView, error)
	ToolsVideoCompress(ctx context.Context, body apiclient.ToolsVideoCompressBody) (apiclient.ConversionView, error)
	ToolsVideoToGif(ctx context.Context, body apiclient.ToolsVideoToGifBody) (apiclient.ConversionView, error)
	ToolsVideoDownloadMetadata(
		ctx context.Context,
		body apiclient.ToolsVideoDownloadMetadataBody,
	) (apiclient.MediaView, error)
	ToolsVideoDownload(ctx context.Context, body apiclient.ToolsVideoDownloadBody) (apiclient.ConversionView, error)
	ToolsPdfCompress(ctx context.Context, body apiclient.ToolsPdfCompressBody) (apiclient.ConversionView, error)
	ToolsPdfToText(ctx context.Context, body apiclient.ToolsPdfToTextBody) (apiclient.ConversionView, error)
	ToolsPdfOcr(ctx context.Context, body apiclient.ToolsPdfOcrBody) (apiclient.ConversionView, error)
	ToolsPdfRepair(ctx context.Context, body apiclient.ToolsPdfRepairBody) (apiclient.ConversionView, error)
	ToolsPdfMerge(ctx context.Context, body apiclient.ToolsPdfMergeBody) (apiclient.ConversionView, error)
	ToolsWordToPdf(ctx context.Context, body apiclient.ToolsWordToPdfBody) (apiclient.ConversionView, error)
	ToolsPowerpointToPdf(ctx context.Context, body apiclient.ToolsPowerpointToPdfBody) (apiclient.ConversionView, error)
	ToolsExcelToPdf(ctx context.Context, body apiclient.ToolsExcelToPdfBody) (apiclient.ConversionView, error)
	ToolsArchiveExtract(ctx context.Context, body apiclient.ToolsArchiveExtractBody) (apiclient.ConversionView, error)
	ToolsArchiveCreate(ctx context.Context, body apiclient.ToolsArchiveCreateBody) (apiclient.ConversionView, error)
}

// Links is the part of the API a program uses before it has a key: starting a
// sign-in link and collecting the key once a person approves it. Both
// operations are public by design; the link's codes are the credential.
type Links interface {
	DevicelinksStart(ctx context.Context, body apiclient.DevicelinksStartBody) (apiclient.LinkStarted, error)
	DevicelinksToken(ctx context.Context, body apiclient.DevicelinksTokenBody) (apiclient.LinkToken, error)
}

// Uploader sends a file to the resumable upload endpoint. It is separate from
// API because tus is not part of the JSON API: no generated client has it.
type Uploader interface {
	Upload(ctx context.Context, name string, size int64, body io.Reader) error
}

// Session is everything the bot holds for one signed-in person's calls.
type Session struct {
	API      API
	Uploader Uploader
}

// Abilities is every ability the bot's keys carry, and nothing more. The
// server decides it (abilities.Telegram) when the person approves the bot;
// the bot only reports it, and tests check that its menus use all of it and
// nothing outside it.
func Abilities() []string { return abilities.Telegram() }

// apiTimeout bounds one API call. Generous because the metadata probe waits on
// a platform, and well under the conversion timeout because no single call
// should ever take that long.
const apiTimeout = 90 * time.Second

// uploadTimeout bounds sending one file. Twenty megabytes is the most the bot
// ever sends, so this only trips on a connection that has stalled.
const uploadTimeout = 5 * time.Minute

// NewLinks returns the client for the public link operations.
func NewLinks(baseURL string, base http.RoundTripper) (Links, error) {
	if strings.TrimSpace(baseURL) == "" {
		return nil, errors.New("bot: the API base URL is required")
	}

	if base == nil {
		base = http.DefaultTransport
	}

	client := apiclient.New(baseURL)
	client.HTTP = &http.Client{Transport: base, Timeout: apiTimeout}

	return client, nil
}

// NewSession returns the calls for one person's key.
func NewSession(baseURL, key string, base http.RoundTripper) (Session, error) {
	if strings.TrimSpace(baseURL) == "" {
		return Session{}, errors.New("bot: the API base URL is required")
	}

	if strings.TrimSpace(key) == "" {
		return Session{}, errors.New("bot: an API key is required")
	}

	if base == nil {
		base = http.DefaultTransport
	}

	header := func(r *http.Request) { r.Header.Set(httpx.APIKeyHeader, key) }

	client := apiclient.New(baseURL)
	client.HTTP = &http.Client{Transport: base, Timeout: apiTimeout}
	client.Header = header

	return Session{
		API: client,
		Uploader: &tusUploader{
			base:   strings.TrimRight(baseURL, "/"),
			header: header,
			http:   &http.Client{Transport: base, Timeout: uploadTimeout},
		},
	}, nil
}

// tusUploadPath is where the resumable upload endpoint is mounted
// (routes.TusPath). It is repeated here because the bot is a client of the
// HTTP surface and must not import the server's routes.
const tusUploadPath = "/uploads/tus/"

const tusVersion = "1.0.0"

// tusUploader speaks the three requests of the tus creation extension that
// the server needs: create, then one PATCH with the whole body.
type tusUploader struct {
	base   string
	header func(*http.Request)
	http   *http.Client
}

// Upload implements Uploader.
//
// The body is streamed from Telegram to the server as it arrives, never held
// whole: the size is known up front because Telegram states it, and tus wants
// it in the creation request.
func (u *tusUploader) Upload(ctx context.Context, name string, size int64, body io.Reader) error {
	if size <= 0 {
		return errors.New("bot: an upload needs a known size")
	}

	create, err := http.NewRequestWithContext(ctx, http.MethodPost, u.base+tusUploadPath, http.NoBody)
	if err != nil {
		return fmt.Errorf("bot: build the upload request: %w", err)
	}

	create.Header.Set("Tus-Resumable", tusVersion)
	create.Header.Set("Upload-Length", strconv.FormatInt(size, 10))
	create.Header.Set("Upload-Metadata", "filename "+base64.StdEncoding.EncodeToString([]byte(name)))
	u.header(create)

	res, err := u.http.Do(create)
	if err != nil {
		return fmt.Errorf("bot: start the upload: %w", err)
	}

	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
	_ = res.Body.Close()

	if res.StatusCode != http.StatusCreated {
		return &UploadError{Status: res.StatusCode}
	}

	// The location may be relative, and must end up on the API's own host: a
	// server that sent the bot's key somewhere else would be a leak.
	location, err := url.Parse(res.Header.Get("Location"))
	if err != nil || location.Path == "" {
		return errors.New("bot: the server did not say where to send the file")
	}

	target, err := url.Parse(u.base)
	if err != nil {
		return fmt.Errorf("bot: parse the API address: %w", err)
	}

	target = target.ResolveReference(location)
	if !strings.EqualFold(target.Host, mustHost(u.base)) {
		return errors.New("bot: the upload location is on another host")
	}

	patch, err := http.NewRequestWithContext(ctx, http.MethodPatch, target.String(), io.LimitReader(body, size))
	if err != nil {
		return fmt.Errorf("bot: build the upload body: %w", err)
	}

	patch.ContentLength = size
	patch.Header.Set("Tus-Resumable", tusVersion)
	patch.Header.Set("Upload-Offset", "0")
	patch.Header.Set("Content-Type", "application/offset+octet-stream")
	u.header(patch)

	res, err = u.http.Do(patch)
	if err != nil {
		return fmt.Errorf("bot: send the file: %w", err)
	}

	defer func() { _ = res.Body.Close() }()

	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))

	if res.StatusCode != http.StatusNoContent && res.StatusCode != http.StatusOK {
		return &UploadError{Status: res.StatusCode}
	}

	return nil
}

func mustHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}

	return u.Host
}

// UploadError is a refusal from the upload endpoint.
type UploadError struct{ Status int }

// Error implements error.
func (e *UploadError) Error() string {
	return "bot: the upload was refused (HTTP " + strconv.Itoa(e.Status) + ")"
}

// apiMessage is the sentence to show for an API failure.
//
// The API's own problem detail is preferred, because it is already written for
// a person and comes from the same taxonomy the web interface renders, which
// is the point of the bot having no vocabulary of its own.
func apiMessage(err error) string {
	if err == nil {
		return ""
	}

	if errors.Is(err, errStillRunning) {
		return "That is taking longer than I wait for. It is still running: ask again in a few minutes."
	}

	if upload, ok := errors.AsType[*UploadError](err); ok {
		switch upload.Status {
		case http.StatusUnauthorized:
			return "You are signed out. Sign in again from the menu."
		case http.StatusRequestEntityTooLarge:
			return "That file is too large."
		case http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity:
			return "The server cannot read that kind of file."
		case http.StatusTooManyRequests:
			return "Too many requests for now. Try again in a minute."
		}

		return "The server did not accept that file."
	}

	var apiErr *apiclient.Error
	if !errors.As(err, &apiErr) {
		return "I could not reach the server. Try again shortly."
	}

	for _, messages := range apiErr.Problem.Errors {
		if len(messages) > 0 {
			return plain(messages[0], 300)
		}
	}

	if apiErr.Problem.Detail != "" {
		return plain(apiErr.Problem.Detail, 300)
	}

	if apiErr.Status == http.StatusTooManyRequests {
		return "Too many requests for now. Try again in a minute."
	}

	return "That did not work. Try again shortly."
}

// errStillRunning is reported when a conversion outlives the bot's patience.
// The work carries on; only the bot stops watching.
var errStillRunning = errors.New("the conversion is taking longer than I wait for")
