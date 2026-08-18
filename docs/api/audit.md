# pkg/audit — API Reference

`import "github.com/totalwindupflightsystems/helix/pkg/audit"`

12-step audit trail checker per spec

## Signatures (from `go doc`)

```go
package audit // import "github.com/totalwindupflightsystems/helix/pkg/audit"

Package audit implements the 12-step audit trail checker per spec §6.5.

For any merged PR, an auditor MUST be able to trace evidence through all 12
steps of the Helix pipeline. Missing evidence at any step is an audit failure —
the merge is flagged for review.

The checker is a pure Go composition layer: it takes structured evidence from
each pipeline stage (already produced by other Helix packages) and verifies
completeness. It does NOT make live API calls — callers supply the evidence,
the checker validates it.

Package audit — JSON marshaling + (de)serialization for the 12-step audit
evidence chain.

Per specs/SPECIFICATION.md §6.5 (Audit Trail Requirements), an auditor MUST
be able to trace evidence through all 12 steps of the Helix pipeline. Without
persistence the evidence is in-memory only — operators can't audit a past run,
and there's no way to replay a chain into the validation layer after a crash.

This file adds explicit JSON (de)serialization for the polymorphic AuditEvidence
struct: each step has its own struct, but the top-level AuditEvidence uses a
`kind` discriminator field on round-trip so any future variant can be added
without breaking existing JSON files.

Two flavors of persistence:

    audit.MarshalEvidence / UnmarshalEvidence  — single AuditEvidence,
                                                one JSON object per call.

    builder.WriteToFile / ReadFromFile          — JSONL stream semantics:
                                                one evidence event per line,
                                                append-friendly via O_APPEND.

const KindForgejoIssue = "forgejo_issue" ...
func MarshalEvidence(ev *AuditEvidence) ([]byte, error)
func StepDescription(id StepID) string
func StepName(id StepID) string
func WriteJSONL(w io.Writer, ev *AuditEvidence) error
type ApprovalRecord struct{ ... }
type AuditEvidence struct{ ... }
    func ReadJSONL(r io.Reader) (*AuditEvidence, error)
    func UnmarshalEvidence(data []byte) (*AuditEvidence, error)
type AuditReport struct{ ... }
type AxiomWorkItemEvidence struct{ ... }
type ChainBuilder struct{ ... }
    func NewChainBuilder() *ChainBuilder
type Checker struct{ ... }
    func NewChecker() *Checker
    func NewCheckerWithValidators(validators map[StepID]StepValidator) *Checker
type ChimeraReviewEvidence struct{ ... }
type CoApprovalEvidence struct{ ... }
type ConscientiousnessEvidence struct{ ... }
type ForgejoIssueEvidence struct{ ... }
type GitCommitEvidence struct{ ... }
type GitReinsVerdictEvidence struct{ ... }
type Ledger struct{ ... }
    func NewLedger() *Ledger
type LedgerEntry struct{ ... }
type MergeEvidence struct{ ... }
type OpenCodeSessionEvidence struct{ ... }
type PRMetadataEvidence struct{ ... }
type PromptFooCIEvidence struct{ ... }
type PromptFooResult struct{ ... }
type RalphLoopEvidence struct{ ... }
type StepID int
    const StepForgejoIssue StepID = 1 ...
    func AllSteps() []StepID
type StepResult struct{ ... }
type StepValidator func(evidence interface{}) []string
```

## Related

- [docs/api/README.md](README.md) — package index
