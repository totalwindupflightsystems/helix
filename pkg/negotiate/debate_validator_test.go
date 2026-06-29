package negotiate

import (
	"strings"
	"testing"
	"time"
)

func TestParseRoundComment_FullValidComment(t *testing.T) {
	body := `## Negotiation Round 2 — Agent: alice (trust: 85)

### Position
REQUEST_CHANGES

### Evidence
- [ ] Spec reference: specs/auth.md §3 — missing rate limiting
- [ ] Test output: go test ./pkg/auth/... → FAIL: TestRateLimitDenied
- [ ] AC coverage: AC-12 is violated because no rate limiter on /login

### Counter-Argument
In response to @bob's point about the login endpoint being acceptable:
The spec explicitly requires rate limiting per §3, and the test suite
confirms the endpoint is unprotected.

### Concession Conditions
I will concede if: the rate limiter is added and the test passes.`

	comment := ParseRoundComment(body)

	if comment.RoundNum != 2 {
		t.Errorf("RoundNum = %d, want 2", comment.RoundNum)
	}
	if comment.AgentName != "alice" {
		t.Errorf("AgentName = %q, want %q", comment.AgentName, "alice")
	}
	if comment.TrustLevel != 85 {
		t.Errorf("TrustLevel = %d, want 85", comment.TrustLevel)
	}
	if comment.Position != "REQUEST_CHANGES" {
		t.Errorf("Position = %q, want %q", comment.Position, "REQUEST_CHANGES")
	}
	if len(comment.Evidence) != 3 {
		t.Fatalf("Evidence count = %d, want 3", len(comment.Evidence))
	}
	if comment.Evidence[0].Type != EvidenceSpec {
		t.Errorf("Evidence[0].Type = %q, want %q", comment.Evidence[0].Type, EvidenceSpec)
	}
	if comment.Evidence[1].Type != EvidenceTest {
		t.Errorf("Evidence[1].Type = %q, want %q", comment.Evidence[1].Type, EvidenceTest)
	}
	if comment.Evidence[2].Type != EvidenceAC {
		t.Errorf("Evidence[2].Type = %q, want %q", comment.Evidence[2].Type, EvidenceAC)
	}
	if comment.CounterArgumentAgent != "bob" {
		t.Errorf("CounterArgumentAgent = %q, want %q", comment.CounterArgumentAgent, "bob")
	}
	if comment.CounterArgument == "" {
		t.Error("CounterArgument should not be empty")
	}
	if comment.ConcessionCondition == "" {
		t.Error("ConcessionCondition should not be empty")
	}
}

func TestParseRoundComment_ApprovedPosition(t *testing.T) {
	body := `## Negotiation Round 1 — Agent: bob (trust: 72)

### Position
APPROVED

### Evidence
- [ ] Test output: go test ./... → PASS
- [ ] GitReins verdict: Tier 2 PASS on criteria auth-flow`

	comment := ParseRoundComment(body)

	if comment.Position != "APPROVED" {
		t.Errorf("Position = %q, want APPROVED", comment.Position)
	}
	if comment.TrustLevel != 72 {
		t.Errorf("TrustLevel = %d, want 72", comment.TrustLevel)
	}
	if len(comment.Evidence) != 2 {
		t.Fatalf("Evidence count = %d, want 2", len(comment.Evidence))
	}
	if comment.Evidence[1].Type != EvidenceGitReins {
		t.Errorf("Evidence[1].Type = %q, want %q", comment.Evidence[1].Type, EvidenceGitReins)
	}
}

func TestParseRoundComment_EvidenceTypes(t *testing.T) {
	tests := []struct {
		line     string
		wantType EvidenceType
	}{
		{"- [ ] Spec reference: specs/foo.md §1", EvidenceSpec},
		{"- [ ] Test output: pytest -v → PASS", EvidenceTest},
		{"- [ ] AC coverage: AC-5 is satisfied", EvidenceAC},
		{"- [ ] GitReins verdict: Tier 2 PASS", EvidenceGitReins},
		{"- [ ] Finding: CF-001 — high — fixed", EvidenceFinding},
		{"- [ ] Some random evidence", EvidenceOther},
	}
	for _, tt := range tests {
		item := parseEvidenceLine(tt.line)
		if item.Type != tt.wantType {
			t.Errorf("parseEvidenceLine(%q).Type = %q, want %q", tt.line, item.Type, tt.wantType)
		}
	}
}

func TestParseRoundComment_ConcessionInline(t *testing.T) {
	body := `## Negotiation Round 1 — Agent: alice (trust: 50)

### Position
APPROVED

### Evidence
- [ ] Test output: go test → PASS
- [ ] Spec reference: specs/auth.md §1

CONCEDE: bob's evidence about the rate limiter gap is convincing`

	comment := ParseRoundComment(body)

	if !comment.HasConcession {
		t.Error("HasConcession should be true")
	}
	if !strings.Contains(comment.ConcessionCondition, "bob") {
		t.Errorf("ConcessionCondition should mention bob, got %q", comment.ConcessionCondition)
	}
}

func TestParseRoundComment_EmptyBody(t *testing.T) {
	comment := ParseRoundComment("")
	if len(comment.Evidence) != 0 {
		t.Errorf("Empty body should have 0 evidence items, got %d", len(comment.Evidence))
	}
	if comment.Position != "" {
		t.Errorf("Empty body should have empty position, got %q", comment.Position)
	}
}

func TestValidateEvidence_FullValid(t *testing.T) {
	comment := ParsedRoundComment{
		Position: "REQUEST_CHANGES",
		Evidence: []EvidenceItem{
			{Type: EvidenceSpec, Raw: "spec ref", Content: "specs/auth.md §3"},
			{Type: EvidenceTest, Raw: "test ref", Content: "go test → FAIL"},
			{Type: EvidenceOther, Raw: "other", Content: "additional evidence"},
		},
		CounterArgument:      "In response to @bob's point...",
		CounterArgumentAgent: "bob",
	}

	result := ValidateEvidence(comment)

	if !result.Valid {
		t.Errorf("Expected valid, got errors: %v", result.Errors)
	}
	if !result.MeetsMinimum {
		t.Error("MeetsMinimum should be true with 3 evidence items")
	}
	if !result.HasSpecOrTest {
		t.Error("HasSpecOrTest should be true with spec + test evidence")
	}
	if !result.HasCounterArgRef {
		t.Error("HasCounterArgRef should be true with @bob reference")
	}
}

func TestValidateEvidence_MinimumTwoItems(t *testing.T) {
	comment := ParsedRoundComment{
		Evidence: []EvidenceItem{
			{Type: EvidenceSpec, Content: "specs/auth.md §3"},
		},
		CounterArgumentAgent: "bob",
	}

	result := ValidateEvidence(comment)

	if result.MeetsMinimum {
		t.Error("MeetsMinimum should be false with 1 evidence item")
	}
	if !strings.Contains(result.Errors[0], "minimum 2") {
		t.Errorf("First error should mention minimum 2, got: %v", result.Errors)
	}
	if result.Valid {
		t.Error("Should not be valid with only 1 evidence item")
	}
}

func TestValidateEvidence_NoSpecOrTest(t *testing.T) {
	comment := ParsedRoundComment{
		Evidence: []EvidenceItem{
			{Type: EvidenceAC, Content: "AC-5 is satisfied"},
			{Type: EvidenceGitReins, Content: "Tier 2 PASS"},
		},
		CounterArgumentAgent: "bob",
	}

	result := ValidateEvidence(comment)

	if result.HasSpecOrTest {
		t.Error("HasSpecOrTest should be false without spec/test evidence")
	}
	if result.Valid {
		t.Error("Should not be valid without spec/test reference")
	}
}

func TestValidateEvidence_NoCounterArgRef(t *testing.T) {
	comment := ParsedRoundComment{
		Evidence: []EvidenceItem{
			{Type: EvidenceSpec, Content: "specs/auth.md §3"},
			{Type: EvidenceTest, Content: "go test → FAIL"},
		},
		CounterArgument:      "",
		CounterArgumentAgent: "",
	}

	result := ValidateEvidence(comment)

	if result.HasCounterArgRef {
		t.Error("HasCounterArgRef should be false without counter-argument")
	}
	if result.Valid {
		t.Error("Should not be valid without counter-argument reference")
	}
}

func TestValidateEvidence_BareDisagree(t *testing.T) {
	// "I disagree" without evidence — spec §7.2: comment rejected, agent gets strike
	comment := ParsedRoundComment{
		Evidence:             []EvidenceItem{},
		CounterArgumentAgent: "",
	}

	result := ValidateEvidence(comment)

	if result.Valid {
		t.Error("Bare disagreement should not be valid")
	}
	if result.StrikeReason != StrikeNoEvidence {
		t.Errorf("StrikeReason = %q, want %q", result.StrikeReason, StrikeNoEvidence)
	}
}

func TestValidateEvidence_SpecSatisfiesSpecOrTest(t *testing.T) {
	comment := ParsedRoundComment{
		Evidence: []EvidenceItem{
			{Type: EvidenceSpec, Content: "specs/auth.md §3"},
			{Type: EvidenceAC, Content: "AC-5"},
		},
		CounterArgumentAgent: "bob",
	}

	result := ValidateEvidence(comment)

	if !result.HasSpecOrTest {
		t.Error("Spec evidence alone should satisfy HasSpecOrTest")
	}
}

func TestValidateEvidence_TestSatisfiesSpecOrTest(t *testing.T) {
	comment := ParsedRoundComment{
		Evidence: []EvidenceItem{
			{Type: EvidenceTest, Content: "go test → PASS"},
			{Type: EvidenceAC, Content: "AC-5"},
		},
		CounterArgumentAgent: "bob",
	}

	result := ValidateEvidence(comment)

	if !result.HasSpecOrTest {
		t.Error("Test evidence alone should satisfy HasSpecOrTest")
	}
}

func TestValidateComment_Convenience(t *testing.T) {
	body := `## Negotiation Round 1 — Agent: alice (trust: 60)

### Position
REQUEST_CHANGES

### Evidence
- [ ] Spec reference: specs/auth.md §3
- [ ] Test output: go test → FAIL

### Counter-Argument
In response to @bob: the spec says otherwise`

	comment, result := ValidateComment(body)

	if comment.AgentName != "alice" {
		t.Errorf("AgentName = %q, want alice", comment.AgentName)
	}
	if !result.Valid {
		t.Errorf("Expected valid result, errors: %v", result.Errors)
	}
}

func TestValidateComment_NoEvidence(t *testing.T) {
	body := `## Negotiation Round 1 — Agent: alice (trust: 60)

### Position
APPROVED

### Evidence

### Counter-Argument
I just disagree with everything.`

	_, result := ValidateComment(body)

	if result.Valid {
		t.Error("Should not be valid with no evidence")
	}
	if result.StrikeReason != StrikeNoEvidence {
		t.Errorf("StrikeReason = %q, want %q", result.StrikeReason, StrikeNoEvidence)
	}
}

// --- StrikeTracker tests ---

func TestStrikeTracker_AccumulateStrike(t *testing.T) {
	st := NewStrikeTracker()
	now := time.Now()

	count, autoConcede := st.AccumulateStrike("alice", StrikeNoEvidence, 1, "no spec ref", now)
	if count != 1 {
		t.Errorf("After 1st strike, count = %d, want 1", count)
	}
	if autoConcede {
		t.Error("autoConcede should be false after 1st strike")
	}

	count, autoConcede = st.AccumulateStrike("alice", StrikeNoEvidence, 2, "no test ref", now)
	if count != 2 {
		t.Errorf("After 2nd strike, count = %d, want 2", count)
	}
	if autoConcede {
		t.Error("autoConcede should be false after 2nd non-round-miss strike")
	}

	count, autoConcede = st.AccumulateStrike("alice", StrikeNoEvidence, 2, "no AC ref", now)
	if count != 3 {
		t.Errorf("After 3rd strike, count = %d, want 3", count)
	}
	if !autoConcede {
		t.Error("autoConcede should be true after 3 strikes")
	}
}

func TestStrikeTracker_TwoRoundMissesAutoConcede(t *testing.T) {
	st := NewStrikeTracker()
	now := time.Now()

	// First miss: strike but no auto-concede
	missCount, autoConcede := st.RecordRoundMiss("alice", 1, now)
	if missCount != 1 {
		t.Errorf("After 1st miss, missCount = %d, want 1", missCount)
	}
	if autoConcede {
		t.Error("autoConcede should be false after 1st round miss")
	}

	// Second miss: auto-concede
	missCount, autoConcede = st.RecordRoundMiss("alice", 2, now)
	if missCount != 2 {
		t.Errorf("After 2nd miss, missCount = %d, want 2", missCount)
	}
	if !autoConcede {
		t.Error("autoConcede should be true after 2nd round miss")
	}
}

func TestStrikeTracker_ShouldAutoConcede(t *testing.T) {
	st := NewStrikeTracker()
	now := time.Now()

	if st.ShouldAutoConcede("alice") {
		t.Error("Should not auto-concede with 0 strikes")
	}

	st.AccumulateStrike("alice", StrikeNoEvidence, 1, "no evidence", now)
	if st.ShouldAutoConcede("alice") {
		t.Error("Should not auto-concede with 1 strike")
	}

	st.AccumulateStrike("alice", StrikeNoEvidence, 1, "no evidence", now)
	if st.ShouldAutoConcede("alice") {
		t.Error("Should not auto-concede with 2 non-miss strikes")
	}

	st.AccumulateStrike("alice", StrikeNoEvidence, 1, "no evidence", now)
	if !st.ShouldAutoConcede("alice") {
		t.Error("Should auto-concede with 3 strikes")
	}
}

func TestStrikeTracker_ShouldAutoConcede_RoundMissPath(t *testing.T) {
	st := NewStrikeTracker()
	now := time.Now()

	st.RecordRoundMiss("alice", 1, now)
	if st.ShouldAutoConcede("alice") {
		t.Error("Should not auto-concede with 1 round miss")
	}

	st.RecordRoundMiss("alice", 2, now)
	if !st.ShouldAutoConcede("alice") {
		t.Error("Should auto-concede with 2 round misses")
	}
}

func TestStrikeTracker_StrikeCount(t *testing.T) {
	st := NewStrikeTracker()
	now := time.Now()

	if st.StrikeCount("alice") != 0 {
		t.Error("Initial strike count should be 0")
	}

	st.AccumulateStrike("alice", StrikeNoEvidence, 1, "x", now)
	if st.StrikeCount("alice") != 1 {
		t.Errorf("Strike count = %d, want 1", st.StrikeCount("alice"))
	}

	if st.StrikeCount("bob") != 0 {
		t.Error("Other agent should have 0 strikes")
	}
}

func TestStrikeTracker_MixedStrikesAutoConcede(t *testing.T) {
	st := NewStrikeTracker()
	now := time.Now()

	// 1 no-evidence strike + 1 round miss = 2 total strikes, 1 round miss
	st.AccumulateStrike("alice", StrikeNoEvidence, 1, "no spec", now)
	_, autoConcede := st.RecordRoundMiss("alice", 1, now)
	if autoConcede {
		t.Error("Should not auto-concede with 2 total (1 non-miss + 1 miss)")
	}

	// One more strike (not a miss) → 3 total → auto-concede
	_, autoConcede = st.AccumulateStrike("alice", StrikeNoEvidence, 2, "no AC", now)
	if !autoConcede {
		t.Error("Should auto-concede with 3 total strikes (regardless of miss type)")
	}
}

func TestStrikeTracker_DifferentAgentsIsolated(t *testing.T) {
	st := NewStrikeTracker()
	now := time.Now()

	st.AccumulateStrike("alice", StrikeNoEvidence, 1, "x", now)
	st.AccumulateStrike("alice", StrikeNoEvidence, 1, "y", now)
	st.AccumulateStrike("bob", StrikeNoEvidence, 1, "z", now)

	if st.StrikeCount("alice") != 2 {
		t.Errorf("alice strike count = %d, want 2", st.StrikeCount("alice"))
	}
	if st.StrikeCount("bob") != 1 {
		t.Errorf("bob strike count = %d, want 1", st.StrikeCount("bob"))
	}
}

func TestStrikeTracker_GetStrikeLog(t *testing.T) {
	st := NewStrikeTracker()
	now := time.Now()

	st.AccumulateStrike("alice", StrikeNoEvidence, 1, "missing spec ref", now)
	st.AccumulateStrike("alice", StrikeMissedRound, 2, "timeout", now)

	log := st.GetStrikeLog("alice")
	if len(log) != 2 {
		t.Fatalf("Strike log length = %d, want 2", len(log))
	}
	if log[0].Agent != "alice" {
		t.Errorf("Log[0].Agent = %q, want alice", log[0].Agent)
	}
	if log[0].Reason != StrikeNoEvidence {
		t.Errorf("Log[0].Reason = %q, want %q", log[0].Reason, StrikeNoEvidence)
	}
	if log[1].Reason != StrikeMissedRound {
		t.Errorf("Log[1].Reason = %q, want %q", log[1].Reason, StrikeMissedRound)
	}
	if log[1].Round != 2 {
		t.Errorf("Log[1].Round = %d, want 2", log[1].Round)
	}
}

func TestStrikeTracker_GetStrikeLog_Empty(t *testing.T) {
	st := NewStrikeTracker()
	log := st.GetStrikeLog("unknown")
	if len(log) != 0 {
		t.Errorf("Unknown agent should have empty log, got %d entries", len(log))
	}
}

func TestStrikeTracker_RecordRoundMiss(t *testing.T) {
	st := NewStrikeTracker()
	now := time.Now()

	missCount, _ := st.RecordRoundMiss("alice", 1, now)
	if missCount != 1 {
		t.Errorf("RoundMissCount = %d, want 1", missCount)
	}
	if st.RoundMissCount("alice") != 1 {
		t.Errorf("RoundMissCount() = %d, want 1", st.RoundMissCount("alice"))
	}
}

func TestStrikeTracker_ConcurrentStrikes(t *testing.T) {
	st := NewStrikeTracker()
	now := time.Now()

	// Run strikes from multiple goroutines to verify thread safety
	done := make(chan bool, 2)
	go func() {
		for i := 0; i < 5; i++ {
			st.AccumulateStrike("alice", StrikeNoEvidence, 1, "concurrent", now)
		}
		done <- true
	}()
	go func() {
		for i := 0; i < 3; i++ {
			st.AccumulateStrike("bob", StrikeNoEvidence, 1, "concurrent", now)
		}
		done <- true
	}()
	<-done
	<-done

	if st.StrikeCount("alice") != 5 {
		t.Errorf("alice concurrent strikes = %d, want 5", st.StrikeCount("alice"))
	}
	if st.StrikeCount("bob") != 3 {
		t.Errorf("bob concurrent strikes = %d, want 3", st.StrikeCount("bob"))
	}
}

// --- Helper extraction tests ---

func TestExtractRoundNumber(t *testing.T) {
	tests := []struct {
		header string
		want   int
	}{
		{"## Negotiation Round 1 — Agent: alice", 1},
		{"## Negotiation Round 3 — Agent: bob", 3},
		{"## Negotiation Round 10 — Agent: charlie", 10},
		{"no round here", 0},
	}
	for _, tt := range tests {
		got := extractRoundNumber(tt.header)
		if got != tt.want {
			t.Errorf("extractRoundNumber(%q) = %d, want %d", tt.header, got, tt.want)
		}
	}
}

func TestExtractAgentFromHeader(t *testing.T) {
	tests := []struct {
		header string
		want   string
	}{
		{"## Negotiation Round 1 — Agent: alice (trust: 85)", "alice"},
		{"## Negotiation Round 1 — Agent: bob-smith (trust: 50)", "bob-smith"},
		{"## Negotiation Round 1 — Agent: charlie_test", "charlie_test"},
		{"## Negotiation Round 1 — Agent: dave", "dave"},
		{"## Round 1", ""},
	}
	for _, tt := range tests {
		got := extractAgentFromHeader(tt.header)
		if got != tt.want {
			t.Errorf("extractAgentFromHeader(%q) = %q, want %q", tt.header, got, tt.want)
		}
	}
}

func TestExtractTrustFromHeader(t *testing.T) {
	tests := []struct {
		header string
		want   int
	}{
		{"## Negotiation Round 1 — Agent: alice (trust: 85)", 85},
		{"## Negotiation Round 1 — Agent: alice (trust: 0)", 0},
		{"## Negotiation Round 1 — Agent: alice (trust: 100)", 100},
		{"## Negotiation Round 1 — Agent: alice", 0},
	}
	for _, tt := range tests {
		got := extractTrustFromHeader(tt.header)
		if got != tt.want {
			t.Errorf("extractTrustFromHeader(%q) = %d, want %d", tt.header, got, tt.want)
		}
	}
}

func TestExtractAtMention(t *testing.T) {
	tests := []struct {
		line string
		want string
	}{
		{"In response to @bob's point", "bob"},
		{"In response to @alice-smith", "alice-smith"},
		{"@charlie", "charlie"},
		{"no mention here", ""},
		{"@x", "x"},
	}
	for _, tt := range tests {
		got := extractAtMention(tt.line)
		if got != tt.want {
			t.Errorf("extractAtMention(%q) = %q, want %q", tt.line, got, tt.want)
		}
	}
}

func TestExtractPosition(t *testing.T) {
	tests := []struct {
		line string
		want string
	}{
		{"APPROVED", "APPROVED"},
		{"REQUEST_CHANGES", "REQUEST_CHANGES"},
		{"approved", "APPROVED"},
		{"request_changes", "REQUEST_CHANGES"},
		{"unknown", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := extractPosition(tt.line)
		if got != tt.want {
			t.Errorf("extractPosition(%q) = %q, want %q", tt.line, got, tt.want)
		}
	}
}

func TestParseEvidenceLine_BulletVariations(t *testing.T) {
	// Different bullet styles should all be recognized as evidence
	lines := []string{
		"- [ ] Spec reference: foo",
		"- [x] Test output: bar",
		"- Spec reference: baz",
		"* Test output: qux",
	}
	for _, line := range lines {
		if !isEvidenceLine(line) {
			t.Errorf("isEvidenceLine(%q) = false, want true", line)
		}
	}
}

func TestEvidenceItem_IsSpecOrTest(t *testing.T) {
	if !(EvidenceItem{Type: EvidenceSpec}).IsSpecOrTest() {
		t.Error("Spec should be spec-or-test")
	}
	if !(EvidenceItem{Type: EvidenceTest}).IsSpecOrTest() {
		t.Error("Test should be spec-or-test")
	}
	if (EvidenceItem{Type: EvidenceAC}).IsSpecOrTest() {
		t.Error("AC should not be spec-or-test")
	}
	if (EvidenceItem{Type: EvidenceGitReins}).IsSpecOrTest() {
		t.Error("GitReins should not be spec-or-test")
	}
	if (EvidenceItem{Type: EvidenceFinding}).IsSpecOrTest() {
		t.Error("Finding should not be spec-or-test")
	}
	if (EvidenceItem{Type: EvidenceOther}).IsSpecOrTest() {
		t.Error("Other should not be spec-or-test")
	}
}

// --- Integration: parse + validate end-to-end ---

func TestEndToEnd_ValidCommentWithStrikes(t *testing.T) {
	st := NewStrikeTracker()
	now := time.Now()

	// Agent alice posts a valid comment
	validBody := `## Negotiation Round 1 — Agent: alice (trust: 70)

### Position
REQUEST_CHANGES

### Evidence
- [ ] Spec reference: specs/auth.md §3
- [ ] Test output: go test → FAIL

### Counter-Argument
In response to @bob's claim that auth is fine: spec §3 disagrees`

	_, validation := ValidateComment(validBody)

	if !validation.Valid {
		t.Fatalf("Valid comment should pass validation, errors: %v", validation.Errors)
	}
	// No strike should be added for a valid comment — only add when validation fails

	// Now alice posts an invalid comment
	invalidBody := `## Negotiation Round 2 — Agent: alice (trust: 70)

### Position
APPROVED

### Evidence

### Counter-Argument
I just think it's fine.`

	_, badValidation := ValidateComment(invalidBody)

	if badValidation.Valid {
		t.Fatal("Invalid comment should fail validation")
	}
	count, _ := st.AccumulateStrike("alice", badValidation.StrikeReason, 2,
		strings.Join(badValidation.Errors, "; "), now)

	if count != 1 {
		t.Errorf("After 1 invalid comment, strike count = %d, want 1", count)
	}
}

func TestEndToEnd_StrikeEscalationToAutoConcede(t *testing.T) {
	st := NewStrikeTracker()
	now := time.Now()

	// Three invalid comments → 3 strikes → auto-concede
	for i := 1; i <= 3; i++ {
		body := `## Negotiation Round ` + string(rune('0'+i)) + ` — Agent: alice (trust: 50)

### Position
APPROVED

### Evidence

### Counter-Argument
Trust me.`

		_, validation := ValidateComment(body)
		if validation.Valid {
			t.Fatalf("Round %d comment should fail validation", i)
		}
		_, autoConcede := st.AccumulateStrike("alice", validation.StrikeReason, i, "", now)
		if i < 3 && autoConcede {
			t.Errorf("Round %d: should not auto-concede yet", i)
		}
		if i == 3 && !autoConcede {
			t.Error("Round 3: should auto-concede with 3 strikes")
		}
	}
}
