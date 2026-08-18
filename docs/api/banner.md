# pkg/banner — API Reference

`import "github.com/totalwindupflightsystems/helix/pkg/banner"`

ASCII art startup banners for Helix CLI

## Signatures (from `go doc`)

```go
package banner // import "github.com/totalwindupflightsystems/helix/pkg/banner"

Package banner implements ASCII art startup banners for the Helix CLI.

The banners are intentionally ASCII-only (no box-drawing characters) so they
survive copy-paste into chat / tickets / logs without rendering artifacts.
The full banner is 7 lines; the compact variant is 3 lines for tight output
contexts (`helix status`, `helix doctor`, etc.).

Both Render and RenderCompact return the version string on the last line so
operators immediately know which build they're on.

const FullWidth = 53 ...
func Render(version string) string
func RenderCompact(version string) string
```

## Related

- [docs/api/README.md](README.md) — package index
