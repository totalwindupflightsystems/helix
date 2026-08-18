# pkg/log — API Reference

`import "github.com/totalwindupflightsystems/helix/pkg/log"`

Dependency-free structured logging facility

## Signatures (from `go doc`)

```go
package log // import "github.com/totalwindupflightsystems/helix/pkg/log"

Package log provides a dependency-free, structured logging facility for Helix
CLIs and libraries.

Design goals:

  - Zero external dependencies (no zap/logrus/zerolog). The platform binary
    stays small and verifiable.

  - Two output formats: "json" for Splunk/Promtail/Loki ingestion, and "text"
    for human-readable terminal output. Format is chosen via the `format`
    constructor argument or the WithFormat builder.

  - Four well-known levels (Debug, Info, Warn, Error) with numeric ranks so
    callers can compare without string matching.

  - Structured fields. Every Emit carries a free-form map[string]any plus a
    handful of well-known fields (ts, level, msg, app) that are rendered first
    in every format.

Typical usage:

    hl := log.New(os.Stderr, "text", log.LevelInfo)
    hl.Emit(log.LevelInfo, "subcommand_complete", map[string]any{
        "subcommand":   "adversarial",
        "rc":           0,
        "duration_ms":  42,
    })

Thread-safety: Logger is safe for concurrent use; the underlying writer is
wrapped with a sync.Mutex so multi-line JSON entries are not interleaved.

type Level int
    const LevelDebug Level = 0 ...
    func ParseLevel(s string) (Level, error)
type Logger struct{ ... }
    func DefaultLogger() *Logger
    func New(w io.Writer, format string, min Level) (*Logger, error)
```

## Related

- [docs/api/README.md](README.md) — package index
