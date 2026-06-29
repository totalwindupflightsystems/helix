package review

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

// =============================================================================
// EvidenceBundle — per spec §Evidence Bundles
// =============================================================================

type EvidenceBundle struct {
	PRURL            string    `json:"pr_url"`
	ReviewID         string    `json:"review_id"`
	Timestamp        time.Time `json:"timestamp"`
	Formation        Formation `json:"formation"`
	BiasStrippedSHA  string    `json:"bias_stripped_commit"`
	OriginalCommit   string    `json:"original_commit"`
	Findings         []Finding `json:"findings"`
	Consensus        Consensus `json:"consensus"`
	Signatures       `json:"signatures"`
	PreviousBundleID string `json:"previous_bundle_id,omitempty"`
}

type Formation struct {
	Primary     ModelInfo `json:"primary"`
	Adversarial ModelInfo `json:"adversarial"`
	Audit       ModelInfo `json:"audit"`
}

type ModelInfo struct {
	Model    string `json:"model"`
	Provider string `json:"provider"`
}

type Finding struct {
	Model       string `json:"model"`
	Severity    string `json:"severity"`
	Type        string `json:"type"`
	File        string `json:"file"`
	Line        int    `json:"line"`
	Description string `json:"description"`
	Evidence    string `json:"evidence"`
	Mitigation  string `json:"mitigation,omitempty"`
}

type Consensus struct {
	PrimaryVerdict     string `json:"primary_verdict"`
	AdversarialVerdict string `json:"adversarial_verdict"`
	AuditVerdict       string `json:"audit_verdict"`
	Resolution         string `json:"resolution"`
	TieBreaker         string `json:"tie_breaker"`
}

type Signatures struct {
	Primary     string `json:"primary,omitempty"`
	Adversarial string `json:"adversarial,omitempty"`
	Audit       string `json:"audit,omitempty"`
}

const (
	ResolutionApproved = "approved"
	ResolutionBlocked  = "blocked"
	ResolutionTieBreak = "tie_breaker"
)

const (
	VerdictApproved           = "approved"
	VerdictPassWithNotes      = "pass_with_notes"
	VerdictBlock              = "block"
	VerdictConfirmAdversarial = "confirm_adversarial"
	VerdictOverrule           = "overrule"
)

// =============================================================================
// Bundle creation
// =============================================================================

func NewEvidenceBundle(prURL, reviewID string, formation Formation, biasStrippedSHA, originalCommit string) *EvidenceBundle {
	return &EvidenceBundle{
		PRURL:           prURL,
		ReviewID:        reviewID,
		Timestamp:       time.Now().UTC(),
		Formation:       formation,
		BiasStrippedSHA: biasStrippedSHA,
		OriginalCommit:  originalCommit,
		Findings:        []Finding{},
	}
}

func (b *EvidenceBundle) AddFinding(f Finding) {
	b.Findings = append(b.Findings, f)
}

// SetConsensus sets verdicts and resolves the outcome.
func (b *EvidenceBundle) SetConsensus(primary, adversarial, audit string) {
	b.Consensus = Consensus{
		PrimaryVerdict:     primary,
		AdversarialVerdict: adversarial,
		AuditVerdict:       audit,
		Resolution:         resolveConsensus(primary, adversarial, audit),
	}
}

// resolveConsensus determines the overall resolution.
//
// Rules:
//   - 3-model: audit overrule → approved; audit confirm → blocked
//   - 3-model contract changes require 3/3 — any block blocks
//   - 2-model (no audit): 2/2 for approval, disagreement → tie_breaker
func resolveConsensus(primary, adversarial, audit string) string {
	// ---- 2-model formation ----
	if audit == "" {
		if isApproving(primary) && isApproving(adversarial) {
			return ResolutionApproved
		}
		if primary == VerdictBlock && adversarial == VerdictBlock {
			return ResolutionBlocked
		}
		return ResolutionTieBreak
	}

	// ---- 3-model formation ----
	if audit == VerdictOverrule {
		return ResolutionApproved
	}
	if audit == VerdictConfirmAdversarial {
		return ResolutionBlocked
	}
	// All three approve
	if isApproving(primary) && isApproving(adversarial) && isApproving(audit) {
		return ResolutionApproved
	}
	// Contract changes: adversarial block blocks
	if isApproving(primary) && adversarial == VerdictBlock {
		return ResolutionBlocked
	}
	if primary == VerdictBlock && isApproving(adversarial) {
		return ResolutionTieBreak
	}
	if primary == VerdictBlock && adversarial == VerdictBlock {
		return ResolutionBlocked
	}
	return ResolutionTieBreak
}

func isApproving(v string) bool {
	return v == VerdictApproved || v == VerdictPassWithNotes
}

// Consensus helpers
func (c *Consensus) IsApproved() bool      { return c.Resolution == ResolutionApproved }
func (c *Consensus) IsBlocked() bool       { return c.Resolution == ResolutionBlocked }
func (c *Consensus) NeedsTieBreaker() bool { return c.Resolution == ResolutionTieBreak }

// =============================================================================
// ED25519 signing
// =============================================================================

func GenerateKeyPair() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	return ed25519.GenerateKey(rand.Reader)
}

// SignBundle signs the canonical hash of the bundle's content (excluding
// signatures) and stores the hex-encoded signature.
func (b *EvidenceBundle) SignBundle(role string, priv ed25519.PrivateKey) (string, error) {
	hash, err := b.contentHash()
	if err != nil {
		return "", fmt.Errorf("content hash: %w", err)
	}
	sig := ed25519.Sign(priv, []byte(hash))
	sigHex := hex.EncodeToString(sig)
	switch role {
	case "primary":
		b.Signatures.Primary = sigHex
	case "adversarial":
		b.Signatures.Adversarial = sigHex
	case "audit":
		b.Signatures.Audit = sigHex
	default:
		return "", fmt.Errorf("unknown role %q", role)
	}
	return sigHex, nil
}

// VerifySignature checks the stored signature for the given role.
func (b *EvidenceBundle) VerifySignature(role string, pub ed25519.PublicKey) (bool, error) {
	hash, err := b.contentHash()
	if err != nil {
		return false, fmt.Errorf("content hash: %w", err)
	}
	sigHex := ""
	switch role {
	case "primary":
		sigHex = b.Signatures.Primary
	case "adversarial":
		sigHex = b.Signatures.Adversarial
	case "audit":
		sigHex = b.Signatures.Audit
	default:
		return false, fmt.Errorf("unknown role %q", role)
	}
	if sigHex == "" {
		return false, fmt.Errorf("no signature for role %q", role)
	}
	sig, err := hex.DecodeString(sigHex)
	if err != nil {
		return false, fmt.Errorf("decode sig: %w", err)
	}
	return ed25519.Verify(pub, []byte(hash), sig), nil
}

// VerifyAllSignatures checks signatures against a map of role→public key.
func (b *EvidenceBundle) VerifyAllSignatures(keys map[string]ed25519.PublicKey) (map[string]bool, error) {
	results := make(map[string]bool)
	for role, pub := range keys {
		ok, err := b.VerifySignature(role, pub)
		if err != nil {
			results[role] = false
		} else {
			results[role] = ok
		}
	}
	return results, nil
}

// BundleHash returns the SHA-256 hex of the bundle's content (excluding signatures).
func (b *EvidenceBundle) BundleHash() string {
	h, err := b.contentHash()
	if err != nil {
		return ""
	}
	return h
}

// BundleID returns a 16-char hex prefix of the bundle hash.
func (b *EvidenceBundle) BundleID() string {
	h := b.BundleHash()
	if len(h) >= 16 {
		return h[:16]
	}
	return h
}

// contentHash computes SHA-256 of the deterministic canonical JSON
// representation of the bundle's CONTENT (excluding signatures).
func (b *EvidenceBundle) contentHash() (string, error) {
	data := b.canonicalContent()
	jsonBytes, err := canonicalMarshal(data)
	if err != nil {
		return "", fmt.Errorf("canonical marshal: %w", err)
	}
	sum := sha256.Sum256(jsonBytes)
	return hex.EncodeToString(sum[:]), nil
}

// canonicalContent builds the map of fields to hash — signatures are excluded
// so that signing does not change the signed data.
func (b *EvidenceBundle) canonicalContent() map[string]interface{} {
	var findings []map[string]interface{}
	for _, f := range b.Findings {
		fm := map[string]interface{}{
			"model": f.Model, "severity": f.Severity, "type": f.Type,
			"file": f.File, "line": f.Line,
			"description": f.Description, "evidence": f.Evidence,
		}
		if f.Mitigation != "" {
			fm["mitigation"] = f.Mitigation
		}
		findings = append(findings, fm)
	}
	if findings == nil {
		findings = []map[string]interface{}{}
	}

	return map[string]interface{}{
		"pr_url":    b.PRURL,
		"review_id": b.ReviewID,
		"timestamp": b.Timestamp.UTC().Format(time.RFC3339Nano),
		"formation": map[string]interface{}{
			"primary": map[string]interface{}{
				"model": b.Formation.Primary.Model, "provider": b.Formation.Primary.Provider,
			},
			"adversarial": map[string]interface{}{
				"model": b.Formation.Adversarial.Model, "provider": b.Formation.Adversarial.Provider,
			},
			"audit": map[string]interface{}{
				"model": b.Formation.Audit.Model, "provider": b.Formation.Audit.Provider,
			},
		},
		"bias_stripped_commit": b.BiasStrippedSHA,
		"original_commit":      b.OriginalCommit,
		"findings":             findings,
		"consensus": map[string]interface{}{
			"primary_verdict":     b.Consensus.PrimaryVerdict,
			"adversarial_verdict": b.Consensus.AdversarialVerdict,
			"audit_verdict":       b.Consensus.AuditVerdict,
			"resolution":          b.Consensus.Resolution,
			"tie_breaker":         b.Consensus.TieBreaker,
		},
	}
}

// canonicalMarshal recursively marshals with sorted map keys for deterministic output.
func canonicalMarshal(v interface{}) ([]byte, error) {
	switch m := v.(type) {
	case map[string]interface{}:
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		buf := []byte{'{'}
		for i, k := range keys {
			if i > 0 {
				buf = append(buf, ',')
			}
			kb, _ := json.Marshal(k)
			buf = append(buf, kb...)
			buf = append(buf, ':')
			vb, err := canonicalMarshal(m[k])
			if err != nil {
				return nil, err
			}
			buf = append(buf, vb...)
		}
		buf = append(buf, '}')
		return buf, nil
	case []interface{}:
		buf := []byte{'['}
		for i, item := range m {
			if i > 0 {
				buf = append(buf, ',')
			}
			vb, err := canonicalMarshal(item)
			if err != nil {
				return nil, err
			}
			buf = append(buf, vb...)
		}
		buf = append(buf, ']')
		return buf, nil
	default:
		return json.Marshal(v)
	}
}
