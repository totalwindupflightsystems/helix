package ci

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestDefaultTestWorkflow_Structure verifies the default workflow has all required components.
func TestDefaultTestWorkflow_Structure(t *testing.T) {
	w := DefaultTestWorkflow()

	assert.Equal(t, DefaultWorkflowName, w.Name)
	assert.NotNil(t, w.On.Push)
	assert.NotNil(t, w.On.PullRequest)
	assert.Contains(t, w.On.Push.Branches, "master")
	assert.Contains(t, w.On.Push.Branches, "main")
	assert.Contains(t, w.On.PullRequest.Types, "opened")
	assert.Contains(t, w.On.PullRequest.Types, "synchronize")
}

// TestDefaultTestWorkflow_Jobs verifies both unit and integration jobs exist.
func TestDefaultTestWorkflow_Jobs(t *testing.T) {
	w := DefaultTestWorkflow()

	assert.True(t, w.HasUnitJob(), "workflow should have a unit job")
	assert.True(t, w.HasIntegrationJob(), "workflow should have an integration job")
}

// TestDefaultTestWorkflow_UnitJob verifies the unit job structure.
func TestDefaultTestWorkflow_UnitJob(t *testing.T) {
	w := DefaultTestWorkflow()
	unit := w.Jobs[UnitJobName]

	assert.Equal(t, DefaultRunnerImage, unit.RunsOn)
	assert.NotEmpty(t, unit.Steps)

	// Should have checkout, setup-go, test, and coverage gate steps
	var hasCheckout, hasSetupGo, hasTest, hasCoverageGate bool
	for _, step := range unit.Steps {
		if step.Uses == "actions/checkout@v4" {
			hasCheckout = true
		}
		if step.Uses == "actions/setup-go@v5" {
			hasSetupGo = true
		}
		if contains(step.Run, "go test") {
			hasTest = true
		}
		if containsCoverageGate(step.Run) {
			hasCoverageGate = true
		}
	}
	assert.True(t, hasCheckout, "unit job should have checkout step")
	assert.True(t, hasSetupGo, "unit job should have setup-go step")
	assert.True(t, hasTest, "unit job should have go test step")
	assert.True(t, hasCoverageGate, "unit job should have coverage gate step")
}

// TestDefaultTestWorkflow_IntegrationJob verifies the integration job structure.
func TestDefaultTestWorkflow_IntegrationJob(t *testing.T) {
	w := DefaultTestWorkflow()
	integ := w.Jobs[IntegrationJobName]

	assert.Equal(t, DefaultRunnerImage, integ.RunsOn)
	assert.Equal(t, UnitJobName, integ.Needs)
	assert.NotEmpty(t, integ.Steps)

	// Should have a Forgejo service container
	forgejoService, ok := integ.Services["forgejo"]
	require.True(t, ok, "integration job should have forgejo service")
	assert.Equal(t, DefaultForgejoImage, forgejoService.Image)
	assert.Equal(t, "true", forgejoService.Env["FORGEJO__security__INSTALL_LOCK"])
}

// TestDefaultTestWorkflow_Validate verifies the default workflow passes validation.
func TestDefaultTestWorkflow_Validate(t *testing.T) {
	w := DefaultTestWorkflow()
	err := w.Validate()
	assert.NoError(t, err)
}

// TestValidate_MissingName verifies validation catches missing workflow name.
func TestValidate_MissingName(t *testing.T) {
	w := DefaultTestWorkflow()
	w.Name = ""
	err := w.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "name")
}

// TestValidate_NoTriggers verifies validation catches missing triggers.
func TestValidate_NoTriggers(t *testing.T) {
	w := DefaultTestWorkflow()
	w.On = TestTrigger{}
	err := w.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "trigger")
}

// TestValidate_NoJobs verifies validation catches missing jobs.
func TestValidate_NoJobs(t *testing.T) {
	w := DefaultTestWorkflow()
	w.Jobs = map[string]*TestJob{}
	err := w.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "job")
}

// TestValidate_NoUnitJob verifies validation catches missing unit job.
func TestValidate_NoUnitJob(t *testing.T) {
	w := DefaultTestWorkflow()
	delete(w.Jobs, UnitJobName)
	err := w.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unit")
}

// TestValidate_EmptyRunsOn verifies validation catches empty runs-on.
func TestValidate_EmptyRunsOn(t *testing.T) {
	w := DefaultTestWorkflow()
	w.Jobs[UnitJobName].RunsOn = ""
	err := w.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "runs-on")
}

// TestValidate_NoSteps verifies validation catches jobs with no steps.
func TestValidate_NoSteps(t *testing.T) {
	w := DefaultTestWorkflow()
	w.Jobs[UnitJobName].Steps = nil
	err := w.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "step")
}

// TestValidate_StepMissingAction verifies validation catches steps with no uses or run.
func TestValidate_StepMissingAction(t *testing.T) {
	w := DefaultTestWorkflow()
	w.Jobs[UnitJobName].Steps[0] = TestStep{Name: "empty"}
	err := w.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "uses")
}

// TestValidate_IntegrationMissingNeeds verifies integration job must depend on unit.
func TestValidate_IntegrationMissingNeeds(t *testing.T) {
	w := DefaultTestWorkflow()
	w.Jobs[IntegrationJobName].Needs = ""
	err := w.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "depend")
}

// TestHasCoverageGate verifies coverage gate detection.
func TestHasCoverageGate(t *testing.T) {
	w := DefaultTestWorkflow()
	assert.True(t, w.HasCoverageGate())
}

// TestHasForgejoService verifies Forgejo service detection.
func TestHasForgejoService(t *testing.T) {
	w := DefaultTestWorkflow()
	assert.True(t, w.HasForgejoService())
}

// TestHasCoverageGate_NoGate verifies false when no coverage gate step exists.
func TestHasCoverageGate_NoGate(t *testing.T) {
	w := DefaultTestWorkflow()
	w.Jobs[UnitJobName].Steps = []TestStep{
		{Uses: "actions/checkout@v4"},
		{Run: "go test ./..."},
	}
	assert.False(t, w.HasCoverageGate())
}

// TestHasForgejoService_NoService verifies false when no Forgejo service exists.
func TestHasForgejoService_NoService(t *testing.T) {
	w := DefaultTestWorkflow()
	w.Jobs[IntegrationJobName].Services = nil
	assert.False(t, w.HasForgejoService())
}

// TestMarshal_YAMLOutput verifies the workflow can be marshaled to YAML.
func TestMarshal_YAMLOutput(t *testing.T) {
	w := DefaultTestWorkflow()
	data, err := w.Marshal()
	require.NoError(t, err)
	assert.NotEmpty(t, data)

	yamlStr := string(data)
	assert.Contains(t, yamlStr, "name: Test")
	assert.Contains(t, yamlStr, "unit:")
	assert.Contains(t, yamlStr, "integration:")
	assert.Contains(t, yamlStr, "go test")
}

// TestMarshal_InvalidWorkflow verifies marshaling invalid workflow fails.
func TestMarshal_InvalidWorkflow(t *testing.T) {
	w := DefaultTestWorkflow()
	w.Name = ""
	_, err := w.Marshal()
	assert.Error(t, err)
}

// TestParse_ValidYAML verifies parsing a valid workflow YAML.
func TestParse_ValidYAML(t *testing.T) {
	yamlStr := `
name: Test
on:
  push:
    branches: [master]
  pull_request:
    types: [opened]
jobs:
  unit:
    runs-on: ubuntu-24.04
    steps:
      - uses: actions/checkout@v4
      - run: go test ./...
`
	w, err := Parse([]byte(yamlStr))
	require.NoError(t, err)
	assert.Equal(t, "Test", w.Name)
	assert.NotNil(t, w.On.Push)
	assert.True(t, w.HasUnitJob())
}

// TestParse_InvalidYAML verifies parsing invalid YAML fails.
func TestParse_InvalidYAML(t *testing.T) {
	_, err := Parse([]byte("not: valid: yaml:"))
	assert.Error(t, err)
}

// TestRoundTrip verifies marshal → parse → validate produces the same workflow.
func TestRoundTrip(t *testing.T) {
	original := DefaultTestWorkflow()
	data, err := original.Marshal()
	require.NoError(t, err)

	parsed, err := Parse(data)
	require.NoError(t, err)
	assert.Equal(t, original.Name, parsed.Name)
	assert.True(t, parsed.HasUnitJob())
	assert.True(t, parsed.HasIntegrationJob())
	assert.NoError(t, parsed.Validate())
}

// TestCoverageGateScript verifies the coverage gate script contains threshold.
func TestCoverageGateScript(t *testing.T) {
	script := coverageGateScript(85.0)
	assert.Contains(t, script, "85")
	assert.Contains(t, script, "coverage.out")
	assert.Contains(t, script, "cover -func")
}

// TestDefaultBranches verifies the default branch list.
func TestDefaultBranches(t *testing.T) {
	branches := DefaultBranches()
	assert.Contains(t, branches, "master")
	assert.Contains(t, branches, "main")
}

// TestDefaultPRTypes verifies the default PR trigger types.
func TestDefaultPRTypes(t *testing.T) {
	types := DefaultPRTypes()
	assert.Contains(t, types, "opened")
	assert.Contains(t, types, "synchronize")
}

// TestContains verifies the contains helper.
func TestContains(t *testing.T) {
	assert.True(t, contains("hello world", "hello"))
	assert.True(t, contains("hello world", "world"))
	assert.True(t, contains("hello world", "o w"))
	assert.False(t, contains("hello", "hello world"))
	assert.False(t, contains("", "anything"))
	assert.True(t, contains("anything", ""))
}

// TestContainsCoverageGate verifies coverage gate detection logic.
func TestContainsCoverageGate(t *testing.T) {
	assert.True(t, containsCoverageGate("go tool cover -func=coverage.out"))
	assert.False(t, containsCoverageGate("go test ./..."))
	assert.False(t, containsCoverageGate("coverage.out"))
	assert.False(t, containsCoverageGate("cover -func"))
}

// TestWorkflow_ExtraJobs verifies that extra custom jobs are allowed.
func TestWorkflow_ExtraJobs(t *testing.T) {
	w := DefaultTestWorkflow()
	w.Jobs["lint"] = &TestJob{
		RunsOn: DefaultRunnerImage,
		Steps: []TestStep{
			{Run: "golangci-lint run ./..."},
		},
	}
	err := w.Validate()
	assert.NoError(t, err)
}

// TestWorkflow_MarshalYAMLTag verifies that the yaml tags are correct by
// unmarshaling into a generic map and checking key names.
func TestWorkflow_MarshalYAMLTag(t *testing.T) {
	w := DefaultTestWorkflow()
	data, err := yaml.Marshal(w)
	require.NoError(t, err)

	var generic map[string]any
	require.NoError(t, yaml.Unmarshal(data, &generic))

	// Top-level keys
	assert.Contains(t, generic, "name")
	assert.Contains(t, generic, "on")
	assert.Contains(t, generic, "jobs")

	// Jobs map
	jobs, ok := generic["jobs"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, jobs, "unit")
	assert.Contains(t, jobs, "integration")

	// Unit job has runs-on and steps
	unit, ok := jobs["unit"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, unit, "runs-on")
	assert.Contains(t, unit, "steps")
}
