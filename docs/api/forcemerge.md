# pkg/forcemerge — API Reference

`import "github.com/totalwindupflightsystems/helix/pkg/forcemerge"`

Audit trail for every admin override merge

## Signatures (from `go doc`)

```go
package forcemerge // import "github.com/totalwindupflightsystems/helix/pkg/forcemerge"

Package forcemerge records and audits every Helix PR merge that used the
`force-merge` label — the operator override that lets a human merge a PR without
the co-approval gate (1 human + 1 trusted agent).

Per specs/SPECIFICATION.md §5.4:

    "Human can force-merge without agent approval by applying the
    `force-merge` label. This is logged in the audit trail with human
    identity and justification comment. Use sparingly — defeats the
    co-approval invariant. Agent can NEVER force-merge. No override
    exists for agents. `force-merge` triggers a post-merge review by
    Conscientiousness (was the override justified?)."

And §6.6 (operational hardening):

    "`force-merge` label usage reviewed monthly (should be rare)"

This package provides the data layer: a JSONL append-only audit log of every
force-merge, the Conscientiousness bridge that records the post-merge review
verdict, and the monthly aggregation report used by the §6.6 review.

Design goals:

  - Append-only. Each merge writes one JSONL record; we never rewrite history.
    The Conscientiousness verdict is a separate record that references the merge
    record by PR URL + merge SHA.

  - Validation at the boundary. Justification text is required (≥20 chars per
    spec §5.4 spirit — "Use sparingly" implies a real explanation). Empty or
    short strings are rejected before the record is appended.

  - AuditReport is a pure function over the JSONL. It never mutates the log;
    it only reads and aggregates. The cron job that drives the monthly review
    reads the log and calls AuditReport.

  - The store is a file under ~/.helix/forcemerge-audit.jsonl by default,
    but callers can pass any io.Writer (e.g. tests use bytes.Buffer; ops can
    point at /var/log/helix-forcemerge.jsonl).

Threading: AuditStore is safe for concurrent Record* calls (a sync.Mutex
protects the underlying writer). Reads via AuditReport do not lock — callers
should snapshot the file or pass a snapshot Reader.

const DefaultAuditPath = "~/.helix/forcemerge-audit.jsonl"
const DefaultJustificationMinLen = 20
const ForceMergeLabel = "force-merge"
const MaxJustificationLen = 2000
func ExpandPath(p string) (string, error)
func FormatReport(rep AuditReport) string
func HasForceMergeLabel(labels []string) bool
func ValidateAuditEntry(e AuditEntry) error
func ValidateJustification(s string) error
func ValidateReviewEntry(e ReviewEntry) error
type AuditEntry struct{ ... }
type AuditReport struct{ ... }
    func BuildAuditReport(r io.Reader, now time.Time) (AuditReport, error)
type AuditStore struct{ ... }
    func NewFileStore(path string) (*AuditStore, error)
    func NewWriterStore(w io.Writer) *AuditStore
type HumanUsage struct{ ... }
type MonthlyStats struct{ ... }
type ReviewEntry struct{ ... }
type ReviewStatus string
    const ReviewPending ReviewStatus = "PENDING" ...
```

## Related

- [docs/api/README.md](README.md) — package index
