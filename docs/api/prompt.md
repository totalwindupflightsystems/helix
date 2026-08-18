# pkg/prompt — API Reference

`import "github.com/totalwindupflightsystems/helix/pkg/prompt"`

Prompt provenance, hash attestation, PromptFoo bridge

## Signatures (from `go doc`)

```go
package prompt // import "github.com/totalwindupflightsystems/helix/pkg/prompt"

Package prompt — runner.go

Prompt-test runner: a Go-side executor for the assertions defined in
.promptfoo.yaml. The full PromptFoo CLI is the source of truth for production
prompt evaluation (spec §10) but its Go integration is limited — this runner
fills the gap for CI pipelines that need pre-deploy verification without
spawning an external process.

Scope (spec §10 — PromptFoo bridge):

 1. Parse .promptfoo.yaml and locate the named prompt's test cases.

 2. Read the prompt file from disk and evaluate each assertion against the
    prompt's content. Supported assertions:

    - contains PASS if substring present in prompt - not-contains PASS if
    substring absent - regex PASS if regex matches - length PASS if string
    length within bounds

 3. Return a structured TestRunReport with per-test-case PASS/FAIL and a summary
    exit code.

The runner does NOT call any LLM. For LLM-rubric or provider-side
assertions, run `promptfoo eval` externally and pipe results through
pkg/prompt.ParsePromptFooResults — this runner is the offline smoke-check used
in CI before invoking PromptFoo.

Package prompt implements the Helix Prompt Registry — prompt storage,
versioning, content-addressed hashing, lifecycle state machine, commit
attestation, PromptFoo CI integration, and provenance chain verification.

See specs/prompt-registry.md for the full design.

const ExitOK = 0 ...
var RegistryDir = "prompts"
var Verbose bool
func AllowedForAttestation(s LifecycleStatus) (allowed bool, warn bool)
func AppendHelixAttestation(commitMsg string, ha *HelixAttestation) (string, error)
func ApplyTransition(metadata *Metadata, to LifecycleStatus) error
func DeprecationGrace(s LifecycleStatus) time.Duration
func ExtractPromptRef(message string) string
func FormatHelixAttestation(ha *HelixAttestation) (string, error)
func FormatProvenanceChain(chain *ProvenanceChain) string
func FormatTamperReport(commitSHA, promptRef, storedHash, computedHash string) string
func GeneratePromptFooYAML(prompts []Prompt) ([]byte, error)
func HasHelixAttestation(commitMsg string) bool
func HasPromptTrailer(message string) bool
func HasValidPromptTrailer(message string) bool
func Hash(prompt string) string
func IsHashFormat(ref string) bool
func IsPathFormat(ref string) bool
func Normalize(prompt string) string
func NormalizeForHash(raw string) string
func ParseCommitMsgFromFile(filePath string) (string, error)
func RebuildIndex() error
func ResolvePromptPath(component, version string) (string, error)
func RunCommitMsgHook(commitMsgFile string) error
func ShouldDeprecate(metadata *Metadata, now time.Time, config AutoDeprecationConfig, ...) bool
func ShouldRetire(metadata *Metadata, now time.Time, config AutoDeprecationConfig) bool
func TransitionStatus(component, version string, to LifecycleStatus, trigger string) error
func UpdateStatus(component, version string, newStatus LifecycleStatus) error
func ValidTransition(from, to LifecycleStatus) bool
func ValidateHelixAttestation(ha *HelixAttestation) error
func ValidateTransition(from, to LifecycleStatus, metadata *Metadata) error
func VerifyHash(prompt string, expectedHash string) bool
func VerifyProvenance(chain *ProvenanceChain) (allOK bool, failures []string)
type Attestation struct{ ... }
    func AttestPrompt(prompt Prompt) (*Attestation, error)
    func GetCommitAttestation(commitSHA, workDir string) (*Attestation, error)
    func HelixAttestationToLegacy(ha *HelixAttestation) *Attestation
    func ParseCommitMessage(msg string) (*Attestation, error)
    func Verify(commitRef string) (*Attestation, error)
type AttestationReport struct{ ... }
type AttestationResult struct{ ... }
    func Attest(att *Attestation, commitSHA, workDir string) (*AttestationResult, error)
    func ValidateAttestation(att *Attestation, workDir string) (*AttestationResult, error)
type AttestationValidator struct{ ... }
    func NewAttestationValidator() *AttestationValidator
type AuditEntry struct{ ... }
type AuditLogger struct{ ... }
    func NewAuditLogger(path string) (*AuditLogger, error)
type AutoDeprecationConfig struct{ ... }
    func DefaultAutoDeprecationConfig() AutoDeprecationConfig
type ChainLink struct{ ... }
type CommitAttestationResult struct{ ... }
type CommitAttestationStatus string
    const CommitAttestValid CommitAttestationStatus = "VALID" ...
type CommitRef struct{ ... }
type ConsistencyCheck struct{ ... }
type ConsistencyReport struct{ ... }
    func CheckIndex(autoRebuild bool) (*ConsistencyReport, error)
type ConsistencyStatus string
    const ConsistencyOK ConsistencyStatus = "ok" ...
type Cost struct{ ... }
type EvalResults struct{ ... }
    func ParsePromptFooResults(results []byte) (*EvalResults, error)
type EvalTestResult struct{ ... }
type HelixAttestation struct{ ... }
    func HelixAttestationFromLegacy(att *Attestation) *HelixAttestation
    func ParseHelixAttestation(commitMsg string) (*HelixAttestation, error)
type Index map[string]map[string]*PromptEntry
type LifecycleStatus string
    const StatusDraft LifecycleStatus = "draft" ...
    func AllowedTransitions(from LifecycleStatus) []LifecycleStatus
type ListFilter struct{ ... }
type Metadata struct{ ... }
    func GetMetadata(component, version string) (*Metadata, error)
    func UpdatePromptFooStatus(component, version, status string) (*Metadata, error)
type MetricsCollector struct{ ... }
    func NewMetricsCollector() *MetricsCollector
type Prompt struct{ ... }
    func Resolve(component, version string) (*Prompt, error)
type PromptDiff struct{ ... }
    func Diff(component, v1, v2 string) (*PromptDiff, error)
type PromptEntry struct{ ... }
type PromptFooAssert struct{ ... }
type PromptFooPrompt struct{ ... }
type PromptFooProvider struct{ ... }
type PromptFooTest struct{ ... }
type PromptFooYAML struct{ ... }
type PromptVersion struct{ ... }
    func List(filter ListFilter) ([]*PromptVersion, error)
    func Lookup(hash string) (*PromptVersion, error)
    func LookupByComponent(component, version string) (*PromptVersion, error)
    func Register(component, version, promptFilePath, model, provider, specRef string, ...) (*PromptVersion, error)
type PromptfooResult struct{ ... }
type ProvenanceChain struct{ ... }
    func WalkProvenance(commitSHA, attestHash, workDir string) (*ProvenanceChain, error)
type ProvenanceSummary struct{ ... }
    func SummarizeProvenance(chain *ProvenanceChain) ProvenanceSummary
type RegisterOptions struct{ ... }
type RunOptions struct{ ... }
type TestRunReport struct{ ... }
    func RunFor(opts RunOptions) (*TestRunReport, error)
type TokenUsage struct{ ... }
type Transition struct{ ... }
type Version struct{ ... }
    func ListVersions(component string) ([]Version, error)
```

## Related

- [docs/api/README.md](README.md) — package index
