package secrets

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// =============================================================================
// Helpers
// =============================================================================

// requireFinding fails the test unless there is at least one finding matching
// the supplied rule. Returns the FIRST matching finding so callers can inspect
// Snippet etc. Multiple findings from different rules (e.g. openrouter-key AND
// env-assignment on the same line) are expected and acceptable.
func requireFinding(t *testing.T, got []Finding, rule string) Finding {
	t.Helper()
	for _, f := range got {
		if f.Rule == rule {
			return f
		}
	}
	t.Fatalf("expected finding for rule %q, got %+v", rule, got)
	return Finding{}
}

// requireNoFindings fails the test if any finding is returned.
func requireNoFindings(t *testing.T, got []Finding) {
	t.Helper()
	if len(got) != 0 {
		t.Fatalf("expected no findings, got %d: %+v", len(got), got)
	}
}

// =============================================================================
// Pattern Catalog
// =============================================================================

func TestAllPatterns_ReturnsExpectedCatalog(t *testing.T) {
	catalog := AllPatterns()
	wantNames := []string{"openrouter-key", "github-pat", "private-key", "env-assignment"}
	if len(catalog) != len(wantNames) {
		t.Fatalf("expected %d patterns, got %d", len(wantNames), len(catalog))
	}
	for i, want := range wantNames {
		if catalog[i].Name != want {
			t.Errorf("catalog[%d].Name = %q, want %q", i, catalog[i].Name, want)
		}
		if catalog[i].Pattern == nil {
			t.Errorf("catalog[%d].Pattern is nil", i)
		}
	}
}

// =============================================================================
// OpenRouter / DeepSeek key pattern
// =============================================================================

func TestScanString_OpenRouterKey(t *testing.T) {
	// Real-world OpenRouter key shape: "sk-or-v1-" prefix + base62 body.
	const line = `OPENROUTER_API_KEY=sk-or-v1-abcdef0123456789abcdef0123456789`
	got := ScanString("test.env", line)
	f := requireFinding(t, got, "openrouter-key")
	if f.Line != 1 {
		t.Errorf("Line = %d, want 1", f.Line)
	}
	if f.Column < 1 {
		t.Errorf("Column = %d, want >= 1", f.Column)
	}
	if !strings.HasPrefix(f.Snippet, "sk-") {
		t.Errorf("Snippet = %q, want sk- prefix", f.Snippet)
	}
}

func TestScanString_DeepSeekKey(t *testing.T) {
	// DeepSeek keys are also sk- prefixed.
	const line = `key="sk-65e41234abcdef0123456789"`
	got := ScanString("test.go", line)
	requireFinding(t, got, "openrouter-key")
}

func TestScanString_KeyWithUnderscores(t *testing.T) {
	// Underscores ARE allowed in key bodies (DeepSeek occasionally embeds them).
	const line = `token = "sk-abc_def_0123456789abcdef0123"`
	got := ScanString("test.go", line)
	requireFinding(t, got, "openrouter-key")
}

func TestScanString_ShortKeyNotMatched(t *testing.T) {
	// Keys shorter than 20 chars after "sk-" must NOT match — protects against
	// false positives on short identifiers like "sk-test".
	const line = `value = "sk-short"`
	got := ScanString("test.go", line)
	requireNoFindings(t, got)
}

func TestScanString_KeyWithoutWordBoundary(t *testing.T) {
	// "xsk-..." must not match — the pattern uses \b to require a word
	// boundary. Otherwise test fixtures like `cs_sk_...` would trigger
	// false positives (note: cs_sk_ prefix IS handled by the test allowlist).
	const line = `name = "xsk-0123456789abcdef012345"`
	got := ScanString("test.go", line)
	// This SHOULD match — the `\b` only matches at word boundaries, but
	// `x` followed by `sk-` is not a word boundary at `s`. So xsk-... is
	// NOT a real sk- key. Verify no findings.
	requireNoFindings(t, got)
}

// =============================================================================
// GitHub PAT
// =============================================================================

func TestScanString_GitHubPAT(t *testing.T) {
	const line = `token: "ghp_abcdef0123456789abcdef0123456789abcd"`
	got := ScanString("config.yml", line)
	f := requireFinding(t, got, "github-pat")
	if f.Snippet != "ghp_abcdef0123456789abcdef0123456789abcd" {
		t.Errorf("Snippet = %q, want full PAT", f.Snippet)
	}
}

func TestScanString_GitHubPAT_WrongLength(t *testing.T) {
	// 35-char body must NOT match.
	const line = `token: "ghp_abcdef0123456789abcdef0123456789abc"`
	got := ScanString("config.yml", line)
	requireNoFindings(t, got)
}

func TestScanString_GitHubFineGrainedPat_NotMatched(t *testing.T) {
	// Fine-grained PATs use a different prefix (github_pat_). Out of scope.
	const line = `token: "github_pat_11ABCDEFG0abcdefghijklmnopqrstuvwxyzABCDEFGH"`
	got := ScanString("config.yml", line)
	requireNoFindings(t, got)
}

// =============================================================================
// Private key
// =============================================================================

func TestScanString_RSAKeyHeader(t *testing.T) {
	const line = `-----BEGIN RSA PRIVATE KEY-----`
	got := ScanString("key.pem", line)
	requireFinding(t, got, "private-key")
}

func TestScanString_ECKeyHeader(t *testing.T) {
	const line = `-----BEGIN EC PRIVATE KEY-----`
	got := ScanString("key.pem", line)
	requireFinding(t, got, "private-key")
}

func TestScanString_OpenSSHKeyHeader(t *testing.T) {
	const line = `-----BEGIN OPENSSH PRIVATE KEY-----`
	got := ScanString("key", line)
	requireFinding(t, got, "private-key")
}

func TestScanString_BarePrivateKeyHeader(t *testing.T) {
	// No algorithm prefix is also legal in some legacy keys.
	const line = `-----BEGIN PRIVATE KEY-----`
	got := ScanString("key.pem", line)
	requireFinding(t, got, "private-key")
}

// =============================================================================
// Env-var assignment
// =============================================================================

func TestScanString_OpenRouterEnvAssignment(t *testing.T) {
	const line = `OPENROUTER_API_KEY=sk-or-v1-abcdef0123456789abcdef0123456789`
	got := ScanString(".env", line)
	f := requireFinding(t, got, "env-assignment")
	// The env-assignment snippet includes the full assignment.
	if f.Snippet != "OPENROUTER_API_KEY=sk-or-v1-abcdef0123456789abcdef0123456789" {
		t.Errorf("Snippet = %q, want full assignment", f.Snippet)
	}
}

func TestScanString_DeepSeekEnvAssignment(t *testing.T) {
	// Real DeepSeek keys are 32+ chars after "sk-" prefix. Use a value
	// that satisfies both patterns (env-assignment + openrouter-key).
	const line = `DEEPSEEK_API_KEY="sk-65e41234abcdef0123456789abcdef0123"`
	got := ScanString(".env", line)
	// Both openrouter-key and env-assignment fire. We require the
	// env-assignment to be present.
	requireFinding(t, got, "env-assignment")
}

func TestScanString_ZAIEnvAssignment(t *testing.T) {
	const line = `ZAI_API_KEY=abcdefghijklmnopqrstuvwxyz0123456789ABCD`
	got := ScanString(".env", line)
	// 36-char body matches base64 shape.
	requireFinding(t, got, "env-assignment")
}

func TestScanString_AnthropicEnvAssignment(t *testing.T) {
	const line = `ANTHROPIC_API_KEY=sk-ant-api03-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa`
	got := ScanString(".env", line)
	requireFinding(t, got, "env-assignment")
}

func TestScanString_UnknownProviderEnvVar_NotMatched(t *testing.T) {
	// Only the four known providers trigger the env-assignment rule.
	const line = `RANDOM_API_KEY=abcdefghijklmnopqrstuvwxyz0123456789ABCD`
	got := ScanString(".env", line)
	requireNoFindings(t, got)
}

func TestScanString_ShellVarRef_NotMatched(t *testing.T) {
	// `$DEEPSEEK_API_KEY` reference (not a real value) must NOT trigger.
	const line = `export DEEPSEEK_API_KEY=$DEEPSEEK_API_KEY`
	got := ScanString("start.sh", line)
	requireNoFindings(t, got)
}

func TestScanString_PlaceholderValue_NotMatched(t *testing.T) {
	// Short or non-key-shaped values must NOT trigger.
	for _, line := range []string{
		`DEEPSEEK_API_KEY=changeme`,
		`DEEPSEEK_API_KEY=your-key-here`,
		`DEEPSEEK_API_KEY=`,
		`DEEPSEEK_API_KEY="`,
	} {
		got := ScanString(".env", line)
		if len(got) != 0 {
			t.Errorf("placeholder %q should not match, got %+v", line, got)
		}
	}
}

// =============================================================================
// Multi-line inputs
// =============================================================================

func TestScanString_MultiLine_FindingsAcrossLines(t *testing.T) {
	input := strings.Join([]string{
		`# config file`, // 1: no finding
		`OTHER=value`,   // 2: no finding
		`OPENROUTER_API_KEY=sk-abc123def456ghi789jkl012mno`, // 3: openrouter + env-assignment
		``,                                // 4: no finding
		`-----BEGIN RSA PRIVATE KEY-----`, // 5: private-key
	}, "\n")
	got := ScanString("config.env", input)
	// 3 expected: openrouter-key (line 3), env-assignment (line 3), private-key (line 5).
	if len(got) != 3 {
		t.Fatalf("expected 3 findings, got %d: %+v", len(got), got)
	}
	// Findings should report correct line numbers.
	var openrouterLines, envLines, privKeyLines []int
	for _, f := range got {
		switch f.Rule {
		case "openrouter-key":
			openrouterLines = append(openrouterLines, f.Line)
		case "env-assignment":
			envLines = append(envLines, f.Line)
		case "private-key":
			privKeyLines = append(privKeyLines, f.Line)
		}
	}
	if len(openrouterLines) != 1 || openrouterLines[0] != 3 {
		t.Errorf("openrouter-key lines = %v, want [3]", openrouterLines)
	}
	if len(envLines) != 1 || envLines[0] != 3 {
		t.Errorf("env-assignment lines = %v, want [3]", envLines)
	}
	if len(privKeyLines) != 1 || privKeyLines[0] != 5 {
		t.Errorf("private-key lines = %v, want [5]", privKeyLines)
	}
}

func TestScanString_EmptyInput_NoFindings(t *testing.T) {
	requireNoFindings(t, ScanString("empty.txt", ""))
}

// =============================================================================
// Allowlist
// =============================================================================

func TestAllowlist_LinePrefix_SuppressesCsSk(t *testing.T) {
	// cs_sk_ prefixed test fixtures should not trigger.
	got := ScanString("test.go", `key := "cs_sk_abc123def456ghi789jkl012mno"`)
	requireNoFindings(t, got)
}

func TestAllowlist_LinePrefix_SuppressesTestKey(t *testing.T) {
	got := ScanString("test.go", `test_key_abcdefghijklmnopqrstuvwxyz0123`)
	requireNoFindings(t, got)
}

func TestAllowlist_LinePrefix_SuppressesNolintComment(t *testing.T) {
	got := ScanString("test.go", `// nolint:secret ghp_abcdef0123456789abcdef0123456789abcd`)
	requireNoFindings(t, got)
}

func TestAllowlist_CustomRegex(t *testing.T) {
	scanner := NewScanner(Allowlist{
		AllowRegex: regexp.MustCompile(`sk-prod-`),
	})
	got := scanner.ScanString("config.env",
		`OPENROUTER_API_KEY=sk-prod-abc123def456ghi789jkl012mno`)
	// Both env-assignment and openrouter-key fire; both should be
	// suppressed because the regex matches the key body inside the snippet.
	for _, f := range got {
		t.Errorf("expected no findings, got %+v", f)
	}

	// A different prefix is not suppressed.
	got = scanner.ScanString("config.env",
		`OPENROUTER_API_KEY=sk-staging-abc123def456ghi789jkl012mno`)
	if len(got) == 0 {
		t.Fatalf("expected finding for staging key, got none")
	}
}

func TestAllowlist_EmptyAllowlist_DoesNotSuppress(t *testing.T) {
	scanner := NewScanner(Allowlist{})
	// A real sk- key with empty allowlist IS matched.
	got := scanner.ScanString("test.go", `key := "sk-abc123def456ghi789jkl012mno"`)
	requireFinding(t, got, "openrouter-key")
}

func TestAllowlist_DoesNotMatchAcrossWordBoundary(t *testing.T) {
	// cs_sk_ has no word boundary between c and s, so the \b prefix
	// in PatternOpenRouter rejects it even with an empty allowlist.
	// This is intentional — the default test allowlist also catches it
	// via the cs_sk_ LinePrefix.
	got := ScanString("test.go", `key := "cs_sk_abc123def456ghi789jkl012mno"`)
	requireNoFindings(t, got)
}

// =============================================================================
// Bytes / file APIs
// =============================================================================

func TestScanBytes_FindsFindings(t *testing.T) {
	input := []byte("OPENROUTER_API_KEY=sk-abc123def456ghi789jkl012mno\n")
	got := ScanBytes("input.env", input)
	requireFinding(t, got, "openrouter-key")
}

func TestScanFile_Integration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.env")
	if err := writeFile(path, "ghp_abcdef0123456789abcdef0123456789abcd\n"); err != nil {
		t.Fatalf("writeFile: %v", err)
	}
	got, err := ScanFile(path)
	if err != nil {
		t.Fatalf("ScanFile: %v", err)
	}
	requireFinding(t, got, "github-pat")
	if got[0].File != path {
		t.Errorf("File = %q, want %q", got[0].File, path)
	}
}

func TestScanFile_Nonexistent_ReturnsError(t *testing.T) {
	_, err := ScanFile("/nonexistent/path/to/file")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

// =============================================================================
// ScanPath — directory walk
// =============================================================================

func TestScanPath_DirectoryFindsSecrets(t *testing.T) {
	dir := t.TempDir()
	// One secret file — produces 2 findings: openrouter-key + env-assignment.
	if err := writeFile(filepath.Join(dir, "a.env"), "OPENROUTER_API_KEY=sk-abc123def456ghi789jkl012mno\n"); err != nil {
		t.Fatal(err)
	}
	// One clean file.
	if err := writeFile(filepath.Join(dir, "b.txt"), "hello world\n"); err != nil {
		t.Fatal(err)
	}
	// A nested clean file.
	if err := writeFile(filepath.Join(dir, "sub", "c.txt"), "no secrets here\n"); err != nil {
		t.Fatal(err)
	}
	got, err := ScanPath(dir)
	if err != nil {
		t.Fatalf("ScanPath: %v", err)
	}
	// Expect openrouter-key + env-assignment.
	if len(got) != 2 {
		t.Fatalf("expected 2 findings, got %d: %+v", len(got), got)
	}
	rules := map[string]bool{}
	for _, f := range got {
		rules[f.Rule] = true
	}
	if !rules["openrouter-key"] || !rules["env-assignment"] {
		t.Errorf("expected both openrouter-key and env-assignment, got %v", rules)
	}
}

func TestScanPath_SkipsHiddenDirs(t *testing.T) {
	dir := t.TempDir()
	// .git subdir with a fake secret file — should be skipped.
	if err := writeFile(filepath.Join(dir, ".git", "fake.env"),
		"OPENROUTER_API_KEY=sk-abc123def456ghi789jkl012mno\n"); err != nil {
		// mkdir .git
		mustMkdir(t, filepath.Join(dir, ".git"))
		if err := writeFile(filepath.Join(dir, ".git", "fake.env"),
			"OPENROUTER_API_KEY=sk-abc123def456ghi789jkl012mno\n"); err != nil {
			t.Fatal(err)
		}
	}
	got, err := ScanPath(dir)
	if err != nil {
		t.Fatalf("ScanPath: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected no findings (.git skipped), got %d: %+v", len(got), got)
	}
}

func TestScanPath_SingleFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "single.env")
	if err := writeFile(path, "ZAI_API_KEY=abcdefghijklmnopqrstuvwxyz012345\n"); err != nil {
		t.Fatal(err)
	}
	got, err := ScanPath(path)
	if err != nil {
		t.Fatalf("ScanPath: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(got))
	}
}

func TestScanPath_NonexistentPath_ReturnsError(t *testing.T) {
	_, err := ScanPath("/nonexistent/path/to/dir")
	if err == nil {
		t.Fatal("expected error for nonexistent path")
	}
}

// =============================================================================
// Finding.String
// =============================================================================

func TestFindingString_Format(t *testing.T) {
	f := Finding{
		Rule:    "github-pat",
		File:    "config.yml",
		Line:    42,
		Column:  7,
		Snippet: "ghp_abcdef0123456789abcdef0123456789abcd",
	}
	got := f.String()
	want := `config.yml:42:7: github-pat "ghp_abcdef0123456789abcdef0123456789abcd"`
	if got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

// =============================================================================
// Report / FormatReport
// =============================================================================

func TestReport_ByRule(t *testing.T) {
	r := Report{
		Path: ".",
		Findings: []Finding{
			{Rule: "github-pat"},
			{Rule: "github-pat"},
			{Rule: "openrouter-key"},
		},
	}
	got := r.ByRule()
	want := map[string]int{
		"github-pat":     2,
		"openrouter-key": 1,
	}
	if len(got) != len(want) {
		t.Fatalf("ByRule() size = %d, want %d", len(got), len(want))
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("ByRule()[%q] = %d, want %d", k, got[k], v)
		}
	}
}

func TestReport_HasFindings(t *testing.T) {
	empty := Report{Path: "."}
	if empty.HasFindings() {
		t.Error("empty report should not have findings")
	}
	nonEmpty := Report{Findings: []Finding{{Rule: "x"}}}
	if !nonEmpty.HasFindings() {
		t.Error("non-empty report should have findings")
	}
}

func TestFormatReport_NoFindings(t *testing.T) {
	r := Report{Path: "clean-dir"}
	got := FormatReport(r)
	if !strings.Contains(got, "no secrets detected") {
		t.Errorf("FormatReport missing 'no secrets detected' line: %s", got)
	}
}

func TestFormatReport_WithFindings(t *testing.T) {
	r := Report{
		Path: "leaky-dir",
		Findings: []Finding{
			{Rule: "github-pat", File: "a.yml", Line: 1, Column: 1, Snippet: "ghp_xxx"},
		},
	}
	got := FormatReport(r)
	if !strings.Contains(got, "Findings: 1") {
		t.Errorf("FormatReport missing finding count: %s", got)
	}
	if !strings.Contains(got, "github-pat") {
		t.Errorf("FormatReport missing rule name: %s", got)
	}
}

// =============================================================================
// Edge cases
// =============================================================================

func TestScanLine_NoMatch(t *testing.T) {
	s := NewScanner(Allowlist{})
	got := s.ScanLine("this is a normal line of code")
	if len(got) != 0 {
		t.Errorf("expected no findings, got %+v", got)
	}
}

func TestScanLine_ColumnPosition(t *testing.T) {
	// Position of the match within a line.
	s := NewScanner(Allowlist{})
	got := s.ScanLine(`const token = "ghp_abcdef0123456789abcdef0123456789abcd"`)
	if len(got) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(got))
	}
	// Column should be at the start of "ghp_" — 15 in 1-indexed terms.
	const wantCol = 16
	if got[0].Column != wantCol {
		t.Errorf("Column = %d, want %d", got[0].Column, wantCol)
	}
}

func TestScanReader_LongLine(t *testing.T) {
	// Construct a line longer than the default scanner buffer (1 MiB is
	// our limit). We use 100 KiB which is comfortably above the default
	// 64 KiB start size to verify buffer growth works.
	long := strings.Repeat("a", 100*1024)
	long = "OPENROUTER_API_KEY=sk-abc123def456ghi789jkl012mno" + long
	got := ScanString("big.env", long)
	// Expect openrouter-key + env-assignment, both surviving the long line.
	if len(got) != 2 {
		t.Errorf("expected 2 findings from long line, got %d", len(got))
	}
}

func TestScanner_ImplementsImmutability(t *testing.T) {
	// Finding values are returned as values, not pointers. Verify the
	// scanner does not return shared mutable state.
	s := NewScanner(Allowlist{})
	got := s.ScanString("a", `OPENROUTER_API_KEY=sk-abc123def456ghi789jkl012mno`)
	if len(got) < 1 {
		t.Fatal("expected at least one finding")
	}
	got[0].Snippet = "MUTATED"
	got2 := s.ScanString("a", `OPENROUTER_API_KEY=sk-abc123def456ghi789jkl012mno`)
	if len(got2) < 1 {
		t.Fatal("expected at least one finding on second scan")
	}
	if got2[0].Snippet == "MUTATED" {
		t.Error("ScanString returned shared mutable state")
	}
}

// =============================================================================
// Test helpers
// =============================================================================

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := osMkdirAll(dir); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
}
