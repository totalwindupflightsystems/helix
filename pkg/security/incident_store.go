// Package security — incident_store.go
//
// JSONL persistence layer for IncidentRecord. Mirrors the pattern used
// by pkg/forcemerge/audit.go (NewFileStore + appendJSON) but tailored
// to IncidentRecord. Each line is a single marshal'd record, enabling
// crash-safe append-only writes.
//
// Spec reference: specs/SPECIFICATION.md §6.7 (Incident Response) —
// operators need an audit trail of declared incidents, status
// transitions, and resolutions.

package security

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DefaultIncidentPath is the canonical location of the JSONL incident log.
// Mirrors forcemerge.DefaultAuditPath convention.
const DefaultIncidentPath = "~/.helix/incidents.jsonl"

// =============================================================================
// IncidentStore — JSONL append + read
// =============================================================================

// IncidentStore owns a JSONL file of IncidentRecord entries.
//
// The store is safe for concurrent use. Each Record append holds the
// mutex; reads copy the file independently.
type IncidentStore struct {
	mu     sync.Mutex
	path   string
	w      io.WriteCloser // nil for writer-only stores
	closer io.Closer
}

// NewIncidentFileStore opens (or creates) a JSONL file at the given
// path with mode 0o600 and returns a store that appends to it.
//
// "~/" prefixes are expanded via os.UserHomeDir. Parent directories
// are created with mode 0o755.
func NewIncidentFileStore(path string) (*IncidentStore, error) {
	expanded, err := expandIncidentPath(path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(expanded), 0o755); err != nil {
		return nil, fmt.Errorf("security: mkdir %s: %w", filepath.Dir(expanded), err)
	}
	f, err := os.OpenFile(expanded, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("security: open %s: %w", expanded, err)
	}
	return &IncidentStore{
		path:   expanded,
		w:      f,
		closer: f,
	}, nil
}

// NewIncidentWriterStore wraps an arbitrary io.Writer. Used by tests
// and the in-memory CLI mode.
//
// The returned store uses the writer directly (no mutex serialisation
// — callers are responsible for thread-safety on the underlying writer,
// e.g. by wrapping in a *sync.Mutex-protected writer if needed).
func NewIncidentWriterStore(w io.Writer) *IncidentStore {
	return &IncidentStore{w: writerOnlyWriter{w: w}}
}

// writerOnlyWriter is the sentinel wrapper for writer-only stores.
// Its presence (non-nil) signals to append() that no file mutex is
// needed. The underlying io.Writer handles writes directly.
type writerOnlyWriter struct {
	w io.Writer
}

func (wow writerOnlyWriter) Write(p []byte) (int, error) {
	return wow.w.Write(p)
}
func (wow writerOnlyWriter) Close() error {
	if c, ok := wow.w.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

// Path returns the underlying file path, or "" for a writer-only store.
func (s *IncidentStore) Path() string {
	return s.path
}

// Close flushes and closes the underlying file. No-op for writer-only stores
// whose writer doesn't implement io.Closer. Idempotent — safe to call
// multiple times.
func (s *IncidentStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closer != nil {
		err := s.closer.Close()
		s.closer = nil
		s.w = nil
		return err
	}
	if s.w != nil {
		if c, ok := s.w.(io.Closer); ok {
			err := c.Close()
			s.w = nil
			return err
		}
		s.w = nil
	}
	return nil
}

// Record appends inc as one JSONL line. Validates required fields first.
func (s *IncidentStore) Record(inc IncidentRecord) error {
	if err := validateIncidentRecord(inc); err != nil {
		return err
	}
	return s.append(inc)
}

// append marshals rec and writes it as a single JSONL line.
//
// For file-backed stores the mutex serialises writes (read-modify-write
// safety). For writer-only stores the underlying writer is responsible
// for its own concurrency control (e.g. *bytes.Buffer is not safe).
func (s *IncidentStore) append(rec IncidentRecord) error {
	data, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("security: marshal incident: %w", err)
	}
	data = append(data, '\n')

	// For file-backed stores, take the mutex so concurrent Record calls
	// don't interleave bytes. Writer-only stores skip the mutex.
	if s.isFileBacked() {
		s.mu.Lock()
		defer s.mu.Unlock()
	}
	if s.w == nil {
		return fmt.Errorf("security: store has no writer")
	}
	if _, err := s.w.Write(data); err != nil {
		return fmt.Errorf("security: write incident: %w", err)
	}
	return nil
}

// isFileBacked reports whether this store owns a real file (and thus
// needs the per-store mutex to serialise appends).
func (s *IncidentStore) isFileBacked() bool {
	return s.path != ""
}

// LoadAll reads every record from the underlying file and returns them
// in JSONL order. Malformed lines are skipped with a warning written
// to stderr (or io.Discard if stderr is nil).
func (s *IncidentStore) LoadAll() ([]*IncidentRecord, error) {
	if s.path == "" {
		return nil, fmt.Errorf("security: cannot LoadAll on writer-only store")
	}
	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // empty store is not an error
		}
		return nil, fmt.Errorf("security: open %s: %w", s.path, err)
	}
	defer f.Close()
	return readIncidentJSONL(f)
}

// =============================================================================
// Helpers
// =============================================================================

// readIncidentJSONL parses every JSON object line from r and returns
// the decoded records. Lines starting with '#' or empty lines are
// skipped silently (forward-compatible with header comments).
func readIncidentJSONL(r io.Reader) ([]*IncidentRecord, error) {
	var out []*IncidentRecord
	scanner := bufio.NewScanner(r)
	// 4 MiB buffer covers even the largest IncidentRecord (StepsCompleted
	// can grow for complex SEV-0 procedures).
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var rec IncidentRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			// Skip malformed lines — the JSONL is append-only and a
			// partially-written line is recoverable on the next Record.
			continue
		}
		out = append(out, &rec)
	}
	if err := scanner.Err(); err != nil {
		return out, fmt.Errorf("security: read incident JSONL: %w", err)
	}
	return out, nil
}

// validateIncidentRecord enforces the minimum invariants the CLI and
// other callers depend on. Returned errors are safe to surface to operators.
func validateIncidentRecord(r IncidentRecord) error {
	if strings.TrimSpace(r.ID) == "" {
		return fmt.Errorf("security: incident record requires ID")
	}
	if r.Severity != SeveritySEV0 && r.Severity != SeveritySEV1 &&
		r.Severity != SeveritySEV2 && r.Severity != SeveritySEV3 {
		return fmt.Errorf("security: incident %q has invalid severity %q", r.ID, r.Severity)
	}
	if r.Status == "" {
		return fmt.Errorf("security: incident %q has empty status", r.ID)
	}
	if r.CreatedAt.IsZero() {
		return fmt.Errorf("security: incident %q has zero CreatedAt", r.ID)
	}
	return nil
}

// expandIncidentPath expands a leading "~/" to the user's home directory.
func expandIncidentPath(p string) (string, error) {
	if !strings.HasPrefix(p, "~") {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("security: cannot expand %q: %w", p, err)
	}
	if p == "~" {
		return home, nil
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:]), nil
	}
	return p, nil
}

// ReplayIncidents reads every record from the store and returns them
// as a slice of pointers, suitable for re-populating an
// IncidentResponseEngine via RegisterIncident. Returns (nil, nil)
// when the store is empty or the file does not exist.
func (s *IncidentStore) ReplayIncidents() ([]*IncidentRecord, error) {
	recs, err := s.LoadAll()
	if err != nil {
		return nil, err
	}
	return recs, nil
}

// FormatTimestamp returns the canonical CLI-friendly timestamp string.
// Used by callers that build their own display rows from raw records.
func FormatTimestamp(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.UTC().Format("2006-01-02T15:04:05Z")
}
