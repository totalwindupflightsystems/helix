package prompt

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewAuditLogger(t *testing.T) {
	t.Run("valid_path", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "audit.jsonl")
		logger, err := NewAuditLogger(path)
		if err != nil {
			t.Fatalf("NewAuditLogger(%q) error = %v", path, err)
		}
		if logger == nil {
			t.Fatal("NewAuditLogger returned nil logger")
		}
		defer logger.Close()

		if logger.path != path {
			t.Errorf("path = %q, want %q", logger.path, path)
		}
		if logger.file == nil {
			t.Error("file is nil")
		}
		if logger.enc == nil {
			t.Error("enc is nil")
		}
	})

	t.Run("empty_path_returns_nil", func(t *testing.T) {
		logger, err := NewAuditLogger("")
		if err != nil {
			t.Fatalf("NewAuditLogger(\"\") error = %v", err)
		}
		if logger != nil {
			t.Error("NewAuditLogger with empty path should return nil logger")
		}
	})

	t.Run("unwritable_path", func(t *testing.T) {
		path := "/root/audit.jsonl"
		_, err := NewAuditLogger(path)
		if err == nil {
			t.Fatal("NewAuditLogger for unwritable path should return error")
		}
	})

	t.Run("creates_file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "audit.jsonl")
		logger, err := NewAuditLogger(path)
		if err != nil {
			t.Fatalf("NewAuditLogger error: %v", err)
		}
		defer logger.Close()

		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("file %q was not created", path)
		}
	})
}

func TestAuditLogger_Log(t *testing.T) {
	t.Run("writes_json_line", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "audit.jsonl")
		logger, err := NewAuditLogger(path)
		if err != nil {
			t.Fatalf("NewAuditLogger error: %v", err)
		}
		defer logger.Close()

		entry := AuditEntry{
			Timestamp: "2026-06-24T12:00:00Z",
			Operation: "register",
			Component: "agent-identity",
			Version:   "v1.0.0",
			Hash:      "sha256:abc123",
		}
		if err := logger.Log(entry); err != nil {
			t.Fatalf("Log error: %v", err)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile error: %v", err)
		}

		var decoded AuditEntry
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal error: %v\nraw: %s", err, string(data))
		}
		if decoded.Operation != "register" {
			t.Errorf("Operation = %q, want register", decoded.Operation)
		}
		if decoded.Component != "agent-identity" {
			t.Errorf("Component = %q, want agent-identity", decoded.Component)
		}
		if decoded.Version != "v1.0.0" {
			t.Errorf("Version = %q, want v1.0.0", decoded.Version)
		}
		if decoded.Hash != "sha256:abc123" {
			t.Errorf("Hash = %q, want sha256:abc123", decoded.Hash)
		}
	})

	t.Run("nil_logger_noop", func(t *testing.T) {
		var logger *AuditLogger = nil
		entry := AuditEntry{Operation: "register"}
		if err := logger.Log(entry); err != nil {
			t.Errorf("nil logger.Log should not error, got %v", err)
		}
	})

	t.Run("multiple_entries_append", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "audit.jsonl")
		logger, err := NewAuditLogger(path)
		if err != nil {
			t.Fatalf("NewAuditLogger error: %v", err)
		}
		defer logger.Close()

		for i := 0; i < 3; i++ {
			entry := AuditEntry{
				Timestamp: "2026-06-24T12:00:00Z",
				Operation: "register",
				Component: "test",
				Version:   "v1.0.0",
			}
			if err := logger.Log(entry); err != nil {
				t.Fatalf("Log %d error: %v", i, err)
			}
		}

		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile error: %v", err)
		}
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		if len(lines) != 3 {
			t.Errorf("expected 3 lines, got %d\nraw: %s", len(lines), string(data))
		}
		for i, line := range lines {
			var entry AuditEntry
			if err := json.Unmarshal([]byte(line), &entry); err != nil {
				t.Errorf("line %d unmarshal error: %v", i, err)
			}
		}
	})

	t.Run("reopen_appends", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "audit.jsonl")

		// First session
		log1, err := NewAuditLogger(path)
		if err != nil {
			t.Fatalf("NewAuditLogger error: %v", err)
		}
		if err := log1.Log(AuditEntry{Operation: "first"}); err != nil {
			t.Fatalf("Log error: %v", err)
		}
		log1.Close()

		// Second session — should append, not overwrite
		log2, err := NewAuditLogger(path)
		if err != nil {
			t.Fatalf("second NewAuditLogger error: %v", err)
		}
		defer log2.Close()
		if err := log2.Log(AuditEntry{Operation: "second"}); err != nil {
			t.Fatalf("second Log error: %v", err)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile error: %v", err)
		}
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		if len(lines) != 2 {
			t.Errorf("expected 2 lines, got %d\nraw: %s", len(lines), string(data))
		}
	})
}

func TestAuditLogger_Close(t *testing.T) {
	t.Run("valid_logger", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "audit.jsonl")
		logger, err := NewAuditLogger(path)
		if err != nil {
			t.Fatalf("NewAuditLogger error: %v", err)
		}
		if err := logger.Close(); err != nil {
			t.Errorf("Close error: %v", err)
		}
		// Verify file is flushed — reopen and check it's valid
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile after close error: %v", err)
		}
		if len(data) == 0 {
			t.Log("file empty after close (no entries logged — expected)")
		}
	})

	t.Run("nil_logger", func(t *testing.T) {
		var logger *AuditLogger = nil
		if err := logger.Close(); err != nil {
			t.Errorf("nil logger.Close should not error, got %v", err)
		}
	})
}

func TestAuditEntry_JSON(t *testing.T) {
	t.Run("full_entry", func(t *testing.T) {
		entry := AuditEntry{
			Timestamp:  "2026-06-24T12:00:00Z",
			Operation:  "transition",
			Component:  "agent-identity",
			Version:    "v1.0.0",
			Hash:       "sha256:abc123",
			Commit:     "abc123def",
			Actor:      "wojons",
			Reason:     "second_commit",
			FromStatus: "attested",
			ToStatus:   "active",
		}
		data, err := json.Marshal(entry)
		if err != nil {
			t.Fatalf("marshal error: %v", err)
		}
		var decoded map[string]interface{}
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}
		checks := map[string]string{
			"timestamp":  "2026-06-24T12:00:00Z",
			"operation":  "transition",
			"component":  "agent-identity",
			"version":    "v1.0.0",
			"hash":       "sha256:abc123",
			"commit":     "abc123def",
			"actor":      "wojons",
			"reason":     "second_commit",
			"from":       "attested",
			"to":         "active",
		}
		for key, want := range checks {
			got, ok := decoded[key]
			if !ok {
				t.Errorf("missing key %q in JSON: %s", key, string(data))
				continue
			}
			if got != want {
				t.Errorf("%s = %q, want %q", key, got, want)
			}
		}
	})

	t.Run("minimal_entry", func(t *testing.T) {
		entry := AuditEntry{
			Timestamp: "2026-06-24T12:00:00Z",
			Operation: "register",
		}
		data, err := json.Marshal(entry)
		if err != nil {
			t.Fatalf("marshal error: %v", err)
		}
		var decoded map[string]interface{}
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}
		// omitempty should exclude empty fields
		if _, ok := decoded["component"]; ok {
			t.Errorf("empty component should be omitted, got %s", string(data))
		}
		if _, ok := decoded["version"]; ok {
			t.Errorf("empty version should be omitted")
		}
	})

	t.Run("all_operations", func(t *testing.T) {
		operations := []string{"register", "attest", "override", "transition"}
		for _, op := range operations {
			entry := AuditEntry{
				Timestamp: "2026-06-24T12:00:00Z",
				Operation: op,
			}
			data, err := json.Marshal(entry)
			if err != nil {
				t.Fatalf("marshal %q error: %v", op, err)
			}
			if !strings.Contains(string(data), `"`+op+`"`) {
				t.Errorf("expected operation %q in JSON: %s", op, string(data))
			}
		}
	})
}
