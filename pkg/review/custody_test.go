package review

import (
	"crypto/ed25519"
	"strings"
	"testing"
)

// =============================================================================
// Test helpers
// =============================================================================

func makeBundleForCustody(t *testing.T) (*EvidenceBundle, map[string]ed25519.PublicKey, map[string]ed25519.PrivateKey) {
	t.Helper()
	pubP, privP, _ := GenerateKeyPair()
	pubA, privA, _ := GenerateKeyPair()
	pubU, privU, _ := GenerateKeyPair()

	formation := Formation{
		Primary:     ModelInfo{Model: "glm-5.2", Provider: "zai"},
		Adversarial: ModelInfo{Model: "deepseek-v4", Provider: "deepseek"},
		Audit:       ModelInfo{Model: "minimax-m3", Provider: "minimax"},
	}

	bundle := NewEvidenceBundle(
		"https://forgejo.example.com/org/repo/pulls/99",
		"custody-review-001",
		formation,
		"abc123stripped",
		"def456original",
	)

	bundle.AddFinding(Finding{
		Model: "glm-5.2", Severity: "high", Type: "security",
		File: "auth.go", Line: 42, Description: "SQL injection",
		Evidence: "user input concatenated into query",
	})

	keys := map[string]ed25519.PublicKey{
		"primary":     pubP,
		"adversarial": pubA,
		"audit":       pubU,
	}

	privKeys := map[string]ed25519.PrivateKey{
		"primary":     privP,
		"adversarial": privA,
		"audit":       privU,
	}

	return bundle, keys, privKeys
}

func makeSignedBundleForCustody(t *testing.T) (*EvidenceBundle, map[string]ed25519.PublicKey, map[string]ed25519.PrivateKey) {
	t.Helper()
	bundle, pubKeys, privKeys := makeBundleForCustody(t)

	for role, priv := range privKeys {
		_, _ = bundle.SignBundle(role, priv)
	}
	return bundle, pubKeys, privKeys
}

// =============================================================================
// NewChainOfCustody
// =============================================================================

func TestNewChainOfCustody_CreationEvent(t *testing.T) {
	bundle := NewEvidenceBundle("pr-url", "rev-1", Formation{}, "abc", "def")
	chain := NewChainOfCustody(bundle)

	if chain.ReviewID != "rev-1" {
		t.Errorf("expected review_id=rev-1, got %s", chain.ReviewID)
	}
	if len(chain.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(chain.Events))
	}
	if chain.Events[0].Type != CustodyCreated {
		t.Errorf("expected first event type=created, got %s", chain.Events[0].Type)
	}
	if chain.Events[0].SeqNum != 1 {
		t.Errorf("expected seq=1, got %d", chain.Events[0].SeqNum)
	}
	if chain.LastHash == "" {
		t.Error("last_hash should be non-empty")
	}
	if len(chain.SignedRoles) != 0 {
		t.Error("signed_roles should be empty at creation")
	}
}

// =============================================================================
// RecordSigning
// =============================================================================

func TestRecordSigning_AddsEvent(t *testing.T) {
	bundle, _, _ := makeSignedBundleForCustody(t)
	chain := NewChainOfCustody(bundle)

	chain.RecordSigning("primary", "glm-5.2", bundle)

	if len(chain.Events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(chain.Events))
	}
	if chain.Events[1].Type != CustodySigned {
		t.Errorf("expected type=signed, got %s", chain.Events[1].Type)
	}
	if chain.Events[1].Role != "primary" {
		t.Errorf("expected role=primary, got %s", chain.Events[1].Role)
	}
	if !sliceContains(chain.SignedRoles, "primary") {
		t.Error("primary should be in signed_roles")
	}
}

func TestRecordSigning_AllThreeRoles(t *testing.T) {
	bundle, _, _ := makeSignedBundleForCustody(t)
	chain := NewChainOfCustody(bundle)

	chain.RecordSigning("primary", "glm-5.2", bundle)
	chain.RecordSigning("adversarial", "deepseek", bundle)
	chain.RecordSigning("audit", "minimax", bundle)

	if len(chain.SignedRoles) != 3 {
		t.Errorf("expected 3 signed roles, got %d", len(chain.SignedRoles))
	}
	if len(chain.Events) != 4 {
		t.Errorf("expected 4 events (1 create + 3 sign), got %d", len(chain.Events))
	}
}

// =============================================================================
// RecordVerification
// =============================================================================

func TestRecordVerification_PassAndFail(t *testing.T) {
	bundle, _, _ := makeSignedBundleForCustody(t)
	chain := NewChainOfCustody(bundle)

	chain.RecordVerification("primary", "auditor", true, bundle)
	chain.RecordVerification("adversarial", "auditor", false, bundle)

	if len(chain.Events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(chain.Events))
	}
	if !chain.Events[1].Verified {
		t.Error("first verification should be true")
	}
	if chain.Events[2].Verified {
		t.Error("second verification should be false")
	}
}

// =============================================================================
// RecordModification
// =============================================================================

func TestRecordModification_ChangesHash(t *testing.T) {
	bundle := NewEvidenceBundle("pr", "rev", Formation{}, "abc", "def")
	chain := NewChainOfCustody(bundle)
	initialHash := chain.LastHash

	bundle.AddFinding(Finding{
		Model: "test", Severity: "low", Type: "style",
		File: "x.go", Line: 1, Description: "test",
		Evidence: "evidence",
	})
	chain.RecordModification("agent-1", "added finding", bundle)

	if chain.LastHash == initialHash {
		t.Error("hash should change after modification")
	}
}

func TestRecordModification_NoChange_Skips(t *testing.T) {
	bundle := NewEvidenceBundle("pr", "rev", Formation{}, "abc", "def")
	chain := NewChainOfCustody(bundle)
	eventCount := len(chain.Events)

	// Record modification without actually changing the bundle
	chain.RecordModification("agent-1", "no change", bundle)

	if len(chain.Events) != eventCount {
		t.Error("should not add event when no change occurred")
	}
}

// =============================================================================
// RecordAddFinding
// =============================================================================

func TestRecordAddFinding(t *testing.T) {
	bundle := NewEvidenceBundle("pr", "rev", Formation{}, "abc", "def")
	chain := NewChainOfCustody(bundle)

	finding := Finding{Model: "m", Severity: "high", Type: "bug", File: "f.go", Line: 10, Description: "d", Evidence: "e"}
	bundle.AddFinding(finding)
	chain.RecordAddFinding("agent-1", finding, bundle)

	if len(chain.Events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(chain.Events))
	}
	if chain.Events[1].Type != CustodyAddFinding {
		t.Errorf("expected finding_added, got %s", chain.Events[1].Type)
	}
}

// =============================================================================
// RecordSetConsensus
// =============================================================================

func TestRecordSetConsensus(t *testing.T) {
	bundle := NewEvidenceBundle("pr", "rev", Formation{}, "abc", "def")
	chain := NewChainOfCustody(bundle)

	bundle.SetConsensus("approved", "approved", "approved")
	chain.RecordSetConsensus("orchestrator", "approved", bundle)

	if len(chain.Events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(chain.Events))
	}
	if chain.Events[1].Type != CustodySetConsensus {
		t.Errorf("expected consensus_set, got %s", chain.Events[1].Type)
	}
}

// =============================================================================
// VerifyChain — clean chain
// =============================================================================

func TestVerifyChain_CleanChain_AllSigned(t *testing.T) {
	bundle, keys, _ := makeSignedBundleForCustody(t)
	chain := NewChainOfCustody(bundle)
	chain.RecordSigning("primary", "glm-5.2", bundle)
	chain.RecordSigning("adversarial", "deepseek", bundle)
	chain.RecordSigning("audit", "minimax", bundle)
	chain.RecordVerification("primary", "auditor", true, bundle)

	report, err := chain.VerifyChain(keys)
	if err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if !report.IsValid() {
		t.Errorf("clean chain should be valid, summary: %s", report.Summary)
	}
	if !report.IsComplete {
		t.Error("should be complete with all roles signed")
	}
	if report.IsTampered {
		t.Error("should not be tampered")
	}
}

// =============================================================================
// VerifyChain — missing signatures
// =============================================================================

func TestVerifyChain_MissingSignatures(t *testing.T) {
	bundle, keys, _ := makeSignedBundleForCustody(t)
	chain := NewChainOfCustody(bundle)
	chain.RecordSigning("primary", "glm-5.2", bundle)
	// Only primary signed — adversarial and audit are missing

	report, err := chain.VerifyChain(keys)
	if err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if report.IsComplete {
		t.Error("should be incomplete with missing signatures")
	}
	if len(report.MissingSignatures) != 2 {
		t.Errorf("expected 2 missing signatures, got %d", len(report.MissingSignatures))
	}
}

// =============================================================================
// VerifyChain — tampered (modification after signing without re-sign)
// =============================================================================

func TestVerifyChain_ModifiedAfterSign_Detected(t *testing.T) {
	bundle, keys, _ := makeSignedBundleForCustody(t)
	chain := NewChainOfCustody(bundle)
	chain.RecordSigning("primary", "glm-5.2", bundle)
	chain.RecordSigning("adversarial", "deepseek", bundle)
	chain.RecordSigning("audit", "minimax", bundle)

	// Simulate modification after signing (no re-sign)
	bundle.AddFinding(Finding{
		Model: "hacker", Severity: "critical", Type: "injected",
		File: "exploit.go", Line: 1, Description: "tampered",
		Evidence: "malicious",
	})
	chain.RecordModification("attacker", "injected finding", bundle)
	// NO re-signing event

	report, err := chain.VerifyChain(keys)
	if err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if !report.IsTampered {
		t.Error("should detect modification after signing without re-sign")
	}
	if !report.ShouldBlockMerge() {
		t.Error("tampered chain should block merge")
	}
}

func TestVerifyChain_ModifiedThenReSigned_OK(t *testing.T) {
	bundle, keys, _ := makeSignedBundleForCustody(t)
	chain := NewChainOfCustody(bundle)
	chain.RecordSigning("primary", "glm-5.2", bundle)
	chain.RecordSigning("adversarial", "deepseek", bundle)
	chain.RecordSigning("audit", "minimax", bundle)

	// Modify and re-sign
	bundle.AddFinding(Finding{
		Model: "late", Severity: "medium", Type: "edge_case",
		File: "edge.go", Line: 5, Description: "missed case",
		Evidence: "found late",
	})
	chain.RecordModification("reviewer", "added late finding", bundle)

	// Re-sign all roles
	_, _, privKeys := makeSignedBundleForCustody(t) // dummy for priv keys
	for role, priv := range privKeys {
		_, _ = bundle.SignBundle(role, priv)
		chain.RecordReSigning(role, role+"-model", bundle)
	}

	report, err := chain.VerifyChain(keys)
	if err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if report.IsTampered {
		t.Errorf("should not be tampered after re-sign, details: %v", report.TamperDetails)
	}
}

// =============================================================================
// VerifyChain — verification failure
// =============================================================================

func TestVerifyChain_VerificationFailed_Flagged(t *testing.T) {
	bundle, keys, _ := makeSignedBundleForCustody(t)
	chain := NewChainOfCustody(bundle)
	chain.RecordSigning("primary", "glm-5.2", bundle)
	chain.RecordSigning("adversarial", "deepseek", bundle)
	chain.RecordSigning("audit", "minimax", bundle)
	chain.RecordVerification("primary", "auditor", false, bundle) // failed

	report, err := chain.VerifyChain(keys)
	if err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if !report.IsTampered {
		t.Error("verification failure should flag as tampered")
	}
}

// =============================================================================
// CustodyReport methods
// =============================================================================

func TestCustodyReport_IsValid_True(t *testing.T) {
	r := &CustodyReport{IsComplete: true, IsTampered: false}
	if !r.IsValid() {
		t.Error("complete + not tampered should be valid")
	}
}

func TestCustodyReport_IsValid_Incomplete(t *testing.T) {
	r := &CustodyReport{IsComplete: false, IsTampered: false}
	if r.IsValid() {
		t.Error("incomplete should be invalid")
	}
}

func TestCustodyReport_IsValid_Tampered(t *testing.T) {
	r := &CustodyReport{IsComplete: true, IsTampered: true}
	if r.IsValid() {
		t.Error("tampered should be invalid")
	}
}

func TestCustodyReport_ShouldBlockMerge(t *testing.T) {
	r := &CustodyReport{IsTampered: true}
	if !r.ShouldBlockMerge() {
		t.Error("tampered should block merge")
	}
}

func TestCustodyReport_ShouldBlockMerge_Clean(t *testing.T) {
	r := &CustodyReport{IsTampered: false}
	if r.ShouldBlockMerge() {
		t.Error("clean should not block merge")
	}
}

// =============================================================================
// FormatReport
// =============================================================================

func TestFormatReport_ContainsSummary(t *testing.T) {
	report := &CustodyReport{
		ReviewID:    "rev-1",
		TotalEvents: 5,
		Summary:     "VALID: chain of custody intact",
		IsComplete:  true,
		IsTampered:  false,
		SignedRoles: []string{"primary", "adversarial"},
		EventResults: []CustodyEventResult{
			{SeqNum: 1, Type: CustodyCreated, Actor: "system", Status: CustodyOK, Note: "created"},
		},
	}
	output := report.FormatReport()
	if !strings.Contains(output, "VALID") {
		t.Error("report should contain VALID in output")
	}
	if !strings.Contains(output, "rev-1") {
		t.Error("report should contain review ID")
	}
}

func TestFormatReport_Tampered(t *testing.T) {
	report := &CustodyReport{
		ReviewID:      "rev-1",
		IsTampered:    true,
		TamperDetails: []string{"modification after signing"},
		Summary:       "TAMPERED: 1 issues detected",
		EventResults: []CustodyEventResult{
			{SeqNum: 1, Type: CustodyCreated, Actor: "system", Status: CustodyOK},
		},
	}
	output := report.FormatReport()
	if !strings.Contains(output, "TAMPERED") {
		t.Error("should contain TAMPERED")
	}
}

// =============================================================================
// CustodyStore integration
// =============================================================================

func TestCustodyStore_InitChain(t *testing.T) {
	store := makeTestStore(t)
	cs := NewCustodyStore(store)
	bundle := NewEvidenceBundle("pr", "rev-1", Formation{}, "abc", "def")

	chain, err := cs.InitChain(bundle)
	if err != nil {
		t.Fatalf("InitChain: %v", err)
	}
	if chain == nil {
		t.Fatal("chain should not be nil")
	}
	if chain.ReviewID != "rev-1" {
		t.Errorf("expected review_id=rev-1, got %s", chain.ReviewID)
	}
}

func TestCustodyStore_TrackSigning(t *testing.T) {
	store := makeTestStore(t)
	cs := NewCustodyStore(store)
	bundle := NewEvidenceBundle("pr", "rev-1", Formation{}, "abc", "def")
	chain, _ := cs.InitChain(bundle)

	cs.TrackSigning(chain, "primary", "glm-5.2", bundle)

	if !sliceContains(chain.SignedRoles, "primary") {
		t.Error("primary should be signed")
	}
}

func TestCustodyStore_VerifyAndReport(t *testing.T) {
	store := makeTestStore(t)
	store.SetKeys(map[string]ed25519.PublicKey{
		"primary": makeTestKey(t),
	})
	cs := NewCustodyStore(store)
	bundle := NewEvidenceBundle("pr", "rev-1", Formation{}, "abc", "def")
	chain, _ := cs.InitChain(bundle)
	chain.RecordSigning("primary", "glm-5.2", bundle)

	report, err := cs.VerifyAndReport("rev-1", chain)
	if err != nil {
		t.Fatalf("VerifyAndReport: %v", err)
	}
	if report == nil {
		t.Fatal("report should not be nil")
	}
}

func makeTestKey(t *testing.T) ed25519.PublicKey {
	t.Helper()
	pub, _, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	return pub
}

// =============================================================================
// Event ordering and seq numbers
// =============================================================================

func TestEventSeqNumbers_Sequential(t *testing.T) {
	bundle := NewEvidenceBundle("pr", "rev", Formation{}, "abc", "def")
	chain := NewChainOfCustody(bundle)
	chain.RecordSigning("primary", "a", bundle)
	chain.RecordVerification("primary", "b", true, bundle)
	chain.RecordModification("c", "mod", bundle)

	for i, evt := range chain.Events {
		if evt.SeqNum != i+1 {
			t.Errorf("event %d: expected seq=%d, got %d", i, i+1, evt.SeqNum)
		}
	}
}

// =============================================================================
// Empty / edge cases
// =============================================================================

func TestVerifyChain_NoKeys_AllComplete(t *testing.T) {
	bundle := NewEvidenceBundle("pr", "rev", Formation{}, "abc", "def")
	chain := NewChainOfCustody(bundle)

	report, err := chain.VerifyChain(nil)
	if err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if !report.IsComplete {
		t.Error("no keys → no missing signatures → should be complete")
	}
}

func TestVerifyChain_ModificationBeforeSigning_OK(t *testing.T) {
	bundle := NewEvidenceBundle("pr", "rev", Formation{}, "abc", "def")
	chain := NewChainOfCustody(bundle)

	// Modify before any signing — this is fine
	bundle.AddFinding(Finding{Model: "m", Severity: "low", Type: "t", File: "f", Line: 1, Description: "d", Evidence: "e"})
	chain.RecordModification("author", "added finding", bundle)

	// Now sign
	chain.RecordSigning("primary", "glm", bundle)

	report, err := chain.VerifyChain(nil)
	if err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if report.IsTampered {
		t.Error("modification before signing should not be tampered")
	}
}
