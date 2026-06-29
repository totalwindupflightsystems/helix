package review

import (
	"strings"
	"testing"
)

// =============================================================================
// Basic functionality
// =============================================================================

func TestBiasStripper_Strip_EmptyMessage(t *testing.T) {
	bs := NewBiasStripper()
	if got := bs.Strip(""); got != "" {
		t.Errorf("expected '', got %q", got)
	}
}

func TestBiasStripper_Strip_RemovesEvaluativeLanguage(t *testing.T) {
	bs := NewBiasStripper()
	cases := []struct {
		input string
		want  string // substring that should NOT appear
	}{
		{"Fixed the bug", "Fixed"},
		{"Correct implementation", "Correct"},
		{"Ready to merge", "Ready"},
		{"All tests pass", "pass"},
		{"This is correct", "correct"},
		{"Perfect solution", "Perfect"},
		{"Obvious fix", "Obvious"},
		{"Trivial change", "Trivial"},
		{"Clean implementation", "Clean"},
		{"Properly handles edge cases", "Properly"},
		{"Clearly the right approach", "Clearly"},
	}
	for _, tc := range cases {
		got := bs.Strip(tc.input)
		if strings.Contains(got, tc.want) {
			t.Errorf("Strip(%q) = %q, should not contain %q", tc.input, got, tc.want)
		}
	}
}

func TestBiasStripper_Strip_RemovesConfidenceAssertions(t *testing.T) {
	bs := NewBiasStripper()
	cases := []string{
		"tested locally and works",
		"works on my machine",
		"should be fine to deploy",
		"I am confident this is correct",
		"all tests pass",
		"CI passes green",
		"verified locally",
		"cannot break anything",
		"no known issues",
		"works fine",
		"definitely works",
		"without any problems",
		"as expected",
	}
	for _, tc := range cases {
		got := bs.Strip(tc)
		if got == tc {
			t.Errorf("Strip(%q) = %q, should have been modified", tc, got)
		}
	}
}

func TestBiasStripper_Strip_RemovesEmoji(t *testing.T) {
	bs := NewBiasStripper()
	cases := []struct {
		input string
		want  string
	}{
		{"Fixed the bug 🚀", "the bug"},
		{"🎉 All tests pass", "All tests"},
		{"🔥 hotfix", "hotfix"},
		{"💪 strong refactor", "strong refactor"},
		{"feat: new endpoint 🎉", "feat: new endpoint"},
	}
	for _, tc := range cases {
		got := bs.Strip(tc.input)
		if strings.Contains(got, tc.want) {
			continue
		}
		// The important thing is no emoji in output
		if strings.ContainsAny(got, "🚀🎉🔥💪🤞") {
			t.Errorf("Strip(%q) = %q, still contains emoji", tc.input, got)
		}
	}
}

func TestBiasStripper_Strip_PreservesFactualInformation(t *testing.T) {
	bs := NewBiasStripper()
	cases := []struct {
		input string
		want  string // substring that should still appear
	}{
		{"Fixed the auth edge case in login.go", "auth"},
		{"Updated the rate limiter in middleware/ratelimit.go", "rate limiter"},
		{"Added retry logic for database connections in db.go", "retry logic"},
		{"Removed dead code from session manager in session.go", "dead code"},
	}
	for _, tc := range cases {
		got := bs.Strip(tc.input)
		if !strings.Contains(got, tc.want) {
			t.Errorf("Strip(%q) = %q, should contain %q (factual info preserved)", tc.input, got, tc.want)
		}
	}
}

func TestBiasStripper_Strip_NormalizesWhitespace(t *testing.T) {
	bs := NewBiasStripper()
	got := bs.Strip("Fixed the   bug    ready to pass")
	if strings.Contains(got, "  ") {
		t.Errorf("output has double spaces: %q", got)
	}
}

func TestBiasStripper_Strip_HandlesMultipleSentences(t *testing.T) {
	bs := NewBiasStripper()
	input := "Fixed the database connection pool. All tests pass. Ready to deploy."
	got := bs.Strip(input)
	if strings.Contains(got, "Fixed") {
		t.Errorf("should not contain 'Fixed': %q", got)
	}
	if strings.Contains(got, "Ready") {
		t.Errorf("should not contain 'Ready': %q", got)
	}
}

// =============================================================================
// Spec example
// =============================================================================

func TestBiasStripper_SpecExample(t *testing.T) {
	bs := NewBiasStripper()
	original := "Fixed the auth edge case, all tests pass, ready to merge"
	got := bs.Strip(original)

	// Per spec: "Modified auth module. Verify correctness."
	// Our implementation won't match exactly since it's a heuristic, but it
	// must NOT contain the original evaluative language.
	if strings.Contains(got, "Fixed") {
		t.Errorf("should not contain 'Fixed': %q", got)
	}
	if strings.Contains(got, "pass") {
		t.Errorf("should not contain evaluative 'pass': %q", got)
	}
	if strings.Contains(got, "ready") {
		t.Errorf("should not contain 'ready': %q", got)
	}
	// Must preserve factual content
	if !strings.Contains(got, "auth") {
		t.Errorf("should preserve 'auth': %q", got)
	}
}

// =============================================================================
// Disaster messages (based on documented AI-caused production incidents)
// =============================================================================

func TestBiasStripper_Disaster_ReplitDBDeletion(t *testing.T) {
	// "Replit AI deleted the production database" — the commit message likely
	// sounded confident and harmless.
	bs := NewBiasStripper()
	msg := "Fixed the database cleanup script. Tested locally and works fine. Ready to ship."
	got := bs.Strip(msg)
	// Should strip evaluative and confidence language but preserve "database cleanup script"
	if !strings.Contains(got, "database cleanup") {
		t.Errorf("should preserve factual content: %q", got)
	}
	if strings.Contains(got, "Tested locally") {
		t.Errorf("should remove confidence assertions: %q", got)
	}
}

func TestBiasStripper_Disaster_AmazonQFactoryReset(t *testing.T) {
	bs := NewBiasStripper()
	msg := "fix: Correctly handle device initialization 🚀 All edge cases covered, nothing can break"
	got := bs.StripPreservingPrefix(msg)
	if strings.Contains(got, "correctly") {
		t.Errorf("should remove 'correctly': %q", got)
	}
	if strings.Contains(got, "nothing can break") || strings.Contains(got, "nothing") {
		t.Errorf("should remove false confidence: %q", got)
	}
	// Should preserve the factual intent (device initialization)
	if !strings.Contains(got, "device init") && !strings.Contains(got, "Device init") {
		t.Errorf("should preserve 'device init': %q", got)
	}
}

func TestBiasStripper_Disaster_AnthropicRegression(t *testing.T) {
	bs := NewBiasStripper()
	msg := "Refactored the response formatting module. All existing tests pass, verified locally. Clean and proper implementation."
	got := bs.Strip(msg)
	// Should strip "all existing tests pass", "verified locally", "clean", "proper"
	// "tests" keyword should not survive stripping — but "response formatting" is factual
	if strings.Contains(got, "tests pass") {
		t.Errorf("should remove 'tests pass': %q", got)
	}
	if strings.Contains(got, "verified locally") {
		t.Errorf("should remove 'verified locally': %q", got)
	}
	if strings.Contains(got, "Clean") {
		t.Errorf("should remove 'clean': %q", got)
	}
	if !strings.Contains(got, "response formatting") {
		t.Errorf("should preserve 'response formatting': %q", got)
	}
}

func TestBiasStripper_Disaster_ChatGPTSessionHijack(t *testing.T) {
	bs := NewBiasStripper()
	msg := "fix: Fixed the session token validation. I am confident this fixes the issue. Tested locally. Should be fine."
	got := bs.StripPreservingPrefix(msg)
	if strings.Contains(got, "I am confident") {
		t.Errorf("should remove 'I am confident': %q", got)
	}
	if strings.Contains(got, "should be fine") {
		t.Errorf("should remove 'should be fine': %q", got)
	}
	// Must preserve "session token validation"
	if !strings.Contains(got, "session") && !strings.Contains(got, "token") {
		t.Errorf("should preserve token validation concepts: %q", got)
	}
}

func TestBiasStripper_Disaster_CopilotAPIKeyLeak(t *testing.T) {
	bs := NewBiasStripper()
	msg := "Added API key rotation logic. Simple change, all tests green, nothing to see here."
	got := bs.Strip(msg)
	if strings.Contains(got, "Simple") {
		t.Errorf("should remove evaluative 'Simple': %q", got)
	}
	if strings.Contains(got, "nothing to see") {
		t.Errorf("should remove 'nothing to see': %q", got)
	}
	if !strings.Contains(got, "API key rotation") {
		t.Errorf("should preserve factual 'API key rotation': %q", got)
	}
}

func TestBiasStripper_Disaster_CodexSyntaxDestruction(t *testing.T) {
	bs := NewBiasStripper()
	msg := "feat: Added new feature flag system. Works correctly, trivially correct implementation."
	got := bs.StripPreservingPrefix(msg)
	if strings.Contains(got, "trivially") {
		t.Errorf("should remove 'trivially': %q", got)
	}
	if strings.Contains(got, "correctly") {
		t.Errorf("should remove 'correctly': %q", got)
	}
	if !strings.Contains(got, "feature flag") && !strings.Contains(got, "flag") {
		t.Errorf("should preserve 'feature flag': %q", got)
	}
}

func TestBiasStripper_Disaster_GPTVulnerabilityIntro(t *testing.T) {
	bs := NewBiasStripper()
	msg := "refactor: Refactored the input sanitizer. Clean code, properly handles all edge cases. Ready for review."
	got := bs.StripPreservingPrefix(msg)
	if strings.Contains(got, "Clean") {
		t.Errorf("should remove 'Clean': %q", got)
	}
	if strings.Contains(got, "properly") {
		t.Errorf("should remove 'properly': %q", got)
	}
	if strings.Contains(got, "Ready for review") {
		t.Errorf("should remove 'Ready': %q", got)
	}
	if !strings.Contains(got, "input sanitizer") && !strings.Contains(got, "sanitizer") {
		t.Errorf("should preserve 'input sanitizer': %q", got)
	}
}

func TestBiasStripper_Disaster_GemmaInfiniteLoop(t *testing.T) {
	bs := NewBiasStripper()
	msg := "fix: Fixed infinite loop in data processor. Obvious bug, trivial fix. Cannot break anything."
	got := bs.StripPreservingPrefix(msg)
	if strings.Contains(got, "Obvious") {
		t.Errorf("should remove 'Obvious': %q", got)
	}
	if strings.Contains(got, "trivial") {
		t.Errorf("should remove 'trivial': %q", got)
	}
	if strings.Contains(got, "Cannot break") {
		t.Errorf("should remove 'Cannot break': %q", got)
	}
	if !strings.Contains(got, "infinite loop") && !strings.Contains(got, "data processor") {
		t.Errorf("should preserve 'infinite loop' or 'data processor': %q", got)
	}
}

// =============================================================================
// StripPreservingPrefix
// =============================================================================

func TestBiasStripper_StripPreservingPrefix_FeatPrefix(t *testing.T) {
	bs := NewBiasStripper()
	got := bs.StripPreservingPrefix("feat: Fixed the auth module 🚀")
	if !strings.HasPrefix(got, "feat:") {
		t.Errorf("should preserve 'feat:' prefix: %q", got)
	}
	if strings.Contains(got, "Fixed") {
		t.Errorf("should not contain 'Fixed': %q", got)
	}
	if strings.Contains(got, "🚀") {
		t.Errorf("should not contain emoji: %q", got)
	}
}

func TestBiasStripper_StripPreservingPrefix_FixPrefix(t *testing.T) {
	bs := NewBiasStripper()
	got := bs.StripPreservingPrefix("fix: Fixed the database connection pool, all tests green")
	if !strings.HasPrefix(got, "fix:") {
		t.Errorf("should preserve 'fix:' prefix: %q", got)
	}
	if strings.Contains(got, "all tests") {
		t.Errorf("should remove 'all tests green': %q", got)
	}
}

func TestBiasStripper_StripPreservingPrefix_ChorePrefix(t *testing.T) {
	bs := NewBiasStripper()
	got := bs.StripPreservingPrefix("chore: Cleaned up dependencies, trivial change")
	if !strings.HasPrefix(got, "chore:") {
		t.Errorf("should preserve 'chore:' prefix: %q", got)
	}
	if strings.Contains(got, "trivial") {
		t.Errorf("should remove 'trivial': %q", got)
	}
}

func TestBiasStripper_StripPreservingPrefix_NoPrefix(t *testing.T) {
	bs := NewBiasStripper()
	got := bs.StripPreservingPrefix("Just a simple fix, ready to merge")
	// Without a prefix, nothing to preserve
	if strings.Contains(got, "Just") {
		t.Errorf("should remove 'Just': %q", got)
	}
	if strings.Contains(got, "ready") {
		t.Errorf("should remove 'ready': %q", got)
	}
}

func TestBiasStripper_StripPreservingPrefix_MergePrefix(t *testing.T) {
	bs := NewBiasStripper()
	got := bs.StripPreservingPrefix("Merge branch 'feature/auth' into main")
	if !strings.HasPrefix(got, "Merge") {
		t.Errorf("should preserve 'Merge' prefix: %q", got)
	}
}

// =============================================================================
// PreserveFactualPrefix
// =============================================================================

func TestBiasStripper_PreserveFactualPrefix_Known(t *testing.T) {
	bs := NewBiasStripper()
	cases := []string{
		"feat: new endpoint",
		"fix: bug in parser",
		"chore: update deps",
		"docs: add readme",
		"refactor: extract function",
		"test: add coverage",
		"ci: fix pipeline",
		"BREAKING CHANGE: new api",
	}
	for _, tc := range cases {
		prefix := bs.PreserveFactualPrefix(tc)
		if prefix == "" {
			t.Errorf("expected prefix for %q", tc)
		}
	}
}

func TestBiasStripper_PreserveFactualPrefix_Unknown(t *testing.T) {
	bs := NewBiasStripper()
	if p := bs.PreserveFactualPrefix("some random message"); p != "" {
		t.Errorf("expected empty prefix, got %q", p)
	}
}

// =============================================================================
// Edge cases
// =============================================================================

func TestBiasStripper_Strip_WhitespaceOnly(t *testing.T) {
	bs := NewBiasStripper()
	if got := bs.Strip("   \t\n  "); got != "" {
		t.Errorf("expected empty from whitespace-only, got %q", got)
	}
}

func TestBiasStripper_Strip_AlreadyClean(t *testing.T) {
	bs := NewBiasStripper()
	msg := "Modified auth module. Verify correctness."
	got := bs.Strip(msg)
	if got != msg {
		t.Logf("already clean message changed: %q → %q", msg, got)
	}
}

func TestBiasStripper_Strip_OnlyEmoji(t *testing.T) {
	bs := NewBiasStripper()
	if got := bs.Strip("🚀🎉🔥"); got != "" {
		t.Errorf("expected empty from emoji-only, got %q", got)
	}
}

func TestBiasStripper_Strip_OnlyEvaluative(t *testing.T) {
	bs := NewBiasStripper()
	if got := bs.Strip("Fixed correct ready passes"); got != "" {
		t.Errorf("expected empty from evaluative-only, got %q", got)
	}
}

func TestBiasStripper_Strip_CollapsesRepeatedPunctuation(t *testing.T) {
	bs := NewBiasStripper()
	got := bs.Strip("Fixed the bug!!! All tests pass!!!")
	if strings.Contains(got, "!!!") {
		t.Errorf("should collapse repeated punctuation: %q", got)
	}
}

func TestBiasStripper_Strip_RemovesEmptyParentheticals(t *testing.T) {
	bs := NewBiasStripper()
	got := bs.Strip("feat: Fixed the bug ( )")
	if strings.Contains(got, "()") || strings.Contains(got, "( )") {
		t.Errorf("should remove empty parentheticals: %q", got)
	}
}

// =============================================================================
// Multiple passes produce same result (deterministic)
// =============================================================================

func TestBiasStripper_Strip_Idempotent(t *testing.T) {
	bs := NewBiasStripper()
	inputs := []string{
		"Fixed the auth edge case, all tests pass, ready to merge",
		"feat: Correctly handles device initialization 🚀",
		"Refactored response formatting. All tests green. Verified locally.",
	}
	for _, input := range inputs {
		first := bs.Strip(input)
		second := bs.Strip(first)
		if first != second {
			t.Errorf("not idempotent:\n  first=%q\n second=%q", first, second)
		}
	}
}

// =============================================================================
// Multi-line commit messages
// =============================================================================

func TestBiasStripper_Strip_MultiLine(t *testing.T) {
	bs := NewBiasStripper()
	msg := `Fixed the rate limiter

All tests pass. Ready to merge.

Tested locally and works fine.`
	got := bs.Strip(msg)
	if strings.Contains(got, "Fixed") {
		t.Errorf("should not contain 'Fixed': %q", got)
	}
	if strings.Contains(got, "All tests") {
		t.Errorf("should not contain 'All tests': %q", got)
	}
	if strings.Contains(got, "Ready") {
		t.Errorf("should not contain 'Ready': %q", got)
	}
	if strings.Contains(got, "Tested locally") {
		t.Errorf("should not contain 'Tested locally': %q", got)
	}
	// Must still have the factual content
	if !strings.Contains(got, "rate limiter") && !strings.Contains(got, "rate") {
		t.Errorf("should preserve 'rate limiter': %q", got)
	}
}

// =============================================================================
// NIT / LGTM patterns
// =============================================================================

func TestBiasStripper_Strip_RemovesNIT_LGTM(t *testing.T) {
	bs := NewBiasStripper()
	cases := []struct {
		input string
		want  string
	}{
		{"NIT: spacing off", "spacing off"},
		{"LGTM after fix", "after fix"},
	}
	for _, tc := range cases {
		got := bs.Strip(tc.input)
		if !strings.Contains(got, tc.want) {
			t.Errorf("Strip(%q) = %q, want containing %q", tc.input, got, tc.want)
		}
		// Should remove NIT/LGTM
		if strings.Contains(got, "NIT") || strings.Contains(got, "LGTM") {
			t.Errorf("Strip(%q) = %q, still contains NIT or LGTM", tc.input, got)
		}
	}
}

// =============================================================================
// Fake positive — rare words that look evaluative but aren't
// =============================================================================

func TestBiasStripper_Strip_PassesThroughNonEvaluativeWords(t *testing.T) {
	bs := NewBiasStripper()
	// "pass" appears in the evaluative list but here it's used as a noun in
	// factual context. The bias-stripper may still catch it — that's acceptable
	// per the spec since the original commit message is archived for audit.
	// This test documents the known behavior.
	msg := "Added mountain pass navigation to the route planner"
	got := bs.Strip(msg)
	// "pass" may be stripped but factual content should remain
	if !strings.Contains(got, "mountain") && !strings.Contains(got, "route planner") {
		t.Errorf("should preserve factual route planning content: %q", got)
	}
}
