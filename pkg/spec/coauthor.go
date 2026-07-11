package spec

import (
	"fmt"
	"strings"
	"time"
)

// Standard section titles the @spec-generator checks for.
var standardSections = []string{
	"Overview",
	"Requirements",
	"Non-Goals",
	"Constraints",
	"Acceptance Criteria",
}

// SpecCoAuthor produces an annotated spec by dispatching two deterministic
// rule-based agent personas. No LLM calls — all analysis is structural.
type SpecCoAuthor struct{}

// NewSpecCoAuthor returns a co-author instance.
func NewSpecCoAuthor() *SpecCoAuthor { return &SpecCoAuthor{} }

// CoAuthor analyzes a spec and returns the spec with annotations from both
// the @spec-generator and @spec-challenger personas.
func (c *SpecCoAuthor) CoAuthor(spec *Spec) (*Spec, error) {
	if spec == nil {
		return nil, fmt.Errorf("spec: spec is nil")
	}
	if spec.ID == "" {
		return nil, fmt.Errorf("spec: id is required")
	}

	now := time.Now().UTC()
	var annotations []SpecAnnotation

	for _, ann := range c.generate(spec) {
		ann.CreatedAt = now
		ann.Status = AnnotationProposed
		annotations = append(annotations, ann)
	}
	for _, ann := range c.challenge(spec) {
		ann.CreatedAt = now
		ann.Status = AnnotationProposed
		annotations = append(annotations, ann)
	}

	spec.Annotations = annotations
	spec.Status = StatusInReview
	spec.UpdatedAt = now
	return spec, nil
}

// ---------------------------------------------------------------------------
// @spec-generator — proposes edge cases, failure modes, missing sections
// ---------------------------------------------------------------------------

// generate runs the @spec-generator persona and returns its annotations.
func (c *SpecCoAuthor) generate(spec *Spec) []SpecAnnotation {
	var out []SpecAnnotation

	// 1. Missing standard sections.
	haveSections := sectionTitleSet(spec.Sections)
	for _, want := range standardSections {
		if !haveSections[want] {
			out = append(out, SpecAnnotation{
				Line:           1,
				AgentType:      AgentSpecGenerator,
				AnnotationType: AnnIncompleteness,
				Content:        fmt.Sprintf("Missing standard section: %q — consider adding it for structural completeness", want),
				Severity:       SeverityWarning,
			})
		}
	}

	// 2. Edge cases and failure modes for each section.
	for _, sec := range spec.Sections {
		sectionLine := findSectionLine(spec, sec.Title)
		out = append(out, generatorEdgeCases(sec.Title, sec.Content, sectionLine)...)
		out = append(out, generatorFailureModes(sec.Title, sec.Content, sectionLine)...)
	}

	return out
}

func generatorEdgeCases(sectionTitle, content string, line int) []SpecAnnotation {
	var out []SpecAnnotation
	lower := strings.ToLower(content)
	// Pattern: input handling
	if containsAny(lower, "input", "request", "payload", "parameter", "argument") {
		if !strings.Contains(lower, "empty") && !strings.Contains(lower, "zero-length") && !strings.Contains(lower, "nil") {
			out = append(out, SpecAnnotation{
				Line:           line,
				AgentType:      AgentSpecGenerator,
				AnnotationType: AnnEdgeCase,
				Content:        fmt.Sprintf("Section %q handles input but does not address empty/zero-length/nil input", sectionTitle),
				Severity:       SeverityWarning,
			})
		}
	}
	// Pattern: timeout
	if containsAny(lower, "timeout", "deadline", "latency", "async", "concurrent", "goroutine", "channel") {
		if !strings.Contains(lower, "deadline exceeded") && !strings.Contains(lower, "timeout error") && !strings.Contains(lower, "cancel") {
			out = append(out, SpecAnnotation{
				Line:           line,
				AgentType:      AgentSpecGenerator,
				AnnotationType: AnnEdgeCase,
				Content:        fmt.Sprintf("Section %q involves timeouts/concurrency but does not address cancellation or deadline-exceeded behavior", sectionTitle),
				Severity:       SeverityWarning,
			})
		}
	}
	// Pattern: rate limiting / throttling
	if containsAny(lower, "rate", "throttle", "quota", "limit") {
		if !strings.Contains(lower, "429") && !strings.Contains(lower, "retry") && !strings.Contains(lower, "backoff") {
			out = append(out, SpecAnnotation{
				Line:           line,
				AgentType:      AgentSpecGenerator,
				AnnotationType: AnnEdgeCase,
				Content:        fmt.Sprintf("Section %q mentions rate limiting but does not address throttled/429 response handling or backoff strategy", sectionTitle),
				Severity:       SeverityInfo,
			})
		}
	}
	// Pattern: concurrent writes
	if containsAny(lower, "concurrent", "parallel", "shared state", "mutex") {
		if !strings.Contains(lower, "race") && !strings.Contains(lower, "lock") && !strings.Contains(lower, "mutex") {
			out = append(out, SpecAnnotation{
				Line:           line,
				AgentType:      AgentSpecGenerator,
				AnnotationType: AnnEdgeCase,
				Content:        fmt.Sprintf("Section %q involves concurrency but does not address race conditions or locking", sectionTitle),
				Severity:       SeverityWarning,
			})
		}
	}
	return out
}

func generatorFailureModes(sectionTitle, content string, line int) []SpecAnnotation {
	var out []SpecAnnotation
	lower := strings.ToLower(content)
	// Pattern: external dependency
	if containsAny(lower, "database", "db", "redis", "kafka", "queue", "external", "upstream", "third-party", "api") {
		if !strings.Contains(lower, "unavailable") && !strings.Contains(lower, "down") && !strings.Contains(lower, "circuit breaker") && !strings.Contains(lower, "fallback") {
			out = append(out, SpecAnnotation{
				Line:           line,
				AgentType:      AgentSpecGenerator,
				AnnotationType: AnnFailureMode,
				Content:        fmt.Sprintf("Section %q depends on external service(s) but does not address what happens when the dependency is down", sectionTitle),
				Severity:       SeverityCritical,
			})
		}
	}
	// Pattern: data persistence
	if containsAny(lower, "store", "persist", "database", "save", "write", "insert") {
		if !strings.Contains(lower, "corrupt") && !strings.Contains(lower, "validation") && !strings.Contains(lower, "checksum") {
			out = append(out, SpecAnnotation{
				Line:           line,
				AgentType:      AgentSpecGenerator,
				AnnotationType: AnnFailureMode,
				Content:        fmt.Sprintf("Section %q involves data persistence but does not address corrupt or invalid data handling", sectionTitle),
				Severity:       SeverityWarning,
			})
		}
	}
	// Pattern: auth/security without error handling
	if containsAny(lower, "auth", "token", "credential", "password", "permission", "rbac") {
		if !strings.Contains(lower, "unauthorized") && !strings.Contains(lower, "forbidden") && !strings.Contains(lower, "401") && !strings.Contains(lower, "403") {
			out = append(out, SpecAnnotation{
				Line:           line,
				AgentType:      AgentSpecGenerator,
				AnnotationType: AnnFailureMode,
				Content:        fmt.Sprintf("Section %q handles auth/credentials but does not address unauthorized/forbidden access failures", sectionTitle),
				Severity:       SeverityCritical,
			})
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// @spec-challenger — reviews for consistency, alignment, threat surface
// ---------------------------------------------------------------------------

// challenge runs the @spec-challenger persona and returns its annotations.
func (c *SpecCoAuthor) challenge(spec *Spec) []SpecAnnotation {
	var out []SpecAnnotation

	// 1. Cross-section consistency — look for contradictions.
	out = append(out, challengerConsistency(spec)...)

	// 2. ADR alignment.
	if len(spec.ADRRefs) == 0 && len(spec.Sections) > 0 {
		out = append(out, SpecAnnotation{
			Line:           1,
			AgentType:      AgentSpecChallenger,
			AnnotationType: AnnIncompleteness,
			Content:        "No ADR references linked — architectural decisions are untraceable",
			Severity:       SeverityInfo,
		})
	}

	// 3. Contract references.
	if len(spec.ContractRefs) == 0 && len(spec.Sections) > 0 {
		out = append(out, SpecAnnotation{
			Line:           1,
			AgentType:      AgentSpecChallenger,
			AnnotationType: AnnIncompleteness,
			Content:        "No contract references linked — API/service contracts are unspecified",
			Severity:       SeverityInfo,
		})
	}

	// 4. Threat surface gaps.
	out = append(out, challengerThreatSurface(spec)...)

	return out
}

func challengerConsistency(spec *Spec) []SpecAnnotation {
	var out []SpecAnnotation

	// Build full text for keyword analysis.
	allText := strings.Builder{}
	for _, sec := range spec.Sections {
		allText.WriteString(sec.Content)
		allText.WriteString("\n")
	}
	fullLower := strings.ToLower(allText.String())

	// Detect contradiction: "sync" vs "async", "sync" vs "eventually consistent"
	mentionsSync := strings.Contains(fullLower, "synchronous") || (strings.Contains(fullLower, "sync ") && !strings.Contains(fullLower, "async"))
	mentionsAsync := strings.Contains(fullLower, "asynchronous") || strings.Contains(fullLower, "async")
	if mentionsSync && mentionsAsync {
		line := 0
		for _, sec := range spec.Sections {
			lc := strings.ToLower(sec.Content)
			if strings.Contains(lc, "synchronous") || strings.Contains(lc, "asynchronous") {
				line = findSectionLine(spec, sec.Title)
				break
			}
		}
		out = append(out, SpecAnnotation{
			Line:           line,
			AgentType:      AgentSpecChallenger,
			AnnotationType: AnnConsistency,
			Content:        "Spec mentions both synchronous and asynchronous patterns — verify this is intentional and clarify per-operation",
			Severity:       SeverityWarning,
		})
	}

	// Detect contradiction: "strong consistency" vs "eventual consistency"
	if strings.Contains(fullLower, "strong consistency") && strings.Contains(fullLower, "eventual") {
		out = append(out, SpecAnnotation{
			Line:           findSectionLine(spec, spec.Sections[0].Title),
			AgentType:      AgentSpecChallenger,
			AnnotationType: AnnConsistency,
			Content:        "Spec references both strong and eventual consistency — contradictory consistency guarantees",
			Severity:       SeverityCritical,
		})
	}

	// Duplicate requirement detection: same MUST/SHALL line repeated.
	mustLines := extractMustLines(allText.String())
	seen := make(map[string]bool)
	for _, ml := range mustLines {
		key := strings.TrimSpace(strings.ToLower(ml))
		if seen[key] {
			out = append(out, SpecAnnotation{
				Line:           0,
				AgentType:      AgentSpecChallenger,
				AnnotationType: AnnConsistency,
				Content:        fmt.Sprintf("Duplicate requirement: %q appears more than once", truncateForDisplay(ml, 80)),
				Severity:       SeverityWarning,
			})
		}
		seen[key] = true
	}

	return out
}

func challengerThreatSurface(spec *Spec) []SpecAnnotation {
	var out []SpecAnnotation

	allText := strings.Builder{}
	for _, sec := range spec.Sections {
		allText.WriteString(sec.Content)
		allText.WriteString("\n")
	}
	fullLower := strings.ToLower(allText.String())

	// If spec mentions user data, credentials, or auth but no security section.
	hasSecuritySection := false
	for _, sec := range spec.Sections {
		if strings.EqualFold(sec.Title, "Security") || strings.Contains(strings.ToLower(sec.Title), "threat") {
			hasSecuritySection = true
			break
		}
	}

	handlesSensitive := containsAny(fullLower, "user data", "personal", "credential", "password", "token", "secret", "pii")
	if handlesSensitive && !hasSecuritySection {
		out = append(out, SpecAnnotation{
			Line:           1,
			AgentType:      AgentSpecChallenger,
			AnnotationType: AnnSecurity,
			Content:        "Spec handles sensitive data but has no dedicated Security/Threat-Model section",
			Severity:       SeverityCritical,
		})
	}

	// If spec mentions network/API but no input validation.
	if containsAny(fullLower, "api", "endpoint", "http", "rest", "grpc", "graphql") {
		if !containsAny(fullLower, "validation", "sanitize", "input check", "schema validation") {
			out = append(out, SpecAnnotation{
				Line:           1,
				AgentType:      AgentSpecChallenger,
				AnnotationType: AnnSecurity,
				Content:        "Spec exposes API endpoints but does not mention input validation or sanitization",
				Severity:       SeverityWarning,
			})
		}
	}

	return out
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func sectionTitleSet(sections []SpecSection) map[string]bool {
	m := make(map[string]bool, len(sections))
	for _, sec := range sections {
		m[sec.Title] = true
	}
	return m
}

// findSectionLine returns the approximate 1-based line number of a section
// title within the serialized spec. Line numbers are informational for
// annotation display — we compute them from section ordering.
func findSectionLine(spec *Spec, title string) int {
	line := 1 // start after frontmatter
	for _, sec := range spec.Sections {
		if sec.Title == title {
			return line
		}
		// title line + blank + content lines + blank.
		line += 1
		if sec.Content != "" {
			line += strings.Count(sec.Content, "\n") + 1
		}
		line += 1
	}
	return 1
}

func containsAny(haystack string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			return true
		}
	}
	return false
}

// extractMustLines returns lines containing MUST, SHALL, or MUST NOT.
func extractMustLines(text string) []string {
	var out []string
	lines := strings.Split(text, "\n")
	upperKeywords := []string{"must ", "shall ", "must not", "shall not"}
	for _, line := range lines {
		lower := strings.ToLower(strings.TrimSpace(line))
		for _, kw := range upperKeywords {
			if strings.Contains(lower, kw) {
				out = append(out, strings.TrimSpace(line))
				break
			}
		}
	}
	return out
}

func truncateForDisplay(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
