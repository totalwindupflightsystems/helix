# pkg/sandbox — API Reference

`import "github.com/totalwindupflightsystems/helix/pkg/sandbox"`

Bubblewrap-based agent isolation

## Signatures (from `go doc`)

```go
package sandbox // import "github.com/totalwindupflightsystems/helix/pkg/sandbox"

Package sandbox provides Bubblewrap-based process isolation for the Helix
platform.

This package implements the sandbox primitive that every Helix platform action
flows through — agent spawns, code execution, and tool invocation. It wraps
bubblewrap (bwrap) with configurable isolation levels, cgroup v2 resource
limits, and a structured error taxonomy.

Design goals:
  - Zero daemon: every invocation is a fresh process.
  - Zero config: all parameters come from CLI flags or defaults.
  - Zero images: uses the host filesystem with bind mounts.
  - Stdlib only: no external Go dependencies.

const ExitOK = 0 ...
const BwrapBinary = "/usr/bin/bwrap"
const BwrapVersion = "0.11.1"
const CgroupV2MountPoint = "/sys/fs/cgroup"
const DefaultSessionRoot = "/tmp/helix-sandbox"
var ErrBwrapNotFound = errors.New("sandbox: bubblewrap binary not found")
var ErrConfigInvalid = errors.New("sandbox: configuration is invalid")
var ErrExecutionFailed = errors.New("sandbox: execution failed")
var ErrNotImplemented = errors.New("sandbox: not implemented")
var ErrSetupFailed = errors.New("sandbox: setup failed")
var ErrTimeoutExceeded = errors.New("sandbox: time limit exceeded")
func CheckSessionPermissions(cfg *SandboxConfig) error
func ForbiddenMountSources() []string
func ValidateMountSpec(source, dest string) error
func ValidateStrict(cfg *SandboxConfig) error
type BwrapExecutor struct{ ... }
    func NewExecutor(cfg SandboxConfig) (*BwrapExecutor, error)
type CgroupV2 struct{ ... }
    func NewCgroup(cfg SandboxConfig) *CgroupV2
type IsolationLevel string
    const IsolationNone IsolationLevel = "none" ...
    func ValidIsolationLevels() []IsolationLevel
type MountKind int
    const MountBind MountKind = iota ...
type MountPoint struct{ ... }
    func RequiredMountPoints(level IsolationLevel) []MountPoint
type MountSpec struct{ ... }
    func BuildMountSpec(cfg SandboxConfig) (*MountSpec, error)
type NetworkMode string
    const NetworkNone NetworkMode = "none" ...
type ResourceUsageTracker struct{ ... }
    func NewResourceUsageTracker() *ResourceUsageTracker
type SandboxConfig struct{ ... }
    func DefaultConfig() SandboxConfig
type SecurityCheckResult struct{ ... }
type SecurityReport struct{ ... }
    func ValidateSecurity(cfg *SandboxConfig) *SecurityReport
type SessionSummary struct{ ... }
type UsageReport struct{ ... }
```

## Related

- [docs/api/README.md](README.md) — package index
