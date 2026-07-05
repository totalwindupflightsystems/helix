package ci

import (
	"errors"
	"strings"
	"testing"
)

// =============================================================================
// Default workflow shape — spec §7.7 compliance
// =============================================================================

func TestDefaultWorkflow_HasSpecShape(t *testing.T) {
	w := DefaultWorkflow()
	if w.Name == "" {
		t.Error("default workflow must have a name")
	}
	if w.On.Push == nil {
		t.Fatal("default workflow must have a push trigger")
	}
	if len(w.On.Push.Paths) == 0 {
		t.Error("push trigger must have at least one path filter")
	}
	job, ok := w.Jobs[DefaultJobName]
	if !ok {
		t.Fatalf("default workflow must define job %q", DefaultJobName)
	}
	if job.Container != DefaultImage {
		t.Errorf("Container = %q, want %q", job.Container, DefaultImage)
	}
	if len(job.Steps) < 4 {
		t.Errorf("default job should have ≥4 steps (checkout, setup, install, run, upload); got %d", len(job.Steps))
	}
}

func TestDefaultWorkflow_PathFilterCoversPrompts(t *testing.T) {
	w := DefaultWorkflow()
	if !HasPromptPath(w.On.Push.Paths) {
		t.Errorf("default path filter must include prompts/**; got %v", w.On.Push.Paths)
	}
}

func TestDefaultWorkflow_StepsIncludeSpecSequence(t *testing.T) {
	w := DefaultWorkflow()
	job := w.Jobs[DefaultJobName]
	wantSubstrings := []string{
		"actions/checkout",
		"setup-node",
		"promptfoo",
		"promptfoo eval",
	}
	joined := stepsToString(job.Steps)
	for _, want := range wantSubstrings {
		if !strings.Contains(joined, want) {
			t.Errorf("default steps should mention %q; joined:\n%s", want, joined)
		}
	}
}

func TestDefaultWorkflow_UsesTwoProviders(t *testing.T) {
	// DefaultWorkflow itself doesn't embed the providers list (the
	// providers live in .promptfoo.yaml, not the workflow YAML).
	// Verify the constant still matches the spec example.
	if len(DefaultProviders) != 2 {
		t.Errorf("DefaultProviders should have 2 entries; got %d", len(DefaultProviders))
	}
	for _, want := range []string{"claude-sonnet-4", "gemini-2.5-flash-lite"} {
		joined := strings.Join(DefaultProviders, ",")
		if !strings.Contains(joined, want) {
			t.Errorf("DefaultProviders should include %q", want)
		}
	}
}

func TestDefaultTimeout_IsTwoMinutes(t *testing.T) {
	if DefaultTimeoutMinutes != 2 {
		t.Errorf("DefaultTimeoutMinutes = %d, want 2 (spec §7.7)", DefaultTimeoutMinutes)
	}
}

// =============================================================================
// Validation
// =============================================================================

func TestValidate_NilReturnsError(t *testing.T) {
	var w *Workflow
	if err := w.Validate(); err == nil {
		t.Error("nil workflow should fail validation")
	}
}

func TestValidate_EmptyNameReturnsError(t *testing.T) {
	w := DefaultWorkflow()
	w.Name = ""
	if err := w.Validate(); !errors.Is(err, ErrEmptyWorkflowName) {
		t.Errorf("Validate() = %v, want ErrEmptyWorkflowName", err)
	}
	w.Name = "   "
	if err := w.Validate(); !errors.Is(err, ErrEmptyWorkflowName) {
		t.Errorf("whitespace name should fail validation; got %v", err)
	}
}

func TestValidate_NoTriggersReturnsError(t *testing.T) {
	w := DefaultWorkflow()
	w.On = Trigger{}
	if err := w.Validate(); err == nil {
		t.Error("workflow with no triggers should fail validation")
	}
}

func TestValidate_PushWithoutPathsFails(t *testing.T) {
	w := DefaultWorkflow()
	w.On.Push.Paths = nil
	if err := w.Validate(); !errors.Is(err, ErrEmptyTriggerPaths) {
		t.Errorf("Validate() = %v, want ErrEmptyTriggerPaths", err)
	}
}

func TestValidate_PullRequestWithoutPathsFails(t *testing.T) {
	w := DefaultWorkflow()
	w.On.Push = nil
	w.On.PullRequest = &TriggerRule{Branches: []string{"main"}}
	if err := w.Validate(); !errors.Is(err, ErrEmptyTriggerPaths) {
		t.Errorf("Validate() = %v, want ErrEmptyTriggerPaths", err)
	}
}

func TestValidate_NoJobsFails(t *testing.T) {
	w := DefaultWorkflow()
	w.Jobs = map[string]*Job{}
	if err := w.Validate(); err == nil {
		t.Error("workflow with zero jobs should fail validation")
	}
}

func TestValidate_NilJobFails(t *testing.T) {
	w := DefaultWorkflow()
	w.Jobs["bad"] = nil
	if err := w.Validate(); err == nil {
		t.Error("workflow with nil job should fail validation")
	}
}

func TestValidate_JobWithoutRunsOnOrContainerFails(t *testing.T) {
	w := DefaultWorkflow()
	w.Jobs[DefaultJobName] = &Job{Steps: []Step{{Name: "noop", Run: "true"}}}
	err := w.Validate()
	if !errors.Is(err, ErrEmptyJobRunsOn) {
		t.Errorf("Validate() = %v, want ErrEmptyJobRunsOn", err)
	}
}

func TestValidate_JobWithoutStepsFails(t *testing.T) {
	w := DefaultWorkflow()
	w.Jobs[DefaultJobName] = &Job{RunsOn: "ubuntu-latest"}
	err := w.Validate()
	if !errors.Is(err, ErrEmptyJobSteps) {
		t.Errorf("Validate() = %v, want ErrEmptyJobSteps", err)
	}
}

func TestValidate_JobWithOnlyContainerIsOK(t *testing.T) {
	w := DefaultWorkflow()
	w.Jobs[DefaultJobName] = &Job{Container: "node:20", Steps: []Step{{Name: "noop", Run: "true"}}}
	if err := w.Validate(); err != nil {
		t.Errorf("Container alone should satisfy runs-on requirement; got %v", err)
	}
}

func TestValidate_DefaultWorkflowPasses(t *testing.T) {
	if err := DefaultWorkflow().Validate(); err != nil {
		t.Errorf("DefaultWorkflow().Validate() = %v, want nil", err)
	}
}

// =============================================================================
// Marshal — output shape
// =============================================================================

func TestMarshal_StartsWithHeaderComment(t *testing.T) {
	out, err := DefaultWorkflow().Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(out), "# .forgejo/workflows/prompt-eval.yml") {
		t.Errorf("output should start with header comment; got:\n%s", string(out)[:200])
	}
}

func TestMarshal_ContainsExpectedKeys(t *testing.T) {
	out, err := DefaultWorkflow().Marshal()
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{
		"name:",
		"on:",
		"jobs:",
		"promptfoo:",
		"runs-on:",
		"steps:",
		"actions/checkout",
		"npx promptfoo eval",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("output missing %q", want)
		}
	}
}

func TestMarshal_InvalidWorkflowReturnsError(t *testing.T) {
	w := &Workflow{Name: ""}
	if _, err := w.Marshal(); err == nil {
		t.Error("Marshal on invalid workflow should return error")
	}
}

func TestMarshal_OnKeyIsUnquoted(t *testing.T) {
	// Regression guard: yaml.v3 treats the bare `on` key as a YAML
	// 1.1 boolean literal and quotes it as `"on":`. Marshal must
	// strip the quotes so the generated workflow matches the
	// conventional Forgejo Actions spelling.
	out, err := DefaultWorkflow().Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "\"on\":") {
		t.Errorf("output contains quoted \"on\": key; should be unquoted:\n%s", string(out))
	}
	if !strings.Contains(string(out), "\non:\n") && !strings.Contains(string(out), "\non: ") {
		t.Errorf("output should contain unquoted `on:` key; got:\n%s", string(out))
	}
}

func TestMarshal_Deterministic(t *testing.T) {
	a, err := DefaultWorkflow().Marshal()
	if err != nil {
		t.Fatal(err)
	}
	b, err := DefaultWorkflow().Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Error("Marshal must be deterministic; two runs produced different bytes")
	}
}

// =============================================================================
// Round-trip — Parse after Marshal
// =============================================================================

func TestParse_MarshalThenParsePreservesFields(t *testing.T) {
	original := DefaultWorkflow()
	data, err := original.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Name != original.Name {
		t.Errorf("Name round-trip: %q vs %q", parsed.Name, original.Name)
	}
	if len(parsed.Jobs) != len(original.Jobs) {
		t.Errorf("Jobs count: %d vs %d", len(parsed.Jobs), len(original.Jobs))
	}
	for name, origJob := range original.Jobs {
		parsedJob, ok := parsed.Jobs[name]
		if !ok {
			t.Errorf("job %q missing after round-trip", name)
			continue
		}
		if parsedJob.Container != origJob.Container {
			t.Errorf("job %q container: %q vs %q", name, parsedJob.Container, origJob.Container)
		}
		if len(parsedJob.Steps) != len(origJob.Steps) {
			t.Errorf("job %q steps: %d vs %d", name, len(parsedJob.Steps), len(origJob.Steps))
		}
	}
	if parsed.On.Push == nil || len(parsed.On.Push.Paths) != len(original.On.Push.Paths) {
		t.Errorf("push trigger paths lost in round-trip")
	}
}

func TestParse_GarbageReturnsError(t *testing.T) {
	if _, err := Parse([]byte("not yaml at all:\n  - oops\n ::: bad")); err == nil {
		t.Error("garbage input should fail Parse")
	}
}

// =============================================================================
// Path-filter helpers
// =============================================================================

func TestPromptPathFilter(t *testing.T) {
	if got := PromptPathFilter(); got != "prompts/**" {
		t.Errorf("PromptPathFilter = %q, want prompts/**", got)
	}
}

func TestHasPromptPath(t *testing.T) {
	cases := []struct {
		globs []string
		want  bool
	}{
		{[]string{"prompts/**"}, true},
		{[]string{"prompts/agent-identity/v3.md"}, true},
		{[]string{"src/**"}, false},
		{[]string{}, false},
		{nil, false},
	}
	for _, tc := range cases {
		if got := HasPromptPath(tc.globs); got != tc.want {
			t.Errorf("HasPromptPath(%v) = %v, want %v", tc.globs, got, tc.want)
		}
	}
}

// =============================================================================
// Custom workflows — caller construction path
// =============================================================================

func TestCustomWorkflow_WithPullRequestTrigger(t *testing.T) {
	w := DefaultWorkflow()
	w.On.PullRequest = &TriggerRule{Paths: []string{"prompts/**"}}
	if err := w.Validate(); err != nil {
		t.Errorf("custom workflow with both push and PR triggers should validate; got %v", err)
	}
}

func TestCustomWorkflow_AddEnvAtRepoLevel(t *testing.T) {
	w := DefaultWorkflow()
	w.Env = map[string]string{
		"PROMPTFOO_CACHE_DIR": "/tmp/promptfoo-cache",
	}
	if err := w.Validate(); err != nil {
		t.Errorf("repo-level env should not break validation; got %v", err)
	}
	out, err := w.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "PROMPTFOO_CACHE_DIR") {
		t.Errorf("repo-level env should appear in output")
	}
}

// =============================================================================
// Internal helpers
// =============================================================================

func stepsToString(steps []Step) string {
	var sb strings.Builder
	for _, s := range steps {
		sb.WriteString(s.Name)
		sb.WriteString("|")
		sb.WriteString(s.Uses)
		sb.WriteString("|")
		sb.WriteString(s.Run)
		sb.WriteString("\n")
	}
	return sb.String()
}
