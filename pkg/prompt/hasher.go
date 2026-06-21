package prompt

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

// reMultiSpace matches runs of two or more space characters for collapsing.
// Tabs are intentionally excluded — markdown indentation and code blocks
// rely on tabs.
var reMultiSpace = regexp.MustCompile(` {2,}`)

// Normalize applies the 5-step normalization pipeline from spec §9.1:
//  1. Normalize line endings to \n (CRLF and bare CR → LF)
//  2. Collapse whitespace: multiple spaces → single space
//  3. Strip trailing whitespace from each line
//  4. Strip trailing newline at end of file
//  5. Keep leading whitespace (markdown indentation matters)
func Normalize(prompt string) string {
	// Step 1: normalize line endings
	s := strings.ReplaceAll(prompt, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")

	// Step 2: collapse multiple spaces to single space
	s = reMultiSpace.ReplaceAllString(s, " ")

	// Step 3: strip trailing whitespace from each line
	lines := strings.Split(s, "\n")
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
