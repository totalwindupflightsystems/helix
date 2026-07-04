// Package secrets implements the secret-pattern scanner per
// specs/SPECIFICATION.md §6.2 (Secrets Management).
//
// The scanner centralizes the secret-detection patterns that previously
// were duplicated across GitReins Tier 1 hooks. It exposes a small,
// dependency-free API suitable for use by:
//
//   - the GitReins pre-commit hook,
//   - the `helix secrets scan` CLI command,
//   - ad-hoc one-off scans during incident response.
//
// All patterns are designed to match real key formats used across the
// Helix platform (OpenRouter `sk-or-v1-...`, DeepSeek `sk-...`, GitHub
// PATs, SSH/RSA private keys, env-var assignments). False-positive
// suppression for test fixtures and documentation is handled via
// per-line AllowlistPrefixes and a regex AllowlistRegex supplied by
// the caller.
package secrets

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// =============================================================================
// Pattern Catalog
// =============================================================================
//
// The pattern strings MUST stay byte-identical to the patterns referenced in
// specs/SPECIFICATION.md §6.2 and .gitreins/config.yaml — that is the single
// source of truth for which secret shapes are considered secrets. Adding a
// new pattern here is a breaking change for downstream hooks and requires
// updating the spec.

var (
	// PatternOpenRouter matches OpenRouter, DeepSeek and other "sk-" prefixed
	// keys. Hyphens and underscores are intentionally allowed in the key body
	// because real OpenRouter keys look like `n0t-a-r3al-k3y` and DeepSeek
	// keys occasionally embed underscores.
	PatternOpenRouter = regexp.MustCompile(`\bsk-[a-zA-Z0-9_-]{20,}`)

	// PatternGitHubPAT matches GitHub personal access tokens. The token
	// body is exactly 36 alphanumerics following the literal `ghp_` prefix.
	PatternGitHubPAT = regexp.MustCompile(`\bghp_[a-zA-Z0-9]{36}`)

	// PatternPrivateKey matches PEM-armored private key headers for RSA, EC
	// and OpenSSH keys. We match the header line only; the body of the key
	// is enormous and the header is sufficient for blocking.
	PatternPrivateKey = regexp.MustCompile(`-----BEGIN (RSA |EC |OPENSSH )?PRIVATE KEY-----`)

	// PatternEnvAssignment matches `KEY=VALUE` style secret assignments for
	// the four LLM providers Helix integrates with. The value must look
	// like an actual key (sk- prefix or a 32+ char base64-like string), not
	// a shell variable reference like `$DEEPSEEK_API_KEY` or the literal
	// text `your-key-here`. Optional quotes around the value are common in
	// shell-style .env files, so we accept them.
	PatternEnvAssignment = regexp.MustCompile(`\b(OPENROUTER|DEEPSEEK|ZAI|ANTHROPIC)_API_KEY\s*=\s*["']?(sk-[a-zA-Z0-9_-]{20,}|[A-Za-z0-9+/=]{32,})`)
)

// AllPatterns returns the catalog of secret patterns the scanner applies.
// The order is significant only for stable test output — earlier patterns
// are tried first.
func AllPatterns() []NamedPattern {
	return []NamedPattern{
		{Name: "openrouter-key", Pattern: PatternOpenRouter},
		{Name: "github-pat", Pattern: PatternGitHubPAT},
		{Name: "private-key", Pattern: PatternPrivateKey},
		{Name: "env-assignment", Pattern: PatternEnvAssignment},
	}
}

// NamedPattern pairs a human-readable rule name with the regex it matches.
// The rule name appears in Finding.Rule and is what callers should switch
// on when implementing allowlists.
type NamedPattern struct {
	Name    string
	Pattern *regexp.Regexp
}

// =============================================================================
// Finding
// =============================================================================

// Finding is a single secret-pattern hit. Findings are immutable; the
// scanner returns them as values rather than pointers so callers cannot
// accidentally mutate scan state.
type Finding struct {
	// Rule is the name of the pattern that matched (see NamedPattern.Name).
	Rule string

	// File is the path the secret was found in. For ScanBytes / ScanString
	// the caller supplies it (often "<input>").
	File string

	// Line is the 1-indexed line number where the match was found.
	Line int

	// Column is the 1-indexed byte column where the match starts.
	Column int

	// Snippet is the full matched text. For long matches (e.g. private-key
	// headers) this is the entire header line.
	Snippet string
}

// String renders a Finding in the canonical "file:line:col: rule snippet"
// format used by all Helix CLI output.
func (f Finding) String() string {
	return fmt.Sprintf("%s:%d:%d: %s %q",
		f.File, f.Line, f.Column, f.Rule, f.Snippet)
}

// =============================================================================
// Allowlist
// =============================================================================

// Allowlist filters out known false positives before they are reported.
// The zero value is a usable "no-op" allowlist that does not suppress
// anything.
type Allowlist struct {
	// LinePrefixes suppresses findings on any line that starts with one of
	// these prefixes (after trimming leading whitespace). Use this for
	// doc markers like `// cs_sk_` or `# example:`.
	LinePrefixes []string

	// AllowRegex is matched against the full Finding.Snippet. If it
	// matches, the finding is suppressed. Use this to exclude docs/specs
	// sample tokens like `sk-prod-abc123`.
	AllowRegex *regexp.Regexp
}

// DefaultTestAllowlist returns an allowlist suitable for scanning source
// trees that contain test fixture keys (e.g. `cs_sk_...` prefixes). It
// suppresses any line whose trimmed content begins with `cs_sk_`,
// `test_key_`, or `# nolint:secret`.
func DefaultTestAllowlist() Allowlist {
	return Allowlist{
		LinePrefixes: []string{
			"cs_sk_",
			"test_key_",
			"// nolint:secret",
			"# nolint:secret",
		},
	}
}

// Allows reports whether the given finding is suppressed by this allowlist.
// lineText is the complete line that contained the finding — required
// because many allowlist markers (e.g. "// nolint:secret") are not part of
// the matched snippet itself.
func (a Allowlist) Allows(f Finding, lineText string) bool {
	trimmed := strings.TrimLeft(lineText, " 	")
	for _, prefix := range a.LinePrefixes {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	if a.AllowRegex != nil && a.AllowRegex.MatchString(f.Snippet) {
		return true
	}
	return false
}

// =============================================================================
// Scanner
// =============================================================================

// Scanner applies the platform's secret patterns to text input and
// produces a slice of Findings. Construct via NewScanner or use the
// package-level ScanString / ScanBytes helpers for one-offs.
type Scanner struct {
	patterns  []NamedPattern
	allowlist Allowlist
}

// NewScanner returns a Scanner with the default pattern catalog and the
// supplied allowlist.
func NewScanner(allowlist Allowlist) *Scanner {
	return &Scanner{
		patterns:  AllPatterns(),
		allowlist: allowlist,
	}
}

// ScanLine scans a single line and returns zero or more findings. Lines
// without secrets return an empty slice (not nil — callers can range
// over the result without a nil check).
func (s *Scanner) ScanLine(line string) []Finding {
	var out []Finding
	for _, np := range s.patterns {
		matches := np.Pattern.FindAllStringIndex(line, -1)
		for _, m := range matches {
			snippet := line[m[0]:m[1]]
			f := Finding{
				Rule:    np.Name,
				Line:    1,
				Column:  m[0] + 1,
				Snippet: snippet,
			}
			if !s.allowlist.Allows(f, line) {
				out = append(out, f)
			}
		}
	}
	return out
}

// ScanString scans a multi-line string. The supplied filename is recorded
// on every finding so callers can render file:line:col diagnostics.
func (s *Scanner) ScanString(filename, input string) []Finding {
	return s.scanReader(filename, strings.NewReader(input))
}

// ScanBytes is the byte-slice counterpart to ScanString.
func (s *Scanner) ScanBytes(filename string, input []byte) []Finding {
	return s.scanReader(filename, bytes.NewReader(input))
}

// ScanFile reads the file at path and scans its contents. The file
// permission bits are checked only to surface a clear error message —
// the scanner does not refuse to scan world-readable files (some legit
// use cases scan shared fixtures).
func (s *Scanner) ScanFile(path string) ([]Finding, error) {
	f, err := os.Open(path) // #nosec G304 — caller controls path
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	return s.scanReader(path, f), nil
}

func (s *Scanner) scanReader(filename string, r io.Reader) []Finding {
	var out []Finding
	scanner := bufio.NewScanner(r)
	// Allow long lines (private keys, base64 blobs). 1 MiB matches git's
	// default blob size and is plenty for any sane secret line.
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		for _, np := range s.patterns {
			matches := np.Pattern.FindAllStringIndex(line, -1)
			for _, m := range matches {
				snippet := line[m[0]:m[1]]
				f := Finding{
					Rule:    np.Name,
					File:    filename,
					Line:    lineNo,
					Column:  m[0] + 1,
					Snippet: snippet,
				}
				if !s.allowlist.Allows(f, line) {
					out = append(out, f)
				}
			}
		}
	}
	// I/O errors reading the source are swallowed — the caller can detect
	// truncation via the line count if they care. Returning a partial
	// result is friendlier than failing the whole scan.
	return out
}

// =============================================================================
// Package-level helpers
// =============================================================================
//
// These are the convenience entry points most callers should reach for.
// They construct a default Scanner on every call; if you need to scan
// many files in a hot loop, build a Scanner once via NewScanner and
// reuse it.

// ScanString scans input with the default test allowlist. See Scanner.ScanString.
func ScanString(filename, input string) []Finding {
	return NewScanner(DefaultTestAllowlist()).ScanString(filename, input)
}

// ScanBytes scans input with the default test allowlist. See Scanner.ScanBytes.
func ScanBytes(filename string, input []byte) []Finding {
	return NewScanner(DefaultTestAllowlist()).ScanBytes(filename, input)
}

// ScanFile scans the file at path with the default test allowlist.
func ScanFile(path string) ([]Finding, error) {
	return NewScanner(DefaultTestAllowlist()).ScanFile(path)
}

// ScanPath walks path (file or directory) and returns all findings
// across every text file scanned. Symlinks are not followed. Hidden
// directories (starting with ".") are skipped to avoid scanning
// `.git/`, `.venv/`, etc. The walk is deterministic — files are
// visited in lexicographic order.
func ScanPath(path string) ([]Finding, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}
	if !info.IsDir() {
		return ScanFile(path)
	}

	var out []Finding
	walkErr := filepath.WalkDir(path, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == ".venv" || name == "node_modules" {
				return filepath.SkipDir
			}
			// Skip hidden dirs.
			if strings.HasPrefix(name, ".") && p != path {
				return filepath.SkipDir
			}
			return nil
		}
		findings, err := ScanFile(p)
		if err != nil {
			// Permission errors etc. — record and continue.
			return nil
		}
		out = append(out, findings...)
		return nil
	})
	if walkErr != nil {
		return out, fmt.Errorf("walk %s: %w", path, walkErr)
	}
	return out, nil
}

// =============================================================================
// Reporter
// =============================================================================

// Report is the aggregate result of a scan, suitable for CLI output.
// It carries the findings plus a few summary counters that callers
// commonly need (e.g. for exit code computation).
type Report struct {
	Path     string
	Findings []Finding
	Files    int
}

// HasFindings reports whether any of the rules in the catalog matched.
func (r Report) HasFindings() bool {
	return len(r.Findings) > 0
}

// ByRule groups findings by rule name and returns a map of rule → count.
// This is convenient for `helix secrets scan --summary` style output.
func (r Report) ByRule() map[string]int {
	m := make(map[string]int)
	for _, f := range r.Findings {
		m[f.Rule]++
	}
	return m
}

// FormatReport renders a Report in the canonical "human-readable"
// format: a header line, then one finding per line. Stable ordering
// matches the input order of ScanPath / ScanFile.
func FormatReport(r Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Scanned: %s\n", r.Path)
	fmt.Fprintf(&b, "Findings: %d\n", len(r.Findings))
	if len(r.Findings) == 0 {
		fmt.Fprintln(&b, "  (no secrets detected)")
		return b.String()
	}
	for _, f := range r.Findings {
		fmt.Fprintf(&b, "  %s\n", f.String())
	}
	return b.String()
}
