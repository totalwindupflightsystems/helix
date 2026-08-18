# pkg/memory — API Reference

`import "github.com/totalwindupflightsystems/helix/pkg/memory"`

DuckBrain and Hivemind memory schema types and interfaces

## Signatures (from `go doc`)

```go
package memory // import "github.com/totalwindupflightsystems/helix/pkg/memory"

Package memory implements DuckBrain and Hivemind memory schema types and the
Hivemind memory bank lifecycle. It encodes the spec §8.5 (DuckBrain Memory
Schema) and §8.6 (Hivemind Memory Bank Lifecycle) contracts as Go types with
full validation, supersession chain tracking, and a pluggable MemoryStore
interface for write/query/delete operations.

The package is pure: no I/O is performed directly by this code. Persisting
entries is the responsibility of callers implementing MemoryStore (typically
backed by git, an MCP client, or an embedded DuckDB).

Package memory — schema_validator.go

Per-record DuckBrain memory validation per spec §8.5. The validator is the
contract enforcement layer between raw MemoryEntry records (produced by the
persistence bridge or a manual write) and the VSS-backed storage layer.

It enforces:

 1. Required fields (id, content, agent_id, created_at, namespace,
    schema_version) — surfaced as per-field errors so callers can report exactly
    which fields are missing in a batch.

 2. Embedding dimensions must match the configured VSS index size. Default
    1536 (text-embedding-3-small); other models use 768 (BERT-base) or 3072
    (text-embedding-3-large).

 3. Content hash = sha256(content). The hash field is required and must equal
    the hex-encoded SHA-256 of the content string.

 4. No PII patterns in content or attributes (email, US SSN, credit-card).
    PII detection is best-effort regex — matches the purpose of catching
    accidental secrets before they hit long-term storage. Not a substitute for a
    real secret scanner.

 5. ID format: "helix://memory/<sha256-hex>" — sha256 of the content,
    64 lowercase hex chars. Validated with regex.

The validator returns a ValidationReport (rather than `error`) so a caller
running it across a batch of records can collect every failure before deciding
what to do. ValidationError is provided for callers that want the fast-fail
path.

const BatchWindow = 5 * time.Minute ...
const EmbeddingDimOpenAISmall = 1536 ...
const CurrentSchemaVersion = "1.0.0"
const DefaultEmbeddingDim = EmbeddingDimOpenAISmall
var ErrNotFound = errors.New("memory: entry not found") ...
func ApplySupersession(store MemoryStore, previousKey, newKey string) error
func ContentHashOf(content string) string
func Path(key string) string
func RootOf(key string) (string, error)
func ValidNamespace(ns Namespace) bool
func ValidateKey(key string) error
type Attributes struct{ ... }
type BatchReport struct{ ... }
type CompiledEntry struct{ ... }
type Compiler struct{ ... }
    func NewCompiler(in *Inbox) *Compiler
type Domain string
    const DomainConcept Domain = "concept" ...
    func AllDomains() []Domain
type EventType string
    const EventGateFailure EventType = "gate_failure" ...
type FieldError struct{ ... }
type Inbox struct{ ... }
    func NewInbox(capacity int) *Inbox
type InboxEvent struct{ ... }
type Index struct{ ... }
    func BuildIndex(store MemoryStore, ns Namespace, now time.Time) (Index, error)
type IndexEntry struct{ ... }
type Lifecycle struct{ ... }
    func NewLifecycle(in *Inbox, store MemoryStore) *Lifecycle
type MemStore struct{ ... }
    func NewMemStore() *MemStore
type MemoryEntry struct{ ... }
    func SupersessionChain(store MemoryStore, startKey string) ([]MemoryEntry, error)
type MemoryQuery struct{ ... }
type MemoryRecord struct{ ... }
type MemorySchemaValidator struct{ ... }
    func NewMemorySchemaValidator() *MemorySchemaValidator
type MemoryStore interface{ ... }
type Namespace string
    const NSAgentsDecisions Namespace = "agents/decisions" ...
    func AllNamespaces() []Namespace
    func NamespaceOf(key string) (Namespace, error)
type PersistenceBridge struct{ ... }
    func NewPersistenceBridge(store MemoryStore) *PersistenceBridge
type ValidationError struct{ ... }
type ValidationReport struct{ ... }
```

## Related

- [docs/api/README.md](README.md) — package index
