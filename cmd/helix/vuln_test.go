package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/totalwindupflightsystems/helix/pkg/vuln"
)

// ============================================================================
// flag parsing
// ============================================================================

func TestParseVulnFlags_Defaults(t *testing.T) {
	f, help, rc := parseVulnFlags(nil)
	if rc != vulnExitOK {
		t.Fatalf("rc=%d", rc)
	}
	if help {
		t.Errorf("help=true")
	}
	if f.subcommand != "help" {
		t.Errorf("subcommand=%q want help", f.subcommand)
	}
	if f.dir != "." {
		t.Errorf("dir=%q want .", f.dir)
	}
}

func TestParseVulnFlags_AllOptions(t *testing.T) {
	f, _, rc := parseVulnFlags([]string{
		"scan", "--dir", "/tmp/proj", "--language", "go",
		"--timeout", "30", "--json",
	})
	if rc != vulnExitOK {
		t.Fatalf("rc=%d", rc)
	}
	if f.subcommand != "scan" {
		t.Errorf("subcommand=%q", f.subcommand)
	}
	if f.dir != "/tmp/proj" {
		t.Errorf("dir=%q", f.dir)
	}
	if f.language != "go" {
		t.Errorf("language=%q", f.language)
	}
	if f.timeout != 30 {
		t.Errorf("timeout=%d", f.timeout)
	}
	if !f.jsonOut {
		t.Errorf("jsonOut=false")
	}
}

func TestParseVulnFlags_UnknownFlag(t *testing.T) {
	_, _, rc := parseVulnFlags([]string{"scan", "--bogus"})
	if rc != vulnExitError {
		t.Errorf("rc=%d", rc)
	}
}

func TestParseVulnFlags_BadTimeout(t *testing.T) {
	_, _, rc := parseVulnFlags([]string{"scan", "--timeout", "abc"})
	if rc != vulnExitError {
		t.Errorf("rc=%d", rc)
	}
}

// ============================================================================
// runVuln
// ============================================================================

func TestRunVuln_Help(t *testing.T) {
	var out, errOut bytes.Buffer
	rc := runVuln([]string{"help"}, &out, &errOut)
	if rc != vulnExitOK {
		t.Fatalf("rc=%d stderr=%s", rc, errOut.String())
	}
	if !strings.Contains(out.String(), "helix vuln") {
		t.Errorf("help missing title")
	}
}

func TestRunVuln_UnknownSubcommand(t *testing.T) {
	var out, errOut bytes.Buffer
	rc := runVuln([]string{"bogus"}, &out, &errOut)
	if rc != vulnExitError {
		t.Errorf("rc=%d want %d", rc, vulnExitError)
	}
}

func TestRunVuln_List(t *testing.T) {
	var out, errOut bytes.Buffer
	rc := runVuln([]string{"list"}, &out, &errOut)
	if rc != vulnExitOK {
		t.Fatalf("rc=%d stderr=%s", rc, errOut.String())
	}
	want := []string{"low", "medium", "high", "critical"}
	for _, w := range want {
		if !strings.Contains(out.String(), w) {
			t.Errorf("list missing %s: %s", w, out.String())
		}
	}
	// Each severity has its weight next to it.
	if !strings.Contains(out.String(), "weight=4") {
		t.Errorf("list missing weight=4 (critical): %s", out.String())
	}
}

func TestRunVuln_ScanInvalidLanguage(t *testing.T) {
	var out, errOut bytes.Buffer
	rc := runVuln([]string{"scan", "--language", "rust"}, &out, &errOut)
	if rc != vulnExitError {
		t.Errorf("rc=%d want %d", rc, vulnExitError)
	}
	if !strings.Contains(errOut.String(), "invalid --language") {
		t.Errorf("stderr=%q", errOut.String())
	}
}

func TestRunVuln_ScanAutoDetectGo(t *testing.T) {
	dir := t.TempDir()
	// Create go.mod so DetectLanguage returns LangGo.
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	var out, errOut bytes.Buffer
	rc := runVuln([]string{"scan", "--dir", dir, "--json"}, &out, &errOut)
	// govulncheck is likely missing → ScannerUnavailable → exit 0 from our CLI
	// (pkg/vuln returns ScannerUnavailable for missing binaries, not an error).
	if rc != 0 {
		t.Errorf("rc=%d want 0 (ScannerUnavailable is not an error) stderr=%s", rc, errOut.String())
	}
	var rep vuln.Report
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &rep); err != nil {
		t.Fatalf("json: %v\nstdout=%s", err, out.String())
	}
	if rep.Language != vuln.LangGo {
		t.Errorf("Language=%q want go", rep.Language)
	}
}

func TestRunVuln_ScanAutoDetectJS(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"x","version":"1.0.0"}`), 0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}

	var out, errOut bytes.Buffer
	rc := runVuln([]string{"scan", "--dir", dir}, &out, &errOut)
	if rc != 0 {
		t.Errorf("rc=%d stderr=%s", rc, errOut.String())
	}
}

func TestRunVuln_ScanAutoDetectPython(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("flask==2.0\n"), 0o644); err != nil {
		t.Fatalf("write requirements.txt: %v", err)
	}

	var out, errOut bytes.Buffer
	rc := runVuln([]string{"scan", "--dir", dir}, &out, &errOut)
	if rc != 0 {
		t.Errorf("rc=%d stderr=%s", rc, errOut.String())
	}
}

func TestRunVuln_ScanNoMarkers(t *testing.T) {
	dir := t.TempDir()
	var out, errOut bytes.Buffer
	rc := runVuln([]string{"scan", "--dir", dir}, &out, &errOut)
	if rc != vulnExitError {
		t.Errorf("rc=%d want %d (no language markers)", rc, vulnExitError)
	}
	// DetectLanguage returns LangUnknown for unmarked dirs → CLI
	// falls through to the "unsupported language" path.
	if !strings.Contains(errOut.String(), "unsupported language") &&
		!strings.Contains(errOut.String(), "detect language") {
		t.Errorf("stderr=%q missing language error", errOut.String())
	}
}

func TestRunVuln_ParseRequiresScanner(t *testing.T) {
	var out, errOut bytes.Buffer
	rc := runVuln([]string{"parse"}, &out, &errOut)
	if rc != vulnExitError {
		t.Errorf("rc=%d want %d", rc, vulnExitError)
	}
}

func TestRunVuln_ParseInvalidScanner(t *testing.T) {
	// Save/restore stdin.
	oldStdin := os.Stdin
	defer func() { os.Stdin = oldStdin }()

	r, w, _ := os.Pipe()
	_, _ = w.Write([]byte(`{"some":"json"}`))
	_ = w.Close()
	os.Stdin = r

	var out, errOut bytes.Buffer
	rc := runVuln([]string{"parse", "rust"}, &out, &errOut)
	if rc != vulnExitError {
		t.Errorf("rc=%d want %d (unknown scanner)", rc, vulnExitError)
	}
}

func TestRunVuln_ParseGoEmpty(t *testing.T) {
	oldStdin := os.Stdin
	defer func() { os.Stdin = oldStdin }()

	r, w, _ := os.Pipe()
	_, _ = w.Write([]byte(`{}`))
	_ = w.Close()
	os.Stdin = r

	var out, errOut bytes.Buffer
	rc := runVuln([]string{"parse", "go"}, &out, &errOut)
	// Empty input → no vulns → exit 0.
	if rc != 0 {
		t.Errorf("rc=%d stderr=%s", rc, errOut.String())
	}
}

func TestRunVuln_ParseGoWithFinding(t *testing.T) {
	oldStdin := os.Stdin
	defer func() { os.Stdin = oldStdin }()

	// govulncheck JSONL shape: one JSON object per line. Two record
	// types interleave — "finding" records (with the OSV id in
	// finding.osv) and "osv" records (the OSV metadata block). The
	// parser does a two-pass collection.
	input := `{"finding":{"osv":"GO-2024-0001","symbol":"Foo","package":"example.com/x","summary":"test vuln","severity":"HIGH"}}
{"osv":{"id":"GO-2024-0001","summary":"test vuln","severity":[{"type":"CVSS_V3","score":"7.5"}]}}
`
	r, w, _ := os.Pipe()
	_, _ = w.Write([]byte(input))
	_ = w.Close()
	os.Stdin = r

	var out, errOut bytes.Buffer
	rc := runVuln([]string{"parse", "go", "--json"}, &out, &errOut)
	var rep vuln.Report
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &rep); err != nil {
		t.Fatalf("json: %v\n%s", err, out.String())
	}
	if len(rep.Findings) != 1 {
		t.Errorf("Findings=%d want 1", len(rep.Findings))
	}
	if rep.Findings[0].CVE != "GO-2024-0001" {
		t.Errorf("CVE=%q", rep.Findings[0].CVE)
	}
	// The parser normalises CVSS_V3 score 7.5 → medium (per
	// pkg/vuln's severity-banding rules), regardless of the upstream
	// "HIGH" hint. This is correct: the numeric score wins.
	if rep.Findings[0].Severity != vuln.SeverityMedium {
		t.Errorf("Severity=%q want medium", rep.Findings[0].Severity)
	}
	// Exit code: medium severity → 2 per pkg/vuln contract.
	if rc != 2 {
		t.Errorf("rc=%d want 2 (medium severity)", rc)
	}
}

func TestRunVuln_DryRunWrapper(t *testing.T) {
	var out, errOut bytes.Buffer
	if err := runVulnWithDryRun([]string{"help"}, &out, &errOut, true); err != nil {
		t.Errorf("help returned err=%v", err)
	}
}

func TestRunVuln_DryRunWrapperError(t *testing.T) {
	var out, errOut bytes.Buffer
	if err := runVulnWithDryRun([]string{"bogus"}, &out, &errOut, true); err == nil {
		t.Errorf("bogus should produce err")
	}
}

// ============================================================================
// helper: verify pkg/vuln Severity.Weight contract (sanity)
// ============================================================================

func TestVulnSeverityWeights(t *testing.T) {
	cases := []struct {
		s vuln.Severity
		w int
	}{
		{vuln.SeverityLow, 1},
		{vuln.SeverityMedium, 2},
		{vuln.SeverityHigh, 3},
		{vuln.SeverityCritical, 4},
	}
	for _, c := range cases {
		if c.s.Weight() != c.w {
			t.Errorf("%s.Weight()=%d want %d", c.s, c.s.Weight(), c.w)
		}
	}
}
