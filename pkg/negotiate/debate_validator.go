package negotiate

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Debate Round Validator — specs/pr-negotiation.md §7.2 + §7.5
// ---------------------------------------------------------------------------

// EvidenceType classifies the kind of evidence cited in a debate comment.
type EvidenceType string

const (
	EvidenceSpec     EvidenceType = "spec"     // Spec reference: <file> §<section>
	EvidenceTest     EvidenceType = "test"     // Test output: <command> → <result>
	EvidenceAC       EvidenceType = "ac"       // AC coverage: AC-<id> ...
	EvidenceGitReins EvidenceType = "gitreins" // GitReins verdict: Tier 2 ...
	EvidenceFinding  EvidenceType = "finding"  // Finding: <id> — <severity> — <response>
	EvidenceOther    EvidenceType = "other"    // Unrecognized evidence format
)

// EvidenceItem is a single piece of evidence in a debate comment (spec §7.2).
type EvidenceItem struct {
	Type    EvidenceType `json:"type"`
	Raw     string       `json:"raw"`     // original line text
	Content string       `json:"content"` // extracted substantive content
}

// IsSpecOrTest returns true if the evidence item cites a spec file or test output,
// satisfying the §7.2 requirement that "at least 1 must cite a spec file or test output".
func (e EvidenceItem) IsSpecOrTest() bool {
	return e.Type == EvidenceSpec || e.Type == EvidenceTest
}

// ParsedRoundComment is a structured representation of a debate round comment
// parsed from the §7.2 markdown format.
type ParsedRoundComment struct {
	RoundNum             int            `json:"round"`
	AgentName            string         `json:"agent"`
	TrustLevel           int            `json:"trust_level"`
	Position             string         `json:"position"` // "APPROVED" or "REQUEST_CHANGES"
	Evidence             []EvidenceItem `json:"evidence"`
	CounterArgument      string         `json:"counter_argument"`
	CounterArgumentAgent string         `json:"counter_argument_agent"` // the @<name> referenced
	ConcessionCondition  string         `json:"concession_condition"`
	HasConcession        bool           `json:"has_concession"`
	Body                 string         `json:"-"` // raw body for reference
}

// EvidenceValidation holds the result of validating a comment's evidence.
type EvidenceValidation struct {
	Valid            bool     `json:"valid"`
	Errors           []string `json:"errors,omitempty"`
	StrikeReason     string   `json:"strike_reason,omitempty"`
	EvidenceCount    int      `json:"evidence_count"`
	HasSpecOrTest    bool     `json:"has_spec_or_test"`
	HasCounterArgRef bool     `json:"has_counter_arg_ref"`
	MeetsMinimum     bool     `json:"meets_minimum"`
}

// StrikeReason constants (spec §7.5).
const (
	StrikeNoEvidence  = "posting_without_evidence"
	StrikeMissedRound = "missed_round"
)

// StrikeRecord captures one strike event for audit logging.
type StrikeRecord struct {
	Agent     string    `json:"agent"`
	Reason    string    `json:"reason"`
	Round     int       `json:"round"`
	Detail    string    `json:"detail"`
	Timestamp time.Time `json:"timestamp"`
}

// StrikeMaxStrikes is the number of strikes that triggers auto-concede (spec §7.5: "3 strikes").
const StrikeMaxStrikes = 3

// StrikeRoundMissThreshold is the number of round misses that triggers auto-concede
// on the 2nd miss (spec §7.5: "auto-concede on 2nd miss").
const StrikeRoundMissThreshold = 2

// StrikeTracker accumulates strikes per agent within a single negotiation
// (spec §7.5).
type StrikeTracker struct {
	mu          sync.Mutex
	strikes     map[string]int            // agent → total strike count
	strikeLog   map[string][]StrikeRecord // agent → detailed records
	roundMisses map[string]int            // agent → round-miss count
}

// NewStrikeTracker creates an empty strike tracker.
func NewStrikeTracker() *StrikeTracker {
	return &StrikeTracker{
		strikes:     make(map[string]int),
		strikeLog:   make(map[string][]StrikeRecord),
		roundMisses: make(map[string]int),
	}
}

// AccumulateStrike adds a strike to an agent with a reason and detail.
// Returns the new total strike count and whether auto-concede is triggered.
func (st *StrikeTracker) AccumulateStrike(agent string, reason string, round int, detail string, at time.Time) (count int, autoConcede bool) {
	st.mu.Lock()
	defer st.mu.Unlock()

	if reason == StrikeMissedRound {
		st.roundMisses[agent]++
	}

	st.strikes[agent]++
	count = st.strikes[agent]
	st.strikeLog[agent] = append(st.strikeLog[agent], StrikeRecord{
		Agent:     agent,
		Reason:    reason,
		Round:     round,
		Detail:    detail,
		Timestamp: at,
	})

	// 3 total strikes → auto-concede
	if count >= StrikeMaxStrikes {
		return count, true
	}
	// 2nd round miss → auto-concede
	if reason == StrikeMissedRound && st.roundMisses[agent] >= StrikeRoundMissThreshold {
		return count, true
	}
	return count, false
}

// StrikeCount returns the current strike count for an agent.
func (st *StrikeTracker) StrikeCount(agent string) int {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.strikes[agent]
}

// RoundMissCount returns the number of round misses for an agent.
func (st *StrikeTracker) RoundMissCount(agent string) int {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.roundMisses[agent]
}

// ShouldAutoConcede returns true if the agent has enough strikes to trigger
// auto-concede (3 strikes, or 2 round misses).
func (st *StrikeTracker) ShouldAutoConcede(agent string) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.strikes[agent] >= StrikeMaxStrikes {
		return true
	}
	if st.roundMisses[agent] >= StrikeRoundMissThreshold {
		return true
	}
	return false
}

// RecordRoundMiss records that an agent missed a round (no post within 5 min).
// Returns the new round-miss count and whether auto-concede is triggered
// (spec §7.5: "Missing a round → 1 strike + auto-concede on 2nd miss").
func (st *StrikeTracker) RecordRoundMiss(agent string, round int, at time.Time) (missCount int, autoConcede bool) {
	return st.AccumulateStrike(agent, StrikeMissedRound, round, "agent did not post within round timeout", at)
}

// GetStrikeLog returns the detailed strike records for an agent.
func (st *StrikeTracker) GetStrikeLog(agent string) []StrikeRecord {
	st.mu.Lock()
	defer st.mu.Unlock()
	// Return a copy to avoid external mutation
	log := st.strikeLog[agent]
	result := make([]StrikeRecord, len(log))
	copy(result, log)
	return result
}

// --- Parsing ---

// ParseRoundComment parses a structured debate comment body (spec §7.2 format)
// into a ParsedRoundComment. It extracts position, evidence items, counter-argument,
// and concession conditions.
//
// Expected format:
//
//	## Negotiation Round N — Agent: <name> (trust: <level>)
//	### Position
//	APPROVED | REQUEST_CHANGES
//	### Evidence
//	- [ ] Spec reference: <spec-file> §<section> — <excerpt>
//	- [ ] Test output: <test-command> → <result>
//	### Counter-Argument
//	In response to @<other-agent>'s point about <topic>:
//	### Concession Conditions
//	I will concede if: <condition>
func ParseRoundComment(body string) ParsedRoundComment {
	result := ParsedRoundComment{
		Body:     body,
		Evidence: []EvidenceItem{},
	}

	lines := strings.Split(body, "\n")
	currentSection := ""
	var evidenceLines []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Detect section headers
		switch {
		case strings.HasPrefix(trimmed, "## Negotiation Round"):
			result.RoundNum = extractRoundNumber(trimmed)
			result.AgentName = extractAgentFromHeader(trimmed)
			result.TrustLevel = extractTrustFromHeader(trimmed)
		case strings.HasPrefix(trimmed, "### Position"):
			currentSection = "position"
		case strings.HasPrefix(trimmed, "### Evidence"):
			currentSection = "evidence"
		case strings.HasPrefix(trimmed, "### Counter-Argument"):
			currentSection = "counter"
		case strings.HasPrefix(trimmed, "### Concession"):
			currentSection = "concession"
		case strings.HasPrefix(trimmed, "CONCEDE:"):
			result.HasConcession = true
			result.ConcessionCondition = strings.TrimPrefix(trimmed, "CONCEDE:")
			currentSection = ""
		default:
			// Process content based on current section
			switch currentSection {
			case "position":
				if trimmed != "" && (trimmed == "APPROVED" || trimmed == "REQUEST_CHANGES" ||
					strings.HasPrefix(trimmed, "APPROVED") || strings.HasPrefix(trimmed, "REQUEST_CHANGES")) {
					result.Position = extractPosition(trimmed)
				}
			case "evidence":
				if isEvidenceLine(trimmed) {
					evidenceLines = append(evidenceLines, trimmed)
				}
			case "counter":
				if trimmed != "" {
					// Collect counter-argument content
					if result.CounterArgument != "" {
						result.CounterArgument += "\n"
					}
					result.CounterArgument += trimmed
					// Extract referenced agent
					if result.CounterArgumentAgent == "" {
						if agent := extractAtMention(trimmed); agent != "" {
							result.CounterArgumentAgent = agent
						}
					}
				}
			case "concession":
				if trimmed != "" && !strings.HasPrefix(trimmed, "###") {
					result.ConcessionCondition = trimmed
				}
			}
		}
	}

	// Parse evidence lines into EvidenceItem structs
	for _, el := range evidenceLines {
		result.Evidence = append(result.Evidence, parseEvidenceLine(el))
	}

	return result
}

// ValidateEvidence checks all evidence requirements from spec §7.2:
//  1. Minimum 2 evidence items per comment
//  2. At least 1 must cite a spec file or test output
//  3. At least 1 must reference the other agent's previous argument
//
// "I disagree" without evidence → comment rejected, agent gets strike.
func ValidateEvidence(comment ParsedRoundComment) EvidenceValidation {
	result := EvidenceValidation{
		EvidenceCount: len(comment.Evidence),
	}

	// Requirement 1: minimum 2 evidence items
	result.MeetsMinimum = result.EvidenceCount >= 2
	if !result.MeetsMinimum {
		result.Errors = append(result.Errors,
			fmt.Sprintf("minimum 2 evidence items required, got %d (spec §7.2)", result.EvidenceCount))
	}

	// Requirement 2: at least 1 spec or test reference
	for _, e := range comment.Evidence {
		if e.IsSpecOrTest() {
			result.HasSpecOrTest = true
			break
		}
	}
	if !result.HasSpecOrTest {
		result.Errors = append(result.Errors,
			"at least 1 evidence item must cite a spec file or test output (spec §7.2)")
	}

	// Requirement 3: at least 1 must reference the other agent's previous argument
	result.HasCounterArgRef = comment.CounterArgumentAgent != "" && comment.CounterArgument != ""
	if !result.HasCounterArgRef {
		result.Errors = append(result.Errors,
			"at least 1 evidence item must reference the other agent's previous argument (spec §7.2)")
	}

	result.Valid = len(result.Errors) == 0
	if !result.Valid {
		result.StrikeReason = StrikeNoEvidence
	}

	return result
}

// ValidateComment is a convenience function that parses a body string and validates
// its evidence requirements in one call. Returns the parsed comment and validation result.
func ValidateComment(body string) (ParsedRoundComment, EvidenceValidation) {
	comment := ParseRoundComment(body)
	return comment, ValidateEvidence(comment)
}

// --- Evidence parsing helpers ---

// isEvidenceLine checks if a line looks like an evidence bullet item.
func isEvidenceLine(line string) bool {
	// Evidence lines start with "- [ ]" or "- " or "* "
	return strings.HasPrefix(line, "- [ ]") ||
		strings.HasPrefix(line, "- [x]") ||
		strings.HasPrefix(line, "- ") ||
		strings.HasPrefix(line, "* ")
}

// parseEvidenceLine classifies a single evidence line and extracts content.
func parseEvidenceLine(line string) EvidenceItem {
	// Strip bullet prefix
	content := line
	for _, prefix := range []string{"- [ ] ", "- [x] ", "- ", "* "} {
		if strings.HasPrefix(content, prefix) {
			content = strings.TrimPrefix(content, prefix)
			break
		}
	}
	content = strings.TrimSpace(content)

	lower := strings.ToLower(content)
	switch {
	case strings.HasPrefix(lower, "spec reference:") || strings.HasPrefix(lower, "spec:"):
		return EvidenceItem{Type: EvidenceSpec, Raw: line, Content: content}
	case strings.HasPrefix(lower, "test output:") || strings.HasPrefix(lower, "test:"):
		return EvidenceItem{Type: EvidenceTest, Raw: line, Content: content}
	case strings.HasPrefix(lower, "ac coverage:") || strings.HasPrefix(lower, "ac-"):
		return EvidenceItem{Type: EvidenceAC, Raw: line, Content: content}
	case strings.HasPrefix(lower, "gitreins verdict:") || strings.HasPrefix(lower, "gitreins:"):
		return EvidenceItem{Type: EvidenceGitReins, Raw: line, Content: content}
	case strings.HasPrefix(lower, "finding:"):
		return EvidenceItem{Type: EvidenceFinding, Raw: line, Content: content}
	default:
		return EvidenceItem{Type: EvidenceOther, Raw: line, Content: content}
	}
}

// extractRoundNumber extracts the round number from a header like
// "## Negotiation Round 2 — Agent: ...".
func extractRoundNumber(header string) int {
	// Look for "Round N" pattern
	lower := strings.ToLower(header)
	idx := strings.Index(lower, "round ")
	if idx == -1 {
		return 0
	}
	rest := header[idx+6:]
	num := 0
	for _, c := range rest {
		if c >= '0' && c <= '9' {
			num = num*10 + int(c-'0')
		} else if num > 0 {
			break
		}
	}
	return num
}

// extractAgentFromHeader extracts the agent name from a header like
// "## Negotiation Round 2 — Agent: alice (trust: 85)".
func extractAgentFromHeader(header string) string {
	lower := strings.ToLower(header)
	idx := strings.Index(lower, "agent:")
	if idx == -1 {
		return ""
	}
	rest := header[idx+6:]
	rest = strings.TrimSpace(rest)
	// Name ends at " (" or " —" or "("
	for _, sep := range []string{" (", " (trust", " —"} {
		if si := strings.Index(rest, sep); si != -1 {
			return strings.TrimSpace(rest[:si])
		}
	}
	return strings.TrimSpace(rest)
}

// extractTrustFromHeader extracts the trust level from a header like
// "## Negotiation Round 2 — Agent: alice (trust: 85)".
func extractTrustFromHeader(header string) int {
	lower := strings.ToLower(header)
	idx := strings.Index(lower, "trust:")
	if idx == -1 {
		return 0
	}
	rest := header[idx+6:]
	num := 0
	for _, c := range rest {
		if c >= '0' && c <= '9' {
			num = num*10 + int(c-'0')
		} else if num > 0 {
			break
		}
	}
	return num
}

// extractPosition extracts the position from a line like "APPROVED" or "REQUEST_CHANGES".
func extractPosition(line string) string {
	upper := strings.ToUpper(strings.TrimSpace(line))
	if strings.HasPrefix(upper, "APPROVED") {
		return "APPROVED"
	}
	if strings.HasPrefix(upper, "REQUEST_CHANGES") {
		return "REQUEST_CHANGES"
	}
	return ""
}

// extractAtMention extracts the @<name> from a line.
func extractAtMention(line string) string {
	idx := strings.Index(line, "@")
	if idx == -1 {
		return ""
	}
	rest := line[idx+1:]
	end := 0
	for end < len(rest) {
		c := rest[end]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '_' || c == '-' {
			end++
		} else {
			break
		}
	}
	if end == 0 {
		return ""
	}
	return rest[:end]
}
