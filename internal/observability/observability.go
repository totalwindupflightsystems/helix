// Package observability provides a unified, shared structured-logging
// wrapper for every Helix CLI binary (helix, helix-identity, helix-estimate,
// helix-negotiate, helix-prompt, helix-marketplace, sandbox).
//
// Why a shared package? Previously the observability wrapper lived inside
// cmd/helix/observability.go and could not be reused by the delegated
// binaries (which have their own main packages). This package hoists the
// useful surface — config resolution, init, subcommand wrapping — so every
// CLI emits a single uniform "subcommand_complete" log line on exit.
//
// Design goals:
//
//   - Zero coupling between this package and any particular subcommand
//     binary. All state is process-global (one *log.Logger + a metrics
//     hook), configured exactly once at startup by the binary's main().
//
//   - Optional metrics hook. Helix's unified `helix` binary integrates
//     with pkg/health.PromStore to feed Prometheus counters; the
//     delegated binaries don't have that, so the hook is a no-op when
//     not installed. The interface is intentionally tiny so callers
//     don't need to import pkg/health just to compile.
//
//   - Same flag/env contract as cmd/helix's previous behaviour:
//     --log-format text|json, HELIX_LOG_FORMAT, HELIX_LOG=1,
//     HELIX_LOG_FILE, HELIX_AGENT_ID. This lets operators move between
//     binaries without learning new knobs.
//
//   - Stable output. JSON keys are sorted alphabetically, text mode uses
//     fixed short labels, every line ends with a newline. Two binaries
//     emitting at the same time will produce identical formatting.
//
// Typical usage from a delegated CLI:
//
//	import "github.com/totalwindupflightsystems/helix/internal/observability"
//
//	func main() {
//	    // Set up logging before cobra so parse errors are logged.
//	    obs, err := observability.Init(observability.Options{
//	        App:    "helix-identity",
//	        Format: "", // defer to env / default
//	    })
//	    if err != nil { fmt.Fprintln(os.Stderr, err); os.Exit(2) }
//	    defer obs.Close()
//
//	    rootCmd := buildRootCmd()
//	    rc := observability.Run(rootCmd.Name(), func() error {
//	        return rootCmd.Execute()
//	    })
//	    if rc != nil { os.Exit(observability.ExitCodeFrom(rc)) }
//	}
package observability

import (
	"context"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/totalwindupflightsystems/helix/pkg/log"
)

// =============================================================================
// Environment variables
// =============================================================================
//
// Stable names — DO NOT rename without coordinating with operators that
// have production log shipping pipelines keyed on these.

const (
	// EnvLog enables DEBUG-level entries when set to a truthy value
	// ("1", "true", "debug"). Empty or unset → INFO. Case-insensitive.
	EnvLog = "HELIX_LOG"

	// EnvLogFormat selects output format. Accepts "text" | "json" |
	// legacy alias "logfmt" (mapped to "text"). Unknown values surface
	// as an Init error.
	EnvLogFormat = "HELIX_LOG_FORMAT"

	// EnvLogFile overrides stderr. The file is opened in append mode
	// with mode 0600 (umask respected). Created if missing.
	EnvLogFile = "HELIX_LOG_FILE"

	// EnvAgentID attaches an agent_id field to every emitted entry
	// when set. Per-invocation overrides (RunWithObsAgent) win over
	// this env var.
	EnvAgentID = "HELIX_AGENT_ID"
)

// internal format aliases. "logfmt" is an accepted legacy alias for "text".
const (
	formatText   = "text"
	formatJSON   = "json"
	formatLogfmt = "logfmt"
	defaultFmt   = formatText
)

// =============================================================================
// MetricsRecorder is the optional integration point for Prometheus metrics.
// =============================================================================
//
// The package emits structured log entries on every subcommand invocation.
// It can ALSO feed a metrics counter (used by cmd/helix to populate the
// helix_subcommand_* metric family on the /metrics endpoint).
//
// Pass nil in Options.Metrics to disable. The interface is intentionally
// minimal — only what every recorder needs.
type MetricsRecorder interface {
	// RecordInvocation is called exactly once per completed subcommand.
	// `rc` is the exit code (0 = clean, non-zero = error). `duration` is
	// the wall-clock time the subcommand took. Implementations must be
	// safe for concurrent use.
	RecordInvocation(name string, rc int, duration time.Duration)
}

// =============================================================================
// Options configures Init().
// =============================================================================
//
// Zero values are safe: an empty Format defers to env vars (or "text"),
// an empty Sink defers to os.Stderr, an empty App produces entries without
// an "app" field, an empty AgentID defers to env.
type Options struct {
	// App sets the "app" baseline field on every emitted entry. Each
	// Helix CLI should pass its binary name (e.g. "helix-identity",
	// "helix-estimate"). Trimmed; empty omits the field.
	App string

	// Format is the --log-format flag value (also overridable via
	// EnvLogFormat). Accepts "text" | "json" | "logfmt". Empty defers
	// to env, then to "text".
	Format string

	// Sink is the destination for log entries. nil → os.Stderr.
	// HELIX_LOG_FILE overrides this if both are set.
	Sink io.Writer

	// AgentID forces a specific agent_id on every entry. Empty defers
	// to EnvAgentID, then omits the field.
	AgentID string

	// Metrics is an optional metrics recorder (PromStore or compatible).
	// nil → no-op. The same instance is shared across all wrapped
	// subcommands in this process.
	Metrics MetricsRecorder

	// LogFileMode overrides the default 0o600 when HELIX_LOG_FILE
	// creates the destination. Zero → 0o600.
	LogFileMode os.FileMode

	// Tracing configures OpenTelemetry distributed tracing. The zero
	// value (Enabled: false) returns a no-op TracerProvider with zero
	// overhead — no goroutines, no connections. See SetupTracing in
	// tracer.go for the full env-var contract.
	Tracing TracerConfig
}

// =============================================================================
// Observer — process-global handle returned by Init.
// =============================================================================
//
// Observer owns the logger + metrics recorder + agent-id binding. There
// is one Observer per process. The zero value is NOT usable — call Init.
type Observer struct {
	app     string
	logger  *log.Logger
	metrics MetricsRecorder
	agentID string

	// ownsSink is true when the Observer created the underlying sink
	// (i.e. from HELIX_LOG_FILE). Close() closes it; sinks passed in
	// via Options.Sink are the caller's responsibility.
	ownsSink bool
	sink     io.Writer

	// tpShutdown flushes pending trace spans during Close(). Nil when
	// tracing is disabled (the no-op case).
	tpShutdown func() error
}

// =============================================================================
// Package-global singleton
// =============================================================================
//
// One Observer per process. Held in a Mutex-protected variable so
// concurrent Init / Wrap calls don't race.
var (
	globalMu sync.RWMutex
	global   *Observer
)

// SetGlobal installs o as the process-global Observer. Most callers
// should use Init() instead; this entry point exists for tests that
// need to swap the observer mid-process (use the returned restore
// closure to undo).
func SetGlobal(o *Observer) func() {
	globalMu.Lock()
	defer globalMu.Unlock()
	prev := global
	global = o
	return func() {
		globalMu.Lock()
		defer globalMu.Unlock()
		global = prev
	}
}

// Global returns the current process-global Observer, or nil if Init
// has not been called. Wrap calls read this lazily — if no observer is
// installed, Wrap becomes a no-op (run() is invoked directly).
func Global() *Observer {
	globalMu.RLock()
	defer globalMu.RUnlock()
	return global
}

// =============================================================================
// Init — process-global setup
// =============================================================================
//
// Init resolves the logger configuration from Options + env vars and
// installs the resulting Observer as the process-global handle. Returns
// the Observer so callers can hold a reference (e.g. for Close at
// shutdown).
//
// Safe to call multiple times: each call installs a fresh Observer and
// returns a Close closure. The previous Observer is replaced; its
// resources are NOT closed automatically (the caller that created it
// owns its lifetime).
func Init(opts Options) (*Observer, error) {
	format, err := resolveFormat(opts.Format)
	if err != nil {
		return nil, err
	}
	level := resolveLevel()

	sink, ownsSink, err := resolveSink(opts.Sink, opts.LogFileMode)
	if err != nil {
		return nil, err
	}

	logger, err := log.New(sink, format, level)
	if err != nil {
		return nil, err
	}

	app := strings.TrimSpace(opts.App)
	if app != "" {
		logger = logger.WithApp(app)
	}

	// Set up tracing (PROD-003a). When opts.Tracing is the zero value
	// (Enabled == false), SetupTracing returns a no-op provider with
	// zero overhead — no goroutines, no connections, no exporters started.
	// Tracing setup failures are non-fatal: log and continue.
	tp, tpErr := SetupTracing(opts.Tracing)
	if tpErr != nil {
		return nil, tpErr
	}

	o := &Observer{
		app:      app,
		logger:   logger,
		metrics:  opts.Metrics,
		agentID:  strings.TrimSpace(opts.AgentID),
		ownsSink: ownsSink,
		sink:     sink,
	}
	if tp != nil {
		o.tpShutdown = func() error { return ShutdownTraceProvider(tp) }
	}
	SetGlobal(o)
	return o, nil
}

// Close releases resources owned by the Observer (the file handle
// opened from HELIX_LOG_FILE, if any, and the TracerProvider, if
// tracing is configured). Idempotent; safe to call on an Observer
// whose sink was supplied externally.
//
// Shutdown order: TracerProvider flush first so spans are emitted
// before the log sink closes (preserves ordering in the log-trace
// correlation).
func (o *Observer) Close() error {
	if o == nil {
		return nil
	}
	// Flush pending trace spans before closing the log sink.
	if o.tpShutdown != nil {
		_ = o.tpShutdown()
		o.tpShutdown = nil
	}
	if !o.ownsSink {
		return nil
	}
	if c, ok := o.sink.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

// Logger returns the underlying *log.Logger so callers can emit custom
// entries (DEBUG breadcrumbs, etc.) outside the Wrap flow.
func (o *Observer) Logger() *log.Logger {
	if o == nil {
		return nil
	}
	return o.logger
}

// App returns the configured app name (empty if unset).
func (o *Observer) App() string {
	if o == nil {
		return ""
	}
	return o.app
}

// =============================================================================
// Configuration helpers
// =============================================================================

// resolveFormat merges Options.Format with EnvLogFormat, normalising
// the legacy "logfmt" alias to "text". Empty input → "text".
func resolveFormat(flag string) (string, error) {
	f := strings.ToLower(strings.TrimSpace(flag))
	if f == "" {
		f = strings.ToLower(strings.TrimSpace(os.Getenv(EnvLogFormat)))
	}
	switch f {
	case "":
		return defaultFmt, nil
	case formatText, formatLogfmt:
		return formatText, nil
	case formatJSON:
		return formatJSON, nil
	}
	return "", &ConfigError{Field: EnvLogFormat, Value: flag, Msg: "expected text | json"}
}

// resolveLevel reads EnvLog. Truthy values ("1", "true", "debug") enable
// DEBUG; everything else → INFO.
func resolveLevel() log.Level {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(EnvLog)))
	if v == "1" || v == "true" || v == "debug" {
		return log.LevelDebug
	}
	return log.LevelInfo
}

// resolveSink returns the writer + an `ownsSink` flag indicating whether
// the caller should close it. Precedence:
//  1. HELIX_LOG_FILE (always wins, creates the file with LogFileMode)
//  2. opts.Sink (caller-supplied; not owned)
//  3. os.Stderr (default; not owned)
func resolveSink(supplied io.Writer, mode os.FileMode) (io.Writer, bool, error) {
	if path := strings.TrimSpace(os.Getenv(EnvLogFile)); path != "" {
		if mode == 0 {
			mode = 0o600
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, mode)
		if err != nil {
			return nil, false, &ConfigError{Field: EnvLogFile, Value: path, Msg: err.Error()}
		}
		return f, true, nil
	}
	if supplied != nil {
		return supplied, false, nil
	}
	return os.Stderr, false, nil
}

// ConfigError is returned when HELIX_LOG_FORMAT or HELIX_LOG_FILE is
// malformed.
type ConfigError struct {
	Field string
	Value string
	Msg   string
}

func (e *ConfigError) Error() string {
	return "observability: " + e.Field + "=" + strconv.Quote(e.Value) + ": " + e.Msg
}

// =============================================================================
// Run wrappers
// =============================================================================
//
// Run / RunWithObsAgent / RunWithDryRun are the primary entry points.
// They call fn, measure wall-clock duration, emit exactly one structured
// log entry ("subcommand_complete"), record metrics (if installed), and
// return fn's error unchanged so the caller can map it to an exit code.
//
// All functions are no-ops (besides fn's invocation) if no Observer
// has been installed — keeps tests safe without per-test setup.

// Run wraps fn with structured observability. The emitted entry uses
// the env-derived or observer-configured agent_id; pass RunWithObsAgent
// to override explicitly.
func Run(name string, fn func() error) error {
	return runWith(Global(), name, "", fn, false)
}

// RunWithObsAgent wraps fn with an explicit agent_id. Use this when the
// caller has authoritative agent knowledge (test fixtures, dispatcher
// context). The env var EnvAgentID is ignored when agentID is non-empty.
func RunWithObsAgent(name, agentID string, fn func() error) error {
	return runWith(Global(), name, strings.TrimSpace(agentID), fn, false)
}

// RunWithDryRun is the variant that takes an explicit dryRun flag —
// matches the cmd/helix.RunWithObs signature that bundles dry_run into
// the emitted entry.
func RunWithDryRun(name string, dryRun bool, fn func() error) error {
	return runWith(Global(), name, "", fn, dryRun)
}

// RunWithTrace wraps fn with a root OpenTelemetry trace span. This is
// the PROD-003a entry point: every CLI subcommand should call this
// instead of Run when distributed tracing is desired. When tracing is
// disabled (tpShutdown is nil), RunWithTrace behaves identically to
// Run — it measures duration and emits the log entry, but without
// span creation.
func RunWithTrace(name string, fn func() error) error {
	o := Global()
	if o == nil || o.tpShutdown == nil {
		// Tracing not configured — behave like Run.
		return runWith(o, name, "", fn, false)
	}
	tracer := otel.Tracer("helix")
	_, span := tracer.Start(context.Background(), "helix."+name)
	defer func() {
		span.End()
	}()
	err := fn()
	if err != nil {
		span.RecordError(err)
	}
	return err
}

// runWith is the shared implementation. It is package-private so the
// public surface stays small.
//
// rc extraction:
//   - If fn returned an *ExitError, use its Code.
//   - Else, default to 0 (the caller's exit-code mapping in main()
//     handles non-zero exit semantics).
//
// Callers that need a specific non-zero rc surfaced in the log entry
// should wrap their error in *ExitError.
func runWith(o *Observer, name, agentID string, fn func() error, dryRun bool) error {
	if o == nil {
		return fn()
	}
	start := time.Now()
	err := fn()
	duration := time.Since(start)

	rc := 0
	if err != nil {
		if e, ok := err.(*ExitError); ok {
			rc = e.Code
		} else if e, ok := err.(interface{ ExitCode() int }); ok {
			rc = e.ExitCode()
		} else {
			// Generic error → non-zero rc so log readers can filter.
			rc = 1
		}
	}

	if o.metrics != nil {
		o.metrics.RecordInvocation(name, rc, duration)
	}

	fields := map[string]any{
		"subcommand":  name,
		"rc":          rc,
		"duration_ms": duration.Milliseconds(),
		"dry_run":     dryRun,
	}
	if agentID != "" {
		fields["agent_id"] = agentID
	} else if aid := o.agentID; aid != "" {
		fields["agent_id"] = aid
	} else if aid := strings.TrimSpace(os.Getenv(EnvAgentID)); aid != "" {
		fields["agent_id"] = aid
	}

	// Attach the current trace ID when tracing is active. This lets
	// operators correlate structured log lines with distributed trace
	// spans in their observability backend.
	if o.tpShutdown != nil {
		sc := trace.SpanContextFromContext(context.Background())
		if sc.IsValid() {
			fields["trace_id"] = sc.TraceID().String()
		}
	}

	fields["pid"] = os.Getpid()

	lvl := log.LevelInfo
	msg := "subcommand_complete"
	if rc != 0 {
		lvl = log.LevelWarn
	}
	o.logger.Emit(lvl, msg, fields)

	return err
}

// =============================================================================
// ExitError — adapter so callers can wrap exit codes
// =============================================================================
//
// Standard library's os.Exit isn't testable. Callers that need to
// surface a specific non-zero rc in the log entry should return an
// *ExitError from their wrapped function. Other error types are
// treated as rc=1.

// ExitError wraps a non-zero return code. It satisfies `error` and
// exposes the code via the field and ExitCode().
type ExitError struct {
	Code int
	Msg  string
}

// NewExitError returns an *ExitError with the supplied code and msg.
// Convenience for callers; equivalent to &ExitError{Code: c, Msg: m}.
func NewExitError(code int, msg string) *ExitError {
	return &ExitError{Code: code, Msg: msg}
}

func (e *ExitError) Error() string {
	if e.Msg == "" {
		return "observability: exit " + strconv.Itoa(e.Code)
	}
	return e.Msg
}

// ExitCode returns e.Code. Satisfies the optional ExitCoder interface
// recognised by runWith.
func (e *ExitError) ExitCode() int { return e.Code }

// ExitCodeFrom extracts a process exit code from err. If err is an
// *ExitError, returns its Code. If err implements ExitCode() int,
// returns that. Else 1 (or 0 if err is nil).
//
// Convenience for main() — keeps the wrappers clean.
func ExitCodeFrom(err error) int {
	if err == nil {
		return 0
	}
	if e, ok := err.(*ExitError); ok {
		return e.Code
	}
	if e, ok := err.(interface{ ExitCode() int }); ok {
		return e.ExitCode()
	}
	return 1
}
