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
	StatusRetired:    {},                             // terminal
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
