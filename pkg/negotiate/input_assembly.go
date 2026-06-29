package negotiate

// input_assembly.go implements the Chimera arbiter input prompt assembly
// per specs/pr-negotiation.md §9.2 (Input Assembly).
//
// The prompt sent to Chimera assembles four sections:
//   1. PR Context: title, description, diff (truncated to 50K chars), spec files
//   2. Agent Reviews: both agents' names, trust levels, verdicts, and bodies
//   3. Debate Transcript: all debate rounds
//   4. Question: APPROVE or REJECT?

import (
	"fmt"
	"strings"
)

// MaxDiffChars is the maximum character limit for PR diffs in the arbiter
// prompt (spec §9.2: "Diff: <truncated to 50K chars>").
const MaxDiffChars = 50000

// ArbiterInput carries all the data needed to build the Chimera arbiter prompt.
type ArbiterInput struct {
	PRNumber     int        `json:"pr_number"`
	Title        string     `json:"title"`
	Description  string     `json:"description"`
	Diff         string     `json:"diff"`
	SpecFiles    []SpecFile `json:"spec_files"`
	AgentA       Agent      `json:"agent_a"`
	AgentB       Agent      `json:"agent_b"`
	VerdictA     Verdict    `json:"verdict_a"`
	VerdictB     Verdict    `json:"verdict_b"`
	BodyA        string     `json:"body_a"`
	BodyB        string     `json:"body_b"`
	DebateRounds []Round    `json:"debate_rounds"`
}

// SpecFile is a concatenated spec file with its path for the arbiter prompt.
type SpecFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// TruncateDiff clips the diff to MaxDiffChars. If the diff exceeds the limit,
// it is truncated with a truncation notice appended (spec §9.2).
func TruncateDiff(diff string) string {
	if len(diff) <= MaxDiffChars {
		return diff
	}
	truncated := diff[:MaxDiffChars]
	notice := fmt.Sprintf(
		"\n\n[... DIFF TRUNCATED: %d chars shown of %d total (%.0f%% omitted) ...]",
		MaxDiffChars, len(diff),
		float64(len(diff)-MaxDiffChars)/float64(len(diff))*100,
	)
	return truncated + notice
}

// ConcatSpecFiles merges multiple spec file contents into a single string.
// Each file is labeled with its path (spec §9.2: "Spec files: <concatenated spec content>").
func ConcatSpecFiles(files []SpecFile) string {
	if len(files) == 0 {
		return "(no spec files provided)"
	}

	var sb strings.Builder
	for i, f := range files {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(fmt.Sprintf("--- %s ---\n", f.Path))
		sb.WriteString(f.Content)
	}
	return sb.String()
}

// AssembleArbiterInput builds the complete Chimera arbiter prompt from the
// given ArbiterInput data (spec §9.2).
//
// The prompt structure:
//
//	=== PR CONTEXT ===
//	Title: <title>
//	Description: <description>
//	Diff: <truncated to 50K chars>
//	Spec files: <concatenated>
//
//	=== AGENT REVIEWS ===
//	Agent A (@<name>, trust=<level>): <verdict>
//	<body>
//
//	Agent B (@<name>, trust=<level>): <verdict>
//	<body>
//
//	=== DEBATE TRANSCRIPT ===
//	Round 1: ...
//	Round 2: ...
//
//	=== QUESTION ===
//	Resolve the conflict. Based on the spec, evidence, and debate:
//	APPROVE or REJECT?
func AssembleArbiterInput(input *ArbiterInput) string {
	if input == nil {
		return buildEmptyArbiterPrompt()
	}

	var sb strings.Builder

	// === PR CONTEXT ===
	sb.WriteString("=== PR CONTEXT ===\n")
	sb.WriteString(fmt.Sprintf("Title: %s\n", defaultStr(input.Title, "(no title)")))
	sb.WriteString(fmt.Sprintf("Description: %s\n", defaultStr(input.Description, "(no description)")))
	sb.WriteString("Diff: ")
	sb.WriteString(TruncateDiff(input.Diff))
	sb.WriteString("\n")
	sb.WriteString("Spec files:\n")
	sb.WriteString(ConcatSpecFiles(input.SpecFiles))
	sb.WriteString("\n\n")

	// === AGENT REVIEWS ===
	sb.WriteString("=== AGENT REVIEWS ===\n")
	sb.WriteString(fmt.Sprintf("Agent A (@%s, trust=%d): %s\n",
		input.AgentA.Name, input.AgentA.TrustLevel, input.VerdictA))
	sb.WriteString(defaultStr(input.BodyA, "(no review body)"))
	sb.WriteString("\n\n")
	sb.WriteString(fmt.Sprintf("Agent B (@%s, trust=%d): %s\n",
		input.AgentB.Name, input.AgentB.TrustLevel, input.VerdictB))
	sb.WriteString(defaultStr(input.BodyB, "(no review body)"))
	sb.WriteString("\n\n")

	// === DEBATE TRANSCRIPT ===
	sb.WriteString("=== DEBATE TRANSCRIPT ===\n")
	if len(input.DebateRounds) == 0 {
		sb.WriteString("(no debate rounds)\n")
	} else {
		for _, r := range input.DebateRounds {
			sb.WriteString(fmt.Sprintf("Round %d [%s]: %s\n", r.Number, r.Agent, r.Body))
		}
	}
	sb.WriteString("\n")

	// === QUESTION ===
	sb.WriteString("=== QUESTION ===\n")
	sb.WriteString("Resolve the conflict. Based on the spec, evidence, and debate:\n")
	sb.WriteString("APPROVE or REJECT?\n")

	return sb.String()
}

// AssembleFromNegotiator builds the arbiter prompt directly from a Negotiator's
// state. This is the convenience wrapper that connects the negotiation engine
// to Chimera. Uses the Negotiator's stored agents, verdicts, debate rounds,
// and PR context.
func AssembleFromNegotiator(n *Negotiator, title, description, diff string, specFiles []SpecFile) string {
	if n == nil {
		return buildEmptyArbiterPrompt()
	}

	input := &ArbiterInput{
		PRNumber:    n.Neg.PRNumber,
		Title:       title,
		Description: description,
		Diff:        diff,
		SpecFiles:   specFiles,
		AgentA:      n.Neg.AgentA,
		AgentB:      n.Neg.AgentB,
		VerdictA:    n.Neg.VerdictA,
		VerdictB:    n.Neg.VerdictB,
	}

	if n.Debate != nil {
		input.DebateRounds = n.Debate.Rounds
	}

	return AssembleArbiterInput(input)
}

// defaultStr returns the value or a fallback if empty.
func defaultStr(val, fallback string) string {
	if val == "" {
		return fallback
	}
	return val
}

// buildEmptyArbiterPrompt returns a minimal prompt for nil input.
func buildEmptyArbiterPrompt() string {
	return "=== QUESTION ===\nResolve the conflict: APPROVE or REJECT?\n"
}

// EstimatePromptSize estimates the approximate character count of the arbiter
// prompt without building it. Useful for pre-flight budget checks.
func EstimatePromptSize(input *ArbiterInput) int {
	if input == nil {
		return 0
	}

	size := 0
	// PR context overhead (~200 chars)
	size += 200
	size += len(input.Title)
	size += len(input.Description)
	size += min(len(input.Diff), MaxDiffChars)
	for _, f := range input.SpecFiles {
		size += len(f.Path) + len(f.Content) + 10
	}

	// Agent reviews (~300 chars overhead)
	size += 300
	size += len(input.BodyA)
	size += len(input.BodyB)

	// Debate rounds
	for _, r := range input.DebateRounds {
		size += len(r.Body) + 50
	}

	// Question section (~100 chars)
	size += 100

	return size
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
