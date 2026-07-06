// Package ci provides a Go API for generating and validating Forgejo Actions
// workflow YAML, specifically the test CI pipeline defined in spec §12.5.
//
// The materialised workflow (.forgejo/workflows/test.yml) is the canonical
// record; this package allows it to be generated programmatically and
// round-tripped against the spec example for CI compliance verification.
package ci

import (
	"errors"
	"fmt"

	"gopkg.in/yaml.v3"
)

// =============================================================================
// Schema types — Forgejo Actions workflow YAML
// =============================================================================

// TestWorkflow is the top-level Forgejo Actions document for the test CI
// pipeline. It mirrors the YAML structure so yaml.v3 can round-trip it.
type TestWorkflow struct {
	Name string              `yaml:"name"`
	On   TestTrigger         `yaml:"on"`
	Jobs map[string]*TestJob `yaml:"jobs"`
}

// TestTrigger encodes the `on:` block for the test workflow.
type TestTrigger struct {
	Push        *TestTriggerRule `yaml:"push,omitempty"`
	PullRequest *TestTriggerRule `yaml:"pull_request,omitempty"`
}

// TestTriggerRule is the body of a single trigger event.
type TestTriggerRule struct {
	Branches []string `yaml:"branches,omitempty"`
	Types    []string `yaml:"types,omitempty"`
}

// TestJob is one entry under `jobs:`.
type TestJob struct {
	RunsOn   string                 `yaml:"runs-on"`
	Needs    string                 `yaml:"needs,omitempty"`
	Services map[string]TestService `yaml:"services,omitempty"`
	Steps    []TestStep             `yaml:"steps"`
}

// TestService is a service container definition.
type TestService struct {
	Image string            `yaml:"image"`
	Env   map[string]string `yaml:"env,omitempty"`
	Ports []string          `yaml:"ports,omitempty"`
}

// TestStep is a single `steps:` entry.
type TestStep struct {
	Name string         `yaml:"name,omitempty"`
	Uses string         `yaml:"uses,omitempty"`
	With map[string]any `yaml:"with,omitempty"`
	Run  string         `yaml:"run,omitempty"`
}

// =============================================================================
// Defaults — spec §12.5 mirror
// =============================================================================

const (
	// DefaultWorkflowName is the canonical workflow name.
	DefaultWorkflowName = "Test"

	// DefaultRunnerImage is the runner image from spec §12.5.
	DefaultRunnerImage = "ubuntu-24.04"

	// DefaultGoVersion is the Go version for the test pipeline.
	DefaultGoVersion = "1.24"

	// DefaultForgejoImage is the Forgejo service container image.
	DefaultForgejoImage = "codeberg.org/forgejo/forgejo:9"

	// DefaultCoverageThreshold is the minimum coverage percentage (85%).
	DefaultCoverageThreshold = 85.0

	// UnitJobName is the canonical job ID for the unit test job.
	UnitJobName = "unit"

	// IntegrationJobName is the canonical job ID for the integration job.
	IntegrationJobName = "integration"

	// DefaultIntegrationTimeout is the timeout for integration tests.
	DefaultIntegrationTimeout = "120s"
)

// DefaultBranches returns the branches that trigger the workflow.
func DefaultBranches() []string {
	return []string{"master", "main"}
}

// DefaultPRTypes returns the PR event types that trigger the workflow.
func DefaultPRTypes() []string {
	return []string{"opened", "synchronize"}
}

// =============================================================================
// Constructors
// =============================================================================

// DefaultTestWorkflow returns a workflow matching the spec §12.5 example.
// It includes a unit job (with coverage gate) and an integration job
// (with Forgejo service container).
func DefaultTestWorkflow() *TestWorkflow {
	return &TestWorkflow{
		Name: DefaultWorkflowName,
		On: TestTrigger{
			Push: &TestTriggerRule{
				Branches: DefaultBranches(),
			},
			PullRequest: &TestTriggerRule{
				Types: DefaultPRTypes(),
			},
		},
		Jobs: map[string]*TestJob{
			UnitJobName:        defaultUnitJob(),
			IntegrationJobName: defaultIntegrationJob(),
		},
	}
}

// defaultUnitJob returns the unit test job with coverage gate.
func defaultUnitJob() *TestJob {
	return &TestJob{
		RunsOn: DefaultRunnerImage,
		Steps: []TestStep{
			{
				Uses: "actions/checkout@v4",
			},
			{
				Name: "Set up Go",
				Uses: "actions/setup-go@v5",
				With: map[string]any{
					"go-version": DefaultGoVersion,
				},
			},
			{
				Name: "Run unit tests with coverage",
				Run:  "go test ./... -short -cover -coverprofile=coverage.out",
			},
			{
				Name: "Coverage gate (85%)",
				Run:  coverageGateScript(DefaultCoverageThreshold),
			},
		},
	}
}

// defaultIntegrationJob returns the integration test job with Forgejo service.
func defaultIntegrationJob() *TestJob {
	return &TestJob{
		RunsOn: DefaultRunnerImage,
		Needs:  UnitJobName,
		Services: map[string]TestService{
			"forgejo": {
				Image: DefaultForgejoImage,
				Env: map[string]string{
					"FORGEJO__security__INSTALL_LOCK": "true",
					"FORGEJO__server__HTTP_PORT":      "3030",
				},
				Ports: []string{"3030:3030"},
			},
		},
		Steps: []TestStep{
			{
				Uses: "actions/checkout@v4",
			},
			{
				Name: "Set up Go",
				Uses: "actions/setup-go@v5",
				With: map[string]any{
					"go-version": DefaultGoVersion,
				},
			},
			{
				Name: "Run integration tests",
				Run:  "go test ./pkg/integration/... -tags=integration -count=1 -timeout " + DefaultIntegrationTimeout,
			},
		},
	}
}

// coverageGateScript returns the shell script for the coverage gate step.
func coverageGateScript(threshold float64) string {
	return fmt.Sprintf(`COVERAGE=$(go tool cover -func=coverage.out | grep total | awk '{print $3}' | sed 's/%%//')
echo "Total coverage: ${COVERAGE}%%"
if [ $(echo "$COVERAGE < %v" | bc -l) -eq 1 ]; then
  echo "Coverage ${COVERAGE}%% below %v%% threshold"
  exit 1
fi
echo "Coverage ${COVERAGE}%% meets %v%% threshold"`, threshold, threshold, threshold)
}

// =============================================================================
// Validation
// =============================================================================

// Validate checks that the workflow has all required fields per spec §12.5.
func (w *TestWorkflow) Validate() error {
	if w.Name == "" {
		return errors.New("workflow name is required")
	}

	if w.On.Push == nil && w.On.PullRequest == nil {
		return errors.New("at least one trigger (push or pull_request) is required")
	}

	if len(w.Jobs) == 0 {
		return errors.New("at least one job is required")
	}

	// Unit job is mandatory
	unit, ok := w.Jobs[UnitJobName]
	if !ok {
		return fmt.Errorf("unit job (%q) is required", UnitJobName)
	}
	if err := validateJob(unit, UnitJobName); err != nil {
		return err
	}

	// Integration job is optional but if present must be valid
	if integ, ok := w.Jobs[IntegrationJobName]; ok {
		if err := validateJob(integ, IntegrationJobName); err != nil {
			return err
		}
		// Integration job must depend on unit
		if integ.Needs != UnitJobName {
			return fmt.Errorf("integration job must depend on %q, got %q", UnitJobName, integ.Needs)
		}
	}

	return nil
}

// validateJob checks that a single job has required fields.
func validateJob(job *TestJob, name string) error {
	if job.RunsOn == "" {
		return fmt.Errorf("job %q: runs-on is required", name)
	}
	if len(job.Steps) == 0 {
		return fmt.Errorf("job %q: at least one step is required", name)
	}
	for i, step := range job.Steps {
		if step.Uses == "" && step.Run == "" {
			return fmt.Errorf("job %q step %d: must have either 'uses' or 'run'", name, i)
		}
	}
	return nil
}

// HasUnitJob checks if the workflow has a unit job.
func (w *TestWorkflow) HasUnitJob() bool {
	_, ok := w.Jobs[UnitJobName]
	return ok
}

// HasIntegrationJob checks if the workflow has an integration job.
func (w *TestWorkflow) HasIntegrationJob() bool {
	_, ok := w.Jobs[IntegrationJobName]
	return ok
}

// HasCoverageGate checks if the unit job contains a coverage gate step.
func (w *TestWorkflow) HasCoverageGate() bool {
	unit, ok := w.Jobs[UnitJobName]
	if !ok {
		return false
	}
	for _, step := range unit.Steps {
		if step.Run != "" && containsCoverageGate(step.Run) {
			return true
		}
	}
	return false
}

// HasForgejoService checks if the integration job has a Forgejo service container.
func (w *TestWorkflow) HasForgejoService() bool {
	integ, ok := w.Jobs[IntegrationJobName]
	if !ok {
		return false
	}
	_, hasService := integ.Services["forgejo"]
	return hasService
}

// =============================================================================
// Serialization
// =============================================================================

// Marshal renders the workflow to YAML bytes.
func (w *TestWorkflow) Marshal() ([]byte, error) {
	if err := w.Validate(); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}
	return yaml.Marshal(w)
}

// Parse reads a workflow from YAML bytes.
func Parse(data []byte) (*TestWorkflow, error) {
	var w TestWorkflow
	if err := yaml.Unmarshal(data, &w); err != nil {
		return nil, fmt.Errorf("parsing workflow YAML: %w", err)
	}
	return &w, nil
}

// containsCoverageGate checks if a script string contains coverage gating logic.
func containsCoverageGate(script string) bool {
	return contains(script, "coverage.out") && contains(script, "cover -func")
}

// contains is a simple substring check (avoids importing strings for a one-liner).
func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
