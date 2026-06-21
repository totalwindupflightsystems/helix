package negotiate

import (
	"encoding/json"
	"fmt"
	"os"
)

// AuditLogger writes debate events to a JSONL file (spec §13).
// Each call to LogEvent appends one JSON object as a single line.
type AuditLogger struct {
	path string
	file *os.File
	enc  *json.Encoder
}

// NewAuditLogger creates a logger at the given file path. The file is opened in
// append mode so multiple negotiations can share the same transcript file.
func NewAuditLogger(path string) (*AuditLogger, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("open audit log %q: %w", path, err)
	}
	enc := json.NewEncoder(f)
	return &AuditLogger{path: path, file: f, enc: enc}, nil
}

// LogEvent appends a DebateEvent as one JSON line.
func (l *AuditLogger) LogEvent(e DebateEvent) error {
	if l.enc == nil {
		return fmt.Errorf("audit logger not initialized")
	}
	return l.enc.Encode(&e)
}

// Close closes the underlying file.
func (l *AuditLogger) Close() error {
	if l.file == nil {
		return nil
	}
	return l.file.Close()
}
