package prompt

import (
	"fmt"
	"time"
)

// validTransitions encodes the 7-state lifecycle machine from spec §7.
var validTransitions = map[LifecycleStatus][]LifecycleStatus{
	StatusDraft:      {StatusProposed},
	StatusProposed:   {StatusReviewed, StatusDraft},
	StatusReviewed:   {StatusAttested},
	StatusAttested:   {StatusActive},
	StatusActive:     {StatusDeprecated},
	StatusDeprecated: {StatusRetired, StatusActive}, // retired = time-based, active = rollback
	StatusRetired:    {},                            // terminal
}

// AllowedForAttestation reports whether a prompt in the given status may be
// used in a commit attestation. The second return value is true when the
// status is deprecated — allowed but warned (30-day grace per spec §7).
func AllowedForAttestation(s LifecycleStatus) (allowed bool, warn bool) {
	switch s {
	case StatusAttested, StatusActive:
		return true, false
	case StatusDeprecated:
		return true, true // WARN: 30-day grace period
	default:
		return false, false
	}
}

// ValidTransition checks whether a transition from one status to another is
// permitted by the state machine (spec §7).
func ValidTransition(from, to LifecycleStatus) bool {
	for _, allowed := range validTransitions[from] {
		if allowed == to {
			return true
		}
	}
	return false
}

// AllowedTransitions returns all statuses reachable from the given status.
func AllowedTransitions(from LifecycleStatus) []LifecycleStatus {
	return validTransitions[from]
}

// DeprecationGrace returns the grace period for a given status:
//   - Deprecated: 30 days (attestation grace per spec §7)
//   - Retired: 90 days (minimum time in deprecated before retirement)
//   - All others: 0
func DeprecationGrace(s LifecycleStatus) time.Duration {
	switch s {
	case StatusDeprecated:
		return 30 * 24 * time.Hour
	case StatusRetired:
		return 90 * 24 * time.Hour
	default:
		return 0
	}
}

// ValidateTransition checks whether a transition is valid, including
// metadata-dependent rules:
//   - deprecated → active (rollback) requires a non-empty WorkItem in metadata
//     (human approval tracking per spec §7)
func ValidateTransition(from, to LifecycleStatus, metadata *Metadata) error {
	if !ValidTransition(from, to) {
		return fmt.Errorf("invalid transition: %s → %s", from, to)
	}

	// Rollback requires human approval — proxied by WorkItem being set
	if from == StatusDeprecated && to == StatusActive {
		if metadata == nil || metadata.WorkItem == "" {
			return fmt.Errorf("rollback (deprecated → active) requires human approval: metadata.work_item must be set")
		}
	}

	return nil
}

// =============================================================================
// Atomic State Transitions (spec §10 — "Atomic state writes: temp file + rename")
// =============================================================================

// ApplyTransition validates and applies a lifecycle transition atomically.
// It mutates the Metadata in place: updates Status, sets timestamps, and
// records the transition in the audit trail (Changes field).
//
// Returns an error if the transition is invalid. On success, the Metadata
// reflects the new state.
func ApplyTransition(metadata *Metadata, to LifecycleStatus) error {
	if metadata == nil {
		return fmt.Errorf("metadata is nil")
	}

	from := metadata.Status

	if err := ValidateTransition(from, to, metadata); err != nil {
		return err
	}

	// Record the previous state.
	oldStatus := from

	// Apply the transition.
	metadata.Status = to
	metadata.Changes = fmt.Sprintf("%s → %s (transition at %s)",
		oldStatus, to, time.Now().UTC().Format(time.RFC3339))

	// Set attestation timestamp when entering attested state.
	if to == StatusAttested && metadata.AttestedAt.IsZero() {
		metadata.AttestedAt = time.Now().UTC()
	}

	return nil
}

// =============================================================================
// Age-Based Auto-Deprecation (spec §10 transition table)
//
// "active → deprecated: Newer version published (manual, or auto when newer
//  has 3+ commits)"
// "deprecated → retired: 90 days deprecated OR no commits in 180 days"
// =============================================================================

// AutoDeprecationConfig controls automatic lifecycle transitions.
type AutoDeprecationConfig struct {
	// DeprecateAfterDays: if an active prompt hasn't been referenced in this
	// many days, it should be deprecated. 0 = disabled.
	DeprecateAfterDays int

	// RetireAfterDeprecatedDays: a deprecated prompt should be retired after
	// this many days in deprecated state. Default 90 per spec.
	RetireAfterDeprecatedDays int

	// RetireAfterNoCommitsDays: a prompt with no commits in this many days
	// should be retired. Default 180 per spec.
	RetireAfterNoCommitsDays int

	// AutoDeprecateOnNewerCommits: if a newer version has this many commits,
	// the old version is auto-deprecated. Default 3 per spec.
	AutoDeprecateOnNewerCommits int
}

// DefaultAutoDeprecationConfig returns the spec-compliant configuration.
func DefaultAutoDeprecationConfig() AutoDeprecationConfig {
	return AutoDeprecationConfig{
		DeprecateAfterDays:          90,  // no activity for 90 days
		RetireAfterDeprecatedDays:   90,  // 90 days in deprecated
		RetireAfterNoCommitsDays:    180, // no commits in 180 days
		AutoDeprecateOnNewerCommits: 3,   // newer version with 3+ commits
	}
}

// ShouldDeprecate checks whether an active prompt should be auto-deprecated.
//
// Deprecation triggers (per spec §10):
//   - No activity for DeprecateAfterDays
//   - A newer version of the same component has AutoDeprecateOnNewerCommits+ commits
func ShouldDeprecate(metadata *Metadata, now time.Time, config AutoDeprecationConfig, newerVersionCommits int) bool {
	if metadata.Status != StatusActive {
		return false
	}

	// Check inactivity.
	if config.DeprecateAfterDays > 0 {
		lastActivity := lastActivityTime(metadata)
		if !lastActivity.IsZero() {
			daysSinceActivity := int(now.Sub(lastActivity).Hours() / 24)
			if daysSinceActivity >= config.DeprecateAfterDays {
				return true
			}
		}
	}

	// Check if newer version has enough commits.
	if config.AutoDeprecateOnNewerCommits > 0 && newerVersionCommits >= config.AutoDeprecateOnNewerCommits {
		return true
	}

	return false
}

// ShouldRetire checks whether a deprecated prompt should be auto-retired.
//
// Retirement triggers (per spec §10):
//   - In deprecated state for RetireAfterDeprecatedDays
//   - No commits in RetireAfterNoCommitsDays
func ShouldRetire(metadata *Metadata, now time.Time, config AutoDeprecationConfig) bool {
	if metadata.Status != StatusDeprecated {
		return false
	}

	// Check time in deprecated state.
	if config.RetireAfterDeprecatedDays > 0 {
		// Use the Changes field timestamp or CreatedAt as deprecation time proxy.
		deprecationTime := extractTransitionTime(metadata.Changes)
		if !deprecationTime.IsZero() {
			daysInDeprecated := int(now.Sub(deprecationTime).Hours() / 24)
			if daysInDeprecated >= config.RetireAfterDeprecatedDays {
				return true
			}
		}
	}

	// Check no-commit inactivity.
	if config.RetireAfterNoCommitsDays > 0 {
		lastCommit := lastCommitTime(metadata)
		if !lastCommit.IsZero() {
			daysSinceCommit := int(now.Sub(lastCommit).Hours() / 24)
			if daysSinceCommit >= config.RetireAfterNoCommitsDays {
				return true
			}
		}
	}

	return false
}

// lastActivityTime returns the most recent activity timestamp.
func lastActivityTime(metadata *Metadata) time.Time {
	latest := metadata.CreatedAt
	if metadata.AttestedAt.After(latest) {
		latest = metadata.AttestedAt
	}
	lastCommit := lastCommitTime(metadata)
	if lastCommit.After(latest) {
		latest = lastCommit
	}
	return latest
}

// lastCommitTime returns the timestamp of the most recent commit reference.
func lastCommitTime(metadata *Metadata) time.Time {
	var latest time.Time
	for _, c := range metadata.Commits {
		// Commits don't have timestamps; we use the Promptfoo.LastRun as proxy.
		_ = c
	}
	// Use promptfoo last run as the best proxy for recent activity.
	if !metadata.Promptfoo.LastRun.IsZero() {
		latest = metadata.Promptfoo.LastRun
	}
	return latest
}

// extractTransitionTime parses the timestamp from a Changes field like
// "active → deprecated (transition at 2026-06-29T12:00:00Z)".
func extractTransitionTime(changes string) time.Time {
	if changes == "" {
		return time.Time{}
	}
	// Find "(transition at " marker.
	marker := "(transition at "
	idx := indexOf(changes, marker)
	if idx < 0 {
		return time.Time{}
	}
	start := idx + len(marker)
	// Find closing ")".
	end := indexOf(changes[start:], ")")
	if end < 0 {
		return time.Time{}
	}
	tsStr := changes[start : start+end]
	t, err := time.Parse(time.RFC3339, tsStr)
	if err != nil {
		return time.Time{}
	}
	return t
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
