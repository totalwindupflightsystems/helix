# pkg/design — API Reference

`import "github.com/totalwindupflightsystems/helix/pkg/design"`

Automated design review via adversarial agents (5 roles)

## Signatures (from `go doc`)

```go
package design // import "github.com/totalwindupflightsystems/helix/pkg/design"

Package design implements automated design review via adversarial
agents (Phase 2 §2.3). A DesignReviewDispatcher dispatches prosecutor
agents (@assumption-buster, @redteam, @cost-auditor, @chaos-engineer,
@consistency-checker) against a spec + ADR design surface and returns a Change
Management View with risk, threat map, cost projection, and PASS/WARN/FAIL
consensus.

const VerdictPASS = "PASS" ...
const AgentConsistencyChecker review.AgentType = "consistency-checker"
func FindRiskLevel(severity string) string
func ValidDesignAspect(a DesignAspect) bool
type AttackVector struct{ ... }
type ConsensusResult struct{ ... }
type DataFlow struct{ ... }
type DesignAspect string
    const AspectAssumption DesignAspect = "assumption" ...
type DesignContext struct{ ... }
type DesignFinding struct{ ... }
    func AssumptionsByRisk(findings []DesignFinding) []DesignFinding
    func FilterFindingsByID(findings []DesignFinding, id string) []DesignFinding
    func FindingsByAspect(findings []DesignFinding, aspect DesignAspect) []DesignFinding
type DesignReviewDispatcher struct{ ... }
    func NewDesignReviewDispatcher() *DesignReviewDispatcher
type DesignReviewReport struct{ ... }
type DesignReviewRequest struct{ ... }
type ThreatMap struct{ ... }
type ThreatService struct{ ... }
type TrustBoundary struct{ ... }
```

## Related

- [docs/api/README.md](README.md) — package index
