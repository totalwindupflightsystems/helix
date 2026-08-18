# pkg/backup — API Reference

`import "github.com/totalwindupflightsystems/helix/pkg/backup"`

Structured backup strategy data and validation

## Signatures (from `go doc`)

```go
package backup // import "github.com/totalwindupflightsystems/helix/pkg/backup"

Package backup encodes the Helix backup strategy as structured Go data,
enabling programmatic validation of backup compliance and generation of restore
procedures.

Data is derived from specs/SPECIFICATION.md §10.1 (Backup Strategy) and §10.2
(Restore Procedure).

func FormatBackupReport(results []ValidationResult) string
func FormatRestorePlan(steps []RestoreStep) string
type BackupManager struct{ ... }
    func NewBackupManager() *BackupManager
    func NewBackupManagerWithTargets(targets []BackupTarget) *BackupManager
type BackupTarget struct{ ... }
type Frequency string
    const FrequencyDaily Frequency = "daily" ...
type FreshnessStatus struct{ ... }
type RestoreStep struct{ ... }
    func RestorePlan(backupDate string) []RestoreStep
type RetentionEntry struct{ ... }
type ValidationResult struct{ ... }
```

## Related

- [docs/api/README.md](README.md) — package index
