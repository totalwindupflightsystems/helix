// Package log provides a dependency-free, structured logging facility for
// Helix CLIs and libraries.
//
// Design goals:
//
//   - Zero external dependencies (no zap/logrus/zerolog). The platform
//     binary stays small and verifiable.
//
//   - Two output formats: "json" for Splunk/Promtail/Loki ingestion, and
//     "text" for human-readable terminal output. Format is chosen via the
//     `format` constructor argument or the WithFormat builder.
//
//   - Four well-known levels (Debug, Info, Warn, Error) with numeric ranks
//     so callers can compare without string matching.
//
//   - Structured fields. Every Emit carries a free-form map[string]any
//     plus a handful of well-known fields (ts, level, msg, app) that are
//     rendered first in every format.
//
// Typical usage:
//
//	hl := log.New(os.Stderr, "text", log.LevelInfo)
//	hl.Emit(log.LevelInfo, "subcommand_complete", map[string]any{
//	    "subcommand":   "adversarial",
//	    "rc":           0,
//	    "duration_ms":  42,
//	})
//
// Thread-safety: Logger is safe for concurrent use; the underlying writer
// is wrapped with a sync.Mutex so multi-line JSON entries are not
// interleaved.
package log

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// Level is the severity of a log entry. Lower numeric rank = lower
// severity. Comparison: Debug < Info < Warn < Error.
//
// Entries with `level < min` are dropped by Emit. So a logger with
// min=Info drops Debug entries but keeps everything else.
type Level int

// Levels in increasing severity. Debug has rank 0 (lowest).
const (
	LevelDebug Level = 0
	LevelInfo  Level = 1
	LevelWarn  Level = 2
	LevelError Level = 3
)

// ParseLevel converts a human label ("debug", "INFO", "warn") into a
// Level. Empty input returns LevelInfo. Unknown input returns an error.
func ParseLevel(s string) (Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "info":
		return LevelInfo, nil
	case "warn", "warning":
		return LevelWarn, nil
	case "error", "err":
		return LevelError, nil
	case "debug", "trace":
		return LevelDebug, nil
	}
	return LevelInfo, fmt.Errorf("log: unknown level %q", s)
}

// String returns the canonical lowercase label for the level.
func (l Level) String() string {
	switch l {
	case LevelInfo:
		return "info"
	case LevelWarn:
		return "warn"
	case LevelError:
		return "error"
	case LevelDebug:
		return "debug"
	}
	return fmt.Sprintf("level(%d)", int(l))
}

// =============================================================================
// Logger
// =============================================================================

// Logger writes structured entries to an underlying writer. The zero
// value is NOT usable; call New. Copying a Logger by value shares the
// underlying mutex and writer — always pass by pointer.
type Logger struct {
	w      io.Writer
	format string // "json" | "text"
	min    Level
	app    string // optional baseline field; set via WithApp.

	mu sync.Mutex // serialises Write calls so multi-line text entries
	// don't interleave when multiple goroutines Emit simultaneously.
}

// New constructs a Logger that writes to w. format must be "json" or
// "text" (case-insensitive); other values return an error. min sets the
// minimum severity: entries below this level are silently dropped.
//
// An empty format defaults to "text"; an unknown format is rejected so
// misconfiguration surfaces immediately rather than at log time.
func New(w io.Writer, format string, min Level) (*Logger, error) {
	if w == nil {
		w = os.Stderr
	}
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "text":
		format = "text"
	case "json":
		format = "json"
	default:
		return nil, fmt.Errorf("log: invalid format %q (expected: text | json)", format)
	}
	return &Logger{w: w, format: format, min: min}, nil
}

// WithApp returns a copy of l with the "app" baseline field set to app.
// Each Emit() rendered by the copy includes app as the first field
// after the timestamp and level. Pass "" to clear it. The original
// Logger is not modified.
//
// Implementation note: we do NOT copy l directly because *Logger embeds
// a sync.Mutex; copying would violate copylocks. Instead we create a
// fresh Logger and copy only the scalar fields explicitly.
func (l *Logger) WithApp(app string) *Logger {
	cp := &Logger{
		w:      l.w,
		format: l.format,
		min:    l.min,
		app:    strings.TrimSpace(app),
	}
	return cp
}

// SetMinLevel replaces the minimum severity threshold. Existing fields
// and writer are preserved; only the filter level changes.
func (l *Logger) SetMinLevel(min Level) {
	l.mu.Lock()
	l.min = min
	l.mu.Unlock()
}

// =============================================================================
// Emit
// =============================================================================

// Emit writes a single structured entry with the supplied message and
// fields. Entries below the minimum severity are silently dropped.
//
// `msg` is required (callers should treat it as a constant template
// like "subcommand_complete"). `fields` may be nil; reserved keys
// (ts, level, msg, app) are written first to keep the structure
// uniform across formats.
//
// Field types: strings, numbers, bools, and time.Time are rendered
// natively in JSON; everything else is rendered via fmt.Sprintf("%v").
// Maps and slices are NOT supported — flatten them before calling.
//
// Emit is safe for concurrent use; the underlying writer is locked
// while the entry is rendered.
func (l *Logger) Emit(level Level, msg string, fields map[string]any) {
	if l == nil {
		return
	}
	if level < l.min {
		return
	}
	if strings.TrimSpace(msg) == "" {
		msg = "(no message)"
	}

	// Copy the caller-supplied map so concurrent mutation is safe.
	cp := make(map[string]any, len(fields)+4)
	for k, v := range fields {
		cp[k] = v
	}
	cp["ts"] = time.Now().UTC().Format(time.RFC3339Nano)
	cp["level"] = level.String()
	cp["msg"] = msg
	if l.app != "" {
		cp["app"] = l.app
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	var line []byte
	switch l.format {
	case "json":
		line = renderJSON(cp)
	default:
		line = renderText(cp)
	}
	if _, err := l.w.Write(line); err != nil {
		// We can't log a logger error without creating an infinite
		// loop. Swallow; the next Emit call may also fail and the
		// health monitor (if configured) will catch it.
		_ = err
	}
}

// =============================================================================
// Renderers
// =============================================================================

// renderJSON encodes m as a single-line JSON document terminated with a
// newline. Keys are sorted to make output deterministic for test
// assertions and log-comparison tooling.
func renderJSON(m map[string]any) []byte {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		kb, err := json.Marshal(k)
		if err != nil {
			// Fall back to the unsafe string-equivalent; this
			// shouldn't happen for ASCII keys.
			kb = []byte(fmt.Sprintf("%q", k))
		}
		b.Write(kb)
		b.WriteByte(':')
		b.Write(renderJSONValue(m[k]))
	}
	b.WriteByte('}')
	b.WriteByte('\n')
	return []byte(b.String())
}

// renderJSONValue renders a single value for use inside a JSON object.
// time.Time is rendered as an RFC3339Nano string (NOT a MarshalJSON
// object); everything else delegates to json.Marshal.
func renderJSONValue(v any) []byte {
	switch val := v.(type) {
	case nil:
		return []byte("null")
	case string:
		b, err := json.Marshal(val)
		if err != nil {
			return []byte("null")
		}
		return b
	case bool:
		if val {
			return []byte("true")
		}
		return []byte("false")
	case int:
		return []byte(fmt.Sprintf("%d", val))
	case int8:
		return []byte(fmt.Sprintf("%d", val))
	case int16:
		return []byte(fmt.Sprintf("%d", val))
	case int32:
		return []byte(fmt.Sprintf("%d", val))
	case int64:
		return []byte(fmt.Sprintf("%d", val))
	case uint:
		return []byte(fmt.Sprintf("%d", val))
	case uint8:
		return []byte(fmt.Sprintf("%d", val))
	case uint16:
		return []byte(fmt.Sprintf("%d", val))
	case uint32:
		return []byte(fmt.Sprintf("%d", val))
	case uint64:
		return []byte(fmt.Sprintf("%d", val))
	case float32:
		return []byte(fmt.Sprintf("%g", val))
	case float64:
		return []byte(fmt.Sprintf("%g", val))
	case time.Time:
		b, err := json.Marshal(val.UTC().Format(time.RFC3339Nano))
		if err != nil {
			return []byte("null")
		}
		return b
	case []byte:
		// Inline base64 — keeps the JSON single-line.
		b, err := json.Marshal(val)
		if err != nil {
			return []byte("null")
		}
		return b
	}
	// Fall through to json.Marshal for everything else (e.g. structs).
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("null")
	}
	return b
}

// renderText produces a one-line, key=value rendering suitable for
// human terminals. Keys are sorted for deterministic output. Reserved
// keys are written first in a fixed order (ts level msg [app]) and
// prefixed by their canonical short label; user-supplied keys retain
// their full name.
func renderText(m map[string]any) []byte {
	var b strings.Builder

	// Reserved ordering.
	reserved := []string{"ts", "level", "msg", "app"}
	for _, k := range reserved {
		if v, ok := m[k]; ok {
			fmt.Fprintf(&b, "%s=%s ", shortKey(k), renderTextValue(v))
			delete(m, k)
		}
	}

	// Remaining keys in sorted order.
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "%s=%s ", k, renderTextValue(m[k]))
	}
	b.WriteByte('\n')
	return []byte(b.String())
}

// shortKey maps the reserved keys to the conventional short labels used
// in `text` mode (matches industry-standard logfmt-like conventions).
func shortKey(k string) string {
	switch k {
	case "ts":
		return "ts"
	case "level":
		return "lvl"
	case "msg":
		return "msg"
	case "app":
		return "app"
	}
	return k
}

// renderTextValue converts a value to its text-mode printable form.
// Strings are quoted; numbers/bools/times use their Go default %v form.
// Anything else is rendered via fmt.Sprintf and quoted.
func renderTextValue(v any) string {
	switch val := v.(type) {
	case nil:
		return "null"
	case string:
		// Quote with double-quotes; escape embedded quotes / newlines.
		// Single-line values are not quoted for readability.
		if !strings.ContainsAny(val, " \t\"\n\\") {
			return val
		}
		b, err := json.Marshal(val)
		if err != nil {
			return fmt.Sprintf("%q", val)
		}
		return string(b)
	case time.Time:
		return val.UTC().Format(time.RFC3339Nano)
	case []byte:
		if !strings.ContainsAny(string(val), " \t\"\n\\") {
			return string(val)
		}
		return fmt.Sprintf("%q", val)
	}
	return fmt.Sprintf("%v", v)
}

// =============================================================================
// Helpers
// =============================================================================

// DefaultLogger returns a process-wide logger that writes to stderr in
// text mode at LevelInfo. Convenience for callers that don't want to
// construct their own. The returned *Logger is fresh on every call so
// callers can take ownership of it (e.g. set app, change level) without
// affecting other callers.
//
// Because every call returns a new instance, the DefaultLogger pattern
// should be used for the very first log line of a process — once an
// app starts configuring logging, build one Logger and pass it around.
func DefaultLogger() *Logger {
	l, _ := New(os.Stderr, "text", LevelInfo)
	return l
}
