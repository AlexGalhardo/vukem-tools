package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"time"

	"github.com/AlexGalhardo/vukem/web"
)

const (
	viaCEPBase   = "https://viacep.com.br/ws"
	brasilAPIURL = "https://brasilapi.com.br/api/cnpj/v1"

	// lookupTimeout bounds one outbound call; the page shows a retry, so a
	// slow upstream should fail fast rather than hold a worker.
	lookupTimeout = 8 * time.Second
	// lookupMaxBody is far above a CEP or CNPJ record and far below anything
	// that would hurt a small container.
	lookupMaxBody = 1 << 20
)

var (
	cepPattern  = regexp.MustCompile(`^\d{8}$`)
	cnpjPattern = regexp.MustCompile(`^[0-9A-Z]{12}\d{2}$`)
)

// errUpstream is a lookup that could not be answered by the provider.
var errUpstream = errors.New("the lookup service did not answer")

// Lookup proxies the two public Brazilian registries the developer tools use.
//
// It is a proxy rather than a browser call so the page works under the site's
// CSP (connect-src is 'self'), so a provider's outage is one error message
// here rather than a CORS failure there, and so the hosts are fixed: the path
// segment is validated to digits before it is placed in a URL, which leaves no
// way to point the request anywhere else.
type Lookup struct {
	client   *http.Client
	cepBase  string
	cnpjBase string
}

// NewLookup returns the handlers using client. A nil client gets one with a
// timeout.
func NewLookup(client *http.Client) *Lookup {
	if client == nil {
		client = &http.Client{Timeout: lookupTimeout}
	}

	return &Lookup{client: client, cepBase: viaCEPBase, cnpjBase: brasilAPIURL}
}

// CEP answers GET /lookup/cep/{cep} with ViaCEP's record.
func (l *Lookup) CEP(c *web.Ctx) error {
	cep := c.Param("cep")
	if !cepPattern.MatchString(cep) {
		return c.JSON(http.StatusUnprocessableEntity, map[string]string{"error": "A CEP is eight digits."})
	}

	body, status, err := l.get(c.Request().Context(), fmt.Sprintf("%s/%s/json/", l.cepBase, cep))
	if err != nil {
		return upstreamError(c, "ViaCEP")
	}

	// ViaCEP answers 200 with {"erro": true} for a CEP that does not exist.
	var probe struct {
		Erro any `json:"erro"`
	}
	if status == http.StatusOK && json.Unmarshal(body, &probe) == nil && probe.Erro != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "No address has that CEP."})
	}

	return passthrough(c, status, body, "No address has that CEP.")
}

// CNPJ answers GET /lookup/cnpj/{cnpj} with BrasilAPI's record.
func (l *Lookup) CNPJ(c *web.Ctx) error {
	cnpj := c.Param("cnpj")
	if !cnpjPattern.MatchString(cnpj) {
		return c.JSON(http.StatusUnprocessableEntity, map[string]string{"error": "A CNPJ is fourteen characters."})
	}

	body, status, err := l.get(c.Request().Context(), fmt.Sprintf("%s/%s", l.cnpjBase, cnpj))
	if err != nil {
		return upstreamError(c, "BrasilAPI")
	}

	return passthrough(c, status, body, "No company has that CNPJ.")
}

func (l *Lookup) get(ctx context.Context, url string) ([]byte, int, error) {
	ctx, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, 0, fmt.Errorf("lookup: build request: %w", err)
	}

	req.Header.Set("Accept", "application/json")

	resp, err := l.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("lookup: %w: %w", errUpstream, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, lookupMaxBody))
	if err != nil {
		return nil, 0, fmt.Errorf("lookup: read body: %w", err)
	}

	return body, resp.StatusCode, nil
}

// passthrough relays a provider's record, and turns its 4xx into the message
// the page shows. A provider's own error text is not forwarded.
func passthrough(c *web.Ctx, status int, body []byte, notFound string) error {
	switch {
	case status == http.StatusOK && json.Valid(body):
		c.SetHeader("Cache-Control", "private, max-age=3600")

		return c.JSON(http.StatusOK, json.RawMessage(body))
	case status == http.StatusNotFound || status == http.StatusBadRequest:
		return c.JSON(http.StatusNotFound, map[string]string{"error": notFound})
	default:
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "The lookup service failed. Try again."})
	}
}

func upstreamError(c *web.Ctx, name string) error {
	return c.JSON(http.StatusBadGateway, map[string]string{"error": name + " did not answer. Try again in a moment."})
}
