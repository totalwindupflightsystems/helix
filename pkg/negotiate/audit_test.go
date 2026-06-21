package negotiate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewAuditLogger(t *testing.T) {
	t.Run("valid_path", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "audit.jsonl")

		logger, err := NewAuditLogger(path)
		if err != nil {
			t.Fatalf("NewAuditLogger(%q) error = %v", path, err)
		}
		if logger == nil {
			t.Fatal("NewAuditLogger returned nil logger")
		}
		if logger.path != path {
			t.Errorf("logger.path = %q, want %q", logger.path, path)
		}
		defer logger.Close()

		// Verify file was created
		if _, statErr := os.Stat(path); statErr != nil {
			t.Errorf("file not created: %v", statErr)
		}
	})

	t.Run("invalid_path", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "nonexistent", "subdir", "audit.jsonl")

		_, err := NewAuditLogger(path)
		if err == nil {
			t.Fatal("NewAuditLogger for invalid path should return error")
		}
	})

	t.Run("file_is_appendable", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "append.jsonl")

		// Create with one event
		log1, err := NewAuditLogger(path)
		if err != nil {
			t.Fatalf("NewAuditLogger error: %v", err)
		}
		err = log1.LogEvent(DebateEvent{
			Type:      "round_start",
			Agent:     "agent-a",
			Timestamp: time.Now(),
		})
		if err != nil {
			t.Fatalf("LogEvent error: %v", err)
		}
		log1.Close()

		// Re-open and append another event
		log2, err := NewAuditLogger(path)
		if err != nil {
			t.Fatalf("second NewAuditLogger error: %v", err)
		}
		err = log2.LogEvent(DebateEvent{
			Type:      "round_end",
			Agent:     "agent-b",
			Timestamp: time.Now(),
		})
		if err != nil {
			t.Fatalf("second LogEvent error: %v", err)
		}
		log2.Close()

		// Read back — should have 2 lines
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile error: %v", err)
		}
		lines := 0
		for _, b := range data {
			if b == '\n' {
				lines++
			}
		}
		if lines != 2 {
			t.Errorf("got %d lines in audit file, want 2", lines)
		}
	})
}

func TestAuditLogger_LogEvent(t *testing.T) {
	t.Run("valid_event", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "events.jsonl")
		logger, err := NewAuditLogger(path)
		if err != nil {
			t.Fatalf("NewAuditLogger error: %v", err)
		}
		defer logger.Close()

		event := DebateEvent{
			Round:         1,
			Type:          "agent_argument",
			Agent:         "agent-a",
			Body:          "This PR needs evidence.",
			EvidenceCount: 3,
			Timestamp:     time.Now(),
		}

		if err := logger.LogEvent(event); err != nil {
			t.Fatalf("LogEvent error: %v", err)
		}

		// Verify the written content is valid JSON
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile error: %v", err)
		}

		var decoded DebateEvent
		if err := json.Unmarshal(data[:len(data)-1], &decoded); err != nil { // strip trailing \n
			t.Fatalf("JSON decode error: %v", err)
		}
		if decoded.Type != event.Type {
			t.Errorf("decoded.Type = %q, want %q", decoded.Type, event.Type)
		}
		if decoded.Agent != event.Agent {
			t.Errorf("decoded.Agent = %q, want %q", decoded.Agent, event.Agent)
		}
	})

	t.Run("nil_encoder", func(t *testing.T) {
		// Construct a logger with nil encoder (bypass NewAuditLogger)
		logger := &AuditLogger{path: "/dev/null"}
		err := logger.LogEvent(DebateEvent{Type: "test"})
		if err == nil {
			t.Fatal("LogEvent with nil encoder should return error")
		}
	})
}

func TestAuditLogger_Close(t *testing.T) {
	t.Run("close_open_file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "close.jsonl")
		logger, err := NewAuditLogger(path)
		if err != nil {
			t.Fatalf("NewAuditLogger error: %v", err)
		}

		if err := logger.Close(); err != nil {
			t.Errorf("Close error: %v", err)
		}

		// Second close on already-closed file — os.File.Close returns an error
		// for double-close, which is expected behavior.
		_ = logger.Close()
	})

	t.Run("close_nil_file", func(t *testing.T) {
		logger := &AuditLogger{}
		if err := logger.Close(); err != nil {
			t.Errorf("Close with nil file should not error, got: %v", err)
		}
	})
}

func TestAuditLogger_NewAndClose_EmptyPath(t *testing.T) {
	// Empty path should fail (os.OpenFile on empty string)
	_, err := NewAuditLogger("")
	if err == nil {
		t.Fatal("NewAuditLogger with empty path should return error")
	}
}
