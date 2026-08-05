package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// =============================================================================
// Env helpers
// =============================================================================

// unsetTraceEnv removes every HELIX_TRACE_* var for the duration of the
// test, restoring previous values (if any) afterwards.
func unsetTraceEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{EnvTraceEnabled, EnvTraceEndpoint, EnvTraceSampler, EnvTraceRatio, EnvTraceInsecure} {
		old, had := os.LookupEnv(k)
		if err := os.Unsetenv(k); err != nil {
			t.Fatalf("unset %s: %v", k, err)
		}
		if had {
			t.Cleanup(func() { _ = os.Setenv(k, old) })
		}
	}
}

// restoreGlobalTracing captures the current global TracerProvider and
// TextMapPropagator and returns a closure that restores them. Use via
// t.Cleanup(restoreGlobalTracing(t)) in tests that call SetupTracing or
// otel.SetTracerProvider, so global OTel state does not leak between tests.
func restoreGlobalTracing(t *testing.T) func() {
	t.Helper()
	prevTP := otel.GetTracerProvider()
	prevProp := otel.GetTextMapPropagator()
	return func() {
		otel.SetTracerProvider(prevTP)
		otel.SetTextMapPropagator(prevProp)
	}
}

func TestEnvBool(t *testing.T) {
	tests := []struct {
		name string
		val  string
		def  bool
		want bool
	}{
		{"unset defaults true", "", true, true},
		{"unset defaults false", "", false, false},
		{"one", "1", false, true},
		{"true", "true", false, true},
		{"yes", "yes", false, true},
		{"on", "on", false, true},
		{"uppercase TRUE", "TRUE", false, true},
		{"mixed case On", "On", false, true},
		{"whitespace padded", "  true  ", false, true},
		{"zero", "0", true, false},
		{"false", "false", true, false},
		{"no", "no", true, false},
		{"off", "off", true, false},
		{"uppercase OFF", "OFF", true, false},
		{"garbage defaults true", "banana", true, true},
		{"garbage defaults false", "banana", false, false},
		{"spaced garbage defaults true", " maybe ", true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HELIX_TEST_ENVBOOL", tt.val)
			if got := envBool("HELIX_TEST_ENVBOOL", tt.def); got != tt.want {
				t.Errorf("envBool(%q, %v) = %v, want %v", tt.val, tt.def, got, tt.want)
			}
		})
	}
}

func TestEnvFloat(t *testing.T) {
	tests := []struct {
		name string
		val  string
		def  float64
		want float64
	}{
		{"unset defaults", "", 0.1, 0.1},
		{"blank defaults", "   ", 7, 7},
		{"whole number", "2", 0.1, 2},
		{"fraction", "0.5", 0.1, 0.5},
		{"scientific notation", "1e-3", 0.1, 0.001},
		{"garbage defaults", "abc", 0.1, 0.1},
		{"garbage after number", "0.5x", 0.1, 0.1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HELIX_TEST_ENVFLOAT", tt.val)
			if got := envFloat("HELIX_TEST_ENVFLOAT", tt.def); got != tt.want {
				t.Errorf("envFloat(%q, %v) = %v, want %v", tt.val, tt.def, got, tt.want)
			}
		})
	}
}

func TestTracerConfigFromEnv_AllSet(t *testing.T) {
	unsetTraceEnv(t)
	t.Setenv(EnvTraceEnabled, "true")
	t.Setenv(EnvTraceEndpoint, "  otel.example.com:4317 ")
	t.Setenv(EnvTraceSampler, " PARENTBASED_RATIO ")
	t.Setenv(EnvTraceRatio, "0.42")
	t.Setenv(EnvTraceInsecure, "false")

	cfg := TracerConfigFromEnv()
	if !cfg.Enabled {
		t.Error("Enabled = false, want true")
	}
	if cfg.Endpoint != "otel.example.com:4317" {
		t.Errorf("Endpoint = %q, want trimmed otel.example.com:4317", cfg.Endpoint)
	}
	if cfg.Sampler != "parentbased_ratio" {
		t.Errorf("Sampler = %q, want lowercased parentbased_ratio", cfg.Sampler)
	}
	if cfg.Ratio != 0.42 {
		t.Errorf("Ratio = %v, want 0.42", cfg.Ratio)
	}
	if cfg.Insecure {
		t.Error("Insecure = true, want false")
	}
}

func TestTracerConfigFromEnv_Defaults(t *testing.T) {
	unsetTraceEnv(t)
	cfg := TracerConfigFromEnv()
	if cfg.Enabled {
		t.Error("Enabled = true, want false (default)")
	}
	if cfg.Endpoint != "" {
		t.Errorf("Endpoint = %q, want empty", cfg.Endpoint)
	}
	if cfg.Sampler != "" {
		t.Errorf("Sampler = %q, want empty", cfg.Sampler)
	}
	if cfg.Ratio != 0.1 {
		t.Errorf("Ratio = %v, want 0.1 default", cfg.Ratio)
	}
	if !cfg.Insecure {
		t.Error("Insecure = false, want true (default)")
	}
}

func TestTracerConfig_ApplyDefaults(t *testing.T) {
	got := TracerConfig{}.ApplyDefaults()
	if got.Endpoint != "localhost:4317" {
		t.Errorf("Endpoint = %q, want localhost:4317", got.Endpoint)
	}
	if got.Sampler != "parentbased_always" {
		t.Errorf("Sampler = %q, want parentbased_always", got.Sampler)
	}
	if got.Ratio != 0.1 {
		t.Errorf("Ratio = %v, want 0.1", got.Ratio)
	}
	if got.Insecure {
		t.Error("Insecure = true, want false — zero value must not be overridden")
	}

	// Explicit values are preserved; the receiver is not mutated.
	partial := TracerConfig{Endpoint: "x:1", Sampler: "never", Ratio: 0.9, Insecure: true}
	applied := partial.ApplyDefaults()
	if applied != partial {
		t.Errorf("ApplyDefaults mutated explicit fields: got %+v, want %+v", applied, partial)
	}
	c := TracerConfig{}
	_ = c.ApplyDefaults()
	if c.Endpoint != "" || c.Sampler != "" || c.Ratio != 0 {
		t.Errorf("ApplyDefaults mutated receiver: %+v", c)
	}
}

// =============================================================================
// SetupTracing
// =============================================================================

func TestSetupTracing_Disabled(t *testing.T) {
	t.Cleanup(restoreGlobalTracing(t))

	tp, err := SetupTracing(TracerConfig{})
	if err != nil {
		t.Fatalf("SetupTracing(disabled): %v", err)
	}
	if tp != nil {
		t.Errorf("expected nil provider when disabled, got %v", tp)
	}
	// The global provider must be the noop one: spans from it carry no
	// valid (exportable) SpanContext.
	if _, ok := otel.GetTracerProvider().(noop.TracerProvider); !ok {
		t.Errorf("expected noop global provider, got %T", otel.GetTracerProvider())
	}
	_, span := otel.Tracer("test").Start(context.Background(), "x")
	defer span.End()
	if span.SpanContext().IsValid() {
		t.Error("expected noop span to have invalid SpanContext")
	}
}

func TestSetupTracing_Enabled(t *testing.T) {
	t.Cleanup(restoreGlobalTracing(t))

	// Unreachable endpoint: the OTLP gRPC client is lazy (no dial until
	// the first export) and the "never" sampler means no span is ever
	// recorded, so this test performs zero network activity.
	cfg := TracerConfig{
		Enabled:  true,
		Endpoint: "127.0.0.1:1",
		Sampler:  "never",
		Ratio:    0.5,
		Insecure: true,
	}
	tp, err := SetupTracing(cfg)
	if err != nil {
		t.Fatalf("SetupTracing(enabled): %v", err)
	}
	if tp == nil {
		t.Fatal("expected non-nil provider when enabled")
	}
	// Installed as the process-global provider.
	global, ok := otel.GetTracerProvider().(*sdktrace.TracerProvider)
	if !ok {
		t.Fatalf("expected *sdktrace.TracerProvider global, got %T", otel.GetTracerProvider())
	}
	if global != tp {
		t.Error("global provider != provider returned by SetupTracing")
	}
	// A span started through the SDK provider has a valid context.
	_, span := tp.Tracer("test").Start(context.Background(), "s")
	span.End()
	if !span.SpanContext().IsValid() {
		t.Error("expected valid SpanContext from SDK provider")
	}
	if err := ShutdownTraceProvider(tp); err != nil {
		t.Fatalf("ShutdownTraceProvider: %v", err)
	}
}

func TestSetupTracing_ApplyDefaultsWhenEnabled(t *testing.T) {
	t.Cleanup(restoreGlobalTracing(t))

	// Zero-valued fields must be defaulted before the provider is built.
	tp, err := SetupTracing(TracerConfig{Enabled: true})
	if err != nil {
		t.Fatalf("SetupTracing(enabled, no fields): %v", err)
	}
	if tp == nil {
		t.Fatal("expected non-nil provider")
	}
	if err := ShutdownTraceProvider(tp); err != nil {
		t.Fatalf("ShutdownTraceProvider: %v", err)
	}
}

func TestSetupTracing_RatioOutOfRange(t *testing.T) {
	t.Cleanup(restoreGlobalTracing(t))

	for _, ratio := range []float64{1.5, -0.1} {
		_, err := SetupTracing(TracerConfig{Enabled: true, Ratio: ratio})
		if err == nil {
			t.Errorf("ratio %v: expected error", ratio)
			continue
		}
		if !strings.Contains(err.Error(), EnvTraceRatio) {
			t.Errorf("ratio %v: error %q does not mention %s", ratio, err, EnvTraceRatio)
		}
	}
}

func TestSetupTracing_UnknownSampler(t *testing.T) {
	t.Cleanup(restoreGlobalTracing(t))

	_, err := SetupTracing(TracerConfig{Enabled: true, Sampler: "sometimes"})
	if err == nil {
		t.Fatal("expected error for unknown sampler")
	}
	if !strings.Contains(err.Error(), EnvTraceSampler) {
		t.Errorf("error %q does not mention %s", err, EnvTraceSampler)
	}
}

// =============================================================================
// Builders (package-private)
// =============================================================================

func TestBuildOTLPExporter(t *testing.T) {
	for _, tt := range []struct {
		name     string
		insecure bool
	}{
		{"insecure", true},
		{"tls", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			exp, err := buildOTLPExporter(TracerConfig{Endpoint: "127.0.0.1:1", Insecure: tt.insecure})
			if err != nil {
				t.Fatalf("buildOTLPExporter: %v", err)
			}
			if exp == nil {
				t.Fatal("expected non-nil exporter")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := exp.Shutdown(ctx); err != nil {
				t.Fatalf("exporter shutdown: %v", err)
			}
		})
	}
}

func TestBuildSampler(t *testing.T) {
	for _, name := range []string{
		"always",
		"never",
		"parentbased_always",
		"parentbased_never",
		"ratio",
		"parentbased_ratio",
	} {
		t.Run(name, func(t *testing.T) {
			s, err := buildSampler(TracerConfig{Sampler: name, Ratio: 0.25})
			if err != nil {
				t.Fatalf("buildSampler(%q): %v", name, err)
			}
			if s == nil {
				t.Fatalf("buildSampler(%q): nil sampler", name)
			}
		})
	}

	for _, bad := range []string{"", "sometimes", "ALWAYS"} {
		_, err := buildSampler(TracerConfig{Sampler: bad})
		if err == nil {
			t.Errorf("buildSampler(%q): expected error", bad)
			continue
		}
		if !strings.Contains(err.Error(), EnvTraceSampler) {
			t.Errorf("buildSampler(%q): error %q does not mention %s", bad, err, EnvTraceSampler)
		}
	}
}

func TestBuildResource(t *testing.T) {
	res, err := buildResource()
	if err != nil {
		t.Fatalf("buildResource: %v", err)
	}
	if res == nil {
		t.Fatal("expected non-nil resource")
	}
	attrs := make(map[string]string)
	for _, kv := range res.Attributes() {
		attrs[string(kv.Key)] = kv.Value.Emit()
	}
	if attrs["service.name"] != "helix" {
		t.Errorf("service.name = %q, want helix", attrs["service.name"])
	}
	if v := attrs["service.version"]; v == "" {
		t.Error("expected a service.version attribute")
	}
}

// =============================================================================
// Context helpers
// =============================================================================

func TestTraceIDFromContext(t *testing.T) {
	if got := TraceIDFromContext(nil); got != "" {
		t.Errorf("nil ctx → %q, want empty", got)
	}
	if got := TraceIDFromContext(context.Background()); got != "" {
		t.Errorf("empty ctx → %q, want empty", got)
	}

	// A real SDK span carries a valid trace ID.
	tp := sdktrace.NewTracerProvider()
	defer func() { _ = tp.Shutdown(context.Background()) }()
	ctx, span := tp.Tracer("test").Start(context.Background(), "with-trace")
	span.End()

	want := span.SpanContext().TraceID().String()
	if len(want) != 32 {
		t.Fatalf("trace ID %q is not 32 hex chars", want)
	}
	if got := TraceIDFromContext(ctx); got != want {
		t.Errorf("TraceIDFromContext = %q, want %q", got, want)
	}
}

func TestShutdownTraceProvider(t *testing.T) {
	if err := ShutdownTraceProvider(nil); err != nil {
		t.Errorf("nil provider → %v, want nil", err)
	}

	tp := sdktrace.NewTracerProvider()
	if err := ShutdownTraceProvider(tp); err != nil {
		t.Fatalf("first shutdown: %v", err)
	}
	// SDK shutdown is idempotent; ours must be too.
	if err := ShutdownTraceProvider(tp); err != nil {
		t.Fatalf("second shutdown: %v", err)
	}
}

// failingShutdownProcessor is a SpanProcessor whose Shutdown returns an
// error, so the provider's Shutdown surfaces it (joined) to callers.
type failingShutdownProcessor struct{}

func (*failingShutdownProcessor) OnStart(context.Context, sdktrace.ReadWriteSpan) {}
func (*failingShutdownProcessor) OnEnd(sdktrace.ReadOnlySpan)                     {}
func (*failingShutdownProcessor) Shutdown(context.Context) error {
	return errors.New("processor boom")
}
func (*failingShutdownProcessor) ForceFlush(context.Context) error { return nil }

func TestShutdownTraceProvider_PropagatesProcessorError(t *testing.T) {
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(&failingShutdownProcessor{}))
	err := ShutdownTraceProvider(tp)
	if err == nil {
		t.Fatal("expected wrapped shutdown error")
	}
	if !strings.Contains(err.Error(), "tracer shutdown") || !strings.Contains(err.Error(), "processor boom") {
		t.Errorf("error = %q, want wrapped processor error", err)
	}
}

// =============================================================================
// RunWithTrace
// =============================================================================

// recordingSpanProcessor captures every span it is handed at OnEnd, for
// asserting what the tracer actually created.
type recordingSpanProcessor struct {
	mu    sync.Mutex
	spans []sdktrace.ReadOnlySpan
}

func (r *recordingSpanProcessor) OnStart(context.Context, sdktrace.ReadWriteSpan) {}
func (r *recordingSpanProcessor) OnEnd(s sdktrace.ReadOnlySpan) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.spans = append(r.spans, s)
}
func (r *recordingSpanProcessor) Shutdown(context.Context) error   { return nil }
func (r *recordingSpanProcessor) ForceFlush(context.Context) error { return nil }

func (r *recordingSpanProcessor) snapshot() []sdktrace.ReadOnlySpan {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]sdktrace.ReadOnlySpan(nil), r.spans...)
}

func TestRunWithTrace_NoObserver(t *testing.T) {
	clearEnv(t)
	called := false
	err := RunWithTrace("nop", func() error {
		called = true
		return nil
	})
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if !called {
		t.Error("expected fn to be called even without observer")
	}
}

func TestRunWithTrace_NoTracing_BehavesLikeRun(t *testing.T) {
	clearEnv(t)
	_, buf, restore := freshObserver(t, "trace-app")
	defer restore()

	err := RunWithTrace("plain", func() error { return nil })
	if err != nil {
		t.Fatalf("RunWithTrace: %v", err)
	}
	var entry map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry); err != nil {
		t.Fatalf("unmarshal: %v\nraw=%q", err, buf.String())
	}
	if entry["subcommand"] != "plain" {
		t.Errorf("subcommand = %v, want plain", entry["subcommand"])
	}
	if rc, _ := entry["rc"].(float64); rc != 0 {
		t.Errorf("rc = %v, want 0", entry["rc"])
	}
	if _, has := entry["trace_id"]; has {
		t.Error("expected no trace_id field when tracing is not configured")
	}
}

func TestRunWithTrace_NoTracing_PropagatesError(t *testing.T) {
	clearEnv(t)
	_, _, restore := freshObserver(t, "x")
	defer restore()

	sentinel := errors.New("boom")
	err := RunWithTrace("f", func() error { return sentinel })
	if !errors.Is(err, sentinel) {
		t.Errorf("got %v, want %v", err, sentinel)
	}
}

// newTracingObserver installs a global observer with a working trace
// shutdown hook, backed by a recording SDK provider (no exporters — fully
// offline). Returns the observer, the recorder, and the restore closure.
func newTracingObserver(t *testing.T) (*Observer, *recordingSpanProcessor, func()) {
	t.Helper()
	rec := &recordingSpanProcessor{}
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	o := &Observer{tpShutdown: func() error { return nil }}
	return o, rec, SetGlobal(o)
}

func TestRunWithTrace_WithTracing_CreatesSpan(t *testing.T) {
	clearEnv(t)
	t.Cleanup(restoreGlobalTracing(t))
	_, rec, restore := newTracingObserver(t)
	defer restore()

	if err := RunWithTrace("subcmd", func() error { return nil }); err != nil {
		t.Fatalf("RunWithTrace: %v", err)
	}
	spans := rec.snapshot()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if got := spans[0].Name(); got != "helix.subcmd" {
		t.Errorf("span name = %q, want helix.subcmd", got)
	}
	if !spans[0].SpanContext().IsValid() {
		t.Error("expected valid SpanContext")
	}
}

func TestRunWithTrace_WithTracing_RecordsError(t *testing.T) {
	clearEnv(t)
	t.Cleanup(restoreGlobalTracing(t))
	_, rec, restore := newTracingObserver(t)
	defer restore()

	sentinel := errors.New("subcommand failed")
	err := RunWithTrace("bad", func() error { return sentinel })
	if !errors.Is(err, sentinel) {
		t.Errorf("got %v, want %v", err, sentinel)
	}
	spans := rec.snapshot()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	events := spans[0].Events()
	if len(events) == 0 {
		t.Fatal("expected an exception event from span.RecordError")
	}
	if events[0].Name != "exception" {
		t.Errorf("event name = %q, want exception", events[0].Name)
	}
}

// =============================================================================
// Init integration with tracing
// =============================================================================

func TestInit_WithTracing_CloseShutsDownProvider(t *testing.T) {
	clearEnv(t)
	t.Cleanup(restoreGlobalTracing(t))
	buf := &bytes.Buffer{}

	o, err := Init(Options{
		App:    "tracey",
		Format: "json",
		Sink:   buf,
		Tracing: TracerConfig{
			Enabled:  true,
			Endpoint: "127.0.0.1:1",
			Sampler:  "never",
			Ratio:    0.5,
			Insecure: true,
		},
	})
	if err != nil {
		t.Fatalf("Init with tracing: %v", err)
	}
	if o.tpShutdown == nil {
		t.Fatal("expected tpShutdown hook to be installed")
	}

	// runWith takes the trace-context path: context.Background() carries
	// no span, so the entry must NOT carry a trace_id.
	if err := Run("traced", func() error { return nil }); err != nil {
		t.Fatalf("Run: %v", err)
	}
	var entry map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, has := entry["trace_id"]; has {
		t.Error("expected no trace_id: runWith reads context.Background(), which has no span")
	}

	if err := o.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if o.tpShutdown != nil {
		t.Error("expected tpShutdown cleared after Close")
	}
}

func TestInit_WithTracing_SetupErrorSurfaces(t *testing.T) {
	clearEnv(t)
	t.Cleanup(restoreGlobalTracing(t))

	_, err := Init(Options{App: "x", Tracing: TracerConfig{Enabled: true, Ratio: 2.0}})
	if err == nil {
		t.Fatal("expected Init error for out-of-range ratio")
	}
	if !strings.Contains(err.Error(), EnvTraceRatio) {
		t.Errorf("error %q does not mention %s", err, EnvTraceRatio)
	}
}
