# pkg/ideation — API Reference

`import "github.com/totalwindupflightsystems/helix/pkg/ideation"`

Offline idea capture, validation, prioritization, promotion

## Signatures (from `go doc`)

```go
package ideation // import "github.com/totalwindupflightsystems/helix/pkg/ideation"

Package ideation implements Helix offline idea capture, validation,
prioritization, and promotion (Phase 1 §1.1–1.3).

const PositionFor = "for" ...
const SourceHuman = "human" ...
const StatusDraft = "draft" ...
const EvidenceIncident = "incident" ...
const VerdictPass = "pass" ...
const SeverityInfo = "info" ...
const AgentAssumptionBuster = "@assumption-buster" ...
const DefaultIdeasDir = ".helix/ideas"
const DefaultIdeasFile = "ideas.jsonl"
func Capture(store *Store, idea *Idea) error
func EstimateCost(idea *Idea) float64
func NewIdeaID() string
func ValidSource(s string) bool
func ValidStatus(s string) bool
type AdvocacyRecord struct{ ... }
type EvidenceRef struct{ ... }
type Idea struct{ ... }
type IdeaPrioritizer struct{ ... }
    func NewIdeaPrioritizer(path string) *IdeaPrioritizer
type IdeaValidator struct{}
    func NewIdeaValidator() *IdeaValidator
type PrioritizedIdea struct{ ... }
type Roadmap struct{ ... }
type Store struct{ ... }
    func NewStore(path string) (*Store, error)
type ValidationFinding struct{ ... }
type ValidationReport struct{ ... }
```

## Related

- [docs/api/README.md](README.md) — package index
