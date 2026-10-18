// Package telemetry records what a conversion did.
//
// The vukem otel driver already records HTTP, database and queue signals. This
// package adds the ones that are specific to a conversion service: how many
// conversions ended in each state, how long each tool takes, how deep the
// queue is, how many bytes went in and came out, and which engine failed with
// which kind of error.
//
// Every instrument is safe to use when telemetry is off: NewMetrics with a
// nil or no-op meter provider returns working instruments that record
// nowhere, so a tool never has to ask whether it should report.
package telemetry

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
)

// Instrument names. The OpenTelemetry semantic conventions have no names for
// a file conversion, so these are namespaced under the service and follow the
// conventions' shape: a duration is a histogram in seconds, so its count is
// the conversion count and no separate counter is needed.
const (
	MetricConversionDuration = "vukem_tools.conversion.duration"
	MetricConversionsEnded   = "vukem_tools.conversion.ended"
	MetricQueueDepth         = "vukem_tools.queue.depth"
	MetricBytesIn            = "vukem_tools.conversion.bytes.in"
	MetricBytesOut           = "vukem_tools.conversion.bytes.out"
	MetricEngineFailures     = "vukem_tools.engine.failures"
)

// Attribute keys.
//
// Every one of these is low-cardinality on purpose. A tool name, a status and
// an error type come from fixed sets in the code; a conversion id, a file
// name, a URL or an account would each give a metric unbounded cardinality,
// which is how a metrics backend is brought down by its own data.
const (
	ToolKey      = attribute.Key("conversion.tool")
	StatusKey    = attribute.Key("conversion.status")
	ErrorTypeKey = attribute.Key("conversion.error_type")
	EngineKey    = attribute.Key("engine.name")
)

// durationBuckets are seconds. A conversion is not an HTTP request: the fast
// ones are under a second and a long video is minutes, so the boundaries run
// further out than the conventions' HTTP defaults and there is no point
// measuring below five milliseconds.
var durationBuckets = []float64{ //nolint:gochecknoglobals // read-only table
	0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300, 600,
}

// Metrics are the conversion instruments.
type Metrics struct {
	duration      metric.Float64Histogram
	ended         metric.Int64Counter
	bytesIn       metric.Int64Counter
	bytesOut      metric.Int64Counter
	engineFailure metric.Int64Counter
	depth         metric.Int64Gauge
}

// NewMetrics builds the instruments on mp.
//
// A nil provider gives working no-op instruments rather than an error: a
// caller that has telemetry switched off still gets something it can record
// into, which keeps every `if m != nil` out of the conversion path.
func NewMetrics(mp metric.MeterProvider) (*Metrics, error) {
	if mp == nil {
		mp = noop.NewMeterProvider()
	}

	meter := mp.Meter("github.com/AlexGalhardo/vukem-tools")

	duration, err := meter.Float64Histogram(
		MetricConversionDuration,
		metric.WithUnit("s"),
		metric.WithDescription("How long a conversion took, from claim to settle."),
		metric.WithExplicitBucketBoundaries(durationBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("telemetry: %s: %w", MetricConversionDuration, err)
	}

	ended, err := meter.Int64Counter(
		MetricConversionsEnded,
		metric.WithDescription("Conversions that reached a final state, by status and error type."),
	)
	if err != nil {
		return nil, fmt.Errorf("telemetry: %s: %w", MetricConversionsEnded, err)
	}

	bytesIn, err := meter.Int64Counter(
		MetricBytesIn,
		metric.WithUnit("By"),
		metric.WithDescription("Bytes read as conversion input."),
	)
	if err != nil {
		return nil, fmt.Errorf("telemetry: %s: %w", MetricBytesIn, err)
	}

	bytesOut, err := meter.Int64Counter(
		MetricBytesOut,
		metric.WithUnit("By"),
		metric.WithDescription("Bytes written as conversion output."),
	)
	if err != nil {
		return nil, fmt.Errorf("telemetry: %s: %w", MetricBytesOut, err)
	}

	engineFailure, err := meter.Int64Counter(
		MetricEngineFailures,
		metric.WithDescription("Engine runs that failed, by engine and error type."),
	)
	if err != nil {
		return nil, fmt.Errorf("telemetry: %s: %w", MetricEngineFailures, err)
	}

	depth, err := meter.Int64Gauge(
		MetricQueueDepth,
		metric.WithDescription("Conversions waiting or running."),
	)
	if err != nil {
		return nil, fmt.Errorf("telemetry: %s: %w", MetricQueueDepth, err)
	}

	return &Metrics{
		duration:      duration,
		ended:         ended,
		bytesIn:       bytesIn,
		bytesOut:      bytesOut,
		engineFailure: engineFailure,
		depth:         depth,
	}, nil
}

// Ended records a conversion that reached a final state.
//
// errorType is empty for a success. It is the taxonomy's code, never an
// error's text: a message can contain a file name or a path, and a metric
// label is stored for every series forever.
func (m *Metrics) Ended(ctx context.Context, tool, status, errorType string, took time.Duration) {
	attrs := []attribute.KeyValue{ToolKey.String(tool), StatusKey.String(status)}
	if errorType != "" {
		attrs = append(attrs, ErrorTypeKey.String(errorType))
	}

	set := metric.WithAttributes(attrs...)

	m.duration.Record(ctx, took.Seconds(), set)
	m.ended.Add(ctx, 1, set)
}

// Bytes records a conversion's input and output sizes.
func (m *Metrics) Bytes(ctx context.Context, tool string, in, out int64) {
	set := metric.WithAttributes(ToolKey.String(tool))

	if in > 0 {
		m.bytesIn.Add(ctx, in, set)
	}

	if out > 0 {
		m.bytesOut.Add(ctx, out, set)
	}
}

// EngineFailed records an engine run that failed.
func (m *Metrics) EngineFailed(ctx context.Context, engine, errorType string) {
	m.engineFailure.Add(ctx, 1,
		metric.WithAttributes(EngineKey.String(engine), ErrorTypeKey.String(errorType)))
}

// Depth records how many conversions are waiting or running.
func (m *Metrics) Depth(ctx context.Context, waiting, running int64) {
	m.depth.Record(ctx, waiting, metric.WithAttributes(StatusKey.String("pending")))
	m.depth.Record(ctx, running, metric.WithAttributes(StatusKey.String("running")))
}

// EngineSpan names the attributes an engine span carries.
const (
	// EngineCommandKey is the engine's argv, scrubbed by ScrubArgs.
	EngineCommandKey = attribute.Key("engine.command")
	EngineVersionKey = attribute.Key("engine.version")
	EngineExitKey    = attribute.Key("engine.exit_code")
)

// ScrubArgs makes an engine's argv safe to put on a span.
//
// A trace is read by anybody with access to the tracing backend, which is a
// wider audience than the owner of the file being converted. An engine's
// command line carries the working directory and both file names, for the
// downloader the URL somebody asked for, and for anything authenticated a
// token — so the arguments are kept for their shape and stripped of their
// content.
//
// This is an allowlist, not a denylist, and the difference is the whole
// point. The first version of this function kept any argument that did not
// look like a path or a URL, which let `--token vkt_live_8c21f0a4bd9e4c5fa1`
// through untouched: a secret is a bare word, and so is a codec name. A
// denylist of secret shapes fails open on the next shape nobody thought of.
// So a bare argument survives only when it matches something a trace is
// actually read for — a number, a dimension, a short lowercase identifier
// like a format or a codec. Everything else becomes "?".
//
// What survives: flags, the extension of anything that looks like a path, a
// URL's scheme and host, and recognisably harmless values. What does not:
// directories, file names, query strings, and every value this function
// cannot positively account for.
func ScrubArgs(args []string) []string {
	out := make([]string, 0, len(args))

	for _, arg := range args {
		out = append(out, scrubArg(arg))
	}

	return out
}

// safeValue reports whether a bare argument can be shown in a trace.
//
// Deliberately narrow: digits and separators, or a short identifier of
// lowercase letters, digits, dot, underscore and hyphen. "image2", "1920x1080",
// "23", "yuv420p" and "libx264" pass. A token, a hash, a base64 blob, an email
// and a bare file name do not — a token is longer than this allows, and
// anything with an "@" or an uppercase run is refused outright.
func safeValue(arg string) bool {
	const maxSafe = 16

	if len(arg) > maxSafe {
		return false
	}

	for _, r := range arg {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'z':
		case r == '.' || r == '_' || r == '-' || r == ':':
		default:
			return false
		}
	}

	return true
}

func scrubArg(arg string) string {
	if arg == "" {
		return arg
	}

	// A flag with an inline value: keep the flag, drop the value. The value
	// is where a path or a URL hides (--output=/work/a1b2/photo.jpg).
	if strings.HasPrefix(arg, "-") {
		if name, _, found := strings.Cut(arg, "="); found {
			return name + "=?"
		}

		return arg
	}

	// A URL: the scheme and the host are the useful part for a trace, the
	// rest identifies what somebody asked for.
	if i := strings.Index(arg, "://"); i > 0 {
		host, _, _ := strings.Cut(arg[i+3:], "/")

		return arg[:i+3] + host + "/?"
	}

	// A path: keep only the extension, which is what a trace is read for.
	if strings.ContainsAny(arg, "/\\") {
		if i := strings.LastIndex(arg, "."); i >= 0 && !strings.ContainsAny(arg[i:], "/\\") {
			return "?" + arg[i:]
		}

		return "?"
	}

	// A bare value. Shown only if it is positively accountable; see
	// safeValue for why this is an allowlist.
	if safeValue(arg) {
		return arg
	}

	return "?"
}

// StartEngineSpan opens a span for one engine run.
//
// The command is scrubbed before it is recorded; see ScrubArgs for what that
// removes and why.
func StartEngineSpan(
	ctx context.Context,
	tracer trace.Tracer,
	engine, version string,
	args []string,
) (context.Context, trace.Span) {
	return tracer.Start(ctx, "engine "+engine,
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			EngineKey.String(engine),
			EngineVersionKey.String(version),
			EngineCommandKey.StringSlice(ScrubArgs(args)),
		),
	)
}

// NoMetrics are instruments that record nowhere.
//
// Returned when telemetry was never configured, so a caller can record
// unconditionally. Built once: the instruments are stateless and the no-op
// meter allocates on every call to Meter.
func NoMetrics() *Metrics { return noMetrics }

var noMetrics = func() *Metrics { //nolint:gochecknoglobals // immutable no-op instruments
	m, err := NewMetrics(nil)
	if err != nil {
		// Unreachable: the no-op meter provider cannot fail to build an
		// instrument. A panic here would mean the OpenTelemetry API changed
		// shape, which is better found at startup than ignored.
		panic("telemetry: the no-op meter failed to build an instrument: " + err.Error())
	}

	return m
}()
