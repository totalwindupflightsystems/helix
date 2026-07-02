// Package backup encodes the Helix backup strategy as structured Go data,
// enabling programmatic validation of backup compliance and generation of
// restore procedures.
//
// Data is derived from specs/SPECIFICATION.md §10.1 (Backup Strategy)
// and §10.2 (Restore Procedure).
package backup

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Frequency
// ---------------------------------------------------------------------------

// Frequency describes how often a backup target should be backed up.
type Frequency string

const (
	FrequencyDaily  Frequency = "daily"
	FrequencyWeekly Frequency = "weekly"
)

// IsValid reports whether f is a recognized frequency.
func (f Frequency) IsValid() bool {
	switch f {
	case FrequencyDaily, FrequencyWeekly:
		return true
	default:
		return false
	}
}

// MaxAge returns the maximum acceptable age for a backup at this frequency.
func (f Frequency) MaxAge() time.Duration {
	switch f {
	case FrequencyDaily:
		return 24 * time.Hour
	case FrequencyWeekly:
		return 7 * 24 * time.Hour
	default:
		return 0
	}
}

// ---------------------------------------------------------------------------
// BackupTarget
// ---------------------------------------------------------------------------

// BackupTarget describes a single backup target from spec §10.1.
type BackupTarget struct {
	// Path is the filesystem path to back up.
	Path string `json:"path"`
	// Content describes what the path contains.
	Content string `json:"content"`
	// Frequency is how often the backup should occur.
	Frequency Frequency `json:"frequency"`
	// Retention is how long to keep backups.
	Retention string `json:"retention"` // e.g., "30 days", "90 days", "14 days"
}

// ---------------------------------------------------------------------------
// BackupManager
// ---------------------------------------------------------------------------

// BackupManager holds all configured backup targets and provides validation.
type BackupManager struct {
	targets []BackupTarget
}

// NewBackupManager creates a manager populated with all spec §10.1 targets.
func NewBackupManager() *BackupManager {
	return &BackupManager{targets: defaultTargets()}
}

// NewBackupManagerWithTargets creates a manager from a custom target set.
func NewBackupManagerWithTargets(targets []BackupTarget) *BackupManager {
	return &BackupManager{targets: targets}
}

// Targets returns all backup targets (copy).
func (m *BackupManager) Targets() []BackupTarget {
	out := make([]BackupTarget, len(m.targets))
	copy(out, m.targets)
	return out
}

// Count returns the number of configured backup targets.
func (m *BackupManager) Count() int {
	return len(m.targets)
}

// LookupByPath returns the backup target for a given path, or nil.
func (m *BackupManager) LookupByPath(path string) *BackupTarget {
	for i := range m.targets {
		if m.targets[i].Path == path {
			return &m.targets[i]
		}
	}
	return nil
}

// LookupByContent returns backup targets matching a content description
// (case-insensitive substring match).
func (m *BackupManager) LookupByContent(content string) []BackupTarget {
	target := strings.ToLower(strings.TrimSpace(content))
	var result []BackupTarget
	for _, t := range m.targets {
		if strings.Contains(strings.ToLower(t.Content), target) {
			result = append(result, t)
		}
	}
	return result
}

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

// ValidationResult describes the compliance state of a single backup target.
type ValidationResult struct {
	Target     BackupTarget  `json:"target"`
	PathExists bool          `json:"path_exists"`
	Fresh      bool          `json:"fresh"`
	LastBackup time.Time     `json:"last_backup,omitempty"`
	Age        time.Duration `json:"age,omitempty"`
	Issue      string        `json:"issue,omitempty"`
}

// IsHealthy returns true if the backup target has no issues.
func (v ValidationResult) IsHealthy() bool {
	return v.PathExists && v.Fresh && v.Issue == ""
}

// Validate checks all backup targets for compliance.
// For each target it verifies: path exists, and last backup is within frequency window.
// lastBackups maps path → last backup time.
func (m *BackupManager) Validate(lastBackups map[string]time.Time) []ValidationResult {
	results := make([]ValidationResult, 0, len(m.targets))
	for _, t := range m.targets {
		result := ValidationResult{Target: t}

		// Check path existence
		if _, err := os.Stat(t.Path); err == nil {
			result.PathExists = true
		} else {
			result.PathExists = false
			result.Issue = "path does not exist"
		}

		// Check freshness
		if lastTime, ok := lastBackups[t.Path]; ok {
			result.LastBackup = lastTime
			result.Age = time.Since(lastTime)
			maxAge := t.Frequency.MaxAge()
			if maxAge > 0 && result.Age <= maxAge {
				result.Fresh = true
			}
		} else if result.PathExists {
			// Path exists but no backup recorded — assume no backup taken
			result.Fresh = false
			if result.Issue == "" {
				result.Issue = "no backup on record"
			}
		}

		results = append(results, result)
	}
	return results
}

// ValidateAtTime validates backup compliance as of a specific reference time.
// This is the testable version of Validate that doesn't use time.Now().
func (m *BackupManager) ValidateAtTime(lastBackups map[string]time.Time, now time.Time) []ValidationResult {
	results := make([]ValidationResult, 0, len(m.targets))
	for _, t := range m.targets {
		result := ValidationResult{Target: t}

		if _, err := os.Stat(t.Path); err == nil {
			result.PathExists = true
		} else {
			result.PathExists = false
			result.Issue = "path does not exist"
		}

		if lastTime, ok := lastBackups[t.Path]; ok {
			result.LastBackup = lastTime
			result.Age = now.Sub(lastTime)
			maxAge := t.Frequency.MaxAge()
			if maxAge > 0 && result.Age <= maxAge {
				result.Fresh = true
			}
		} else if result.PathExists {
			result.Fresh = false
			if result.Issue == "" {
				result.Issue = "no backup on record"
			}
		}

		results = append(results, result)
	}
	return results
}

// ---------------------------------------------------------------------------
// Freshness checking
// ---------------------------------------------------------------------------

// FreshnessStatus reports whether a backup is fresh, stale, or overdue.
type FreshnessStatus struct {
	Target     BackupTarget
	Status     string // "fresh", "stale", "overdue", "unknown"
	Age        time.Duration
	LastBackup time.Time
	HasBackup  bool
}

// CheckFreshness evaluates the freshness of all backup targets.
func (m *BackupManager) CheckFreshness(lastBackups map[string]time.Time) []FreshnessStatus {
	return m.CheckFreshnessAtTime(lastBackups, time.Now())
}

// CheckFreshnessAtTime is the testable variant.
func (m *BackupManager) CheckFreshnessAtTime(lastBackups map[string]time.Time, now time.Time) []FreshnessStatus {
	results := make([]FreshnessStatus, 0, len(m.targets))
	for _, t := range m.targets {
		status := FreshnessStatus{Target: t, Status: "unknown"}

		if lastTime, ok := lastBackups[t.Path]; ok {
			status.HasBackup = true
			status.LastBackup = lastTime
			status.Age = now.Sub(lastTime)
			maxAge := t.Frequency.MaxAge()
			if maxAge > 0 {
				if status.Age <= maxAge {
					status.Status = "fresh"
				} else if status.Age <= maxAge*2 {
					status.Status = "stale"
				} else {
					status.Status = "overdue"
				}
			}
		}

		results = append(results, status)
	}
	return results
}

// ---------------------------------------------------------------------------
// Retention cleanup
// ---------------------------------------------------------------------------

// RetentionEntry describes a backup file and whether it should be cleaned up.
type RetentionEntry struct {
	Filename string
	ModTime  time.Time
	Expired  bool
	AgeDays  int
}

// ComputeRetentionCleanup evaluates backup files against retention policies.
// backupFiles maps target path → list of backup filenames with modification times.
func (m *BackupManager) ComputeRetentionCleanup(
	backupFiles map[string][]RetentionEntry,
	now time.Time,
) []RetentionEntry {
	var expired []RetentionEntry
	for _, t := range m.targets {
		entries, ok := backupFiles[t.Path]
		if !ok {
			continue
		}
		retentionDays := parseRetentionDays(t.Retention)
		if retentionDays <= 0 {
			continue
		}
		for _, entry := range entries {
			entry.AgeDays = int(now.Sub(entry.ModTime).Hours() / 24)
			if entry.AgeDays > retentionDays {
				entry.Expired = true
				expired = append(expired, entry)
			}
		}
	}
	// Sort by age descending (oldest first for cleanup priority)
	sort.Slice(expired, func(i, j int) bool {
		return expired[i].AgeDays > expired[j].AgeDays
	})
	return expired
}

// parseRetentionDays parses a retention string like "30 days" into days.
func parseRetentionDays(s string) int {
	s = strings.TrimSpace(s)
	s = strings.ToLower(s)

	var days int
	var unit string
	_, _ = fmt.Sscanf(s, "%d %s", &days, &unit)

	switch unit {
	case "day", "days", "d":
		return days
	case "week", "weeks", "w":
		return days * 7
	default:
		return days
	}
}

// ---------------------------------------------------------------------------
// Restore Plan (spec §10.2)
// ---------------------------------------------------------------------------

// RestoreStep describes a single step in the restore procedure.
type RestoreStep struct {
	Order       int    `json:"order"`
	Description string `json:"description"`
	Command     string `json:"command"`
	VerifyCmd   string `json:"verify_cmd,omitempty"`
}

// RestorePlan generates the spec §10.2 restore procedure for a given backup date.
func RestorePlan(backupDate string) []RestoreStep {
	return []RestoreStep{
		{
			Order:       1,
			Description: "Stop Forgejo and restore data",
			Command:     fmt.Sprintf("docker compose stop forgejo && rm -rf /var/lib/docker/volumes/helix_forgejo_data/ && tar -xzf \"/mnt/backups/helix/forgejo-%s.tar.gz\" -C / && docker compose start forgejo", backupDate),
			VerifyCmd:   "curl -s http://localhost:3030/api/v1/version",
		},
		{
			Order:       2,
			Description: "Restore databases (Conscientiousness + Hivemind)",
			Command:     fmt.Sprintf("cp \"/mnt/backups/helix/conscience-%s.db\" /data/conscience.db && cp \"/mnt/backups/helix/hivemind-%s.db\" /data/hivemind.db", backupDate, backupDate),
		},
		{
			Order:       3,
			Description: "Restore memory bank and secrets",
			Command:     fmt.Sprintf("tar -xzf \"/mnt/backups/helix/memory-%s.tar.gz\" -C / && cp \"/mnt/backups/helix/env-%s\" /opt/helix/.env && chmod 600 /opt/helix/.env", backupDate, backupDate),
		},
		{
			Order:       4,
			Description: "Restart the platform",
			Command:     "systemctl restart helix-platform",
			VerifyCmd:   "docker compose ps --format \"table {{.Service}}\\t{{.Status}}\"",
		},
	}
}

// FormatRestorePlan renders the restore procedure as a human-readable guide.
func FormatRestorePlan(steps []RestoreStep) string {
	var b strings.Builder
	b.WriteString("Helix Platform Restore Procedure (spec §10.2)\n")
	b.WriteString("==============================================\n\n")
	for _, step := range steps {
		b.WriteString(fmt.Sprintf("Step %d: %s\n", step.Order, step.Description))
		b.WriteString(fmt.Sprintf("  $ %s\n", step.Command))
		if step.VerifyCmd != "" {
			b.WriteString(fmt.Sprintf("  Verify: %s\n", step.VerifyCmd))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Formatting
// ---------------------------------------------------------------------------

// FormatBackupReport renders a summary of backup validation results.
func FormatBackupReport(results []ValidationResult) string {
	var b strings.Builder
	b.WriteString("Helix Backup Report\n")
	b.WriteString("===================\n\n")

	healthy := 0
	for _, r := range results {
		mark := "✓"
		if !r.IsHealthy() {
			mark = "✗"
		} else {
			healthy++
		}
		b.WriteString(fmt.Sprintf("%s %s — %s\n", mark, r.Target.Path, r.Target.Content))
		b.WriteString(fmt.Sprintf("  Frequency: %s, Retention: %s\n", r.Target.Frequency, r.Target.Retention))
		if r.PathExists {
			b.WriteString("  Path: exists\n")
		} else {
			b.WriteString("  Path: MISSING\n")
		}
		if r.HasFreshBackup(results, r.Target.Path) {
			b.WriteString(fmt.Sprintf("  Last backup: %s ago (%s)\n",
				formatDuration(r.Age), r.LastBackup.Format("2006-01-02 15:04")))
		} else {
			b.WriteString("  Last backup: UNKNOWN\n")
		}
		if r.Issue != "" {
			b.WriteString(fmt.Sprintf("  Issue: %s\n", r.Issue))
		}
		b.WriteString("\n")
	}

	b.WriteString(fmt.Sprintf("Summary: %d/%d healthy\n", healthy, len(results)))
	return b.String()
}

// HasFreshBackup is a helper to check if a path has a fresh backup in results.
func (r ValidationResult) HasFreshBackup(_ []ValidationResult, _ string) bool {
	return !r.LastBackup.IsZero()
}

// formatDuration renders a time.Duration in a human-friendly way.
func formatDuration(d time.Duration) string {
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

// ---------------------------------------------------------------------------
// Default backup targets (spec §10.1)
// ---------------------------------------------------------------------------

func defaultTargets() []BackupTarget {
	return []BackupTarget{
		{
			Path:      "/var/lib/forgejo",
			Content:   "Git repos, issues, PRs, wiki",
			Frequency: FrequencyDaily,
			Retention: "30 days",
		},
		{
			Path:      "/opt/helix/.env",
			Content:   "Secrets, config",
			Frequency: FrequencyDaily,
			Retention: "90 days",
		},
		{
			Path:      "/data/conscience.db",
			Content:   "Conscientiousness state",
			Frequency: FrequencyDaily,
			Retention: "30 days",
		},
		{
			Path:      "/data/hivemind.db",
			Content:   "Hivemind state",
			Frequency: FrequencyDaily,
			Retention: "30 days",
		},
		{
			Path:      "/data/memory/",
			Content:   "Hivemind memory bank",
			Frequency: FrequencyDaily,
			Retention: "90 days",
		},
		{
			Path:      "/var/lib/postgresql/data",
			Content:   "LangFuse traces",
			Frequency: FrequencyDaily,
			Retention: "14 days",
		},
		{
			Path:      "/prometheus/",
			Content:   "Metrics TSDB",
			Frequency: FrequencyWeekly,
			Retention: "7 days",
		},
		{
			Path:      "/data/duckbrain/",
			Content:   "DuckBrain memory",
			Frequency: FrequencyDaily,
			Retention: "90 days",
		},
	}
}
