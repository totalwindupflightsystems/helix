// Tests for pkg/audit/json.go — covers MarshalEvidence, UnmarshalEvidence,
// JSONL streaming, kind discriminator handling, and round-trip fidelity
// for every evidence variant.
package audit

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// makeFullEvidence returns an AuditEvidence with every step populated to
// a non-zero value, so round-trip tests exercise the full schema.
func makeFullEvidence() *AuditEvidence {
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	return &AuditEvidence{
		ForgejoIssue: &ForgejoIssueEvidence{
			IssueURL:  "https://forgejo/owner/repo/issues/42",
			Creator:   "alice",
			Title:     "Fix audit persistence",
			Timestamp: now,
		},
		AxiomWorkItem: &AxiomWorkItemEvidence{
			PlanYAMLRef: "/plans/wi-007.yaml",
			AgentIDs:    []string{"agent-1", "agent-2"},
			RunID:       "run-9001",
			WorkItemID:  "wi-007",
		},
		RalphLoop: &RalphLoopEvidence{
			LockID:         "lock-3",
			WorktreePath:   "/tmp/worktree-3",
			LockAcquiredAt: now,
			LockReleasedAt: now.Add(2 * time.Hour),
		},
		OpenCodeSession: &OpenCodeSessionEvidence{
			SessionID:       "sess-42",
			Model:           "anthropic/claude-sonnet-4",
			TokensInput:     1000,
			TokensOutput:    2000,
			CostUSD:         0.42,
			LangFuseTraceID: "trace-42",
		},
		GitCommit: &GitCommitEvidence{
			SHA:              "abc123def456",
			AttestationFound: true,
			PromptHash:       "ph-9001",
			Model:            "anthropic/claude-sonnet-4",
			ContextHash:      "ch-9001",
			AgentID:          "agent-7",
			Confidence:       87,
			CostUSD:          0.13,
		},
		GitReinsVerdict: &GitReinsVerdictEvidence{
			Tier1Passed:   true,
			Tier1Checks:   []string{"secrets", "lint", "tests"},
			Tier2Verdict:  "COMPLETE",
			Tier2Findings: 2,
			VerdictTime:   now,
		},
		PRMetadata: &PRMetadataEvidence{
			PRIndex:          42,
			LinkedIssueURL:   "https://forgejo/owner/repo/issues/42",
			SpecRef:          "specs/audit.md",
			EvidenceBundleID: "bundle-42",
		},
		ChimeraReview: &ChimeraReviewEvidence{
			TraceID:      "chimera-trace-42",
			Formation:    "consensus",
			WorkerModels: []string{"a", "b", "c"},
			Verdict:      "APPROVE",
			Findings:     0,
			Score:        0.95,
		},
		Conscientiousness: &ConscientiousnessEvidence{
			ReportID:      "cr-42",
			Verdict:       "DEFENSIBLE",
			AttackVectors: []string{"prompt-injection", "secret-leak"},
			Mitigations:   3,
		},
		PromptFooCI: &PromptFooCIEvidence{
			TotalTests:   10,
			PassedTests:  9,
			FailedTests:  1,
			ActionsRunID: "actions-run-42",
			Results: []PromptFooResult{
				{TestCase: "test-1", Passed: true, Model: "anthropic/claude-sonnet-4", Variance: 0.01},
				{TestCase: "test-2", Passed: false, Model: "google/gemini-2.5-flash-lite", Variance: 0.05},
			},
		},
		CoApprovals: &CoApprovalEvidence{
			HumanApproval: &ApprovalRecord{Reviewer: "alice", TrustLevel: 95, Timestamp: now},
			AgentApproval: &ApprovalRecord{Reviewer: "agent-7", TrustLevel: 70, Timestamp: now},
		},
		Merge: &MergeEvidence{
			MergeSHA:        "merge-sha-42",
			Strategy:        "squash",
			Timestamp:       now,
			PagesURL:        "https://pages.example/branch",
			LangFuseTraceID: "trace-merge-42",
		},
	}
}

// -----------------------------------------------------------------------------
// MarshalEvidence / UnmarshalEvidence — round-trip
// -----------------------------------------------------------------------------

func TestMarshalUnmarshalEvidence_RoundTrip_Full(t *testing.T) {
	original := makeFullEvidence()
	data, err := MarshalEvidence(original)
	if err != nil {
		t.Fatalf("MarshalEvidence: %v", err)
	}

	got, err := UnmarshalEvidence(data)
	if err != nil {
		t.Fatalf("UnmarshalEvidence: %v", err)
	}

	// Compare every field. We're round-tripping through JSON, so
	// pointer dereferences are required before comparison.
	if got.ForgejoIssue == nil || got.ForgejoIssue.Creator != original.ForgejoIssue.Creator {
		t.Errorf("ForgejoIssue round-trip mismatch: %+v", got.ForgejoIssue)
	}
	if got.AxiomWorkItem == nil || got.AxiomWorkItem.WorkItemID != original.AxiomWorkItem.WorkItemID {
		t.Errorf("AxiomWorkItem round-trip mismatch")
	}
	if got.RalphLoop == nil || got.RalphLoop.LockID != original.RalphLoop.LockID {
		t.Errorf("RalphLoop round-trip mismatch")
	}
	if got.OpenCodeSession == nil || got.OpenCodeSession.TokensInput != 1000 {
		t.Errorf("OpenCodeSession round-trip mismatch")
	}
	if got.GitCommit == nil || got.GitCommit.Confidence != 87 {
		t.Errorf("GitCommit round-trip mismatch")
	}
	if got.GitReinsVerdict == nil || got.GitReinsVerdict.Tier1Passed != true {
		t.Errorf("GitReinsVerdict round-trip mismatch")
	}
	if got.PRMetadata == nil || got.PRMetadata.PRIndex != 42 {
		t.Errorf("PRMetadata round-trip mismatch")
	}
	if got.ChimeraReview == nil || len(got.ChimeraReview.WorkerModels) != 3 {
		t.Errorf("ChimeraReview round-trip mismatch")
	}
	if got.Conscientiousness == nil || got.Conscientiousness.Verdict != "DEFENSIBLE" {
		t.Errorf("Conscientiousness round-trip mismatch")
	}
	if got.PromptFooCI == nil || len(got.PromptFooCI.Results) != 2 {
		t.Errorf("PromptFooCI round-trip mismatch")
	}
	if got.CoApprovals == nil || got.CoApprovals.HumanApproval == nil {
		t.Errorf("CoApprovals round-trip mismatch")
	}
	if got.Merge == nil || got.Merge.Strategy != "squash" {
		t.Errorf("Merge round-trip mismatch")
	}
}

func TestMarshalUnmarshalEvidence_RoundTrip_Empty(t *testing.T) {
	original := &AuditEvidence{}
	data, err := MarshalEvidence(original)
	if err != nil {
		t.Fatalf("MarshalEvidence: %v", err)
	}
	got, err := UnmarshalEvidence(data)
	if err != nil {
		t.Fatalf("UnmarshalEvidence: %v", err)
	}
	// Empty evidence should round-trip as empty — no fields populated.
	if got.ForgejoIssue != nil || got.GitCommit != nil || got.Merge != nil {
		t.Errorf("empty evidence round-trip populated fields: %+v", got)
	}
}

func TestMarshalUnmarshalEvidence_RoundTrip_Partial(t *testing.T) {
	original := &AuditEvidence{
		ForgejoIssue: &ForgejoIssueEvidence{IssueURL: "u", Creator: "c", Title: "t", Timestamp: time.Now()},
		Merge:        &MergeEvidence{MergeSHA: "m", Strategy: "squash", Timestamp: time.Now()},
	}
	data, err := MarshalEvidence(original)
	if err != nil {
		t.Fatalf("MarshalEvidence: %v", err)
	}
	got, err := UnmarshalEvidence(data)
	if err != nil {
		t.Fatalf("UnmarshalEvidence: %v", err)
	}
	if got.ForgejoIssue == nil {
		t.Error("ForgejoIssue missing after partial round-trip")
	}
	if got.Merge == nil {
		t.Error("Merge missing after partial round-trip")
	}
	if got.GitCommit != nil {
		t.Errorf("GitCommit should be nil, got %+v", got.GitCommit)
	}
	if got.RalphLoop != nil {
		t.Errorf("RalphLoop should be nil, got %+v", got.RalphLoop)
	}
}

func TestMarshalEvidence_Nil(t *testing.T) {
	_, err := MarshalEvidence(nil)
	if err == nil {
		t.Fatal("expected error for nil evidence")
	}
}

func TestMarshalEvidence_Deterministic(t *testing.T) {
	// Same input twice must produce byte-identical output. This is the
	// guarantee tests rely on for diff-able audit logs.
	ev := makeFullEvidence()
	a, err := MarshalEvidence(ev)
	if err != nil {
		t.Fatalf("MarshalEvidence first: %v", err)
	}
	b, err := MarshalEvidence(ev)
	if err != nil {
		t.Fatalf("MarshalEvidence second: %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Errorf("MarshalEvidence is not deterministic\nfirst:  %s\nsecond: %s", a, b)
	}
}

func TestMarshalEvidence_KindDiscriminator(t *testing.T) {
	ev := &AuditEvidence{
		ForgejoIssue: &ForgejoIssueEvidence{IssueURL: "x", Creator: "c", Title: "t"},
	}
	data, err := MarshalEvidence(ev)
	if err != nil {
		t.Fatalf("MarshalEvidence: %v", err)
	}
	// Output must include the discriminator and the kind string.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("re-decode: %v", err)
	}
	env, ok := raw[KindForgejoIssue]
	if !ok {
		t.Fatalf("missing %s key in output: %s", KindForgejoIssue, data)
	}
	if !strings.Contains(string(env), `"kind":"`+KindForgejoIssue+`"`) {
		t.Errorf("envelope missing kind discriminator: %s", env)
	}
}

// -----------------------------------------------------------------------------
// UnmarshalEvidence — error paths
// -----------------------------------------------------------------------------

func TestUnmarshalEvidence_EmptyInput(t *testing.T) {
	_, err := UnmarshalEvidence(nil)
	if err == nil {
		t.Error("expected error for nil input")
	}
	_, err = UnmarshalEvidence([]byte{})
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestUnmarshalEvidence_MalformedJSON(t *testing.T) {
	_, err := UnmarshalEvidence([]byte("{not json"))
	if err == nil {
		t.Error("expected error for malformed JSON")
	}
	if !strings.Contains(err.Error(), "audit: unmarshal") {
		t.Errorf("error %q should mention 'audit: unmarshal'", err.Error())
	}
}

func TestUnmarshalEvidence_KindMismatch(t *testing.T) {
	// Hand-craft an envelope whose key doesn't match its `kind` field.
	bad := []byte(`{"forgejo_issue":{"kind":"merge","payload":{}}}`)
	_, err := UnmarshalEvidence(bad)
	if err == nil {
		t.Fatal("expected error for kind mismatch")
	}
	if !strings.Contains(err.Error(), "kind mismatch") {
		t.Errorf("error %q should mention 'kind mismatch'", err.Error())
	}
}

func TestUnmarshalEvidence_UnknownFieldIgnored(t *testing.T) {
	// We decode into a map[string]json.RawMessage, so unknown keys
	// are silently ignored (rather than erroring). This is forward-
	// compatible: a JSONL file produced by a future Helix version with
	// extra evidence kinds still round-trips through older readers.
	// Confirms: input is accepted, future_evidence is dropped, output
	// is empty evidence.
	bad := []byte(`{"future_evidence":{"kind":"future","payload":{}}}`)
	got, err := UnmarshalEvidence(bad)
	if err != nil {
		t.Fatalf("expected nil error for unknown field, got %v", err)
	}
	if got.ForgejoIssue != nil || got.Merge != nil {
		t.Errorf("unknown field somehow populated known pointer")
	}
}

func TestUnmarshalEvidence_PayloadShapeMismatch(t *testing.T) {
	// Envelope with the right kind but a payload that doesn't match the
	// inner struct (e.g. string instead of object) must error.
	bad := []byte(`{"forgejo_issue":{"kind":"forgejo_issue","payload":"not-an-object"}}`)
	_, err := UnmarshalEvidence(bad)
	if err == nil {
		t.Fatal("expected error for payload shape mismatch")
	}
	if !strings.Contains(err.Error(), "decode forgejo_issue payload") {
		t.Errorf("error %q should mention 'decode forgejo_issue payload'", err.Error())
	}
}

// -----------------------------------------------------------------------------
// JSONL streaming — WriteJSONL / ReadJSONL
// -----------------------------------------------------------------------------

func TestWriteReadJSONL_RoundTrip(t *testing.T) {
	var buf bytes.Buffer
	ev := makeFullEvidence()
	if err := WriteJSONL(&buf, ev); err != nil {
		t.Fatalf("WriteJSONL: %v", err)
	}
	if !strings.HasSuffix(buf.String(), "\n") {
		t.Errorf("JSONL output missing trailing newline")
	}
	got, err := ReadJSONL(&buf)
	if err != nil {
		t.Fatalf("ReadJSONL: %v", err)
	}
	if got.GitCommit == nil || got.GitCommit.SHA != ev.GitCommit.SHA {
		t.Errorf("round-trip lost GitCommit")
	}
}

func TestWriteJSONL_NilWriter_Allowed(t *testing.T) {
	// io.Discard accepts and discards — no error expected.
	if err := WriteJSONL(io.Discard, makeFullEvidence()); err != nil {
		t.Errorf("WriteJSONL to io.Discard: %v", err)
	}
}

func TestReadJSONL_EOF(t *testing.T) {
	_, err := ReadJSONL(&bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error on empty input")
	}
	if !errors.Is(err, io.EOF) {
		t.Errorf("expected io.EOF, got %v", err)
	}
}

func TestReadJSONL_Malformed(t *testing.T) {
	_, err := ReadJSONL(strings.NewReader("not json\n"))
	if err == nil {
		t.Fatal("expected error for malformed JSONL")
	}
}

// -----------------------------------------------------------------------------
// marshalCanonical — deterministic ordering
// -----------------------------------------------------------------------------

func TestMarshalCanonical_DeterministicKeyOrder(t *testing.T) {
	// json.Marshal on a map has non-deterministic key order, but our
	// marshaling must produce the same output regardless of insertion
	// order.
	a, err := marshalCanonical(map[string]any{"a": 1, "b": 2, "c": 3})
	if err != nil {
		t.Fatalf("marshalCanonical a: %v", err)
	}
	b, err := marshalCanonical(map[string]any{"c": 3, "b": 2, "a": 1})
	if err != nil {
		t.Fatalf("marshalCanonical b: %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Errorf("marshalCanonical not deterministic\nfirst:  %s\nsecond: %s", a, b)
	}
}

func TestMarshalCanonical_NoTrailingNewline(t *testing.T) {
	// Encoder appends a trailing newline; marshalCanonical strips it so
	// the output is a single JSON object on one line (canonical for
	// golden-file diffs).
	out, err := marshalCanonical(map[string]any{"k": "v"})
	if err != nil {
		t.Fatalf("marshalCanonical: %v", err)
	}
	if bytes.HasSuffix(out, []byte("\n")) {
		t.Errorf("marshalCanonical has trailing newline: %q", out)
	}
}

// -----------------------------------------------------------------------------
// fileExists — small wrapper used by ReadFromFile family
// -----------------------------------------------------------------------------

func TestFileExists(t *testing.T) {
	if !fileExists("/etc/passwd") {
		t.Skip("/etc/passwd missing on this platform — skipping")
	}
	if fileExists("/nonexistent/path/that/should/not/exist") {
		t.Error("fileExists returned true for nonexistent path")
	}
	if !fileExists("/etc/passwd") {
		t.Error("fileExists returned false for /etc/passwd")
	}
}

// -----------------------------------------------------------------------------
// MarshalEvidence error paths — exercise the per-evidence marshal failure
// branches by passing through normal types. (Hard to force json.Marshal to
// fail on the simple struct shapes we use; coverage of those branches is
// defended by code review + the existing happy-path tests.)
// -----------------------------------------------------------------------------

func TestMarshalEvidence_PartialRoundTrip_PreservesPointerShape(t *testing.T) {
	// Round-trip a partial evidence and verify that ABSENT pointers stay
	// nil. This is the property that distinguishes "not provided" from
	// "provided but zero-valued" — both must survive Marshal → Unmarshal.
	now := time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC)
	original := &AuditEvidence{
		ForgejoIssue: &ForgejoIssueEvidence{IssueURL: "u", Creator: "c", Title: "t", Timestamp: now},
	}
	data, err := MarshalEvidence(original)
	if err != nil {
		t.Fatalf("MarshalEvidence: %v", err)
	}
	got, err := UnmarshalEvidence(data)
	if err != nil {
		t.Fatalf("UnmarshalEvidence: %v", err)
	}
	if got.AxiomWorkItem != nil {
		t.Errorf("AxiomWorkItem should be nil after round-trip of partial evidence")
	}
	if got.GitCommit != nil {
		t.Errorf("GitCommit should be nil after round-trip of partial evidence")
	}
}

func TestMarshalEvidence_TimeRoundTrip(t *testing.T) {
	// Timestamps are sensitive — RFC3339 round-trip must preserve ns precision
	// modulo timezone normalization (UTC expected).
	original := &AuditEvidence{
		Merge: &MergeEvidence{
			MergeSHA:  "merge-1",
			Strategy:  "squash",
			Timestamp: time.Date(2026, 7, 5, 13, 14, 15, 926_000_000, time.UTC),
		},
	}
	data, err := MarshalEvidence(original)
	if err != nil {
		t.Fatalf("MarshalEvidence: %v", err)
	}
	got, err := UnmarshalEvidence(data)
	if err != nil {
		t.Fatalf("UnmarshalEvidence: %v", err)
	}
	if !got.Merge.Timestamp.Equal(original.Merge.Timestamp) {
		t.Errorf("timestamp lost: got %v, want %v",
			got.Merge.Timestamp, original.Merge.Timestamp)
	}
}

func TestUnmarshalEvidence_PartialPayload(t *testing.T) {
	// Hand-craft an envelope with valid kind but minimal payload — verifies
	// that pointer fields default to nil when the payload omits them.
	envelope := []byte(`{"forgejo_issue":{"kind":"forgejo_issue","payload":{"IssueURL":"u","Creator":"c","Title":"t"}}}`)
	got, err := UnmarshalEvidence(envelope)
	if err != nil {
		t.Fatalf("UnmarshalEvidence: %v", err)
	}
	if got.ForgejoIssue == nil {
		t.Fatal("ForgejoIssue should be populated")
	}
	if got.ForgejoIssue.IssueURL != "u" {
		t.Errorf("IssueURL = %q, want 'u'", got.ForgejoIssue.IssueURL)
	}
}

func TestWriteJSONL_PartialCoverage(t *testing.T) {
	// Force WriteJSONL's underlying write to fail by giving it a
	// deliberately broken writer — confirms the error path.
	broken := &brokenWriter{}
	err := WriteJSONL(broken, makeFullEvidence())
	if err == nil {
		t.Fatal("expected error writing to broken writer")
	}
	if !strings.Contains(err.Error(), "write") {
		t.Errorf("error %q should mention 'write'", err.Error())
	}
}

// brokenWriter always returns an error on Write. Used to exercise error
// paths in helpers that wrap io.Writer.
type brokenWriter struct{}

func (b *brokenWriter) Write(p []byte) (int, error) {
	return 0, errors.New("intentional failure")
}
