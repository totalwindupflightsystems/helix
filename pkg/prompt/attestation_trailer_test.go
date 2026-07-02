package prompt

import (
	"strings"
	"testing"
)

func TestParseHelixAttestation(t *testing.T) {
	tests := []struct {
		name    string
		msg     string
		wantErr bool
		check   func(t *testing.T, ha *HelixAttestation)
	}{
		{
			name: "complete trailer single line",
			msg: `feat: add agent identity

Implement the provisioning flow.

Helix-Attestation: {"task_id":"abc-123","prompt_hash":"sha256:abcd","model":"deepseek/v4-pro","context_hash":"sha256:ef01","cost_usd":0.005,"tokens":{"input":12000,"output":3400},"langfuse_trace_id":"trace-xyz","agent":"tower-axiom","confidence":72}
`,
			check: func(t *testing.T, ha *HelixAttestation) {
				if ha.TaskID != "abc-123" {
					t.Errorf("TaskID = %q, want %q", ha.TaskID, "abc-123")
				}
				if ha.PromptHash != "sha256:abcd" {
					t.Errorf("PromptHash = %q, want %q", ha.PromptHash, "sha256:abcd")
				}
				if ha.Model != "deepseek/v4-pro" {
					t.Errorf("Model = %q, want %q", ha.Model, "deepseek/v4-pro")
				}
				if ha.ContextHash != "sha256:ef01" {
					t.Errorf("ContextHash = %q, want %q", ha.ContextHash, "sha256:ef01")
				}
				if ha.CostUSD != 0.005 {
					t.Errorf("CostUSD = %f, want 0.005", ha.CostUSD)
				}
				if ha.Tokens.Input != 12000 || ha.Tokens.Output != 3400 {
					t.Errorf("Tokens = %+v, want {12000, 3400}", ha.Tokens)
				}
				if ha.LangfuseTraceID != "trace-xyz" {
					t.Errorf("LangfuseTraceID = %q, want %q", ha.LangfuseTraceID, "trace-xyz")
				}
				if ha.Agent != "tower-axiom" {
					t.Errorf("Agent = %q, want %q", ha.Agent, "tower-axiom")
				}
				if ha.Confidence != 72 {
					t.Errorf("Confidence = %d, want 72", ha.Confidence)
				}
			},
		},
		{
			name: "trailer with multi-line JSON",
			msg: `fix: patch bug

Helix-Attestation: {
  "task_id": "t-1",
  "prompt_hash": "sha256:deadbeef",
  "model": "glm-5.2",
  "cost_usd": 0.01,
  "tokens": {"input": 500, "output": 200},
  "agent": "agent-7",
  "confidence": 85
}
`,
			check: func(t *testing.T, ha *HelixAttestation) {
				if ha.TaskID != "t-1" {
					t.Errorf("TaskID = %q", ha.TaskID)
				}
				if ha.Model != "glm-5.2" {
					t.Errorf("Model = %q", ha.Model)
				}
			},
		},
		{
			name:    "no trailer",
			msg:     "feat: no attestation here\n\njust a commit",
			wantErr: true,
		},
		{
			name:    "malformed JSON",
			msg:     "feat: bad\n\nHelix-Attestation: {not valid json}",
			wantErr: true,
		},
		{
			name: "trailer with empty optional fields",
			msg: `feat: minimal

Helix-Attestation: {"prompt_hash":"sha256:aa","model":"m","agent":"a","confidence":50}`,
			check: func(t *testing.T, ha *HelixAttestation) {
				if ha.PromptHash != "sha256:aa" {
					t.Errorf("PromptHash = %q", ha.PromptHash)
				}
				if ha.TaskID != "" {
					t.Errorf("TaskID should be empty, got %q", ha.TaskID)
				}
			},
		},
		{
			name: "trailer at end of complex message",
			msg: `feat: complex change

This is a detailed description.

Closes #42
Spec: specs/trust-model.md §6

Co-authored-by: agent-7 <agent-7@helix>
Helix-Attestation: {"prompt_hash":"sha256:cafebabe","model":"kimi/k2.7","agent":"agent-7","confidence":90}`,
			check: func(t *testing.T, ha *HelixAttestation) {
				if ha.PromptHash != "sha256:cafebabe" {
					t.Errorf("PromptHash = %q", ha.PromptHash)
				}
				if ha.Confidence != 90 {
					t.Errorf("Confidence = %d", ha.Confidence)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ha, err := ParseHelixAttestation(tt.msg)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.check != nil {
				tt.check(t, ha)
			}
		})
	}
}

func TestFormatHelixAttestation(t *testing.T) {
	ha := &HelixAttestation{
		TaskID:      "uuid-1",
		PromptHash:  "sha256:abcd",
		Model:       "glm-5.2",
		CostUSD:     0.003,
		Agent:       "agent-9",
		Confidence:  80,
		Tokens:      TokenUsage{Input: 1000, Output: 500},
		ContextHash: "sha256:context",
	}

	out, err := FormatHelixAttestation(ha)
	if err != nil {
		t.Fatalf("FormatHelixAttestation error: %v", err)
	}

	if !strings.HasPrefix(out, "Helix-Attestation: ") {
		t.Errorf("output doesn't start with prefix: %q", out)
	}

	// Round-trip: parse the formatted output
	parsed, err := ParseHelixAttestation(out)
	if err != nil {
		t.Fatalf("round-trip parse failed: %v", err)
	}
	if parsed.TaskID != ha.TaskID {
		t.Errorf("round-trip TaskID mismatch: %q vs %q", parsed.TaskID, ha.TaskID)
	}
	if parsed.PromptHash != ha.PromptHash {
		t.Errorf("round-trip PromptHash mismatch: %q vs %q", parsed.PromptHash, ha.PromptHash)
	}
	if parsed.Confidence != ha.Confidence {
		t.Errorf("round-trip Confidence mismatch: %d vs %d", parsed.Confidence, ha.Confidence)
	}
}

func TestFormatHelixAttestation_Nil(t *testing.T) {
	_, err := FormatHelixAttestation(nil)
	if err == nil {
		t.Fatal("expected error for nil attestation")
	}
}

func TestFormatHelixAttestation_MissingPromptHash(t *testing.T) {
	ha := &HelixAttestation{
		Model: "test-model",
		Agent: "test-agent",
	}
	_, err := FormatHelixAttestation(ha)
	if err == nil {
		t.Fatal("expected error for missing prompt_hash")
	}
}

func TestAppendHelixAttestation(t *testing.T) {
	ha := &HelixAttestation{
		PromptHash: "sha256:test",
		Model:      "test",
		Agent:      "a",
		Confidence: 50,
	}

	msg := "feat: add thing\n\nDescription here."
	result, err := AppendHelixAttestation(msg, ha)
	if err != nil {
		t.Fatalf("AppendHelixAttestation error: %v", err)
	}

	if !HasHelixAttestation(result) {
		t.Error("result should contain Helix-Attestation trailer")
	}

	// Verify round-trip
	parsed, err := ParseHelixAttestation(result)
	if err != nil {
		t.Fatalf("round-trip parse failed: %v", err)
	}
	if parsed.PromptHash != "sha256:test" {
		t.Errorf("PromptHash = %q, want sha256:test", parsed.PromptHash)
	}
}

func TestAppendHelixAttestation_EmptyMessage(t *testing.T) {
	ha := &HelixAttestation{
		PromptHash: "sha256:x",
		Model:      "m",
		Agent:      "a",
		Confidence: 10,
	}

	result, err := AppendHelixAttestation("", ha)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !HasHelixAttestation(result) {
		t.Error("should contain trailer")
	}
}

func TestHasHelixAttestation(t *testing.T) {
	tests := []struct {
		msg  string
		want bool
	}{
		{"Helix-Attestation: {}", true},
		{"feat: no trailer\n\njust text", false},
		{"Helix-Attestation: {\"prompt_hash\":\"sha256:x\"}", true},
		{"", false},
		{"co-authored-by: x\nHelix-Attestation: {\"a\":1}", true},
	}

	for _, tt := range tests {
		got := HasHelixAttestation(tt.msg)
		if got != tt.want {
			t.Errorf("HasHelixAttestation(%q) = %v, want %v", tt.msg, got, tt.want)
		}
	}
}

func TestValidateHelixAttestation(t *testing.T) {
	tests := []struct {
		name    string
		ha      *HelixAttestation
		wantErr bool
		errSub  string
	}{
		{
			name: "valid complete",
			ha: &HelixAttestation{
				PromptHash:  "sha256:abc",
				Model:       "glm-5.2",
				Agent:       "agent-7",
				Confidence:  75,
				CostUSD:     0.01,
				ContextHash: "sha256:ctx",
			},
			wantErr: false,
		},
		{
			name:    "nil",
			ha:      nil,
			wantErr: true,
		},
		{
			name: "missing prompt_hash",
			ha: &HelixAttestation{
				Model:      "m",
				Agent:      "a",
				Confidence: 50,
			},
			wantErr: true,
			errSub:  "prompt_hash is required",
		},
		{
			name: "bad prompt_hash prefix",
			ha: &HelixAttestation{
				PromptHash: "md5:abc",
				Model:      "m",
				Agent:      "a",
				Confidence: 50,
			},
			wantErr: true,
			errSub:  "must start with 'sha256:'",
		},
		{
			name: "missing model",
			ha: &HelixAttestation{
				PromptHash: "sha256:abc",
				Agent:      "a",
				Confidence: 50,
			},
			wantErr: true,
			errSub:  "model is required",
		},
		{
			name: "missing agent",
			ha: &HelixAttestation{
				PromptHash: "sha256:abc",
				Model:      "m",
				Confidence: 50,
			},
			wantErr: true,
			errSub:  "agent is required",
		},
		{
			name: "confidence out of range negative",
			ha: &HelixAttestation{
				PromptHash: "sha256:abc",
				Model:      "m",
				Agent:      "a",
				Confidence: -1,
			},
			wantErr: true,
			errSub:  "confidence must be 0-100",
		},
		{
			name: "confidence out of range high",
			ha: &HelixAttestation{
				PromptHash: "sha256:abc",
				Model:      "m",
				Agent:      "a",
				Confidence: 101,
			},
			wantErr: true,
			errSub:  "confidence must be 0-100",
		},
		{
			name: "negative cost",
			ha: &HelixAttestation{
				PromptHash: "sha256:abc",
				Model:      "m",
				Agent:      "a",
				Confidence: 50,
				CostUSD:    -1.0,
			},
			wantErr: true,
			errSub:  "cost_usd must be non-negative",
		},
		{
			name: "bad context_hash prefix",
			ha: &HelixAttestation{
				PromptHash:  "sha256:abc",
				Model:       "m",
				Agent:       "a",
				Confidence:  50,
				ContextHash: "md5:abc",
			},
			wantErr: true,
			errSub:  "context_hash must start with 'sha256:'",
		},
		{
			name: "empty context_hash allowed",
			ha: &HelixAttestation{
				PromptHash: "sha256:abc",
				Model:      "m",
				Agent:      "a",
				Confidence: 50,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateHelixAttestation(tt.ha)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errSub != "" && !strings.Contains(err.Error(), tt.errSub) {
					t.Errorf("error %q doesn't contain %q", err.Error(), tt.errSub)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestHelixAttestationLegacyConversion(t *testing.T) {
	t.Run("from legacy", func(t *testing.T) {
		att := &Attestation{
			Hash:             "sha256:legacy",
			Model:            "glm-5.2",
			EstimatedCostUSD: 0.05,
			AgentAuthor:      "agent-3",
		}

		ha := HelixAttestationFromLegacy(att)
		if ha.PromptHash != "sha256:legacy" {
			t.Errorf("PromptHash = %q", ha.PromptHash)
		}
		if ha.Model != "glm-5.2" {
			t.Errorf("Model = %q", ha.Model)
		}
		if ha.CostUSD != 0.05 {
			t.Errorf("CostUSD = %f", ha.CostUSD)
		}
		if ha.Agent != "agent-3" {
			t.Errorf("Agent = %q", ha.Agent)
		}
	})

	t.Run("to legacy", func(t *testing.T) {
		ha := &HelixAttestation{
			PromptHash: "sha256:new",
			Model:      "kimi",
			CostUSD:    0.02,
			Agent:      "agent-5",
		}

		att := HelixAttestationToLegacy(ha)
		if att.Hash != "sha256:new" {
			t.Errorf("Hash = %q", att.Hash)
		}
		if att.Model != "kimi" {
			t.Errorf("Model = %q", att.Model)
		}
	})

	t.Run("nil from legacy", func(t *testing.T) {
		if HelixAttestationFromLegacy(nil) != nil {
			t.Error("expected nil for nil input")
		}
	})

	t.Run("nil to legacy", func(t *testing.T) {
		if HelixAttestationToLegacy(nil) != nil {
			t.Error("expected nil for nil input")
		}
	})
}

func TestParseHelixAttestation_SpecExample(t *testing.T) {
	// Exact example from spec §2.2 Step 5
	msg := `feat: implement agent identity

Closes #12

Helix-Attestation: {
  "task_id": "uuid-v4",
  "prompt_hash": "sha256:hex",
  "model": "provider/model-name",
  "context_hash": "sha256:hex",
  "cost_usd": 0.0023,
  "tokens": {"input": 12000, "output": 3400},
  "langfuse_trace_id": "string",
  "agent": "tower-axiom",
  "confidence": 72
}`

	ha, err := ParseHelixAttestation(msg)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	if ha.TaskID != "uuid-v4" {
		t.Errorf("TaskID = %q", ha.TaskID)
	}
	if ha.CostUSD != 0.0023 {
		t.Errorf("CostUSD = %f", ha.CostUSD)
	}
	if ha.Tokens.Input != 12000 {
		t.Errorf("Tokens.Input = %d", ha.Tokens.Input)
	}
	if ha.Agent != "tower-axiom" {
		t.Errorf("Agent = %q", ha.Agent)
	}
}
