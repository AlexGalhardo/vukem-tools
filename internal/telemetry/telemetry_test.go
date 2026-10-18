package telemetry_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/AlexGalhardo/vukem-tools/internal/telemetry"
)

// The ROADMAP's assert, and the one that matters most here: no span attribute
// carries a signed URL, an API key or a user's email.
//
// A trace is read by anybody with access to the tracing backend, which is a
// wider audience than the owner of the file being converted. An engine's
// command line is the place these leak from: it holds the working directory,
// both file names, and for the downloader the URL somebody asked for.
func TestAnEngineCommandCarriesNoSecretAndNoIdentity(t *testing.T) {
	t.Parallel()

	secrets := []string{
		// A signed download URL: the signature is a bearer credential.
		"https://tools.vukem.dev/api/v1/conversions/01JBX/result?expires=1760000000&signature=9f2b7c",
		// An API key.
		"vkt_live_8c21f0a4bd9e4c5fa1",
		// A user's email, which a filename can contain.
		"alex@example.com",
	}

	args := []string{
		"-i", "/app/work/01JBXQ8/alex@example.com-invoice.pdf",
		"--output=/app/work/01JBXQ8/out.png",
		"--token", "vkt_live_8c21f0a4bd9e4c5fa1",
		"https://tools.vukem.dev/api/v1/conversions/01JBX/result?expires=1760000000&signature=9f2b7c",
		"-f", "image2",
	}

	got := strings.Join(telemetry.ScrubArgs(args), " ")

	for _, secret := range secrets {
		if strings.Contains(got, secret) {
			t.Errorf("the scrubbed command still contains %q:\n%s", secret, got)
		}
	}

	// The shape has to survive, or the attribute is worth nothing: a trace is
	// read to find out which flags an engine was given.
	for _, keep := range []string{"-i", "--output=", "-f", "image2"} {
		if !strings.Contains(got, keep) {
			t.Errorf("the scrubbed command lost %q, so it no longer says what ran:\n%s", keep, got)
		}
	}
}

func TestScrubArgs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "a path keeps only its extension",
			in:   []string{"/app/work/01JBX/holiday-photo.jpg"},
			want: []string{"?.jpg"},
		},
		{
			name: "a path with no extension keeps nothing",
			in:   []string{"/app/work/01JBX/frames"},
			want: []string{"?"},
		},
		{
			name: "a windows path is a path too",
			in:   []string{`C:\work\01JBX\photo.png`},
			want: []string{"?.png"},
		},
		{
			name: "a flag with an inline value keeps the flag",
			in:   []string{"--output=/app/work/01JBX/out.webp"},
			want: []string{"--output=?"},
		},
		{
			name: "a bare flag is kept whole",
			in:   []string{"-progress", "--no-playlist"},
			want: []string{"-progress", "--no-playlist"},
		},
		{
			name: "a url keeps its scheme and host",
			in:   []string{"https://www.youtube.com/watch?v=dQw4w9WgXcQ"},
			want: []string{"https://www.youtube.com/?"},
		},
		{
			name: "a format or a number is not a secret",
			in:   []string{"image2", "1920x1080", "23"},
			want: []string{"image2", "1920x1080", "23"},
		},
		{
			name: "an empty argument is left alone",
			in:   []string{""},
			want: []string{""},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(c.want, telemetry.ScrubArgs(c.in)); diff != "" {
				t.Errorf("ScrubArgs(%q) mismatch (-want +got):\n%s", c.in, diff)
			}
		})
	}
}

// A failed conversion must produce the error type as an attribute, because
// that is what an alert on the error rate groups by. An error's *message*
// must not: it can contain a file name, and a metric label is stored for
// every series indefinitely.
func TestAFailureRecordsItsErrorTypeAndNothingElse(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	m, err := telemetry.NewMetrics(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	if err != nil {
		t.Fatalf("new metrics: %v", err)
	}

	m.Ended(context.Background(), "pixels-to-svg", "error", "too_large", 2*time.Second)

	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("collect: %v", err)
	}

	attrs := attributesOf(t, &collected, telemetry.MetricConversionsEnded)

	for _, want := range []string{"pixels-to-svg", "error", "too_large"} {
		if !strings.Contains(attrs, want) {
			t.Errorf("the ended counter does not carry %q: %s", want, attrs)
		}
	}
}

// A success records no error type at all, rather than an empty one: an empty
// label value is a series of its own, and it would split every success metric
// in two for no reason.
func TestASuccessRecordsNoErrorType(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	m, err := telemetry.NewMetrics(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	if err != nil {
		t.Fatalf("new metrics: %v", err)
	}

	m.Ended(context.Background(), "pixels-to-svg", "finished", "", time.Second)

	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("collect: %v", err)
	}

	if attrs := attributesOf(t, &collected, telemetry.MetricConversionsEnded); strings.Contains(attrs, "error_type") {
		t.Errorf("a success carries an error_type attribute: %s", attrs)
	}
}

// Recording with telemetry switched off must not panic. Every tool records
// unconditionally, so the no-op path is the one that runs most often.
func TestTheNoOpInstrumentsRecordWithoutTelemetry(t *testing.T) {
	t.Parallel()

	m := telemetry.NoMetrics()
	ctx := context.Background()

	m.Ended(ctx, "pixels-to-svg", "finished", "", time.Second)
	m.Bytes(ctx, "pixels-to-svg", 1024, 2048)
	m.EngineFailed(ctx, "ffmpeg", "engine_failed")
	m.Depth(ctx, 3, 1)
}

// Bytes are recorded in both directions, which is what makes "how much did
// this conversion save" answerable from metrics rather than from a database
// scan.
func TestBytesAreRecordedBothWays(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	m, err := telemetry.NewMetrics(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	if err != nil {
		t.Fatalf("new metrics: %v", err)
	}

	m.Bytes(context.Background(), "image-compress", 4096, 1024)

	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("collect: %v", err)
	}

	for _, name := range []string{telemetry.MetricBytesIn, telemetry.MetricBytesOut} {
		if !recorded(&collected, name) {
			t.Errorf("%s was not recorded", name)
		}
	}
}

// A zero size records nothing: a counter that is incremented by zero still
// creates the series, and an empty upload is not information.
func TestAZeroSizeRecordsNothing(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	m, err := telemetry.NewMetrics(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	if err != nil {
		t.Fatalf("new metrics: %v", err)
	}

	m.Bytes(context.Background(), "image-compress", 0, 0)

	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("collect: %v", err)
	}

	if recorded(&collected, telemetry.MetricBytesIn) {
		t.Error("a zero input size created a series")
	}
}

// attributesOf renders one instrument's attribute sets as text.
func attributesOf(t *testing.T, rm *metricdata.ResourceMetrics, name string) string {
	t.Helper()

	var out strings.Builder

	for _, scope := range rm.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name != name {
				continue
			}

			sum, ok := metric.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}

			for _, point := range sum.DataPoints {
				for _, kv := range point.Attributes.ToSlice() {
					out.WriteString(string(kv.Key))
					out.WriteString("=")
					out.WriteString(kv.Value.String())
					out.WriteString(" ")
				}
			}
		}
	}

	if out.Len() == 0 {
		t.Fatalf("%s was not recorded at all", name)
	}

	return out.String()
}

// recorded reports whether an instrument produced any data point.
func recorded(rm *metricdata.ResourceMetrics, name string) bool {
	for _, scope := range rm.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name == name {
				return true
			}
		}
	}

	return false
}
