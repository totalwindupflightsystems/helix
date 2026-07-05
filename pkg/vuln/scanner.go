// Package vuln implements the dependency vulnerability scan runner per
// specs/SPECIFICATION.md §6.6 (Dependency vulnerability scan).
//
// The package wraps three external scanners — govulncheck (Go), npm audit
// (Node.js / JavaScript), pip-audit (Python) — behind a single, language-
// aware API. Each scanner emits a language-specific JSON document; the
// runners here normalise the output into a unified Vulnerability report
// that callers (CI steps, doctor, dispatch) can act on with a single
// severity-based exit code.
//
// Design notes:
//
//   - Scanner binaries are NOT required at build or test time. Each
//     language runner uses a pluggable Executor function (defaults to
//     exec.CommandContext) so unit tests can inject canned stdout/stderr
//     and exit codes without invoking the real scanners. Callers
//     constructing a Scanner that points at a real path must therefore
//     treat the binary as an external dependency.
//
//   - When the binary is missing (exec.LookPath returns ENOENT), the
//     runner records ScannerUnavailable rather than returning an error.
//     This matches the rest of Helix's "soft-fail on missing external
//     tool" convention so a clean checkout still produces a usable
//     report.
//
//   - Exit codes follow spec §6.6: critical/high → 1, medium → 2,
//     low → 0. Callers that want a unified "vulnerabilities found"
//     signal should treat any non-zero ExitCode as a CI failure.
package vuln

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// =============================================================================
// Severity & Language Types
// =============================================================================

// Severity classifies a single vulnerability finding. The string values
// match what the upstream scanners emit (low/medium/high/critical).
type Severity string

const (
	SeverityLow      Severity = "low"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

// AllSeverities returns the canonical severity ordering from least to most
// severe. Useful for sorted CLI output and severity-band aggregation.
func AllSeverities() []Severity {
	return []Severity{SeverityLow, SeverityMedium, SeverityHigh, SeverityCritical}
}

// ParseSeverity converts a free-form severity string (case-insensitive) to
// a Severity. Returns an error for unknown values so callers can fail
// loudly on bad scanner output.
func ParseSeverity(s string) (Severity, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "low":
		return SeverityLow, nil
	case "medium", "med", "moderate":
		return SeverityMedium, nil
	case "high":
		return SeverityHigh, nil
	case "critical", "crit":
		return SeverityCritical, nil
	default:
		return "", fmt.Errorf("vuln: unknown severity %q", s)
	}
}

// Weight returns a numeric ordering suitable for "highest severity wins"
// comparisons. Critical=4, High=3, Medium=2, Low=1, "" (unknown)=0.
func (s Severity) Weight() int {
	switch s {
	case SeverityCritical:
		return 4
	case SeverityHigh:
		return 3
	case SeverityMedium:
		return 2
	case SeverityLow:
		return 1
	default:
		return 0
	}
}

// Language identifies the project language for vulnerability scanning.
// Detection is purely file-extension-based so the runner does not need to
// import language-specific toolchains.
type Language string

const (
	LangGo      Language = "go"
	LangJS      Language = "javascript"
	LangPython  Language = "python"
	LangUnknown Language = ""
)

// DetectLanguage inspects the supplied directory and returns the
// language whose manifest file it finds first. Precedence (highest
// first) is Go > Python > JavaScript because the Go ecosystem is
// the primary Helix host language.
//
// Recognised files:
//   - go.mod            → LangGo
//   - pyproject.toml    → LangPython
//   - requirements.txt  → LangPython
//   - setup.py          → LangPython
//   - package.json      → LangJS
//
// Returns LangUnknown when no recognised manifest is present — callers
// should report a "no scan target" finding rather than attempting a
// scan. An error is returned only when the directory itself is
// inaccessible.
func DetectLanguage(projectDir string) (Language, error) {
	candidates := []struct {
		file Language
		path string
	}{
		{LangGo, "go.mod"},
		{LangPython, "pyproject.toml"},
		{LangPython, "requirements.txt"},
		{LangPython, "setup.py"},
		{LangJS, "package.json"},
	}
	for _, c := range candidates {
		full := filepath.Join(projectDir, c.path)
		if info, err := os.Stat(full); err == nil && !info.IsDir() {
			return c.file, nil
		}
	}
	return LangUnknown, nil
}

// =============================================================================
// Scanner status — distinguishes a clean run from "tool missing"
// =============================================================================

// ScannerStatus reports the outcome of a single language-scanner invocation.
// The values are independent of Vulnerability findings: a scanner may run
// cleanly and report zero findings, or it may be missing/timeout/error.
type ScannerStatus string

const (
	ScannerOK          ScannerStatus = "ok"
	ScannerUnavailable ScannerStatus = "unavailable" // binary not on PATH
	ScannerTimeout     ScannerStatus = "timeout"     // context deadline
	ScannerError       ScannerStatus = "error"       // non-zero exit with stderr
)

// =============================================================================
// Vulnerability + Report
// =============================================================================

// Vulnerability is the unified finding record produced by every scanner
// runner. Fields are populated to the extent the upstream scanner exposes
// them — package and severity are always present; CVE / FixedIn /
// AdvisoryURL are populated when the upstream payload includes them.
type Vulnerability struct {
	CVE         string   // e.g. "CVE-2024-1234"; "" when not exposed
	Package     string   // affected module/dependency, e.g. "github.com/x/y"
	Severity    Severity // low | medium | high | critical
	FixedIn     string   // version that fixes the issue; "" if none known
	AdvisoryURL string   // upstream advisory / NVD link; "" if not exposed
	Summary     string   // human-readable one-liner from the upstream scanner
}

// Report is the top-level result returned by a Scan. It captures which
// language was scanned, the scanner status, every finding, and counters
// per severity band.
type Report struct {
	Language    Language
	Status      ScannerStatus
	Findings    []Vulnerability
	Stdout      string // captured scanner stdout (truncated by Executor; see Executor docs)
	Stderr      string // captured scanner stderr
	ElapsedMS   int64  // wall-clock scan time
	GeneratedAt time.Time
	ProjectDir  string
}

// HighestSeverity returns the maximum severity across Findings, or ""
// when the report is empty or the scanner did not run cleanly.
func (r *Report) HighestSeverity() Severity {
	max := Severity("")
	for _, f := range r.Findings {
		if f.Severity.Weight() > max.Weight() {
			max = f.Severity
		}
	}
	return max
}

// CountBySeverity returns a map from severity to finding count. Severities
// with zero findings are still present in the map (with value 0) for
// stable iteration in CLI output.
func (r *Report) CountBySeverity() map[Severity]int {
	out := map[Severity]int{
		SeverityLow:      0,
		SeverityMedium:   0,
		SeverityHigh:     0,
		SeverityCritical: 0,
	}
	for _, f := range r.Findings {
		out[f.Severity]++
	}
	return out
}

// ExitCode returns the recommended CI exit code per spec §6.6:
//   - critical / high findings → 1
//   - medium findings          → 2
//   - low findings             → 0
//   - no findings              → 0
//
// Scanner status is intentionally ignored here — the caller decides
// whether a missing/timeout scanner is itself a CI failure. Use the
// ScannerStatus field for that.
func (r *Report) ExitCode() int {
	switch r.HighestSeverity() {
	case SeverityCritical, SeverityHigh:
		return 1
	case SeverityMedium:
		return 2
	default:
		return 0
	}
}

// FormatSummary returns a one-line human-readable summary suitable for
// CLI output: "<lang>: <n> findings (highest=<sev>)". When the scanner
// did not produce findings, the message reflects the scanner status.
func (r *Report) FormatSummary() string {
	if r.Status != ScannerOK {
		return fmt.Sprintf("%s: scanner %s", r.Language, r.Status)
	}
	if len(r.Findings) == 0 {
		return fmt.Sprintf("%s: 0 findings", r.Language)
	}
	counts := r.CountBySeverity()
	return fmt.Sprintf("%s: %d findings (low=%d, medium=%d, high=%d, critical=%d; highest=%s)",
		r.Language, len(r.Findings),
		counts[SeverityLow], counts[SeverityMedium], counts[SeverityHigh], counts[SeverityCritical],
		r.HighestSeverity())
}

// =============================================================================
// Scanner — pluggable command executor
// =============================================================================

// Executor abstracts the exec.CommandContext call. The default Executor
// (DefaultExecutor) shells out to the real binary; tests inject a fake
// that returns canned stdout/stderr/exit codes without touching the host.
//
// Stdout/Stderr are captured separately so callers can render them on
// CI failure. Implementations MUST honour ctx cancellation so the 60s
// default timeout works as expected.
type Executor func(ctx context.Context, dir, name string, args []string) (stdout string, stderr string, exitCode int, err error)

// DefaultExecutor shells out to the real binary using exec.CommandContext.
// It returns exec.Error (typically *exec.ExitError) in err when the
// command exits non-zero; exitCode carries the raw code in that case
// (typically -1 when the binary was not found).
func DefaultExecutor(ctx context.Context, dir, name string, args []string) (string, string, int, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	stdout := outBuf.String()
	stderr := errBuf.String()
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		} else {
			code = -1
		}
	}
	return stdout, stderr, code, err
}

// Scanner is the top-level entry point. Construct one with NewScanner,
// then call Scan(projectDir) (auto-detect) or one of the language
// runners directly (RunGo / RunJS / RunPython).
type Scanner struct {
	Executor     Executor      // defaults to DefaultExecutor
	Timeout      time.Duration // per-scan wall clock; default 60s
	GoBin        string        // defaults to "govulncheck"
	JSBin        string        // defaults to "npm"
	PyBin        string        // defaults to "pip-audit"
	productionOK bool          // LookPath gate; true only for NewScanner default
}

// NewScanner returns a Scanner with the documented defaults wired in.
// Caller may mutate fields before invoking runners.
func NewScanner() *Scanner {
	return &Scanner{
		Executor:     DefaultExecutor,
		Timeout:      60 * time.Second,
		GoBin:        "govulncheck",
		JSBin:        "npm",
		PyBin:        "pip-audit",
		productionOK: true,
	}
}

// SetTestExecutor swaps the Executor and disables the LookPath gate.
// Intended for unit tests that want the runner to call canned-output
// Executors without resolving the real binary. Returns the receiver so
// the call chain stays compact:
//
//	s := NewScanner().SetTestExecutor(fakeExec(...))
func (s *Scanner) SetTestExecutor(e Executor) *Scanner {
	s.Executor = e
	s.productionOK = false
	return s
}

// Scan auto-detects the project language and invokes the matching
// runner. An Unknown language returns a Report with status=ScannerError
// and a descriptive Stderr so CI logs make the cause obvious.
func (s *Scanner) Scan(ctx context.Context, projectDir string) (*Report, error) {
	lang, err := DetectLanguage(projectDir)
	if err != nil {
		return nil, fmt.Errorf("vuln: detect language in %s: %w", projectDir, err)
	}
	if lang == LangUnknown {
		return &Report{
			Language:    LangUnknown,
			Status:      ScannerError,
			Findings:    nil,
			Stderr:      "no recognised manifest file (go.mod / pyproject.toml / requirements.txt / setup.py / package.json)",
			GeneratedAt: time.Now().UTC(),
			ProjectDir:  projectDir,
		}, nil
	}
	switch lang {
	case LangGo:
		return s.RunGo(ctx, projectDir)
	case LangJS:
		return s.RunJS(ctx, projectDir)
	case LangPython:
		return s.RunPython(ctx, projectDir)
	default:
		return nil, fmt.Errorf("vuln: unsupported language %q", lang)
	}
}

// withTimeout returns a derived context with the scanner's configured
// timeout applied. When Timeout is zero, ctx is returned unchanged.
func (s *Scanner) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if s.Timeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, s.Timeout)
}

// =============================================================================
// Runner: Go — govulncheck
// =============================================================================

// RunGo invokes `govulncheck ./...` from projectDir, parses the JSON
// output, and returns a Report. govulncheck emits a stream of JSON
// messages (one per line) under -json mode; we use the -json flag to
// obtain the structured payload.
//
// govulncheck exit codes:
//   - 0   → no vulnerabilities
//   - 3   → vulnerabilities found (still parsed)
//   - >0  → scan error
//
// When the caller has injected a custom Executor (e.g. in unit tests),
// LookPath is skipped — the Executor is responsible for honouring
// cancellation and returning whatever canned output it wants. LookPath
// only guards the production DefaultExecutor path.
func (s *Scanner) RunGo(ctx context.Context, projectDir string) (*Report, error) {
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()
	bin := s.GoBin
	if bin == "" {
		bin = "govulncheck"
	}
	if s.productionOK {
		if _, err := exec.LookPath(bin); err != nil {
			return &Report{
				Language:    LangGo,
				Status:      ScannerUnavailable,
				Stderr:      fmt.Sprintf("govulncheck binary %q not on PATH", bin),
				GeneratedAt: time.Now().UTC(),
				ProjectDir:  projectDir,
			}, nil
		}
	}
	start := time.Now()
	stdout, stderr, code, err := s.Executor(ctx, projectDir, bin, []string{"-json", "./..."})
	elapsed := time.Since(start)
	report := &Report{
		Language:    LangGo,
		ElapsedMS:   elapsed.Milliseconds(),
		Stdout:      stdout,
		Stderr:      stderr,
		GeneratedAt: time.Now().UTC(),
		ProjectDir:  projectDir,
	}
	if ctxErr := ctx.Err(); ctxErr == context.DeadlineExceeded {
		report.Status = ScannerTimeout
		return report, nil
	}
	// govulncheck exits 3 when vulnerabilities are found — that's a
	// successful scan, not a failure. Anything else with err != nil
	// is a real error.
	if err != nil && code != 3 {
		if errors.Is(err, exec.ErrNotFound) {
			report.Status = ScannerUnavailable
		} else {
			report.Status = ScannerError
		}
		return report, nil
	}
	findings, _ := ParseGoVulnCheck(stdout)
	report.Status = ScannerOK
	report.Findings = findings
	return report, nil
}

// goVulnFinding is the minimal subset of the govulncheck JSON message
// stream we care about. govulncheck emits many message types; we only
// surface the "finding" records that carry a Symbol/Position/Effect.
type goVulnFinding struct {
	Finding struct {
		OSV      string `json:"osv"` // e.g. "GO-2024-1234"
		Symbol   string `json:"symbol"`
		Package  string `json:"package"`
		Version  string `json:"version"`
		Summary  string `json:"summary"`
		Details  string `json:"details"`
		Severity string `json:"severity"` // upstream severity hint if present
	} `json:"finding"`
}

// goVulnOSV is the OSV-level enrichment (CVE / fixed versions /
// references) keyed by the OSV id seen in the finding stream.
type goVulnOSV struct {
	ID       string   `json:"id"`
	Aliases  []string `json:"aliases"`
	Summary  string   `json:"summary"`
	Severity []struct {
		Type  string `json:"type"`
		Score string `json:"score"`
	} `json:"severity"`
	Affected []struct {
		Package struct {
			Name string `json:"name"`
		} `json:"package"`
		Ranges []struct {
			Type   string `json:"type"`
			Events []struct {
				Introduced string `json:"introduced,omitempty"`
				Fixed      string `json:"fixed,omitempty"`
			} `json:"events"`
		} `json:"ranges"`
	} `json:"affected"`
	References []struct {
		Type string `json:"type"`
		URL  string `json:"url"`
	} `json:"references"`
}

// ParseGoVulnCheck converts a stream of govulncheck JSON messages into a
// unified Vulnerability list. The same OSV id may appear multiple times
// (once per affected symbol); we dedupe by OSV id and keep the first
// occurrence's severity/advisory.
//
// govulncheck severity is optional and not standardised; we fall back to
// "medium" when the upstream payload omits it.
func ParseGoVulnCheck(stdout string) ([]Vulnerability, error) {
	var findings []Vulnerability
	seen := map[string]bool{}
	osv := map[string]goVulnOSV{}
	// First pass: collect OSV metadata (it follows the findings stream).
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var os goVulnOSV
		if err := json.Unmarshal([]byte(line), &os); err == nil && os.ID != "" {
			osv[os.ID] = os
		}
	}
	// Second pass: collect findings.
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var f goVulnFinding
		if err := json.Unmarshal([]byte(line), &f); err != nil || f.Finding.OSV == "" {
			continue
		}
		if seen[f.Finding.OSV] {
			continue
		}
		seen[f.Finding.OSV] = true
		v := Vulnerability{
			Package:  f.Finding.Package,
			Summary:  f.Finding.Summary,
			Severity: SeverityMedium,
		}
		if v.Package == "" {
			v.Package = f.Finding.Symbol
		}
		if meta, ok := osv[f.Finding.OSV]; ok {
			// CVE is the first alias matching CVE-* prefix.
			for _, a := range meta.Aliases {
				if strings.HasPrefix(a, "CVE-") {
					v.CVE = a
					break
				}
			}
			if v.CVE == "" {
				v.CVE = meta.ID
			}
			if v.Summary == "" {
				v.Summary = meta.Summary
			}
			// Derive severity from CVSS score when present.
			v.Severity = severityFromCVSS(meta.Severity)
			// Pull first fixed version from the first affected package
			// range (good enough for CI display; full affected-range
			// resolution is out of scope for this runner).
			for _, aff := range meta.Affected {
				for _, rng := range aff.Ranges {
					for _, ev := range rng.Events {
						if ev.Fixed != "" {
							v.FixedIn = ev.Fixed
							break
						}
					}
					if v.FixedIn != "" {
						break
					}
				}
				if v.FixedIn != "" {
					break
				}
			}
			for _, ref := range meta.References {
				if ref.Type == "WEB" {
					v.AdvisoryURL = ref.URL
					break
				}
			}
		} else {
			v.CVE = f.Finding.OSV
		}
		if v.Summary == "" {
			v.Summary = f.Finding.Details
		}
		findings = append(findings, v)
	}
	sortFindings(findings)
	return findings, nil
}

// severityFromCVSS converts a govulncheck/OSV severity block to our
// canonical Severity. When no CVSS score is present we return
// SeverityMedium as a conservative default for CI gating.
func severityFromCVSS(items []struct {
	Type  string `json:"type"`
	Score string `json:"score"`
}) Severity {
	if len(items) == 0 {
		return SeverityMedium
	}
	for _, it := range items {
		if !strings.EqualFold(it.Type, "CVSS_V3") {
			continue
		}
		score, err := parseFloatPrefix(it.Score)
		if err != nil {
			continue
		}
		switch {
		case score >= 9.0:
			return SeverityCritical
		case score >= 7.0:
			return SeverityHigh
		case score >= 4.0:
			return SeverityMedium
		default:
			return SeverityLow
		}
	}
	return SeverityMedium
}

// parseFloatPrefix extracts the leading numeric prefix of strings like
// "7.5/CVSS:3.1" or "AV:N/AC:L/...". Returns an error if no leading
// numeric token exists.
func parseFloatPrefix(s string) (float64, error) {
	s = strings.TrimSpace(s)
	end := 0
	for end < len(s) && (s[end] == '.' || (s[end] >= '0' && s[end] <= '9')) {
		end++
	}
	if end == 0 {
		return 0, fmt.Errorf("no numeric prefix in %q", s)
	}
	return parseFloat(s[:end])
}

// parseFloat is a tiny stdlib-free wrapper to keep this package free of
// strconv cross-import churn. Production paths use strconv elsewhere.
func parseFloat(s string) (float64, error) {
	// Use fmt.Sscanf so we don't pull strconv into the public dep set
	// for a single call site. The error is reported when the input is
	// not a valid decimal.
	var f float64
	n, err := fmt.Sscanf(s, "%f", &f)
	if err != nil || n != 1 {
		return 0, fmt.Errorf("invalid float %q", s)
	}
	return f, nil
}

// =============================================================================
// Runner: JS — npm audit --json
// =============================================================================

// RunJS invokes `npm audit --json` from projectDir and parses the
// machine-readable payload. npm audit returns JSON even when
// vulnerabilities are found; non-zero exit codes are normal here.
func (s *Scanner) RunJS(ctx context.Context, projectDir string) (*Report, error) {
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()
	bin := s.JSBin
	if bin == "" {
		bin = "npm"
	}
	if s.productionOK {
		if _, err := exec.LookPath(bin); err != nil {
			return &Report{
				Language:    LangJS,
				Status:      ScannerUnavailable,
				Stderr:      fmt.Sprintf("npm binary %q not on PATH", bin),
				GeneratedAt: time.Now().UTC(),
				ProjectDir:  projectDir,
			}, nil
		}
	}
	start := time.Now()
	stdout, stderr, _, err := s.Executor(ctx, projectDir, bin, []string{"audit", "--json"})
	elapsed := time.Since(start)
	report := &Report{
		Language:    LangJS,
		ElapsedMS:   elapsed.Milliseconds(),
		Stdout:      stdout,
		Stderr:      stderr,
		GeneratedAt: time.Now().UTC(),
		ProjectDir:  projectDir,
	}
	if ctxErr := ctx.Err(); ctxErr == context.DeadlineExceeded {
		report.Status = ScannerTimeout
		return report, nil
	}
	if err != nil {
		// npm audit exits 1 when vulnerabilities exist; that is still
		// a successful scan. Only treat a hard exec error as failure.
		if !errors.Is(err, exec.ErrNotFound) {
			// continue — attempt to parse whatever stdout we got
		} else {
			report.Status = ScannerUnavailable
			return report, nil
		}
	}
	findings, parseErr := ParseNPMAudit(stdout)
	if parseErr != nil {
		report.Status = ScannerError
		report.Stderr = fmt.Sprintf("parse npm audit output: %v\n%s", parseErr, stderr)
		return report, nil
	}
	report.Status = ScannerOK
	report.Findings = findings
	return report, nil
}

// npmAuditVuln is the npm audit JSON envelope. We only consume the
// fields we surface in Vulnerability; the rest (e.g. fixAvailable) is
// intentionally ignored to keep this package's contract narrow.
type npmAuditMeta struct {
	Vulnerabilities map[string]struct {
		Severity         string            `json:"severity"`
		Via              []json.RawMessage `json:"via"`
		Effects          []string          `json:"effects"`
		Range            string            `json:"range"`
		Nodes            []string          `json:"nodes"`
		FixAvailable     json.RawMessage   `json:"fixAvailable"`
		EffectsOnParents []string          `json:"effectsOnParents,omitempty"`
	} `json:"vulnerabilities"`
}

// npmAuditVia is the per-advisory detail we extract from a `via` entry.
type npmAuditVia struct {
	Source   int      `json:"source"`
	Name     string   `json:"name"`
	Title    string   `json:"title"`
	URL      string   `json:"url"`
	Severity string   `json:"severity"`
	CWE      []string `json:"cwe"`
	CVSS     struct {
		Score        float64 `json:"score"`
		VectorString string  `json:"vectorString"`
	} `json:"cvss"`
	Range string `json:"range"`
}

// ParseNPMAudit converts npm audit JSON into unified Vulnerabilities.
// When the same advisory is referenced by multiple packages we emit one
// finding per package, sorted by severity desc.
func ParseNPMAudit(stdout string) ([]Vulnerability, error) {
	var meta npmAuditMeta
	if err := json.Unmarshal([]byte(stdout), &meta); err != nil {
		return nil, fmt.Errorf("vuln: parse npm audit envelope: %w", err)
	}
	var out []Vulnerability
	for pkg, v := range meta.Vulnerabilities {
		sev, _ := ParseSeverity(v.Severity)
		// Each `via` entry can be a string (transitive name only) or
		// a detailed advisory object. We surface the first advisory
		// object we can decode; missing → fall back to the package-level
		// severity.
		var adv *npmAuditVia
		var summary string
		var fixedIn string
		var advisoryURL string
		for _, raw := range v.Via {
			var candidate npmAuditVia
			if err := json.Unmarshal(raw, &candidate); err == nil && candidate.Title != "" {
				adv = &candidate
				break
			}
		}
		if adv != nil {
			summary = adv.Title
			advisoryURL = adv.URL
			if adv.Severity != "" {
				if s, err := ParseSeverity(adv.Severity); err == nil {
					sev = s
				}
			}
			if strings.HasPrefix(adv.Range, ">=") {
				fixedIn = strings.TrimPrefix(adv.Range, ">=")
			}
		}
		out = append(out, Vulnerability{
			Package:     pkg,
			Severity:    sev,
			Summary:     summary,
			FixedIn:     fixedIn,
			AdvisoryURL: advisoryURL,
		})
	}
	sortFindings(out)
	return out, nil
}

// =============================================================================
// Runner: Python — pip-audit --format json
// =============================================================================

// RunPython invokes `pip-audit --format json` from projectDir. pip-audit
// is normally installed in the project's venv; when missing we surface
// ScannerUnavailable so CI logs make the cause obvious.
func (s *Scanner) RunPython(ctx context.Context, projectDir string) (*Report, error) {
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()
	bin := s.PyBin
	if bin == "" {
		bin = "pip-audit"
	}
	if s.productionOK {
		if _, err := exec.LookPath(bin); err != nil {
			return &Report{
				Language:    LangPython,
				Status:      ScannerUnavailable,
				Stderr:      fmt.Sprintf("pip-audit binary %q not on PATH", bin),
				GeneratedAt: time.Now().UTC(),
				ProjectDir:  projectDir,
			}, nil
		}
	}
	start := time.Now()
	stdout, stderr, _, err := s.Executor(ctx, projectDir, bin, []string{"--format", "json"})
	elapsed := time.Since(start)
	report := &Report{
		Language:    LangPython,
		ElapsedMS:   elapsed.Milliseconds(),
		Stdout:      stdout,
		Stderr:      stderr,
		GeneratedAt: time.Now().UTC(),
		ProjectDir:  projectDir,
	}
	if ctxErr := ctx.Err(); ctxErr == context.DeadlineExceeded {
		report.Status = ScannerTimeout
		return report, nil
	}
	if err != nil {
		if !errors.Is(err, exec.ErrNotFound) {
			// continue — attempt to parse whatever stdout we got
		} else {
			report.Status = ScannerUnavailable
			return report, nil
		}
	}
	findings, parseErr := ParsePipAudit(stdout)
	if parseErr != nil {
		report.Status = ScannerError
		report.Stderr = fmt.Sprintf("parse pip-audit output: %v\n%s", parseErr, stderr)
		return report, nil
	}
	report.Status = ScannerOK
	report.Findings = findings
	return report, nil
}

// pipAuditDep is the pip-audit per-dependency entry. We surface only
// the fields that match Vulnerability.
type pipAuditDep struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Vulns   []struct {
		ID          string   `json:"id"` // e.g. "PYSEC-2024-1234"
		FixVersions []string `json:"fix_versions"`
		Aliases     []string `json:"aliases"`
		Description string   `json:"description"`
		Advisory    string   `json:"advisory"` // URL
		Severity    string   `json:"severity,omitempty"`
		CVSS        struct {
			Score        float64 `json:"score"`
			VectorString string  `json:"vector_string"`
		} `json:"cvss"`
	} `json:"vulns"`
}

// pipAuditEnvelope is the top-level pip-audit document.
type pipAuditEnvelope struct {
	Dependencies []pipAuditDep `json:"dependencies"`
}

// ParsePipAudit converts pip-audit JSON into unified Vulnerabilities.
// One finding is emitted per (package, advisory) pair so multiple
// advisories on the same dep are visible to CI.
func ParsePipAudit(stdout string) ([]Vulnerability, error) {
	var env pipAuditEnvelope
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		return nil, fmt.Errorf("vuln: parse pip-audit envelope: %w", err)
	}
	var out []Vulnerability
	for _, dep := range env.Dependencies {
		for _, v := range dep.Vulns {
			sev := SeverityMedium
			if v.Severity != "" {
				if s, err := ParseSeverity(v.Severity); err == nil {
					sev = s
				}
			} else if v.CVSS.Score > 0 {
				switch {
				case v.CVSS.Score >= 9.0:
					sev = SeverityCritical
				case v.CVSS.Score >= 7.0:
					sev = SeverityHigh
				case v.CVSS.Score >= 4.0:
					sev = SeverityMedium
				default:
					sev = SeverityLow
				}
			}
			cve := v.ID
			for _, a := range v.Aliases {
				if strings.HasPrefix(a, "CVE-") {
					cve = a
					break
				}
			}
			fix := ""
			if len(v.FixVersions) > 0 {
				fix = v.FixVersions[0]
			}
			out = append(out, Vulnerability{
				CVE:         cve,
				Package:     dep.Name,
				Severity:    sev,
				FixedIn:     fix,
				AdvisoryURL: v.Advisory,
				Summary:     v.Description,
			})
		}
	}
	sortFindings(out)
	return out, nil
}

// =============================================================================
// Internal helpers
// =============================================================================

// sortFindings orders findings by (severity desc, package asc, CVE asc)
// for deterministic CLI output.
func sortFindings(findings []Vulnerability) {
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Severity.Weight() != findings[j].Severity.Weight() {
			return findings[i].Severity.Weight() > findings[j].Severity.Weight()
		}
		if findings[i].Package != findings[j].Package {
			return findings[i].Package < findings[j].Package
		}
		return findings[i].CVE < findings[j].CVE
	})
}

// _ ensures io.Discard stays referenced even when no helper uses it;
// this avoids an unused-import error if future helpers are added.
var _ = io.Discard
