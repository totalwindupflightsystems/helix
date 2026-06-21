package marketplace

import (
	"testing"
)

// ---------------------------------------------------------------------------
// TestCapability_Valid
// ---------------------------------------------------------------------------

func TestCapability_Valid(t *testing.T) {
	tests := []struct {
		cap  Capability
		want bool
	}{
		// All 11 valid capabilities (typed constants)
		{cap: CapGo, want: true},
		{cap: CapTypeScript, want: true},
		{cap: CapPython, want: true},
		{cap: CapCodeReview, want: true},
		{cap: CapSpecWriting, want: true},
		{cap: CapSecurityReview, want: true},
		{cap: CapTesting, want: true},
		{cap: CapRefactoring, want: true},
		{cap: CapDocs, want: true},
		{cap: CapDevOps, want: true},
		{cap: CapNegotiation, want: true},

		// Also valid: string literals matching the constant values are the same type
		{cap: Capability("go"), want: true},
		{cap: Capability("typescript"), want: true},

		// Invalid / unrecognized
		{cap: "", want: false},
		{cap: "Go", want: false},       // wrong case
		{cap: "GO", want: false},       // wrong case
		{cap: "rust", want: false},
		{cap: "nonexistent", want: false},
	}

	for _, tt := range tests {
		t.Run("cap="+string(tt.cap), func(t *testing.T) {
			got := tt.cap.Valid()
			if got != tt.want {
				t.Errorf("%q.Valid() = %v, want %v", tt.cap, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestValidCapability
// ---------------------------------------------------------------------------

func TestValidCapability(t *testing.T) {
	tests := []struct {
		input   string
		wantCap Capability
		wantOk  bool
	}{
		{input: "go", wantCap: CapGo, wantOk: true},
		{input: "typescript", wantCap: CapTypeScript, wantOk: true},
		{input: "python", wantCap: CapPython, wantOk: true},
		{input: "devops", wantCap: CapDevOps, wantOk: true},
		{input: "negotiation", wantCap: CapNegotiation, wantOk: true},

		// Invalid
		{input: "", wantCap: "", wantOk: false},
		{input: "Go", wantCap: "Go", wantOk: false},
		{input: "rust", wantCap: "rust", wantOk: false},
	}

	for _, tt := range tests {
		t.Run("input="+tt.input, func(t *testing.T) {
			gotCap, gotOk := ValidCapability(tt.input)
			if gotCap != tt.wantCap {
				t.Errorf("ValidCapability(%q) cap = %q, want %q", tt.input, gotCap, tt.wantCap)
			}
			if gotOk != tt.wantOk {
				t.Errorf("ValidCapability(%q) ok = %v, want %v", tt.input, gotOk, tt.wantOk)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestAgentStatus_Valid
// ---------------------------------------------------------------------------

func TestAgentStatus_Valid(t *testing.T) {
	tests := []struct {
		status AgentStatus
		want   bool
	}{
		{status: StatusActive, want: true},
		{status: StatusDeprecated, want: true},
		{status: StatusRetired, want: true},

		// String literals matching the constant values are valid
		{status: AgentStatus("active"), want: true},
		{status: AgentStatus("deprecated"), want: true},

		// Invalid
		{status: "", want: false},
		{status: "Active", want: false},
		{status: "unknown", want: false},
		{status: "pending", want: false},
	}

	for _, tt := range tests {
		t.Run("status="+string(tt.status), func(t *testing.T) {
			got := tt.status.Valid()
			if got != tt.want {
				t.Errorf("%q.Valid() = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestCostProfile_Valid
// ---------------------------------------------------------------------------

func TestCostProfile_Valid(t *testing.T) {
	tests := []struct {
		profile CostProfile
		want    bool
	}{
		{profile: CostLow, want: true},
		{profile: CostMedium, want: true},
		{profile: CostHigh, want: true},

		// String literals matching are valid
		{profile: CostProfile("low"), want: true},

		// Invalid
		{profile: "", want: false},
		{profile: "Low", want: false},
		{profile: "free", want: false},
		{profile: "premium", want: false},
	}

	for _, tt := range tests {
		t.Run("profile="+string(tt.profile), func(t *testing.T) {
			got := tt.profile.Valid()
			if got != tt.want {
				t.Errorf("%q.Valid() = %v, want %v", tt.profile, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestTier_Valid
// ---------------------------------------------------------------------------

func TestTier_Valid(t *testing.T) {
	tests := []struct {
		tier Tier
		want bool
	}{
		{tier: TierPro, want: true},
		{tier: TierFlash, want: true},

		// String literals matching are valid
		{tier: Tier("pro"), want: true},

		// Invalid
		{tier: "", want: false},
		{tier: "Pro", want: false},
		{tier: "ultra", want: false},
		{tier: "free", want: false},
	}

	for _, tt := range tests {
		t.Run("tier="+string(tt.tier), func(t *testing.T) {
			got := tt.tier.Valid()
			if got != tt.want {
				t.Errorf("%q.Valid() = %v, want %v", tt.tier, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestExitError_Error
// ---------------------------------------------------------------------------

func TestExitError_Error(t *testing.T) {
	tests := []struct {
		name string
		e    ExitError
		want string
	}{
		{
			name: "agent_not_found",
			e:    ExitError{Code: ExitAgentNotFound, Message: "AGENT_NOT_FOUND: missing"},
			want: "AGENT_NOT_FOUND: missing",
		},
		{
			name: "invalid_rating",
			e:    ExitError{Code: ExitInvalidRating, Message: "INVALID_RATING: must be 1-5"},
			want: "INVALID_RATING: must be 1-5",
		},
		{
			name: "empty_message",
			e:    ExitError{Code: ExitSuccess, Message: ""},
			want: "",
		},
		{
			name: "unauthorized",
			e:    ExitError{Code: ExitUnauthorized, Message: "UNAUTHORIZED"},
			want: "UNAUTHORIZED",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.e.Error()
			if got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestHasCapability
// ---------------------------------------------------------------------------

func TestHasCapability(t *testing.T) {
	tests := []struct {
		name  string
		agent *Agent
		cap   Capability
		want  bool
	}{
		{
			name:  "has_go",
			agent: &Agent{Capabilities: []Capability{CapGo, CapTypeScript, CapPython}},
			cap:   CapGo,
			want:  true,
		},
		{
			name:  "has_negotiation",
			agent: &Agent{Capabilities: []Capability{CapNegotiation, CapDocs}},
			cap:   CapNegotiation,
			want:  true,
		},
		{
			name:  "string_literal_matches",
			agent: &Agent{Capabilities: []Capability{CapGo}},
			cap:   Capability("go"),    // same underlying value as CapGo
			want:  true,
		},
		{
			name:  "missing_capability",
			agent: &Agent{Capabilities: []Capability{CapGo, CapTypeScript}},
			cap:   CapPython,
			want:  false,
		},
		{
			name:  "empty_capabilities",
			agent: &Agent{Capabilities: nil},
			cap:   CapGo,
			want:  false,
		},
		{
			name:  "wrong_case",
			agent: &Agent{Capabilities: []Capability{CapGo}},
			cap:   "Go",
			want:  false,
		},
		{
			name:  "last_element",
			agent: &Agent{Capabilities: []Capability{CapDocs, CapDevOps, CapGo}},
			cap:   CapGo,
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasCapability(tt.agent, tt.cap)
			if got != tt.want {
				t.Errorf("hasCapability(%v, %q) = %v, want %v", tt.agent.Capabilities, tt.cap, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestCapabilitiesString
// ---------------------------------------------------------------------------

func TestCapabilitiesString(t *testing.T) {
	tests := []struct {
		name string
		caps []Capability
		want string
	}{
		{
			name: "single",
			caps: []Capability{CapGo},
			want: "go",
		},
		{
			name: "multiple",
			caps: []Capability{CapGo, CapTypeScript, CapPython},
			want: "go, typescript, python",
		},
		{
			name: "empty",
			caps: nil,
			want: "",
		},
		{
			name: "with_devops",
			caps: []Capability{CapDevOps, CapNegotiation},
			want: "devops, negotiation",
		},
		{
			name: "all_eleven",
			caps: []Capability{CapGo, CapTypeScript, CapPython,
				CapCodeReview, CapSpecWriting, CapSecurityReview, CapTesting,
				CapRefactoring, CapDocs, CapDevOps, CapNegotiation},
			want: "go, typescript, python, code-review, spec-writing, security-review, testing, refactoring, docs, devops, negotiation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := capabilitiesString(tt.caps)
			if got != tt.want {
				t.Errorf("capabilitiesString(%v) = %q, want %q", tt.caps, got, tt.want)
			}
		})
	}
}
