package review

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// =============================================================================
// EvidenceStore — persists evidence bundles to disk as JSON files.
// Spec §Evidence Bundles: "stored in DuckBrain and linked from the merge commit"
// =============================================================================

// DefaultEvidenceDir is the default storage path for evidence bundles.
const DefaultEvidenceDir = ".helix/evidence"

// StoreEntry wraps an EvidenceBundle with agent metadata for indexed retrieval.
type StoreEntry struct {
	AgentID  string          `json:"agent_id,omitempty"`
	StoredAt time.Time       `json:"stored_at"`
	Bundle   *EvidenceBundle `json:"bundle"`
}

// EvidenceStore persists evidence bundles to the filesystem.
type EvidenceStore struct {
	dir  string
	keys map[string]ed25519.PublicKey // role → public key for verification
}

// NewEvidenceStore creates a store rooted at ~/.helix/evidence by default.
func NewEvidenceStore() (*EvidenceStore, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("get home dir: %w", err)
	}
	return NewEvidenceStoreWithDir(filepath.Join(home, DefaultEvidenceDir))
}

// NewEvidenceStoreWithDir creates a store rooted at the given directory.
func NewEvidenceStoreWithDir(dir string) (*EvidenceStore, error) {
	if dir == "" {
		return nil, fmt.Errorf("dir must not be empty")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create evidence dir %s: %w", dir, err)
	}
	return &EvidenceStore{
		dir:  dir,
		keys: make(map[string]ed25519.PublicKey),
	}, nil
}

// SetKeys registers role→public key mappings for signature verification on load.
func (s *EvidenceStore) SetKeys(keys map[string]ed25519.PublicKey) {
	s.keys = keys
}

// Dir returns the storage directory path.
func (s *EvidenceStore) Dir() string {
	return s.dir
}

// bundlePath returns the filesystem path for a bundle by review ID.
func (s *EvidenceStore) bundlePath(reviewID string) string {
	return filepath.Join(s.dir, reviewID+".json")
}

// Store writes an evidence bundle to disk. If agentID is non-empty, it is
// recorded for ListByAgent queries.
func (s *EvidenceStore) Store(bundle *EvidenceBundle, agentID string) (string, error) {
	if bundle == nil {
		return "", fmt.Errorf("bundle is nil")
	}
	if bundle.ReviewID == "" {
		return "", fmt.Errorf("bundle has empty review_id")
	}
	entry := StoreEntry{
		AgentID:  agentID,
		StoredAt: time.Now().UTC(),
		Bundle:   bundle,
	}
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal entry: %w", err)
	}
	path := s.bundlePath(bundle.ReviewID)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	return path, nil
}

// Load reads a bundle by review ID. If keys are registered, signatures are
// verified on load. Returns the bundle and its store entry metadata.
func (s *EvidenceStore) Load(reviewID string) (*EvidenceBundle, *StoreEntry, error) {
	path := s.bundlePath(reviewID)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", path, err)
	}
	var entry StoreEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, nil, fmt.Errorf("unmarshal %s: %w", path, err)
	}
	if entry.Bundle == nil {
		return nil, nil, fmt.Errorf("entry %s has nil bundle", reviewID)
	}
	return entry.Bundle, &entry, nil
}

// LoadRaw reads a bundle without verification (for cases where keys are unknown).
func (s *EvidenceStore) LoadRaw(reviewID string) (*EvidenceBundle, error) {
	b, _, err := s.Load(reviewID)
	return b, err
}

// VerifyIntegrity re-checks all signatures for a stored bundle using the
// registered keys. Returns per-role results.
func (s *EvidenceStore) VerifyIntegrity(reviewID string) (map[string]bool, error) {
	bundle, _, err := s.Load(reviewID)
	if err != nil {
		return nil, err
	}
	if len(s.keys) == 0 {
		return nil, fmt.Errorf("no keys registered for verification")
	}
	return bundle.VerifyAllSignatures(s.keys)
}

// VerifyAllIntegrity scans all stored bundles and verifies their signatures.
// Returns a map of reviewID → per-role verification results.
func (s *EvidenceStore) VerifyAllIntegrity() (map[string]map[string]bool, error) {
	entries, err := s.ListAll()
	if err != nil {
		return nil, err
	}
	if len(s.keys) == 0 {
		return nil, fmt.Errorf("no keys registered for verification")
	}
	results := make(map[string]map[string]bool)
	for _, entry := range entries {
		bundle := entry.Bundle
		verified, err := bundle.VerifyAllSignatures(s.keys)
		if err != nil {
			results[bundle.ReviewID] = nil
		} else {
			results[bundle.ReviewID] = verified
		}
	}
	return results, nil
}

// ListAll reads every stored bundle entry from disk.
func (s *EvidenceStore) ListAll() ([]StoreEntry, error) {
	files, err := filepath.Glob(filepath.Join(s.dir, "*.json"))
	if err != nil {
		return nil, fmt.Errorf("glob %s: %w", s.dir, err)
	}
	var entries []StoreEntry
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue // skip unreadable files
		}
		var entry StoreEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			continue // skip malformed files
		}
		if entry.Bundle != nil {
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

// ListByAgent returns all bundles for a given agent ID.
func (s *EvidenceStore) ListByAgent(agentID string) ([]*EvidenceBundle, error) {
	entries, err := s.ListAll()
	if err != nil {
		return nil, err
	}
	var bundles []*EvidenceBundle
	for _, e := range entries {
		if e.AgentID == agentID {
			bundles = append(bundles, e.Bundle)
		}
	}
	return bundles, nil
}

// ListByPR returns all bundles associated with a PR URL.
func (s *EvidenceStore) ListByPR(prURL string) ([]*EvidenceBundle, error) {
	entries, err := s.ListAll()
	if err != nil {
		return nil, err
	}
	var bundles []*EvidenceBundle
	for _, e := range entries {
		if e.Bundle != nil && e.Bundle.PRURL == prURL {
			bundles = append(bundles, e.Bundle)
		}
	}
	return bundles, nil
}

// LinkFromMerge returns a human-readable path string for embedding in a merge
// commit message. Format: "evidence:<review_id>" pointing to the file path.
func (s *EvidenceStore) LinkFromMerge(reviewID string) string {
	return fmt.Sprintf("evidence:%s (%s)", reviewID, s.bundlePath(reviewID))
}

// Delete removes a stored bundle by review ID.
func (s *EvidenceStore) Delete(reviewID string) error {
	path := s.bundlePath(reviewID)
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
}

// Count returns the number of stored bundles.
func (s *EvidenceStore) Count() (int, error) {
	entries, err := s.ListAll()
	if err != nil {
		return 0, err
	}
	return len(entries), nil
}

// Search performs a case-insensitive text search across all stored bundles,
// matching against PR URL, review ID, finding descriptions, and agent ID.
func (s *EvidenceStore) Search(query string) ([]*EvidenceBundle, error) {
	entries, err := s.ListAll()
	if err != nil {
		return nil, err
	}
	query = strings.ToLower(query)
	var results []*EvidenceBundle
	for _, e := range entries {
		if s.entryMatches(&e, query) {
			results = append(results, e.Bundle)
		}
	}
	return results, nil
}

func (s *EvidenceStore) entryMatches(entry *StoreEntry, query string) bool {
	if entry.AgentID != "" && strings.Contains(strings.ToLower(entry.AgentID), query) {
		return true
	}
	b := entry.Bundle
	if b == nil {
		return false
	}
	if strings.Contains(strings.ToLower(b.ReviewID), query) {
		return true
	}
	if strings.Contains(strings.ToLower(b.PRURL), query) {
		return true
	}
	for _, f := range b.Findings {
		if strings.Contains(strings.ToLower(f.Description), query) ||
			strings.Contains(strings.ToLower(f.Evidence), query) ||
			strings.Contains(strings.ToLower(f.File), query) {
			return true
		}
	}
	return false
}
