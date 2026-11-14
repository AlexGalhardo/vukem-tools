package account_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AlexGalhardo/vukem-tools/app/controllers/account"
)

func post(body, contentType, remote string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	req.RemoteAddr = remote

	return req
}

func TestTheLoginThrottleKeyReadsTheEmailFromJSONAndForms(t *testing.T) {
	t.Parallel()

	json := post(`{"email":"  A@B.co "}`, "application/json", "203.0.113.7:1000")
	form := post("email=a%40b.co", "application/x-www-form-urlencoded", "203.0.113.7:1000")

	if a, b := account.LoginAccountThrottleKey(json), account.LoginAccountThrottleKey(form); a != b {
		t.Errorf("the same account keys differently by encoding: %q vs %q", a, b)
	}

	// The handler still has to read the body after the key was computed.
	if _, err := json.Body.Read(make([]byte, 1)); err != nil {
		t.Errorf("the body was consumed by the throttle: %v", err)
	}
}

// A body that is not JSON has no email to key on. Falling back to an empty
// email would put every malformed request from everywhere in one shared
// bucket, so it is keyed by the caller's address alone instead.
func TestAMalformedLoginBodyIsKeyedByAddressNotByASharedBucket(t *testing.T) {
	t.Parallel()

	first := post("{not json", "application/json", "203.0.113.7:1000")
	same := post("also not json", "application/json", "203.0.113.7:2000")
	other := post("{not json", "application/json", "198.51.100.9:1000")

	for name, key := range map[string]func(*http.Request) string{
		"login":         account.LoginThrottleKey,
		"login-account": account.LoginAccountThrottleKey,
		"register":      account.RegisterThrottleKey,
	} {
		if key(first) == "" {
			t.Errorf("%s: a malformed POST was left unthrottled", name)
		}

		if key(first) != key(same) {
			t.Errorf("%s: one address got two buckets for malformed bodies", name)
		}

		if key(first) == key(other) {
			t.Errorf("%s: two addresses share one bucket for malformed bodies", name)
		}
	}
}
