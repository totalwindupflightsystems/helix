# Helix — Distributed Tracing Specification

**Spec version:** 1.0
**Status:** Draft
**Last updated:** 2026-07-23
**Implements:** PROD-003
**Depends on:** observability wrapper (`internal/observability/`), Prometheus metrics (`pkg/health/`), config spec (`helix-config.md`)

---

## 1. Overview

Helix has comprehensive Prometheus-style metrics aggregation (`pkg/health/`: PromStore, PlatformMetricsCollector, alerts, SLA monitoring). However, it has **no distributed tracing** — there are no trace spans, no `trace.Span` objects, no trace context propagation across CLI command boundaries or event-driven subsystems. When an operator sees a slow `helix negotiate` in aggregate latency histograms, they cannot trace which sub-operation (Chimera API call, Forgejo fetch, prompt compilation) caused the slowdown.

The OpenTelemetry SDK (`go.opentelemetry.io/otel`) is already a transitive dependency in go.mod (pulled in by SOPS v3.13.2 via Google Cloud Operations). This spec promotes it to a direct dependency and adds distributed tracing instrumentation across Helix.

**Key goals:**
- Add OpenTelemetry trace spans to every CLI subcommand execution
- Propagate trace context across subsystem boundaries (Forgejo HTTP, Chimera gRPC, prompt evaluation)
- Expose trace IDs in structured log lines for correlation
- Provide a lightweight OTLP exporter (gRPC or HTTP) configurable via CLI flags / env vars
- Allow operators to disable tracing entirely (zero overhead when not configured)
- Keep the binary lean — no mandatory OTel dependency at compile time

**Existing state:**
- ✅ Prometheus metrics via `pkg/health/prom.go` (405 lines)
- ✅ Platform metrics recorder via `pkg/health/platform_metrics.go` (701 lines)
- ✅ Observability wrapper via `internal/observability/observability.go` (492 lines)
- ✅ OTel SDK v1.44.0 in go.mod (indirect, promoted to direct by this spec)
- ❌ No OTel trace spans anywhere in the codebase
- ❌ No trace exporter configured
- ❌ No trace propagation in HTTP/gRPC clients or servers

**Non-goals:**
- No mandatory tracing — always opt-in via config/env
- No custom instrumentation agents (no auto-instrumentation bytecode weaving)
- No replacement of existing Prometheus metrics — tracing complements, not replaces
- No per-span sampling decisions beyond the global sampler

---

## 2. Dependencies

| Dependency | Version | Purpose |
|------------|---------|---------|
| `go.opentelemetry.io/otel` | v1.44+ | Core SDK (already indirect, promote to direct) |
| `go.opentelemetry.io/otel/sdk` | v1.44+ | SDK implementation (already indirect) |
| `go.opentelemetry.io/otel/trace` | v1.44+ | Trace API (already indirect) |
| `go.opentelemetry.io/otel/exporters/otlp/otlptrace` | v1.44+ | OTLP exporter (NEW — download) |
| `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc` | v1.44+ | OTLP gRPC transport (NEW — download) |

**Decision: OTLP gRPC over HTTP**
- OTLP gRPC is the canonical OpenTelemetry Collector ingestion protocol
- Most production deployments already run an OTel Collector on the same host
- HTTP/1.1 OTLP is available as a fallback if gRPC is unavailable (optional follow-up)

---

## 3. Tracing Architecture

```
┌──────────────────────────────────────────────────────────────┐
│  cmd/helix (any subcommand)                                  │
│                                                              │
│  internal/observability/                                     │
│    observability.go  ───  Init() creates TracerProvider      │
│    tracer.go (NEW)   ───  OTel setup, config, shutdown       │
│                              │                               │
│                              ▼                               │
│  ┌─────────────────────────────────────────────────┐         │
│  │  TracerProvider (singleton, process-global)      │         │
│  │  • Sampler: parent-based, rate-limited           │         │
│  │  • Exporter: OTLP gRPC → Collector              │         │
│  │  • Resource: service.name=helix, version, host   │         │
│  └─────────────────────────────────────────────────┘         │
│                              │                               │
│                    ┌─────────┴─────────┐                     │
│                    ▼                   ▼                     │
│  ┌─────────────────────┐  ┌─────────────────────┐            │
│  │ Subcommand Span     │  │ HTTP/gRPC Spans     │            │
│  │ (per `helix <cmd>`) │  │ (Forgejo, Chimera,  │            │
│  │ Wraps observability │  │  Dashboard, etc.)   │            │
│  │ .Run wrapper        │  │ OTel HTTP transport │            │
│  └─────────────────────┘  └─────────────────────┘            │
└──────────────────────────────────────────────────────────────┘
```

### Span Hierarchy

```
helix negotiate                                        [root span]
  ├── helix.negotiate.fetch_forgejo_pr                 [child span]
  │     └── HTTP GET /api/v1/repos/{owner}/{repo}/pulls/{id}
  │                                                      [HTTP span via otelhttp]
  ├── helix.negotiate.compile_prompts                  [child span]
  ├── helix.negotiate.call_chimera                     [child span]
  │     └── CHIMERA /api/v1/deliberate                 [HTTP span]
  └── helix.negotiate.record_audit                     [child span]
```

### Span Attributes

Every span carries:
| Attribute | Example | Source |
|-----------|---------|--------|
| `service.name` | `helix` | Static |
| `helix.version` | `v1.2.3` | Build tag |
| `helix.subcommand` | `negotiate` | cobra command |
| `helix.pr_id` | `42` | Dynamic (when applicable) |
| `helix.trace_id` | (auto) | OTel trace ID |
| `helix.agent_id` | `agent-abc` | HELIX_AGENT_ID env |

---

## 4. Configuration

All configuration is via environment variables, consistent with Helix's existing env-var-first design pattern (`HELIX_LOG_FORMAT`, `HELIX_AGENT_ID`, etc.).

| Env Var | Default | Purpose |
|---------|---------|---------|
| `HELIX_TRACE_ENABLED` | `false` | Enable tracing (opt-in) |
| `HELIX_TRACE_ENDPOINT` | `http://localhost:4317` | OTLP gRPC endpoint |
| `HELIX_TRACE_SAMPLER` | `always` | `always`, `never`, `parentbased_always`, `ratio=N` |
| `HELIX_TRACE_RATIO` | `0.1` | Sampling ratio when sampler=ratio (float 0.0–1.0) |
| `HELIX_TRACE_INSECURE` | `true` | Allow insecure gRPC (default: local collector) |

When `HELIX_TRACE_ENABLED=false` (the default), the TracerProvider is a no-op (`trace.NewNoopTracerProvider()`). There is zero runtime overhead — no connection to the collector is attempted, no goroutines are spawned. This satisfies the "zero overhead when not configured" goal.

---

## 5. Integration Points

### 5.1 Tracer Provider Lifecycle

The TracerProvider is created once in `internal/observability/tracer.go` (NEW file) and initialized during `observability.Init()`. It is shut down during `observability.Close()` to flush pending spans.

### 5.2 Subcommand Instrumentation

The existing `observability.Run()` wrapper already captures subcommand execution. A new `observability.RunWithTrace()` variant wraps the subcommand in a root trace span:

```go
func RunWithTrace(ctx context.Context, name string, fn func() error) (rc error) {
    tracer := trace.Lookup("helix")
    ctx, span := tracer.Start(ctx, "helix."+name, trace.WithSpanKind(trace.SpanKindInternal))
    defer func() {
        if rc != nil {
            span.RecordError(rc)
            span.SetStatus(codes.Error, rc.Error())
        }
        span.End()
    }()
    return fn()
}
```

### 5.3 Forgejo HTTP Calls

The `pkg/forgejo/client.go` HTTP transport should wrap its `http.RoundTripper` with `otelhttp.NewTransport()`:

```go
import "go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

client := &http.Client{
    Transport: otelhttp.NewTransport(
        http.DefaultTransport,
        otelhttp.WithSpanNameFormatter(func(operation string, r *http.Request) string {
            return "forgejo." + r.Method + " " + r.URL.Path
        }),
    ),
}
```

This automatically creates HTTP client spans for every Forgejo API call, with HTTP method, URL, and status code as attributes.

### 5.4 Chimera gRPC Calls

The `pkg/review/client_chimera.go` HTTP client should use the same `otelhttp.NewTransport()` pattern for HTTP-based Chimera calls.

### 5.5 Log-Trace Correlation

Structured log lines (JSON format, already in `internal/observability/`) should include the trace ID when tracing is active:

```json
{"time":"2026-07-23T04:00:00Z","level":"info","msg":"subcommand_complete","subcommand":"negotiate","rc":0,"trace_id":"abcd1234..."}
```

This is achieved by storing the trace ID in a context value or passing `trace.SpanFromContext(ctx).SpanContext().TraceID()`.

---

## 6. Implementation Plan

### Phase 1: Tracer Provider + Core Wiring (PROD-003a — Cpx 2)

| # | File | Description |
|---|------|-------------|
| 1 | `internal/observability/tracer.go` | TracerProvider setup, config parsing, shutdown |
| 2 | `internal/observability/observability.go` | Add trace ID to log lines, wire in Init/Close |
| 3 | `cmd/helix/*.go` | Update to use `RunWithTrace` for all subcommands |
| 4 | `go.mod` + `go.sum` | Promote OTel deps to direct, add OTLP exporter |

**Acceptance criteria:**
- `HELIX_TRACE_ENABLED=true helix status` sends a root span to the configured OTLP endpoint
- `HELIX_TRACE_ENABLED=false` (default) has zero overhead — no connection, no goroutines
- Trace ID appears in JSON log lines when tracing is enabled
- All existing tests pass; CI build stays green

### Phase 2: HTTP Instrumentation (PROD-003b — Cpx 2)

| # | File | Description |
|---|------|-------------|
| 1 | `pkg/forgejo/client.go` | Wrap HTTP transport with otelhttp |
| 2 | `pkg/review/client_chimera.go` | Wrap HTTP transport with otelhttp |
| 3 | `pkg/review/client_deepseek.go` | Wrap HTTP transport with otelhttp |

**Acceptance criteria:**
- Forgejo/Chimera/DeepSeek HTTP calls emit child spans under the parent subcommand span
- HTTP method, URL path, and status code are present as span attributes
- Error responses propagate span status correctly

---

## 7. Error Catalog

| Code | Type | Description |
|------|------|-------------|
| TE-001 | Config | `HELIX_TRACE_RATIO` not a valid float (0.0–1.0) |
| TE-002 | Config | `HELIX_TRACE_SAMPLER` unrecognized value |
| TE-003 | Runtime | OTLP exporter connection refused (non-fatal — logged, tracing degraded) |
| TE-004 | Shutdown | TracerProvider.Shutdown timeout exceeded (logged, best-effort flush) |

---

## 8. Future Work

- `HELIX_TRACE_HEADERS` — propagate trace context via W3C traceparent/tracestate from incoming webhooks
- OpenTelemetry Collector configuration guide for Helix operators
- Per-span attributes for prompt hashing (trace prompt provenance end-to-end)
- Trace-based test assertions (verify spans emitted during integration tests)

---

## 9. Build Order

1. `internal/observability/tracer.go` — new file (TracerProvider, config parsing)
2. Patch `internal/observability/observability.go` — wire into Init/Close, add trace ID to log output
3. `go mod edit -require` — promote OTel deps to direct, add OTLP exporter
4. `go mod tidy` — resolve transitive deps
5. `go build ./...` — verify compilation
6. `go test ./...` — verify all tests pass
7. Phase 2: HTTP instrumentation in forgejo, chimera, deepseek clients
