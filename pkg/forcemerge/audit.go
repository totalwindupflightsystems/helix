// Package forcemerge records and audits every Helix PR merge that used
// the `force-merge` label — the operator override that lets a human
// merge a PR without the co-approval gate (1 human + 1 trusted agent).
//
// Per specs/SPECIFICATION.md §5.4:
//
//	"Human can force-merge without agent approval by applying the
//	`force-merge` label. This is logged in the audit trail with human
//	identity and justification comment. Use sparingly — defeats the
//	co-approval invariant. Agent can NEVER force-merge. No override
//	exists for agents. `force-merge` triggers a post-merge review by
//	Conscientiousness (was the override justified?)."
//
// And §6.6 (operational hardening):
//
//	"`force-merge` label usage reviewed monthly (should be rare)"
//
// This package provides the data layer: a JSONL append-only audit log
// of every force-merge, the Conscientiousness bridge that records the
// post-merge review verdict, and the monthly aggregation report used by
// the §6.6 review.
//
// Design goals:
//
//   - Append-only. Each merge writes one JSONL record; we never
//     rewrite history. The Conscientiousness verdict is a separate
//     record that references the merge record by PR URL + merge SHA.
//
//   - Validation at the boundary. Justification text is required
//     (≥20 chars per spec §5.4 spirit — "Use sparingly" implies a
//     real explanation). Empty or short strings are rejected before
//     the record is appended.
//
//   - AuditReport is a pure function over the JSONL. It never mutates
//     the log; it only reads and aggregates. The cron job that drives
//     the monthly review reads the log and calls AuditReport.
//
//   - The store is a file under ~/.helix/forcemerge-audit.jsonl by
//     default, but callers can pass any io.Writer (e.g. tests use
//     bytes.Buffer; ops can point at /var/log/helix-forcemerge.jsonl).
//
// Threading: AuditStore is safe for concurrent Record* calls (a sync.Mutex
// protects the underlying writer). Reads via AuditReport do not lock —
// callers should snapshot the file or pass a snapshot Reader.
package forcemerge

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// =============================================================================
// Constants
// =============================================================================

// DefaultJustificationMinLen is the minimum length of the human-supplied
// justification comment. Spec §5.4 doesn't pin a number but "Use
// sparingly — defeats the co-approval invariant" implies a real
// explanation, not a one-word "ok". 20 chars is the floor.
const DefaultJustificationMinLen = 20

// DefaultAuditPath is the canonical location of the JSONL audit log
// when callers don't supply their own path.
const DefaultAuditPath = "~/.helix/forcemerge-audit.jsonl"

// MaxJustificationLen caps the justification to prevent pathological
// pastes from blowing up the log file.
const MaxJustificationLen = 2000

// =============================================================================
// Labels
// =============================================================================

// ReviewStatus is the state of the post-merge Conscientiousness review.
type ReviewStatus string

const (
	// ReviewPending means the merge happened but Conscientiousness has
	// not yet returned a verdict.
	ReviewPending ReviewStatus = "PENDING"
	// ReviewPassed means Conscientiousness judged the override justified.
	ReviewPassed ReviewStatus = "PASSED"
	// ReviewFailed means Conscientiousness judged the override NOT
	// justified (the merge was inappropriate). The post-merge review
	// is the §5.4 retroactive check.
	ReviewFailed ReviewStatus = "FAILED"
)

// IsValid returns true if r is one of the three known review states.
func (r ReviewStatus) IsValid() bool {
	switch r {
	case ReviewPending, ReviewPassed, ReviewFailed:
		return true
	}
	return false
}

// ForceMergeLabel is the literal label string operators apply to a PR
// to trigger the override. Spec §5.4 mandates this exact name.
const ForceMergeLabel = "force-merge"

// HasForceMergeLabel returns true if labels contains ForceMergeLabel.
// Labels are matched exactly (case-insensitive). Whitespace-tolerant.
func HasForceMergeLabel(labels []string) bool {
	for _, l := range labels {
		if strings.EqualFold(strings.TrimSpace(l), ForceMergeLabel) {
			return true
		}
	}
	return false
}

// =============================================================================
// Entry types
// =============================================================================

// AuditEntry is one JSONL record in the force-merge audit log.
// Append-only — never mutate fields after Marshal.
type AuditEntry struct {
	// PRURL is the canonical Forgejo URL (https://forgejo.example.com/owner/repo/pulls/123).
	PRURL string `json:"pr_url"`

	// HumanIdentity is the username of the human who applied the label
	// (i.e. the human who is bypassing the co-approval gate).
	HumanIdentity string `json:"human_identity"`

	// Justification is the human-supplied explanation. Required,
	// ≥DefaultJustificationMinLen, ≤MaxJustificationLen.
	Justification string `json:"justification"`

	// MergeSHA is the commit SHA that landed on main. Used by the
	// audit trail to correlate with git history.
	MergeSHA string `json:"merge_sha"`

	// Timestamp records when the merge happened (RFC3339Nano UTC).
	Timestamp string `json:"timestamp"`

	// Repo identifies the Forgejo repo (owner/name). Empty if PR URL
	// is empty.
	Repo string `json:"repo,omitempty"`
}

// ReviewEntry is the Conscientiousness post-merge verdict. One
// ReviewEntry references one AuditEntry via PRURL + MergeSHA. Review
// records are appended in the same JSONL stream as audit records
// (distinguished by the presence of the "verdict" key).
type ReviewEntry struct {
	// PRURL is the canonical Forgejo URL of the PR.
	PRURL string `json:"pr_url"`

	// MergeSHA is the commit SHA the review covers.
	MergeSHA string `json:"merge_sha"`

	// Reviewer is the Conscientiousness model that produced the verdict
	// (e.g. "conscientiousness/gpt-5").
	Reviewer string `json:"reviewer"`

	// Status is one of ReviewPending / ReviewPassed / ReviewFailed.
	Status ReviewStatus `json:"status"`

	// Reason is the free-text explanation from the Conscientiousness
	// review (typically a multi-sentence justification).
	Reason string `json:"reason"`

	// Confidence is the model's confidence in its verdict (0-100).
	// Below 40 → the override is logged as ambiguous and routed for
	// human follow-up review.
	Confidence int `json:"confidence"`

	// Timestamp records when the review happened (RFC3339Nano UTC).
	Timestamp string `json:"timestamp"`
}

// =============================================================================
// Validation
// =============================================================================

// ValidateJustification returns an error if s is empty, too short, or
// too long. The min length is enforced at write time; this helper lets
// callers pre-validate before constructing an entry.
func ValidateJustification(s string) error {
	t := strings.TrimSpace(s)
	if t == "" {
		return errors.New("forcemerge: justification is required")
	}
	if len(t) < DefaultJustificationMinLen {
		return fmt.Errorf("forcemerge: justification too short (%d chars, min %d)",
			len(t), DefaultJustificationMinLen)
	}
	if len(t) > MaxJustificationLen {
		return fmt.Errorf("forcemerge: justification too long (%d chars, max %d)",
			len(t), MaxJustificationLen)
	}
	return nil
}

// ValidateAuditEntry enforces the required-field contract for an
// AuditEntry. Used by AuditStore.Record before appending.
func ValidateAuditEntry(e AuditEntry) error {
	if strings.TrimSpace(e.PRURL) == "" {
		return errors.New("forcemerge: PRURL is required")
	}
	if strings.TrimSpace(e.HumanIdentity) == "" {
		return errors.New("forcemerge: human_identity is required")
	}
	if err := ValidateJustification(e.Justification); err != nil {
		return err
	}
	if strings.TrimSpace(e.MergeSHA) == "" {
		return errors.New("forcemerge: merge_sha is required")
	}
	if _, err := time.Parse(time.RFC3339Nano, e.Timestamp); err != nil {
		return fmt.Errorf("forcemerge: timestamp must be RFC3339Nano: %w", err)
	}
	return nil
}

// ValidateReviewEntry enforces the contract for a ReviewEntry.
func ValidateReviewEntry(e ReviewEntry) error {
	if strings.TrimSpace(e.PRURL) == "" {
		return errors.New("forcemerge: PRURL is required")
	}
	if strings.TrimSpace(e.MergeSHA) == "" {
		return errors.New("forcemerge: merge_sha is required")
	}
	if !e.Status.IsValid() {
		return fmt.Errorf("forcemerge: invalid review status %q", e.Status)
	}
	if e.Confidence < 0 || e.Confidence > 100 {
		return fmt.Errorf("forcemerge: confidence %d out of [0,100]", e.Confidence)
	}
	if _, err := time.Parse(time.RFC3339Nano, e.Timestamp); err != nil {
		return fmt.Errorf("forcemerge: timestamp must be RFC3339Nano: %w", err)
	}
	return nil
}

// =============================================================================
// AuditStore
// =============================================================================

// AuditStore is the append-only writer for the force-merge log.
// Thread-safe.
type AuditStore struct {
	mu sync.Mutex

	// path is the underlying file (when NewFileStore was used) or
	// empty (when NewWriterStore was used).
	path string
	w    io.Writer

	// closes is set when NewFileStore opened the file. The store will
	// close it on Close().
	closer io.Closer
}

// NewWriterStore builds an AuditStore that writes to w. The store does
// NOT close w on Close() — the caller owns the writer.
func NewWriterStore(w io.Writer) *AuditStore {
	return &AuditStore{w: w}
}

// NewFileStore opens path in append mode (creating if missing) with
// mode 0o600 (umask respected). The returned store closes the file on
// Close(). Errors opening the file are returned directly.
func NewFileStore(path string) (*AuditStore, error) {
	expanded, err := ExpandPath(path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(expanded), 0o755); err != nil {
		return nil, fmt.Errorf("forcemerge: mkdir %s: %w", filepath.Dir(expanded), err)
	}
	f, err := os.OpenFile(expanded, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("forcemerge: open %s: %w", expanded, err)
	}
	return &AuditStore{
		path:   expanded,
		w:      f,
		closer: f,
	}, nil
}

// ExpandPath expands a leading "~/" to the user's home directory. Other
// paths are returned unchanged. Errors only when HOME is unset.
func ExpandPath(p string) (string, error) {
	if !strings.HasPrefix(p, "~") {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("forcemerge: cannot expand %q: %w", p, err)
	}
	if p == "~" {
		return home, nil
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:]), nil
	}
	// "~user/..." form is intentionally not supported — the platform
	// runs as a single user per host.
	return p, nil
}

// Path returns the underlying file path, or "" if the store was built
// via NewWriterStore.
func (s *AuditStore) Path() string {
	return s.path
}

// Close releases any resources owned by the store (the underlying file
// when NewFileStore was used). Idempotent; safe to call on a writer
// store (no-op).
func (s *AuditStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closer != nil {
		err := s.closer.Close()
		s.closer = nil
		return err
	}
	return nil
}

// RecordAudit appends e as one JSONL line. Validates first; returns an
// error if any required field is missing.
func (s *AuditStore) RecordAudit(e AuditEntry) error {
	if err := ValidateAuditEntry(e); err != nil {
		return err
	}
	return s.appendJSON(e)
}

// RecordReview appends r as one JSONL line. Validates first.
func (s *AuditStore) RecordReview(r ReviewEntry) error {
	if err := ValidateReviewEntry(r); err != nil {
		return err
	}
	return s.appendJSON(r)
}

// appendJSON marshals v and writes it as a single JSONL line.
func (s *AuditStore) appendJSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("forcemerge: marshal: %w", err)
	}
	data = append(data, '\n')

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.w.Write(data); err != nil {
		return fmt.Errorf("forcemerge: write: %w", err)
	}
	return nil
}

// =============================================================================
// AuditReport — pure aggregation
// =============================================================================

// AuditReport is the monthly review per spec §6.6. Built by reading the
// JSONL log and aggregating by month. Pure function over the input.
type AuditReport struct {
	// Total merges recorded in the period.
	TotalMerges int `json:"total_merges"`

	// ByMonth maps "YYYY-MM" → MonthlyStats for that month.
	ByMonth map[string]MonthlyStats `json:"by_month"`

	// HumansByMonth maps "YYYY-MM" → sorted list of human identities
	// that used force-merge in that month (most-frequent first).
	HumansByMonth map[string][]HumanUsage `json:"humans_by_month"`

	// PendingReviewCount is the number of audit entries with no
	// matching review (or a matching review with status PENDING).
	// These are the merges waiting on Conscientiousness.
	PendingReviewCount int `json:"pending_review_count"`

	// FailedReviewCount is the number of reviews with status FAILED
	// in the period — merges where Conscientiousness judged the
	// override inappropriate.
	FailedReviewCount int `json:"failed_review_count"`
}

// MonthlyStats aggregates the four counts operators check monthly.
type MonthlyStats struct {
	Merges         int `json:"merges"`
	PassedReviews  int `json:"passed_reviews"`
	FailedReviews  int `json:"failed_reviews"`
	PendingReviews int `json:"pending_reviews"`
}

// HumanUsage counts how many times one human used the override. Used
// for "should be rare" reporting in §6.6.
type HumanUsage struct {
	Human string `json:"human"`
	Count int    `json:"count"`
}

// BuildAuditReport reads r (a JSONL stream of audit + review records)
// and returns the aggregated report. Empty input → zero-valued report
// with empty maps. Malformed lines are skipped (the log is
// best-effort-readable per the JSONL convention).
//
// The `now` parameter determines "this month" for the report's primary
// month grouping. Pass time.Now().UTC() in production.
//
// Implementation: two-pass. Pass 1 collects every ReviewEntry keyed by
// mergeKey(PRURL, MergeSHA). Pass 2 walks the audit entries and looks
// up the matching review. This makes the report order-independent —
// reviews can precede or follow their audit entries in the JSONL.
func BuildAuditReport(r io.Reader, now time.Time) (AuditReport, error) {
	month := now.UTC().Format("2006-01")
	rep := AuditReport{
		ByMonth:       map[string]MonthlyStats{},
		HumansByMonth: map[string][]HumanUsage{},
	}
	humanCount := map[string]map[string]int{} // month → human → count

	// Read every line into memory so we can iterate twice. Lines are
	// small (≤MaxJustificationLen + JSON overhead) so this is bounded.
	type record struct {
		kind string // "audit" or "review"
		raw  []byte
	}
	var records []record

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // 1 MiB cap per line

	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		kind, err := detectRecordKind(string(line))
		if err != nil {
			continue // malformed — skip
		}
		records = append(records, record{kind: kind, raw: append([]byte(nil), line...)})
	}
	if err := scanner.Err(); err != nil {
		return rep, fmt.Errorf("forcemerge: scan: %w", err)
	}

	// Pass 1: collect reviews.
	reviews := map[string]ReviewEntry{}
	for _, rec := range records {
		if rec.kind != "review" {
			continue
		}
		var rev ReviewEntry
		if err := json.Unmarshal(rec.raw, &rev); err != nil {
			continue
		}
		reviews[mergeKey(rev.PRURL, rev.MergeSHA)] = rev
	}

	// Pass 2: walk audits and aggregate.
	for _, rec := range records {
		if rec.kind != "audit" {
			continue
		}
		var e AuditEntry
		if err := json.Unmarshal(rec.raw, &e); err != nil {
			continue
		}
		rep.TotalMerges++
		entryMonth := parseMonth(e.Timestamp)
		if entryMonth == "" {
			entryMonth = month
		}
		ms := rep.ByMonth[entryMonth]
		ms.Merges++

		if rev, ok := reviews[mergeKey(e.PRURL, e.MergeSHA)]; ok {
			switch rev.Status {
			case ReviewPassed:
				ms.PassedReviews++
			case ReviewFailed:
				ms.FailedReviews++
				rep.FailedReviewCount++
			case ReviewPending:
				ms.PendingReviews++
			}
		} else {
			ms.PendingReviews++
			rep.PendingReviewCount++
		}
		rep.ByMonth[entryMonth] = ms

		if humanCount[entryMonth] == nil {
			humanCount[entryMonth] = map[string]int{}
		}
		humanCount[entryMonth][e.HumanIdentity]++
	}

	// Build HumansByMonth (sorted by count desc, then name asc).
	for m, counts := range humanCount {
		usages := make([]HumanUsage, 0, len(counts))
		for h, c := range counts {
			usages = append(usages, HumanUsage{Human: h, Count: c})
		}
		sortByCountThenName(usages)
		rep.HumansByMonth[m] = usages
	}

	return rep, nil
}

// detectRecordKind peeks at the first JSON key. "pr_url" + "merge_sha" +
// "reviewer" → review. "pr_url" + "merge_sha" + "human_identity" → audit.
func detectRecordKind(line string) (string, error) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal([]byte(line), &probe); err != nil {
		return "", err
	}
	if _, hasReviewer := probe["reviewer"]; hasReviewer {
		return "review", nil
	}
	if _, hasHuman := probe["human_identity"]; hasHuman {
		return "audit", nil
	}
	return "", fmt.Errorf("unknown record shape")
}

func mergeKey(prURL, sha string) string {
	return prURL + "@" + sha
}

func parseMonth(ts string) string {
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return ""
	}
	return t.UTC().Format("2006-01")
}

// sortByCountThenName sorts a slice of HumanUsage in-place by count
// (descending) then human name (ascending) for stable output.
func sortByCountThenName(s []HumanUsage) {
	// Insertion sort — small N, allocation-free.
	for i := 1; i < len(s); i++ {
		j := i
		for j > 0 && (s[j].Count > s[j-1].Count ||
			(s[j].Count == s[j-1].Count && s[j].Human < s[j-1].Human)) {
			s[j], s[j-1] = s[j-1], s[j]
			j--
		}
	}
}

// FormatReport renders rep as a human-readable multi-line string. The
// output is suitable for `cat`-friendly operator review (not a
// machine-readable format — use rep's JSON form for that).
func FormatReport(rep AuditReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Force-merge audit — total merges: %d\n", rep.TotalMerges)
	fmt.Fprintf(&b, "Pending reviews: %d | Failed reviews: %d\n",
		rep.PendingReviewCount, rep.FailedReviewCount)

	if len(rep.ByMonth) == 0 {
		fmt.Fprintln(&b, "(no records)")
		return b.String()
	}

	// Stable order: months ascending.
	months := make([]string, 0, len(rep.ByMonth))
	for m := range rep.ByMonth {
		months = append(months, m)
	}
	sortStrings(months)

	for _, m := range months {
		ms := rep.ByMonth[m]
		fmt.Fprintf(&b, "\n%s  merges=%d  passed=%d  failed=%d  pending=%d\n",
			m, ms.Merges, ms.PassedReviews, ms.FailedReviews, ms.PendingReviews)
		if usages, ok := rep.HumansByMonth[m]; ok {
			for _, u := range usages {
				fmt.Fprintf(&b, "  %s × %d\n", u.Human, u.Count)
			}
		}
	}
	return b.String()
}

// sortStrings sorts a slice of strings in-place using insertion sort.
// Stdlib sort.Strings would also work; the local helper keeps the
// package's imports focused.
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		j := i
		for j > 0 && s[j] < s[j-1] {
			s[j], s[j-1] = s[j-1], s[j]
			j--
		}
	}
}
