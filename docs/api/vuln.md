# pkg/vuln — API Reference

`import "github.com/totalwindupflightsystems/helix/pkg/vuln"`

Dependency vulnerability scanner

## Signatures (from `go doc`)

```go
package vuln // import "github.com/totalwindupflightsystems/helix/pkg/vuln"

Package vuln implements the dependency vulnerability scan runner per
specs/SPECIFICATION.md §6.6 (Dependency vulnerability scan).

The package wraps three external scanners — govulncheck (Go), npm audit (Node.js
/ JavaScript), pip-audit (Python) — behind a single, language- aware API.
Each scanner emits a language-specific JSON document; the runners here normalise
the output into a unified Vulnerability report that callers (CI steps, doctor,
dispatch) can act on with a single severity-based exit code.

Design notes:

  - Scanner binaries are NOT required at build or test time. Each language
    runner uses a pluggable Executor function (defaults to exec.CommandContext)
    so unit tests can inject canned stdout/stderr and exit codes without
    invoking the real scanners. Callers constructing a Scanner that points at a
    real path must therefore treat the binary as an external dependency.

  - When the binary is missing (exec.LookPath returns ENOENT), the runner
    records ScannerUnavailable rather than returning an error. This matches the
    rest of Helix's "soft-fail on missing external tool" convention so a clean
    checkout still produces a usable report.

  - Exit codes follow spec §6.6: critical/high → 1, medium → 2, low → 0. Callers
    that want a unified "vulnerabilities found" signal should treat any non-zero
    ExitCode as a CI failure.

func DefaultExecutor(ctx context.Context, dir, name string, args []string) (string, string, int, error)
type Executor func(ctx context.Context, dir, name string, args []string) (stdout string, stderr string, exitCode int, err error)
type Language string
    const LangGo Language = "go" ...
    func DetectLanguage(projectDir string) (Language, error)
type Report struct{ ... }
type Scanner struct{ ... }
    func NewScanner() *Scanner
type ScannerStatus string
    const ScannerOK ScannerStatus = "ok" ...
type Severity string
    const SeverityLow Severity = "low" ...
    func AllSeverities() []Severity
    func ParseSeverity(s string) (Severity, error)
type Vulnerability struct{ ... }
    func ParseGoVulnCheck(stdout string) ([]Vulnerability, error)
    func ParseNPMAudit(stdout string) ([]Vulnerability, error)
    func ParsePipAudit(stdout string) ([]Vulnerability, error)
```

## Related

- [docs/api/README.md](README.md) — package index
