package review

import (
	"crypto/ed25519"
	"fmt"
	"sort"
	"strings"
	"time"
)

// =============================================================================
// ChainOfCustody — tracks the full lifecycle of an evidence bundle.
//
// Spec §Evidence Bundles: "stored in DuckBrain and linked from the merge commit"
// — chain-of-custody ensures that any modification to a bundle after creation
// is tracked and auditable. Tampering with signed content is detected.
// =============================================================================

// CustodyEventType describes what happened to the bundle.
type CustodyEventType string

const (
	CustodyCreated      CustodyEventType = "created"
	CustodySigned       CustodyEventType = "signed"
	CustodyVerified     CustodyEventType = "verified"
	CustodyModified     CustodyEventType = "modified"
	CustodyAddFinding   CustodyEventType = "finding_added"
	CustodySetConsensus CustodyEventType = "consensus_set"
	CustodyReSigned     CustodyEventType = "re_signed"
)

// CustodyEvent records one event in the chain of custody.
type CustodyEvent struct {
	SeqNum     int              `json:"seq_num"`
	Type       CustodyEventType `json:"type"`
	Timestamp  time.Time        `json:"timestamp"`
	Actor      string           `json:"actor"`          // model ID, agent ID, or "system"
	Role       string           `json:"role,omitempty"` // primary/adversarial/audit
	BundleHash string           `json:"bundle_hash"`    // content hash at this event
	Details    string           `json:"details,omitempty"`
	Verified   bool             `json:"verified,omitempty"`
}

// ChainOfCustody tracks the full lifecycle of an evidence bundle.
type ChainOfCustody struct {
	ReviewID    string         `json:"review_id"`
	Events      []CustodyEvent `json:"events"`
	CreatedAt   time.Time      `json:"created_at"`
	LastHash    string         `json:"last_hash"`    // content hash at last event
	SignedRoles []string       `json:"signed_roles"` // roles that have signed
}

// NewChainOfCustody creates a new chain for a bundle at creation time.
func NewChainOfCustody(bundle *EvidenceBundle) *ChainOfCustody {
	hash := bundle.BundleHash()
	return &ChainOfCustody{
		ReviewID:    bundle.ReviewID,
		CreatedAt:   time.Now().UTC(),
		LastHash:    hash,
		SignedRoles: []string{},
		Events: []CustodyEvent{
			{
				SeqNum:     1,
				Type:       CustodyCreated,
				Timestamp:  time.Now().UTC(),
				Actor:      "system",
				BundleHash: hash,
				Details:    fmt.Sprintf("PR=%s, findings=%d", bundle.PRURL, len(bundle.Findings)),
			},
		},
	}
}

// RecordSigning records that a role has signed the bundle.
func (c *ChainOfCustody) RecordSigning(role, actor string, bundle *EvidenceBundle) {
	c.addEvent(CustodySigned, actor, role, bundle,
		fmt.Sprintf("role=%s signed bundle content", role))
	if !sliceContains(c.SignedRoles, role) {
		c.SignedRoles = append(c.SignedRoles, role)
	}
}

// RecordVerification records a signature verification attempt.
func (c *ChainOfCustody) RecordVerification(role, actor string, verified bool, bundle *EvidenceBundle) {
	c.addEvent(CustodyVerified, actor, role, bundle,
		fmt.Sprintf("verification %v for role=%s", verified, role))
	// Set the verified field on the last event
	c.Events[len(c.Events)-1].Verified = verified
}

// RecordModification records a change to the bundle content (adding finding, etc.).
func (c *ChainOfCustody) RecordModification(actor, description string, bundle *EvidenceBundle) {
	oldHash := c.LastHash
	newHash := bundle.BundleHash()
	if oldHash == newHash {
		return // no actual change
	}
	c.addEvent(CustodyModified, actor, "", bundle,
		fmt.Sprintf("%s (hash: %s → %s)", description, oldHash[:8], newHash[:8]))
}

// RecordAddFinding records that a finding was added to the bundle.
func (c *ChainOfCustody) RecordAddFinding(actor string, finding Finding, bundle *EvidenceBundle) {
	c.addEvent(CustodyAddFinding, actor, "", bundle,
		fmt.Sprintf("finding: %s in %s:%d", finding.Type, finding.File, finding.Line))
}

// RecordSetConsensus records that consensus was set.
func (c *ChainOfCustody) RecordSetConsensus(actor string, resolution string, bundle *EvidenceBundle) {
	c.addEvent(CustodySetConsensus, actor, "", bundle,
		fmt.Sprintf("consensus resolution: %s", resolution))
}

// RecordReSigning records that the bundle was re-signed after modification.
func (c *ChainOfCustody) RecordReSigning(role, actor string, bundle *EvidenceBundle) {
	c.addEvent(CustodyReSigned, actor, role, bundle,
		fmt.Sprintf("role=%s re-signed after modification", role))
}

// =============================================================================
// Verification
// =============================================================================

// VerifyChain checks that no tampering occurred since the last valid signature.
// Tampering = the content hash changed after a signing event without a
// corresponding re-signing event.
func (c *ChainOfCustody) VerifyChain(keys map[string]ed25519.PublicKey) (*CustodyReport, error) {
	report := &CustodyReport{
		ReviewID:     c.ReviewID,
		TotalEvents:  len(c.Events),
		SignedRoles:  append([]string{}, c.SignedRoles...),
		IsComplete:   true,
		IsTampered:   false,
		EventResults: make([]CustodyEventResult, 0),
	}

	// Track hash transitions: after a signing event, the hash should not change
	// until a modification event (which requires re-signing).
	lastSignedHash := ""

	for _, evt := range c.Events {
		result := CustodyEventResult{
			SeqNum: evt.SeqNum,
			Type:   evt.Type,
			Actor:  evt.Actor,
			Status: CustodyOK,
		}

		switch evt.Type {
		case CustodyCreated:
			result.Note = "bundle created"

		case CustodySigned, CustodyReSigned:
			lastSignedHash = evt.BundleHash
			result.Note = fmt.Sprintf("%s signed at hash %s", evt.Role, evt.BundleHash[:8])
			if evt.Type == CustodyReSigned {
				// Re-signing clears the "modified since last sign" flag
				result.Note = fmt.Sprintf("%s re-signed after modification", evt.Role)
			}

		case CustodyVerified:
			result.Verified = evt.Verified
			if !evt.Verified {
				report.IsTampered = true
				report.TamperDetails = append(report.TamperDetails,
					fmt.Sprintf("event %d: verification failed for role=%s", evt.SeqNum, evt.Role))
				result.Status = CustodyTampered
			} else {
				result.Note = fmt.Sprintf("verification passed for role=%s", evt.Role)
			}

		case CustodyModified, CustodyAddFinding, CustodySetConsensus:
			if lastSignedHash != "" && evt.BundleHash != lastSignedHash {
				// Hash changed after signing — check if there's a subsequent re-sign
				hasResign := c.hasNextReSign(evt.SeqNum)
				if !hasResign {
					report.IsTampered = true
					report.TamperDetails = append(report.TamperDetails,
						fmt.Sprintf("event %d: modification after signing without re-signing", evt.SeqNum))
					result.Status = CustodyModifiedUnsigned
				} else {
					result.Note = "modification followed by re-signing"
				}
			} else {
				result.Note = "content modified before any signing"
			}

		default:
			result.Note = "unknown event type"
		}

		report.EventResults = append(report.EventResults, result)
	}

	// Check that all expected roles signed
	for role := range keys {
		if !sliceContains(c.SignedRoles, role) {
			report.IsComplete = false
			report.MissingSignatures = append(report.MissingSignatures, role)
		}
	}
	sort.Strings(report.MissingSignatures)

	if !report.IsComplete {
		report.Summary = fmt.Sprintf("INCOMPLETE: missing signatures from %s",
			strings.Join(report.MissingSignatures, ", "))
	} else if report.IsTampered {
		report.Summary = fmt.Sprintf("TAMPERED: %d issues detected", len(report.TamperDetails))
	} else {
		report.Summary = "VALID: chain of custody intact, all signatures present"
	}

	return report, nil
}

// hasNextReSign checks if there's a re-signing event after the given sequence number.
func (c *ChainOfCustody) hasNextReSign(afterSeq int) bool {
	for _, evt := range c.Events {
		if evt.SeqNum > afterSeq && evt.Type == CustodyReSigned {
			return true
		}
	}
	return false
}

// =============================================================================
// Report
// =============================================================================

// CustodyEventStatus describes the status of a single event in the chain.
type CustodyEventStatus string

const (
	CustodyOK               CustodyEventStatus = "ok"
	CustodyTampered         CustodyEventStatus = "tampered"
	CustodyModifiedUnsigned CustodyEventStatus = "modified_unsigned"
)

// CustodyEventResult is the per-event verification result.
type CustodyEventResult struct {
	SeqNum   int                `json:"seq_num"`
	Type     CustodyEventType   `json:"type"`
	Actor    string             `json:"actor"`
	Status   CustodyEventStatus `json:"status"`
	Verified bool               `json:"verified,omitempty"`
	Note     string             `json:"note,omitempty"`
}

// CustodyReport summarizes the chain-of-custody verification for audit display.
type CustodyReport struct {
	ReviewID          string               `json:"review_id"`
	TotalEvents       int                  `json:"total_events"`
	IsComplete        bool                 `json:"is_complete"`
	IsTampered        bool                 `json:"is_tampered"`
	SignedRoles       []string             `json:"signed_roles"`
	MissingSignatures []string             `json:"missing_signatures,omitempty"`
	TamperDetails     []string             `json:"tamper_details,omitempty"`
	Summary           string               `json:"summary"`
	EventResults      []CustodyEventResult `json:"event_results"`
}

// IsValid returns true if the chain is complete and untampered.
func (r *CustodyReport) IsValid() bool {
	return r.IsComplete && !r.IsTampered
}

// ShouldBlockMerge returns true if the chain indicates evidence should not be trusted.
func (r *CustodyReport) ShouldBlockMerge() bool {
	return r.IsTampered
}

// FormatReport renders a human-readable audit trail.
func (r *CustodyReport) FormatReport() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("=== Chain of Custody Report: %s ===\n", r.ReviewID))
	sb.WriteString(fmt.Sprintf("Events: %d | Signed by: [%s]\n", r.TotalEvents, strings.Join(r.SignedRoles, ", ")))

	if len(r.MissingSignatures) > 0 {
		sb.WriteString(fmt.Sprintf("Missing signatures: %s\n", strings.Join(r.MissingSignatures, ", ")))
	}
	if r.IsTampered {
		sb.WriteString(fmt.Sprintf("⚠ TAMPERED — %d issues:\n", len(r.TamperDetails)))
		for _, d := range r.TamperDetails {
			sb.WriteString(fmt.Sprintf("  • %s\n", d))
		}
	}
	sb.WriteString("\nEvent Trail:\n")
	for _, e := range r.EventResults {
		status := "✓"
		if e.Status != CustodyOK {
			status = "✗"
		}
		sb.WriteString(fmt.Sprintf("  [%d] %s %s by %s — %s\n",
			e.SeqNum, status, e.Type, e.Actor, e.Note))
	}
	sb.WriteString(fmt.Sprintf("\nSummary: %s\n", r.Summary))
	return sb.String()
}

// =============================================================================
// EvidenceStore integration
// =============================================================================

// CustodyStore manages chain-of-custody records alongside evidence bundles.
type CustodyStore struct {
	store *EvidenceStore
}

// NewCustodyStore wraps an EvidenceStore for chain-of-custody operations.
func NewCustodyStore(store *EvidenceStore) *CustodyStore {
	return &CustodyStore{store: store}
}

// InitChain creates and persists a chain-of-custody for a newly stored bundle.
func (cs *CustodyStore) InitChain(bundle *EvidenceBundle) (*ChainOfCustody, error) {
	chain := NewChainOfCustody(bundle)
	return chain, nil
}

// TrackSigning records a signing event and returns the updated chain.
func (cs *CustodyStore) TrackSigning(chain *ChainOfCustody, role, actor string, bundle *EvidenceBundle) {
	chain.RecordSigning(role, actor, bundle)
}

// VerifyAndReport loads a bundle, verifies its chain, and returns the report.
func (cs *CustodyStore) VerifyAndReport(reviewID string, chain *ChainOfCustody) (*CustodyReport, error) {
	return chain.VerifyChain(cs.store.keys)
}

// =============================================================================
// Helpers
// =============================================================================

func (c *ChainOfCustody) addEvent(typ CustodyEventType, actor, role string, bundle *EvidenceBundle, details string) {
	newHash := bundle.BundleHash()
	event := CustodyEvent{
		SeqNum:     len(c.Events) + 1,
		Type:       typ,
		Timestamp:  time.Now().UTC(),
		Actor:      actor,
		Role:       role,
		BundleHash: newHash,
		Details:    details,
	}
	c.Events = append(c.Events, event)
	c.LastHash = newHash
}

func sliceContains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
