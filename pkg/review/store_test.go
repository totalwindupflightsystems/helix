package review

import (
	"crypto/ed25519"
	"path/filepath"
	"testing"
)

func makeTestStore(t *testing.T) *EvidenceStore {
	t.Helper()
	dir := t.TempDir()
	store, err := NewEvidenceStoreWithDir(dir)
	if err != nil {
		t.Fatalf("NewEvidenceStoreWithDir: %v", err)
	}
	return store
}

func makeSignedBundle(t *testing.T) (*EvidenceBundle, map[string]ed25519.PublicKey) {
	t.Helper()
	pub1, priv1, _ := ed25519.GenerateKey(nil)
	pub2, priv2, _ := ed25519.GenerateKey(nil)
	pub3, priv3, _ := ed25519.GenerateKey(nil)

	formation := Formation{
		Primary:     ModelInfo{Model: "gpt-5.5", Provider: "openai"},
		Adversarial: ModelInfo{Model: "deepseek-v4-pro", Provider: "deepseek"},
		Audit:       ModelInfo{Model: "owl-alpha", Provider: "openrouter"},
	}
	b := NewEvidenceBundle("https://forgejo.example.com/org/repo/pulls/42",
		"review-uuid-1", formation, "abc123", "def456")
	b.AddFinding(Finding{
		Model: "adversarial", Severity: "high", Type: "race_condition",
		File: "pkg/auth/session.go", Line: 142,
		Description: "Token refresh race",
		Evidence:    "test_run_id: uuid, output: FAIL",
	})
	b.SetConsensus("pass_with_notes", "block", "confirm_adversarial")
	_, _ = b.SignBundle("primary", priv1)
	_, _ = b.SignBundle("adversarial", priv2)
	_, _ = b.SignBundle("audit", priv3)

	keys := map[string]ed25519.PublicKey{
		"primary":     pub1,
		"adversarial": pub2,
		"audit":       pub3,
	}
	return b, keys
}

// ─── Store ───

func TestEvidenceStore_Store(t *testing.T) {
	store := makeTestStore(t)
	b, _ := makeSignedBundle(t)

	path, err := store.Store(b, "agent-001")
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if path == "" {
		t.Fatal("Store returned empty path")
	}
	if filepath.Base(path) != "review-uuid-1.json" {
		t.Fatalf("expected review-uuid-1.json, got %s", filepath.Base(path))
	}
}

func TestEvidenceStore_StoreNilBundle(t *testing.T) {
	store := makeTestStore(t)
	_, err := store.Store(nil, "agent-001")
	if err == nil {
		t.Fatal("expected error for nil bundle")
	}
}

func TestEvidenceStore_StoreEmptyReviewID(t *testing.T) {
	store := makeTestStore(t)
	b := &EvidenceBundle{ReviewID: ""}
	_, err := store.Store(b, "agent-001")
	if err == nil {
		t.Fatal("expected error for empty review_id")
	}
}

// ─── Load ───

func TestEvidenceStore_Load(t *testing.T) {
	store := makeTestStore(t)
	b, _ := makeSignedBundle(t)

	if _, err := store.Store(b, "agent-001"); err != nil {
		t.Fatalf("Store: %v", err)
	}

	loaded, entry, err := store.Load("review-uuid-1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded == nil {
		t.Fatal("loaded bundle is nil")
	}
	if loaded.ReviewID != "review-uuid-1" {
		t.Fatalf("expected review-uuid-1, got %s", loaded.ReviewID)
	}
	if loaded.PRURL != b.PRURL {
		t.Fatalf("PR URL mismatch: %s != %s", loaded.PRURL, b.PRURL)
	}
	if entry == nil {
		t.Fatal("entry is nil")
	}
	if entry.AgentID != "agent-001" {
		t.Fatalf("expected agent-001, got %s", entry.AgentID)
	}
	if len(loaded.Findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(loaded.Findings))
	}
}

func TestEvidenceStore_LoadNotFound(t *testing.T) {
	store := makeTestStore(t)
	_, _, err := store.Load("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent review")
	}
}

func TestEvidenceStore_LoadRaw(t *testing.T) {
	store := makeTestStore(t)
	b, _ := makeSignedBundle(t)

	if _, err := store.Store(b, ""); err != nil {
		t.Fatalf("Store: %v", err)
	}

	loaded, err := store.LoadRaw("review-uuid-1")
	if err != nil {
		t.Fatalf("LoadRaw: %v", err)
	}
	if loaded == nil {
		t.Fatal("loaded bundle is nil")
	}
}

func TestEvidenceStore_RoundTripIntegrity(t *testing.T) {
	store := makeTestStore(t)
	b, keys := makeSignedBundle(t)

	if _, err := store.Store(b, "agent-001"); err != nil {
		t.Fatalf("Store: %v", err)
	}
	store.SetKeys(keys)

	loaded, _, err := store.Load("review-uuid-1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Verify bundle hash is unchanged
	if loaded.BundleHash() != b.BundleHash() {
		t.Fatal("bundle hash changed during store/load round-trip")
	}
	// Verify all signatures still valid
	results, err := loaded.VerifyAllSignatures(keys)
	if err != nil {
		t.Fatalf("VerifyAllSignatures: %v", err)
	}
	for role, ok := range results {
		if !ok {
			t.Errorf("signature for %s is invalid after round-trip", role)
		}
	}
}

// ─── VerifyIntegrity ───

func TestEvidenceStore_VerifyIntegrity(t *testing.T) {
	store := makeTestStore(t)
	b, keys := makeSignedBundle(t)

	if _, err := store.Store(b, "agent-001"); err != nil {
		t.Fatalf("Store: %v", err)
	}
	store.SetKeys(keys)

	results, err := store.VerifyIntegrity("review-uuid-1")
	if err != nil {
		t.Fatalf("VerifyIntegrity: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	for role, ok := range results {
		if !ok {
			t.Errorf("signature for %s failed verification", role)
		}
	}
}

func TestEvidenceStore_VerifyIntegrity_NoKeys(t *testing.T) {
	store := makeTestStore(t)
	b, _ := makeSignedBundle(t)

	if _, err := store.Store(b, "agent-001"); err != nil {
		t.Fatalf("Store: %v", err)
	}
	_, err := store.VerifyIntegrity("review-uuid-1")
	if err == nil {
		t.Fatal("expected error when no keys registered")
	}
}

func TestEvidenceStore_VerifyAllIntegrity(t *testing.T) {
	store := makeTestStore(t)
	b1, keys1 := makeSignedBundle(t)
	b2, _ := makeSignedBundle(t)
	b2.ReviewID = "review-uuid-2"
	b2.PRURL = "https://forgejo.example.com/org/repo/pulls/99"

	if _, err := store.Store(b1, "agent-001"); err != nil {
		t.Fatalf("Store b1: %v", err)
	}
	if _, err := store.Store(b2, "agent-002"); err != nil {
		t.Fatalf("Store b2: %v", err)
	}
	store.SetKeys(keys1)

	results, err := store.VerifyAllIntegrity()
	if err != nil {
		t.Fatalf("VerifyAllIntegrity: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 bundles verified, got %d", len(results))
	}
	// b1 should verify all true (uses same keys)
	v1 := results["review-uuid-1"]
	for _, ok := range v1 {
		if !ok {
			t.Error("b1 signature failed")
		}
	}
}

// ─── ListAll ───

func TestEvidenceStore_ListAll(t *testing.T) {
	store := makeTestStore(t)
	b1, _ := makeSignedBundle(t)
	b2, _ := makeSignedBundle(t)
	b2.ReviewID = "review-uuid-2"

	if _, err := store.Store(b1, "agent-a"); err != nil {
		t.Fatalf("Store b1: %v", err)
	}
	if _, err := store.Store(b2, "agent-b"); err != nil {
		t.Fatalf("Store b2: %v", err)
	}

	entries, err := store.ListAll()
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
}

func TestEvidenceStore_ListAll_Empty(t *testing.T) {
	store := makeTestStore(t)
	entries, err := store.ListAll()
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(entries))
	}
}

// ─── ListByAgent ───

func TestEvidenceStore_ListByAgent(t *testing.T) {
	store := makeTestStore(t)
	b1, _ := makeSignedBundle(t)
	b2, _ := makeSignedBundle(t)
	b2.ReviewID = "review-uuid-2"
	b3, _ := makeSignedBundle(t)
	b3.ReviewID = "review-uuid-3"

	_, _ = store.Store(b1, "agent-001")
	_, _ = store.Store(b2, "agent-002")
	_, _ = store.Store(b3, "agent-001")

	bundles, err := store.ListByAgent("agent-001")
	if err != nil {
		t.Fatalf("ListByAgent: %v", err)
	}
	if len(bundles) != 2 {
		t.Fatalf("expected 2 bundles for agent-001, got %d", len(bundles))
	}
}

func TestEvidenceStore_ListByAgent_None(t *testing.T) {
	store := makeTestStore(t)
	b, _ := makeSignedBundle(t)
	_, _ = store.Store(b, "agent-001")

	bundles, err := store.ListByAgent("nonexistent-agent")
	if err != nil {
		t.Fatalf("ListByAgent: %v", err)
	}
	if len(bundles) != 0 {
		t.Fatalf("expected 0 bundles, got %d", len(bundles))
	}
}

func TestEvidenceStore_ListByAgent_EmptyAgentID(t *testing.T) {
	store := makeTestStore(t)
	b1, _ := makeSignedBundle(t)
	_, _ = store.Store(b1, "") // no agent ID

	bundles, err := store.ListByAgent("")
	if err != nil {
		t.Fatalf("ListByAgent: %v", err)
	}
	if len(bundles) != 1 {
		t.Fatalf("expected 1 bundle with empty agent_id, got %d", len(bundles))
	}
}

// ─── ListByPR ───

func TestEvidenceStore_ListByPR(t *testing.T) {
	store := makeTestStore(t)
	b1, _ := makeSignedBundle(t) // PR 42
	b2, _ := makeSignedBundle(t)
	b2.ReviewID = "review-uuid-2"
	b2.PRURL = "https://forgejo.example.com/org/repo/pulls/99"

	_, _ = store.Store(b1, "agent-001")
	_, _ = store.Store(b2, "agent-002")

	bundles, err := store.ListByPR("https://forgejo.example.com/org/repo/pulls/42")
	if err != nil {
		t.Fatalf("ListByPR: %v", err)
	}
	if len(bundles) != 1 {
		t.Fatalf("expected 1 bundle for PR 42, got %d", len(bundles))
	}
}

func TestEvidenceStore_ListByPR_MultipleReviews(t *testing.T) {
	store := makeTestStore(t)
	prURL := "https://forgejo/pr/100"

	b1, _ := makeSignedBundle(t)
	b1.PRURL = prURL
	b2, _ := makeSignedBundle(t)
	b2.ReviewID = "review-uuid-2"
	b2.PRURL = prURL

	_, _ = store.Store(b1, "agent-a")
	_, _ = store.Store(b2, "agent-b")

	bundles, err := store.ListByPR(prURL)
	if err != nil {
		t.Fatalf("ListByPR: %v", err)
	}
	if len(bundles) != 2 {
		t.Fatalf("expected 2 bundles for PR 100, got %d", len(bundles))
	}
}

// ─── LinkFromMerge ───

func TestEvidenceStore_LinkFromMerge(t *testing.T) {
	store := makeTestStore(t)
	b, _ := makeSignedBundle(t)
	_, _ = store.Store(b, "agent-001")

	link := store.LinkFromMerge("review-uuid-1")
	if link == "" {
		t.Fatal("LinkFromMerge returned empty string")
	}
	if !storeContains(link, "evidence:") {
		t.Fatalf("expected evidence: prefix, got %s", link)
	}
	if !storeContains(link, "review-uuid-1") {
		t.Fatalf("expected review-uuid-1 in link, got %s", link)
	}
}

func storeContains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// ─── Delete ───

func TestEvidenceStore_Delete(t *testing.T) {
	store := makeTestStore(t)
	b, _ := makeSignedBundle(t)
	_, _ = store.Store(b, "agent-001")

	if err := store.Delete("review-uuid-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, _, err := store.Load("review-uuid-1")
	if err == nil {
		t.Fatal("expected error loading deleted bundle")
	}
}

func TestEvidenceStore_DeleteNotFound(t *testing.T) {
	store := makeTestStore(t)
	err := store.Delete("nonexistent")
	if err == nil {
		t.Fatal("expected error deleting nonexistent")
	}
}

// ─── Count ───

func TestEvidenceStore_Count(t *testing.T) {
	store := makeTestStore(t)
	b1, _ := makeSignedBundle(t)
	b2, _ := makeSignedBundle(t)
	b2.ReviewID = "review-uuid-2"

	_, _ = store.Store(b1, "agent-a")
	_, _ = store.Store(b2, "agent-b")

	count, err := store.Count()
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected count 2, got %d", count)
	}
}

// ─── Search ───

func TestEvidenceStore_Search_ByDescription(t *testing.T) {
	store := makeTestStore(t)
	b, _ := makeSignedBundle(t)
	_, _ = store.Store(b, "agent-001")

	results, err := store.Search("race")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}

func TestEvidenceStore_Search_ByAgentID(t *testing.T) {
	store := makeTestStore(t)
	b, _ := makeSignedBundle(t)
	_, _ = store.Store(b, "agent-007")

	results, err := store.Search("agent-007")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}

func TestEvidenceStore_Search_ByPRURL(t *testing.T) {
	store := makeTestStore(t)
	b, _ := makeSignedBundle(t)
	_, _ = store.Store(b, "agent-001")

	results, err := store.Search("pulls/42")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}

func TestEvidenceStore_Search_CaseInsensitive(t *testing.T) {
	store := makeTestStore(t)
	b, _ := makeSignedBundle(t)
	_, _ = store.Store(b, "agent-001")

	results, err := store.Search("TOKEN REFRESH RACE")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}

func TestEvidenceStore_Search_NoMatch(t *testing.T) {
	store := makeTestStore(t)
	b, _ := makeSignedBundle(t)
	_, _ = store.Store(b, "agent-001")

	results, err := store.Search("nonexistent-query-xyz")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

// ─── NewEvidenceStore (default path) ───

func TestNewEvidenceStore(t *testing.T) {
	// Can't test actual ~/.helix/evidence in CI, but can test error path
	store, err := NewEvidenceStore()
	if err != nil {
		t.Fatalf("NewEvidenceStore: %v", err)
	}
	if store == nil {
		t.Fatal("store is nil")
	}
	if store.Dir() == "" {
		t.Fatal("Dir() returned empty")
	}
}

func TestNewEvidenceStoreWithDir_Empty(t *testing.T) {
	_, err := NewEvidenceStoreWithDir("")
	if err == nil {
		t.Fatal("expected error for empty dir")
	}
}

// ─── StoreEntry ───

func TestEvidenceStore_StoreEntryMetadata(t *testing.T) {
	store := makeTestStore(t)
	b, _ := makeSignedBundle(t)
	_, _ = store.Store(b, "agent-xyz")

	_, entry, err := store.Load("review-uuid-1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if entry.AgentID != "agent-xyz" {
		t.Fatalf("expected agent-xyz, got %s", entry.AgentID)
	}
	if entry.StoredAt.IsZero() {
		t.Fatal("StoredAt is zero")
	}
}

// ─── Spec Example ───

func TestEvidenceStore_SpecExample(t *testing.T) {
	store := makeTestStore(t)
	pub1, priv1, _ := ed25519.GenerateKey(nil)
	pub2, priv2, _ := ed25519.GenerateKey(nil)
	pub3, priv3, _ := ed25519.GenerateKey(nil)

	formation := Formation{
		Primary:     ModelInfo{Model: "gpt-5.5", Provider: "openai"},
		Adversarial: ModelInfo{Model: "deepseek-v4-pro", Provider: "deepseek"},
		Audit:       ModelInfo{Model: "owl-alpha", Provider: "openrouter"},
	}
	b := NewEvidenceBundle(
		"https://forgejo.example.com/org/repo/pulls/42",
		"uuid-spec-example",
		formation,
		"sha256-of-stripped-message",
		"sha256-of-original",
	)
	b.AddFinding(Finding{
		Model:       "adversarial",
		Severity:    "high",
		Type:        "race_condition",
		File:        "pkg/auth/session.go",
		Line:        142,
		Description: "Token refresh and invalidation race under concurrent access",
		Evidence:    "test_run_id: uuid, output: 'FAIL: TestConcurrentTokenRefresh'",
	})
	b.SetConsensus("pass_with_notes", "block", "confirm_adversarial")
	_, _ = b.SignBundle("primary", priv1)
	_, _ = b.SignBundle("adversarial", priv2)
	_, _ = b.SignBundle("audit", priv3)

	keys := map[string]ed25519.PublicKey{
		"primary":     pub1,
		"adversarial": pub2,
		"audit":       pub3,
	}

	path, err := store.Store(b, "agent-spec-test")
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if path == "" {
		t.Fatal("empty path")
	}

	store.SetKeys(keys)

	results, err := store.VerifyIntegrity("uuid-spec-example")
	if err != nil {
		t.Fatalf("VerifyIntegrity: %v", err)
	}
	for role, ok := range results {
		if !ok {
			t.Errorf("spec example: signature %s failed", role)
		}
	}

	link := store.LinkFromMerge("uuid-spec-example")
	if !storeContains(link, "evidence:") {
		t.Fatalf("LinkFromMerge missing evidence: prefix: %s", link)
	}
}
