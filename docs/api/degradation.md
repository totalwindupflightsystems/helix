# pkg/degradation — API Reference

`import "github.com/totalwindupflightsystems/helix/pkg/degradation"`

Platform graceful-degradation policies

## Signatures (from `go doc`)

```go
package degradation // import "github.com/totalwindupflightsystems/helix/pkg/degradation"

Package degradation encodes Helix platform graceful-degradation policies as
structured Go data, enabling programmatic lookup of which platform action to
take (continue / use fallback / fail fast / notify) when a dependent service is
unhealthy.

Data is derived from specs/SPECIFICATION.md §14.2 (Graceful Degradation).
Where §14.2 specifies which *capabilities* remain available (handled by
pkg/health.DegradationChecker), this package encodes which concrete *platform
actions* to take when a service is degraded.

func FormatApplyResult(res ApplyResult) string
type Action string
    const ActionContinueWithCache Action = "continue_with_cache" ...
type ApplyResult struct{ ... }
type HealthState string
    const HealthHealthy HealthState = "healthy" ...
type NotificationLevel string
    const NotifySilent NotificationLevel = "silent" ...
type Policy struct{ ... }
type Registry struct{ ... }
    func DefaultRegistry() (*Registry, error)
    func NewRegistry() *Registry
type Report struct{ ... }
type Service string
    const ServiceForgejo Service = "forgejo" ...
    func AllServices() []Service
```

## Related

- [docs/api/README.md](README.md) — package index
