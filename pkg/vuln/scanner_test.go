package vuln

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// Severity & Language detection
// =============================================================================

func TestParseSeverity_AllKnown(t *testing.T) {
	cases := map[string]Severity{
		"low":      SeverityLow,
		"LOW":      SeverityLow,
		" medium ": SeverityMedium,
		"med":      SeverityMedium,
		"moderate": SeverityMedium,
		"high":     SeverityHigh,
		"HIGH":     SeverityHigh,
		"critical": SeverityCritical,
		"crit":     SeverityCritical,
	}
	for in, want := range cases {
		got, err := ParseSeverity(in)
		if err != nil {
			t.Errorf("ParseSeverity(%q) returned error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseSeverity(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseSeverity_UnknownReturnsError(t *testing.T) {
	for _, in := range []string{"", "ultra", "10", "sev"} {
		if _, err := ParseSeverity(in); err == nil {
			t.Errorf("ParseSeverity(%q) expected error, got nil", in)
		}
	}
}

func TestSeverityWeight_Ordering(t *testing.T) {
	if !(SeverityLow.Weight() < SeverityMedium.Weight()) {
		t.Error("low should be < medium")
	}
	if !(SeverityMedium.Weight() < SeverityHigh.Weight()) {
		t.Error("medium should be < high")
	}
	if !(SeverityHigh.Weight() < SeverityCritical.Weight()) {
		t.Error("high should be < critical")
	}
	if Severity("").Weight() != 0 {
		t.Error("unknown severity should have weight 0")
	}
}

func TestAllSeverities_ReturnsCanonicalOrder(t *testing.T) {
	want := []Severity{SeverityLow, SeverityMedium, SeverityHigh, SeverityCritical}
	got := AllSeverities()
	if len(got) != len(want) {
		t.Fatalf("got %d severities, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestDetectLanguage_AllManifests(t *testing.T) {
	cases := []struct {
		files []string
		want  Language
	}{
		{[]string{"go.mod"}, LangGo},
		{[]string{"pyproject.toml"}, LangPython},
		{[]string{"requirements.txt"}, LangPython},
		{[]string{"setup.py"}, LangPython},
		{[]string{"package.json"}, LangJS},
		// Precedence: Go beats Python beats JS (matches spec §6.6 ordering)
		{[]string{"go.mod", "pyproject.toml", "package.json"}, LangGo},
		{[]string{"pyproject.toml", "package.json"}, LangPython},
		// No manifest
		{nil, LangUnknown},
		{[]string{"README.md", "main.tf"}, LangUnknown},
	}
	for _, tc := range cases {
		dir := t.TempDir()
		for _, f := range tc.files {
			path := filepath.Join(dir, f)
			if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		got, err := DetectLanguage(dir)
		if err != nil {
			t.Errorf("DetectLanguage(%v) error: %v", tc.files, err)
			continue
		}
		if got != tc.want {
			t.Errorf("DetectLanguage(%v) = %q, want %q", tc.files, got, tc.want)
		}
	}
}

func TestDetectLanguage_IgnoresDirectories(t *testing.T) {
	// A directory named "go.mod" must not be detected as Go.
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "go.mod"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := DetectLanguage(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != LangUnknown {
		t.Errorf("DetectLanguage should ignore directories; got %q", got)
	}
}

func TestDetectLanguage_MissingDirReturnsUnknown(t *testing.T) {
	got, err := DetectLanguage("/this/path/does/not/exist/anywhere/12345")
	if err != nil {
		t.Fatalf("DetectLanguage on missing dir returned error: %v", err)
	}
	if got != LangUnknown {
		t.Errorf("DetectLanguage on missing dir = %q, want unknown", got)
	}
}

// =============================================================================
// Parsers — unit-level fixture tests
// =============================================================================

func TestParseGoVulnCheck_HappyPath(t *testing.T) {
	stdout := `{"finding":{"osv":"GO-2024-0001","symbol":"foo.Bar","package":"github.com/x/y","version":"1.0.0","summary":"Bad thing"}}
{"id":"GO-2024-0001","aliases":["CVE-2024-9999"],"summary":"Bad thing","severity":[{"type":"CVSS_V3","score":"8.5"}],"affected":[{"package":{"name":"github.com/x/y"},"ranges":[{"type":"SEMVER","events":[{"introduced":"0"},{"fixed":"1.2.3"}]}]}],"references":[{"type":"WEB","url":"https://example.com/advisory"}]}`
	got, err := ParseGoVulnCheck(stdout)
	if err != nil {
		t.Fatalf("ParseGoVulnCheck error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 finding, got %d", len(got))
	}
	v := got[0]
	if v.CVE != "CVE-2024-9999" {
		t.Errorf("CVE = %q, want CVE-2024-9999", v.CVE)
	}
	if v.Package != "github.com/x/y" {
		t.Errorf("Package = %q", v.Package)
	}
	if v.Severity != SeverityHigh {
		t.Errorf("Severity = %q, want high (CVSS 8.5)", v.Severity)
	}
	if v.FixedIn != "1.2.3" {
		t.Errorf("FixedIn = %q", v.FixedIn)
	}
	if v.AdvisoryURL != "https://example.com/advisory" {
		t.Errorf("AdvisoryURL = %q", v.AdvisoryURL)
	}
}

func TestParseGoVulnCheck_DeduplicatesByOSV(t *testing.T) {
	stdout := `{"finding":{"osv":"GO-2024-0001","symbol":"a","package":"x/y","summary":"x"}}
{"finding":{"osv":"GO-2024-0001","symbol":"b","package":"x/y","summary":"y"}}
{"id":"GO-2024-0001","aliases":["CVE-2024-1234"]}`
	got, err := ParseGoVulnCheck(stdout)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("want 1 deduped finding, got %d", len(got))
	}
}

func TestParseGoVulnCheck_SeverityFromCVSSBuckets(t *testing.T) {
	cases := []struct {
		score string
		want  Severity
	}{
		{"9.5", SeverityCritical},
		{"9.0", SeverityCritical},
		{"8.9", SeverityHigh},
		{"7.0", SeverityHigh},
		{"6.9", SeverityMedium},
		{"4.0", SeverityMedium},
		{"3.9", SeverityLow},
		{"0.1", SeverityLow},
	}
	for _, tc := range cases {
		stdout := `{"finding":{"osv":"X","symbol":"a","package":"p","summary":"s"}}
{"id":"X","aliases":["CVE-X"],"severity":[{"type":"CVSS_V3","score":"` + tc.score + `"}]}`
		got, err := ParseGoVulnCheck(stdout)
		if err != nil {
			t.Fatalf("score %s: %v", tc.score, err)
		}
		if len(got) != 1 || got[0].Severity != tc.want {
			sev := Severity("")
			if len(got) == 1 {
				sev = got[0].Severity
			}
			t.Errorf("score %s -> severity %q, want %q", tc.score, sev, tc.want)
		}
	}
}

func TestParseGoVulnCheck_NoCVSSDefaultsMedium(t *testing.T) {
	stdout := `{"finding":{"osv":"X","symbol":"a","package":"p","summary":"s"}}
{"id":"X","aliases":["CVE-X"]}`
	got, err := ParseGoVulnCheck(stdout)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Severity != SeverityMedium {
		t.Errorf("no CVSS should default medium; got %+v", got)
	}
}

func TestParseGoVulnCheck_IgnoresMalformedLines(t *testing.T) {
	stdout := "not-json\n{\"unrelated\":1}\n"
	got, err := ParseGoVulnCheck(stdout)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("malformed input should yield 0 findings; got %d", len(got))
	}
}

func TestParseNPMAudit_HappyPath(t *testing.T) {
	payload := map[string]any{
		"vulnerabilities": map[string]any{
			"lodash": map[string]any{
				"severity": "high",
				"via": []any{
					map[string]any{
						"source":   1,
						"name":     "lodash",
						"title":    "Prototype pollution",
						"url":      "https://github.com/advisories/GHSA-xxxx",
						"severity": "high",
						"range":    ">=4.0.0 <4.17.21",
					},
				},
			},
		},
	}
	b, _ := json.Marshal(payload)
	got, err := ParseNPMAudit(string(b))
	if err != nil {
		t.Fatalf("ParseNPMAudit error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 finding, got %d", len(got))
	}
	v := got[0]
	if v.Package != "lodash" {
		t.Errorf("Package = %q", v.Package)
	}
	if v.Severity != SeverityHigh {
		t.Errorf("Severity = %q", v.Severity)
	}
	if v.Summary != "Prototype pollution" {
		t.Errorf("Summary = %q", v.Summary)
	}
	if v.FixedIn != "4.0.0 <4.17.21" {
		t.Errorf("FixedIn = %q", v.FixedIn)
	}
	if v.AdvisoryURL != "https://github.com/advisories/GHSA-xxxx" {
		t.Errorf("AdvisoryURL = %q", v.AdvisoryURL)
	}
}

func TestParseNPMAudit_PackageLevelSeverityWhenViaIsString(t *testing.T) {
	payload := `{"vulnerabilities":{"x":{"severity":"medium","via":["y"]}}}`
	got, err := ParseNPMAudit(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Severity != SeverityMedium {
		t.Errorf("expected medium from package-level severity; got %+v", got)
	}
}

func TestParseNPMAudit_InvalidJSONReturnsError(t *testing.T) {
	if _, err := ParseNPMAudit("not json"); err == nil {
		t.Error("expected error on invalid JSON")
	}
}

func TestParsePipAudit_HappyPath(t *testing.T) {
	payload := map[string]any{
		"dependencies": []any{
			map[string]any{
				"name":    "requests",
				"version": "2.25.0",
				"vulns": []any{
					map[string]any{
						"id":           "PYSEC-2024-1",
						"aliases":      []string{"CVE-2024-12345"},
						"description":  "SSL verification bypass",
						"advisory":     "https://osv.dev/PYSEC-2024-1",
						"fix_versions": []string{"2.32.0"},
						"cvss":         map[string]any{"score": 7.5},
					},
				},
			},
		},
	}
	b, _ := json.Marshal(payload)
	got, err := ParsePipAudit(string(b))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 finding, got %d", len(got))
	}
	v := got[0]
	if v.CVE != "CVE-2024-12345" {
		t.Errorf("CVE = %q", v.CVE)
	}
	if v.Package != "requests" {
		t.Errorf("Package = %q", v.Package)
	}
	if v.Severity != SeverityHigh {
		t.Errorf("Severity = %q (CVSS 7.5)", v.Severity)
	}
	if v.FixedIn != "2.32.0" {
		t.Errorf("FixedIn = %q", v.FixedIn)
	}
	if v.AdvisoryURL != "https://osv.dev/PYSEC-2024-1" {
		t.Errorf("AdvisoryURL = %q", v.AdvisoryURL)
	}
}

func TestParsePipAudit_MultipleAdvisoriesPerDep(t *testing.T) {
	payload := `{"dependencies":[{"name":"django","version":"3.0","vulns":[{"id":"A","description":"a","fix_versions":["3.2"]},{"id":"B","description":"b","fix_versions":["3.1"]}]}]}`
	got, err := ParsePipAudit(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("want 2 findings (one per advisory), got %d", len(got))
	}
}

func TestParsePipAudit_InvalidJSONReturnsError(t *testing.T) {
	if _, err := ParsePipAudit("nope"); err == nil {
		t.Error("expected error on invalid JSON")
	}
}

// =============================================================================
// Report helpers
// =============================================================================

func TestReport_HighestSeverityAndCount(t *testing.T) {
	r := &Report{
		Findings: []Vulnerability{
			{Severity: SeverityLow},
			{Severity: SeverityCritical},
			{Severity: SeverityMedium},
			{Severity: SeverityCritical},
		},
	}
	if r.HighestSeverity() != SeverityCritical {
		t.Errorf("HighestSeverity = %q", r.HighestSeverity())
	}
	c := r.CountBySeverity()
	if c[SeverityLow] != 1 || c[SeverityMedium] != 1 || c[SeverityHigh] != 0 || c[SeverityCritical] != 2 {
		t.Errorf("CountBySeverity = %+v", c)
	}
}

func TestReport_EmptyHighestIsBlank(t *testing.T) {
	r := &Report{}
	if r.HighestSeverity() != Severity("") {
		t.Errorf("empty report should have blank highest severity; got %q", r.HighestSeverity())
	}
}

func TestReport_ExitCode(t *testing.T) {
	cases := []struct {
		name string
		sev  Severity
		want int
	}{
		{"critical", SeverityCritical, 1},
		{"high", SeverityHigh, 1},
		{"medium", SeverityMedium, 2},
		{"low", SeverityLow, 0},
		{"empty", "", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &Report{}
			if tc.sev != "" {
				r.Findings = []Vulnerability{{Severity: tc.sev}}
			}
			if got := r.ExitCode(); got != tc.want {
				t.Errorf("ExitCode = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestReport_FormatSummary_NoFindings(t *testing.T) {
	r := &Report{Language: LangGo, Status: ScannerOK}
	s := r.FormatSummary()
	if !strings.Contains(s, "0 findings") {
		t.Errorf("summary should report 0 findings; got %q", s)
	}
}

func TestReport_FormatSummary_WithFindings(t *testing.T) {
	r := &Report{
		Language: LangJS,
		Status:   ScannerOK,
		Findings: []Vulnerability{
			{Severity: SeverityHigh},
			{Severity: SeverityMedium},
		},
	}
	s := r.FormatSummary()
	for _, want := range []string{"javascript", "2 findings", "high=1", "medium=1", "highest=high"} {
		if !strings.Contains(s, want) {
			t.Errorf("summary missing %q: %q", want, s)
		}
	}
}

func TestReport_FormatSummary_NonOKStatus(t *testing.T) {
	r := &Report{Language: LangPython, Status: ScannerUnavailable}
	s := r.FormatSummary()
	if !strings.Contains(s, "unavailable") {
		t.Errorf("summary should report unavailable; got %q", s)
	}
}

// =============================================================================
// Scanner — Executor injection
// =============================================================================

// fakeExec returns canned output regardless of the binary/args. The
// exitCode is passed through; err is set when code != 0 to mirror
// exec.ExitError behaviour.
func fakeExec(stdout, stderr string, code int) Executor {
	return func(_ context.Context, _, _ string, _ []string) (string, string, int, error) {
		var err error
		if code != 0 {
			// Mirror cmd.Run error for code != 0 without exposing
			// exec.ExitError internals here.
			err = &fakeExitError{code: code}
		}
		return stdout, stderr, code, err
	}
}

type fakeExitError struct{ code int }

func (e *fakeExitError) Error() string { return "fake exit" }

func TestScanner_DefaultsArePopulated(t *testing.T) {
	s := NewScanner()
	if s.Executor == nil {
		t.Error("Executor must default to DefaultExecutor")
	}
	if s.Timeout != 60*time.Second {
		t.Errorf("Timeout = %v, want 60s", s.Timeout)
	}
	if s.GoBin != "govulncheck" || s.JSBin != "npm" || s.PyBin != "pip-audit" {
		t.Errorf("binary defaults wrong: %+v", s)
	}
}

func TestScanner_RunGo_RecordsFindings(t *testing.T) {
	stdout := `{"finding":{"osv":"GO-1","symbol":"x.Bar","package":"x/y","summary":"x"}}
{"id":"GO-1","aliases":["CVE-1"],"severity":[{"type":"CVSS_V3","score":"9.5"}]}`
	s := NewScanner().SetTestExecutor(fakeExec(stdout, "", 3)) // govulncheck exits 3 on vulns
	dir := t.TempDir()
	r, err := s.RunGo(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != ScannerOK {
		t.Errorf("Status = %q, want ok", r.Status)
	}
	if len(r.Findings) != 1 {
		t.Errorf("Findings = %d, want 1", len(r.Findings))
	}
	if r.Findings[0].Severity != SeverityCritical {
		t.Errorf("Severity = %q", r.Findings[0].Severity)
	}
	if r.ElapsedMS < 0 {
		t.Errorf("ElapsedMS = %d", r.ElapsedMS)
	}
}

func TestScanner_RunGo_BinaryMissingReturnsUnavailable(t *testing.T) {
	s := NewScanner()
	s.GoBin = "/no/such/binary/anywhere"
	dir := t.TempDir()
	r, err := s.RunGo(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != ScannerUnavailable {
		t.Errorf("Status = %q, want unavailable", r.Status)
	}
	if !strings.Contains(r.Stderr, "not on PATH") {
		t.Errorf("Stderr should mention PATH: %q", r.Stderr)
	}
}

func TestScanner_RunGo_TimeoutCancels(t *testing.T) {
	// Executor blocks on ctx.Done so we can verify the timeout path.
	blockingExec := Executor(func(ctx context.Context, _, _ string, _ []string) (string, string, int, error) {
		<-ctx.Done()
		return "", "", -1, ctx.Err()
	})
	s := NewScanner().SetTestExecutor(blockingExec)
	s.Timeout = 10 * time.Millisecond
	dir := t.TempDir()
	r, err := s.RunGo(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != ScannerTimeout {
		t.Errorf("Status = %q, want timeout", r.Status)
	}
}

func TestScanner_RunGo_GarbageStdoutYieldsZeroFindingsOK(t *testing.T) {
	// The parser is intentionally tolerant: malformed JSON lines are
	// skipped, not failed. This guarantees that a scanner that emits
	// a stream of mixed control + finding messages still parses the
	// findings we recognise. Garbage-only stdout must therefore yield
	// zero findings with ScannerOK (not ScannerError).
	s := NewScanner().SetTestExecutor(fakeExec("garbage stdout", "", 0))
	dir := t.TempDir()
	r, err := s.RunGo(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != ScannerOK {
		t.Errorf("Status = %q, want ok (parser is tolerant)", r.Status)
	}
	if len(r.Findings) != 0 {
		t.Errorf("Findings = %d, want 0", len(r.Findings))
	}
}

func TestScanner_RunJS_HappyPath(t *testing.T) {
	payload := `{"vulnerabilities":{"x":{"severity":"medium","via":[{"title":"x","url":"u"}]}}}`
	s := NewScanner().SetTestExecutor(fakeExec(payload, "", 1)) // npm audit exits 1 on findings
	dir := t.TempDir()
	r, err := s.RunJS(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != ScannerOK {
		t.Errorf("Status = %q, want ok", r.Status)
	}
	if len(r.Findings) != 1 || r.Findings[0].Severity != SeverityMedium {
		t.Errorf("Findings = %+v", r.Findings)
	}
}

func TestScanner_RunJS_BinaryMissingReturnsUnavailable(t *testing.T) {
	s := NewScanner()
	s.JSBin = "/no/such/npm"
	r, err := s.RunJS(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != ScannerUnavailable {
		t.Errorf("Status = %q", r.Status)
	}
}

func TestScanner_RunPython_HappyPath(t *testing.T) {
	payload := `{"dependencies":[{"name":"r","version":"1","vulns":[{"id":"P1","description":"d","fix_versions":["2"]}]}]}`
	s := NewScanner().SetTestExecutor(fakeExec(payload, "", 1))
	dir := t.TempDir()
	r, err := s.RunPython(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != ScannerOK {
		t.Errorf("Status = %q, want ok", r.Status)
	}
	if len(r.Findings) != 1 || r.Findings[0].Package != "r" {
		t.Errorf("Findings = %+v", r.Findings)
	}
}

func TestScanner_RunPython_BinaryMissingReturnsUnavailable(t *testing.T) {
	s := NewScanner()
	s.PyBin = "/no/such/pip-audit"
	r, err := s.RunPython(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != ScannerUnavailable {
		t.Errorf("Status = %q", r.Status)
	}
}

func TestScanner_SetTestExecutor_DisablesLookPath(t *testing.T) {
	// SetTestExecutor must turn off productionOK so the LookPath gate
	// does not intercept canned-output test runs.
	s := NewScanner()
	if !s.productionOK {
		t.Fatal("NewScanner should set productionOK=true")
	}
	s.SetTestExecutor(fakeExec("{}", "", 0))
	if s.productionOK {
		t.Error("SetTestExecutor must clear productionOK")
	}
}

// =============================================================================
// Scanner.Scan — auto-detect dispatch
// =============================================================================

func TestScan_DispatchesToCorrectRunner(t *testing.T) {
	cases := []struct {
		files    []string
		wantLang Language
	}{
		{[]string{"go.mod"}, LangGo},
		{[]string{"package.json"}, LangJS},
		{[]string{"pyproject.toml"}, LangPython},
	}
	for _, tc := range cases {
		dir := t.TempDir()
		for _, f := range tc.files {
			if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		s := NewScanner()
		r, err := s.Scan(context.Background(), dir)
		if err != nil {
			t.Fatalf("Scan error: %v", err)
		}
		if r.Language != tc.wantLang {
			t.Errorf("Scan(%v) language = %q, want %q", tc.files, r.Language, tc.wantLang)
		}
		// Status depends on whether the binary is on PATH and whether
		// the dummy fixture dir is valid for the scanner — not on the
		// dispatch logic. Only reject a fatal error (nil-Report, non-nil
		// error on the Scan itself).
	}
}

func TestScan_NoManifestReturnsErrorReport(t *testing.T) {
	dir := t.TempDir()
	s := NewScanner()
	r, err := s.Scan(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != ScannerError {
		t.Errorf("Status = %q, want error", r.Status)
	}
	if !strings.Contains(r.Stderr, "no recognised manifest") {
		t.Errorf("Stderr should mention missing manifest: %q", r.Stderr)
	}
}

// =============================================================================
// parseFloatPrefix — internal helper
// =============================================================================

func TestParseFloatPrefix(t *testing.T) {
	cases := []struct {
		in      string
		want    float64
		wantErr bool
	}{
		{"7.5/CVSS:3.1", 7.5, false},
		{"  9.0 ", 9.0, false},
		{"AV:N/AC:L", 0, true},
		{"", 0, true},
		{"abc", 0, true},
	}
	for _, tc := range cases {
		got, err := parseFloatPrefix(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseFloatPrefix(%q) expected error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseFloatPrefix(%q) error: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseFloatPrefix(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// =============================================================================
// sortFindings — deterministic order
// =============================================================================

func TestSortFindings_BySeverityDescThenPackage(t *testing.T) {
	in := []Vulnerability{
		{Package: "b", Severity: SeverityLow},
		{Package: "a", Severity: SeverityLow},
		{Package: "x", Severity: SeverityCritical},
		{Package: "y", Severity: SeverityMedium},
		{Package: "z", Severity: SeverityHigh},
	}
	sortFindings(in)
	wantOrder := []Severity{SeverityCritical, SeverityHigh, SeverityMedium, SeverityLow, SeverityLow}
	wantPkg := []string{"x", "z", "y", "a", "b"}
	for i := range in {
		if in[i].Severity != wantOrder[i] {
			t.Errorf("[%d] severity = %q, want %q", i, in[i].Severity, wantOrder[i])
		}
		if in[i].Package != wantPkg[i] {
			t.Errorf("[%d] package = %q, want %q", i, in[i].Package, wantPkg[i])
		}
	}
}

// =============================================================================
// DefaultExecutor — sanity check (only runs when the binary is present)
// =============================================================================

func TestDefaultExecutor_BinaryNotFoundIsErrNotFound(t *testing.T) {
	_, _, _, err := DefaultExecutor(context.Background(), t.TempDir(), "/no/such/binary/here", nil)
	if err == nil {
		t.Error("expected error from DefaultExecutor with missing binary")
	}
}
