// Package dispatcher — clarification.go
//
// Structured clarification protocol for in-progress agent collaboration.
// When an agent encounters ambiguity during step execution, it files a
// CLARIFICATION_NEEDED with a specific question, context, and blocked
// progress. The human or a trusted agent responds, and the resolution is
// linked to the task and spec for audit.
//
// Reference: specs/plans/phase-3-4-task-impl.md §4.2

package dispatcher

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Clarification types
// ---------------------------------------------------------------------------

// ClarificationRequest is emitted by an agent when it encounters ambiguity.
type ClarificationRequest struct {
	Type            string               `json:"type"` // always "CLARIFICATION_NEEDED"
	TaskID          string               `json:"task_id"`
	BlockedStep     int                  `json:"blocked_step"`
	Question        string               `json:"question"`
	Context         ClarificationContext `json:"context"`
	SuggestedAnswer string               `json:"suggested_answer"`
	BlockedSince    string               `json:"blocked_since"` // ISO8601
}

// ClarificationContext carries the ambient information that helps answer
// the question without requiring the responder to re-load the task.
type ClarificationContext struct {
	SpecSection     string `json:"spec_section"`
	SpecSectionHash string `json:"spec_section_hash"`
	RelevantCode    string `json:"relevant_code"`
	RelevantADR     string `json:"relevant_adr"`
}

// ClarificationResponse resolves a pending clarification.
type ClarificationResponse struct {
	Type       string `json:"type"` // "CLARIFICATION_RESOLVED"
	TaskID     string `json:"task_id"`
	Resolution string `json:"resolution"`
	ResolvedBy string `json:"resolved_by"` // "human:<name>" or "agent:<name>"
	ResolvedAt string `json:"resolved_at"` // ISO8601
}

// ClarificationRecord is the persistent shape stored on disk for each
// pending or resolved clarification.
type ClarificationRecord struct {
	Request    ClarificationRequest   `json:"request"`
	Response   *ClarificationResponse `json:"response,omitempty"`
	Status     string                 `json:"status"` // "pending" | "resolved"
	CreatedAt  string                 `json:"created_at"`
	ResolvedAt string                 `json:"resolved_at,omitempty"`
}

// ClarificationFilter limits List results.
type ClarificationFilter struct {
	AgentName string // filter by agent who filed the request (optional)
	Status    string // "pending" | "resolved" | "" (all)
}

// ---------------------------------------------------------------------------
// Store
// ---------------------------------------------------------------------------

// ClarificationStore manages pending and resolved clarifications on disk.
// Each task has one file at ~/.helix/clarifications/<task-id>.json.
type ClarificationStore struct {
	dir string
}

// DefaultClarificationDir returns the canonical clarification store root.
func DefaultClarificationDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".helix/clarifications"
	}
	return filepath.Join(home, ".helix", "clarifications")
}

// NewClarificationStore creates a store under the given directory.
func NewClarificationStore(dir string) *ClarificationStore {
	return &ClarificationStore{dir: dir}
}

// filePath returns the on-disk file for a given task.
func (cs *ClarificationStore) filePath(taskID string) string {
	return filepath.Join(cs.dir, sanitizeTaskID(taskID)+".json")
}

// Save persists a clarification record to disk. Creates the store directory
// on first use.
func (cs *ClarificationStore) Save(rec *ClarificationRecord) error {
	if err := os.MkdirAll(cs.dir, 0o755); err != nil {
		return fmt.Errorf("clarification store: mkdir: %w", err)
	}
	b, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("clarification store: marshal: %w", err)
	}
	return os.WriteFile(cs.filePath(rec.Request.TaskID), b, 0o644)
}

// Load reads an existing clarification record for the given task.
// Returns os.ErrNotExist if no record exists.
func (cs *ClarificationStore) Load(taskID string) (*ClarificationRecord, error) {
	path := cs.filePath(taskID)
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var rec ClarificationRecord
	if err := json.Unmarshal(b, &rec); err != nil {
		return nil, fmt.Errorf("clarification store: unmarshal %s: %w", path, err)
	}
	return &rec, nil
}

// Delete removes a clarification record from disk. No-op if not found.
func (cs *ClarificationStore) Delete(taskID string) error {
	path := cs.filePath(taskID)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("clarification store: remove %s: %w", path, err)
	}
	return nil
}

// List returns all clarification records matching the given filter.
func (cs *ClarificationStore) List(filter ClarificationFilter) ([]ClarificationRecord, error) {
	entries, err := os.ReadDir(cs.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("clarification store: readdir: %w", err)
	}

	var out []ClarificationRecord
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		rec, err := cs.Load(strings.TrimSuffix(e.Name(), ".json"))
		if err != nil {
			continue // skip unreadable files
		}
		// Apply filters.
		if filter.Status != "" && rec.Status != filter.Status {
			continue
		}
		// The request carries the agent ID in TaskID convention; if a
		// specific agent name is requested, match on the resolved-by field
		// or skip (we don't have an explicit agent field on the request yet).
		if filter.AgentName != "" {
			if rec.Response == nil || rec.Response.ResolvedBy == "" {
				continue
			}
			if !strings.Contains(rec.Response.ResolvedBy, filter.AgentName) {
				continue
			}
		}
		out = append(out, *rec)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Auto-resolution
// ---------------------------------------------------------------------------

// ADRStore abstracts a source of architecture decision records.
type ADRStore interface {
	// Search returns ADR content that matches the query. Returns the best
	// match text and true, or empty string and false if nothing matches.
	Search(query string) (string, bool)
}

// AutoResolve attempts to answer a clarification without human involvement.
// It checks (in order):
//  1. The spec itself — is the answer already in the spec section?
//  2. Prior similar clarifications — same question answered before?
//  3. ADR store — does an ADR directly answer this?
//
// Returns the resolution text and true if resolved, or empty string and false.
func AutoResolve(req ClarificationRequest, clarStore *ClarificationStore, adrStore ADRStore) (string, bool) {
	// 1. Already resolved? Check if a previous clarification for this task
	//    exists with the same question.
	existing, err := clarStore.Load(req.TaskID)
	if err == nil && existing.Response != nil && existing.Status == "resolved" {
		// Same task, already resolved — reuse the answer if the question
		// matches (fuzzy match by word count overlap).
		if fuzzyMatch(existing.Request.Question, req.Question) {
			return existing.Response.Resolution, true
		}
	}

	// 2. Search ADR store for relevant decisions.
	query := fmt.Sprintf("%s %s", req.Question, req.Context.RelevantADR)
	if adrStore != nil {
		if answer, found := adrStore.Search(query); found {
			return answer, true
		}
	}

	return "", false
}

// IsClarificationRequest checks whether the given error wraps a
// ClarificationRequest. Callers should use errors.As or check for the
// sentinel error to detect clarification requests.
//
// Usage in ForgejoLoop:
//
//	if clar, ok := IsClarificationRequest(err); ok { ... }
func IsClarificationRequest(err error) (*ClarificationRequest, bool) {
	if err == nil {
		return nil, false
	}
	var ce *clarificationError
	if AsClarificationError(err, &ce) {
		return ce.req, true
	}
	return nil, false
}

// ---------------------------------------------------------------------------
// Clarification error
// ---------------------------------------------------------------------------

// ErrClarificationNeeded is the sentinel error returned when an agent
// encounters ambiguity and files a clarification request.
var ErrClarificationNeeded = fmt.Errorf("dispatcher: clarification needed")

// clarificationError wraps a ClarificationRequest inside an error so it can
// propagate through the step execution chain.
type clarificationError struct {
	req *ClarificationRequest
}

func (e *clarificationError) Error() string {
	return fmt.Sprintf("clarification needed for task %s at step %d: %s",
		e.req.TaskID, e.req.BlockedStep, e.req.Question)
}

func (e *clarificationError) Unwrap() error { return ErrClarificationNeeded }

// NewClarificationError creates an error that wraps a clarification request.
func NewClarificationError(req *ClarificationRequest) error {
	return &clarificationError{req: req}
}

// AsClarificationError is a type-safe unwrap helper.
func AsClarificationError(err error, target **clarificationError) bool {
	for err != nil {
		if ce, ok := err.(*clarificationError); ok {
			*target = ce
			return true
		}
		err = unwrapError(err)
	}
	return false
}

// ---------------------------------------------------------------------------
// Request construction helper
// ---------------------------------------------------------------------------

// NewClarificationRequest builds a ClarificationRequest with defaults
// filled in. The caller provides the task ID, blocked step index, question,
// and optional context fields.
func NewClarificationRequest(taskID string, blockedStep int, question string, ctx ClarificationContext) *ClarificationRequest {
	if ctx.SpecSection != "" && ctx.SpecSectionHash == "" {
		ctx.SpecSectionHash = hashSection(ctx.SpecSection)
	}
	return &ClarificationRequest{
		Type:         "CLARIFICATION_NEEDED",
		TaskID:       taskID,
		BlockedStep:  blockedStep,
		Question:     question,
		Context:      ctx,
		BlockedSince: time.Now().UTC().Format(time.RFC3339),
	}
}

// NewClarificationResponse builds a response for a given request.
func NewClarificationResponse(taskID, resolution, resolvedBy string) *ClarificationResponse {
	return &ClarificationResponse{
		Type:       "CLARIFICATION_RESOLVED",
		TaskID:     taskID,
		Resolution: resolution,
		ResolvedBy: resolvedBy,
		ResolvedAt: time.Now().UTC().Format(time.RFC3339),
	}
}

// Resolve ties a request and response together into a record and persists it.
func (cs *ClarificationStore) Resolve(req *ClarificationRequest, resp *ClarificationResponse) (*ClarificationRecord, error) {
	// Try to load existing record (may have been filed earlier).
	rec, err := cs.Load(req.TaskID)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		// Fresh record.
		rec = &ClarificationRecord{
			Request:   *req,
			Status:    "resolved",
			CreatedAt: req.BlockedSince,
		}
	}
	rec.Response = resp
	rec.Status = "resolved"
	rec.ResolvedAt = resp.ResolvedAt
	if err := cs.Save(rec); err != nil {
		return nil, err
	}
	return rec, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// sanitizeTaskID replaces path-separator characters in a task ID so it can be
// used as a filename.
func sanitizeTaskID(id string) string {
	r := strings.NewReplacer("/", "-", "\\", "-", ":", "-", " ", "_")
	return r.Replace(id)
}

// fuzzyMatch returns true when two strings share a significant word overlap.
// This is intentionally simple — it compares lowercased word sets.
func fuzzyMatch(a, b string) bool {
	wordsA := wordSet(a)
	wordsB := wordSet(b)
	if len(wordsA) == 0 || len(wordsB) == 0 {
		return false
	}
	shared := 0
	for w := range wordsA {
		if wordsB[w] {
			shared++
		}
	}
	// Require >50% overlap on the smaller set.
	smaller := len(wordsA)
	if len(wordsB) < smaller {
		smaller = len(wordsB)
	}
	return float64(shared)/float64(smaller) > 0.5
}

func wordSet(s string) map[string]bool {
	set := make(map[string]bool)
	for _, w := range strings.Fields(strings.ToLower(s)) {
		w = strings.Trim(w, ".,;:!?\"'()")
		if len(w) > 1 {
			set[w] = true
		}
	}
	return set
}

// hashSection returns a truncated hex digest for a spec section string.
// Uses a simple djb2 for zero-alloc hashing — the hash only needs to be
// stable within a single session for matching, not cryptographically secure.
func hashSection(s string) string {
	const seed = 5381
	var h uint64 = seed
	for _, c := range s {
		h = ((h << 5) + h) + uint64(c)
	}
	return fmt.Sprintf("djb2:%016x", h)
}

// unwrapError is a minimal unwrap helper that works with the stdlib
// Unwrap() convention without importing errors package.
func unwrapError(err error) error {
	type wrapper interface {
		Unwrap() error
	}
	if w, ok := err.(wrapper); ok {
		return w.Unwrap()
	}
	return nil
}
