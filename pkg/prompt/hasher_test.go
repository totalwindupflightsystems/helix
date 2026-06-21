// Package prompt_test contains external tests for the prompt package.
//
// These tests cover pkg/prompt/hasher.go — the content-addressed SHA-256
// hasher used for prompt tamper detection (spec §9.1, §11.3).
package prompt_test

import (
	"strings"
	"testing"

	"github.com/totalwindupflightsystems/helix/pkg/prompt"
)

// ---------------------------------------------------------------------------
// TestNormalize
// ---------------------------------------------------------------------------

func TestNormalize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		// Empty string passes through every step unchanged.
		{
			name:  "empty string",
			input: "",
			want:  "",
		},

		// Single line, no transformations required.
		{
			name:  "single line no changes",
			input: "hello world",
			want:  "hello world",
		},

		// Step 1a: CRLF → LF
		{
			name:  "CRLF line endings to LF",
			input: "line1\r\nline2\r\nline3",
			want:  "line1\nline2\nline3",
		},

		// Step 1b: bare CR (classic-Mac line endings) → LF
		{
			name:  "bare CR to LF",
			input: "line1\rline2\rline3",
			want:  "line1\nline2\nline3",
		},

		// Mixed CRLF and bare CR on the same input.
		{
			name:  "mixed CRLF and bare CR",
			input: "line1\r\nline2\rline3",
			want:  "line1\nline2\nline3",
		},

		// Step 2: reMultiSpace collapses runs of 2+ spaces.
		{
			name:  "two spaces collapsed to one",
			input: "hello  world",
			want:  "hello world",
		},
		{
			name:  "many spaces collapsed to one",
			input: "a          b",
			want:  "a b",
		},
		{
			name:  "multiple collapsed regions in one line",
			input: "foo  bar  baz  qux",
			want:  "foo bar baz qux",
		},
		{
			name:  "collapsed across lines",
			input: "line1    line1\nline2  line2",
			want:  "line1 line1\nline2 line2",
		},

		// Step 2 negative: tabs are intentionally NOT collapsed. Markdown
		// indentation and code-block fences rely on tab semantics; collapsing
		// them would silently rewrite user content.
		{
			name:  "single tab preserved",
			input: "hello\tworld",
			want:  "hello\tworld",
		},
		{
			name:  "two tabs preserved (not collapsed)",
			input: "hello\t\tworld",
			want:  "hello\t\tworld",
		},
		{
			name:  "tabs and multi-spaces mixed",
			input: "col1\tcol2  col3",
			want:  "col1\tcol2 col3",
		},

		// Step 3: strip trailing whitespace from each line.
		{
			name:  "trailing spaces stripped",
			input: "hello   ",
			want:  "hello",
		},
		{
			name:  "trailing tabs stripped",
			input: "hello\t\t",
			want:  "hello",
		},
		{
			name:  "trailing mix of space and tab stripped",
			input: "hello \t  \t",
			want:  "hello",
		},
		{
			name:  "trailing whitespace stripped from each line independently",
			input: "alpha    \nbeta\t\t\ngamma \t ",
			want:  "alpha\nbeta\ngamma",
		},

		// Step 4: strip trailing newline at end of file.
		{
			name:  "single trailing newline stripped",
			input: "hello\n",
			want:  "hello",
		},
		{
			name:  "multiple trailing newlines stripped",
			input: "hello\n\n\n",
			want:  "hello",
		},
		{
			name:  "interior newlines preserved while trailing stripped",
			input: "line1\nline2\nline3\n",
			want:  "line1\nline2\nline3",
		},

		// Step 5 in the source comment says "keep leading whitespace", but
		// reMultiSpace runs before the per-line trim and matches leading
		// 2+ spaces. As implemented:
		//   - a single leading space is preserved
		//   - 2+ leading spaces are collapsed to a single space
		//   - leading tabs are preserved (reMultiSpace ignores tabs)
		// These tests document actual behavior, not the spec comment.
		{
			name:  "leading single space preserved",
			input: " hello world",
			want:  " hello world",
		},
		{
			name:  "leading two spaces collapsed to one",
			input: "  hello world",
			want:  " hello world",
		},
		{
			name:  "leading four spaces collapsed to one",
			input: "    hello world",
			want:  " hello world",
		},
		{
			name:  "leading tabs preserved (markdown code block indent)",
			input: "	code line 1\n	code line 2",
			want:  "	code line 1\n	code line 2",
		},
		{
			name:  "leading single space preserved on each line",
			input: " - item one\n - item two",
			want:  " - item one\n - item two",
		},
		{
			name:  "leading two spaces on each line collapsed to one",
			input: "  - item one\n  - item two",
			want:  " - item one\n - item two",
		},

		// Combined pipeline: every step fires at once.
		{
			name:  "combined CRLF plus multi-space plus trailing whitespace plus trailing newline",
			input: "  alpha    beta  \r\n	gamma 	 \r\n",
			want:  " alpha beta\n	gamma",
		},
		{
			name:  "combined bare CR plus multi-space plus trailing whitespace",
			input: "header\rrow1   col1  col2   \rrow2   col1  col2   \r",
			want:  "header\nrow1 col1 col2\nrow2 col1 col2",
		},

		// Edge: only a newline → empty after trim.
		{
			name:  "only newline",
			input: "\n",
			want:  "",
		},
		// Edge: only spaces → empty after collapse+trim.
		{
			name:  "only spaces",
			input: "    ",
			want:  "",
		},
		// Edge: only spaces plus trailing newline → empty.
		{
			name:  "only spaces and trailing newline",
			input: "    \n",
			want:  "",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := prompt.Normalize(tc.input)
			if got != tc.want {
				t.Errorf("Normalize(%q)\n  got:  %q\n  want: %q", tc.input, got, tc.want)
			}
		})
	}
}

// TestNormalize_Idempotent verifies that Normalize is a fixed point:
// applying it twice yields the same result as applying it once. Any
// pipeline that mutates its own output would break content-addressed
// hashing (Hash would not be deterministic on already-normalized input).
func TestNormalize_Idempotent(t *testing.T) {
	t.Parallel()

	inputs := []string{
		"",
		"hello world",
		"  hello  world  \r\n",
		"line1\nline2\n",
		"\tindented\n\tindented  \n",
		"mixed \t CRLF\r\n  plus  trailing  \r\n",
	}

	for _, in := range inputs {
		in := in
		t.Run("idempotent_"+sanitizeName(in), func(t *testing.T) {
			t.Parallel()

			once := prompt.Normalize(in)
			twice := prompt.Normalize(once)
			if once != twice {
				t.Errorf("Normalize not idempotent on %q: once=%q twice=%q", in, once, twice)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestHash
// ---------------------------------------------------------------------------

// Golden SHA-256 values precomputed against Normalize. If Normalize changes
// (different reMultiSpace, different trim set, etc.) these values will break
// the test — which is the desired behavior, because hash stability is part
// of the contract (spec §9.1).
const (
	hashEmpty       = "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	hashHelloWorld  = "sha256:b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
	hashHelloTabWld = "sha256:3c6a8408beade7815485a4af8c55c1adbf249dfe34087c7d3d887a9dc3b43d1b"
	hashLeadingSpc  = "sha256:9ff3ac98fbfb78fa9630c93d925e5eaac41c3d39b0a8197baea4e8514d85cdfc"
	hashThreeLines  = "sha256:6bb6a5ad9b9c43a7cb535e636578716b64ac42edea814a4cad102ba404946837"
)

func TestHash(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty string has well-known SHA-256",
			input: "",
			want:  hashEmpty,
		},
		{
			name:  "simple input has golden hash",
			input: "hello world",
			want:  hashHelloWorld,
		},
		{
			name:  "tab preserved (tab is content, not whitespace to collapse)",
			input: "hello\tworld",
			want:  hashHelloTabWld,
		},
		{
			name:  "leading space preserved (markdown list)",
			input: "  hello world",
			want:  hashLeadingSpc,
		},
		{
			name:  "multi-line without trailing newline",
			input: "line1\nline2\nline3",
			want:  hashThreeLines,
		},
		// Normalize must run before hashing: any variant of the same
		// normalized form must collide. If this fails, the tamper-detection
		// contract in spec §11.3 is broken.
		{
			name:  "trailing newline is normalized away",
			input: "hello world\n",
			want:  hashHelloWorld,
		},
		{
			name:  "CRLF is normalized away",
			input: "hello world\r\n",
			want:  hashHelloWorld,
		},
		{
			name:  "bare CR is normalized away",
			input: "hello world\r",
			want:  hashHelloWorld,
		},
		{
			name:  "multi-space is normalized away",
			input: "hello          world",
			want:  hashHelloWorld,
		},
		{
			name:  "trailing whitespace is normalized away",
			input: "hello world   \t \t",
			want:  hashHelloWorld,
		},
		{
			name:  "all normalizations combined",
			input: "hello          world  	\r\n",
			want:  hashHelloWorld,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := prompt.Hash(tc.input)
			if got != tc.want {
				t.Errorf("Hash(%q)\n  got:  %s\n  want: %s", tc.input, got, tc.want)
			}
		})
	}
}

func TestHash_PrefixIsAlwaysSHA256(t *testing.T) {
	t.Parallel()

	inputs := []string{
		"",
		"hello",
		"hello world\n",
		"  leading\n  multi  line  \r\n",
		"a",
		"\t\ttabs and spaces   ",
	}

	for _, in := range inputs {
		in := in
		t.Run("prefix_"+sanitizeName(in), func(t *testing.T) {
			t.Parallel()

			got := prompt.Hash(in)
			const prefix = "sha256:"
			if !strings.HasPrefix(got, prefix) {
				t.Errorf("Hash(%q) = %q, missing %q prefix", in, got, prefix)
			}
			// 7 (prefix) + 64 (hex SHA-256) = 71 chars exactly.
			if len(got) != len(prefix)+64 {
				t.Errorf("Hash(%q) = %q, length = %d, want %d", in, got, len(got), len(prefix)+64)
			}
			// Every character after the prefix must be a lowercase hex digit.
			for i, r := range got[len(prefix):] {
				if !isLowerHex(r) {
					t.Errorf("Hash(%q) = %q, non-hex char %q at offset %d", in, got, r, i+len(prefix))
				}
			}
		})
	}
}

func TestHash_Deterministic(t *testing.T) {
	t.Parallel()

	// Same input → same hash, every call. Required for content-addressed
	// lookups and tamper detection.
	const input = "the quick brown fox jumps over the lazy dog\n"
	first := prompt.Hash(input)
	for i := 0; i < 10; i++ {
		got := prompt.Hash(input)
		if got != first {
			t.Fatalf("Hash nondeterministic on call %d: first=%s got=%s", i, first, got)
		}
	}
}

// ---------------------------------------------------------------------------
// TestVerifyHash
// ---------------------------------------------------------------------------

func TestVerifyHash(t *testing.T) {
	t.Parallel()

	const prompt1 = "hello world"

	tests := []struct {
		name         string
		input        string
		expectedHash string
		want         bool
	}{
		{
			name:         "matching hash returns true",
			input:        prompt1,
			expectedHash: hashHelloWorld,
			want:         true,
		},
		{
			name:         "non-matching hash returns false",
			input:        prompt1,
			expectedHash: "sha256:" + strings.Repeat("0", 64),
			want:         false,
		},
		{
			name:         "empty prompt with correct empty hash",
			input:        "",
			expectedHash: hashEmpty,
			want:         true,
		},
		{
			name:         "empty prompt with non-empty hash",
			input:        "",
			expectedHash: hashHelloWorld,
			want:         false,
		},
		{
			name:         "hash missing prefix returns false",
			input:        prompt1,
			expectedHash: strings.TrimPrefix(hashHelloWorld, "sha256:"),
			want:         false,
		},
		{
			name:         "completely garbage expected hash returns false",
			input:        prompt1,
			expectedHash: "not-a-hash",
			want:         false,
		},
		{
			name:         "Verify sees through normalization (trailing newline)",
			input:        "hello world\n",
			expectedHash: hashHelloWorld,
			want:         true,
		},
		{
			name:         "Verify sees through normalization (CRLF)",
			input:        "hello world\r\n",
			expectedHash: hashHelloWorld,
			want:         true,
		},
		{
			name:         "Verify sees through normalization (multi-space)",
			input:        "hello  world",
			expectedHash: hashHelloWorld,
			want:         true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := prompt.VerifyHash(tc.input, tc.expectedHash)
			if got != tc.want {
				t.Errorf("VerifyHash(%q, %q) = %v, want %v", tc.input, tc.expectedHash, got, tc.want)
			}
		})
	}
}

// TestVerifyHash_DetectsTampering proves the security-relevant contract:
// flipping a single character in the prompt must invalidate the hash.
// This is the core of the tamper-detection feature (spec §11.3).
func TestVerifyHash_DetectsTampering(t *testing.T) {
	t.Parallel()

	const original = "release the kraken"
	originalHash := prompt.Hash(original)
	if !prompt.VerifyHash(original, originalHash) {
		t.Fatalf("baseline VerifyHash returned false for matching hash %q", originalHash)
	}

	// Flip one character at every position. None of these tampered inputs
	// may verify against the original hash.
	for i := 0; i < len(original); i++ {
		i := i
		t.Run("tamper_pos_"+itoa(i), func(t *testing.T) {
			t.Parallel()

			tampered := mutateByte(original, i)
			if tampered == original {
				t.Fatalf("mutateByte returned identical string at position %d", i)
			}
			if prompt.VerifyHash(tampered, originalHash) {
				t.Errorf("VerifyHash accepted tampered input %q at position %d", tampered, i)
			}
		})
	}
}

// TestVerifyHash_DetectsPrependAndAppend guards against the simplest
// attacks on a content-addressed system: adding content to either end
// must change the hash, and VerifyHash must reject the modified input.
func TestVerifyHash_DetectsPrependAndAppend(t *testing.T) {
	t.Parallel()

	const original = "core payload"
	originalHash := prompt.Hash(original)

	prepended := "evil prefix\n" + original
	appended := original + "\nevil suffix"
	withWhitespace := "  " + original + "  \n"

	for _, mutated := range []string{prepended, appended, withWhitespace} {
		mutated := mutated
		t.Run("tamper_"+sanitizeName(mutated), func(t *testing.T) {
			t.Parallel()

			// After normalization, "  core payload  \n" → "  core payload",
			// which differs from "core payload", so its hash is different.
			if prompt.VerifyHash(mutated, originalHash) {
				t.Errorf("VerifyHash accepted mutated input %q", mutated)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestReMultiSpace — direct coverage of the package-level regexp.
// ---------------------------------------------------------------------------

// TestReMultiSpace is a focused test on the package-level reMultiSpace
// regexp itself, which is exercised indirectly by Normalize. This test
// documents and protects its precise semantics:
//
//   - Matches 2 or more consecutive SPACE characters (U+0020).
//   - Does NOT match tabs (\t), newlines (\n), or other whitespace.
//   - Does NOT match a single space.
//
// hasher.go is a one-file package and the regexp is package-private, so
// these tests exercise it through Normalize, which is its only production
// caller. The assertions verify the regexp's effect on the pipeline.
func TestReMultiSpace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "single space unchanged",
			input: "a b",
			want:  "a b",
		},
		{
			name:  "two spaces collapsed",
			input: "a  b",
			want:  "a b",
		},
		{
			name:  "many spaces collapsed",
			input: "a          b",
			want:  "a b",
		},
		{
			name:  "single tab NOT collapsed",
			input: "a\tb",
			want:  "a\tb",
		},
		{
			name:  "two tabs NOT collapsed (tabs are not spaces)",
			input: "a\t\tb",
			want:  "a\t\tb",
		},
		{
			name:  "tab and adjacent space run collapse only the spaces",
			input: "a \t  \tb",
			want:  "a \t \tb",
		},
		{
			name:  "newline-adjacent spaces collapse, then step 3 strips the lone survivor",
			input: "a  \nb",
			want:  "a\nb",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := prompt.Normalize(tc.input)
			if got != tc.want {
				t.Errorf("Normalize(%q) via reMultiSpace\n  got:  %q\n  want: %q", tc.input, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// isLowerHex reports whether r is one of 0-9, a-f. Used to assert that
// Hash output is well-formed lowercase hex.
func isLowerHex(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')
}

// sanitizeName replaces characters that would break `go test -run` or
// shell metacharacters with safe underscores, so test names stay printable
// and grep-friendly.
func sanitizeName(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "empty"
	}
	return b.String()
}

// itoa is a tiny stdlib-free int→ascii helper to keep the helper file
// dependency-free in the test file.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

// mutateByte returns s with one byte changed at position idx. The new byte
// is chosen to differ from the original (ASCII letters, digits, and a
// fallback printable).
func mutateByte(s string, idx int) string {
	if idx < 0 || idx >= len(s) {
		return s
	}
	original := s[idx]
	var replacement byte
	if original != 'X' {
		replacement = 'X'
	} else {
		replacement = 'Y'
	}
	return s[:idx] + string(replacement) + s[idx+1:]
}
