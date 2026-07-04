// Package memory — schema_validator_test.go
//
// Covers every check performed by MemorySchemaValidator:
//   - Required fields (key, agent_id, namespace, schema_version,
//     created_at, content)
//   - Key well-formedness (delegated to ValidateKey)
//   - ID format derived from key
//   - Content hash verification (sha256(content))
//   - Embedding dimensions + non-finite value detection
//   - Domain recognition
//   - PII detection (email, SSN, credit card)
//   - Schema version matching
//   - Future timestamp detection
//   - Batch aggregation (Valid/Invalid/HasErrors counts)
//   - Strict-mode ValidationError (errors.Is/As compatibility)

package memory

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
)

// makeValidRecord returns a record that passes every check. Tests
// mutate a single field to drive the corresponding error.
func makeValidRecord() MemoryRecord {
	return MemoryRecord{
		Key:           "/helix/agents/sandbox-7/decisions/" + ContentHashOf("hello world"),
		Domain:        DomainConcept,
		Content:       "hello world",
		ContentHash:   ContentHashOf("hello world"),
		AgentID:       "sandbox-7",
		Namespace:     NSAgentsDecisions,
		SchemaVersion: CurrentSchemaVersion,
		CreatedAt:     time.Date(2026, 7, 4, 10, 0, 0, 0, time.UTC),
		Embedding:     makeEmbedding(DefaultEmbeddingDim, 0.1),
		EmbeddingText: "hello world",
		Attributes: Attributes{
			Decision:  "use foo",
			Rationale: "because reasons",
		},
	}
}

// makeEmbedding builds a slice of n floats seeded with the supplied
// base value (then increments each element so they're all distinct
// and finite).
func makeEmbedding(n int, seed float32) []float32 {
	out := make([]float32, n)
	for i := range out {
		out[i] = seed + float32(i)*1e-5
	}
	return out
}

// ---------------------------------------------------------------------------
// Happy path
// ---------------------------------------------------------------------------

func TestValidator_HappyPath(t *testing.T) {
	v := NewMemorySchemaValidator()
	rep := v.ValidateMemory(makeValidRecord())
	if rep.HasErrors() {
		t.Fatalf("expected valid record; got errors:\n%s", rep.Error())
	}
}

func TestValidator_DefaultEmbeddingDimIs1536(t *testing.T) {
	if DefaultEmbeddingDim != 1536 {
		t.Errorf("DefaultEmbeddingDim = %d, want 1536", DefaultEmbeddingDim)
	}
	if EmbeddingDimOpenAISmall != 1536 {
		t.Errorf("EmbeddingDimOpenAISmall = %d, want 1536", EmbeddingDimOpenAISmall)
	}
}

// ---------------------------------------------------------------------------
// Required field checks
// ---------------------------------------------------------------------------

func TestValidator_MissingKey(t *testing.T) {
	r := makeValidRecord()
	r.Key = ""
	rep := NewMemorySchemaValidator().ValidateMemory(r)
	assertHasFieldError(t, rep, "key")
}

func TestValidator_MalformedKey(t *testing.T) {
	r := makeValidRecord()
	r.Key = "/not/helix/path"
	rep := NewMemorySchemaValidator().ValidateMemory(r)
	assertHasFieldError(t, rep, "key")
}

func TestValidator_MissingAgentID(t *testing.T) {
	r := makeValidRecord()
	r.AgentID = ""
	rep := NewMemorySchemaValidator().ValidateMemory(r)
	assertHasFieldError(t, rep, "agent_id")
}

func TestValidator_UnknownNamespace(t *testing.T) {
	r := makeValidRecord()
	r.Namespace = "agents/garbage-ns"
	rep := NewMemorySchemaValidator().ValidateMemory(r)
	assertHasFieldError(t, rep, "namespace")
}

func TestValidator_MissingSchemaVersion(t *testing.T) {
	r := makeValidRecord()
	r.SchemaVersion = ""
	rep := NewMemorySchemaValidator().ValidateMemory(r)
	assertHasFieldError(t, rep, "schema_version")
}

func TestValidator_MismatchedSchemaVersion(t *testing.T) {
	r := makeValidRecord()
	r.SchemaVersion = "0.0.1"
	rep := NewMemorySchemaValidator().ValidateMemory(r)
	if !rep.HasErrors() {
		t.Fatal("expected schema_version mismatch to be flagged")
	}
	assertHasFieldError(t, rep, "schema_version")
}

func TestValidator_MissingCreatedAt(t *testing.T) {
	r := makeValidRecord()
	r.CreatedAt = time.Time{}
	rep := NewMemorySchemaValidator().ValidateMemory(r)
	assertHasFieldError(t, rep, "created_at")
}

func TestValidator_FutureCreatedAt(t *testing.T) {
	r := makeValidRecord()
	r.CreatedAt = time.Now().Add(10 * time.Minute)
	rep := NewMemorySchemaValidator().ValidateMemory(r)
	assertHasFieldError(t, rep, "created_at")
}

func TestValidator_RecentFutureCreatedAt_Tolerated(t *testing.T) {
	// A 30s skew should be tolerated — distributed writers with
	// unsynced clocks need some headroom.
	r := makeValidRecord()
	r.CreatedAt = time.Now().Add(30 * time.Second)
	rep := NewMemorySchemaValidator().ValidateMemory(r)
	assertHasNoFieldError(t, rep, "created_at")
}

func TestValidator_MissingContent(t *testing.T) {
	r := makeValidRecord()
	r.Content = ""
	r.ContentHash = ""
	r.EmbeddingText = ""
	rep := NewMemorySchemaValidator().ValidateMemory(r)
	assertHasFieldError(t, rep, "content")
}

func TestValidator_UnknownDomain(t *testing.T) {
	r := makeValidRecord()
	r.Domain = Domain("garbage")
	rep := NewMemorySchemaValidator().ValidateMemory(r)
	assertHasFieldError(t, rep, "domain")
}

// ---------------------------------------------------------------------------
// Content hash verification
// ---------------------------------------------------------------------------

func TestValidator_MissingContentHash(t *testing.T) {
	r := makeValidRecord()
	r.ContentHash = ""
	rep := NewMemorySchemaValidator().ValidateMemory(r)
	assertHasFieldError(t, rep, "content_hash")
}

func TestValidator_WrongContentHash(t *testing.T) {
	r := makeValidRecord()
	r.ContentHash = "deadbeef" + strings.Repeat("0", 56)
	rep := NewMemorySchemaValidator().ValidateMemory(r)
	assertHasFieldError(t, rep, "content_hash")
}

func TestValidator_CorrectContentHash(t *testing.T) {
	r := makeValidRecord()
	r.Content = "distinct payload"
	r.ContentHash = ContentHashOf("distinct payload")
	rep := NewMemorySchemaValidator().ValidateMemory(r)
	assertHasNoFieldError(t, rep, "content_hash")
}

func TestContentHashOf_KnownVector(t *testing.T) {
	// SHA-256 of "hello world" is well-known.
	got := ContentHashOf("hello world")
	want := "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
	if got != want {
		t.Errorf("ContentHashOf(%q) = %q, want %q", "hello world", got, want)
	}
}

// ---------------------------------------------------------------------------
// Embedding dimensions and values
// ---------------------------------------------------------------------------

func TestValidator_EmbeddingDimMismatch(t *testing.T) {
	r := makeValidRecord()
	r.Embedding = makeEmbedding(768, 0.1) // wrong size
	rep := NewMemorySchemaValidator().ValidateMemory(r)
	assertHasFieldError(t, rep, "embedding")
}

func TestValidator_EmbeddingDimCustom(t *testing.T) {
	v := NewMemorySchemaValidator()
	v.EmbeddingDim = EmbeddingDimBertBase
	r := makeValidRecord()
	r.Embedding = makeEmbedding(EmbeddingDimBertBase, 0.1)
	rep := v.ValidateMemory(r)
	assertHasNoFieldError(t, rep, "embedding")
}

func TestValidator_EmbeddingNaN(t *testing.T) {
	r := makeValidRecord()
	emb := makeEmbedding(DefaultEmbeddingDim, 0.1)
	emb[42] = float32(math.NaN())
	r.Embedding = emb
	rep := NewMemorySchemaValidator().ValidateMemory(r)
	assertHasFieldError(t, rep, "embedding")
}

func TestValidator_EmbeddingInf(t *testing.T) {
	r := makeValidRecord()
	emb := makeEmbedding(DefaultEmbeddingDim, 0.1)
	emb[100] = float32(math.Inf(1))
	r.Embedding = emb
	rep := NewMemorySchemaValidator().ValidateMemory(r)
	assertHasFieldError(t, rep, "embedding")
}

func TestValidator_NoEmbedding_Warning(t *testing.T) {
	r := makeValidRecord()
	r.Embedding = nil
	rep := NewMemorySchemaValidator().ValidateMemory(r)
	if rep.HasErrors() {
		t.Errorf("missing embedding should be a warning, not error:\n%s", rep.Error())
	}
	if len(rep.Warnings) == 0 {
		t.Error("expected a warning for missing embedding")
	}
}

// ---------------------------------------------------------------------------
// ID format
// ---------------------------------------------------------------------------

func TestValidator_IDFormat_OK(t *testing.T) {
	r := makeValidRecord()
	// Key already ends with a sha256 hex digest.
	rep := NewMemorySchemaValidator().ValidateMemory(r)
	assertHasNoFieldError(t, rep, "id_format")
}

func TestValidator_IDFormat_BadLeaf(t *testing.T) {
	r := makeValidRecord()
	r.Key = "/helix/agents/sandbox-7/decisions/not-a-sha256"
	rep := NewMemorySchemaValidator().ValidateMemory(r)
	assertHasFieldError(t, rep, "id_format")
}

// ---------------------------------------------------------------------------
// PII detection
// ---------------------------------------------------------------------------

func TestValidator_PII_Email(t *testing.T) {
	r := makeValidRecord()
	r.Content = "Contact me at user@example.com for details"
	rep := NewMemorySchemaValidator().ValidateMemory(r)
	assertHasFieldError(t, rep, "pii")
}

func TestValidator_PII_SSN(t *testing.T) {
	r := makeValidRecord()
	r.Content = "user SSN 123-45-6789"
	rep := NewMemorySchemaValidator().ValidateMemory(r)
	assertHasFieldError(t, rep, "pii")
}

func TestValidator_PII_CreditCard(t *testing.T) {
	r := makeValidRecord()
	r.Content = "card 4111 1111 1111 1111"
	rep := NewMemorySchemaValidator().ValidateMemory(r)
	assertHasFieldError(t, rep, "pii")
}

func TestValidator_PII_NoHit(t *testing.T) {
	r := makeValidRecord()
	r.Content = "just a normal prompt with no PII whatsoever"
	rep := NewMemorySchemaValidator().ValidateMemory(r)
	assertHasNoFieldError(t, rep, "pii")
}

func TestValidator_PII_InAttributes(t *testing.T) {
	r := makeValidRecord()
	r.Attributes.Decision = "Email user@example.com about the rollback"
	rep := NewMemorySchemaValidator().ValidateMemory(r)
	assertHasFieldError(t, rep, "pii")
}

// ---------------------------------------------------------------------------
// Batch + Report
// ---------------------------------------------------------------------------

func TestValidator_BatchReport(t *testing.T) {
	v := NewMemorySchemaValidator()
	good := makeValidRecord()
	bad := makeValidRecord()
	bad.AgentID = ""
	report := v.ValidateBatch([]MemoryRecord{good, bad, good})
	if report.Total != 3 {
		t.Errorf("Total = %d, want 3", report.Total)
	}
	if report.Valid != 2 {
		t.Errorf("Valid = %d, want 2", report.Valid)
	}
	if report.Invalid != 1 {
		t.Errorf("Invalid = %d, want 1", report.Invalid)
	}
	if !report.HasErrors {
		t.Error("HasErrors should be true")
	}
}

func TestValidator_BatchReport_AllValid(t *testing.T) {
	v := NewMemorySchemaValidator()
	report := v.ValidateBatch([]MemoryRecord{makeValidRecord(), makeValidRecord()})
	if report.HasErrors {
		t.Error("HasErrors should be false for an all-valid batch")
	}
	if report.Invalid != 0 {
		t.Errorf("Invalid = %d, want 0", report.Invalid)
	}
}

// ---------------------------------------------------------------------------
// ValidationReport helpers
// ---------------------------------------------------------------------------

func TestValidationReport_SortedErrors(t *testing.T) {
	r := &ValidationReport{}
	r.addErr("zebra", "z")
	r.addErr("alpha", "a")
	r.addErr("middle", "m")
	out := r.Error()
	if !strings.Contains(out, "alpha") || !strings.Contains(out, "middle") || !strings.Contains(out, "zebra") {
		t.Fatalf("Error() missing fields: %s", out)
	}
	// Ensure stable order: alpha < middle < zebra.
	idxAlpha := strings.Index(out, "alpha")
	idxMiddle := strings.Index(out, "middle")
	idxZebra := strings.Index(out, "zebra")
	if !(idxAlpha < idxMiddle && idxMiddle < idxZebra) {
		t.Errorf("errors not sorted alphabetically: %s", out)
	}
}

func TestValidationReport_EmptyMessage(t *testing.T) {
	r := &ValidationReport{}
	if got := r.Error(); got != "" {
		t.Errorf("empty report Error() = %q, want empty", got)
	}
	if !r.IsValid() {
		t.Error("empty report should be IsValid=true")
	}
	if r.HasErrors() {
		t.Error("empty report should be HasErrors=false")
	}
}

// ---------------------------------------------------------------------------
// Strict mode (ValidateMemoryStrict)
// ---------------------------------------------------------------------------

func TestValidator_StrictPasses(t *testing.T) {
	v := NewMemorySchemaValidator()
	if err := v.ValidateMemoryStrict(makeValidRecord()); err != nil {
		t.Errorf("ValidateMemoryStrict should return nil for valid record; got %v", err)
	}
}

func TestValidator_StrictFails(t *testing.T) {
	v := NewMemorySchemaValidator()
	r := makeValidRecord()
	r.AgentID = ""
	err := v.ValidateMemoryStrict(r)
	if err == nil {
		t.Fatal("expected error for invalid record")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("error is not *ValidationError: %T", err)
	}
	if !errors.Is(err, ErrInvalidEntry) {
		t.Errorf("err should be errors.Is ErrInvalidEntry; got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// assertHasFieldError verifies report has an error for the given
// field name.
func assertHasFieldError(t *testing.T, report *ValidationReport, field string) {
	t.Helper()
	for _, e := range report.Errors {
		if e.Field == field {
			return
		}
	}
	t.Errorf("report missing %q error; full report:\n%s", field, report.Error())
}

// assertHasNoFieldError verifies report does NOT have an error for
// the given field. Used to confirm a specific check didn't trigger
// when other unrelated checks might still produce errors.
func assertHasNoFieldError(t *testing.T, report *ValidationReport, field string) {
	t.Helper()
	for _, e := range report.Errors {
		if e.Field == field {
			t.Errorf("unexpected %q error: %s", field, e.Message)
			return
		}
	}
}

// ---------------------------------------------------------------------------
// Sanity — SchemaVersion constant
// ---------------------------------------------------------------------------

func TestCurrentSchemaVersion_Format(t *testing.T) {
	// We pin to semver "X.Y.Z" so callers can compare / detect drift.
	want := fmt.Sprintf("%d.%d.%d",
		int(CurrentSchemaVersion[0]-'0'),
		int(CurrentSchemaVersion[2]-'0'),
		int(CurrentSchemaVersion[4]-'0'))
	if CurrentSchemaVersion != want {
		t.Errorf("CurrentSchemaVersion %q does not parse as semver %q",
			CurrentSchemaVersion, want)
	}
}
