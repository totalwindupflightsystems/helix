# pkg/coordinator — API Reference

`import "github.com/totalwindupflightsystems/helix/pkg/coordinator"`

Full PR lifecycle orchestration composing all services

## Signatures (from `go doc`)

```go
package coordinator // import "github.com/totalwindupflightsystems/helix/pkg/coordinator"

Package coordinator orchestrates the full PR lifecycle by composing every Helix
subsystem in the correct sequence.

Per specs/cross-component-wiring.md, when a PR is opened the platform must:

 1. Pre-flight cost estimate (pkg/estimate)
 2. Multi-model adversarial review (pkg/review)
 3. PR negotiation if agents disagree (pkg/negotiate)
 4. Merge gate validation (pkg/mergegate)
 5. Shadow deployment if merge approved (pkg/verify)
 6. Steady-state surveillance begins (pkg/verify)

The coordinator holds references to each subsystem and calls them in sequence.
Each stage can fail independently without crashing the pipeline — the failure is
recorded in the lifecycle result.

type CoordinatorOption func(*PRLifecycleCoordinator)
    func WithStages(stages ...StageName) CoordinatorOption
type LifecycleDecision string
    const DecisionApproved LifecycleDecision = "APPROVED" ...
type LifecycleResult struct{ ... }
type NegotiationSetup struct{ ... }
type PRLifecycleCoordinator struct{ ... }
    func NewPRLifecycleCoordinator(opts ...CoordinatorOption) *PRLifecycleCoordinator
type PRRequest struct{ ... }
type StageName string
    const StageCostEstimate StageName = "cost_estimate" ...
type StageResult struct{ ... }
type StageStatus string
    const StatusPending StageStatus = "pending" ...
```

## Related

- [docs/api/README.md](README.md) — package index
