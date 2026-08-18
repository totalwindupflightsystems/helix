# pkg/security — API Reference

`import "github.com/totalwindupflightsystems/helix/pkg/security"`

Security hardening checklist verifier

## Signatures (from `go doc`)

```go
package security // import "github.com/totalwindupflightsystems/helix/pkg/security"

Package security implements the security hardening checklist verifier and
incident response engine per spec §6.6 and §6.7.

The hardening checker validates that all deployment and operational security
measures from the spec are in place before the platform goes to production.
Each check returns PASS/FAIL/WARN with detail.

Package security — incident response engine per spec §6.7.

Encodes the 4-level severity classification (SEV-0 through SEV-3) with
structured response procedures. Each severity has a ordered list of
ResponseSteps with action, verification, and expected outcome.

const DefaultIncidentPath = "~/.helix/incidents.jsonl"
func FormatIncident(inc *IncidentRecord) string
func FormatProcedure(proc *ResponseProcedure) string
func FormatStats(stats IncidentStats) string
func FormatTimestamp(t time.Time) string
func SeverityOrder(s Severity) int
type AlertSignal struct{ ... }
type CheckCategory string
    const CategoryDeployment CheckCategory = "deployment" ...
type CheckFunc func() (CheckStatus, string)
type CheckResult struct{ ... }
type CheckStatus string
    const StatusPass CheckStatus = "PASS" ...
    func CheckFileExists(path string) (CheckStatus, string)
    func CheckFilePermissions(path string, want os.FileMode) (CheckStatus, string)
    func CheckPortNotPublic(port int) (CheckStatus, string)
type HardeningCheck struct{ ... }
    func DefaultChecks() []HardeningCheck
type HardeningChecker struct{ ... }
    func NewHardeningChecker() *HardeningChecker
type HardeningReport struct{ ... }
type HardeningSummary struct{ ... }
    func NewHardeningSummary(report *HardeningReport) HardeningSummary
type IncidentRecord struct{ ... }
type IncidentResponseEngine struct{ ... }
    func NewIncidentResponseEngine() *IncidentResponseEngine
type IncidentStats struct{ ... }
type IncidentStatus string
    const IncidentOpen IncidentStatus = "open" ...
type IncidentStore struct{ ... }
    func NewIncidentFileStore(path string) (*IncidentStore, error)
    func NewIncidentWriterStore(w io.Writer) *IncidentStore
type ResponseProcedure struct{ ... }
    func DefaultProcedures() []ResponseProcedure
type ResponseStep struct{ ... }
type Severity string
    const SeveritySEV0 Severity = "SEV-0" ...
    func ClassifyFromAlert(alert AlertSignal) Severity
type SeverityInfo struct{ ... }
    func AllSeverities() []SeverityInfo
```

## Related

- [docs/api/README.md](README.md) — package index
