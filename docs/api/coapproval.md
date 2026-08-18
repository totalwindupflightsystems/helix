# pkg/coapproval — API Reference

`import "github.com/totalwindupflightsystems/helix/pkg/coapproval"`

Final merge approval gate with multi-model consensus

## Signatures (from `go doc`)

```go
package coapproval // import "github.com/totalwindupflightsystems/helix/pkg/coapproval"

Package coapproval implements the co-approval gate — the final merge gate that
requires both 1 human AND 1 trusted agent to approve a PR before it can be
merged.

Per specs/SPECIFICATION.md §7.2 (Gate Ordering — Co-Approval Gate):

    "Co-Approval Gate (human + agent, async)"

Per specs/SPECIFICATION.md §13.3 (Phase 3 success criteria):

    "PR blocked until 1 human + 1 agent approve"

Trust-based agent approval (per trust-model.md integration points):
  - Agent with trust >= 70 satisfies the agent side alone
  - Agent with trust < 70 requires 2 agents to satisfy
  - Agent with veto power (trust >= 90) can override a single dissent

Approval expiry: approvals expire after 24 hours. If the PR changes after an
approval (new push), the approval is automatically invalidated.

const ApprovalExpiry = 24 * time.Hour ...
type Approval struct{ ... }
type CoApprovalGate struct{ ... }
    func NewCoApprovalGate(prURL, commitSHA string) *CoApprovalGate
    func NewCoApprovalGateWithClock(prURL, commitSHA string, clock func() time.Time) *CoApprovalGate
type EligibilityResult struct{ ... }
type MergeEligibility string
    const EligibilityAllowed MergeEligibility = "ALLOWED" ...
type ReviewerType string
    const ReviewerHuman ReviewerType = "human" ...
```

## Related

- [docs/api/README.md](README.md) — package index
