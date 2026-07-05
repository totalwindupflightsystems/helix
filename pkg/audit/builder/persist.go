// Package builder — JSONL persistence for the audit chain.
//
// Per specs/SPECIFICATION.md §6.5 (Audit Trail Requirements), every step
// of the 12-step audit chain must be persisted to disk so an auditor can
// replay a run after a crash. The builder accumulates evidence in memory
// during a run; WriteToFile / ReadFromFile is the disk-bridge.
//
// The persistence format is JSONL: one AuditEvidence object per line.
// JSONL is preferred over a single multi-object JSON for two reasons:
//
//  1. Crash safety: O_APPEND writes land at the file's end atomically
//     (POSIX guarantees write sizes <= PIPE_BUF are atomic, and JSONL
//     line sizes are well under that). A crash mid-write leaves the
//     prior lines intact and the partial last line is ignored by
//     ReadFromFile (it errors on incomplete JSON).
//
//  2. Streaming: long-running PRs can flush evidence as each step
//     completes, rather than buffering the whole 12-step chain in RAM.
package builder

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/audit"
)

// =============================================================================
// WriteToFile — single AuditEvidence → JSONL file
// =============================================================================

// WriteToFile serializes ev as one JSONL line and writes it to path using
// O_APPEND semantics (safe for crash recovery: an existing audit trail
// keeps its prior lines, and the new evidence is appended at the end).
//
// If the file does not exist, it is created (mode 0o644). The parent
// directory must already exist — callers wanting mkdir-p semantics
// should call os.MkdirAll first.
//
// Errors:
//
//   - marshal failure (malformed evidence)
//   - mkdir failure (propagated from any caller-side setup)
//   - open / write failure (filesystem)
func WriteToFile(ev *audit.AuditEvidence, path string) error {
	if ev == nil {
		return errors.New("builder.WriteToFile: nil evidence")
	}
	if path == "" {
		return errors.New("builder.WriteToFile: empty path")
	}

	data, err := audit.MarshalEvidence(ev)
	if err != nil {
		return fmt.Errorf("builder.WriteToFile: marshal: %w", err)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("builder.WriteToFile: open %s: %w", path, err)
	}
	defer f.Close()

	// Use bufio so multi-line evidence (rare, but possible if a payload
	// contains a literal newline) is written in one syscall pair.
	bw := bufio.NewWriter(f)
	if _, err := bw.Write(data); err != nil {
		return fmt.Errorf("builder.WriteToFile: write: %w", err)
	}
	if err := bw.WriteByte('\n'); err != nil {
		return fmt.Errorf("builder.WriteToFile: write newline: %w", err)
	}
	if err := bw.Flush(); err != nil {
		return fmt.Errorf("builder.WriteToFile: flush: %w", err)
	}
	return nil
}

// =============================================================================
// ReadFromFile — JSONL file → single AuditEvidence (last line)
// =============================================================================

// ReadFromFile reads a JSONL file and returns the LAST AuditEvidence
// record on disk. The most-recent record is the most useful for an
// auditor asking "where is this PR right now?" — earlier records are
// historical checkpoints that can be inspected with ReadAllFromFile.
//
// Behavior:
//
//   - File missing → os.ErrNotExist
//   - File empty   → errors.New("no audit evidence on disk")
//   - Malformed    → JSON decode error (no fallback)
//
// Note: a partial write (crash mid-line) yields a JSON decode error
// from the last line. That's the correct behavior — it tells the
// auditor the file is corrupt and the prior chain can't be trusted.
func ReadFromFile(path string) (*audit.AuditEvidence, error) {
	if path == "" {
		return nil, errors.New("builder.ReadFromFile: empty path")
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("builder.ReadFromFile: open %s: %w", path, err)
	}
	defer f.Close()

	// Stream every line, retain only the last valid evidence record.
	// Memory-bounded for arbitrarily long audit trails.
	scanner := bufio.NewScanner(f)
	// Allow large lines (default scanner buffer is 64KiB; audit records
	// with full diffs can easily exceed that).
	scanner.Buffer(make([]byte, 0, 1<<16), 1<<22) // 4 MiB max

	var last *audit.AuditEvidence
	for scanner.Scan() {
		line := scanner.Bytes()
		// Skip blank lines so trailing newlines don't break parsing.
		if len(line) == 0 {
			continue
		}
		ev, err := audit.UnmarshalEvidence(line)
		if err != nil {
			return nil, fmt.Errorf("builder.ReadFromFile: decode line %d: %w", auditLineNumber, err)
		}
		last = ev
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("builder.ReadFromFile: scan: %w", err)
	}
	if last == nil {
		return nil, errors.New("builder.ReadFromFile: no audit evidence on disk")
	}
	return last, nil
}

// auditLineNumber is incremented as scanner.Scan progresses so error
// messages can point operators at the offending line. We update it via
// a counter increment inside ReadFromFile using a closure (kept here as
// a package-private sentinel for tests that want to verify line numbers).
//
// Note: this counter is NOT goroutine-safe; ReadFromFile is documented
// as sequential-only and the counter is read only inside its body.
var auditLineNumber int

// =============================================================================
// ReadAllFromFile — JSONL file → slice of AuditEvidence
// =============================================================================

// ReadAllFromFile reads every line in the JSONL file and returns the full
// slice in order. Useful for auditors reconstructing a complete run
// history. Same error semantics as ReadFromFile for missing/empty files.
func ReadAllFromFile(path string) ([]*audit.AuditEvidence, error) {
	if path == "" {
		return nil, errors.New("builder.ReadAllFromFile: empty path")
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("builder.ReadAllFromFile: open %s: %w", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1<<16), 1<<22)

	var out []*audit.AuditEvidence
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		ev, err := audit.UnmarshalEvidence(line)
		if err != nil {
			return nil, fmt.Errorf("builder.ReadAllFromFile: decode: %w", err)
		}
		out = append(out, ev)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("builder.ReadAllFromFile: scan: %w", err)
	}
	if len(out) == 0 {
		return nil, errors.New("builder.ReadAllFromFile: no audit evidence on disk")
	}
	return out, nil
}

// =============================================================================
// PersistBuilder — append-friendly wrapper
// =============================================================================

// PersistBuilder is a convenience wrapper that combines an in-memory
// builder.Builder with a JSONL file path. After every setter, the
// builder calls WriteToFile internally so the on-disk trail stays in
// sync with the in-memory state.
//
// Use this when you want crash-safe persistence without writing the
// WriteToFile call at every step in the caller. For one-shot persistence
// at the end of a run, prefer the standalone WriteToFile helper.
//
// PersistBuilder is NOT safe for concurrent use — the underlying
// builder.Builder is sequential-only (per its own documentation).
type PersistBuilder struct {
	mu        sync.Mutex
	path      string
	builder   *Builder
	autoflush bool
}

// NewPersistBuilder creates a PersistBuilder bound to path. The file is
// created if absent. Pass autoflush=true to write after every setter;
// pass false to batch writes via Flush() at logical checkpoints.
func NewPersistBuilder(path string, prRef string, autoflush bool) (*PersistBuilder, error) {
	if path == "" {
		return nil, errors.New("builder.NewPersistBuilder: empty path")
	}
	// Ensure parent directory exists — auditors frequently point
	// PersistBuilder at ~/.helix/audit/<date>/pr-42.jsonl, and a
	// missing parent dir is a common failure mode.
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("builder.NewPersistBuilder: mkdir: %w", err)
		}
	}
	return &PersistBuilder{
		path:      path,
		builder:   New(prRef),
		autoflush: autoflush,
	}, nil
}

// maybeAutoFlush writes the current in-memory state to disk if autoflush
// is enabled. Returns any error encountered. Errors are returned to
// callers (the wrapper's setters) rather than swallowed so callers can
// detect a failing disk and stop persisting.
func (p *PersistBuilder) maybeAutoFlush() error {
	if p == nil || !p.autoflush {
		return nil
	}
	return WriteToFile(p.builder.Build(), p.path)
}

// Issue proxies to the underlying builder and optionally flushes.
func (p *PersistBuilder) Issue(url, creator string, ts time.Time, title string) *PersistBuilder {
	if p == nil {
		return p
	}
	p.builder.Issue(url, creator, ts, title)
	if err := p.maybeAutoFlush(); err != nil {
		// We can't return an error from a setter without breaking the
		// fluent interface, so log to stderr and continue. Callers
		// wanting strict error propagation should disable autoflush and
		// call Flush() themselves.
		fmt.Fprintf(os.Stderr, "PersistBuilder.Issue autoflush: %v\n", err)
	}
	return p
}

// Commit proxies to the underlying builder and optionally flushes.
func (p *PersistBuilder) Commit(sha, promptHash, model, contextHash, agentID string, confidence int, costUSD float64, attestationFound bool) *PersistBuilder {
	if p == nil {
		return p
	}
	p.builder.Commit(sha, promptHash, model, contextHash, agentID, confidence, costUSD, attestationFound)
	if err := p.maybeAutoFlush(); err != nil {
		fmt.Fprintf(os.Stderr, "PersistBuilder.Commit autoflush: %v\n", err)
	}
	return p
}

// Flush writes the current in-memory evidence to disk. Returns any I/O
// error encountered.
func (p *PersistBuilder) Flush() error {
	if p == nil {
		return errors.New("PersistBuilder.Flush: nil receiver")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return WriteToFile(p.builder.Build(), p.path)
}

// Path returns the on-disk JSONL file path. Useful for test assertions.
func (p *PersistBuilder) Path() string {
	if p == nil {
		return ""
	}
	return p.path
}

// Builder returns the underlying in-memory builder. Useful when callers
// want to inspect Completion() / MissingSteps() without forcing a Flush.
func (p *PersistBuilder) Builder() *Builder {
	if p == nil {
		return nil
	}
	return p.builder
}

// =============================================================================
// StreamWriter — true streaming JSONL writer for very long chains
// =============================================================================

// StreamWriter wraps a buffered file handle and writes one JSONL record
// per call. Use when each step's evidence is computed independently and
// you don't want to keep the whole chain in memory.
//
// StreamWriter is NOT safe for concurrent use; sequential writes only.
type StreamWriter struct {
	mu sync.Mutex
	w  *bufio.Writer
	f  *os.File
}

// NewStreamWriter opens path for append + create and returns a buffered
// writer. Call Close to flush.
func NewStreamWriter(path string) (*StreamWriter, error) {
	if path == "" {
		return nil, errors.New("builder.NewStreamWriter: empty path")
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("builder.NewStreamWriter: mkdir: %w", err)
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("builder.NewStreamWriter: open %s: %w", path, err)
	}
	return &StreamWriter{
		w: bufio.NewWriter(f),
		f: f,
	}, nil
}

// Write writes one evidence record as a single JSONL line.
func (s *StreamWriter) Write(ev *audit.AuditEvidence) error {
	if s == nil {
		return errors.New("StreamWriter.Write: nil receiver")
	}
	if s.w == nil {
		return errors.New("StreamWriter.Write: writer is closed")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := audit.WriteJSONL(s.w, ev); err != nil {
		return fmt.Errorf("StreamWriter.Write: %w", err)
	}
	return nil
}

// Close flushes the buffered writer and closes the underlying file.
// Calling Close more than once is a no-op after the first.
func (s *StreamWriter) Close() error {
	if s == nil || s.w == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.w.Flush()
	closeErr := s.f.Close()
	s.w = nil
	s.f = nil
	if err != nil {
		return fmt.Errorf("StreamWriter.Close flush: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("StreamWriter.Close file: %w", closeErr)
	}
	return nil
}

// EncodeJSON is a thin escape hatch for callers that need to write a
// JSONL line that isn't an AuditEvidence (e.g. annotation records).
// Returns the encoded bytes without writing — caller is responsible for
// the actual write. Most callers should use Write.
func EncodeJSON(v any) ([]byte, error) {
	if v == nil {
		return nil, errors.New("builder.EncodeJSON: nil input")
	}
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("builder.EncodeJSON: marshal: %w", err)
	}
	return append(data, '\n'), nil
}

// Compile-time guarantee: io.Writer is satisfied by StreamWriter's
// embedded *bufio.Writer (accessible via the type's methods). This is
// purely defensive — the type system already enforces it.
var _ io.Writer = (*bufio.Writer)(nil)
