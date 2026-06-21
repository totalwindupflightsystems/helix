package marketplace

import (
	"testing"
)

// ---------------------------------------------------------------------------
// TestCalculateTrustScore
// ---------------------------------------------------------------------------

func TestCalculateTrustScore(t *testing.T) {
	tests := []struct {
		name                                                           string
		mergedPRs, rejectedPRs, incidents, forceMerges, budgetOverruns int
		avgRating                                                      float64
		want                                                           int
	}{
		// Base only (all zeros)
		{name: "base_only", want: 30},

		// Acceptance bonus (mergedPRs × 2, capped at 40)
		{name: "acceptance_10_merged", mergedPRs: 10, want: 50}, // 30 + min(40, 20) = 50
		{name: "acceptance_capped", mergedPRs: 50, want: 70},    // 30 + min(40, 100) = 70

		// Rejection penalty (rejectedPRs × 3, capped at 20)
		{name: "rejection_3", rejectedPRs: 3, want: 21},       // 30 - min(20, 9) = 21
		{name: "rejection_capped", rejectedPRs: 20, want: 10}, // 30 - min(20, 60) = 10

		// Incident penalty (incidents×10 + forceMerges×5 + budgetOverruns×3, capped at 30)
		{name: "incident_1", incidents: 1, want: 20},                                        // 30 - min(30, 10) = 20
		{name: "incident_combo", incidents: 1, forceMerges: 1, budgetOverruns: 1, want: 12}, // 30 - min(30, 10+5+3) = 12
		{name: "incident_capped", incidents: 5, want: 0},                                    // 30 - min(30, 50) = 0

		// Human rating bonus (avgRating×2, capped at 10, only if ≥3.0)
		{name: "human_bonus", avgRating: 4.5, want: 39},                 // 30 + min(10, 9) = 39
		{name: "human_bonus_capped", avgRating: 5.0, want: 40},          // 30 + min(10, 10) = 40
		{name: "human_no_bonus_below_cutoff", avgRating: 2.9, want: 30}, // 30 + 0 = 30
		{name: "human_bonus_at_cutoff", avgRating: 3.0, want: 36},       // 30 + min(10, 6) = 36

		// Combined scenarios
		{
			name: "proven_agent", mergedPRs: 15, rejectedPRs: 1, incidents: 0,
			avgRating: 4.0, want: 65, // 30 + min(40,30) - min(20,3) - 0 + min(10,8) = 30+30-3+8 = 65
		},
		{
			name: "controversial_agent", mergedPRs: 8, rejectedPRs: 5, incidents: 2,
			forceMerges: 2, avgRating: 2.5, want: 1, // 30 + min(40,16) - min(20,15) - min(30,20+10+0) + 0 = 30+16-15-30 = 1
		},
		{
			name: "floor_at_zero", mergedPRs: 0, rejectedPRs: 0, incidents: 10,
			avgRating: 1.0, want: 0, // 30+0-0-min(30,100)+0 = 0, clamped to 0
		},
		{
			name: "max_possible_score", mergedPRs: 100, rejectedPRs: 0, incidents: 0,
			avgRating: 5.0, want: 80, // 30 + min(40,200) - 0 - 0 + min(10,10) = 80 (formula max with no penalties)
		},

		// Negative score clamped to 0
		{name: "deeply_negative", rejectedPRs: 20, incidents: 5, want: 0}, // 30 - 20 - 30 = -20 → 0

		// Specific edge: exactly at labels
		{name: "at_70", mergedPRs: 20, want: 70}, // 30 + min(40,40) = 70
		{name: "at_50", mergedPRs: 10, want: 50}, // 30 + min(40,20) = 50
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalculateTrustScore(tt.mergedPRs, tt.rejectedPRs, tt.incidents,
				tt.forceMerges, tt.budgetOverruns, tt.avgRating)
			if got != tt.want {
				t.Errorf("CalculateTrustScore(...) = %d, want %d", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestTrustLabel
// ---------------------------------------------------------------------------

func TestTrustLabel(t *testing.T) {
	tests := []struct {
		score int
		want  string
	}{
		// Boundaries for New (0–29)
		{score: 0, want: "New"},
		{score: 15, want: "New"},
		{score: 29, want: "New"},

		// Boundaries for Established (30–49)
		{score: 30, want: "Established"},
		{score: 40, want: "Established"},
		{score: 49, want: "Established"},

		// Boundaries for Trusted (50–69)
		{score: 50, want: "Trusted"},
		{score: 60, want: "Trusted"},
		{score: 69, want: "Trusted"},

		// Boundaries for Senior (70–89)
		{score: 70, want: "Senior"},
		{score: 80, want: "Senior"},
		{score: 89, want: "Senior"},

		// Boundaries for Elder (90–100)
		{score: 90, want: "Elder"},
		{score: 95, want: "Elder"},
		{score: 100, want: "Elder"},
	}

	for _, tt := range tests {
		t.Run(tt.want+"_"+itoa(tt.score), func(t *testing.T) {
			got := TrustLabel(tt.score)
			if got != tt.want {
				t.Errorf("TrustLabel(%d) = %q, want %q", tt.score, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestDailyRecalculation
// ---------------------------------------------------------------------------

func TestDailyRecalculation(t *testing.T) {
	err := DailyRecalculation("/nonexistent/dir")
	if err != nil {
		t.Errorf("DailyRecalculation() returned error on stub: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestClamp
// ---------------------------------------------------------------------------

func TestClamp(t *testing.T) {
	tests := []struct {
		v, lo, hi int
		want      int
	}{
		{v: 5, lo: 0, hi: 10, want: 5},    // within range
		{v: -5, lo: 0, hi: 10, want: 0},   // below lo
		{v: 0, lo: 0, hi: 10, want: 0},    // at lo boundary
		{v: 10, lo: 0, hi: 10, want: 10},  // at hi boundary
		{v: 15, lo: 0, hi: 10, want: 10},  // above hi
		{v: 50, lo: 0, hi: 100, want: 50}, // typical range
		{v: 150, lo: 0, hi: 100, want: 100},
		{v: -100, lo: -50, hi: 50, want: -50},
		{v: 0, lo: -50, hi: 50, want: 0},
	}

	for _, tt := range tests {
		t.Run("v="+itoa(tt.v)+"_lo="+itoa(tt.lo)+"_hi="+itoa(tt.hi), func(t *testing.T) {
			got := clamp(tt.v, tt.lo, tt.hi)
			if got != tt.want {
				t.Errorf("clamp(%d, %d, %d) = %d, want %d", tt.v, tt.lo, tt.hi, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// itoa is a tiny helper to avoid importing strconv for test names.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
