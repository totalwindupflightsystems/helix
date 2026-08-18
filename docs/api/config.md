# pkg/config — API Reference

`import "github.com/totalwindupflightsystems/helix/pkg/config"`

Unified platform configuration loading

## Signatures (from `go doc`)

```go
package config // import "github.com/totalwindupflightsystems/helix/pkg/config"

Package config provides platform-wide configuration loading for Helix CLI tools.
It implements the configuration model from specs/helix-config.md including the
~/.helix/config.yaml schema, defaults, validation, and the 5-tier configuration
loading order (defaults → file → pricing → env → flags).

func FormatEnvVarReport(envvars []EnvVar, env map[string]string, loader EnvLoader) string
func MissingRequiredVars(envvars []EnvVar, env map[string]string, loader EnvLoader) []string
type BudgetConfig struct{ ... }
type BudgetTierConfig struct{ ... }
type ChimeraConfig struct{ ... }
type Config struct{ ... }
    func Defaults() *Config
    func Load(path string) (*Config, error)
type ConfigError struct{ ... }
type ConfigErrors []ConfigError
type DotEnvLoader struct{ ... }
type EnvLoader interface{ ... }
type EnvSource string
    const SourceDotenv EnvSource = ".env" ...
type EnvVar struct{ ... }
    func DefaultEnvVars() []EnvVar
type EnvVarGroup struct{ ... }
    func GroupByService(envvars []EnvVar) []EnvVarGroup
type EnvVarReport struct{ ... }
type EstimatorConfig struct{ ... }
type ForgejoConfig struct{ ... }
type GitReinsConfig struct{ ... }
type IdentityConfig struct{ ... }
type InventoryReport struct{ ... }
    func ValidateEnvVars(envvars []EnvVar, env map[string]string, loader EnvLoader) InventoryReport
type LangFuseConfig struct{ ... }
type MarketplaceConfig struct{ ... }
type NegotiationConfig struct{ ... }
type ProcessEnvLoader struct{}
type PromptsConfig struct{ ... }
type SecretsConfig struct{ ... }
type ServiceTarget struct{ ... }
type ServicesConfig struct{ ... }
type Severity string
    const SeverityError Severity = "error" ...
```

## Related

- [docs/api/README.md](README.md) — package index
