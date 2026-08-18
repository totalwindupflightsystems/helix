# pkg/pipeline — API Reference

`import "github.com/totalwindupflightsystems/helix/pkg/pipeline"`

12-step PR lifecycle state machine

## Signatures (from `go doc`)

```go
package pipeline // import "github.com/totalwindupflightsystems/helix/pkg/pipeline"

Package pipeline encodes the 12-step Helix PR lifecycle as a state machine with
transitions, preconditions, data contracts, and latency budgets.

Data is derived from specs/SPECIFICATION.md §1.5 (12-Step Flow) and §2.2
(Step-by-Step State Transitions and Data Contracts).

type DataContract struct{ ... }
    func GetDataContract(step StepID) DataContract
type PipelineState struct{ ... }
type PipelineStateMachine struct{ ... }
    func LoadState(path string) (*PipelineStateMachine, error)
    func NewPipelineStateMachine(prID string) *PipelineStateMachine
type StepID string
    const StepIdle StepID = "idle" ...
    const StateFailed StepID = "failed" ...
    func AllSteps() []StepID
type StepInfo struct{ ... }
    func GetStepInfo(step StepID) StepInfo
type Transition struct{ ... }
```

## Related

- [docs/api/README.md](README.md) — package index
