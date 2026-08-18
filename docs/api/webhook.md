# pkg/webhook — API Reference

`import "github.com/totalwindupflightsystems/helix/pkg/webhook"`

Forgejo webhook event receiver

## Signatures (from `go doc`)

```go
package webhook // import "github.com/totalwindupflightsystems/helix/pkg/webhook"

Package webhook implements the receiving side of Forgejo webhook events.
Per specs/cross-component-wiring.md §2.1 (Forgejo → Chimera), when a PR is
opened/updated/closed/reviewed/labeled, Forgejo fires an HTTP POST to Helix.
This package:

 1. Verifies the X-Forgejo-Signature HMAC-SHA256 header against a shared secret
    (constant-time compare — defends against timing side-channels).
 2. Parses the JSON payload into a polymorphic PullRequestEvent (one of 5 event
    types per the Forgejo Actions webhook schema).
 3. Serves a minimal HTTP server that prints parsed events as JSON.

This package is the ingestion half. The action half (firing review, merge-gate,
etc.) lives in the coordinator — wiring parsed events into coordinator.Execute
is a separate task.

const SignatureHeader = "X-Forgejo-Signature"
var ErrMissingSignature = errors.New("webhook: missing signature header")
var ErrSignatureMismatch = errors.New("webhook: signature mismatch")
func ListenAndServe(addr string, h *Handler) error
func Serve(addr string, secret []byte) error
func VerifySignature(payload []byte, signatureHeader string, secret []byte) bool
type EventType string
    const EventPROpened EventType = "pull_request_opened" ...
type Handler struct{ ... }
    func NewHandler(secret []byte) *Handler
type PullRequestEvent struct{ ... }
    func ParsePullRequestEvent(payload []byte, eventType string) (*PullRequestEvent, error)
type SecretFunc func() []byte
    func StaticSecret(secret []byte) SecretFunc
```

## Related

- [docs/api/README.md](README.md) — package index
