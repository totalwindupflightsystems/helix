# pkg/mergegate — API Reference

`import "github.com/totalwindupflightsystems/helix/pkg/mergegate"`

Pre-receive hook: trust-tier, secrets, attestation enforcement

## Signatures (from `go doc`)

```go
package mergegate // import "github.com/totalwindupflightsystems/helix/pkg/mergegate"

Package mergegate implements the pre-merge validation gate that composes all
Helix quality checks into a single decision point.

Per specs:
  - adversarial-review.md §Integration Points: "GitReins pre-commit: Blocks
    merges without valid evidence bundles"
  - production-verification.md §Integration Points: "GitReins merge gate:
    Verifies behavior contract exists and is valid before merge"
  - trust-model.md §Integration Points: "GitReins pre-commit: Block merges from
    agents below required trust tier for changed file categories"

The MergeGate validates five preconditions before allowing a merge:
 1. Evidence bundle exists and signatures are valid
 2. Behavior contract exists and assertions are well-formed
 3. Trust tier meets minimum requirement for changed file categories
 4. Consensus threshold was met (from adversarial review)
 5. Cost guard was approved (within tier budget)

# Package mergegate — hook.go

Pre-receive hook evaluation logic. This file implements the server-side gate
enforcement that Forgejo (or any git server) invokes via a pre-receive hook.
The hook reads pushed refs from stdin (standard git pre-receive protocol),
determines whether any protected branch is affected, collects the changed files,
and runs the merge gate pipeline.

Design:
  - The bash script (scripts/helix-pre-receive.sh) is a thin wrapper that pipes
    stdin to `helix mergegate hook`.
  - The Go code does the real work: parsing refs, calling git to list changed
    files, evaluating the gate, and printing a structured accept/reject message.
  - Exit 0 = ALLOW push. Exit 1 = REJECT push.

Spec: specs/plans/phase-7-8-negotiate-merge.md §Gap 2-4, §8.1-8.3

func EvaluateHook(cfg HookConfig, stdin io.Reader, stdout, stderr io.Writer) error
func PipelineSummary(report *PipelineReport) string
type CheckResult struct{ ... }
type CheckStatus string
    const CheckPass CheckStatus = "PASS" ...
type FileCategory string
    const CatIaC FileCategory = "iac" ...
type Gate interface{ ... }
type GateInput struct{ ... }
type GateName string
    const GateGitReinsTier1 GateName = "gitreins_tier1" ...
    func GateOrder() []GateName
type GateOption func(*MergeGate)
    func WithContractSkipped() GateOption
    func WithCostSkipped() GateOption
    func WithSignedBundleRequired() GateOption
type GatePipeline struct{ ... }
    func NewDefaultPipeline() *GatePipeline
    func NewGatePipeline(config PipelineConfig, gates ...Gate) *GatePipeline
type GateReport struct{ ... }
type GateResult struct{ ... }
type HookConfig struct{ ... }
    func DefaultHookConfig() HookConfig
type HookOutput struct{ ... }
type HookRef struct{ ... }
type HookResult struct{ ... }
type MergeDecision string
    const DecisionAllowed MergeDecision = "ALLOWED" ...
type MergeGate struct{ ... }
    func NewMergeGate(opts ...GateOption) *MergeGate
type MergeRequest struct{ ... }
type PipelineConfig struct{ ... }
    func DefaultPipelineConfig() PipelineConfig
type PipelineReport struct{ ... }
type StubGate struct{ ... }
    func NewFailingStub(name GateName, evidence string) *StubGate
    func NewPassingStub(name GateName) *StubGate
```

## Related

- [docs/api/README.md](README.md) — package index
