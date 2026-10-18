package services

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"

	"github.com/AlexGalhardo/vukem"
	"github.com/AlexGalhardo/vukem/drivers/otel"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	promexporter "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/AlexGalhardo/vukem-tools/internal/telemetry"
)

// Telemetry wires OpenTelemetry and the conversion metrics.
//
// It is a provider of its own, registered before Conversions, because the
// driver installs the HTTP middleware and the database and queue observers:
// a provider registered after the things it instruments were built would
// instrument nothing.
type Telemetry struct {
	provider *otel.Provider
	// scrape is the guarded scrape target, nil when none is configured.
	scrape *ScrapeTarget
	// metrics is always non-nil after Register: with no meter provider the
	// instruments record into a no-op, so a tool never has to ask whether
	// telemetry is on.
	metrics *telemetry.Metrics
}

// NewTelemetry returns the provider.
func NewTelemetry() *Telemetry { return &Telemetry{} }

// Metrics are the conversion instruments, for the pipeline and the tools.
func (t *Telemetry) Metrics() *telemetry.Metrics { return t.metrics }

// Register implements vukem.Provider.
//
// Telemetry is off unless it is configured, deliberately: an app that exports
// nothing should cost nothing, and running `serve` on a laptop should not
// require a collector. OTEL_EXPORTER_OTLP_ENDPOINT turns traces and metrics
// on; METRICS_TOKEN turns the scrape target on. Neither implies the other.
func (t *Telemetry) Register(app *vukem.App) error {
	target, err := NewScrapeTarget(app.Source.Get("METRICS_TOKEN"))
	if err != nil {
		return err
	}

	t.scrape = target

	var reader sdkmetric.Reader
	if target != nil {
		reader = target.Reader()
	}

	t.provider = otel.New(otel.Config{
		ServiceName:    app.Source.Get("APP_NAME"),
		ServiceVersion: app.Source.Get("APP_VERSION"),
		MetricReader:   reader,
		// The SQL text stays off the spans. A parameterised query carries no
		// value, but it does carry the shape of every table to anybody who
		// can read a trace. The operation and the table are recorded anyway,
		// which is what diagnosing a slow query needs.
		QueryText: false,
		// Installed as the globals, so a library instrumented through
		// OpenTelemetry's own API joins the same traces.
		Global: true,
	})

	if err := t.provider.Register(app); err != nil {
		return fmt.Errorf("services: telemetry: %w", err)
	}

	t.metrics, err = telemetry.NewMetrics(t.provider.MeterProvider())
	if err != nil {
		return fmt.Errorf("services: conversion metrics: %w", err)
	}

	return nil
}

// Shutdown implements vukem.Provider, flushing what has not been exported.
func (t *Telemetry) Shutdown(ctx context.Context, app *vukem.App) error {
	if t.provider == nil {
		return nil
	}

	return t.provider.Shutdown(ctx, app)
}

// ScrapeTarget is the Prometheus scrape endpoint and the token that guards
// it.
//
// Its own type rather than two fields on Telemetry, because deciding who may
// read the metrics is the security-relevant part and deserves to be testable
// on its own, without an App to build it from.
type ScrapeTarget struct {
	reader  sdkmetric.Reader
	handler http.Handler
	token   string
}

// NewScrapeTarget returns the target, or nil when no token is configured.
//
// Nil rather than a disabled target: metrics are off by default, and a
// disabled object that still answers requests is one refactor away from
// answering them with the metrics.
//
// Pull as well as push, because they answer different needs: OTLP push sends
// to a collector, and a scrape target is what an operator points at when
// there is no collector. Both read the same instruments, so nothing is
// recorded twice.
func NewScrapeTarget(token string) (*ScrapeTarget, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, nil
	}

	// Its own registry rather than prometheus.DefaultRegisterer: the default
	// one carries Go runtime and process collectors this endpoint has no
	// reason to publish, and anything else linked into the binary could
	// register into it.
	registry := prometheus.NewRegistry()

	exporter, err := promexporter.New(promexporter.WithRegisterer(registry))
	if err != nil {
		return nil, fmt.Errorf("services: prometheus exporter: %w", err)
	}

	return &ScrapeTarget{
		reader:  exporter,
		handler: promhttp.HandlerFor(registry, promhttp.HandlerOpts{}),
		token:   token,
	}, nil
}

// Reader is the metric reader the exporter reads from.
func (s *ScrapeTarget) Reader() sdkmetric.Reader { return s.reader }

// ServeHTTP serves the metrics to whoever holds the token.
//
// Guarded, and not by the account middleware: a scraper has no account, and
// metric names and label values describe the service's shape and its traffic.
// A bearer token compared in constant time is the right size of lock.
func (s *ScrapeTarget) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.authorised(r) {
		// WWW-Authenticate, so a misconfigured scraper is told what is wrong
		// instead of guessing at a 404.
		w.Header().Set("WWW-Authenticate", `Bearer realm="metrics"`)
		http.Error(w, "unauthorised", http.StatusUnauthorized)

		return
	}

	s.handler.ServeHTTP(w, r)
}

// authorised compares the bearer token in constant time, so the endpoint does
// not leak the token's prefix through its response time.
func (s *ScrapeTarget) authorised(r *http.Request) bool {
	scheme, token, found := strings.Cut(r.Header.Get("Authorization"), " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return false
	}

	given := []byte(strings.TrimSpace(token))
	want := []byte(s.token)

	return subtle.ConstantTimeCompare(given, want) == 1
}

// Scrape serves the metrics, or 404 when no scrape target is configured.
//
// A 404 rather than a 401 for the unconfigured case: an endpoint that exists
// but refuses tells a scanner the service has metrics worth coming back for,
// and one that was never enabled should look like one that does not exist.
func (t *Telemetry) Scrape(w http.ResponseWriter, r *http.Request) {
	if t.scrape == nil {
		http.NotFound(w, r)

		return
	}

	t.scrape.ServeHTTP(w, r)
}
