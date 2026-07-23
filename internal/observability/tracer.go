package observability

import (
	"context"
	"fmt"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// =============================================================================
// Tracing environment variables (PROD-003a)
// =============================================================================
//
// All tracing knobs are env-var driven, consistent with Helix's env-var-first
// design pattern (HELIX_LOG_*, HELIX_AGENT_ID). When HELIX_TRACE_ENABLED is
// unset / false (the default), SetupTracing returns a no-op TracerProvider
// so there is zero runtime overhead — no connection to the collector, no
// background goroutines, no exporters started.

const (
	// EnvTraceEnabled toggles tracing. Truthy values ("1", "true", "yes",
	// "on", case-insensitive) enable; everything else (including unset)
	// leaves tracing disabled. The default is disabled.
	EnvTraceEnabled = "HELIX_TRACE_ENABLED"

	// EnvTraceEndpoint is the OTLP gRPC endpoint. When insecure mode is
	// on (default), pass a bare "host:port" (gRPC dial target). When
	// insecure is off, TLS is used. Defaults to "localhost:4317" — the
	// canonical OTLP gRPC port.
	EnvTraceEndpoint = "HELIX_TRACE_ENDPOINT"

	// EnvTraceSampler selects the sampler. Recognised values:
	//
	//   - "always"               → sdktrace.AlwaysSample
	//   - "never"                → sdktrace.NeverSample
	//   - "parentbased_always"   → ParentBased(AlwaysSample) [default]
	//   - "parentbased_never"    → ParentBased(NeverSample)
	//   - "ratio"                → ParentBased(TraceIDRatioBased(ratio))
	//   - "parentbased_ratio"    → ParentBased(TraceIDRatioBased(ratio))
	//
	// Recognition is case-insensitive. Unknown values surface as a
	// SetupTracing error (TE-002).
	EnvTraceSampler = "HELIX_TRACE_SAMPLER"

	// EnvTraceRatio is the sampling ratio used when the sampler is
	// "ratio" or "parentbased_ratio". Float 0.0–1.0; out-of-range
	// values surface as a SetupTracing error (TE-001). Defaults to 0.1.
	EnvTraceRatio = "HELIX_TRACE_RATIO"

	// EnvTraceInsecure controls whether the OTLP gRPC connection skips
	// TLS. Truthy → insecure (default). Set to a falsy value for
	// production collectors that present trusted certificates.
	EnvTraceInsecure = "HELIX_TRACE_INSECURE"
)

// =============================================================================
// TracerConfig
// =============================================================================
//
// TracerConfig is the explicit, programmatic counterpart to the env vars
// above. It is what Options.Tracing holds and what SetupTracing consumes.
// The zero value corresponds to "tracing disabled" — SetupTracing returns
// a no-op TracerProvider in that case.

type TracerConfig struct {
	// Enabled toggles tracing. When false, SetupTracing returns a no-op
	// TracerProvider regardless of the other fields.
	Enabled bool

	// Endpoint is the OTLP gRPC dial target. Defaults to
	// "localhost:4317" when empty.
	Endpoint string

	// Sampler is one of "always", "never", "parentbased_always",
	// "parentbased_never", "ratio", "parentbased_ratio". Defaults to
	// "parentbased_always" when empty. See EnvTraceSampler for the
	// full taxonomy.
	Sampler string

	// Ratio is the sampling probability used when Sampler is "ratio"
	// or "parentbased_ratio". Must be in [0.0, 1.0]; defaults to 0.1.
	Ratio float64

	// Insecure controls whether the OTLP gRPC connection uses TLS.
	// Defaults to true (the local collector typically listens on a
	// plaintext port).
	Insecure bool
}

// ApplyDefaults returns a copy of cfg with empty fields populated from
// the documented defaults. Does not mutate the receiver.
//
// Insecure's zero value (false) is meaningful — operators must opt in
// to TLS explicitly. We therefore don't override it here.
func (c TracerConfig) ApplyDefaults() TracerConfig {
	if c.Endpoint == "" {
		c.Endpoint = "localhost:4317"
	}
	if c.Sampler == "" {
		c.Sampler = "parentbased_always"
	}
	if c.Ratio == 0 {
		c.Ratio = 0.1
	}
	return c
}

// =============================================================================
// TracerConfigFromEnv
// =============================================================================
//
// TracerConfigFromEnv reads the HELIX_TRACE_* env vars and returns a
// populated TracerConfig. It never returns an error — malformed values
// (unparseable ratio, unknown sampler) are reported by SetupTracing at
// setup time so the failure surfaces in the same lifecycle as the
// TracerProvider itself. This matches the pattern used elsewhere in
// observability (e.g. resolveFormat, which surfaces ConfigError at
// Init).

func TracerConfigFromEnv() TracerConfig {
	return TracerConfig{
		Enabled:  envBool(EnvTraceEnabled, false),
		Endpoint: strings.TrimSpace(os.Getenv(EnvTraceEndpoint)),
		Sampler:  strings.ToLower(strings.TrimSpace(os.Getenv(EnvTraceSampler))),
		Ratio:    envFloat(EnvTraceRatio, 0.1),
		Insecure: envBool(EnvTraceInsecure, true),
	}
}

// envBool returns the truthy/falsy value of an env var. Truthy values
// are "1", "true", "yes", "on" (case-insensitive). Unset or unrecognised
// values return def.
func envBool(key string, def bool) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	switch v {
	case "":
		return def
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return def
}

// envFloat parses an env var as a float64. Returns def on unset or
// parse error — callers that need strict validation should re-read the
// raw value themselves.
func envFloat(key string, def float64) float64 {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}

// =============================================================================
// SetupTracing
// =============================================================================
//
// SetupTracing builds the process-global TracerProvider according to
// cfg. Returns the provider so callers can hold a reference for
// shutdown. When cfg.Enabled is false (or cfg is the zero value), the
// returned provider is nil — the caller should treat that as a no-op
// and skip Shutdown at Close time.
//
// The returned provider is also installed as the process-global
// provider via otel.SetTracerProvider, and the W3C TraceContext +
// Baggage propagators are installed via otel.SetTextMapPropagator so
// downstream clients/servers can pick up the propagated context.

func SetupTracing(cfg TracerConfig) (*sdktrace.TracerProvider, error) {
	cfg = cfg.ApplyDefaults()

	// Always install the W3C propagators — they're cheap and let
	// downstream HTTP/gRPC clients extract incoming trace context even
	// when this process is not actively tracing. Done unconditionally
	// so toggling tracing on never requires a service restart to
	// start propagating.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	if !cfg.Enabled {
		// No-op provider: zero overhead, satisfies the
		// trace.TracerProvider interface, and short-circuits in the
		// SDK so no exporters are started, no goroutines spawned, no
		// network connections opened.
		otel.SetTracerProvider(noop.NewTracerProvider())
		return nil, nil
	}

	// Validate the ratio before wiring anything up so a malformed value
	// fails fast at Init rather than at the first span emission.
	if cfg.Ratio < 0.0 || cfg.Ratio > 1.0 {
		return nil, &ConfigError{
			Field: EnvTraceRatio,
			Value: strconv.FormatFloat(cfg.Ratio, 'g', -1, 64),
			Msg:   "expected float in [0.0, 1.0]",
		}
	}

	// Build the OTLP gRPC exporter. The exporter is intentionally
	// created lazily — no connection is opened until the first span
	// is exported, so a misconfigured endpoint does not block Init.
	exporter, err := buildOTLPExporter(cfg)
	if err != nil {
		return nil, err
	}

	// Build the sampler. ParentBased wraps the inner sampler so we
	// honour upstream sampling decisions when a parent span is present
	// and fall back to the inner policy for root spans.
	root, err := buildSampler(cfg)
	if err != nil {
		return nil, err
	}

	// Build the resource (service.name, version, etc.).
	// debug.ReadBuildInfo may return a zero-valued struct on binaries
	// built without VCS info; in that case we degrade to "unknown"
	// rather than fail.
	res, err := buildResource()
	if err != nil {
		return nil, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(root),
	)

	otel.SetTracerProvider(tp)
	return tp, nil
}

// buildOTLPExporter constructs the OTLP gRPC exporter pointed at
// cfg.Endpoint. When cfg.Insecure is true (default), the connection is
// plaintext — the local collector case. When false, TLS is enabled.
func buildOTLPExporter(cfg TracerConfig) (*otlptrace.Exporter, error) {
	opts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(cfg.Endpoint),
	}
	if cfg.Insecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}
	return otlptracegrpc.New(context.Background(), opts...)
}

// buildSampler translates the Sampler string into a concrete
// sdktrace.Sampler. Unknown values surface as a ConfigError so the
// failure appears in the same lifecycle as the rest of Init.
func buildSampler(cfg TracerConfig) (sdktrace.Sampler, error) {
	switch cfg.Sampler {
	case "always":
		return sdktrace.AlwaysSample(), nil
	case "never":
		return sdktrace.NeverSample(), nil
	case "parentbased_always":
		return sdktrace.ParentBased(sdktrace.AlwaysSample()), nil
	case "parentbased_never":
		return sdktrace.ParentBased(sdktrace.NeverSample()), nil
	case "ratio":
		return sdktrace.TraceIDRatioBased(cfg.Ratio), nil
	case "parentbased_ratio":
		return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.Ratio)), nil
	}
	return nil, &ConfigError{
		Field: EnvTraceSampler,
		Value: cfg.Sampler,
		Msg:   "expected always | never | parentbased_always | parentbased_never | ratio | parentbased_ratio",
	}
}

// buildResource constructs the OTel resource with the static
// service.name="helix" plus the build version from debug.ReadBuildInfo.
// We use the semconv v1.26.0 schema (the default for OTel SDK v1.44).
func buildResource() (*resource.Resource, error) {
	version := "unknown"
	if bi, ok := debug.ReadBuildInfo(); ok && bi != nil {
		v := strings.TrimSpace(bi.Main.Version)
		// debug.ReadBuildInfo reports "(devel)" or empty for `go run`
		// builds; in that case fall back to "unknown" so the export
		// payload still has a deterministic value.
		if v == "" || v == "(devel)" {
			v = "unknown"
		}
		version = v
	}

	attrs := []attribute.KeyValue{
		attribute.String("service.name", "helix"),
		attribute.String("service.version", version),
	}

	return resource.Merge(
		resource.Default(),
		resource.NewWithAttributes("https://opentelemetry.io/schemas/1.26.0", attrs...),
	)
}

// =============================================================================
// Context helpers
// =============================================================================

// TraceIDFromContext extracts the trace ID from ctx as a lowercase hex
// string suitable for inclusion in log entries. Returns the empty
// string when ctx carries no valid SpanContext (e.g. when tracing is
// disabled, or when ctx has no Span at all). The returned ID is the
// 32-character hex representation per W3C TraceContext.
func TraceIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return ""
	}
	return sc.TraceID().String()
}

// ShutdownTraceProvider flushes pending spans on tp with a hard 5s
// timeout. Returns nil on success, the shutdown error on failure, or
// nil when tp is nil. Always safe to call on a nil receiver.
//
// TE-004: best-effort flush; failures are wrapped but not fatal.
func ShutdownTraceProvider(tp *sdktrace.TracerProvider) error {
	if tp == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := tp.Shutdown(ctx); err != nil {
		return fmt.Errorf("observability: tracer shutdown: %w", err)
	}
	return nil
}
