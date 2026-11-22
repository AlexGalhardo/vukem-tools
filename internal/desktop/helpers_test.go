package desktop_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	qaEmail    = "qa@example.com"
	qaPassword = "Zq7-violet-harbor-lamp-91"
)

var jsonHeader = map[string]string{"Content-Type": "application/json"}

// do sends one request the way a CLI or a browser would, without following
// redirects, and returns the status, the body and the headers.
func do(
	t *testing.T, method, target, key string, body io.Reader, headers map[string]string,
) (int, []byte, http.Header) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		t.Fatal(err)
	}

	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	return resp.StatusCode, raw, resp.Header
}

// apiKey signs the one local account up on first use and mints a key that may
// run the archive tools.
func apiKey(t *testing.T, origin string) string {
	t.Helper()

	mint := func() (int, []byte) {
		body, err := json.Marshal(map[string]any{
			"email": qaEmail, "password": qaPassword, "device_name": "qa", "abilities": []string{"archive:files"},
		})
		if err != nil {
			t.Fatal(err)
		}

		code, raw, _ := do(t, http.MethodPost, origin+"/api/v1/tokens", "", bytes.NewReader(body), jsonHeader)

		return code, raw
	}

	code, raw := mint()
	if code != http.StatusCreated {
		form := url.Values{
			"name": {"QA"}, "email": {qaEmail}, "password": {qaPassword}, "password_confirmation": {qaPassword},
		}

		code, _, _ = do(t, http.MethodPost, origin+"/register", "", strings.NewReader(form.Encode()), map[string]string{
			"Content-Type": "application/x-www-form-urlencoded", "Origin": origin, "Referer": origin + "/register",
		})
		if code != http.StatusSeeOther {
			t.Fatalf("sign-up answered %d", code)
		}

		code, raw = mint()
	}

	if code != http.StatusCreated {
		t.Fatalf("token: %d %s", code, raw)
	}

	var tok struct {
		Token string `json:"token"`
	}

	if err := json.Unmarshal(raw, &tok); err != nil {
		t.Fatal(err)
	}

	return tok.Token
}

// submitArchive uploads a PNG of side n and submits archive-create for it. It
// returns the conversion id.
func submitArchive(t *testing.T, origin, key string, n int) string {
	t.Helper()

	body := pngBytes(t, n)

	code, raw, header := do(t, http.MethodPost, origin+"/uploads/tus/", key, http.NoBody, map[string]string{
		"Tus-Resumable": "1.0.0", "Upload-Length": strconv.Itoa(len(body)),
		"Upload-Metadata": "filename " + base64.StdEncoding.EncodeToString([]byte("a.png")),
	})
	if code != http.StatusCreated {
		t.Fatalf("tus create %d %s", code, raw)
	}

	code, raw, _ = do(t, http.MethodPatch, origin+header.Get("Location"), key, bytes.NewReader(body), map[string]string{
		"Tus-Resumable": "1.0.0", "Upload-Offset": "0", "Content-Type": "application/offset+octet-stream",
	})
	if code != http.StatusNoContent {
		t.Fatalf("tus patch %d %s", code, raw)
	}

	// tus has no body to carry the stored id, so the new row is read back.
	_, raw, _ = do(t, http.MethodGet, origin+"/api/v1/uploads", key, http.NoBody, nil)

	var list struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}

	if err := json.Unmarshal(raw, &list); err != nil || len(list.Data) == 0 {
		t.Fatalf("no uploads listed (%v): %s", err, raw)
	}

	req, err := json.Marshal(map[string]any{
		"file_ids": []string{list.Data[0].ID}, "options": map[string]any{"format": "zip", "name": "b"},
	})
	if err != nil {
		t.Fatal(err)
	}

	code, raw, _ = do(t, http.MethodPost, origin+"/api/v1/tools/archive-create", key, bytes.NewReader(req), jsonHeader)
	if code != http.StatusCreated {
		t.Fatalf("submit %d %s", code, raw)
	}

	var conv struct {
		ID string `json:"id"`
	}

	if err := json.Unmarshal(raw, &conv); err != nil {
		t.Fatal(err)
	}

	return conv.ID
}

func conversionStatus(t *testing.T, origin, key, id string) string {
	t.Helper()

	_, raw, _ := do(t, http.MethodGet, origin+"/api/v1/conversions/"+id, key, http.NoBody, nil)

	var conv struct {
		Status string `json:"status"`
	}

	if err := json.Unmarshal(raw, &conv); err != nil {
		t.Fatalf("conversion %s: %v: %s", id, err, raw)
	}

	return conv.Status
}

// waitStatus polls until the conversion reaches want, or fails after 20 s.
func waitStatus(t *testing.T, origin, key, id, want string) {
	t.Helper()

	var status string

	for range 100 {
		if status = conversionStatus(t, origin, key, id); status == want {
			return
		}

		time.Sleep(200 * time.Millisecond)
	}

	t.Fatalf("conversion %s is %q, want %q", id, status, want)
}

func pngBytes(t *testing.T, n int) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, n, n))
	for i := range img.Pix {
		img.Pix[i] = byte(i * 7)
	}

	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}

	return b.Bytes()
}
