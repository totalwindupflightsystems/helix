package prompt

import (
	"encoding/json"
	"fmt"
	"os"
)

// AuditLogger writes prompt registry operations to an append-only JSONL file.
// Each call to Log appends one AuditEntry as a single line (spec §4, §19).
type AuditLogger struct {
	path string
	file *os.File
	enc  *json.Encoder
}

// AuditEntry is a single audit log record for a prompt registry operation.
// Fields are omitted from JSON when empty (omitempty).
type AuditEntry struct {
	Timestamp  string `json:"timestamp"`
	Operation  string `json:"operation"`
	Component  string `json:"component,omitempty"`
	Version    string `json:"version,omitempty"`
	Hash       string `json:"hash,omitempty"`
	Commit     string `json:"commit,omitempty"`
	Actor      string `json:"actor,omitempty"`
	Reason     string `json:"reason,omitempty"`
	FromStatus string `json:"from,omitempty"`
	ToStatus   string `json:"to,omitempty"`
}

// NewAuditLogger opens the audit log file in append mode, creating it if it
// does not exist. An empty path returns nil with no error (auditing disabled).
func NewAuditLogger(path string) (*AuditLogger, error) {
	if path == "" {
		return nil, nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("open audit log %q: %w", path, err)
	}
	enc := json.NewEncoder(f)
	return &AuditLogger{path: path, file: f, enc: enc}, nil
}

// Log appends an AuditEntry as one JSON line. If the logger is nil (auditing
// disabled), this is a no-op and returns nil.
func (l *AuditLogger) Log(e AuditEntry) error {
	if l == nil {
		return nil
	}
	if l.enc == nil {
		return fmt.Errorf("audit logger not initialized")
	}
	return l.enc.Encode(&e)
}

// Close closes the underlying file. A nil logger is a no-op.
func (l *AuditLogger) Close() error {
	if l == nil {
		return nil
	}
	if l.file == nil {
		return nil
	}
	return l.file.Close()
}
