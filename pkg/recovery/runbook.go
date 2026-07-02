// Package recovery encodes the Helix error recovery procedures as structured
// Go data, enabling programmatic lookup of recovery actions for any component
// failure mode.
//
// The data is derived from:
//   - specs/SPECIFICATION.md §14 (Error Recovery — Component Failure Matrix)
//   - specs/error-recovery.md (per-component recovery procedures)
//   - specs/SPECIFICATION.md §10.5 (Incident Response — SEV levels)
package recovery

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Severity levels (spec §10.5)
// ---------------------------------------------------------------------------

// Severity represents the Helix incident severity classification.
type Severity string

const (
	// SEV1: Platform unavailable. Immediate response, all-hands.
	SEV1 Severity = "SEV-1"
	// SEV2: Degraded service. Response <30 minutes.
	SEV2 Severity = "SEV-2"
	// SEV3: Non-critical issue. Next business day response.
	SEV3 Severity = "SEV-3"
)

// SeverityDescription returns a human-readable description for a severity level.
func SeverityDescription(s Severity) string {
	switch s {
	case SEV1:
		return "Platform unavailable — immediate, all-hands response"
	case SEV2:
		return "Degraded service — response within 30 minutes"
	case SEV3:
		return "Non-critical issue — next business day response"
	default:
		return "Unknown severity"
	}
}

// ---------------------------------------------------------------------------
// Core types
// ---------------------------------------------------------------------------

// RecoveryAction describes a single step in a recovery procedure.
type RecoveryAction struct {
	// Command is the shell command to execute (may contain template variables).
	Command string `json:"command"`
	// Description explains what this action does.
	Description string `json:"description"`
	// VerifyCmd is an optional verification command to run after the action.
	VerifyCmd string `json:"verify_cmd,omitempty"`
	// VerifyExpected is the expected output of the verification command.
	VerifyExpected string `json:"verify_expected,omitempty"`
}

// FailureEntry represents a single row in the spec §14.1 component failure matrix.
type FailureEntry struct {
	// Component is the name of the failing component (Forgejo, Chimera, etc.).
	Component string `json:"component"`
	// FailureMode describes what went wrong.
	FailureMode string `json:"failure_mode"`
	// Detection describes how the failure is detected.
	Detection string `json:"detection"`
	// Impact describes the effect on the platform.
	Impact string `json:"impact"`
	// Actions are the ordered recovery steps to perform.
	Actions []RecoveryAction `json:"actions"`
	// RTO is the Recovery Time Objective (how fast service must be restored).
	RTO string `json:"rto"`
	// RPO is the Recovery Point Objective (max acceptable data loss window).
	RPO string `json:"rpo"`
	// Severity classifies the incident level (SEV-1/2/3).
	Severity Severity `json:"severity"`
	// ErrorID is the spec error-recovery.md procedure ID (e.g., FJ-001).
	ErrorID string `json:"error_id,omitempty"`
}

// ---------------------------------------------------------------------------
// Recovery Registry
// ---------------------------------------------------------------------------

// RecoveryRegistry holds all known failure entries and provides lookup methods.
type RecoveryRegistry struct {
	entries []FailureEntry
}

// NewRecoveryRegistry creates a registry populated with all spec §14.1 failure
// entries and specs/error-recovery.md procedures.
func NewRecoveryRegistry() *RecoveryRegistry {
	return &RecoveryRegistry{entries: defaultFailures()}
}

// NewRecoveryRegistryWithEntries creates a registry from a custom set of entries.
func NewRecoveryRegistryWithEntries(entries []FailureEntry) *RecoveryRegistry {
	return &RecoveryRegistry{entries: entries}
}

// All returns all failure entries in the registry.
func (r *RecoveryRegistry) All() []FailureEntry {
	out := make([]FailureEntry, len(r.entries))
	copy(out, r.entries)
	return out
}

// LookupByComponent returns all failure entries for a given component name
// (case-insensitive).
func (r *RecoveryRegistry) LookupByComponent(component string) []FailureEntry {
	target := strings.ToLower(strings.TrimSpace(component))
	var result []FailureEntry
	for _, e := range r.entries {
		if strings.ToLower(e.Component) == target {
			result = append(result, e)
		}
	}
	return result
}

// LookupByFailureMode returns all failure entries matching a failure mode
// (case-insensitive substring match).
func (r *RecoveryRegistry) LookupByFailureMode(mode string) []FailureEntry {
	target := strings.ToLower(strings.TrimSpace(mode))
	var result []FailureEntry
	for _, e := range r.entries {
		if strings.Contains(strings.ToLower(e.FailureMode), target) {
			result = append(result, e)
		}
	}
	return result
}

// LookupByID returns the failure entry with the given error-recovery.md ID
// (e.g., "FJ-001"), or nil if not found.
func (r *RecoveryRegistry) LookupByID(id string) *FailureEntry {
	target := strings.ToUpper(strings.TrimSpace(id))
	if target == "" {
		return nil
	}
	for i := range r.entries {
		if r.entries[i].ErrorID == target {
			return &r.entries[i]
		}
	}
	return nil
}

// LookupBySeverity returns all entries at a given severity level.
func (r *RecoveryRegistry) LookupBySeverity(s Severity) []FailureEntry {
	var result []FailureEntry
	for _, e := range r.entries {
		if e.Severity == s {
			result = append(result, e)
		}
	}
	return result
}

// Components returns a sorted list of unique component names in the registry.
func (r *RecoveryRegistry) Components() []string {
	seen := make(map[string]bool)
	for _, e := range r.entries {
		seen[e.Component] = true
	}
	result := make([]string, 0, len(seen))
	for k := range seen {
		result = append(result, k)
	}
	sort.Strings(result)
	return result
}

// RecoveryMatrix returns the full grid of failures as a map of component → []FailureEntry.
func (r *RecoveryRegistry) RecoveryMatrix() map[string][]FailureEntry {
	matrix := make(map[string][]FailureEntry)
	for _, e := range r.entries {
		matrix[e.Component] = append(matrix[e.Component], e)
	}
	return matrix
}

// Count returns the number of failure entries in the registry.
func (r *RecoveryRegistry) Count() int {
	return len(r.entries)
}

// ---------------------------------------------------------------------------
// Formatting
// ---------------------------------------------------------------------------

// FormatRunbook renders a failure entry as a human-readable runbook.
func FormatRunbook(e FailureEntry) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("=== %s [%s] ===\n", e.ErrorID, e.Severity))
	b.WriteString(fmt.Sprintf("Component: %s\n", e.Component))
	b.WriteString(fmt.Sprintf("Failure:   %s\n", e.FailureMode))
	b.WriteString(fmt.Sprintf("Detection: %s\n", e.Detection))
	b.WriteString(fmt.Sprintf("Impact:    %s\n", e.Impact))
	b.WriteString(fmt.Sprintf("RTO: %s  RPO: %s\n\n", e.RTO, e.RPO))

	if len(e.Actions) > 0 {
		b.WriteString("Recovery Steps:\n")
		for i, a := range e.Actions {
			b.WriteString(fmt.Sprintf("  %d. %s\n", i+1, a.Description))
			if a.Command != "" {
				b.WriteString(fmt.Sprintf("     $ %s\n", a.Command))
			}
			if a.VerifyCmd != "" {
				b.WriteString(fmt.Sprintf("     Verify: %s\n", a.VerifyCmd))
				if a.VerifyExpected != "" {
					b.WriteString(fmt.Sprintf("     Expected: %s\n", a.VerifyExpected))
				}
			}
		}
	}

	return b.String()
}

// FormatMatrix renders the full recovery matrix as a human-readable table.
func FormatMatrix(r *RecoveryRegistry) string {
	if r == nil || r.Count() == 0 {
		return "No recovery procedures registered.\n"
	}

	var b strings.Builder
	b.WriteString("Helix Recovery Matrix\n")
	b.WriteString("=====================\n\n")

	for _, comp := range r.Components() {
		entries := r.LookupByComponent(comp)
		b.WriteString(fmt.Sprintf("[%s] (%d failure modes)\n", comp, len(entries)))
		for _, e := range entries {
			sev := string(e.Severity)
			if e.ErrorID != "" {
				b.WriteString(fmt.Sprintf("  %s [%s] %s — %s\n", e.ErrorID, sev, e.FailureMode, e.RTO))
			} else {
				b.WriteString(fmt.Sprintf("  [%s] %s — %s\n", sev, e.FailureMode, e.RTO))
			}
		}
		b.WriteString("\n")
	}

	return b.String()
}

// ---------------------------------------------------------------------------
// Retry policy helpers (spec §14.3)
// ---------------------------------------------------------------------------

// RetryConfig describes the retry behavior for LLM API calls per spec §14.3.
type RetryConfig struct {
	MaxAttempts int           // max retry attempts (default 4)
	BaseDelay   time.Duration // initial delay (default 2s)
	MaxDelay    time.Duration // max delay cap (default 8s)
}

// DefaultRetryConfig returns the spec §14.3 retry policy.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts: 4,
		BaseDelay:   2 * time.Second,
		MaxDelay:    8 * time.Second,
	}
}

// BackoffFor returns the delay for the given attempt (0-indexed).
// Attempt 0 = immediate (0 delay), attempt 1 = BaseDelay, etc.
func (c RetryConfig) BackoffFor(attempt int) time.Duration {
	if attempt <= 0 {
		return 0
	}
	delay := c.BaseDelay
	for i := 1; i < attempt; i++ {
		next := delay * 2
		// Check for overflow or exceeding max
		if next <= delay || next > c.MaxDelay {
			return c.MaxDelay
		}
		delay = next
	}
	return delay
}

// ---------------------------------------------------------------------------
// Default failure entries (spec §14.1 + specs/error-recovery.md)
// ---------------------------------------------------------------------------

func defaultFailures() []FailureEntry {
	return []FailureEntry{
		// --- Forgejo ---
		{
			Component:   "Forgejo",
			FailureMode: "Process crash",
			Detection:   "Health check fails (HTTP 503)",
			Impact:      "ALL agents blocked. No git operations.",
			Actions: []RecoveryAction{
				{Description: "Restart container", Command: "docker compose start forgejo", VerifyCmd: "curl -s http://localhost:3030/api/v1/version", VerifyExpected: "{\"version\":"},
				{Description: "If data corruption: restore from backup", Command: "tar -xzf $(ls -t /mnt/backups/helix/forgejo-*.tar.gz | head -1) -C /"},
			},
			RTO:      "5 min (restart), 1 hour (restore)",
			RPO:      "24 hours",
			Severity: SEV1,
			ErrorID:  "FJ-001",
		},
		{
			Component:   "Forgejo",
			FailureMode: "Admin auth failed",
			Detection:   "curl -u helio:$PASSWORD returns 401",
			Impact:      "Cannot provision or manage agents.",
			Actions: []RecoveryAction{
				{Description: "Reset admin password via CLI", Command: `docker exec forgejo-helix forgejo admin user change-password --username helio --password newpassword123`},
				{Description: "Verify new password", Command: "curl -s -u helio:$NEW_PASS http://localhost:3030/api/v1/version"},
			},
			RTO:      "5 min",
			RPO:      "0",
			Severity: SEV2,
			ErrorID:  "FJ-002",
		},
		{
			Component:   "Forgejo",
			FailureMode: "Disk full",
			Detection:   "Write errors, health check degradation",
			Impact:      "Agents can't push. PRs can't be created.",
			Actions: []RecoveryAction{
				{Description: "Clear old CI artifacts", Command: "docker system prune -af --volumes"},
				{Description: "Expand volume if needed"},
			},
			RTO:      "15 min",
			RPO:      "0",
			Severity: SEV1,
		},
		{
			Component:   "Forgejo",
			FailureMode: "Repository corruption",
			Detection:   "Health check failure, git errors",
			Impact:      "Repository data lost or unreadable.",
			Actions: []RecoveryAction{
				{Description: "Stop Forgejo", Command: "docker compose stop forgejo"},
				{Description: "Restore from latest backup", Command: "BACKUP=$(ls -t /mnt/backups/helix/forgejo-*.tar.gz | head -1) && tar -xzf \"$BACKUP\" -C /"},
				{Description: "Recover recent pushes from agent worktrees", Command: `for agent in /worktrees/agent-*; do git --git-dir="$agent/.git" log --since="24 hours ago" --oneline; done`},
				{Description: "Start Forgejo", Command: "docker compose start forgejo"},
			},
			RTO:      "1 hour",
			RPO:      "0 (git is distributed)",
			Severity: SEV1,
		},
		// --- Chimera ---
		{
			Component:   "Chimera",
			FailureMode: "API timeout / unreachable",
			Detection:   "Review doesn't complete, curl timeout",
			Impact:      "PR review blocked. Co-approval (agent side) unavailable.",
			Actions: []RecoveryAction{
				{Description: "Check if process is running", Command: "pgrep -f chimera.api.server"},
				{Description: "Restart Chimera", Command: `.venv/bin/python -c "from chimera.api.server import app; import uvicorn; uvicorn.run(app, host='0.0.0.0', port=8765)" &`},
				{Description: "Verify", Command: "sleep 5 && curl -s http://localhost:8765/v1/health"},
			},
			RTO:      "5 min (restart)",
			RPO:      "0",
			Severity: SEV2,
			ErrorID:  "CH-001",
		},
		{
			Component:   "Chimera",
			FailureMode: "Package import error",
			Detection:   "ImportError from chimera.api.server",
			Impact:      "Chimera service won't start.",
			Actions: []RecoveryAction{
				{Description: "Reinstall package", Command: "cd /home/kara/chimera && .venv/bin/pip install -e ."},
				{Description: "Verify import path", Command: `.venv/bin/python -c "import chimera; print(chimera.__file__)"`},
			},
			RTO:      "5 min",
			RPO:      "0",
			Severity: SEV2,
			ErrorID:  "CH-002",
		},
		{
			Component:   "Chimera",
			FailureMode: "Formation timeout",
			Detection:   "HTTP 504 or >120s response",
			Impact:      "PR review delayed or fails.",
			Actions: []RecoveryAction{
				{Description: "Check provider API keys", Command: "grep -c 'api_key' /home/kara/chimera/chimera.yaml"},
				{Description: "Test single provider", Command: `curl -s https://api.deepseek.com/v1/models -H "Authorization: Bearer $DEEPSEEK_API_KEY"`},
				{Description: "Fall back to budget formation", Command: `curl -X POST http://localhost:8765/v1/deliberate -H "Content-Type: application/json" -d '{"prompt":"test","formation":"budget"}'`},
			},
			RTO:      "2 min",
			RPO:      "0",
			Severity: SEV3,
			ErrorID:  "CH-003",
		},
		// --- Conscientiousness ---
		{
			Component:   "Conscientiousness",
			FailureMode: "Loop stall (>10 min)",
			Detection:   "Timeout, no report generated",
			Impact:      "Adversarial review blocked.",
			Actions: []RecoveryAction{
				{Description: "Kill loop and restart service"},
				{Description: "PR proceeds without adversarial check (human override)"},
			},
			RTO:      "3 min",
			RPO:      "0",
			Severity: SEV3,
		},
		// --- Hivemind ---
		{
			Component:   "Hivemind",
			FailureMode: "SQLite corruption",
			Detection:   "Read/write errors",
			Impact:      "Memory bank unavailable. Task scheduling degraded.",
			Actions: []RecoveryAction{
				{Description: "Check integrity", Command: `sqlite3 /data/hivemind.db "PRAGMA integrity_check;"`},
				{Description: "Restore from daily backup if corrupt", Command: "cp /mnt/backups/helix/hivemind-$(date +%Y%m%d).db /data/hivemind.db"},
				{Description: "Restart platform", Command: "systemctl restart helix-platform"},
			},
			RTO:      "15 min",
			RPO:      "24 hours",
			Severity: SEV2,
		},
		{
			Component:   "Hivemind",
			FailureMode: "Process crash",
			Detection:   "Health check fails",
			Impact:      "Task scheduling stops. No new tasks assigned.",
			Actions: []RecoveryAction{
				{Description: "Restart container", Command: "docker compose start hivemind"},
			},
			RTO:      "2 min",
			RPO:      "0",
			Severity: SEV2,
		},
		// --- Agent container ---
		{
			Component:   "Agent container",
			FailureMode: "OOM kill",
			Detection:   "Container exits, metrics drop",
			Impact:      "That agent stops working. Other agents unaffected.",
			Actions: []RecoveryAction{
				{Description: "H4F restarts container automatically"},
				{Description: "Agent resumes from last commit"},
			},
			RTO:      "1 min",
			RPO:      "0",
			Severity: SEV3,
		},
		{
			Component:   "Agent container",
			FailureMode: "Budget exhausted",
			Detection:   "API returns 403",
			Impact:      "Agent can't make LLM calls.",
			Actions: []RecoveryAction{
				{Description: "Human approves budget increase, or agent waits until next cycle"},
			},
			RTO:      "Manual",
			RPO:      "0",
			Severity: SEV3,
		},
		// --- LangFuse ---
		{
			Component:   "LangFuse",
			FailureMode: "Postgres down",
			Detection:   "Trace writes fail",
			Impact:      "Traces not ingested. Observability degraded.",
			Actions: []RecoveryAction{
				{Description: "Restore Postgres", Command: "docker compose start langfuse-db"},
				{Description: "Traces buffered and replayed"},
			},
			RTO:      "10 min",
			RPO:      "0 (buffered)",
			Severity: SEV3,
		},
		{
			Component:   "LangFuse",
			FailureMode: "Process crash",
			Detection:   "Health check fails",
			Impact:      "Traces not ingested.",
			Actions: []RecoveryAction{
				{Description: "Restart container", Command: "docker compose start langfuse"},
			},
			RTO:      "2 min",
			RPO:      "0",
			Severity: SEV3,
		},
		// --- Prometheus ---
		{
			Component:   "Prometheus",
			FailureMode: "TSDB corruption",
			Detection:   "Scrapes fail, metrics missing",
			Impact:      "Alerting degraded. No metric history.",
			Actions: []RecoveryAction{
				{Description: "Restore from backup or restart with fresh TSDB"},
			},
			RTO:      "5 min",
			RPO:      "Variable",
			Severity: SEV3,
		},
		// --- Caddy ---
		{
			Component:   "Caddy",
			FailureMode: "Process crash",
			Detection:   "HTTPS down, Forgejo unreachable",
			Impact:      "Platform unreachable externally. Internal services still work.",
			Actions: []RecoveryAction{
				{Description: "Restart Caddy", Command: "systemctl restart caddy"},
			},
			RTO:      "1 min",
			RPO:      "0",
			Severity: SEV2,
		},
		// --- DNS ---
		{
			Component:   "DNS",
			FailureMode: "Resolution failure",
			Detection:   "Forgejo unreachable by domain",
			Impact:      "External access blocked. Agents use internal Docker network (unaffected).",
			Actions: []RecoveryAction{
				{Description: "Fix DNS record, reduce TTL"},
			},
			RTO:      "Variable (DNS propagation)",
			RPO:      "0",
			Severity: SEV2,
		},
		// --- GitReins hook ---
		{
			Component:   "GitReins hook",
			FailureMode: "Hook timeout/crash",
			Detection:   "Push fails with hook error",
			Impact:      "Agents can't push commits.",
			Actions: []RecoveryAction{
				{Description: "Disable hook temporarily (emergency)"},
				{Description: "Fix hook code, re-enable"},
			},
			RTO:      "10 min",
			RPO:      "0",
			Severity: SEV2,
		},
	}
}
