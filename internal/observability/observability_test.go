package observability

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/log"
)

// recordingMetrics is a thread-safe MetricsRecorder used in tests.
type recordingMetrics struct {
	mu    sync.Mutex
	calls []recordedInvocation
}

type recordedInvocation struct {
	name     string
	rc       int
	duration time.Duration
}

func (r *recordingMetrics) RecordInvocation(name string, rc int, d time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, recordedInvocation{name, rc, d})
}

func (r *recordingMetrics) snapshot() []recordedInvocation {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]recordedInvocation, len(r.calls))
	copy(out, r.calls)
	return out
}

// freshObserver builds a buffer-backed Observer and registers it as the
// process-global. Returns the observer, the underlying buffer (for
// assertions), and a restore closure.
func freshObserver(t *testing.T, app string) (*Observer, *bytes.Buffer, func()) {
	t.Helper()
	buf := &bytes.Buffer{}
	o, err := Init(Options{
		App:    app,
		Format: "json",
		Sink:   buf,
	})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	restore := SetGlobal(o)
	return o, buf, restore
}

func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{EnvLog, EnvLogFormat, EnvLogFile, EnvAgentID} {
		old, had := os.LookupEnv(k)
		_ = os.Unsetenv(k)
		if had {
			t.Cleanup(func() { _ = os.Setenv(k, old) })
		}
	}
}

// =============================================================================
// Init / Close
// =============================================================================

func TestInit_DefaultsToText(t *testing.T) {
	clearEnv(t)
	// Buffer-backed; format will be "text" (default) since env is empty.
	buf := &bytes.Buffer{}
	o, err := Init(Options{App: "test", Format: "", Sink: buf})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer o.Close()
	if got := o.Logger(); got == nil {
		t.Fatal("expected non-nil Logger")
	}
	// Emit one entry and verify the text rendering includes app="test".
	o.logger.Emit(log.LevelInfo, "hello", nil)
	if !strings.Contains(buf.String(), `app=test`) {
		t.Errorf("expected app=test in text output, got %q", buf.String())
	}
}

func TestInit_RejectsBadFormat(t *testing.T) {
	clearEnv(t)
	_, err := Init(Options{App: "x", Format: "yaml"})
	if err == nil {
		t.Fatal("expected error for unknown format")
	}
	if !strings.Contains(err.Error(), "HELIX_LOG_FORMAT") {
		t.Errorf("expected error to mention HELIX_LOG_FORMAT, got %q", err.Error())
	}
}

func TestInit_EnvFormatWinsOverEmpty(t *testing.T) {
	clearEnv(t)
	t.Setenv(EnvLogFormat, "json")
	buf := &bytes.Buffer{}
	o, err := Init(Options{App: "x", Format: "", Sink: buf})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer o.Close()
	o.logger.Emit(log.LevelInfo, "h", nil)
	out := strings.TrimSpace(buf.String())
	if !strings.HasPrefix(out, "{") {
		t.Errorf("expected JSON output, got %q", out)
	}
}

func TestInit_LogfmtAliasMapsToText(t *testing.T) {
	clearEnv(t)
	t.Setenv(EnvLogFormat, "logfmt")
	buf := &bytes.Buffer{}
	o, err := Init(Options{App: "x", Sink: buf})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer o.Close()
	if o.logger == nil {
		t.Fatal("expected non-nil logger")
	}
	// Confirm format was normalised: emit JSON, expect text rendering.
	o.logger.Emit(log.LevelInfo, "k", nil)
	if strings.HasPrefix(strings.TrimSpace(buf.String()), "{") {
		t.Errorf("expected text rendering for logfmt alias, got JSON: %q", buf.String())
	}
}

func TestInit_LogFileOpensAppendMode(t *testing.T) {
	clearEnv(t)
	dir := t.TempDir()
	logPath := filepath.Join(dir, "sub.log")
	t.Setenv(EnvLogFile, logPath)

	o, err := Init(Options{App: "x", Format: "json"})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	o.logger.Emit(log.LevelInfo, "first", nil)
	if err := o.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	o2, err := Init(Options{App: "x", Format: "json"})
	if err != nil {
		t.Fatalf("Init 2: %v", err)
	}
	o2.logger.Emit(log.LevelInfo, "second", nil)
	if err := o2.Close(); err != nil {
		t.Fatalf("Close 2: %v", err)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(data), `"first"`) || !strings.Contains(string(data), `"second"`) {
		t.Errorf("expected both entries appended, got:\n%s", data)
	}
}

func TestInit_LogFileInvalidPathErrors(t *testing.T) {
	clearEnv(t)
	t.Setenv(EnvLogFile, "/nonexistent_root_zzzzzz/log.txt")
	_, err := Init(Options{App: "x"})
	if err == nil {
		t.Fatal("expected error for invalid log path")
	}
	if !strings.Contains(err.Error(), "HELIX_LOG_FILE") {
		t.Errorf("expected error to mention HELIX_LOG_FILE, got %q", err.Error())
	}
}

func TestObserver_Close_NoSinkOwnership(t *testing.T) {
	// When the sink was supplied externally, Close is a no-op.
	o := &Observer{ownsSink: false, sink: &bytes.Buffer{}}
	if err := o.Close(); err != nil {
		t.Errorf("expected no-op close, got %v", err)
	}
}

func TestObserver_Close_NilSafe(t *testing.T) {
	var o *Observer
	if err := o.Close(); err != nil {
		t.Errorf("expected nil-receiver no-op, got %v", err)
	}
	if got := o.Logger(); got != nil {
		t.Errorf("expected nil Logger from nil receiver, got %v", got)
	}
	if got := o.App(); got != "" {
		t.Errorf("expected empty App from nil receiver, got %q", got)
	}
}

// =============================================================================
// Global set / get
// =============================================================================

func TestGlobal_NilByDefault(t *testing.T) {
	clearEnv(t)
	prev := Global()
	restore := SetGlobal(nil)
	defer restore()
	if Global() != nil {
		t.Error("expected nil after SetGlobal(nil)")
	}
	_ = prev
}

func TestSetGlobal_RestoresPrevious(t *testing.T) {
	clearEnv(t)
	buf := &bytes.Buffer{}
	o, err := Init(Options{App: "first", Format: "json", Sink: buf})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if Global() != o {
		t.Error("expected Global() == o after Init")
	}
	restore := SetGlobal(nil)
	if Global() != nil {
		t.Error("expected nil after SetGlobal(nil)")
	}
	restore()
	if Global() != o {
		t.Error("expected Global() == o after restore")
	}
}

// =============================================================================
// Run wrappers
// =============================================================================

func TestRun_NoObserverIsNoOp(t *testing.T) {
	clearEnv(t)
	// No Init() — Global() should be nil.
	called := false
	err := Run("test", func() error {
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

func TestRun_HappyPath(t *testing.T) {
	clearEnv(t)
	_, buf, restore := freshObserver(t, "test-app")
	defer restore()

	err := Run("greet", func() error { return nil })
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var entry map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry); err != nil {
		t.Fatalf("unmarshal: %v\nraw=%q", err, buf.String())
	}
	if entry["subcommand"] != "greet" {
		t.Errorf("subcommand=%v", entry["subcommand"])
	}
	if rc, _ := entry["rc"].(float64); rc != 0 {
		t.Errorf("rc=%v want 0", entry["rc"])
	}
	if entry["app"] != "test-app" {
		t.Errorf("app=%v want test-app", entry["app"])
	}
	if msg, _ := entry["msg"].(string); msg != "subcommand_complete" {
		t.Errorf("msg=%v", msg)
	}
	if lvl, _ := entry["level"].(string); lvl != "info" {
		t.Errorf("level=%v want info", lvl)
	}
	if _, has := entry["dry_run"]; !has {
		t.Error("expected dry_run field in entry")
	}
	if _, has := entry["pid"]; !has {
		t.Error("expected pid field in entry")
	}
}

func TestRun_NonZeroExit_UsesExitError(t *testing.T) {
	clearEnv(t)
	_, buf, restore := freshObserver(t, "x")
	defer restore()

	err := Run("fail", func() error {
		return NewExitError(7, "boom")
	})
	if err == nil {
		t.Fatal("expected error to bubble up")
	}
	var entry map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rc, _ := entry["rc"].(float64); rc != 7 {
		t.Errorf("rc=%v want 7", entry["rc"])
	}
	if lvl, _ := entry["level"].(string); lvl != "warn" {
		t.Errorf("level=%v want warn", entry["level"])
	}
}

func TestRun_NonZeroExit_GenericErrorDefaultsToOne(t *testing.T) {
	clearEnv(t)
	_, buf, restore := freshObserver(t, "x")
	defer restore()

	_ = Run("err", func() error {
		return errors.New("kaboom")
	})
	var entry map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rc, _ := entry["rc"].(float64); rc != 1 {
		t.Errorf("rc=%v want 1 (generic error)", entry["rc"])
	}
}

func TestRun_ExitCodeMethod(t *testing.T) {
	clearEnv(t)
	_, buf, restore := freshObserver(t, "x")
	defer restore()

	te := &exitCoderErr{code: 42}
	_ = Run("typed", func() error { return te })
	var entry map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rc, _ := entry["rc"].(float64); rc != 42 {
		t.Errorf("rc=%v want 42", entry["rc"])
	}
}

type exitCoderErr struct{ code int }

func (e *exitCoderErr) Error() string { return "typed" }
func (e *exitCoderErr) ExitCode() int { return e.code }

func TestRunWithObsAgent_ExplicitID(t *testing.T) {
	clearEnv(t)
	_, buf, restore := freshObserver(t, "x")
	defer restore()

	_ = RunWithObsAgent("sub", "agent-007", func() error { return nil })
	var entry map[string]any
	_ = json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry)
	if entry["agent_id"] != "agent-007" {
		t.Errorf("agent_id=%v want agent-007", entry["agent_id"])
	}
}

func TestRun_EnvAgentID(t *testing.T) {
	clearEnv(t)
	t.Setenv(EnvAgentID, "env-agent-42")
	_, buf, restore := freshObserver(t, "x")
	defer restore()

	_ = Run("envrun", func() error { return nil })
	var entry map[string]any
	_ = json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry)
	if entry["agent_id"] != "env-agent-42" {
		t.Errorf("agent_id=%v want env-agent-42", entry["agent_id"])
	}
}

func TestRun_ExplicitAgentOverridesEnv(t *testing.T) {
	clearEnv(t)
	t.Setenv(EnvAgentID, "env-agent")
	_, buf, restore := freshObserver(t, "x")
	defer restore()

	_ = RunWithObsAgent("sub", "explicit-agent", func() error { return nil })
	var entry map[string]any
	_ = json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry)
	if entry["agent_id"] != "explicit-agent" {
		t.Errorf("agent_id=%v want explicit-agent", entry["agent_id"])
	}
}

func TestRun_ObserverAgentOverridesEnv(t *testing.T) {
	clearEnv(t)
	t.Setenv(EnvAgentID, "env-agent")
	buf := &bytes.Buffer{}
	obs, err := Init(Options{App: "x", AgentID: "obs-agent", Sink: buf, Format: "json"})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer obs.Close()

	_ = Run("sub", func() error { return nil })
	var entry map[string]any
	_ = json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry)
	if entry["agent_id"] != "obs-agent" {
		t.Errorf("agent_id=%v want obs-agent (observer-level wins over env)", entry["agent_id"])
	}
}

func TestRunWithDryRun_FieldSurfaced(t *testing.T) {
	clearEnv(t)
	_, buf, restore := freshObserver(t, "x")
	defer restore()

	_ = RunWithDryRun("sub", true, func() error { return nil })
	var entry map[string]any
	_ = json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry)
	if dry, _ := entry["dry_run"].(bool); !dry {
		t.Errorf("dry_run=%v want true", entry["dry_run"])
	}
}

func TestRun_DurationIsRecorded(t *testing.T) {
	clearEnv(t)
	_, buf, restore := freshObserver(t, "x")
	defer restore()

	_ = Run("sleepy", func() error {
		time.Sleep(10 * time.Millisecond)
		return nil
	})
	var entry map[string]any
	_ = json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry)
	if dms, _ := entry["duration_ms"].(float64); dms < 5 {
		t.Errorf("duration_ms=%v want >=5", entry["duration_ms"])
	}
}

func TestRun_MetricsRecorderInvoked(t *testing.T) {
	clearEnv(t)
	rec := &recordingMetrics{}
	buf := &bytes.Buffer{}
	obs, err := Init(Options{App: "x", Sink: buf, Format: "json", Metrics: rec})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer obs.Close()

	_ = Run("metric-sub", func() error { return nil })
	calls := rec.snapshot()
	if len(calls) != 1 {
		t.Fatalf("expected 1 metrics call, got %d", len(calls))
	}
	if calls[0].name != "metric-sub" {
		t.Errorf("name=%q", calls[0].name)
	}
	if calls[0].rc != 0 {
		t.Errorf("rc=%d want 0", calls[0].rc)
	}
	if calls[0].duration <= 0 {
		t.Errorf("duration=%v want >0", calls[0].duration)
	}
}

func TestRun_MetricsRecorderRcPropagates(t *testing.T) {
	clearEnv(t)
	rec := &recordingMetrics{}
	buf := &bytes.Buffer{}
	obs, err := Init(Options{App: "x", Sink: buf, Format: "json", Metrics: rec})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer obs.Close()

	_ = Run("fail-sub", func() error {
		return NewExitError(13, "nope")
	})
	calls := rec.snapshot()
	if calls[0].rc != 13 {
		t.Errorf("rc=%d want 13", calls[0].rc)
	}
}

// =============================================================================
// Helpers
// =============================================================================

func TestExitCodeFrom(t *testing.T) {
	if got := ExitCodeFrom(nil); got != 0 {
		t.Errorf("nil err → %d want 0", got)
	}
	if got := ExitCodeFrom(errors.New("x")); got != 1 {
		t.Errorf("generic err → %d want 1", got)
	}
	if got := ExitCodeFrom(NewExitError(5, "")); got != 5 {
		t.Errorf("ExitError → %d want 5", got)
	}
	ec := &exitCoderErr{code: 9}
	if got := ExitCodeFrom(ec); got != 9 {
		t.Errorf("ExitCoder → %d want 9", got)
	}
}

func TestNewExitError_ErrorString(t *testing.T) {
	e := NewExitError(7, "boom")
	if got := e.Error(); got != "boom" {
		t.Errorf("Error()=%q want boom", got)
	}
	if e.ExitCode() != 7 {
		t.Errorf("ExitCode()=%d want 7", e.ExitCode())
	}
	e2 := NewExitError(7, "")
	if got := e2.Error(); !strings.Contains(got, "exit 7") {
		t.Errorf("Error()=%q want contains exit 7", got)
	}
}

func TestConfigError_ErrorString(t *testing.T) {
	e := &ConfigError{Field: "HELIX_LOG_FORMAT", Value: "yaml", Msg: "bad"}
	got := e.Error()
	if !strings.Contains(got, "HELIX_LOG_FORMAT") || !strings.Contains(got, "yaml") {
		t.Errorf("Error()=%q missing expected fields", got)
	}
}

// =============================================================================
// Sink ownership — ownsSink flag plumbing
// =============================================================================

func TestResolveSink_Precedence(t *testing.T) {
	clearEnv(t)
	dir := t.TempDir()
	logPath := filepath.Join(dir, "sink.log")
	t.Setenv(EnvLogFile, logPath)

	supplied := &bytes.Buffer{}
	w, owned, err := resolveSink(supplied, 0)
	if err != nil {
		t.Fatalf("resolveSink: %v", err)
	}
	if owned != true {
		t.Error("expected ownsSink=true when HELIX_LOG_FILE is set")
	}
	if w == supplied {
		t.Error("expected file-backed writer, not the supplied buffer")
	}

	// Without HELIX_LOG_FILE, supplied wins.
	clearEnv(t)
	w, owned, err = resolveSink(supplied, 0)
	if err != nil {
		t.Fatalf("resolveSink 2: %v", err)
	}
	if owned != false {
		t.Error("expected ownsSink=false when sink is supplied")
	}
	if w != supplied {
		t.Error("expected supplied writer to win when no env override")
	}

	// Without any, default is stderr, not owned.
	clearEnv(t)
	w, owned, err = resolveSink(nil, 0)
	if err != nil {
		t.Fatalf("resolveSink 3: %v", err)
	}
	if owned != false {
		t.Error("expected ownsSink=false for stderr default")
	}
	if w != os.Stderr {
		t.Error("expected os.Stderr fallback")
	}
}

// =============================================================================
// Smoke — verify emitted text format is stable across Run calls
// =============================================================================

func TestRun_TextFormatRender(t *testing.T) {
	clearEnv(t)
	buf := &bytes.Buffer{}
	_, err := Init(Options{App: "smoke", Sink: buf}) // text by default
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	_ = Run("cmd", func() error { return nil })
	out := buf.String()
	for _, want := range []string{"app=smoke", "msg=subcommand_complete", "subcommand=cmd"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output, got %q", want, out)
		}
	}
}

// Sink interface check — verify io.Closer works through our ownership flag.
type closingSink struct {
	*bytes.Buffer
	closed bool
}

func (c *closingSink) Close() error {
	c.closed = true
	return nil
}

// closingSink satisfies io.Writer (via embedded *bytes.Buffer) AND
// io.Closer (via its own Close method), so it can stand in for an owned
// file-backed sink.
var _ io.Writer = (*closingSink)(nil)
var _ io.Closer = (*closingSink)(nil)

func TestClose_OwnedFileSinkIsClosed(t *testing.T) {
	cs := &closingSink{Buffer: &bytes.Buffer{}}
	o := &Observer{
		ownsSink: true,
		sink:     cs,
		logger:   log.DefaultLogger(),
	}
	if err := o.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !cs.closed {
		t.Error("expected Close() to be called on owned sink")
	}
}
