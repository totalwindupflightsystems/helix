package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseBackupFlags_Default(t *testing.T) {
	flags, rc := parseBackupFlags([]string{})
	if rc != backupExitOK {
		t.Fatalf("expected rc 0, got %d", rc)
	}
	if flags.subcommand != "status" {
		t.Errorf("expected subcommand status, got %s", flags.subcommand)
	}
}

func TestParseBackupFlags_Validate(t *testing.T) {
	flags, rc := parseBackupFlags([]string{"validate"})
	if rc != backupExitOK {
		t.Fatalf("expected rc 0, got %d", rc)
	}
	if flags.subcommand != "validate" {
		t.Errorf("expected subcommand validate, got %s", flags.subcommand)
	}
}

func TestParseBackupFlags_JSON(t *testing.T) {
	flags, rc := parseBackupFlags([]string{"--json"})
	if rc != backupExitOK {
		t.Fatalf("expected rc 0, got %d", rc)
	}
	if !flags.jsonOut {
		t.Error("expected jsonOut true")
	}
}

func TestParseBackupFlags_DryRun(t *testing.T) {
	flags, rc := parseBackupFlags([]string{"--dry-run"})
	if rc != backupExitOK {
		t.Fatalf("expected rc 0, got %d", rc)
	}
	if !flags.dryRun {
		t.Error("expected dryRun true")
	}
}

func TestParseBackupFlags_Help(t *testing.T) {
	_, rc := parseBackupFlags([]string{"--help"})
	if rc != backupExitOK {
		t.Fatalf("expected rc 0, got %d", rc)
	}
}

func TestParseBackupFlags_UnknownFlag(t *testing.T) {
	_, rc := parseBackupFlags([]string{"--bogus"})
	if rc != backupExitError {
		t.Fatalf("expected rc 2, got %d", rc)
	}
}

func TestRunBackup_Help(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runBackup([]string{"help"}, &stdout, &stderr)
	if rc != backupExitOK {
		t.Fatalf("expected rc 0, got %d", rc)
	}
	if !strings.Contains(stdout.String(), "helix backup") {
		t.Error("expected help text to contain 'helix backup'")
	}
}

func TestRunBackup_Status_Table(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runBackup([]string{"status"}, &stdout, &stderr)
	if rc != backupExitOK {
		t.Fatalf("expected rc 0, got %d", rc)
	}
	out := stdout.String()
	if !strings.Contains(out, "PATH") {
		t.Error("expected table header with PATH")
	}
	if !strings.Contains(out, "backup targets configured") {
		t.Error("expected summary line")
	}
}

func TestRunBackup_Status_JSON(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runBackup([]string{"status", "--json"}, &stdout, &stderr)
	if rc != backupExitOK {
		t.Fatalf("expected rc 0, got %d", rc)
	}
	var report map[string]interface{}
	if err := json.Unmarshal([]byte(stdout.String()), &report); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	targets, ok := report["targets"].([]interface{})
	if !ok {
		t.Fatal("expected targets array in JSON")
	}
	if len(targets) == 0 {
		t.Error("expected at least 1 backup target")
	}
	count, ok := report["count"].(float64)
	if !ok || count == 0 {
		t.Error("expected non-zero count")
	}
}

func TestRunBackup_Validate_Table(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runBackup([]string{"validate"}, &stdout, &stderr)
	if rc != backupExitOK {
		t.Fatalf("expected rc 0, got %d", rc)
	}
	out := stdout.String()
	if !strings.Contains(out, "EXISTS") {
		t.Error("expected table header with EXISTS")
	}
	if !strings.Contains(out, "targets healthy") {
		t.Error("expected healthy count")
	}
}

func TestRunBackup_Validate_JSON(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runBackup([]string{"validate", "--json"}, &stdout, &stderr)
	if rc != backupExitOK {
		t.Fatalf("expected rc 0, got %d", rc)
	}
	var report map[string]interface{}
	if err := json.Unmarshal([]byte(stdout.String()), &report); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	results, ok := report["results"].([]interface{})
	if !ok {
		t.Fatal("expected results array in JSON")
	}
	if len(results) == 0 {
		t.Error("expected at least 1 validation result")
	}
}

func TestRunBackup_UnknownSubcommand(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runBackup([]string{"bogus"}, &stdout, &stderr)
	if rc != backupExitError {
		t.Fatalf("expected rc 2, got %d", rc)
	}
}

func TestRunBackupWithDryRun_GlobalDryRun(t *testing.T) {
	var stdout, stderr strings.Builder
	err := runBackupWithDryRun([]string{"status"}, &stdout, &stderr, true)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	// Should still produce output
	if !strings.Contains(stdout.String(), "backup targets configured") {
		t.Error("expected status output even with dry-run")
	}
}

func TestRunBackupWithDryRun_NoGlobalDryRun(t *testing.T) {
	var stdout, stderr strings.Builder
	err := runBackupWithDryRun([]string{"status"}, &stdout, &stderr, false)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestTruncateStr(t *testing.T) {
	if truncateStr("hello", 10) != "hello" {
		t.Error("short string should not be truncated")
	}
	if truncateStr("hello world foo", 10) != "hello w..." {
		t.Errorf("expected 'hello w...', got %s", truncateStr("hello world foo", 10))
	}
}

func TestCountHealthy(t *testing.T) {
	// countHealthy with empty slice returns 0
	if countHealthy(nil) != 0 {
		t.Error("expected 0 for nil")
	}
}
