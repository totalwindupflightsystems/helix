package prompt

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// reMultiSpace matches runs of two or more space characters for collapsing.
// Tabs are intentionally excluded — markdown indentation and code blocks
// rely on tabs. Inside fenced code blocks, whitespace collapse is suppressed
// entirely (prompt-registry-v2 spec §8.3).
var reMultiSpace = strings.NewReplacer(
	"  ", " ",
	"   ", " ",
	"    ", " ",
	"     ", " ",
	"      ", " ",
	"       ", " ",
	"        ", " ",
)

// collapseSpaces collapses runs of spaces within a single line to a single
// space. Uses iterative replacement (up to 8-wide runs in one pass, then
// repeated until stable). Called for lines outside fenced code blocks.
func collapseSpaces(s string) string {
	for {
		next := reMultiSpace.Replace(s)
		if next == s {
			return s
		}
		s = next
	}
}

// isFenceLine returns true if line is a fenced-code-block delimiter.
// Recognises ``` and ~~~ (with optional language specifier after).
func isFenceLine(line string) bool {
	trimmed := strings.TrimLeft(line, " ")
	if strings.HasPrefix(trimmed, "```") {
		return true
	}
	if strings.HasPrefix(trimmed, "~~~") {
		return true
	}
	return false
}

// stripYAMLFrontmatter strips a leading YAML frontmatter block
// (--- … ---) so only the markdown body is content-addressed.
// prompt-registry-v2 spec §8.2.
func stripYAMLFrontmatter(s string) string {
	if !strings.HasPrefix(s, "---\n") && !strings.HasPrefix(s, "---\r\n") && !strings.HasPrefix(s, "---\r") {
		return s
	}
	// Find the closing --- on a line by itself.
	rest := s[3:] // skip leading "---"
	// Normalize line endings first so we can search for \n---\n
	rest = strings.ReplaceAll(rest, "\r\n", "\n")
	rest = strings.ReplaceAll(rest, "\r", "\n")
	idx := strings.Index(rest, "\n---\n")
	if idx == -1 {
		// Also check for --- at end (last line, no trailing newline).
		if strings.HasSuffix(rest, "\n---") {
			body := rest[:len(rest)-4]
			return strings.TrimLeft(body, "\n")
		}
		return s // no closing delimiter, treat whole thing as body
	}
	body := rest[idx+4:] // skip "\n---\n"
	body = strings.TrimLeft(body, "\n")
	return body
}

// Normalize applies the normalization pipeline from prompt-registry-v2 §8.2:
//  0. Strip YAML frontmatter (--- … ---) if present.
//  1. Normalize line endings to \n (CRLF and bare CR → LF).
//  2. Collapse whitespace: multiple spaces → single space OUTSIDE fenced code blocks.
//     Inside fenced blocks (``` or ~~~), whitespace is preserved exactly.
//  3. Strip trailing whitespace from each line.
//  4. Strip trailing newline at end of file.
//  5. Keep leading whitespace (markdown indentation matters).
func Normalize(prompt string) string {
	// Step 0: strip YAML frontmatter
	s := stripYAMLFrontmatter(prompt)

	// Step 1: normalize line endings
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")

	// Step 2: collapse multiple spaces to single space, suppressed inside fences
	lines := strings.Split(s, "\n")
	inFence := false
	for i, line := range lines {
		if isFenceLine(line) {
			inFence = !inFence
			continue
		}
		if !inFence {
			lines[i] = collapseSpaces(line)
		}
	}

	// Step 3: strip trailing whitespace from each line
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	s = strings.Join(lines, "\n")

	// Step 4: strip trailing newline at end
	s = strings.TrimRight(s, "\n")

	// Step 5: keep leading whitespace — no action needed
	return s
}

// Hash returns the content-addressed SHA-256 hash of a normalized prompt,
// prefixed with "sha256:" per spec §9.1.
func Hash(prompt string) string {
	normalized := Normalize(prompt)
	sum := sha256.Sum256([]byte(normalized))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// VerifyHash checks whether a prompt's normalized hash matches the expected
// hash string. Used for tamper detection (spec §11.3).
func VerifyHash(prompt string, expectedHash string) bool {
	return Hash(prompt) == expectedHash
}
