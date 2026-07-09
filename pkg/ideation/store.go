package ideation

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DefaultIdeasDir is the default directory under the user home for ideas.
const DefaultIdeasDir = ".helix/ideas"

// DefaultIdeasFile is the default JSONL filename.
const DefaultIdeasFile = "ideas.jsonl"

// Store is a thread-safe JSONL idea store.
// Capture appends; Update/Promote/Close rewrite atomically (temp + rename)
// so List() always sees the latest status per ID (last write wins).
type Store struct {
	mu   sync.Mutex
	path string
}

// NewStore constructs a Store. Empty path resolves to
// ~/.helix/ideas/ideas.jsonl via os.UserHomeDir.
func NewStore(path string) (*Store, error) {
	expanded, err := resolveStorePath(path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(expanded), 0o755); err != nil {
		return nil, fmt.Errorf("ideation: mkdir %s: %w", filepath.Dir(expanded), err)
	}
	return &Store{path: expanded}, nil
}

// Path returns the absolute store path.
func (s *Store) Path() string {
	return s.path
}

// Capture assigns ID/CreatedAt/UpdatedAt/Status=draft if missing and
// appends a JSONL line. Returns error if title is empty.
func (s *Store) Capture(idea *Idea) error {
	if idea == nil {
		return fmt.Errorf("ideation: idea is nil")
	}
	if strings.TrimSpace(idea.Title) == "" {
		return fmt.Errorf("ideation: title is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	if idea.ID == "" {
		idea.ID = NewIdeaID()
	}
	if idea.Status == "" {
		idea.Status = StatusDraft
	}
	if idea.Source == "" {
		idea.Source = SourceHuman
	}
	if idea.CreatedAt.IsZero() {
		idea.CreatedAt = now
	}
	idea.UpdatedAt = now

	return s.appendUnlocked(idea)
}

// Get returns the latest idea for id (last write wins).
func (s *Store) Get(id string) (*Idea, error) {
	if id == "" {
		return nil, fmt.Errorf("ideation: id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	m, err := s.loadMapUnlocked()
	if err != nil {
		return nil, err
	}
	idea, ok := m[id]
	if !ok {
		return nil, fmt.Errorf("ideation: idea %q not found", id)
	}
	// Return a copy to avoid external mutation races.
	cp := *idea
	return &cp, nil
}

// List returns all ideas (latest status per ID).
func (s *Store) List() ([]*Idea, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	m, err := s.loadMapUnlocked()
	if err != nil {
		return nil, err
	}
	out := make([]*Idea, 0, len(m))
	for _, idea := range m {
		cp := *idea
		out = append(out, &cp)
	}
	return out, nil
}

// Update rewrites the store with the given idea (latest wins).
func (s *Store) Update(idea *Idea) error {
	if idea == nil {
		return fmt.Errorf("ideation: idea is nil")
	}
	if idea.ID == "" {
		return fmt.Errorf("ideation: id is required")
	}
	if strings.TrimSpace(idea.Title) == "" {
		return fmt.Errorf("ideation: title is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	m, err := s.loadMapUnlocked()
	if err != nil {
		return err
	}
	if _, ok := m[idea.ID]; !ok {
		return fmt.Errorf("ideation: idea %q not found", idea.ID)
	}
	idea.UpdatedAt = time.Now().UTC()
	cp := *idea
	m[idea.ID] = &cp
	return s.rewriteUnlocked(m)
}

// Promote sets Status=promoted and PromotedTo=specPath.
func (s *Store) Promote(id, specPath string) error {
	if id == "" {
		return fmt.Errorf("ideation: id is required")
	}
	if strings.TrimSpace(specPath) == "" {
		return fmt.Errorf("ideation: spec path is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	m, err := s.loadMapUnlocked()
	if err != nil {
		return err
	}
	idea, ok := m[id]
	if !ok {
		return fmt.Errorf("ideation: idea %q not found", id)
	}
	// High-risk ideas must not be promoted (proxy for failed validation).
	if idea.RiskScore >= 70 {
		return fmt.Errorf("idea failed validation (risk_score>=70); re-validate after addressing findings")
	}
	idea.Status = StatusPromoted
	idea.PromotedTo = specPath
	idea.UpdatedAt = time.Now().UTC()
	return s.rewriteUnlocked(m)
}

// Close sets Status=closed with the given reason.
func (s *Store) Close(id, reason string) error {
	if id == "" {
		return fmt.Errorf("ideation: id is required")
	}
	if strings.TrimSpace(reason) == "" {
		return fmt.Errorf("ideation: close reason is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	m, err := s.loadMapUnlocked()
	if err != nil {
		return err
	}
	idea, ok := m[id]
	if !ok {
		return fmt.Errorf("ideation: idea %q not found", id)
	}
	idea.Status = StatusClosed
	idea.ClosedReason = reason
	idea.UpdatedAt = time.Now().UTC()
	return s.rewriteUnlocked(m)
}

// Capture is a package-level helper that delegates to store.Capture.
func Capture(store *Store, idea *Idea) error {
	return store.Capture(idea)
}

// ---------------------------------------------------------------------------
// Internals
// ---------------------------------------------------------------------------

func resolveStorePath(path string) (string, error) {
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("ideation: home dir: %w", err)
		}
		return filepath.Join(home, DefaultIdeasDir, DefaultIdeasFile), nil
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("ideation: home dir: %w", err)
		}
		return filepath.Join(home, path[2:]), nil
	}
	return filepath.Abs(path)
}

func (s *Store) appendUnlocked(idea *Idea) error {
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("ideation: open %s: %w", s.path, err)
	}
	defer f.Close()

	data, err := json.Marshal(idea)
	if err != nil {
		return fmt.Errorf("ideation: marshal: %w", err)
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("ideation: write: %w", err)
	}
	return nil
}

func (s *Store) loadMapUnlocked() (map[string]*Idea, error) {
	m := make(map[string]*Idea)
	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return m, nil
		}
		return nil, fmt.Errorf("ideation: open %s: %w", s.path, err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	// Allow large idea bodies.
	sc.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var idea Idea
		if err := json.Unmarshal([]byte(line), &idea); err != nil {
			return nil, fmt.Errorf("ideation: parse line %d: %w", lineNo, err)
		}
		if idea.ID == "" {
			continue
		}
		cp := idea
		m[idea.ID] = &cp
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("ideation: scan: %w", err)
	}
	return m, nil
}

func (s *Store) rewriteUnlocked(m map[string]*Idea) error {
	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, "ideas-*.jsonl.tmp")
	if err != nil {
		return fmt.Errorf("ideation: temp file: %w", err)
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmpName)
		}
	}()

	enc := json.NewEncoder(tmp)
	for _, idea := range m {
		if err := enc.Encode(idea); err != nil {
			_ = tmp.Close()
			return fmt.Errorf("ideation: encode: %w", err)
		}
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("ideation: sync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("ideation: close temp: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("ideation: rename: %w", err)
	}
	ok = true
	return nil
}
