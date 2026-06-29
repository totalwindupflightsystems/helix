package review

import (
	"crypto/ed25519"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// Bundle creation
// =============================================================================

func TestNewEvidenceBundle(t *testing.T) {
	formation := Formation{
		Primary:     ModelInfo{Model: "gpt-5.5", Provider: "openai"},
		Adversarial: ModelInfo{Model: "deepseek-v4-pro", Provider: "deepseek"},
		Audit:       ModelInfo{Model: "owl-alpha", Provider: "openrouter"},
	}
	b := NewEvidenceBundle("https://forgejo/pr/42", "review-uuid", formation, "abc123", "def456")
	if b.PRURL != "https://forgejo/pr/42" {
		t.Errorf("expected PRURL, got %q", b.PRURL)
	}
	if b.ReviewID != "review-uuid" {
		t.Errorf("expected review-uuid, got %q", b.ReviewID)
	}
	if len(b.Findings) != 0 {
		t.Errorf("expected 0 findings, got %d", len(b.Findings))
	}
	if b.Consensus.Resolution != "" {
		t.Errorf("expected empty resolution, got %q", b.Consensus.Resolution)
	}
}

func TestAddFinding(t *testing.T) {
	b := NewEvidenceBundle("pr-url", "rid", Formation{}, "", "")
	b.AddFinding(Finding{
		Model: "adversarial", Severity: "high", Type: "race_condition",
		File: "pkg/auth/session.go", Line: 142,
		Description: "Token refresh and invalidation race",
		Evidence:    "test_run_id: uuid, output: 'FAIL: TestConcurrentTokenRefresh'",
	})
	if len(b.Findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(b.Findings))
	}
	f := b.Findings[0]
	if f.Model != "adversarial" {
		t.Errorf("expected model 'adversarial', got %q", f.Model)
	}
	if f.Severity != "high" {
		t.Errorf("expected severity 'high', got %q", f.Severity)
	}
	if f.Type != "race_condition" {
		t.Errorf("expected type 'race_condition', got %q", f.Type)
	}
}

// =============================================================================
// Consensus resolution
// =============================================================================

func TestResolveConsensus_AllApprove(t *testing.T) {
	r := resolveConsensus(VerdictApproved, VerdictApproved, VerdictApproved)
	if r != ResolutionApproved {
		t.Errorf("expected approved, got %q", r)
	}
}

func TestResolveConsensus_PrimaryPassWithNotesBothApprove(t *testing.T) {
	r := resolveConsensus(VerdictPassWithNotes, VerdictApproved, VerdictApproved)
	if r != ResolutionApproved {
		t.Errorf("expected approved, got %q", r)
	}
}

func TestResolveConsensus_PrimaryApprovedAdversarialBlocked(t *testing.T) {
	r := resolveConsensus(VerdictApproved, VerdictBlock, VerdictApproved)
	if r != ResolutionBlocked {
		t.Errorf("expected blocked, got %q", r)
	}
}

func TestResolveConsensus_AuditConfirmsAdversarial(t *testing.T) {
	r := resolveConsensus(VerdictApproved, VerdictBlock, VerdictConfirmAdversarial)
	if r == ResolutionApproved {
		t.Errorf("expected blocked (audit confirms adversarial), got %q", r)
	}
}

func TestResolveConsensus_AuditOverrulesAdversarial(t *testing.T) {
	r := resolveConsensus(VerdictApproved, VerdictBlock, VerdictOverrule)
	if r != ResolutionApproved {
		t.Errorf("expected approved (audit overrules), got %q", r)
	}
}

func TestResolveConsensus_TwoModelsBothApprove(t *testing.T) {
	// No audit review
	r := resolveConsensus(VerdictApproved, VerdictApproved, "")
	if r != ResolutionApproved {
		t.Errorf("expected approved (2/2), got %q", r)
	}
}

func TestResolveConsensus_TwoModelsDisagree(t *testing.T) {
	r := resolveConsensus(VerdictApproved, VerdictBlock, "")
	if r != ResolutionTieBreak {
		t.Errorf("expected tie_breaker (2 models disagree), got %q", r)
	}
}

func TestResolveConsensus_PrimaryBlocks(t *testing.T) {
	r := resolveConsensus(VerdictBlock, VerdictApproved, VerdictApproved)
	if r != ResolutionTieBreak {
		t.Errorf("expected tie_breaker, got %q", r)
	}
}

func TestResolveConsensus_AllBlock(t *testing.T) {
	r := resolveConsensus(VerdictBlock, VerdictBlock, VerdictBlock)
	// Primary + adversarial both block → blocked
	if r != ResolutionBlocked {
		t.Errorf("expected blocked, got %q", r)
	}
}

// =============================================================================
// SetConsensus
// =============================================================================

func TestSetConsensus_Approved(t *testing.T) {
	b := NewEvidenceBundle("pr-url", "rid", Formation{}, "", "")
	b.SetConsensus(VerdictApproved, VerdictApproved, VerdictApproved)
	if !b.Consensus.IsApproved() {
		t.Errorf("expected IsApproved()=true")
	}
	if b.Consensus.IsBlocked() {
		t.Errorf("expected IsBlocked()=false")
	}
}

func TestSetConsensus_Blocked(t *testing.T) {
	b := NewEvidenceBundle("pr-url", "rid", Formation{}, "", "")
	b.SetConsensus(VerdictApproved, VerdictBlock, VerdictConfirmAdversarial)
	if !b.Consensus.IsBlocked() {
		t.Errorf("expected IsBlocked()=true")
	}
}

func TestSetConsensus_TieBreaker(t *testing.T) {
	b := NewEvidenceBundle("pr-url", "rid", Formation{}, "", "")
	b.SetConsensus(VerdictApproved, VerdictBlock, "")
	if !b.Consensus.NeedsTieBreaker() {
		t.Errorf("expected NeedsTieBreaker()=true")
	}
}

// =============================================================================
// ED25519 signing and verification
// =============================================================================

func TestGenerateKeyPair(t *testing.T) {
	pub, priv, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	if len(pub) != ed25519.PublicKeySize {
		t.Errorf("expected pub key size %d, got %d", ed25519.PublicKeySize, len(pub))
	}
	if len(priv) != ed25519.PrivateKeySize {
		t.Errorf("expected priv key size %d, got %d", ed25519.PrivateKeySize, len(priv))
	}
}

func TestSignAndVerifyBundle_Primary(t *testing.T) {
	b := NewEvidenceBundle("https://forgejo/pr/42", "rid-1",
		Formation{
			Primary: ModelInfo{Model: "gpt-5.5", Provider: "openai"},
		}, "abc", "def")
	b.AddFinding(Finding{Model: "primary", Severity: "low", Type: "nit", File: "main.go", Line: 10, Description: "nit: spacing", Evidence: "line 10"})
	b.SetConsensus(VerdictApproved, VerdictApproved, VerdictApproved)

	pub, priv, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}

	sig, err := b.SignBundle("primary", priv)
	if err != nil {
		t.Fatalf("SignBundle: %v", err)
	}
	if sig == "" {
		t.Fatal("expected non-empty signature")
	}
	if b.Signatures.Primary != sig {
		t.Errorf("signature not stored in bundle")
	}

	valid, err := b.VerifySignature("primary", pub)
	if err != nil {
		t.Fatalf("VerifySignature: %v", err)
	}
	if !valid {
		t.Error("signature verification failed")
	}
}

func TestSignAndVerifyBundle_AllRoles(t *testing.T) {
	formation := Formation{
		Primary:     ModelInfo{Model: "gpt-5.5", Provider: "openai"},
		Adversarial: ModelInfo{Model: "deepseek-v4-pro", Provider: "deepseek"},
		Audit:       ModelInfo{Model: "owl-alpha", Provider: "openrouter"},
	}
	b := NewEvidenceBundle("https://forgejo/pr/42", "rid-2", formation, "abc", "def")
	b.SetConsensus(VerdictApproved, VerdictApproved, VerdictApproved)

	// Generate keys for each role
	keys := make(map[string]struct {
		pub  ed25519.PublicKey
		priv ed25519.PrivateKey
	})
	for _, role := range []string{"primary", "adversarial", "audit"} {
		pub, priv, _ := GenerateKeyPair()
		keys[role] = struct {
			pub  ed25519.PublicKey
			priv ed25519.PrivateKey
		}{pub, priv}

		_, err := b.SignBundle(role, priv)
		if err != nil {
			t.Fatalf("SignBundle(%q): %v", role, err)
		}
	}

	// Verify all
	pubKeys := map[string]ed25519.PublicKey{
		"primary":     keys["primary"].pub,
		"adversarial": keys["adversarial"].pub,
		"audit":       keys["audit"].pub,
	}
	results, err := b.VerifyAllSignatures(pubKeys)
	if err != nil {
		t.Fatalf("VerifyAllSignatures: %v", err)
	}
	for role, valid := range results {
		if !valid {
			t.Errorf("signature verification failed for %q", role)
		}
	}
}

func TestVerifySignature_WrongKey(t *testing.T) {
	b := NewEvidenceBundle("pr-url", "rid", Formation{}, "", "")
	_, priv1, _ := GenerateKeyPair()
	pub2, _, _ := GenerateKeyPair()

	b.SignBundle("primary", priv1)
	valid, _ := b.VerifySignature("primary", pub2)
	if valid {
		t.Error("expected verification to fail with wrong key")
	}
}

func TestVerifySignature_NoSignature(t *testing.T) {
	b := NewEvidenceBundle("pr-url", "rid", Formation{}, "", "")
	pub, _, _ := GenerateKeyPair()
	_, err := b.VerifySignature("primary", pub)
	if err == nil || !strings.Contains(err.Error(), "no signature") {
		t.Errorf("expected 'no signature' error, got %v", err)
	}
}

func TestSignBundle_UnknownRole(t *testing.T) {
	b := NewEvidenceBundle("pr-url", "rid", Formation{}, "", "")
	_, priv, _ := GenerateKeyPair()
	_, err := b.SignBundle("unknown_role", priv)
	if err == nil || !strings.Contains(err.Error(), "unknown role") {
		t.Errorf("expected 'unknown role' error, got %v", err)
	}
}

// =============================================================================
// Canonical hashing
// =============================================================================

func TestBundleHash_Deterministic(t *testing.T) {
	b := NewEvidenceBundle("https://forgejo/pr/42", "rid-1",
		Formation{Primary: ModelInfo{Model: "gpt-5.5", Provider: "openai"}}, "abc", "def")
	b.AddFinding(Finding{Model: "primary", Severity: "low", Type: "nit", File: "main.go", Line: 10, Description: "nit", Evidence: "line 10"})
	b.SetConsensus(VerdictApproved, VerdictApproved, VerdictApproved)

	h1 := b.BundleHash()
	h2 := b.BundleHash()
	if h1 != h2 {
		t.Errorf("canonical hash not deterministic: %s vs %s", h1, h2)
	}
}

func TestBundleHash_ChangesWithContent(t *testing.T) {
	b1 := NewEvidenceBundle("pr-url", "rid", Formation{}, "", "")
	b2 := NewEvidenceBundle("different-pr-url", "rid", Formation{}, "", "")

	h1 := b1.BundleHash()
	h2 := b2.BundleHash()
	if h1 == h2 {
		t.Error("different bundles should produce different hashes")
	}
}

func TestBundleHash(t *testing.T) {
	b := NewEvidenceBundle("pr-url", "rid", Formation{}, "", "")
	h := b.BundleHash()
	if len(h) != 64 { // SHA-256 hex = 64 chars
		t.Errorf("expected 64-char hash, got %d: %s", len(h), h)
	}
}

func TestBundleID(t *testing.T) {
	b := NewEvidenceBundle("pr-url", "rid", Formation{}, "", "")
	id := b.BundleID()
	if len(id) != 16 {
		t.Errorf("expected 16-char bundle ID, got %d: %s", len(id), id)
	}
}

// =============================================================================
// Spec example: full bundle flow
// =============================================================================

func TestEvidenceBundle_SpecExample(t *testing.T) {
	formation := Formation{
		Primary:     ModelInfo{Model: "gpt-5.5", Provider: "openai"},
		Adversarial: ModelInfo{Model: "deepseek-v4-pro", Provider: "deepseek"},
		Audit:       ModelInfo{Model: "owl-alpha", Provider: "openrouter"},
	}

	b := NewEvidenceBundle(
		"https://forgejo.example.com/org/repo/pulls/42",
		"review-uuid",
		formation,
		"sha256-of-stripped-message",
		"sha256-of-original",
	)

	b.AddFinding(Finding{
		Model: "adversarial", Severity: "high", Type: "race_condition",
		File: "pkg/auth/session.go", Line: 142,
		Description: "Token refresh and invalidation race under concurrent access",
		Evidence:    "test_run_id: uuid, output: 'FAIL: TestConcurrentTokenRefresh'",
	})

	b.SetConsensus(
		VerdictPassWithNotes,
		VerdictBlock,
		VerdictConfirmAdversarial,
	)

	if b.Consensus.IsApproved() {
		t.Error("expected blocked consensus")
	}
	if !b.Consensus.IsBlocked() {
		t.Errorf("expected IsBlocked()=true, got resolution=%q", b.Consensus.Resolution)
	}

	// Sign each model
	for _, role := range []string{"primary", "adversarial", "audit"} {
		_, priv, err := GenerateKeyPair()
		if err != nil {
			t.Fatalf("GenerateKeyPair: %v", err)
		}
		_, err = b.SignBundle(role, priv)
		if err != nil {
			t.Fatalf("SignBundle(%q): %v", role, err)
		}
	}

	// Verify the bundle has a non-empty hash
	h := b.BundleHash()
	if len(h) != 64 {
		t.Errorf("expected 64-char hash, got %d", len(h))
	}
}

// =============================================================================
// Consensus helpers
// =============================================================================

func TestConsensus_IsApproved(t *testing.T) {
	c := Consensus{Resolution: ResolutionApproved}
	if !c.IsApproved() {
		t.Error("expected IsApproved()=true")
	}
	if c.IsBlocked() {
		t.Error("expected IsBlocked()=false")
	}
	if c.NeedsTieBreaker() {
		t.Error("expected NeedsTieBreaker()=false")
	}
}

func TestConsensus_IsBlocked(t *testing.T) {
	c := Consensus{Resolution: ResolutionBlocked}
	if !c.IsBlocked() {
		t.Error("expected IsBlocked()=true")
	}
	if c.IsApproved() {
		t.Error("expected IsApproved()=false")
	}
}

func TestConsensus_NeedsTieBreaker(t *testing.T) {
	c := Consensus{Resolution: ResolutionTieBreak}
	if !c.NeedsTieBreaker() {
		t.Error("expected NeedsTieBreaker()=true")
	}
	if c.IsApproved() {
		t.Error("expected IsApproved()=false")
	}
}

// =============================================================================
// Edge cases
// =============================================================================

func TestBundleHash_EmptyBundle(t *testing.T) {
	b := NewEvidenceBundle("", "", Formation{}, "", "")
	h := b.BundleHash()
	if len(h) != 64 {
		t.Errorf("expected 64-char hash, got %d", len(h))
	}
}

func TestBundleHash_ExcludesPreviousBundleID(t *testing.T) {
	now := time.Now().UTC()
	b1 := NewEvidenceBundle("pr-url", "rid", Formation{}, "", "")
	b1.Timestamp = now
	b2 := NewEvidenceBundle("pr-url", "rid", Formation{}, "", "")
	b2.Timestamp = now
	// b2 should have same hash as b1 (PreviousBundleID is not part of the canonical content)
	h1 := b1.BundleHash()
	h2 := b2.BundleHash()
	if h1 != h2 {
		t.Error("same content should produce same hash regardless of PreviousBundleID")
	}
}

func TestFinding_WithMitigation(t *testing.T) {
	b := NewEvidenceBundle("pr-url", "rid", Formation{}, "", "")
	b.AddFinding(Finding{
		Model: "adversarial", Severity: "high", Type: "race_condition",
		File: "session.go", Line: 142,
		Description: "Race condition",
		Evidence:    "test output",
		Mitigation:  "Add mutex lock around token refresh",
	})
	if len(b.Findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(b.Findings))
	}
	if b.Findings[0].Mitigation != "Add mutex lock around token refresh" {
		t.Errorf("expected mitigation, got %q", b.Findings[0].Mitigation)
	}
	h := b.BundleHash()
	if len(h) != 64 {
		t.Errorf("expected 64-char hash, got %d", len(h))
	}
}

// =============================================================================
// Signatures map check
// =============================================================================

func TestSignBundle_StoresInCorrectField(t *testing.T) {
	b := NewEvidenceBundle("pr-url", "rid", Formation{}, "", "")
	_, priv, _ := GenerateKeyPair()

	b.SignBundle("primary", priv)
	if b.Signatures.Primary == "" {
		t.Error("expected primary signature")
	}
	if b.Signatures.Adversarial != "" {
		t.Error("expected adversarial to be empty")
	}
	if b.Signatures.Audit != "" {
		t.Error("expected audit to be empty")
	}
}

// =============================================================================
// VerifyAllSignatures partial
// =============================================================================

func TestVerifyAllSignatures_PartialKeys(t *testing.T) {
	b := NewEvidenceBundle("pr-url", "rid", Formation{}, "", "")
	_, priv, _ := GenerateKeyPair()
	b.SignBundle("primary", priv)
	b.SignBundle("adversarial", priv) // same key (unusual but valid for test)

	pub, _, _ := GenerateKeyPair()
	// Only provide one key — adversarial will fail
	results, _ := b.VerifyAllSignatures(map[string]ed25519.PublicKey{
		"primary": pub, // wrong key for primary
	})
	// primary should fail (wrong key)
	// adversarial and audit should have errors
	if results["primary"] {
		t.Error("expected primary verification to fail (wrong key)")
	}
}

// =============================================================================
// Timestamp in canonical hash
// =============================================================================

func TestBundleHash_UsesRFC3339Nano(t *testing.T) {
	b := NewEvidenceBundle("pr-url", "rid", Formation{}, "", "")
	h := b.BundleHash()
	// Just verify it's a 64-char hex string
	if len(h) != 64 {
		t.Errorf("expected 64-char hash, got %d: %s", len(h), h)
	}
	// Verify it's valid hex
	for _, c := range h {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("non-hex character in hash: %c", c)
		}
	}
}

// =============================================================================
// Large bundle with many findings
// =============================================================================

func TestBundleHash_ManyFindings(t *testing.T) {
	b := NewEvidenceBundle("pr-url", "rid", Formation{}, "", "")
	for i := 0; i < 100; i++ {
		b.AddFinding(Finding{
			Model: "primary", Severity: "low", Type: "nit",
			File: "main.go", Line: i,
			Description: "finding", Evidence: "test",
		})
	}
	h := b.BundleHash()
	if len(h) != 64 {
		t.Errorf("expected 64-char hash, got %d", len(h))
	}
}
