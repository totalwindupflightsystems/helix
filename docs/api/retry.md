# pkg/retry — API Reference

`import "github.com/totalwindupflightsystems/helix/pkg/retry"`

Exponential backoff for cross-service calls

## Signatures (from `go doc`)

```go
package retry // import "github.com/totalwindupflightsystems/helix/pkg/retry"

Package retry provides exponential backoff retry logic for cross-service calls.
Used by Forgejo client, Chimera adapter, and all HTTP-dependent components.

Based on specs/cross-component-wiring.md §7 (Error Propagation).

# Package retry — status.go

Registry for tracking named retry policies and their circuit-breaker state.
Provides observability for the retry layer per spec §14.1 (Component Failure
Matrix — circuit breakers + retry policies) and §14.3 (Retry Policies —
4-attempt exponential, 5-failure-in-60s circuit breaker).

var ErrMaxAttemptsExceeded = errors.New("max retry attempts exceeded")
func DoHTTP(ctx context.Context, cfg Config, client *http.Client, req *http.Request) (*http.Response, error)
func FormatStatusTable(report StatusReport) string
func IsHTTPRetryable(statusCode int) bool
func IsRetryable(err error) bool
func WithBackoff[T any](ctx context.Context, cfg Config, fn RetryableFunc[T]) (T, error)
type ChaosInjector struct{ ... }
    func NewChaosInjector(failureRate float64, duration time.Duration) *ChaosInjector
type CircuitState string
    const CircuitClosed CircuitState = "closed" ...
type Config struct{ ... }
    func DefaultConfig() Config
type PolicyStats struct{ ... }
type PolicyStatsSnapshot struct{ ... }
type Registry struct{ ... }
    func DefaultRegistry() *Registry
    func NewRegistry() *Registry
type RetryableFunc[T any] func(ctx context.Context) (T, error)
    func WrapWithChaos[T any](fn RetryableFunc[T], chaos *ChaosInjector) RetryableFunc[T]
type StatusReport struct{ ... }
    func BuildStatusReport(r *Registry) StatusReport
```

## Related

- [docs/api/README.md](README.md) — package index
