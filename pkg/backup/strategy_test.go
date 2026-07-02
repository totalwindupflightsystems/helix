package backup

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestFrequency_IsValid(t *testing.T) {
	tests := []struct {
		f    Frequency
		want bool
	}{
		{FrequencyDaily, true},
		{FrequencyWeekly, true},
		{Frequency("hourly"), false},
		{Frequency(""), false},
	}
	for _, tt := range tests {
		if got := tt.f.IsValid(); got != tt.want {
			t.Errorf("Frequency(%q).IsValid() = %v, want %v", tt.f, got, tt.want)
		}
	}
}

func TestFrequency_MaxAge(t *testing.T) {
	if FrequencyDaily.MaxAge() != 24*time.Hour {
		t.Errorf("FrequencyDaily.MaxAge() = %v, want 24h", FrequencyDaily.MaxAge())
	}
	if FrequencyWeekly.MaxAge() != 7*24*time.Hour {
		t.Errorf("FrequencyWeekly.MaxAge() = %v, want 168h", FrequencyWeekly.MaxAge())
	}
	if Frequency("invalid").MaxAge() != 0 {
		t.Error("invalid frequency should return 0")
	}
}

func TestNewBackupManager_Count(t *testing.T) {
	m := NewBackupManager()
	if m.Count() != 8 {
		t.Errorf("Count() = %d, want 8 (spec §10.1 has 8 targets)", m.Count())
	}
}

func TestNewBackupManager_Targets(t *testing.T) {
	m := NewBackupManager()
	targets := m.Targets()
	if len(targets) != 8 {
		t.Fatalf("Targets() returned %d, want 8", len(targets))
	}

	// Verify copy
	targets[0].Path = "MODIFIED"
	if m.Targets()[0].Path == "MODIFIED" {
		t.Error("Targets() returned reference to internal slice")
	}
}

func TestNewBackupManagerWithTargets(t *testing.T) {
	custom := []BackupTarget{
		{Path: "/custom", Content: "test", Frequency: FrequencyDaily, Retention: "7 days"},
	}
	m := NewBackupManagerWithTargets(custom)
	if m.Count() != 1 {
		t.Errorf("Count() = %d, want 1", m.Count())
	}
}

func TestBackupManager_LookupByPath(t *testing.T) {
	m := NewBackupManager()

	entry := m.LookupByPath("/var/lib/forgejo")
	if entry == nil {
		t.Fatal("LookupByPath('/var/lib/forgejo') returned nil")
	}
	if entry.Content != "Git repos, issues, PRs, wiki" {
		t.Errorf("Content = %q, want 'Git repos, issues, PRs, wiki'", entry.Content)
	}

	if m.LookupByPath("/nonexistent") != nil {
		t.Error("LookupByPath('/nonexistent') should return nil")
	}
}

func TestBackupManager_LookupByContent(t *testing.T) {
	m := NewBackupManager()

	// Substring match on content
	results := m.LookupByContent("git repos")
	if len(results) == 0 {
		t.Error("LookupByContent('git repos') returned 0 results")
	}

	// Case insensitive
	results = m.LookupByContent("GIT REPOS")
	if len(results) == 0 {
		t.Error("LookupByContent('GIT REPOS') should match case-insensitively")
	}

	// Match "hivemind"
	results = m.LookupByContent("hivemind")
	if len(results) == 0 {
		t.Error("LookupByContent('hivemind') returned 0 results")
	}

	// No match
	results = m.LookupByContent("nonexistent content xyz")
	if len(results) != 0 {
		t.Error("LookupByContent('nonexistent content xyz') should return 0 results")
	}
}

func TestBackupManager_ValidateAtTime_PathExists(t *testing.T) {
	// Create a temp dir to serve as an existing path
	tmpDir := t.TempDir()
	m := NewBackupManagerWithTargets([]BackupTarget{
		{Path: tmpDir, Content: "test", Frequency: FrequencyDaily, Retention: "30 days"},
		{Path: "/nonexistent/path/xyz", Content: "missing", Frequency: FrequencyDaily, Retention: "30 days"},
	})

	now := time.Now()
	results := m.ValidateAtTime(map[string]time.Time{}, now)

	if len(results) != 2 {
		t.Fatalf("ValidateAtTime returned %d results, want 2", len(results))
	}

	// First target: path exists
	if !results[0].PathExists {
		t.Error("first target should have PathExists=true")
	}
	// Second target: path doesn't exist
	if results[1].PathExists {
		t.Error("second target should have PathExists=false")
	}
	if results[1].Issue != "path does not exist" {
		t.Errorf("second target issue = %q, want 'path does not exist'", results[1].Issue)
	}
}

func TestBackupManager_ValidateAtTime_FreshBackup(t *testing.T) {
	tmpDir := t.TempDir()
	m := NewBackupManagerWithTargets([]BackupTarget{
		{Path: tmpDir, Content: "test", Frequency: FrequencyDaily, Retention: "30 days"},
	})

	now := time.Now()
	lastBackups := map[string]time.Time{
		tmpDir: now.Add(-2 * time.Hour), // 2 hours ago — fresh
	}

	results := m.ValidateAtTime(lastBackups, now)
	if !results[0].Fresh {
		t.Error("2h old daily backup should be fresh")
	}
	if !results[0].IsHealthy() {
		t.Error("path exists + fresh backup should be healthy")
	}
}

func TestBackupManager_ValidateAtTime_StaleBackup(t *testing.T) {
	tmpDir := t.TempDir()
	m := NewBackupManagerWithTargets([]BackupTarget{
		{Path: tmpDir, Content: "test", Frequency: FrequencyDaily, Retention: "30 days"},
	})

	now := time.Now()
	lastBackups := map[string]time.Time{
		tmpDir: now.Add(-48 * time.Hour), // 2 days ago — stale for daily
	}

	results := m.ValidateAtTime(lastBackups, now)
	if results[0].Fresh {
		t.Error("48h old daily backup should not be fresh")
	}
}

func TestBackupManager_ValidateAtTime_NoBackupRecorded(t *testing.T) {
	tmpDir := t.TempDir()
	m := NewBackupManagerWithTargets([]BackupTarget{
		{Path: tmpDir, Content: "test", Frequency: FrequencyDaily, Retention: "30 days"},
	})

	now := time.Now()
	results := m.ValidateAtTime(map[string]time.Time{}, now)

	if results[0].Fresh {
		t.Error("no backup recorded should not be fresh")
	}
	if results[0].Issue != "no backup on record" {
		t.Errorf("Issue = %q, want 'no backup on record'", results[0].Issue)
	}
}

func TestBackupManager_CheckFreshnessAtTime(t *testing.T) {
	tmpDir := t.TempDir()
	m := NewBackupManagerWithTargets([]BackupTarget{
		{Path: tmpDir, Content: "test", Frequency: FrequencyDaily, Retention: "30 days"},
		{Path: "/nonexistent", Content: "missing", Frequency: FrequencyWeekly, Retention: "7 days"},
	})

	now := time.Now()
	lastBackups := map[string]time.Time{
		tmpDir:         now.Add(-1 * time.Hour),       // fresh
		"/nonexistent": now.Add(-20 * 24 * time.Hour), // overdue for weekly (max 7d, >14d)
	}

	results := m.CheckFreshnessAtTime(lastBackups, now)
	if len(results) != 2 {
		t.Fatalf("CheckFreshnessAtTime returned %d, want 2", len(results))
	}

	if results[0].Status != "fresh" {
		t.Errorf("1h old daily backup status = %q, want 'fresh'", results[0].Status)
	}
	if results[1].Status != "overdue" {
		t.Errorf("10d old weekly backup status = %q, want 'overdue'", results[1].Status)
	}
}

func TestBackupManager_CheckFreshnessAtTime_Stale(t *testing.T) {
	tmpDir := t.TempDir()
	m := NewBackupManagerWithTargets([]BackupTarget{
		{Path: tmpDir, Content: "test", Frequency: FrequencyDaily, Retention: "30 days"},
	})

	now := time.Now()
	lastBackups := map[string]time.Time{
		tmpDir: now.Add(-30 * time.Hour), // 30h old — stale (between 1d and 2d)
	}

	results := m.CheckFreshnessAtTime(lastBackups, now)
	if results[0].Status != "stale" {
		t.Errorf("30h old daily backup status = %q, want 'stale'", results[0].Status)
	}
}

func TestBackupManager_CheckFreshnessAtTime_NoBackup(t *testing.T) {
	tmpDir := t.TempDir()
	m := NewBackupManagerWithTargets([]BackupTarget{
		{Path: tmpDir, Content: "test", Frequency: FrequencyDaily, Retention: "30 days"},
	})

	now := time.Now()
	results := m.CheckFreshnessAtTime(map[string]time.Time{}, now)
	if results[0].Status != "unknown" {
		t.Errorf("no backup status = %q, want 'unknown'", results[0].Status)
	}
	if results[0].HasBackup {
		t.Error("HasBackup should be false when no backup recorded")
	}
}

func TestParseRetentionDays(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"30 days", 30},
		{"90 days", 90},
		{"14 days", 14},
		{"7 days", 7},
		{"2 weeks", 14},
		{"1 week", 7},
		{"5", 5},
		{"", 0},
		{"invalid", 0},
	}
	for _, tt := range tests {
		got := parseRetentionDays(tt.input)
		if got != tt.want {
			t.Errorf("parseRetentionDays(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestBackupManager_ComputeRetentionCleanup(t *testing.T) {
	m := NewBackupManagerWithTargets([]BackupTarget{
		{Path: "/test", Content: "test", Frequency: FrequencyDaily, Retention: "30 days"},
	})

	now := time.Now()
	backupFiles := map[string][]RetentionEntry{
		"/test": {
			{Filename: "backup-001.tar.gz", ModTime: now.Add(-35 * 24 * time.Hour)}, // expired
			{Filename: "backup-002.tar.gz", ModTime: now.Add(-10 * 24 * time.Hour)}, // fresh
			{Filename: "backup-003.tar.gz", ModTime: now.Add(-60 * 24 * time.Hour)}, // expired
		},
	}

	expired := m.ComputeRetentionCleanup(backupFiles, now)
	if len(expired) != 2 {
		t.Fatalf("ComputeRetentionCleanup returned %d expired, want 2", len(expired))
	}

	// Should be sorted by age descending (oldest first)
	if expired[0].AgeDays < expired[1].AgeDays {
		t.Error("expired entries should be sorted by age descending")
	}
	for _, e := range expired {
		if !e.Expired {
			t.Error("all returned entries should have Expired=true")
		}
	}
}

func TestBackupManager_ComputeRetentionCleanup_NoMatch(t *testing.T) {
	m := NewBackupManagerWithTargets([]BackupTarget{
		{Path: "/test", Content: "test", Frequency: FrequencyDaily, Retention: "30 days"},
	})

	now := time.Now()
	// Backup files for a different path
	backupFiles := map[string][]RetentionEntry{
		"/other": {
			{Filename: "old.tar.gz", ModTime: now.Add(-100 * 24 * time.Hour)},
		},
	}

	expired := m.ComputeRetentionCleanup(backupFiles, now)
	if len(expired) != 0 {
		t.Errorf("ComputeRetentionCleanup with unmatched paths returned %d, want 0", len(expired))
	}
}

func TestRestorePlan(t *testing.T) {
	steps := RestorePlan("20260619-140000")
	if len(steps) != 4 {
		t.Fatalf("RestorePlan returned %d steps, want 4", len(steps))
	}

	// Verify step ordering
	for i, step := range steps {
		if step.Order != i+1 {
			t.Errorf("step %d has Order %d, want %d", i, step.Order, i+1)
		}
	}

	// Verify commands reference the backup date (steps 1-3)
	for i, step := range steps {
		if i < 3 && !strings.Contains(step.Command, "20260619-140000") {
			t.Errorf("step %d command missing backup date: %s", step.Order, step.Command)
		}
	}

	// Verify first step stops Forgejo
	if !strings.Contains(steps[0].Command, "docker compose stop forgejo") {
		t.Error("step 1 should stop Forgejo")
	}

	// Verify last step restarts platform
	if !strings.Contains(steps[3].Command, "systemctl restart helix-platform") {
		t.Error("step 4 should restart platform")
	}
}

func TestFormatRestorePlan(t *testing.T) {
	steps := RestorePlan("20260619-140000")
	out := FormatRestorePlan(steps)

	checks := []string{
		"Restore Procedure",
		"Step 1:",
		"Step 4:",
		"docker compose stop forgejo",
		"systemctl restart helix-platform",
	}
	for _, c := range checks {
		if !strings.Contains(out, c) {
			t.Errorf("FormatRestorePlan missing %q in output", c)
		}
	}
}

func TestFormatBackupReport(t *testing.T) {
	tmpDir := t.TempDir()
	m := NewBackupManagerWithTargets([]BackupTarget{
		{Path: tmpDir, Content: "test data", Frequency: FrequencyDaily, Retention: "30 days"},
	})

	results := m.ValidateAtTime(map[string]time.Time{}, time.Now())
	out := FormatBackupReport(results)

	if !strings.Contains(out, "Helix Backup Report") {
		t.Error("FormatBackupReport missing header")
	}
	if !strings.Contains(out, "test data") {
		t.Error("FormatBackupReport missing content description")
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Minute, "30m"},
		{2 * time.Hour, "2h"},
		{48 * time.Hour, "2d"},
	}
	for _, tt := range tests {
		got := formatDuration(tt.d)
		if got != tt.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestValidationResult_IsHealthy(t *testing.T) {
	tests := []struct {
		name   string
		result ValidationResult
		want   bool
	}{
		{
			name: "all healthy",
			result: ValidationResult{
				Target:     BackupTarget{Path: "/x"},
				PathExists: true,
				Fresh:      true,
				Issue:      "",
			},
			want: true,
		},
		{
			name: "path missing",
			result: ValidationResult{
				PathExists: false,
				Fresh:      true,
			},
			want: false,
		},
		{
			name: "not fresh",
			result: ValidationResult{
				PathExists: true,
				Fresh:      false,
			},
			want: false,
		},
		{
			name: "has issue",
			result: ValidationResult{
				PathExists: true,
				Fresh:      true,
				Issue:      "custom error",
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.result.IsHealthy(); got != tt.want {
				t.Errorf("IsHealthy() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDefaultTargets_AllSpecEntries(t *testing.T) {
	m := NewBackupManager()
	targets := m.Targets()

	expectedPaths := map[string]bool{
		"/var/lib/forgejo":         true,
		"/opt/helix/.env":          true,
		"/data/conscience.db":      true,
		"/data/hivemind.db":        true,
		"/data/memory/":            true,
		"/var/lib/postgresql/data": true,
		"/prometheus/":             true,
		"/data/duckbrain/":         true,
	}

	for _, tgt := range targets {
		if !expectedPaths[tgt.Path] {
			t.Errorf("unexpected backup target path: %q", tgt.Path)
		}
		delete(expectedPaths, tgt.Path)
	}

	if len(expectedPaths) > 0 {
		t.Errorf("missing backup targets: %v", expectedPaths)
	}
}

func TestValidate_FileSystemCheck(t *testing.T) {
	// Create a real file for the test
	f, err := os.CreateTemp("", "helix-backup-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(f.Name()) }()

	m := NewBackupManagerWithTargets([]BackupTarget{
		{Path: f.Name(), Content: "exists", Frequency: FrequencyDaily, Retention: "30 days"},
		{Path: "/nonexistent/path/xyz123", Content: "missing", Frequency: FrequencyDaily, Retention: "30 days"},
	})

	now := time.Now()
	results := m.ValidateAtTime(map[string]time.Time{}, now)

	if !results[0].PathExists {
		t.Error("existing file should have PathExists=true")
	}
	if results[1].PathExists {
		t.Error("nonexistent path should have PathExists=false")
	}
}

func TestValidate_UsesTimeNow(t *testing.T) {
	tmpDir := t.TempDir()
	m := NewBackupManagerWithTargets([]BackupTarget{
		{Path: tmpDir, Content: "test", Frequency: FrequencyDaily, Retention: "30 days"},
	})

	// Recent backup → should be fresh
	results := m.Validate(map[string]time.Time{
		tmpDir: time.Now().Add(-1 * time.Hour),
	})
	if !results[0].IsHealthy() {
		t.Error("1h old daily backup with existing path should be healthy")
	}

	// Very old backup → should not be fresh
	results = m.Validate(map[string]time.Time{
		tmpDir: time.Now().Add(-72 * time.Hour),
	})
	if results[0].Fresh {
		t.Error("72h old daily backup should not be fresh")
	}
}

func TestCheckFreshness_UsesTimeNow(t *testing.T) {
	tmpDir := t.TempDir()
	m := NewBackupManagerWithTargets([]BackupTarget{
		{Path: tmpDir, Content: "test", Frequency: FrequencyDaily, Retention: "30 days"},
	})

	results := m.CheckFreshness(map[string]time.Time{
		tmpDir: time.Now().Add(-1 * time.Hour),
	})
	if len(results) != 1 {
		t.Fatalf("CheckFreshness returned %d results, want 1", len(results))
	}
	if results[0].Status != "fresh" {
		t.Errorf("1h old daily backup status = %q, want 'fresh'", results[0].Status)
	}
	if !results[0].HasBackup {
		t.Error("HasBackup should be true when backup recorded")
	}
}
