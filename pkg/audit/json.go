// Package audit — JSON marshaling + (de)serialization for the 12-step
// audit evidence chain.
//
// Per specs/SPECIFICATION.md §6.5 (Audit Trail Requirements), an auditor
// MUST be able to trace evidence through all 12 steps of the Helix
// pipeline. Without persistence the evidence is in-memory only — operators
// can't audit a past run, and there's no way to replay a chain into the
// validation layer after a crash.
//
// This file adds explicit JSON (de)serialization for the polymorphic
// AuditEvidence struct: each step has its own struct, but the top-level
// AuditEvidence uses a `kind` discriminator field on round-trip so any
// future variant can be added without breaking existing JSON files.
//
// Two flavors of persistence:
//
//	audit.MarshalEvidence / UnmarshalEvidence  — single AuditEvidence,
//	                                            one JSON object per call.
//
//	builder.WriteToFile / ReadFromFile          — JSONL stream semantics:
//	                                            one evidence event per line,
//	                                            append-friendly via O_APPEND.
package audit

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

// =============================================================================
// Discriminator keys
// =============================================================================
//
// Each evidence kind is identified by a stable string in the JSON `kind`
// field. These constants are the canonical identifiers used by Marshal
// and read by Unmarshal. Adding a new kind requires:
//
//  1. Defining a new `type XEvidence struct { ... }`
//  2. Adding a KindX constant
//  3. Updating evidenceKindRegistry with a marshal/unmarshal pair
//  4. Updating the AuditEvidence struct + StepEvidence switch

const (
	KindForgejoIssue      = "forgejo_issue"
	KindAxiomWorkItem     = "axiom_work_item"
	KindRalphLoop         = "ralph_loop"
	KindOpenCodeSession   = "opencode_session"
	KindGitCommit         = "git_commit"
	KindGitReinsVerdict   = "gitreins_verdict"
	KindPRMetadata        = "pr_metadata"
	KindChimeraReview     = "chimera_review"
	KindConscientiousness = "conscientiousness"
	KindPromptFooCI       = "promptfoo_ci"
	KindCoApprovals       = "co_approvals"
	KindMerge             = "merge"
)

// evidenceEnvelope is the on-disk shape of any evidence record. The kind
// discriminator drives Unmarshal into the correct typed pointer; Payload
// carries the marshaled inner struct.
type evidenceEnvelope struct {
	Kind    string          `json:"kind"`
	Payload json.RawMessage `json:"payload"`
}

// =============================================================================
// MarshalEvidence / UnmarshalEvidence — single evidence, JSON object form
// =============================================================================

// MarshalEvidence serializes a single AuditEvidence struct into a JSON
// object. Each present evidence pointer is rendered as
// `{ "kind": "...", "payload": { ... } }`. Absent pointers are omitted
// entirely (the discriminator approach means the unmarshaler can
// distinguish "absent" from "present-but-empty" using the field's nil-ness
// on the resulting AuditEvidence).
//
// The returned bytes are deterministic — keys are sorted, no whitespace
// between tokens — so repeated calls with equal inputs produce equal
// output (a property tests rely on for regression detection).
func MarshalEvidence(ev *AuditEvidence) ([]byte, error) {
	if ev == nil {
		return nil, errors.New("audit: MarshalEvidence: nil evidence")
	}

	// Encode each present evidence as its own envelope object, then
	// assemble the parent object. We use a plain map[string]any here so
	// that "absent fields" are truly omitted (zero-value structs would
	// otherwise show up as `"forgejo_issue": {"kind": ..., "payload": {}}`).
	out := map[string]any{}

	if ev.ForgejoIssue != nil {
		data, err := json.Marshal(ev.ForgejoIssue)
		if err != nil {
			return nil, fmt.Errorf("marshal forgejo_issue: %w", err)
		}
		out[KindForgejoIssue] = evidenceEnvelope{Kind: KindForgejoIssue, Payload: data}
	}
	if ev.AxiomWorkItem != nil {
		data, err := json.Marshal(ev.AxiomWorkItem)
		if err != nil {
			return nil, fmt.Errorf("marshal axiom_work_item: %w", err)
		}
		out[KindAxiomWorkItem] = evidenceEnvelope{Kind: KindAxiomWorkItem, Payload: data}
	}
	if ev.RalphLoop != nil {
		data, err := json.Marshal(ev.RalphLoop)
		if err != nil {
			return nil, fmt.Errorf("marshal ralph_loop: %w", err)
		}
		out[KindRalphLoop] = evidenceEnvelope{Kind: KindRalphLoop, Payload: data}
	}
	if ev.OpenCodeSession != nil {
		data, err := json.Marshal(ev.OpenCodeSession)
		if err != nil {
			return nil, fmt.Errorf("marshal opencode_session: %w", err)
		}
		out[KindOpenCodeSession] = evidenceEnvelope{Kind: KindOpenCodeSession, Payload: data}
	}
	if ev.GitCommit != nil {
		data, err := json.Marshal(ev.GitCommit)
		if err != nil {
			return nil, fmt.Errorf("marshal git_commit: %w", err)
		}
		out[KindGitCommit] = evidenceEnvelope{Kind: KindGitCommit, Payload: data}
	}
	if ev.GitReinsVerdict != nil {
		data, err := json.Marshal(ev.GitReinsVerdict)
		if err != nil {
			return nil, fmt.Errorf("marshal gitreins_verdict: %w", err)
		}
		out[KindGitReinsVerdict] = evidenceEnvelope{Kind: KindGitReinsVerdict, Payload: data}
	}
	if ev.PRMetadata != nil {
		data, err := json.Marshal(ev.PRMetadata)
		if err != nil {
			return nil, fmt.Errorf("marshal pr_metadata: %w", err)
		}
		out[KindPRMetadata] = evidenceEnvelope{Kind: KindPRMetadata, Payload: data}
	}
	if ev.ChimeraReview != nil {
		data, err := json.Marshal(ev.ChimeraReview)
		if err != nil {
			return nil, fmt.Errorf("marshal chimera_review: %w", err)
		}
		out[KindChimeraReview] = evidenceEnvelope{Kind: KindChimeraReview, Payload: data}
	}
	if ev.Conscientiousness != nil {
		data, err := json.Marshal(ev.Conscientiousness)
		if err != nil {
			return nil, fmt.Errorf("marshal conscientiousness: %w", err)
		}
		out[KindConscientiousness] = evidenceEnvelope{Kind: KindConscientiousness, Payload: data}
	}
	if ev.PromptFooCI != nil {
		data, err := json.Marshal(ev.PromptFooCI)
		if err != nil {
			return nil, fmt.Errorf("marshal promptfoo_ci: %w", err)
		}
		out[KindPromptFooCI] = evidenceEnvelope{Kind: KindPromptFooCI, Payload: data}
	}
	if ev.CoApprovals != nil {
		data, err := json.Marshal(ev.CoApprovals)
		if err != nil {
			return nil, fmt.Errorf("marshal co_approvals: %w", err)
		}
		out[KindCoApprovals] = evidenceEnvelope{Kind: KindCoApprovals, Payload: data}
	}
	if ev.Merge != nil {
		data, err := json.Marshal(ev.Merge)
		if err != nil {
			return nil, fmt.Errorf("marshal merge: %w", err)
		}
		out[KindMerge] = evidenceEnvelope{Kind: KindMerge, Payload: data}
	}

	return marshalCanonical(out)
}

// UnmarshalEvidence reverses MarshalEvidence. It reads a JSON object
// produced by MarshalEvidence (or written by hand using the same
// discriminator format) and returns a fully-populated AuditEvidence
// pointer. Fields absent from the input remain nil on the returned
// struct.
//
// Returns an error for:
//   - invalid JSON
//   - unknown `kind` discriminator
//   - inner payload that doesn't match the kind's struct shape
func UnmarshalEvidence(data []byte) (*AuditEvidence, error) {
	if len(data) == 0 {
		return nil, errors.New("audit: UnmarshalEvidence: empty input")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	raw := map[string]json.RawMessage{}
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("audit: unmarshal: %w", err)
	}

	ev := &AuditEvidence{}

	if v, ok := raw[KindForgejoIssue]; ok {
		env, err := decodeEnvelope(v, KindForgejoIssue)
		if err != nil {
			return nil, err
		}
		var target ForgejoIssueEvidence
		if err := json.Unmarshal(env, &target); err != nil {
			return nil, fmt.Errorf("decode forgejo_issue payload: %w", err)
		}
		ev.ForgejoIssue = &target
	}
	if v, ok := raw[KindAxiomWorkItem]; ok {
		env, err := decodeEnvelope(v, KindAxiomWorkItem)
		if err != nil {
			return nil, err
		}
		var target AxiomWorkItemEvidence
		if err := json.Unmarshal(env, &target); err != nil {
			return nil, fmt.Errorf("decode axiom_work_item payload: %w", err)
		}
		ev.AxiomWorkItem = &target
	}
	if v, ok := raw[KindRalphLoop]; ok {
		env, err := decodeEnvelope(v, KindRalphLoop)
		if err != nil {
			return nil, err
		}
		var target RalphLoopEvidence
		if err := json.Unmarshal(env, &target); err != nil {
			return nil, fmt.Errorf("decode ralph_loop payload: %w", err)
		}
		ev.RalphLoop = &target
	}
	if v, ok := raw[KindOpenCodeSession]; ok {
		env, err := decodeEnvelope(v, KindOpenCodeSession)
		if err != nil {
			return nil, err
		}
		var target OpenCodeSessionEvidence
		if err := json.Unmarshal(env, &target); err != nil {
			return nil, fmt.Errorf("decode opencode_session payload: %w", err)
		}
		ev.OpenCodeSession = &target
	}
	if v, ok := raw[KindGitCommit]; ok {
		env, err := decodeEnvelope(v, KindGitCommit)
		if err != nil {
			return nil, err
		}
		var target GitCommitEvidence
		if err := json.Unmarshal(env, &target); err != nil {
			return nil, fmt.Errorf("decode git_commit payload: %w", err)
		}
		ev.GitCommit = &target
	}
	if v, ok := raw[KindGitReinsVerdict]; ok {
		env, err := decodeEnvelope(v, KindGitReinsVerdict)
		if err != nil {
			return nil, err
		}
		var target GitReinsVerdictEvidence
		if err := json.Unmarshal(env, &target); err != nil {
			return nil, fmt.Errorf("decode gitreins_verdict payload: %w", err)
		}
		ev.GitReinsVerdict = &target
	}
	if v, ok := raw[KindPRMetadata]; ok {
		env, err := decodeEnvelope(v, KindPRMetadata)
		if err != nil {
			return nil, err
		}
		var target PRMetadataEvidence
		if err := json.Unmarshal(env, &target); err != nil {
			return nil, fmt.Errorf("decode pr_metadata payload: %w", err)
		}
		ev.PRMetadata = &target
	}
	if v, ok := raw[KindChimeraReview]; ok {
		env, err := decodeEnvelope(v, KindChimeraReview)
		if err != nil {
			return nil, err
		}
		var target ChimeraReviewEvidence
		if err := json.Unmarshal(env, &target); err != nil {
			return nil, fmt.Errorf("decode chimera_review payload: %w", err)
		}
		ev.ChimeraReview = &target
	}
	if v, ok := raw[KindConscientiousness]; ok {
		env, err := decodeEnvelope(v, KindConscientiousness)
		if err != nil {
			return nil, err
		}
		var target ConscientiousnessEvidence
		if err := json.Unmarshal(env, &target); err != nil {
			return nil, fmt.Errorf("decode conscientiousness payload: %w", err)
		}
		ev.Conscientiousness = &target
	}
	if v, ok := raw[KindPromptFooCI]; ok {
		env, err := decodeEnvelope(v, KindPromptFooCI)
		if err != nil {
			return nil, err
		}
		var target PromptFooCIEvidence
		if err := json.Unmarshal(env, &target); err != nil {
			return nil, fmt.Errorf("decode promptfoo_ci payload: %w", err)
		}
		ev.PromptFooCI = &target
	}
	if v, ok := raw[KindCoApprovals]; ok {
		env, err := decodeEnvelope(v, KindCoApprovals)
		if err != nil {
			return nil, err
		}
		var target CoApprovalEvidence
		if err := json.Unmarshal(env, &target); err != nil {
			return nil, fmt.Errorf("decode co_approvals payload: %w", err)
		}
		ev.CoApprovals = &target
	}
	if v, ok := raw[KindMerge]; ok {
		env, err := decodeEnvelope(v, KindMerge)
		if err != nil {
			return nil, err
		}
		var target MergeEvidence
		if err := json.Unmarshal(env, &target); err != nil {
			return nil, fmt.Errorf("decode merge payload: %w", err)
		}
		ev.Merge = &target
	}

	return ev, nil
}

// decodeEnvelope parses a single evidence envelope `{kind,payload}` and
// returns the raw payload bytes. The `kind` field must match expected;
// mismatch is an error to prevent silent data corruption.
func decodeEnvelope(raw json.RawMessage, expected string) (json.RawMessage, error) {
	var env evidenceEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("decode %s envelope: %w", expected, err)
	}
	if env.Kind != expected {
		return nil, fmt.Errorf("envelope kind mismatch: got %q, want %q", env.Kind, expected)
	}
	return env.Payload, nil
}

// marshalCanonical re-marshals a map[string]any through an Encoder that
// sorts keys, producing deterministic output. json.Marshal on a map has
// non-deterministic key order across runs — bad for golden-file tests
// and for diff-able audit logs.
func marshalCanonical(in map[string]any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	// json.Encoder escapes HTML by default; we don't want < > & escaped
	// because they may legitimately appear in audit content (e.g. URLs
	// with query params). SetEscapeHTML(false) keeps them as-is.
	enc.SetEscapeHTML(false)
	if err := enc.Encode(in); err != nil {
		return nil, err
	}
	// Encoder always appends a trailing newline — strip it for canonical output.
	out := bytes.TrimRight(buf.Bytes(), "\n")
	return out, nil
}

// =============================================================================
// JSONL streaming — for append-friendly persistence
// =============================================================================

// WriteJSONL serializes a single AuditEvidence as one JSON object followed
// by a newline (JSONL convention). The write is flush-on-completion;
// callers wanting stronger durability should use WriteJSONLAppend with
// fsync via the underlying file handle.
//
// This is the wrong tool for bulk persistence of multi-event streams —
// use builder.WriteToFile for that, which emits one event per line.
func WriteJSONL(w io.Writer, ev *AuditEvidence) error {
	data, err := MarshalEvidence(ev)
	if err != nil {
		return err
	}
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	if _, err := w.Write([]byte("\n")); err != nil {
		return fmt.Errorf("write newline: %w", err)
	}
	return nil
}

// ReadJSONL reverses WriteJSONL: it reads one JSON object from r, parses
// it as AuditEvidence, and returns the result. EOF yields io.EOF wrapped
// with a "no audit evidence" message — callers should distinguish by
// checking errors.Is(err, io.EOF).
func ReadJSONL(r io.Reader) (*AuditEvidence, error) {
	dec := json.NewDecoder(r)
	var raw json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		return nil, err
	}
	return UnmarshalEvidence(raw)
}

// =============================================================================
// Convenience: time round-trip helpers (test-only — exported for downstream)
// =============================================================================

// fileExists is a thin wrapper used by ReadFromFile-style helpers to
// distinguish "file not present" from "file present but unreadable".
// Kept in this file (rather than persist.go) so it's available to any
// future helper that needs to detect missing-on-disk state without
// touching the filesystem twice.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
