package prompt

import (
	"strings"
	"testing"
)

func TestFormatProvenanceChain_Complete(t *testing.T) {
	chain := &ProvenanceChain{
		CommitSHA: "abc123def456789",
		Links: []ChainLink{
			{Stage: "commit", Name: "abc123def456789", Status: "parsed", Detail: "attestation hash: sha256:abc", OK: true},
			{Stage: "prompt", Name: "cost-estimator/v1.0.0", Status: "active", Detail: "hash: sha256:abc", OK: true},
			{Stage: "spec", Name: "specs/cost-estimator.md", Status: "found", Detail: "spec version: v1", OK: true},
			{Stage: "work_item", Name: "WI-helix-002", Status: "complete", Detail: "Build pre-flight cost estimator", OK: true},
			{Stage: "intent", Name: "WI-helix-002", Status: "declared", Detail: "Build pre-flight cost estimator", OK: true},
		},
		Complete: true,
	}

	out := FormatProvenanceChain(chain)

	if !strings.Contains(out, "COMMIT:") {
		t.Error("output missing COMMIT:")
	}
	if !strings.Contains(out, "PROMPT:") {
		t.Error("output missing PROMPT:")
	}
	if !strings.Contains(out, "STATUS: active") {
		t.Error("output missing STATUS")
	}
	if !strings.Contains(out, "WORK ITEM:") {
		t.Error("output missing WORK ITEM")
	}
	if !strings.Contains(out, "INTENT:") {
		t.Error("output missing INTENT")
	}
	if !strings.Contains(out, "PROVENANCE CHAIN: COMPLETE") {
		t.Error("output missing COMPLETE marker")
	}
	if !strings.Contains(out, "✅") {
		t.Error("output missing emoji for OK links")
	}
}

func TestFormatProvenanceChain_Incomplete(t *testing.T) {
	chain := &ProvenanceChain{
		CommitSHA: "abc123",
		Links: []ChainLink{
			{Stage: "commit", Name: "abc123", Status: "parsed", OK: true},
			{Stage: "prompt", Name: "test/v1", Status: "not_found", Detail: "hash not in registry", OK: false},
		},
		Complete: false,
	}

	out := FormatProvenanceChain(chain)

	if !strings.Contains(out, "INCOMPLETE") {
		t.Error("output should contain INCOMPLETE for broken chain")
	}
	if !strings.Contains(out, "❌") {
		t.Error("output should contain ❌ for broken links")
	}
}

func TestFormatProvenanceChain_Nil(t *testing.T) {
	out := FormatProvenanceChain(nil)
	if !strings.Contains(out, "nil") {
		t.Error("nil chain should produce nil output")
	}
}

func TestFormatProvenanceChain_ShortSHA(t *testing.T) {
	chain := &ProvenanceChain{
		CommitSHA: "abcdefghij0123456789",
		Links: []ChainLink{
			{Stage: "commit", Name: "abcdefghij0123456789", Status: "parsed", OK: true},
		},
		Complete: true,
	}

	out := FormatProvenanceChain(chain)

	if !strings.Contains(out, "abcdefghij") {
		t.Error("output should contain 10-char SHA prefix")
	}
	if strings.Contains(out, "abcdefghij0123456789") {
		t.Error("output should truncate SHA, not show full hash")
	}
}

func TestFormatTamperReport(t *testing.T) {
	out := FormatTamperReport("abc123def", "cost-estimator/v1.0.0", "sha256:a1b2c3d4", "sha256:e5f6a7b8")

	if !strings.Contains(out, "HASH MISMATCH") {
		t.Error("tamper report missing HASH MISMATCH")
	}
	if !strings.Contains(out, "TAMPER DETECTED") {
		t.Error("tamper report missing TAMPER DETECTED")
	}
	if !strings.Contains(out, "sha256:a1b2c3d4") {
		t.Error("tamper report missing stored hash")
	}
	if !strings.Contains(out, "sha256:e5f6a7b8") {
		t.Error("tamper report missing computed hash")
	}
}

func TestSummarizeProvenance_Complete(t *testing.T) {
	chain := &ProvenanceChain{
		CommitSHA: "abc123",
		Links: []ChainLink{
			{Stage: "commit", OK: true},
			{Stage: "prompt", OK: true},
			{Stage: "spec", OK: true},
		},
		Complete: true,
	}

	summary := SummarizeProvenance(chain)

	if summary.CommitSHA != "abc123" {
		t.Errorf("CommitSHA = %q, want abc123", summary.CommitSHA)
	}
	if !summary.Complete {
		t.Error("Complete = false, want true")
	}
	if len(summary.Stages) != 3 {
		t.Errorf("Stages length = %d, want 3", len(summary.Stages))
	}
	if len(summary.Failures) != 0 {
		t.Errorf("Failures length = %d, want 0", len(summary.Failures))
	}
}

func TestSummarizeProvenance_WithFailures(t *testing.T) {
	chain := &ProvenanceChain{
		CommitSHA: "abc123",
		Links: []ChainLink{
			{Stage: "commit", OK: true},
			{Stage: "prompt", OK: false, Detail: "hash not in registry"},
		},
		Complete: false,
	}

	summary := SummarizeProvenance(chain)

	if summary.Complete {
		t.Error("Complete = true, want false")
	}
	if len(summary.Failures) != 1 {
		t.Fatalf("Failures length = %d, want 1", len(summary.Failures))
	}
	if !strings.Contains(summary.Failures[0], "prompt") {
		t.Errorf("Failure should contain stage name, got: %s", summary.Failures[0])
	}
}

func TestStageDisplayLabel(t *testing.T) {
	cases := []struct {
		stage string
		want  string
	}{
		{"commit", "COMMIT"},
		{"prompt", "PROMPT"},
		{"spec", "SPEC"},
		{"work_item", "WORK ITEM"},
		{"intent", "INTENT"},
		{"unknown", "UNKNOWN"},
		{"", ""},
	}
	for _, tc := range cases {
		got := stageDisplayLabel(tc.stage)
		if got != tc.want {
			t.Errorf("stageDisplayLabel(%q) = %q, want %q", tc.stage, got, tc.want)
		}
	}
}

func TestShortSHA(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"abcdef0123456789", "abcdef0123"},
		{"short", "short"},
		{"", ""},
		{"1234567890ab", "1234567890"},
	}
	for _, tc := range cases {
		got := shortSHA(tc.input)
		if got != tc.want {
			t.Errorf("shortSHA(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
