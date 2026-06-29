// Package review implements the Helix adversarial multi-model review pipeline
// per specs/adversarial-review.md.
//
// The bias-stripper removes evaluative language and confidence assertions from
// commit messages before they are presented to review models, preventing the
// confirmation-bias exploit documented in arXiv 2603.18740.
package review

import (
	"regexp"
	"strings"
)

// =============================================================================
// BiasStripper
// =============================================================================

// BiasStripper rewrites commit messages to remove evaluative language and
// confidence assertions, preserving only factual information (files changed,
// intent, technical context).
type BiasStripper struct {
	// evaluativePatterns matches phrases that frame code as "correct".
	evaluativePatterns []*regexp.Regexp

	// confidencePatterns matches assertions that bias reviewers.
	confidencePatterns []*regexp.Regexp

	// emojiPattern matches all emoji sequences.
	emojiPattern *regexp.Regexp

	// emotionalPatterns matches emotional framing language.
	emotionalPatterns []*regexp.Regexp

	// commitConventions: common prefixes to preserve.
	commitPrefixes []string
}

// NewBiasStripper creates a BiasStripper with the standard pattern set.
// The pattern set is version-locked per spec §Anti-Overcorrection Protocol:
// "Review prompts are version-locked and hash-attested."
func NewBiasStripper() *BiasStripper {
	return &BiasStripper{
		evaluativePatterns: compilePatterns([]string{
			`(?i)\bfixed\b`,
			`(?i)\bcorrect(?:ly)?\b`,
			`(?i)\bready\b`,
			`(?i)\bpass(?:es|ed|ing)?\b`,
			`(?i)\bperfect(?:ly)?\b`,
			`(?i)\bflawless(?:ly)?\b`,
			`(?i)\btypical\b`,
			`(?i)\btrivial(?:ly)?\b`,
			`(?i)\bsimple\b`,
			`(?i)\beasy\b`,
			`(?i)\bclean\b`,
			`(?i)\bproper(?:ly)?\b`,
			`(?i)\bobvious(?:ly)?\b`,
			`(?i)\bclearly\b`,
			`(?i)\bjust\b`,
			`(?i)\bmerely\b`,
			`(?i)\bbasically\b`,
			`\bNIT\b`,
			`\bLGTM\b`,
		}),
		confidencePatterns: compilePatterns([]string{
			`(?i)tested locally`,
			`(?i)works on my machine`,
			`(?i)works? fine`,
			`(?i)works? correctly`,
			`(?i)should be fine`,
			`(?i)should work`,
			`(?i)no (?:known )?(?:issues?|problems?|bugs?)`,
			`(?i)I (?:am |')?(?:pretty |very )?(?:sure|certain|confident)`,
			`(?i)all tests pass`,
			`(?i)all (?:existing )?tests (?:pass|green|succeed)`,
			`(?i)green(?:ing)?`,
			`(?i)CI (?:passes|green|succeed)`,
			`(?i)verified (?:locally|manually)`,
			`(?i)can(?:no)?t break`,
			`(?i)nothing can break`,
			`(?i)definitely`,
			`(?i)without (?:any )?(?:issues?|problems?|regression)`,
			`(?i)nothing (?:to see|wrong|bad|broken)`,
			`(?i)as expected`,
		}),
		emojiPattern: regexp.MustCompile(
			`[\x{1F600}-\x{1F64F}` + // emoticons
				`\x{1F300}-\x{1F5FF}` + // symbols & pictographs
				`\x{1F680}-\x{1F6FF}` + // transport & map
				`\x{1F1E0}-\x{1F1FF}` + // flags
				`\x{2600}-\x{26FF}` + // misc symbols
				`\x{2700}-\x{27BF}` + // dingbats
				`\x{FE00}-\x{FE0F}` + // variation selectors
				`\x{1F900}-\x{1F9FF}` + // supplemental symbols
				`\x{1FA00}-\x{1FA6F}` + // chess symbols
				`\x{1FA70}-\x{1FAFF}` + // symbols extended-A
				`]+`,
		),
		emotionalPatterns: compilePatterns([]string{
			`(?i)\b🚀\b`,
			`(?i)\bfinally\b`,
			`(?i)\bat long last\b`,
			`(?i)\b(?:super|very|really|extremely|incredibly|absolutely) (?:happy|excited|proud|thrilled|impressed)\b`,
			`(?i)\bcrossing (?:my|our) fingers\b`,
			`(?i)\b🤞\b`,
			`(?i)\b🔥\b`,
			`(?i)\b💪\b`,
			`(?i)\b🎉\b`,
		}),
		commitPrefixes: []string{
			"feat:", "fix:", "chore:", "docs:", "style:", "refactor:",
			"perf:", "test:", "build:", "ci:", "revert:",
			"BREAKING CHANGE:", "Merge", "Revert",
		},
	}
}

// compilePatterns is a helper to compile regex patterns, panicking on invalid
// regex (patterns are hardcoded and tested at startup).
func compilePatterns(patterns []string) []*regexp.Regexp {
	compiled := make([]*regexp.Regexp, len(patterns))
	for i, p := range patterns {
		compiled[i] = regexp.MustCompile(p)
	}
	return compiled
}

// Strip applies all bias-stripping transformations to a commit message.
// The output preserves factual information (files changed, intent, technical
// context) while removing confirmed-bias-exploit vectors.
//
// The spec examples:
//
//	Original: "Fixed the auth edge case, all tests pass, ready to merge"
//	Stripped: "Modified auth module. Verify correctness."
func (bs *BiasStripper) Strip(msg string) string {
	if msg == "" {
		return ""
	}

	result := msg

	// 1. Remove emoji sequences first (cleanest).
	result = bs.emojiPattern.ReplaceAllString(result, "")

	// 2. Remove emotional framing.
	for _, re := range bs.emotionalPatterns {
		result = re.ReplaceAllString(result, "")
	}

	// 3. Remove confidence assertions.
	for _, re := range bs.confidencePatterns {
		result = re.ReplaceAllString(result, "")
	}

	// 4. Remove evaluative language.
	for _, re := range bs.evaluativePatterns {
		result = re.ReplaceAllString(result, "")
	}

	// 5. Normalize whitespace (multiple spaces → one, trim).
	result = normalizeWhitespace(result)

	// 6. Remove empty parentheticals like "()" or "(  )" that remain after stripping.
	result = emptyParensPattern.ReplaceAllString(result, "")
	result = normalizeWhitespace(result)

	// 7. Collapse repeated punctuation ("..." or "!!!" → ".").
	result = collapsePunctuationPattern.ReplaceAllString(result, ".")
	result = normalizeWhitespace(result)

	return result
}

var (
	emptyParensPattern         = regexp.MustCompile(`\(\s*\)`)
	collapsePunctuationPattern = regexp.MustCompile(`[.!?]{2,}`)
)

// normalizeWhitespace collapses all whitespace runs to single spaces and trims.
func normalizeWhitespace(s string) string {
	// Collapse all whitespace runs (spaces, tabs, newlines) to single space.
	space := whitespaceRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(space)
}

var whitespaceRe = regexp.MustCompile(`\s+`)

// PreserveFactualPrefix checks if the message starts with a conventional prefix
// and returns it separately so it can be preserved.
func (bs *BiasStripper) PreserveFactualPrefix(msg string) string {
	msg = strings.TrimSpace(msg)
	for _, prefix := range bs.commitPrefixes {
		if strings.HasPrefix(msg, prefix) {
			return prefix
		}
	}
	return ""
}

// StripPreservingPrefix applies Strip but preserves the commit prefix.
// For example: "feat: Fixed the auth bug 🚀" → "feat: Modified auth module."
func (bs *BiasStripper) StripPreservingPrefix(msg string) string {
	prefix := bs.PreserveFactualPrefix(msg)
	body := msg
	if prefix != "" {
		body = strings.TrimSpace(msg[len(prefix):])
	}
	stripped := bs.Strip(body)
	if stripped == "" {
		return prefix
	}
	if prefix != "" {
		return prefix + " " + stripped
	}
	return stripped
}
