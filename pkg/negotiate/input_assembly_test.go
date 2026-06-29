package negotiate

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestTruncateDiff_ShortDiff(t *testing.T) {
	diff := "small diff content"
	result := TruncateDiff(diff)
	if result != diff {
		t.Errorf("short diff should not be truncated, got %q", result)
	}
}

func TestTruncateDiff_ExactLimit(t *testing.T) {
	diff := strings.Repeat("a", MaxDiffChars)
	result := TruncateDiff(diff)
	if result != diff {
		t.Errorf("diff at exact limit should not be truncated")
	}
}

func TestTruncateDiff_OverLimit(t *testing.T) {
	diff := strings.Repeat("a", MaxDiffChars+1000)
	result := TruncateDiff(diff)
	if len(result) <= MaxDiffChars {
		t.Errorf("truncated diff should not be longer than limit + notice, got %d chars", len(result))
	}
	if !strings.Contains(result, "DIFF TRUNCATED") {
		t.Error("truncated diff should contain truncation notice")
	}
	if !strings.Contains(result, "50000") {
		t.Error("truncation notice should show chars shown")
	}
}

func TestTruncateDiff_Empty(t *testing.T) {
	result := TruncateDiff("")
	if result != "" {
		t.Errorf("empty diff should return empty, got %q", result)
	}
}

func TestTruncateDiff_NoticeContainsPercentage(t *testing.T) {
	diff := strings.Repeat("x", MaxDiffChars*2)
	result := TruncateDiff(diff)
	// 50K of 100K = 50% omitted
	if !strings.Contains(result, "50%") {
		t.Error("truncation notice should show percentage omitted")
	}
}

func TestConcatSpecFiles_MultipleFiles(t *testing.T) {
	files := []SpecFile{
		{Path: "specs/auth.md", Content: "# Auth Spec\ncontent1"},
		{Path: "specs/api.md", Content: "# API Spec\ncontent2"},
	}
	result := ConcatSpecFiles(files)

	if !strings.Contains(result, "specs/auth.md") {
		t.Error("result should contain first file path")
	}
	if !strings.Contains(result, "# Auth Spec") {
		t.Error("result should contain first file content")
	}
	if !strings.Contains(result, "specs/api.md") {
		t.Error("result should contain second file path")
	}
	if !strings.Contains(result, "# API Spec") {
		t.Error("result should contain second file content")
	}
}

func TestConcatSpecFiles_SingleFile(t *testing.T) {
	files := []SpecFile{
		{Path: "specs/single.md", Content: "single content"},
	}
	result := ConcatSpecFiles(files)
	if !strings.Contains(result, "specs/single.md") {
		t.Error("result should contain file path")
	}
	if !strings.Contains(result, "single content") {
		t.Error("result should contain file content")
	}
}

func TestConcatSpecFiles_Empty(t *testing.T) {
	result := ConcatSpecFiles(nil)
	if !strings.Contains(result, "no spec files") {
		t.Error("empty concat should return placeholder message")
	}
}

func TestAssembleArbiterInput_FullStructure(t *testing.T) {
	input := &ArbiterInput{
		Title:       "Add auth middleware",
		Description: "Implements JWT-based auth per spec §3",
		Diff:        "+ func AuthMiddleware() {}",
		SpecFiles:   []SpecFile{{Path: "specs/auth.md", Content: "## Auth Requirements"}},
		AgentA:      Agent{Name: "alice", TrustLevel: 85},
		AgentB:      Agent{Name: "bob", TrustLevel: 72},
		VerdictA:    VerdictApproved,
		VerdictB:    VerdictRequestChanges,
		BodyA:       "This looks good. Tests pass.",
		BodyB:       "Missing error handling for expired tokens.",
		DebateRounds: []Round{
			{Number: 1, Agent: "alice", Body: "Round 1 alice body"},
			{Number: 1, Agent: "bob", Body: "Round 1 bob body"},
		},
	}

	result := AssembleArbiterInput(input)

	// Check all 4 sections present
	sections := []string{
		"=== PR CONTEXT ===",
		"=== AGENT REVIEWS ===",
		"=== DEBATE TRANSCRIPT ===",
		"=== QUESTION ===",
	}
	for _, s := range sections {
		if !strings.Contains(result, s) {
			t.Errorf("result should contain section %q", s)
		}
	}

	// Check PR context
	if !strings.Contains(result, "Title: Add auth middleware") {
		t.Error("result should contain PR title")
	}
	if !strings.Contains(result, "Description: Implements JWT-based auth") {
		t.Error("result should contain PR description")
	}
	if !strings.Contains(result, "func AuthMiddleware") {
		t.Error("result should contain diff content")
	}

	// Check agent reviews
	if !strings.Contains(result, "@alice") {
		t.Error("result should contain agent A name")
	}
	if !strings.Contains(result, "trust=85") {
		t.Error("result should contain agent A trust level")
	}
	if !strings.Contains(result, "APPROVED") {
		t.Error("result should contain agent A verdict")
	}
	if !strings.Contains(result, "Tests pass") {
		t.Error("result should contain agent A body")
	}
	if !strings.Contains(result, "@bob") {
		t.Error("result should contain agent B name")
	}
	if !strings.Contains(result, "trust=72") {
		t.Error("result should contain agent B trust level")
	}
	if !strings.Contains(result, "REQUEST_CHANGES") {
		t.Error("result should contain agent B verdict")
	}

	// Check debate transcript
	if !strings.Contains(result, "Round 1 [alice]:") {
		t.Error("result should contain debate round 1 alice")
	}
	if !strings.Contains(result, "Round 1 [bob]:") {
		t.Error("result should contain debate round 1 bob")
	}

	// Check question
	if !strings.Contains(result, "APPROVE or REJECT?") {
		t.Error("result should contain question")
	}
}

func TestAssembleArbiterInput_NilInput(t *testing.T) {
	result := AssembleArbiterInput(nil)
	if !strings.Contains(result, "QUESTION") {
		t.Error("nil input should still produce minimal prompt with question")
	}
}

func TestAssembleArbiterInput_EmptyFields(t *testing.T) {
	input := &ArbiterInput{
		AgentA:   Agent{Name: "alpha"},
		AgentB:   Agent{Name: "beta"},
		VerdictA: VerdictApproved,
		VerdictB: VerdictRequestChanges,
	}

	result := AssembleArbiterInput(input)

	if !strings.Contains(result, "(no title)") {
		t.Error("empty title should show placeholder")
	}
	if !strings.Contains(result, "(no description)") {
		t.Error("empty description should show placeholder")
	}
	if !strings.Contains(result, "(no review body)") {
		t.Error("empty body should show placeholder")
	}
	if !strings.Contains(result, "(no debate rounds)") {
		t.Error("empty debate rounds should show placeholder")
	}
}

func TestAssembleArbiterInput_NoDebateRounds(t *testing.T) {
	input := &ArbiterInput{
		AgentA:   Agent{Name: "a"},
		AgentB:   Agent{Name: "b"},
		VerdictA: VerdictApproved,
		VerdictB: VerdictRequestChanges,
	}

	result := AssembleArbiterInput(input)
	if !strings.Contains(result, "(no debate rounds)") {
		t.Error("result should show no debate rounds placeholder")
	}
}

func TestAssembleArbiterInput_NoSpecFiles(t *testing.T) {
	input := &ArbiterInput{
		AgentA:   Agent{Name: "a"},
		AgentB:   Agent{Name: "b"},
		VerdictA: VerdictApproved,
		VerdictB: VerdictRequestChanges,
	}

	result := AssembleArbiterInput(input)
	if !strings.Contains(result, "no spec files") {
		t.Error("result should show no spec files placeholder")
	}
}

func TestAssembleArbiterInput_DiffTruncationInPrompt(t *testing.T) {
	input := &ArbiterInput{
		Diff:     strings.Repeat("d", MaxDiffChars+5000),
		AgentA:   Agent{Name: "a"},
		AgentB:   Agent{Name: "b"},
		VerdictA: VerdictApproved,
		VerdictB: VerdictRequestChanges,
	}

	result := AssembleArbiterInput(input)
	if !strings.Contains(result, "DIFF TRUNCATED") {
		t.Error("large diff should be truncated in prompt")
	}
}

func TestAssembleFromNegotiator(t *testing.T) {
	agentA := Agent{Name: "alice", TrustLevel: 85}
	agentB := Agent{Name: "bob", TrustLevel: 72}
	dir := t.TempDir()
	neg, err := NewNegotiator(42, agentA, agentB, VerdictApproved, VerdictRequestChanges,
		"http://localhost:8765", filepath.Join(dir, "audit.jsonl"))
	if err != nil {
		t.Fatalf("NewNegotiator error: %v", err)
	}

	// Post some debate rounds
	neg.Debate.Rounds = []Round{
		{Number: 1, Agent: "alice", Body: "I approve this change."},
		{Number: 1, Agent: "bob", Body: "I disagree, missing tests."},
	}

	result := AssembleFromNegotiator(neg, "PR Title", "PR Description", "diff content",
		[]SpecFile{{Path: "specs/test.md", Content: "test spec"}})

	if !strings.Contains(result, "@alice") {
		t.Error("result should contain agent A")
	}
	if !strings.Contains(result, "@bob") {
		t.Error("result should contain agent B")
	}
	if !strings.Contains(result, "PR Title") {
		t.Error("result should contain PR title")
	}
	if !strings.Contains(result, "diff content") {
		t.Error("result should contain diff")
	}
	if !strings.Contains(result, "I approve this change") {
		t.Error("result should contain debate round")
	}
	if !strings.Contains(result, "specs/test.md") {
		t.Error("result should contain spec file path")
	}
}

func TestAssembleFromNegotiator_NilNegotiator(t *testing.T) {
	result := AssembleFromNegotiator(nil, "title", "desc", "diff", nil)
	if !strings.Contains(result, "QUESTION") {
		t.Error("nil negotiator should produce minimal prompt")
	}
}

func TestEstimatePromptSize_BasicEstimate(t *testing.T) {
	input := &ArbiterInput{
		Title:       "Test PR",
		Description: "A test description",
		Diff:        "some diff content",
		AgentA:      Agent{Name: "a"},
		AgentB:      Agent{Name: "b"},
		BodyA:       "body A",
		BodyB:       "body B",
		DebateRounds: []Round{
			{Body: "round body"},
		},
	}

	size := EstimatePromptSize(input)
	if size <= 0 {
		t.Errorf("estimated size should be positive, got %d", size)
	}
	if size < len(input.Title)+len(input.Description)+len(input.Diff) {
		t.Error("estimated size should at least include content lengths")
	}
}

func TestEstimatePromptSize_NilInput(t *testing.T) {
	size := EstimatePromptSize(nil)
	if size != 0 {
		t.Errorf("nil input size = %d, want 0", size)
	}
}

func TestEstimatePromptSize_CapsDiffSize(t *testing.T) {
	input := &ArbiterInput{
		Diff: strings.Repeat("x", MaxDiffChars*3),
	}

	size := EstimatePromptSize(input)
	// The estimate should not include more than MaxDiffChars for the diff
	// Total size should be MaxDiffChars + overhead, not 3*MaxDiffChars + overhead
	if size > MaxDiffChars+1000 {
		t.Errorf("estimated size %d should cap diff at MaxDiffChars+overhead", size)
	}
}

func TestMin(t *testing.T) {
	if min(3, 5) != 3 {
		t.Error("min(3,5) should be 3")
	}
	if min(5, 3) != 3 {
		t.Error("min(5,3) should be 3")
	}
	if min(3, 3) != 3 {
		t.Error("min(3,3) should be 3")
	}
}

// Integration test: build prompt from a real negotiation and verify structure
func TestAssembleArbiterInput_FullNegotiationFlow(t *testing.T) {
	agentA := Agent{Name: "reviewer-alpha", TrustLevel: 65}
	agentB := Agent{Name: "reviewer-beta", TrustLevel: 78}
	dir := t.TempDir()
	neg, err := NewNegotiator(99, agentA, agentB, VerdictApproved, VerdictRequestChanges,
		"http://localhost:8765", filepath.Join(dir, "audit.jsonl"))
	if err != nil {
		t.Fatalf("NewNegotiator error: %v", err)
	}

	// Simulate debate rounds
	neg.Debate.Rounds = []Round{
		{Number: 1, Agent: "reviewer-alpha", Body: "Approve: tests pass, AC met", Evidence: 3},
		{Number: 1, Agent: "reviewer-beta", Body: "Reject: edge case not handled", Evidence: 2},
		{Number: 2, Agent: "reviewer-alpha", Body: "Edge case is covered by test_auth_edge", Evidence: 2},
		{Number: 2, Agent: "reviewer-beta", Body: "Concede: verified test exists", Evidence: 2},
	}

	prompt := AssembleFromNegotiator(neg,
		"feat: add OAuth2 provider support",
		"Adds Google, GitHub, and GitLab OAuth2 providers per spec §4.2",
		"+ func GoogleOAuth() {}",
		[]SpecFile{
			{Path: "specs/auth.md", Content: "## OAuth2 Requirements"},
			{Path: "specs/security.md", Content: "## Security Review"},
		})

	// Verify all 4 sections
	if !strings.Contains(prompt, "=== PR CONTEXT ===") {
		t.Error("missing PR CONTEXT section")
	}
	if !strings.Contains(prompt, "=== AGENT REVIEWS ===") {
		t.Error("missing AGENT REVIEWS section")
	}
	if !strings.Contains(prompt, "=== DEBATE TRANSCRIPT ===") {
		t.Error("missing DEBATE TRANSCRIPT section")
	}
	if !strings.Contains(prompt, "=== QUESTION ===") {
		t.Error("missing QUESTION section")
	}

	// Verify PR number is reflected (via negotiation state, not explicitly in prompt)
	// The negotiator should have PR number 99
	if neg.Neg.PRNumber != 99 {
		t.Errorf("PR number = %d, want 99", neg.Neg.PRNumber)
	}

	// Verify debate transcript shows all 4 rounds
	roundCount := strings.Count(prompt, "Round ")
	if roundCount < 4 {
		t.Errorf("debate transcript should contain 4 rounds, found %d references", roundCount)
	}

	// Verify both spec files included
	if !strings.Contains(prompt, "specs/auth.md") {
		t.Error("prompt should contain auth spec")
	}
	if !strings.Contains(prompt, "specs/security.md") {
		t.Error("prompt should contain security spec")
	}
}
