package controllers_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AlexGalhardo/vukem/web"

	"github.com/AlexGalhardo/vukem-tools/app/controllers"
)

// get calls a handler with a path value, which the shared call helper cannot set.
func get(h func(*web.Ctx) error, key, value string) (*httptest.ResponseRecorder, error) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/lookup", http.NoBody)
	req.SetPathValue(key, value)

	return rec, h(web.NewCtx(rec, req, nil))
}

// rewrite sends every request to the test server, whatever host the handler built.
type rewrite struct{ to string }

func (r rewrite) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = "http"
	req.URL.Host = strings.TrimPrefix(r.to, "http://")

	return http.DefaultTransport.RoundTrip(req)
}

func upstream(t *testing.T, h http.HandlerFunc) *controllers.Lookup {
	t.Helper()

	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	return controllers.NewLookup(&http.Client{Transport: rewrite{to: srv.URL}})
}

func TestACEPIsRelayedAndAnUnknownOneIsA404(t *testing.T) {
	t.Parallel()

	l := upstream(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "99999999") {
			_, _ = w.Write([]byte(`{"erro":"true"}`))

			return
		}

		_, _ = w.Write([]byte(`{"cep":"01310-100","localidade":"São Paulo"}`))
	})

	rec, err := get(l.CEP, "cep", "01310100")
	if err != nil || rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "São Paulo") {
		t.Fatalf("known CEP: err=%v status=%d body=%s", err, rec.Code, rec.Body)
	}

	rec, err = get(l.CEP, "cep", "99999999")
	if err != nil || rec.Code != http.StatusNotFound {
		t.Fatalf("unknown CEP: err=%v status=%d, want 404", err, rec.Code)
	}
}

// The path value goes into a URL, so anything but digits must be refused
// before a request is made.
func TestALookupRefusesAnythingButDigitsAndMakesNoRequest(t *testing.T) {
	t.Parallel()

	called := false
	l := upstream(t, func(http.ResponseWriter, *http.Request) { called = true })

	for _, bad := range []string{"1234567", "0131010a", "../../x", "01310100/../../etc"} {
		rec, err := get(l.CEP, "cep", bad)
		if err != nil || rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("CEP %q: err=%v status=%d, want 422", bad, err, rec.Code)
		}
	}

	if rec, _ := get(l.CNPJ, "cnpj", "123"); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("short CNPJ: status=%d, want 422", rec.Code)
	}

	if called {
		t.Fatal("a refused value still reached the provider")
	}
}

func TestACNPJFailureDoesNotLeakTheProvidersBody(t *testing.T) {
	t.Parallel()

	l := upstream(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("stack trace with secret-token"))
	})

	rec, err := get(l.CNPJ, "cnpj", "11222333000181")
	if err != nil || rec.Code != http.StatusBadGateway {
		t.Fatalf("err=%v status=%d, want 502", err, rec.Code)
	}

	if strings.Contains(rec.Body.String(), "secret-token") {
		t.Fatalf("the provider's body was forwarded: %s", rec.Body)
	}
}
