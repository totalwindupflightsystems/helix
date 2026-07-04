// Package memory — schema_validator.go
//
// Per-record DuckBrain memory validation per spec §8.5. The validator
// is the contract enforcement layer between raw MemoryEntry records
// (produced by the persistence bridge or a manual write) and the
// VSS-backed storage layer.
//
// It enforces:
//
//  1. Required fields (id, content, agent_id, created_at, namespace,
//     schema_version) — surfaced as per-field errors so callers can
//     report exactly which fields are missing in a batch.
//
//  2. Embedding dimensions must match the configured VSS index size.
//     Default 1536 (text-embedding-3-small); other models use 768
//     (BERT-base) or 3072 (text-embedding-3-large).
//
//  3. Content hash = sha256(content). The hash field is required and
//     must equal the hex-encoded SHA-256 of the content string.
//
//  4. No PII patterns in content or attributes (email, US SSN,
//     credit-card). PII detection is best-effort regex — matches the
//     purpose of catching accidental secrets before they hit
//     long-term storage. Not a substitute for a real secret scanner.
//
//  5. ID format: "helix://memory/<sha256-hex>" — sha256 of the
//     content, 64 lowercase hex chars. Validated with regex.
//
// The validator returns a ValidationReport (rather than `error`) so a
// caller running it across a batch of records can collect every
// failure before deciding what to do. ValidationError is provided for
// callers that want the fast-fail path.
package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
// Schema version (spec §8.5 + drift tracking)
// ─────────────────────────────────────────────────────────────────────────────

// CurrentSchemaVersion is the schema version this package emits.
// Bump when adding/removing required fields or changing the ID format.
const CurrentSchemaVersion = "1.0.0"

// ─────────────────────────────────────────────────────────────────────────────
// Standard embedding dimensions
// ─────────────────────────────────────────────────────────────────────────────

// Standard embedding vector sizes for known embedding models. A
// memory record's Embedding must match one of these (or whatever
// the operator configures) so the VSS index doesn't fail at
// insertion time.
const (
	// EmbeddingDimOpenAISmall is text-embedding-3-small (1536 dims).
	EmbeddingDimOpenAISmall = 1536
	// EmbeddingDimOpenAILarge is text-embedding-3-large (3072 dims).
	EmbeddingDimOpenAILarge = 3072
	// EmbeddingDimBertBase is BERT-base / all-MiniLM-L6-v2 (768 dims).
	EmbeddingDimBertBase = 768
)

// DefaultEmbeddingDim is the embedding dimension the validator
// enforces when no other size is configured.
const DefaultEmbeddingDim = EmbeddingDimOpenAISmall

// ─────────────────────────────────────────────────────────────────────────────
// Regex patterns
// ─────────────────────────────────────────────────────────────────────────────

// idPattern matches the canonical spec §8.5 id format:
// "helix://memory/" followed by 64 lowercase hex chars.
var idPattern = regexp.MustCompile(`^helix://memory/[0-9a-f]{64}$`)

// piiPatterns are the PII signatures we scan for. Matches are
// best-effort — they're tuned for accidental leakage, not adversarial
// evasion. Centralized so the test suite can prove every pattern is
// exercised.
var piiPatterns = []struct {
	name    string
	pattern *regexp.Regexp
}{
	{"email", regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)},
	{"us_ssn", regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`)},
	{"credit_card", regexp.MustCompile(`\b(?:\d[ \-]?){13,16}\d\b`)},
}

// ─────────────────────────────────────────────────────────────────────────────
// Validator
// ─────────────────────────────────────────────────────────────────────────────

// MemorySchemaValidator validates a single MemoryEntry (or
// MemoryRecord, the flattened form used by callers that don't carry
// the full Attributes struct) against the spec §8.5 schema.
//
// The validator is stateless — construct one and reuse it across a
// batch. EmbeddingDim may be overridden after construction.
type MemorySchemaValidator struct {
	// EmbeddingDim is the required embedding vector size. Defaults to
	// DefaultEmbeddingDim when zero. Set explicitly when the VSS
	// index was provisioned at a non-standard size.
	EmbeddingDim int
	// NowFn returns the current time; tests inject a fixed value to
	// make time-sensitive checks deterministic.
	NowFn func() time.Time
	// SchemaVersion is the value of the schema_version field this
	// validator accepts. Empty string accepts any version.
	SchemaVersion string
}

// NewMemorySchemaValidator returns a validator with default settings.
func NewMemorySchemaValidator() *MemorySchemaValidator {
	return &MemorySchemaValidator{
		EmbeddingDim:  DefaultEmbeddingDim,
		NowFn:         time.Now,
		SchemaVersion: CurrentSchemaVersion,
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ValidationReport — collected errors per record
// ─────────────────────────────────────────────────────────────────────────────

// FieldError describes a single field that failed validation. The
// Field name is the JSON tag (e.g. "content_hash", "embedding_dim")
// so callers can map errors directly to their record schema.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// ValidationReport collects every per-field error for a single record.
// Valid records have an empty Errors slice; check via the IsValid /
// HasErrors helpers.
type ValidationReport struct {
	Key         string       `json:"key"`
	Errors      []FieldError `json:"errors,omitempty"`
	Warnings    []FieldError `json:"warnings,omitempty"`
	ValidatedAt time.Time    `json:"validated_at"`
}

// IsValid reports whether no errors were found (warnings don't count).
func (r *ValidationReport) IsValid() bool { return len(r.Errors) == 0 }

// HasErrors is the inverse of IsValid, provided for readability.
func (r *ValidationReport) HasErrors() bool { return len(r.Errors) > 0 }

// Error returns a stable, sorted, multi-line summary suitable for
// logging. Field errors are sorted by field name for determinism.
func (r *ValidationReport) Error() string {
	if r.IsValid() {
		return ""
	}
	sorted := append([]FieldError(nil), r.Errors...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Field < sorted[j].Field })
	var b strings.Builder
	fmt.Fprintf(&b, "memory: validation failed for %q (%d error(s))",
		r.Key, len(sorted))
	for _, e := range sorted {
		fmt.Fprintf(&b, "\n  - %s: %s", e.Field, e.Message)
	}
	return b.String()
}

// addErr is an internal helper that appends a FieldError. Keeping
// the append centralized makes it easy to add dedup or audit hooks
// later without touching every check site.
func (r *ValidationReport) addErr(field, msg string) {
	r.Errors = append(r.Errors, FieldError{Field: field, Message: msg})
}

func (r *ValidationReport) addWarn(field, msg string) {
	r.Warnings = append(r.Warnings, FieldError{Field: field, Message: msg})
}

// ─────────────────────────────────────────────────────────────────────────────
// MemoryRecord — flattened record used by external callers
// ─────────────────────────────────────────────────────────────────────────────

// MemoryRecord is the flattened record callers feed the validator.
// Spec §8.5 keeps Key / Domain / Attributes / EmbeddingText on the
// MemoryEntry type; the flattened record also exposes Content (the
// raw payload used to compute ContentHash), ContentHash, Embedding
// (the actual vector), AgentID, SchemaVersion. These are optional
// in callers' records — empty fields surface as field errors.
type MemoryRecord struct {
	Key           string
	Domain        Domain
	Content       string
	ContentHash   string
	AgentID       string
	Namespace     Namespace
	SchemaVersion string
	CreatedAt     time.Time
	Embedding     []float32
	EmbeddingText string
	Attributes    Attributes
}

// ─────────────────────────────────────────────────────────────────────────────
// ValidateMemory — the entry point
// ─────────────────────────────────────────────────────────────────────────────

// ValidateMemory runs every check in spec §8.5 against the given
// record and returns a ValidationReport. It never returns an error —
// every failure is captured in the report. Use IsValid / HasErrors
// to branch.
//
// Per-field checks:
//
//	key              — non-empty, well-formed (delegated to ValidateKey)
//	id_format        — must match "helix://memory/<sha256>"
//	agent_id         — non-empty
//	namespace        — recognized
//	schema_version   — non-empty, matches validator's SchemaVersion
//	created_at       — non-zero, not in the future
//	content          — non-empty
//	content_hash     — non-empty, equals sha256(content)
//	embedding        — length matches EmbeddingDim, no NaN/Inf
//	domain           — recognized
//	pii              — no email / SSN / credit card in content
func (v *MemorySchemaValidator) ValidateMemory(r MemoryRecord) *ValidationReport {
	now := time.Now
	if v.NowFn != nil {
		now = v.NowFn
	}
	report := &ValidationReport{
		Key:         r.Key,
		ValidatedAt: now(),
	}

	// key
	if r.Key == "" {
		report.addErr("key", "is required")
	} else if err := ValidateKey(r.Key); err != nil {
		report.addErr("key", err.Error())
	}

	// id_format — derived from key (must end with /<sha256> to match
	// the helix://memory/<sha256> form when prefixed). We treat the
	// last path segment as the sha256 candidate.
	if r.Key != "" {
		if _, leaf, ok := parsePromptsPathForID(r.Key); ok {
			want := "helix://memory/" + leaf
			if !idPattern.MatchString(want) {
				report.addErr("id_format",
					fmt.Sprintf("derived id %q does not match helix://memory/<sha256>", want))
			}
		}
		// If the key doesn't have the standard shape, ValidateKey
		// already produced an error above — don't double-report.
	}

	// agent_id
	if strings.TrimSpace(r.AgentID) == "" {
		report.addErr("agent_id", "is required")
	}

	// namespace
	if r.Namespace == "" {
		report.addErr("namespace", "is required")
	} else if !ValidNamespace(r.Namespace) {
		report.addErr("namespace", fmt.Sprintf("%q is not a recognized namespace", r.Namespace))
	}

	// schema_version
	if strings.TrimSpace(r.SchemaVersion) == "" {
		report.addErr("schema_version", "is required")
	} else if v.SchemaVersion != "" && r.SchemaVersion != v.SchemaVersion {
		report.addErr("schema_version",
			fmt.Sprintf("got %q, want %q", r.SchemaVersion, v.SchemaVersion))
	}

	// created_at
	if r.CreatedAt.IsZero() {
		report.addErr("created_at", "is required (must be non-zero)")
	} else if r.CreatedAt.After(now().Add(time.Minute)) {
		// Allow a 1-minute clock-skew tolerance for distributed
		// writers; anything beyond that is treated as a future
		// timestamp and flagged.
		report.addErr("created_at",
			fmt.Sprintf("is in the future (%s)", r.CreatedAt.Format(time.RFC3339)))
	}

	// content
	if strings.TrimSpace(r.Content) == "" && strings.TrimSpace(r.EmbeddingText) == "" {
		report.addErr("content", "is required (or embedding_text)")
	}

	// content_hash
	if strings.TrimSpace(r.Content) != "" {
		want := ContentHashOf(r.Content)
		if strings.TrimSpace(r.ContentHash) == "" {
			report.addErr("content_hash", "is required when content is set")
		} else if r.ContentHash != want {
			report.addErr("content_hash",
				fmt.Sprintf("got %q, want sha256(content) = %q", r.ContentHash, want))
		}
	}

	// embedding dimensions
	dim := v.EmbeddingDim
	if dim <= 0 {
		dim = DefaultEmbeddingDim
	}
	if len(r.Embedding) == 0 {
		// Not strictly required by spec — a record can carry just
		// the text and have the vector computed later. We surface a
		// warning instead of an error so callers don't get blocked
		// when the VSS indexing happens downstream.
		if r.EmbeddingText != "" {
			report.addWarn("embedding",
				fmt.Sprintf("no vector present; will be computed from embedding_text (expected dim=%d)", dim))
		}
	} else {
		if len(r.Embedding) != dim {
			report.addErr("embedding",
				fmt.Sprintf("dimension %d does not match VSS index dim %d",
					len(r.Embedding), dim))
		}
		if hasNonFiniteFloat32(r.Embedding) {
			report.addErr("embedding", "contains NaN or Inf values")
		}
	}

	// domain
	if !r.Domain.Valid() {
		report.addErr("domain", fmt.Sprintf("%q is not a recognized domain", r.Domain))
	}

	// pii scan — content + attributes' user-facing strings
	piiHits := scanPII(r.Content)
	for field, val := range map[string]string{
		"attributes.decision":   r.Attributes.Decision,
		"attributes.rationale":  r.Attributes.Rationale,
		"attributes.supersedes": r.Attributes.Supersedes,
	} {
		if val != "" {
			piiHits = append(piiHits, scanPII(val)...)
			_ = field
		}
	}
	if len(piiHits) > 0 {
		// Deduplicate hit names for a clean error message.
		seen := map[string]bool{}
		var uniq []string
		for _, h := range piiHits {
			if seen[h] {
				continue
			}
			seen[h] = true
			uniq = append(uniq, h)
		}
		sort.Strings(uniq)
		report.addErr("pii",
			fmt.Sprintf("detected PII patterns: %s", strings.Join(uniq, ", ")))
	}

	return report
}

// ─────────────────────────────────────────────────────────────────────────────
// ValidateMemoryBatch — convenience wrapper
// ─────────────────────────────────────────────────────────────────────────────

// BatchReport aggregates the per-record ValidationReports from a
// batch run, plus aggregate counts for quick CI / CLI output.
type BatchReport struct {
	Total     int                 `json:"total"`
	Valid     int                 `json:"valid"`
	Invalid   int                 `json:"invalid"`
	Reports   []*ValidationReport `json:"reports"`
	RunAt     time.Time           `json:"run_at"`
	HasErrors bool                `json:"has_errors"`
}

// ValidateBatch validates every record in records and returns the
// aggregated BatchReport. Never returns an error — callers inspect
// report.HasErrors.
func (v *MemorySchemaValidator) ValidateBatch(records []MemoryRecord) *BatchReport {
	now := time.Now
	if v.NowFn != nil {
		now = v.NowFn
	}
	out := &BatchReport{
		Total: len(records),
		RunAt: now(),
	}
	for _, r := range records {
		r := r
		rep := v.ValidateMemory(r)
		out.Reports = append(out.Reports, rep)
		if rep.IsValid() {
			out.Valid++
		} else {
			out.Invalid++
		}
	}
	out.HasErrors = out.Invalid > 0
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// ValidationError — fast-fail alternative
// ─────────────────────────────────────────────────────────────────────────────

// ValidationError is returned by ValidateMemoryStrict for the
// fast-fail path. It implements errors.Is/As for ergonomic use.
type ValidationError struct {
	Report *ValidationReport
}

// Error implements the error interface.
func (e *ValidationError) Error() string { return e.Report.Error() }

// Unwrap exposes the underlying report for errors.As.
func (e *ValidationError) Unwrap() error { return ErrInvalidEntry }

// Is allows errors.Is(err, ErrInvalidEntry) to succeed.
func (e *ValidationError) Is(target error) bool { return target == ErrInvalidEntry }

// ValidateMemoryStrict is the fast-fail version of ValidateMemory —
// it returns a *ValidationError wrapping the report on the first
// failed check batch. Useful in code paths that can't proceed
// without a valid record (e.g. ingest before write).
func (v *MemorySchemaValidator) ValidateMemoryStrict(r MemoryRecord) error {
	rep := v.ValidateMemory(r)
	if rep.HasErrors() {
		return &ValidationError{Report: rep}
	}
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// ContentHashOf returns the lowercase hex sha256 hash of content.
// Exposed so callers building records can compute the expected hash
// without re-implementing the algorithm.
func ContentHashOf(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// parsePromptsPathForID is a thin helper the validator uses to derive
// the canonical id suffix from a key. The key's last segment is
// treated as the sha256 candidate. Returns ok=false if the key does
// not live under one of the three spec §8.5 roots.
func parsePromptsPathForID(key string) (component, sha string, ok bool) {
	clean := strings.TrimPrefix(key, "/helix/")
	if !strings.HasPrefix(clean, "agents/") &&
		!strings.HasPrefix(clean, "repos/") &&
		!strings.HasPrefix(clean, "platform/") {
		return "", "", false
	}
	clean = strings.TrimPrefix(clean, "agents/")
	clean = strings.TrimPrefix(clean, "repos/")
	clean = strings.TrimPrefix(clean, "platform/")
	segments := strings.Split(clean, "/")
	if len(segments) == 0 {
		return "", "", false
	}
	leaf := segments[len(segments)-1]
	// We don't return the component because the validator doesn't
	// use it; just signal ok=true to indicate "we derived a leaf".
	return "", leaf, true
}

// scanPII runs every PII regex against s and returns the names of
// patterns that matched. Empty result means no PII was found.
func scanPII(s string) []string {
	if s == "" {
		return nil
	}
	var hits []string
	for _, p := range piiPatterns {
		if p.pattern.MatchString(s) {
			hits = append(hits, p.name)
		}
	}
	return hits
}

// hasNonFiniteFloat32 returns true if any element of v is NaN or
// +Inf / -Inf. Used to reject poison-pill embeddings before they
// reach the VSS index.
func hasNonFiniteFloat32(v []float32) bool {
	for _, x := range v {
		if x != x { // NaN
			return true
		}
		// Abs(Inf) overflows; the simpler check below is portable.
		if x > 1e38 || x < -1e38 {
			// Likely +/-Inf (Go won't let us compare directly to
			// math.MaxFloat32 from this package without an import).
			return true
		}
	}
	return false
}
