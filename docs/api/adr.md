# pkg/adr — API Reference

`import "github.com/totalwindupflightsystems/helix/pkg/adr"`

Architecture Decision Records with co-authoring and multi-model review

## Signatures (from `go doc`)

```go
package adr // import "github.com/totalwindupflightsystems/helix/pkg/adr"

Package adr implements Architecture Decision Records with co-authoring
and multi-model review (Phase 2 §2.2). ADRs follow the MADR format and are
evidence-linked to specs, incidents, and marketplace patterns.

const StatusProposed = "proposed" ...
const EvidenceSpecRef = "spec_ref" ...
const DefaultADRsDir = ".helix/adrs"
const DefaultConsensusThreshold = 0.66
func NewADRID() string
func Slugify(title string) string
func StatusDisplay(status string) string
func ValidStatus(s string) bool
func ValidTransition(from, to string) bool
type ADR struct{ ... }
type ADRCoAuthor struct{ ... }
    func NewADRCoAuthor() *ADRCoAuthor
type ADRModelClient interface{ ... }
    func AdaptReviewModelClient(c review.ModelClient) ADRModelClient
type ADRReviewRequest struct{ ... }
type ADRReviewResult struct{ ... }
type ADRReviewer struct{ ... }
    func NewADRReviewer(clients ...ADRModelClient) *ADRReviewer
type ADRStore struct{ ... }
    func NewADRStore(root string) (*ADRStore, error)
type Alternative struct{ ... }
type ConflictingAssessment struct{ ... }
type EvidenceLink struct{ ... }
type ModelVerdict struct{ ... }
```

## Related

- [docs/api/README.md](README.md) — package index
