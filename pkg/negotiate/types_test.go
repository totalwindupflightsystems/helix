package negotiate

import "testing"

// TestAgentCanComment verifies that CanComment returns true for any agent
// regardless of trust level (spec §11).
func TestAgentCanComment(t *testing.T) {
	trusts := []struct {
		name  string
		trust TrustLevel
	}{
		{"trust_zero", 0},
		{"trust_low", 10},
		{"trust_mid", 50},
		{"trust_high", 100},
	}
	for _, tc := range trusts {
		t.Run(tc.name, func(t *testing.T) {
			a := Agent{Name: "test", TrustLevel: tc.trust}
			if !a.CanComment() {
				t.Errorf("CanComment() = false, want true (trust=%d)", tc.trust)
			}
		})
	}
}

// TestAgentCanRequestChanges verifies the trust >= 30 threshold (spec §11).
func TestAgentCanRequestChanges(t *testing.T) {
	tests := []struct {
		name  string
		trust TrustLevel
		want  bool
	}{
		{"trust_zero", 0, false},
		{"trust_29_boundary", 29, false},
		{"trust_30_boundary", 30, true},
		{"trust_100", 100, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := Agent{Name: "test", TrustLevel: tc.trust}
			if got := a.CanRequestChanges(); got != tc.want {
				t.Errorf("CanRequestChanges() trust=%d = %v, want %v", tc.trust, got, tc.want)
			}
		})
	}
}

// TestAgentCanParticipate verifies the trust >= 30 threshold (spec §11).
func TestAgentCanParticipate(t *testing.T) {
	tests := []struct {
		name  string
		trust TrustLevel
		want  bool
	}{
		{"trust_zero", 0, false},
		{"trust_29_boundary", 29, false},
		{"trust_30_boundary", 30, true},
		{"trust_100", 100, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := Agent{Name: "test", TrustLevel: tc.trust}
			if got := a.CanParticipate(); got != tc.want {
				t.Errorf("CanParticipate() trust=%d = %v, want %v", tc.trust, got, tc.want)
			}
		})
	}
}

// TestAgentCanTriggerNegotiation verifies the trust >= 50 threshold (spec §11).
func TestAgentCanTriggerNegotiation(t *testing.T) {
	tests := []struct {
		name  string
		trust TrustLevel
		want  bool
	}{
		{"trust_zero", 0, false},
		{"trust_49_boundary", 49, false},
		{"trust_50_boundary", 50, true},
		{"trust_100", 100, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := Agent{Name: "test", TrustLevel: tc.trust}
			if got := a.CanTriggerNegotiation(); got != tc.want {
				t.Errorf("CanTriggerNegotiation() trust=%d = %v, want %v", tc.trust, got, tc.want)
			}
		})
	}
}

// TestAgentCanVeto verifies the trust >= 70 threshold (spec §11).
func TestAgentCanVeto(t *testing.T) {
	tests := []struct {
		name  string
		trust TrustLevel
		want  bool
	}{
		{"trust_zero", 0, false},
		{"trust_69_boundary", 69, false},
		{"trust_70_boundary", 70, true},
		{"trust_100", 100, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := Agent{Name: "test", TrustLevel: tc.trust}
			if got := a.CanVeto(); got != tc.want {
				t.Errorf("CanVeto() trust=%d = %v, want %v", tc.trust, got, tc.want)
			}
		})
	}
}
