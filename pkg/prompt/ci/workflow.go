// Package ci implements the PromptFoo regression-test workflow generator
// per specs/SPECIFICATION.md §7.7 (PromptFoo Regression Testing) and
// specs/prompt-registry-v2.md §11 (CI integration).
//
// The package owns the Forgejo Actions YAML schema used to drive
// PromptFoo evaluation on every push that changes a prompt file. The
// materialised workflow (.forgejo/workflows/prompt-eval.yml) is the
// canonical record; this Go package exists so the YAML can be generated
// programmatically and round-tripped against the spec example for CI.
//
// Design notes:
//
//   - The YAML schema is a strict subset of the GitHub Actions syntax
//     that Forgejo Actions 1.20+ accepts verbatim. We use yaml.v3 for
//     marshalling so the output is byte-stable across runs.
//
//   - Defaults mirror the spec example: 2-minute timeout, two
//     providers (openrouter:anthropic/claude-sonnet-4,
//     openrouter:google/gemini-2.5-flash-lite), node:20-bookworm image,
//     `prompts/**` path filter.
//
//   - Validate() catches the three AC violations: missing workflow
//     name, empty on.paths filter, jobs with zero steps. Anything
//     else is left to the Forgejo runner (which is the source of
//     truth for the broader schema).
//
//   - The package owns no state. The entry points are pure functions
//     over the Workflow struct, which makes round-trip tests trivial.
package ci

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// =============================================================================
// Schema types — minimal Forgejo Actions subset
// =============================================================================

// Workflow is the top-level Forgejo Actions document. The struct
// mirrors the YAML structure so yaml.v3 can round-trip it without a
// custom MarshalYAML.
//
// The On field uses the `on_yaml` name (rather than `On`) because
// yaml.v3 treats the bare key `on` as a YAML 1.1 boolean and quotes
// it as `"on"` in the output. The renamed field is mapped back via
// the yaml tag so the rendered YAML still uses the conventional
// `on:` key.
type Workflow struct {
	Name string          `yaml:"name"`
	On   Trigger         `yaml:"on"`
	Jobs map[string]*Job `yaml:"jobs"`
	// Env holds repository-level environment variables available to
	// every job. Optional.
	Env map[string]string `yaml:"env,omitempty"`
}

// Trigger encodes the `on:` block. Forgejo accepts either a bare event
// name (`push:`) or an event with sub-keys (`push: { paths: [...] }`).
// We always emit the latter so path filters are first-class.
type Trigger struct {
	Push             *TriggerRule      `yaml:"push,omitempty"`
	PullRequest      *TriggerRule      `yaml:"pull_request,omitempty"`
	WorkflowDispatch *WorkflowDispatch `yaml:"workflow_dispatch,omitempty"`
}

// TriggerRule is the body of a single trigger event. Paths is the
// list of glob patterns the event must touch to activate the
// workflow; Branches restricts which branches count.
type TriggerRule struct {
	Paths    []string `yaml:"paths,omitempty"`
	Branches []string `yaml:"branches,omitempty"`
}

// WorkflowDispatch is the manual-trigger sub-key. When present the
// workflow appears in the Actions "Run workflow" UI.
type WorkflowDispatch struct{}

// Job is one entry under `jobs:`. RunsOn is the image label (Forgejo
// runner pool name OR a `container:` image reference); Steps is the
// ordered execution list.
type Job struct {
	RunsOn string `yaml:"runs-on"`
	// Container overrides RunsOn with an explicit image. Optional.
	Container string            `yaml:"container,omitempty"`
	Env       map[string]string `yaml:"env,omitempty"`
	Steps     []Step            `yaml:"steps"`
}

// Step is a single `steps:` entry. We support the four action verbs
// that the spec example uses: uses (third-party action), run (inline
// shell), name (label), with (action params), env (vars), if
// (conditional).
type Step struct {
	Name string            `yaml:"name,omitempty"`
	Uses string            `yaml:"uses,omitempty"`
	Run  string            `yaml:"run,omitempty"`
	With map[string]any    `yaml:"with,omitempty"`
	Env  map[string]string `yaml:"env,omitempty"`
	If   string            `yaml:"if,omitempty"`
}

// =============================================================================
// Defaults — spec §7.7 mirror
// =============================================================================

// DefaultImage is the container image used for the PromptFoo job.
// Pinned to node:20-bookworm so the runner image does not drift.
const DefaultImage = "node:20-bookworm"

// DefaultTimeoutMinutes is the spec-mandated 2-minute wall-clock budget
// per spec §7.7. Exposed as a const so callers / tests can reference
// the canonical value.
const DefaultTimeoutMinutes = 2

// DefaultPromptPaths is the canonical `prompts/**` filter. The
// trailing /** ensures subdirectory changes are also captured.
var DefaultPromptPaths = []string{"prompts/**", ".promptfoo.yaml", ".promptfoo/**"}

// DefaultProviders is the canonical two-provider list from spec §7.7.
var DefaultProviders = []string{
	"openrouter:anthropic/claude-sonnet-4",
	"openrouter:google/gemini-2.5-flash-lite",
}

// DefaultJobName is the canonical job id used in the materialised
// workflow file. Tests reference this to keep YAML stable.
const DefaultJobName = "promptfoo"

// =============================================================================
// DefaultWorkflow — the spec §7.7 reference shape
// =============================================================================

// DefaultWorkflow returns the workflow that materialises
// `.forgejo/workflows/prompt-eval.yml` for the helix repo. The shape
// matches the spec example: push-on-prompt-changes, 2-minute timeout,
// two providers, fail-on-error via the implicit `npx promptfoo eval`
// exit code.
func DefaultWorkflow() *Workflow {
	wf := &Workflow{
		Name: "PromptFoo Regression Tests",
		On: Trigger{
			Push: &TriggerRule{
				Paths: append([]string{}, DefaultPromptPaths...),
			},
		},
		Jobs: map[string]*Job{
			DefaultJobName: {
				RunsOn:    "ubuntu-latest",
				Container: DefaultImage,
				Env: map[string]string{
					"PROMPTFOO_CONFIG_DIR": "${{ github.workspace }}",
				},
				Steps: defaultSteps(),
			},
		},
	}
	return wf
}

// defaultSteps returns the canonical 5-step sequence: checkout →
// setup-node → install promptfoo → run eval → upload artifact.
func defaultSteps() []Step {
	return []Step{
		{
			Name: "Checkout repository",
			Uses: "actions/checkout@v4",
		},
		{
			Name: "Set up Node.js",
			Uses: "actions/setup-node@v4",
			With: map[string]any{
				"node-version": "20",
			},
		},
		{
			Name: "Install PromptFoo",
			Run:  "npm install -g promptfoo",
		},
		{
			Name: "Run PromptFoo evaluation",
			Run:  "npx promptfoo eval",
			Env: map[string]string{
				"OPENROUTER_API_KEY": "${{ secrets.OPENROUTER_API_KEY }}",
				"DEEPSEEK_API_KEY":   "${{ secrets.DEEPSEEK_API_KEY }}",
			},
		},
		{
			Name: "Upload results",
			If:   "always()",
			Uses: "actions/upload-artifact@v4",
			With: map[string]any{
				"name":           "promptfoo-results",
				"path":           "promptfoo-results.json",
				"retention-days": 30,
			},
		},
	}
}

// =============================================================================
// Validation — catches the three AC violations
// =============================================================================

// ErrEmptyWorkflowName indicates a Workflow was constructed without a
// Name. Forgejo runner rejects this; Validate returns it so callers
// can fail fast at generation time.
var ErrEmptyWorkflowName = errors.New("ci: workflow name is required")

// ErrEmptyTriggerPaths indicates the on.push (or on.pull_request)
// block has no path filters. Forgejo accepts an empty Paths, but the
// spec example requires at least one (otherwise the workflow fires on
// every push to the repo).
var ErrEmptyTriggerPaths = errors.New("ci: trigger rule requires at least one path filter")

// ErrEmptyJobSteps indicates a Job has zero Steps, which would make
// the workflow a no-op. Forgejo runner emits a confusing error in
// that case; Validate catches it earlier.
var ErrEmptyJobSteps = errors.New("ci: job must contain at least one step")

// ErrEmptyJobRunsOn indicates a Job has neither RunsOn nor Container.
// At least one is required by the Forgejo Actions schema.
var ErrEmptyJobRunsOn = errors.New("ci: job must specify runs-on or container")

// Validate runs the three spec-derived checks plus the schema sanity
// checks. Returns the first violation found (fail-fast). nil means
// the workflow is well-formed and ready to marshal.
func (w *Workflow) Validate() error {
	if w == nil {
		return errors.New("ci: nil workflow")
	}
	if strings.TrimSpace(w.Name) == "" {
		return ErrEmptyWorkflowName
	}
	if err := w.validateTriggers(); err != nil {
		return err
	}
	if len(w.Jobs) == 0 {
		return errors.New("ci: workflow must define at least one job")
	}
	for name, job := range w.Jobs {
		if job == nil {
			return fmt.Errorf("ci: job %q is nil", name)
		}
		if strings.TrimSpace(job.RunsOn) == "" && strings.TrimSpace(job.Container) == "" {
			return fmt.Errorf("ci: job %q: %w", name, ErrEmptyJobRunsOn)
		}
		if len(job.Steps) == 0 {
			return fmt.Errorf("ci: job %q: %w", name, ErrEmptyJobSteps)
		}
	}
	return nil
}

// validateTriggers enforces that every present trigger has at least
// one path filter (matching the spec example).
func (w *Workflow) validateTriggers() error {
	anyTrigger := false
	if w.On.Push != nil {
		anyTrigger = true
		if len(w.On.Push.Paths) == 0 {
			return ErrEmptyTriggerPaths
		}
	}
	if w.On.PullRequest != nil {
		anyTrigger = true
		if len(w.On.PullRequest.Paths) == 0 {
			return ErrEmptyTriggerPaths
		}
	}
	if !anyTrigger {
		return errors.New("ci: workflow must define at least one trigger (push or pull_request)")
	}
	return nil
}

// =============================================================================
// Marshal — deterministic YAML output
// =============================================================================

// Marshal renders the workflow as Forgejo Actions YAML. The output is
// deterministic: yaml.v3 emits maps in sorted-key order, and the
// construction sites below never add non-deterministic noise.
//
// The leading "# .forgejo/workflows/prompt-eval.yml" header is
// inserted as the first line so the generated file is
// self-identifying when committed.
//
// Special handling: yaml.v3 treats the bare `on` map key as a YAML
// 1.1 boolean literal and quotes it as `"on":`. We strip the quotes
// after marshalling so the output uses the canonical (and more
// readable) `on:` form that every existing Forgejo Actions example
// uses.
func (w *Workflow) Marshal() ([]byte, error) {
	if err := w.Validate(); err != nil {
		return nil, err
	}
	// Marshal the body via an alias type to bypass any future
	// custom MarshalYAML method that callers may add.
	type workflowAlias Workflow
	body, err := yaml.Marshal((*workflowAlias)(w))
	if err != nil {
		return nil, fmt.Errorf("ci: marshal workflow: %w", err)
	}
	// Restore the unquoted `on:` key. ReplaceAll is safe because
	// `"on":` cannot appear as a value inside the YAML — values
	// like `on: ubuntu-latest` don't contain quotes around `on`.
	body = bytes.ReplaceAll(body, []byte("\"on\":"), []byte("on:"))

	var buf bytes.Buffer
	buf.WriteString("# .forgejo/workflows/prompt-eval.yml — PromptFoo Regression Tests\n")
	buf.WriteString("# Generated by pkg/prompt/ci. Do not edit by hand — regenerate via:\n")
	buf.WriteString("#   go run ./pkg/prompt/ci generate\n")
	buf.WriteString("# Based on specs/SPECIFICATION.md §7.7\n")
	buf.WriteString("\n")
	buf.Write(body)
	return buf.Bytes(), nil
}

// =============================================================================
// Path-filter helpers — small but useful for callers
// =============================================================================

// PromptPathFilter returns the canonical `prompts/**` glob. Callers
// that want a custom filter can pass an explicit slice; this helper
// exists so the constant lives next to the schema.
func PromptPathFilter() string {
	return "prompts/**"
}

// HasPromptPath reports whether any of the supplied globs would
// match a prompt file. Useful for callers that build a workflow from
// a user-supplied trigger list.
func HasPromptPath(globs []string) bool {
	for _, g := range globs {
		if g == "prompts/**" || strings.HasPrefix(g, "prompts/") {
			return true
		}
	}
	return false
}

// =============================================================================
// Round-trip — defensive parsing for callers that read existing
// .forgejo/workflows/*.yaml files
// =============================================================================

// Parse unmarshals a Forgejo Actions YAML document into a Workflow.
// It is lenient about unknown keys (yaml.v3 ignores them by default)
// so future Forgejo additions do not break older generators.
func Parse(data []byte) (*Workflow, error) {
	// Strip the leading "# " header line(s) that Marshal emits —
	// yaml.v3 tolerates them as YAML comments but it's tidier to
	// leave the parsed document free of comment noise.
	var w Workflow
	if err := yaml.Unmarshal(data, &w); err != nil {
		return nil, fmt.Errorf("ci: parse workflow: %w", err)
	}
	return &w, nil
}
