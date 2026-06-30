package prompt

import "strings"

// NormalizeForHash applies the full prompt-registry-v2 §8.2-§8.3 normalization
// pipeline to raw prompt text. This is the standalone, spec-compliant
// normalizer that the existing hasher.go Normalize function can delegate to.
//
// Pipeline (5 steps + frontmatter strip):
//  0. Strip YAML frontmatter (--- … ---) if present.
//  1. Normalize line endings: CRLF and lone CR → LF.
//  2. Collapse runs of spaces/tabs within a line to a single space —
//     suppressed inside fenced code blocks (``` or ~~~).
//  3. Strip trailing whitespace from each line.
//  4. Ensure exactly one trailing newline at EOF.
//  5. Preserve leading whitespace (markdown indentation is semantic).
//
// An unclosed fence is treated as "inside" until EOF (spec §8.3).
func NormalizeForHash(raw string) string {
	// Step 0: strip YAML frontmatter.
	s := stripYAMLFrontmatter(raw)

	// Step 1: normalize line endings.
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")

	// Step 2: collapse runs of spaces/tabs to single space, suppressed
	// inside fenced code blocks.
	lines := strings.Split(s, "\n")
	inFence := false
	for i, line := range lines {
		if isFenceLine(line) {
			inFence = !inFence
			continue
		}
		if !inFence {
			lines[i] = collapseSpacesAndTabs(line)
		}
	}

	// Step 3: strip trailing whitespace (spaces + tabs) from each line.
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	s = strings.Join(lines, "\n")

	// Step 4: ensure exactly one trailing newline at EOF.
	// Strip all trailing newlines first, then add exactly one — unless the
	// content is empty (empty input stays empty).
	s = strings.TrimRight(s, "\n")
	if s != "" {
		s += "\n"
	}

	// Step 5: leading whitespace is preserved — no action needed.
	return s
}

// collapseSpacesAndTabs collapses runs of consecutive spaces and/or tabs
// into a single space character. Unlike collapseSpaces (which only handles
// runs of space characters), this also collapses tabs per spec §8.2 step 2.
//
// Leading whitespace is preserved (step 5): only mid-line runs are collapsed.
// This means leading spaces/tabs at the start of a line are kept as-is.
func collapseSpacesAndTabs(s string) string {
	// Count leading whitespace to preserve it.
	leading := 0
	for leading < len(s) && (s[leading] == ' ' || s[leading] == '\t') {
		leading++
	}

	// Process the rest of the line (after leading whitespace).
	if leading == len(s) {
		return s // entire line is whitespace — unchanged
	}

	rest := s[leading:]
	var b strings.Builder
	b.WriteString(s[:leading]) // preserve leading whitespace exactly

	prevSpaceTab := false
	for i := 0; i < len(rest); i++ {
		c := rest[i]
		if c == ' ' || c == '\t' {
			if !prevSpaceTab {
				b.WriteByte(' ')
				prevSpaceTab = true
			}
			// skip additional spaces/tabs
		} else {
			b.WriteByte(c)
			prevSpaceTab = false
		}
	}
	return b.String()
}
