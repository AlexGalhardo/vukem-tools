package toolkit

import (
	"io"
	"net/http"
	"os"
	"strconv"
	"time"
)

const (
	// maxRetries is how many times a refused request is sent again.
	maxRetries = 5
	// maxRetryWait caps one wait, whatever Retry-After says.
	maxRetryWait = 15 * time.Second
)

// rateLimitRetry sends a request again, after the wait the server asked for,
// when the API answers 429. The API admits a couple of requests a second per
// client, and a conversion that polls for a minute would otherwise be
// abandoned half way, still running on the server.
//
// A request whose body cannot be replayed (a streamed upload) is not retried
// here: its 429 is returned for the caller to report.
type rateLimitRetry struct {
	base http.RoundTripper
}

func (t rateLimitRetry) RoundTrip(req *http.Request) (*http.Response, error) {
	for attempt := 0; ; attempt++ {
		res, err := t.base.RoundTrip(req)
		if err != nil || res.StatusCode != http.StatusTooManyRequests || attempt >= maxRetries {
			return res, err
		}

		replayable := req.Body == nil || req.Body == http.NoBody || req.GetBody != nil
		if !replayable {
			return res, nil
		}

		wait := retryAfter(res.Header.Get("Retry-After"))

		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, maxBody))
		_ = res.Body.Close()

		timer := time.NewTimer(wait)
		select {
		case <-req.Context().Done():
			timer.Stop()

			return nil, req.Context().Err()
		case <-timer.C:
		}

		if req.GetBody != nil {
			body, err := req.GetBody()
			if err != nil {
				return nil, err
			}

			req = req.Clone(req.Context())
			req.Body = body
		}
	}
}

// retryAfter reads Retry-After as seconds; anything else waits one second.
func retryAfter(v string) time.Duration {
	secs, err := strconv.Atoi(v)
	if err != nil || secs < 1 {
		return time.Second
	}

	return min(time.Duration(secs)*time.Second, maxRetryWait)
}

// limitedFile is a request body that closes the file it reads.
type limitedFile struct {
	io.Reader
	file *os.File
}

func (l *limitedFile) Close() error { return l.file.Close() }
