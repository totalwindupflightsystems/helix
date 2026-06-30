package prompt_test

import (
	"strings"
	"testing"

	"github.com/totalwindupflightsystems/helix/pkg/prompt"
)

// ---------------------------------------------------------------------------
// TestNormalizeForHash — spec §8.2-§8.3 normalization pipeline
// ---------------------------------------------------------------------------

func TestNormalizeForHash_LineEndingNormalization(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "CRLF to LF",
			input: "line1\r\nline2\r\nline3",
			want:  "line1\nline2\nline3\n",
		},
		{
			name:  "bare CR to LF",
			input: "line1\rline2\rline3",
			want:  "line1\nline2\nline3\n",
		},
		{
			name:  "mixed CRLF and bare CR",
			input: "line1\r\nline2\rline3",
			want:  "line1\nline2\nline3\n",
		},
		{
			name:  "single line CRLF",
			input: "hello\r\n",
			want:  "hello\n",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := prompt.NormalizeForHash(tc.input)
			if got != tc.want {
				t.Errorf("NormalizeForHash(%q)\n  got:  %q\n  want: %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestNormalizeForHash_WhitespaceCollapse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "two spaces to one",
			input: "hello  world",
			want:  "hello world\n",
		},
		{
			name:  "many spaces to one",
			input: "a          b",
			want:  "a b\n",
		},
		{
			name:  "multiple collapsed regions",
			input: "foo  bar  baz  qux",
			want:  "foo bar baz qux\n",
		},
		{
			name:  "collapsed across lines",
			input: "line1    line1\nline2  line2",
			want:  "line1 line1\nline2 line2\n",
		},
		// Tabs are now also collapsed per spec §8.2 step 2.
		{
			name:  "single tab collapsed to space",
			input: "hello\tworld",
			want:  "hello world\n",
		},
		{
			name:  "two tabs collapsed to space",
			input: "hello\t\tworld",
			want:  "hello world\n",
		},
		{
			name:  "tabs and spaces mixed collapsed",
			input: "col1\tcol2  col3",
			want:  "col1 col2 col3\n",
		},
		{
			name:  "space-tab-space collapsed",
			input: "a \t b",
			want:  "a b\n",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := prompt.NormalizeForHash(tc.input)
			if got != tc.want {
				t.Errorf("NormalizeForHash(%q)\n  got:  %q\n  want: %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestNormalizeForHash_TrailingWhitespace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "trailing spaces stripped",
			input: "hello world   ",
			want:  "hello world\n",
		},
		{
			name:  "trailing tabs stripped",
			input: "hello world\t\t",
			want:  "hello world\n",
		},
		{
			name:  "trailing mixed whitespace stripped",
			input: "hello world   \t \t",
			want:  "hello world\n",
		},
		{
			name:  "trailing whitespace on multiple lines",
			input: "line1   \nline2\t\nline3",
			want:  "line1\nline2\nline3\n",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := prompt.NormalizeForHash(tc.input)
			if got != tc.want {
				t.Errorf("NormalizeForHash(%q)\n  got:  %q\n  want: %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestNormalizeForHash_TrailingNewline(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "no trailing newline gets one",
			input: "hello world",
			want:  "hello world\n",
		},
		{
			name:  "single trailing newline preserved",
			input: "hello world\n",
			want:  "hello world\n",
		},
		{
			name:  "multiple trailing newlines reduced to one",
			input: "hello world\n\n\n",
			want:  "hello world\n",
		},
		{
			name:  "multiline with trailing newlines reduced",
			input: "line1\nline2\n\n\n",
			want:  "line1\nline2\n",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := prompt.NormalizeForHash(tc.input)
			if got != tc.want {
				t.Errorf("NormalizeForHash(%q)\n  got:  %q\n  want: %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestNormalizeForHash_LeadingWhitespacePreserved(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "leading spaces preserved (markdown list)",
			input: "  hello world",
			want:  "  hello world\n",
		},
		{
			name:  "leading tabs preserved (code indent)",
			input: "\t\tcode here",
			want:  "\t\tcode here\n",
		},
		{
			name:  "leading spaces on multiple lines",
			input: "  line1\n    line2\n\tline3",
			want:  "  line1\n    line2\n\tline3\n",
		},
		{
			name:  "leading tabs + mid-line collapse combined",
			input: "\t\thello  world",
			want:  "\t\thello world\n",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := prompt.NormalizeForHash(tc.input)
			if got != tc.want {
				t.Errorf("NormalizeForHash(%q)\n  got:  %q\n  want: %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestNormalizeForHash_YAMLFrontmatter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "basic frontmatter stripped",
			input: "---\nversion: v1\n---\nhello world",
			want:  "hello world\n",
		},
		{
			name:  "frontmatter with CRLF stripped",
			input: "---\r\nversion: v1\r\n---\r\nhello world",
			want:  "hello world\n",
		},
		{
			name:  "no frontmatter — whole text is body",
			input: "hello world",
			want:  "hello world\n",
		},
		{
			name:  "frontmatter with multiple body lines",
			input: "---\ntitle: Test\n---\nline1\nline2",
			want:  "line1\nline2\n",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := prompt.NormalizeForHash(tc.input)
			if got != tc.want {
				t.Errorf("NormalizeForHash(%q)\n  got:  %q\n  want: %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestNormalizeForHash_FencedCodeBlockExemption(t *testing.T) {
	t.Parallel()

	// Inside fenced code blocks, whitespace is preserved exactly.
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name: "backtick fence preserves internal spaces",
			input: "```go\n" +
				"func   main()   {\n" +
				"}\n" +
				"```\n" +
				"outside  block",
			want: "```go\n" +
				"func   main()   {\n" +
				"}\n" +
				"```\n" +
				"outside block\n",
		},
		{
			name: "tilde fence preserves internal tabs",
			input: "~~~python\n" +
				"\tdef   foo():\n" +
				"\t\tpass\n" +
				"~~~\n" +
				"after",
			want: "~~~python\n" +
				"\tdef   foo():\n" +
				"\t\tpass\n" +
				"~~~\n" +
				"after\n",
		},
		{
			name: "unclosed fence treated as inside until EOF",
			input: "```\n" +
				"code  with  spaces",
			want: "```\n" +
				"code  with  spaces\n",
		},
		{
			name: "multiple fence blocks",
			input: "```go\n" +
				"a   b\n" +
				"```\n" +
				"text  collapsed\n" +
				"```yaml\n" +
				"key:  value\n" +
				"```",
			want: "```go\n" +
				"a   b\n" +
				"```\n" +
				"text collapsed\n" +
				"```yaml\n" +
				"key:  value\n" +
				"```\n",
		},
		{
			name: "fence with language specifier",
			input: "```bash\n" +
				"echo   done\n" +
				"```\n" +
				"done",
			want: "```bash\n" +
				"echo   done\n" +
				"```\n" +
				"done\n",
		},
		{
			name: "tilde fence with language",
			input: "~~~c\n" +
				"int   main() {}\n" +
				"~~~\n" +
				"end",
			want: "~~~c\n" +
				"int   main() {}\n" +
				"~~~\n" +
				"end\n",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := prompt.NormalizeForHash(tc.input)
			if got != tc.want {
				t.Errorf("NormalizeForHash(%q)\n  got:  %q\n  want: %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestNormalizeForHash_CombinedTransformations(t *testing.T) {
	t.Parallel()

	// A realistic prompt combining all normalization steps.
	input := "---\n" +
		"version: v1\n" +
		"---\r\n" +
		"# Title\r\n" +
		"\r\n" +
		"Some  text   with   spaces\r\n" +
		"\r\n" +
		"```go\r\n" +
		"func  main()   {}\r\n" +
		"```\r\n" +
		"\r\n" +
		"  - list item\r\n" +
		"  - another  item\r\n"

	want := "# Title\n" +
		"\n" +
		"Some text with spaces\n" +
		"\n" +
		"```go\n" +
		"func  main()   {}\n" +
		"```\n" +
		"\n" +
		"  - list item\n" +
		"  - another item\n"

	got := prompt.NormalizeForHash(input)
	if got != want {
		t.Errorf("Combined normalization failed\n  got:  %q\n  want: %q", got, want)
	}
}

func TestNormalizeForHash_EmptyAndEdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty string stays empty",
			input: "",
			want:  "",
		},
		{
			name:  "just whitespace stripped to empty",
			input: "   ",
			want:  "",
		},
		{
			name:  "just newlines",
			input: "\n\n\n",
			want:  "",
		},
		{
			name:  "just frontmatter no body",
			input: "---\nversion: v1\n---\n",
			want:  "",
		},
		{
			name:  "single character",
			input: "a",
			want:  "a\n",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := prompt.NormalizeForHash(tc.input)
			if got != tc.want {
				t.Errorf("NormalizeForHash(%q)\n  got:  %q\n  want: %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestNormalizeForHash_Idempotent(t *testing.T) {
	t.Parallel()

	inputs := []string{
		"",
		"hello world",
		"  hello  world  \r\n",
		"line1\nline2\n",
		"\tindented\n\tindented  \n",
		"mixed \t CRLF\r\n  plus  trailing  \r\n",
		"```\ncode  block\n```\n",
		"---\nv: 1\n---\nbody  text\n",
	}

	for _, in := range inputs {
		in := in
		t.Run("idempotent_"+sanitizeName(in), func(t *testing.T) {
			t.Parallel()

			once := prompt.NormalizeForHash(in)
			twice := prompt.NormalizeForHash(once)
			if once != twice {
				t.Errorf("NormalizeForHash not idempotent on %q: once=%q twice=%q", in, once, twice)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestNormalizeForHash_Deterministic — same input always produces same output
// ---------------------------------------------------------------------------

func TestNormalizeForHash_Deterministic(t *testing.T) {
	t.Parallel()

	input := "hello  world\n```\n   code\n```\n"
	first := prompt.NormalizeForHash(input)
	for i := 0; i < 10; i++ {
		got := prompt.NormalizeForHash(input)
		if got != first {
			t.Fatalf("NormalizeForHash nondeterministic on call %d: first=%q got=%q", i, first, got)
		}
	}
}

// ---------------------------------------------------------------------------
// TestNormalizeForHash_ContentEquivalent — same content, different
// whitespace/line-endings → same hash. This is the core contract of the
// normalization pipeline.
// ---------------------------------------------------------------------------

func TestNormalizeForHash_ContentEquivalent(t *testing.T) {
	t.Parallel()

	base := "hello world\n"
	variants := []struct {
		name  string
		input string
	}{
		{"LF", "hello world\n"},
		{"CRLF", "hello world\r\n"},
		{"bare CR", "hello world\r"},
		{"multi-space", "hello    world\n"},
		{"tab-space", "hello\t world\n"},
		{"trailing whitespace", "hello world   \t\n"},
		{"no trailing newline", "hello world"},
		{"leading whitespace + multi-space", "  hello  world\n"},
	}

	for _, v := range variants {
		v := v
		t.Run("equivalent_"+v.name, func(t *testing.T) {
			t.Parallel()
			got := prompt.NormalizeForHash(v.input)
			// All variants should normalize to base EXCEPT "leading whitespace"
			// which preserves the leading spaces and is thus NOT equivalent.
			if v.name == "leading whitespace + multi-space" {
				want := "  hello world\n"
				if got != want {
					t.Errorf("Expected %q, got %q", want, got)
				}
				return
			}
			if got != base {
				t.Errorf("Content-equivalent variant %q normalized to %q, want %q", v.name, got, base)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Hash integration — NormalizeForHash produces stable hashes
// ---------------------------------------------------------------------------

func TestNormalizeForHash_HashStability(t *testing.T) {
	t.Parallel()

	// Two textually different but content-equivalent inputs should produce
	// the same hash when normalized through NormalizeForHash.
	lfVersion := "hello  world\n"
	crlfVersion := "hello\t\tworld\r\n"

	normLF := prompt.NormalizeForHash(lfVersion)
	normCRLF := prompt.NormalizeForHash(crlfVersion)

	if normLF != normCRLF {
		t.Fatalf("Normalized forms differ: LF=%q CRLF=%q", normLF, normCRLF)
	}

	// Hash of the normalized form should match.
	if prompt.Hash(normLF) != prompt.Hash(normCRLF) {
		t.Fatal("Hash mismatch on content-equivalent normalized forms")
	}

	// The normalized hash must contain the sha256: prefix.
	if !strings.HasPrefix(prompt.Hash(normLF), "sha256:") {
		t.Fatal("Missing sha256: prefix")
	}
}

// ---------------------------------------------------------------------------
// Leading whitespace + fenced code block interaction
// ---------------------------------------------------------------------------

func TestNormalizeForHash_LeadingWhitespaceInsideFence(t *testing.T) {
	t.Parallel()

	// Leading whitespace inside a fenced block is code indentation — must
	// be preserved exactly, including tabs.
	input := "```\n" +
		"\tfunc main() {\n" +
		"\t\tfmt.Println(\"hello\")\n" +
		"\t}\n" +
		"```\n"

	want := "```\n" +
		"\tfunc main() {\n" +
		"\t\tfmt.Println(\"hello\")\n" +
		"\t}\n" +
		"```\n"

	got := prompt.NormalizeForHash(input)
	if got != want {
		t.Errorf("Fence-internal leading whitespace not preserved\n  got:  %q\n  want: %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// Fenced code blocks at different positions
// ---------------------------------------------------------------------------

func TestNormalizeForHash_FenceAtStart(t *testing.T) {
	t.Parallel()

	input := "```\n" +
		"code  here\n" +
		"```\n" +
		"text  after"

	want := "```\n" +
		"code  here\n" +
		"```\n" +
		"text after\n"

	got := prompt.NormalizeForHash(input)
	if got != want {
		t.Errorf("Fence at start\n  got:  %q\n  want: %q", got, want)
	}
}

func TestNormalizeForHash_FenceAtEnd(t *testing.T) {
	t.Parallel()

	input := "text  before\n" +
		"```\n" +
		"code  here\n" +
		"```"

	want := "text before\n" +
		"```\n" +
		"code  here\n" +
		"```\n"

	got := prompt.NormalizeForHash(input)
	if got != want {
		t.Errorf("Fence at end\n  got:  %q\n  want: %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// Collapse behavior at line boundaries
// ---------------------------------------------------------------------------

func TestNormalizeForHash_CollapseWithOnlySpaces(t *testing.T) {
	t.Parallel()

	// A line consisting entirely of spaces (outside a fence) should be
	// collapsed to a single space, then trailing-whitespace-stripped to empty.
	input := "before\n      \nafter"
	want := "before\n\nafter\n"

	got := prompt.NormalizeForHash(input)
	if got != want {
		t.Errorf("All-space line\n  got:  %q\n  want: %q", got, want)
	}
}

func TestNormalizeForHash_CollapseTabOnlyLine(t *testing.T) {
	t.Parallel()

	// A line consisting entirely of tabs (outside a fence) → collapse to
	// one space → trailing strip to empty.
	input := "before\n			after"
	want := "before\n			after\n"

	got := prompt.NormalizeForHash(input)
	if got != want {
		t.Errorf("Tab-only line collapse\n  got:  %q\n  want: %q", got, want)
	}
}
